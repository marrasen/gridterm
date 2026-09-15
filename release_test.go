package main

import (
	"errors"
	"io"
	"io/fs"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/jobs"
	"github.com/marrasen/gridterm/meter"
	"github.com/marrasen/gridterm/ui"
	"github.com/marrasen/gridterm/ui/files"
	"github.com/marrasen/gridterm/vfs"
)

// heldFS is a filesystem that will not answer until the test lets it,
// and will not close when it is asked.
//
// It stands for a machine that has stopped answering: a job on it is
// stuck in a read, and the session behind it cannot be let go of.
type heldFS struct {
	held   chan struct{}
	closed chan struct{}
	err    error

	// reading is closed once something has actually blocked on it. A
	// test that wants a job stuck in a read has to wait for that: a job
	// cancelled before it got that far gives up at once, and then the
	// close it was holding up happens immediately.
	once    sync.Once
	reading chan struct{}
}

func newHeldFS(err error) *heldFS {
	return &heldFS{
		held: make(chan struct{}), closed: make(chan struct{}),
		reading: make(chan struct{}), err: err,
	}
}

// release lets the reads answer.
func (f *heldFS) release() { close(f.held) }

// wait blocks until the test lets go, saying first that it has.
func (f *heldFS) wait() {
	f.once.Do(func() { close(f.reading) })
	<-f.held
}

// stuck waits for a job to be inside a read on this filesystem.
func (f *heldFS) stuck(t *testing.T) {
	t.Helper()
	select {
	case <-f.reading:
	case <-time.After(waitBudget):
		t.Fatal("no job ever read from the filesystem")
	}
}

func (f *heldFS) Name() string { return "held" }
func (f *heldFS) Sep() byte    { return '/' }

func (f *heldFS) Home() (string, error) { return "/", nil }

func (f *heldFS) ReadDir(string) ([]vfs.Entry, error) {
	f.wait()
	return nil, errors.New("the machine has gone")
}

func (f *heldFS) Stat(string) (vfs.Entry, error) {
	f.wait()
	return vfs.Entry{}, errors.New("the machine has gone")
}

func (f *heldFS) Open(string) (io.ReadCloser, error) {
	f.wait()
	return nil, errors.New("the machine has gone")
}

func (f *heldFS) Create(string, fs.FileMode) (io.WriteCloser, error) {
	f.wait()
	return nil, errors.New("the machine has gone")
}

func (f *heldFS) Mkdir(string, fs.FileMode) error { return errors.New("the machine has gone") }
func (f *heldFS) Symlink(string, string) error    { return errors.New("the machine has gone") }
func (f *heldFS) Remove(string) error             { f.wait(); return errors.New("the machine has gone") }
func (f *heldFS) Rename(string, string) error     { return errors.New("the machine has gone") }
func (f *heldFS) Chmod(string, fs.FileMode) error { return errors.New("the machine has gone") }

func (f *heldFS) Close() error {
	select {
	case <-f.closed:
	default:
		close(f.closed)
	}
	return f.err
}

// A filesystem nothing is using is closed there and then, and a failure
// goes straight back to whoever asked.
func TestLettingGoOfAFilesystemNobodyIsUsing(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)

	boom := errors.New("the session would not close")
	f := newHeldFS(boom)
	if err := a.releaseFS(f); !errors.Is(err, boom) {
		t.Fatalf("releaseFS = %v, want the failure", err)
	}
	select {
	case <-f.closed:
	default:
		t.Fatal("the filesystem was not closed")
	}
}

