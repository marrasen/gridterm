package jobs

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/marrasen/gridterm/vfs"
)

// slow is a filesystem that can be made to stop part way through reading
// a file, so a test can cancel a job while it is really copying.
type slow struct {
	vfs.FS

	// reading is closed once a file is being read, and held is what the
	// read waits on before it gives anything back.
	once    sync.Once
	reading chan struct{}
	held    chan struct{}

	// fail is what a read returns instead, for a test about a source
	// that breaks part way through.
	fail error
}

func newSlow(f vfs.FS) *slow {
	return &slow{FS: f, reading: make(chan struct{}), held: make(chan struct{})}
}

// Open wraps the file so the test can hold the read.
func (s *slow) Open(path string) (io.ReadCloser, error) {
	r, err := s.FS.Open(path)
	if err != nil {
		return nil, err
	}
	return &heldReader{s: s, ReadCloser: r}, nil
}

// heldReader gives the first part of a file and then waits.
type heldReader struct {
	s *slow
	io.ReadCloser
	gave bool
}

func (h *heldReader) Read(p []byte) (int, error) {
	if h.gave {
		h.s.once.Do(func() { close(h.s.reading) })
		<-h.s.held
		if h.s.fail != nil {
			return 0, h.s.fail
		}
		return h.ReadCloser.Read(p)
	}
	h.gave = true
	// A first mouthful, so something has really been written before the
	// test pulls the rug.
	if len(p) > 8 {
		p = p[:8]
	}
	return h.ReadCloser.Read(p)
}

// Cancelling stops the job and takes the half-written file away. One
// left behind would be mistaken later for a file that arrived.
func TestCancelTakesTheHalfWrittenFileAway(t *testing.T) {
	from, to := local(t), local(t)
	write(t, from.real, "big.txt", strings.Repeat("x", 4096))
	held := newSlow(from.fs)

	q := New(1)
	j := q.Start(t.Context(), Op{
		Kind: Copy, From: held, At: from.at, Names: []string{"big.txt"},
		To: to.fs, Into: to.at,
	}, Options{})

	select {
	case <-held.reading:
	case <-time.After(budget):
		t.Fatal("the job never started reading")
	}
	// It has written something by now, so there is a part-written file
	// to take away.
	if _, err := os.Stat(filepath.Join(to.real, "big.txt")); err != nil {
		t.Fatalf("nothing was written before cancelling: %v", err)
	}

	j.Cancel()
	close(held.held)

	err := ends(t, j)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want it to say it was cancelled", err)
	}
	gone(t, to.real, "big.txt")
}

// A read that fails part way stops the job and takes away what it had
// written. Carrying on would leave a file that is shorter than the one
// it was copied from, with nothing saying so.
func TestAFailedReadStopsTheJobAndLeavesNothing(t *testing.T) {
	from, to := local(t), local(t)
	write(t, from.real, "big.txt", strings.Repeat("x", 4096))
	held := newSlow(from.fs)
	held.fail = errors.New("the disk went away")

	q := New(1)
	j := q.Start(t.Context(), Op{
		Kind: Copy, From: held, At: from.at, Names: []string{"big.txt"},
		To: to.fs, Into: to.at,
	}, Options{})

	select {
	case <-held.reading:
	case <-time.After(budget):
		t.Fatal("the job never started reading")
	}
	close(held.held)

	err := ends(t, j)
	if err == nil {
		t.Fatal("a read that failed was reported as a job that finished")
	}
	if !strings.Contains(err.Error(), "the disk went away") {
		t.Fatalf("error = %v, want the reason in it", err)
	}
	gone(t, to.real, "big.txt")
}

// A job that cannot be started says so rather than running and failing
// somewhere the user cannot see.
func TestAJobThatMakesNoSense(t *testing.T) {
	here := local(t)
	cases := []struct {
		name string
		op   Op
	}{
		{"nothing to work from", Op{Kind: Copy, Names: []string{"one"}, To: here.fs}},
		{"nothing named", Op{Kind: Copy, From: here.fs, To: here.fs, Into: here.at}},
		{"nowhere to put it", Op{Kind: Copy, From: here.fs, At: here.at, Names: []string{"one"}}},
		{"a name that is not one", Op{
			Kind: Copy, From: here.fs, At: here.at, Names: []string{".."},
			To: here.fs, Into: here.at + "x",
		}},
		{"where it already is", Op{
			Kind: Copy, From: here.fs, At: here.at, Names: []string{"one"},
			To: here.fs, Into: here.at,
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			q := New(1)
			j := q.Start(t.Context(), tc.op, Options{})
			if err := ends(t, j); err == nil {
				t.Fatal("it was accepted")
			}
		})
	}
}

