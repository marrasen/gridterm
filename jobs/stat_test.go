package jobs

import (
	"errors"
	"io"
	"io/fs"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/marrasen/gridterm/vfs"
)

// blind is a filesystem that will not say whether one name is there.
//
// It stands for the machine going quiet, or a directory the user may
// write in but not read: the name is neither there nor free, and the
// job has been told nothing.
type blind struct {
	vfs.FS
	at  string
	why error
}

func (b blind) Stat(path string) (vfs.Entry, error) {
	if path == b.at {
		return vfs.Entry{}, b.why
	}
	return b.FS.Stat(path)
}

// The user says to put the copy beside what is there, under a name the
// machine will not answer about. The job stops rather than writing at
// that name.
//
// A failed stat is not "the name is free". Reading it as one is how a
// file the user still wanted is written over by a job they asked to
// leave it alone.
func TestABesideNameThatCannotBeCheckedStopsTheJob(t *testing.T) {
	from, to := local(t), local(t)
	write(t, from.real, "one.txt", "the new one")
	write(t, to.real, "one.txt", "the old one")
	write(t, to.real, "one (copy).txt", "the one the user still wanted")

	boom := errors.New("the machine would not say")
	q := New(1)
	j := q.Start(t.Context(), Op{
		Kind: Copy, From: from.fs, At: from.at, Names: []string{"one.txt"},
		To:   blind{FS: to.fs, at: filepath.Join(to.at, "one (copy).txt"), why: boom},
		Into: to.at,
	}, Options{Ask: Always{What: Rename, Name: "one (copy).txt"}})

	err := ends(t, j)
	if !errors.Is(err, boom) {
		t.Fatalf("the job = %v, want the failure the machine gave", err)
	}
	if got := read(t, to.real, "one (copy).txt"); got != "the one the user still wanted" {
		t.Fatalf("the file at that name holds %q", got)
	}
	if got := read(t, to.real, "one.txt"); got != "the old one" {
		t.Fatalf("what was there holds %q", got)
	}
}

// A name that is not there is still free, so the ordinary answer works.
func TestABesideNameThatIsNotThereIsStillFree(t *testing.T) {
	from, to := local(t), local(t)
	write(t, from.real, "one.txt", "the new one")
	write(t, to.real, "one.txt", "the old one")

	q := New(1)
	j := q.Start(t.Context(), Op{
		Kind: Copy, From: from.fs, At: from.at, Names: []string{"one.txt"},
		To:   blind{FS: to.fs, at: "nowhere near it", why: fs.ErrPermission},
		Into: to.at,
	}, Options{Ask: Always{What: Rename, Name: "one (copy).txt"}})

	if err := ends(t, j); err != nil {
		t.Fatalf("the job: %v", err)
	}
	if got := read(t, to.real, "one (copy).txt"); got != "the new one" {
		t.Fatalf("what was copied holds %q", got)
	}
}

// stubborn is a filesystem that refuses to set the mode on one
// directory. It records every path a mode was set on, so a test can say
// which ones were tried.
type stubborn struct {
	vfs.FS
	at   string
	why  error
	done *[]string
}

func (s stubborn) Chmod(path string, mode fs.FileMode) error {
	*s.done = append(*s.done, path)
	if path == s.at {
		return s.why
	}
	return nil
}

// narrow reports every directory with a mode that has to be widened to
// write inside it and narrowed again once everything is in it.
type narrow struct{ vfs.FS }

func (n narrow) ReadDir(path string) ([]vfs.Entry, error) {
	out, err := n.FS.ReadDir(path)
	for i := range out {
		if out[i].IsDir() {
			out[i].Mode = fs.ModeDir | 0o550
		}
	}
	return out, err
}

func (n narrow) Stat(path string) (vfs.Entry, error) {
	e, err := n.FS.Stat(path)
	if e.IsDir() {
		e.Mode = fs.ModeDir | 0o550
	}
	return e, err
}

// A directory whose mode cannot be set does not leave the others wide
// open.
//
// The copy makes every directory writable so it can write inside it,
// and narrows them again at the end. Stopping at the first refusal left
// every directory after it in the widened mode, with nothing saying
// which.
func TestADirectoryModeThatCannotBeSetStillNarrowsTheRest(t *testing.T) {
	from, to := local(t), local(t)
	for _, name := range []string{"tree/a/one.txt", "tree/b/one.txt", "tree/c/one.txt"} {
		write(t, from.real, name, "body")
	}

	boom := errors.New("the machine refused the mode")
	var set []string
	stuck := vfs.Join(to.fs, to.at, "tree", "b")
	q := New(1)
	j := q.Start(t.Context(), Op{
		Kind: Copy, From: narrow{from.fs}, At: from.at, Names: []string{"tree"},
		To:   stubborn{FS: to.fs, at: stuck, why: boom, done: &set},
		Into: to.at,
	}, Options{})

	if err := ends(t, j); !errors.Is(err, boom) {
		t.Fatalf("the job = %v, want the refusal", err)
	}
	for _, name := range []string{"tree", "tree/a", "tree/b", "tree/c"} {
		want := vfs.Join(to.fs, to.at, filepath.FromSlash(name))
		if !slices.Contains(set, want) {
			t.Errorf("%s was left in the mode it was written with", name)
		}
	}
}

// A job that has stopped lets go of its context, which is registered on
// the window's for as long as it lives.
func TestAFinishedJobLetsGoOfItsContext(t *testing.T) {
	from, to := local(t), local(t)
	write(t, from.real, "one.txt", "body")

	q := New(1)
	j := q.Start(t.Context(), Op{
		Kind: Copy, From: from.fs, At: from.at, Names: []string{"one.txt"},
		To: to.fs, Into: to.at,
	}, Options{})

	if err := ends(t, j); err != nil {
		t.Fatalf("the job: %v", err)
	}
	select {
	case <-j.ctx.Done():
	default:
		t.Fatal("the job finished still holding its context")
	}
}