// A filesystem a job is still reading through is closed once the job has
// stopped, and the failure is shown in the window.
//
// Neither can happen on the goroutine that draws: a cancelled job stops
// when whatever it is waiting on gives up, and the window may not wait
// with it. So the close happens elsewhere, and what it reports has to
// find its way back.
func TestLettingGoOfAFilesystemAJobIsUsing(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)

	boom := errors.New("the session would not close")
	f := newHeldFS(boom)
	count := meter.New()
	e := &conns.Entry{Host: conns.Local, Kind: conns.Files, Meter: count}
	j := a.queue.Start(a.ctx, jobs.Op{
		Kind: jobs.Delete, From: f, At: "/", Names: []string{"one"},
	}, jobs.Options{Count: count})
	a.jobs[e] = j
	a.registry.Add(e)
	// Stuck in a read, so cancelling it cannot finish it on the spot.
	f.stuck(t)

	// It cannot be closed yet, so it is not.
	if err := a.releaseFS(f); err != nil {
		t.Fatalf("releaseFS = %v, want it to wait", err)
	}
	select {
	case <-f.closed:
		t.Fatal("the filesystem was closed with a job still reading through it")
	default:
	}
	// And the job's row went with the pane that was using it.
	if a.jobs[e] != nil {
		t.Fatal("the job is still on the window's list")
	}

	// The machine answers, the job gives up, and the close happens.
	f.release()
	<-j.Done()
	errs := a.waitForCloses(waitBudget)
	if len(errs) != 1 || !errors.Is(errs[0], boom) {
		t.Fatalf("the close reported %v, want the failure", errs)
	}
}

// The window shows what a close reported, rather than leaving it in a
// log nobody opened the program with.
func TestAFailedCloseIsShownInTheWindow(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)

	boom := errors.New("the session would not close")
	a.closeFailed(boom)
	for _, err := range a.takeCloseErrs() {
		a.reportError("Could not let go of a filesystem", err)
	}
	n, ok := a.root.Modal().(*ui.Notice)
	if !ok {
		t.Fatalf("the failure showed %T, want a dialog", a.root.Modal())
	}
	if !strings.Contains(n.Message(), boom.Error()) {
		t.Fatalf("the dialog says %q", n.Message())
	}
	// And it is only shown once.
	if got := a.takeCloseErrs(); len(got) != 0 {
		t.Fatalf("%d failures are still waiting to be shown", len(got))
	}
}

// Waiting for the closes gives up rather than holding the window open
// for a machine that has stopped answering.
func TestWaitingForClosesGivesUp(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)

	f := newHeldFS(nil)
	count := meter.New()
	e := &conns.Entry{Host: conns.Local, Kind: conns.Files, Meter: count}
	j := a.queue.Start(a.ctx, jobs.Op{
		Kind: jobs.Delete, From: f, At: "/", Names: []string{"one"},
	}, jobs.Options{Count: count})
	a.jobs[e] = j
	f.stuck(t)

	if err := a.releaseFS(f); err != nil {
		t.Fatalf("releaseFS = %v", err)
	}
	start := time.Now()
	if got := a.waitForCloses(50 * time.Millisecond); len(got) != 0 {
		t.Fatalf("it reported %v while still waiting", got)
	}
	if waited := time.Since(start); waited > waitBudget {
		t.Fatalf("it waited %v for a machine that never answered", waited)
	}
	// Let the goroutine go, so the test leaves nothing running.
	f.release()
	<-j.Done()
	a.waitForCloses(waitBudget)
}

// paneOnFS puts a pane on a filesystem the test owns into the window's
// file manager, the way openFilesOn does for a machine.
func paneOnFS(t *testing.T, a *testApp, f vfs.FS) *files.Pane {
	t.Helper()
	if a.files == nil {
		if err := a.openFileManager(); err != nil {
			t.Fatalf("openFileManager: %v", err)
		}
	}
	b := a.files
	p := a.newPane(f, b)
	if !b.view.Add(p) {
		t.Fatal("the manager refused the pane")
	}
	row := a.browserRow(p, conns.Local)
	b.rows[p] = row
	a.registry.Add(row)
	a.relayout()
	return p
}

// A pane whose manager has already gone still has its filesystem let go
// of, and says that it had no manager.
//
// Nothing reaches this today. A filesystem quietly abandoned is a
// session nobody will ever close and nobody will ever hear about, which
// is the one outcome worth ruling out by hand.
func TestAPaneWithNoManagerStillLetsGoOfItsFilesystem(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)

	f := newHeldFS(nil)
	p := paneOnFS(t, a, f)
	a.files = nil

	err := a.filesPaneGone(p)
	if err == nil {
		t.Fatal("it said nothing about the pane having no manager")
	}
	if !strings.Contains(err.Error(), "no file manager") {
		t.Fatalf("it said %q", err)
	}
	select {
	case <-f.closed:
	default:
		t.Fatal("the filesystem was abandoned rather than closed")
	}
}