// Only so many run at once: more than a few on one connection makes
// every one of them slower rather than the set of them faster.
func TestOnlySoManyRunAtOnce(t *testing.T) {
	from, to := local(t), local(t)
	for _, name := range []string{"one.txt", "two.txt", "three.txt", "four.txt"} {
		write(t, from.real, name, strings.Repeat("x", 4096))
	}

	// Each job waits on the same gate, so all of them that started are
	// running at the moment the gate is opened.
	held := newSlow(from.fs)
	var running sync.WaitGroup
	q := New(2)
	var started []*Job
	for _, name := range []string{"one.txt", "two.txt", "three.txt", "four.txt"} {
		running.Add(1)
		j := q.Start(t.Context(), Op{
			Kind: Copy, From: held, At: from.at, Names: []string{name},
			To: to.fs, Into: to.at,
		}, Options{})
		started = append(started, j)
		go func() { defer running.Done(); <-j.Done() }()
	}

	select {
	case <-held.reading:
	case <-time.After(budget):
		t.Fatal("nothing started reading")
	}
	// Two are being copied and two have not been touched.
	live := 0
	for _, j := range started {
		if _, err := os.Stat(filepath.Join(to.real, vfs.Base(to.fs, j.Op().Names[0]))); err == nil {
			live++
		}
	}
	if live > 2 {
		t.Fatalf("%d jobs are writing at once, want at most 2", live)
	}

	close(held.held)
	running.Wait()
	for _, j := range started {
		if err := j.Progress().Err; err != nil {
			t.Fatalf("a job failed: %v", err)
		}
	}
	for _, name := range []string{"one.txt", "two.txt", "three.txt", "four.txt"} {
		if got := len(read(t, to.real, name)); got != 4096 {
			t.Errorf("%s is %d bytes", name, got)
		}
	}
}

// Cancelling one job leaves the ones behind it alone.
func TestCancellingOneJobLeavesTheOthers(t *testing.T) {
	from, to := local(t), local(t)
	write(t, from.real, "one.txt", "one")
	write(t, from.real, "two.txt", "two")

	q := New(1)
	first := q.Start(t.Context(), Op{
		Kind: Copy, From: newSlow(from.fs), At: from.at, Names: []string{"one.txt"},
		To: to.fs, Into: to.at,
	}, Options{})
	second := q.Start(t.Context(), Op{
		Kind: Copy, From: from.fs, At: from.at, Names: []string{"two.txt"},
		To: to.fs, Into: to.at,
	}, Options{})

	first.Cancel()
	if err := ends(t, second); err != nil {
		t.Fatalf("the second job: %v", err)
	}
	if got := read(t, to.real, "two.txt"); got != "two" {
		t.Fatalf("the second job wrote %q", got)
	}
}

// The queue holds what it has run, and lets go of what has finished.
func TestTheQueueKeepsAndClears(t *testing.T) {
	from, to := local(t), local(t)
	write(t, from.real, "one.txt", "one")

	q := New(1)
	j := q.Start(t.Context(), Op{
		Kind: Copy, From: from.fs, At: from.at, Names: []string{"one.txt"},
		To: to.fs, Into: to.at,
	}, Options{})
	if got := len(q.Jobs()); got != 1 {
		t.Fatalf("the queue holds %d jobs", got)
	}
	if err := ends(t, j); err != nil {
		t.Fatalf("the job: %v", err)
	}
	// A finished job stays until it is cleared: what it did is worth
	// reading afterwards.
	if got := len(q.Jobs()); got != 1 {
		t.Fatalf("the queue holds %d jobs after one finished", got)
	}
	if got := q.DropFinished(); got != 1 {
		t.Fatalf("clearing took %d jobs off", got)
	}
	if got := len(q.Jobs()); got != 0 {
		t.Fatalf("the queue still holds %d jobs", got)
	}
}