// Done means the whole job is over, the context with it. Cancelling
// after closing the channel left a gap: a waiter woken by Done could
// look at the context before the job had let go, which it did about
// once in a thousand.
func TestDoneMeansTheContextHasGoneAsWell(t *testing.T) {
	// On one processor the old order could never be caught: closing a
	// channel readies the waiter on the same processor without
	// preempting, so the gap between the close and the cancel was
	// never observable. This asks for more than one whatever the
	// machine defaults to, so the test means the same everywhere.
	if was := runtime.GOMAXPROCS(4); was != 4 {
		t.Cleanup(func() { runtime.GOMAXPROCS(was) })
	}
	from, to := local(t), local(t)
	write(t, from.real, "one.txt", "body")

	held := 0
	for range manyJobs {
		q := New(1)
		j := q.Start(t.Context(), Op{
			Kind: Copy, From: from.fs, At: from.at, Names: []string{"one.txt"},
			To: to.fs, Into: to.at,
		}, Options{})
		// How the copy ended does not matter here, and only the first
		// of them can succeed: every ending goes through the same
		// finish, which is where the ordering being pinned lives.
		select {
		case <-j.Done():
		case <-time.After(budget):
			t.Fatalf("a job never finished: %+v", j.Progress())
		}
		select {
		case <-j.ctx.Done():
		default:
			held++
		}
	}
	if held > 0 {
		t.Errorf("%d of %d finished jobs still held their context", held, manyJobs)
	}
}

// manyJobs is enough runs to catch a gap of a few instructions. At one
// in a thousand, a handful of runs would say nothing.
const manyJobs = 3000

// A job that never ran because it was refused lets go too.
func TestAJobThatWasNeverGoingToRunLetsGoOfItsContext(t *testing.T) {
	here := local(t)

	q := New(1)
	j := q.Start(t.Context(), Op{Kind: Copy, From: here.fs, At: here.at}, Options{})
	if err := ends(t, j); err == nil {
		t.Fatal("a job with nothing to copy was accepted")
	}
	select {
	case <-j.ctx.Done():
	default:
		t.Fatal("the job finished still holding its context")
	}
}

// machine is a filesystem on a machine of its own, whatever it is
// called.
type machine struct {
	vfs.FS
	name  string
	place any
}

func (m machine) Name() string { return m.name }
func (m machine) Place() any   { return m.place }

// Two machines the window happens to call the same thing are still two
// machines, so a move between them copies and deletes.
//
// A move within one filesystem is a rename, and a rename runs on the
// filesystem the file came from. Reading two machines as one because
// their names match would rename the file on the machine it started on
// and report it moved.
func TestAMoveBetweenTwoMachinesWithOneNameIsNotARename(t *testing.T) {
	from, to := local(t), local(t)
	write(t, from.real, "one.txt", "the body")

	renamed := false
	q := New(1)
	j := q.Start(t.Context(), Op{
		Kind: Move,
		From: watchRename{machine{FS: from.fs, name: "margit", place: new(int)}, &renamed},
		At:   from.at, Names: []string{"one.txt"},
		To: machine{FS: to.fs, name: "margit", place: new(int)}, Into: to.at,
	}, Options{})

	if err := ends(t, j); err != nil {
		t.Fatalf("the job: %v", err)
	}
	if renamed {
		t.Error("it renamed on the machine the file came from")
	}
	if got := read(t, to.real, "one.txt"); got != "the body" {
		t.Fatalf("what was moved holds %q", got)
	}
	gone(t, from.real, "one.txt")
}

// watchRename records whether a job renamed on this filesystem.
type watchRename struct {
	vfs.FS
	did *bool
}

func (w watchRename) Rename(from, to string) error {
	*w.did = true
	return w.FS.Rename(from, to)
}

// A copy that stops part way still narrows the directories it made.
//
// They are made wide enough to write inside and narrowed at the end, so
// a copy that returned early -- cancelled, or refused by the machine --
// left every directory it had made open to anyone who could reach it,
// with nothing saying which.
func TestACopyThatStopsPartWayStillNarrowsWhatItMade(t *testing.T) {
	from, to := local(t), local(t)
	for _, name := range []string{"tree/a/one.txt", "tree/b/one.txt", "tree/c/one.txt"} {
		write(t, from.real, name, "body")
	}

	boom := errors.New("the machine stopped answering")
	var set []string
	q := New(1)
	j := q.Start(t.Context(), Op{
		Kind: Copy, From: narrow{from.fs}, At: from.at, Names: []string{"tree"},
		To: refusing{
			FS:  stubborn{FS: to.fs, done: &set},
			at:  vfs.Join(to.fs, to.at, "tree", "b"),
			why: boom,
		},
		Into: to.at,
	}, Options{})

	if err := ends(t, j); !errors.Is(err, boom) {
		t.Fatalf("the job = %v, want the refusal", err)
	}
	// Whatever it had made by then is narrowed, rather than left open.
	if len(set) == 0 {
		t.Fatal("a copy that stopped part way narrowed nothing it had made")
	}
	for _, at := range set {
		if !strings.HasPrefix(at, to.at) {
			t.Errorf("it set the mode on %q, which is not under the destination", at)
		}
	}
}

// refusing is a filesystem that will not write inside one directory, for
// a copy that has to stop half way through.
type refusing struct {
	vfs.FS
	at  string
	why error
}

func (r refusing) Create(path string, mode fs.FileMode) (io.WriteCloser, error) {
	if strings.HasPrefix(path, r.at) {
		return nil, r.why
	}
	return r.FS.Create(path, mode)
}