// Closing a manager whose panes all refuse to let go says so about every
// one of them, not just the first.
func TestEveryPaneThatWillNotCloseIsReported(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)

	one, two := errors.New("the first session held on"), errors.New("the second held on")
	paneOnFS(t, a, newHeldFS(one))
	paneOnFS(t, a, newHeldFS(two))

	err := a.closePane(a.files.view)
	if err == nil {
		t.Fatal("closing the manager reported nothing")
	}
	if !errors.Is(err, one) || !errors.Is(err, two) {
		t.Fatalf("it reported %v, want both failures", err)
	}
}

// A job that finishes reads again only the panes it touched.
//
// The window holds as many panes as the user cares to open, and each one
// may be a machine at the far end of a connection: rereading a directory
// the job never went near is a round trip for nothing.
func TestAFinishedJobReadsOnlyThePanesItTouched(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)

	here, there := vfs.NewLocal(), newHeldFS(nil)
	mine := paneOnFS(t, a, here)
	other := paneOnFS(t, a, there)

	reads := map[*files.Pane]int{}
	for _, p := range []*files.Pane{mine, other} {
		at := p
		at.Read = func(vfs.FS, string, func([]vfs.Entry, error)) { reads[at]++ }
		at.Open("/")
	}
	reads[mine], reads[other] = 0, 0

	a.reloadPanesOn(here, nil)
	if reads[mine] != 1 {
		t.Fatalf("the pane the job touched was read %d times, want once", reads[mine])
	}
	if reads[other] != 0 {
		t.Fatalf("a pane the job never went near was read %d times", reads[other])
	}
}

// The frame that notices a job has finished reads the panes it touched,
// and only those.
func TestTheFinishedJobSweepReadsThePanesItTouched(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)

	dir := t.TempDir()
	putFile(t, dir, "one.txt", "x")
	// One on this machine and one somewhere else. Two panes on this
	// machine are two values standing for one place, and a job that
	// changed something there changed it for both.
	mine, elsewhere := vfs.NewLocal(), newHeldFS(nil)
	touched, untouched := paneOnFS(t, a, mine), paneOnFS(t, a, elsewhere)

	reads := map[*files.Pane]int{}
	for _, p := range []*files.Pane{touched, untouched} {
		at := p
		at.Read = func(vfs.FS, string, func([]vfs.Entry, error)) { reads[at]++ }
		at.Open(dir)
	}

	count := meter.New()
	e := &conns.Entry{Host: conns.Local, Kind: conns.Files, Meter: count}
	j := a.queue.Start(a.ctx, jobs.Op{
		Kind: jobs.Delete, From: mine, At: dir, Names: []string{"one.txt"},
	}, jobs.Options{Count: count})
	a.jobs[e] = j
	a.registry.Add(e)
	<-j.Done()
	if p := j.Progress(); p.Err != nil {
		t.Fatalf("the job failed: %v", p.Err)
	}

	reads[touched], reads[untouched] = 0, 0
	a.refreshJobs()
	if reads[touched] != 1 {
		t.Fatalf("the pane the job worked on was read %d times, want once", reads[touched])
	}
	if reads[untouched] != 0 {
		t.Fatalf("a pane the job never went near was read %d times", reads[untouched])
	}
}

// Two panes on this machine are two values standing for one place, so a
// job that changed something there reads both of them again.
func TestBothPanesOnThisMachineAreReadAgain(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)

	one, two := paneOnFS(t, a, vfs.NewLocal()), paneOnFS(t, a, vfs.NewLocal())
	reads := map[*files.Pane]int{}
	for _, p := range []*files.Pane{one, two} {
		at := p
		at.Read = func(vfs.FS, string, func([]vfs.Entry, error)) { reads[at]++ }
		at.Open(t.TempDir())
	}
	reads[one], reads[two] = 0, 0

	a.reloadPanesOn(vfs.NewLocal(), nil)
	if reads[one] != 1 || reads[two] != 1 {
		t.Fatalf("the panes were read %d and %d times, want once each", reads[one], reads[two])
	}
}