// A job named by what it does, for the panel.
func TestJobName(t *testing.T) {
	here, there := local(t), local(t)
	one := &Job{op: Op{Kind: Copy, From: here.fs, Names: []string{"one.txt"}, To: there.fs}}
	if got := one.Name(); !strings.Contains(got, "one.txt") || !strings.Contains(got, "Local") {
		t.Errorf("one file reads as %q", got)
	}
	many := &Job{op: Op{Kind: Copy, From: here.fs, Names: []string{"a", "b"}, To: there.fs}}
	if got := many.Name(); !strings.Contains(got, "2 items") {
		t.Errorf("two files read as %q", got)
	}
	gone := &Job{op: Op{Kind: Delete, From: here.fs, Names: []string{"one.txt"}}}
	if got := gone.Name(); got != "one.txt" {
		t.Errorf("a delete reads as %q", got)
	}
}

// Dropping a job that is still running cancels it: nothing else would
// ever stop it.
func TestDroppingARunningJobCancelsIt(t *testing.T) {
	from, to := local(t), local(t)
	write(t, from.real, "big.txt", strings.Repeat("x", 4096))
	held := newSlow(from.fs)

	q := New(1)
	j := q.Start(t.Context(), Op{
		Kind: Copy, From: held, At: from.at, Names: []string{"big.txt"},
		To: to.fs, Into: to.at,
	}, Options{})
	select {
	case <-held.reading:
	case <-time.After(budget):
		t.Fatal("the job never started reading")
	}

	q.Drop(j)
	close(held.held)
	if err := ends(t, j); !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want it to say it was cancelled", err)
	}
	if got := len(q.Jobs()); got != 0 {
		t.Fatalf("the queue still holds %d jobs", got)
	}
	if _, err := os.Stat(filepath.Join(to.real, "big.txt")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("the half-written file is still there: %v", err)
	}
}

// noRead is a filesystem nothing can be read from, so a job that copies
// fails and one that renames does not.
type noRead struct{ vfs.FS }

func (noRead) Open(string) (io.ReadCloser, error) {
	return nil, errors.New("nothing can be read here")
}

// Moving on one filesystem renames rather than copying, which is what
// makes moving a large directory instant. A copy would have to read
// every byte, and this filesystem will not give any.
func TestMoveOnOneFilesystemDoesNotRead(t *testing.T) {
	from := local(t)
	write(t, from.real, "tree/one.txt", "one")
	into := filepath.Join(from.real, "elsewhere")
	if err := os.Mkdir(into, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	f := noRead{from.fs}
	q := New(1)
	j := q.Start(t.Context(), Op{
		Kind: Move, From: f, At: from.at, Names: []string{"tree"},
		To: f, Into: into,
	}, Options{})
	if err := ends(t, j); err != nil {
		t.Fatalf("the job: %v", err)
	}
	if got := read(t, into, "tree/one.txt"); got != "one" {
		t.Fatalf("what was moved holds %q", got)
	}
	gone(t, from.real, "tree")
}

// watched records whether a filesystem was asked to rename anything.
type watched struct {
	vfs.FS
	mu       sync.Mutex
	renames  int
	theOther vfs.FS
}

func (w *watched) Rename(from, to string) error {
	w.mu.Lock()
	w.renames++
	w.mu.Unlock()
	return w.FS.Rename(from, to)
}

func (w *watched) count() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.renames
}

// Moving between two machines copies and deletes. Renaming across them
// cannot work: the far machine's path means nothing here, and on a real
// pair of machines there is no one filesystem to rename within.
func TestMoveBetweenMachinesDoesNotRename(t *testing.T) {
	from, to := local(t), far(t)
	write(t, from.real, "one.txt", "the body")
	seen := &watched{FS: from.fs}

	q := New(1)
	j := q.Start(t.Context(), Op{
		Kind: Move, From: seen, At: from.at, Names: []string{"one.txt"},
		To: to.fs, Into: to.at,
	}, Options{})
	if err := ends(t, j); err != nil {
		t.Fatalf("the job: %v", err)
	}

	if got := seen.count(); got != 0 {
		t.Fatalf("it renamed %d times across two machines", got)
	}
	if got := read(t, to.real, "one.txt"); got != "the body" {
		t.Fatalf("what was moved holds %q", got)
	}
	gone(t, from.real, "one.txt")
}
