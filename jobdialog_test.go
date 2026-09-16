package main

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/input"
	"github.com/marrasen/gridterm/internal/sshtest"
	"github.com/marrasen/gridterm/jobs"
	"github.com/marrasen/gridterm/ui"
	"github.com/marrasen/gridterm/ui/files"
	"github.com/marrasen/gridterm/vfs"
)

// pausedFS is this machine, with every file it is asked to write held
// until the test lets go.
//
// It stands for a copy that is still going: a job on it has worked out
// what it is dealing with and is part way through a file, which is the
// state the progress dialog exists to show.
type pausedFS struct {
	vfs.FS

	held chan struct{}

	// writing is closed once something has actually blocked, so a test
	// can wait for the job to be under way rather than guess.
	once    sync.Once
	writing chan struct{}

	release sync.Once

	// removeErr is what Remove hands back instead of doing it, for a
	// machine that will not let go of what a cancelled copy half wrote.
	removeErr error
}

func newPausedFS() *pausedFS {
	return &pausedFS{
		FS:      vfs.NewLocal(),
		held:    make(chan struct{}),
		writing: make(chan struct{}),
	}
}

// Remove takes something away, or refuses when the test says so.
func (f *pausedFS) Remove(path string) error {
	if f.removeErr != nil {
		return f.removeErr
	}
	return f.FS.Remove(path)
}

// Name is what the panel calls it. Not a machine the window knows, so
// its work goes under this machine, which is where it really happens.
func (f *pausedFS) Name() string { return "paused" }

// Create waits for the test before it makes the file.
func (f *pausedFS) Create(path string, mode fs.FileMode) (io.WriteCloser, error) {
	f.once.Do(func() { close(f.writing) })
	<-f.held
	return f.FS.Create(path, mode)
}

// let lets the writes through. It can be called more than once, so a
// test can free a job on its way out without knowing whether it already
// did.
func (f *pausedFS) let() { f.release.Do(func() { close(f.held) }) }

// started waits for a job to be inside a write on this filesystem.
func (f *pausedFS) started(t *testing.T) {
	t.Helper()
	select {
	case <-f.writing:
	case <-time.After(waitBudget):
		t.Fatal("no job ever wrote to the filesystem")
	}
}

// browserOnto puts two panes in the window's file manager, this machine
// on the left and whatever the test brought on the right, each on a
// directory of its own.
func browserOnto(t *testing.T, a *testApp, to vfs.FS) (*files.Browser, string, string) {
	t.Helper()
	left, right := paneOnFS(t, a, vfs.NewLocal()), paneOnFS(t, a, to)
	from, into := t.TempDir(), t.TempDir()
	// The keys go on the first pane: a copy goes from the pane with the
	// keys to the next one.
	a.focus(left)
	left.Open(from)
	right.Open(into)
	waitFor(t, a, "both panes to be read", func() bool {
		return left.At() == from && right.At() == into && !left.Busy() && !right.Busy()
	})
	return a.files.view, from, into
}

// copyTheFirstFile copies what the left pane is showing into the right
// one, the way a user does: down onto it, F5 to pick it out, Tab to the
// other pane and F7 to paste.
func copyTheFirstFile(t *testing.T, a *testApp, b *files.Browser) {
	t.Helper()
	tap(t, b, input.KeyDown)
	tap(t, b, input.KeyF5)
	tap(t, b, input.KeyTab)
	tap(t, b, input.KeyF7)
	if len(a.jobs) != 1 {
		t.Fatalf("the window holds %d jobs, want the copy", len(a.jobs))
	}
}

// openTheRow chooses a sidebar row and presses Enter on it, which is
// what a click on the row does.
func openTheRow(t *testing.T, a *testApp, e *conns.Entry) {
	t.Helper()
	chooseRow(t, a, e)
	if _, err := a.panel.HandleKey(press(input.KeyEnter, 0)); err != nil {
		t.Fatalf("Enter on the row: %v", err)
	}
}

// theJobRow is the sidebar row of the one job the window is holding.
func theJobRow(t *testing.T, a *testApp) *conns.Entry {
	t.Helper()
	var found *conns.Entry
	for e := range a.jobs {
		if found != nil {
			t.Fatal("the window holds more than one job")
		}
		found = e
	}
	if found == nil {
		t.Fatal("the window holds no job")
	}
	return found
}

// dialogText is what the dialog says under its title, as one string.
func dialogText(d *jobDialog) string { return strings.Join(d.Lines, "\n") }

// A job's row opens a dialog saying what the copy is on now, how many
// files it has done and how many bytes have gone.
func TestAJobsRowShowsHowFarItHasGot(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)

	held := newPausedFS()
	t.Cleanup(held.let)
	b, from, _ := browserOnto(t, a, held)
	putFile(t, from, "one.txt", "the body")
	left := b.Panes()[0]
	left.Reload()
	waitFor(t, a, "the listing", func() bool { return !left.Busy() })

	copyTheFirstFile(t, a, b)
	held.started(t)

	openTheRow(t, a, theJobRow(t, a))
	d := awaitModal[*jobDialog](t, a, "the job's dialog", nil)
	a.refreshJobs()

	if !strings.Contains(d.Title, "one.txt") {
		t.Errorf("the dialog is titled %q, want the job it is about", d.Title)
	}
	said := dialogText(d)
	for _, want := range []string{"Copying one.txt", "0 of 1 file, 0 B of 8 B"} {
		if !strings.Contains(said, want) {
			t.Errorf("the dialog says\n%s\nwant a line with %q", said, want)
		}
	}
	// And it offers to stop the copy while it is running.
	if !offersButton(d, "Cancel") {
		t.Errorf("the dialog offers %v while the copy runs", buttonTitles(d))
	}
	// The row opens the one dialog it already has rather than a twin.
	openTheRow(t, a, theJobRow(t, a))
	if got := a.root.Modal(); got != ui.Widget(d) {
		t.Fatalf("choosing the row again put %T on top, want the dialog it opened", got)
	}
	if n := len(a.modals); n != 1 {
		t.Fatalf("the window holds %d dialogs for one job", n)
	}
}

// The box keeps its width as the counts in it grow, so a copy does not
// shuffle sideways while the user is reading it.
func TestAJobsDialogDoesNotMoveAsItCounts(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)

	held := newPausedFS()
	t.Cleanup(held.let)
	b, from, _ := browserOnto(t, a, held)
	putFile(t, from, "one.txt", "the body")
	left := b.Panes()[0]
	left.Reload()
	waitFor(t, a, "the listing", func() bool { return !left.Busy() })

	copyTheFirstFile(t, a, b)
	held.started(t)
	openTheRow(t, a, theJobRow(t, a))
	d := awaitModal[*jobDialog](t, a, "the job's dialog", nil)
	d.Layout(ui.Size{Cols: 80, Rows: 24})

	// Two readings of the same copy, one further along than the other.
	// The numbers are the test's own: what a job does with a real file is
	// not what this is about.
	started := time.Now()
	early := jobs.Progress{
		Files: 40, Bytes: 9_000_000, BytesDone: 8,
		Current: "one.txt", Started: started,
	}
	later := jobs.Progress{
		Files: 40, FilesDone: 12, Bytes: 9_000_000, BytesDone: 3_200_000,
		Current: "one.txt", Started: started,
	}

	d.SetLines(d.report(early, started))
	was := d.Box()
	d.SetLines(d.report(later, started))
	if now := d.Box(); now != was {
		t.Fatalf("the box moved from %+v to %+v as the numbers grew:\n%s",
			was, now, dialogText(d))
	}
	if !strings.Contains(dialogText(d), "12 of 40 files") {
		t.Fatalf("the dialog says\n%s\nwant the larger numbers", dialogText(d))
	}
}

// A copy the user cancelled that could not take away what it half wrote
// says both, on the row and in the dialog, and the disk failure is shown
// rather than left behind the user's own decision.
func TestACancelThatCouldNotClearUpSaysSo(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)

	held := newPausedFS()
	held.removeErr = errors.New("the disk is read-only")
	t.Cleanup(held.let)
	b, from, _ := browserOnto(t, a, held)
	putFile(t, from, "one.txt", "the body")
	left := b.Panes()[0]
	left.Reload()
	waitFor(t, a, "the listing", func() bool { return !left.Busy() })

	copyTheFirstFile(t, a, b)
	held.started(t)
	e := theJobRow(t, a)
	j := a.jobs[e]
	openTheRow(t, a, e)
	d := awaitModal[*jobDialog](t, a, "the job's dialog", nil)

	pressButton(t, a, d.Form, "Cancel")
	held.let()
	waitFor(t, a, "the job to stop", func() bool {
		a.refreshJobs()
		return j.Progress().Done
	})

	if !errors.Is(j.Progress().Err, context.Canceled) {
		t.Fatalf("the job ended with %v, want it cancelled", j.Progress().Err)
	}
	// The row says both.
	if e.Note != "cancelled, trouble" {
		t.Errorf("the row says %q, want the cancel and the trouble", e.Note)
	}
	// So does the account of it.
	said := outcomeOf(j.Progress())
	if !strings.Contains(said, "could not be taken away") ||
		!strings.Contains(said, "read-only") {
		t.Errorf("the outcome reads %q", said)
	}
	// And the window shows the disk failure rather than swallowing it.
	n := awaitModal(t, a, "a dialog saying the copy could not finish",
		byTitlePrefix[*ui.Notice]("Could not finish"))
	if !strings.Contains(n.Message(), "read-only") {
		t.Fatalf("it says %q, want what the disk said", n.Message())
	}
}

// A delete is not offered again: doing it twice would take something
// away without asking.
func TestAFinishedDeleteIsNotRepeated(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)

	b, from, _ := browserOnto(t, a, vfs.NewLocal())
	putFile(t, from, "one.txt", "the body")
	left := b.Panes()[0]
	left.Reload()
	waitFor(t, a, "the listing", func() bool { return !left.Busy() })

	// Down onto the file and F8, which asks first.
	tap(t, b, input.KeyDown)
	tap(t, b, input.KeyF8)
	ask := awaitModal(t, a, "the delete question", byTitlePrefix[*ui.Form]("Delete"))
	pressButton(t, a, ask, "Delete")

	var e *conns.Entry
	waitFor(t, a, "the delete to start", func() bool {
		if len(a.jobs) != 1 {
			return false
		}
		e = theJobRow(t, a)
		return true
	})
	waitFor(t, a, "the delete to finish", func() bool {
		a.refreshJobs()
		return len(a.jobs) == 0
	})

	openTheRow(t, a, e)
	d := awaitModal[*jobDialog](t, a, "the job's dialog", nil)
	a.refreshJobs()
	if offersButton(d, "Repeat") {
		t.Fatalf("a finished delete offers %v", buttonTitles(d))
	}
	if !strings.Contains(dialogText(d), "It finished.") {
		t.Fatalf("the dialog says\n%s", dialogText(d))
	}
}

// When the copy has finished the dialog says how it ended, and offers to
// do it again.
func TestAFinishedJobIsRepeatedFromItsDialog(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)

	held := newPausedFS()
	t.Cleanup(held.let)
	b, from, into := browserOnto(t, a, held)
	putFile(t, from, "one.txt", "the body")
	left := b.Panes()[0]
	left.Reload()
	waitFor(t, a, "the listing", func() bool { return !left.Busy() })

	copyTheFirstFile(t, a, b)
	held.started(t)
	openTheRow(t, a, theJobRow(t, a))
	d := awaitModal[*jobDialog](t, a, "the job's dialog", nil)

	// The finger was on Cancel when it finished.
	sendKey(t, a, press(input.KeyTab, 0))
	if at, isButton := d.Focused(); !isButton || at != 0 {
		t.Fatalf("the focus is on %d (button %v), want Cancel", at, isButton)
	}
	held.let()
	waitFor(t, a, "the copy to finish", func() bool {
		a.refreshJobs()
		return strings.Contains(dialogText(d), "It finished.")
	})
	if said := dialogText(d); !strings.Contains(said, "1 file") {
		t.Errorf("the finished dialog says\n%s\nwant what it did", said)
	}
	// And the focus moved off where Cancel was, so Enter cannot start a
	// repeat the user never asked for.
	if at, isButton := d.Focused(); !isButton || at != len(d.Buttons())-1 {
		t.Fatalf("the focus is on %d (button %v), want Close", at, isButton)
	}

	// The file is copied again from the dialog, with nothing to find at
	// either end by hand.
	copied := filepath.Join(into, "one.txt")
	if err := os.Remove(copied); err != nil {
		t.Fatalf("clearing the copy: %v", err)
	}
	if !offersButton(d, "Repeat") {
		t.Fatalf("the finished dialog offers %v", buttonTitles(d))
	}
	pressButton(t, a, d.Form, "Repeat")

	waitFor(t, a, "the copy to be done again", func() bool {
		a.refreshJobs()
		_, err := os.Stat(copied)
		return err == nil
	})
	// A new row for the new job, and the old one still there.
	rows := 0
	for _, group := range a.registry.Groups(time.Now()) {
		for _, row := range group.Rows {
			if strings.Contains(row.Label, "one.txt") {
				rows++
			}
		}
	}
	if rows != 2 {
		t.Fatalf("the sidebar shows %d rows for the copy, want the first and the repeat", rows)
	}
}

// Cancel stops the work and leaves the row saying so, rather than taking
// the row away with it.
func TestCancellingAJobFromItsDialog(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)

	held := newPausedFS()
	t.Cleanup(held.let)
	b, from, _ := browserOnto(t, a, held)
	putFile(t, from, "one.txt", "the body")
	left := b.Panes()[0]
	left.Reload()
	waitFor(t, a, "the listing", func() bool { return !left.Busy() })

	copyTheFirstFile(t, a, b)
	held.started(t)
	e := theJobRow(t, a)
	j := a.jobs[e]
	openTheRow(t, a, e)
	d := awaitModal[*jobDialog](t, a, "the job's dialog", nil)

	pressButton(t, a, d.Form, "Cancel")
	// The write it was held in answers, and the job gives up on the next
	// thing it was going to do.
	held.let()
	waitFor(t, a, "the job to stop", func() bool {
		a.refreshJobs()
		return j.Progress().Done
	})
	if err := j.Progress().Err; !errors.Is(err, context.Canceled) {
		t.Fatalf("the job ended with %v, want it cancelled", err)
	}
	// Stopped, not dropped: the queue still knows the job, so the window
	// waits for it on the way out rather than closing while it is still
	// writing.
	if !slices.Contains(a.queue.Jobs(), j) {
		t.Error("the queue forgot the job that was cancelled")
	}
	// The row stays, saying how it ended.
	if a.jobs[e] != nil {
		t.Error("the finished job is still on the window's list")
	}
	if e.Note != "cancelled" {
		t.Errorf("the row says %q, want it cancelled", e.Note)
	}
	if rowSaying(t, a, e.Label) != e {
		t.Error("the row went with the job it was cancelled from")
	}
}

// A repeat looks the machines up again by name, so one that has gone is
// said plainly rather than copied to a filesystem nobody holds.
func TestRepeatingAJobWhoseMachineHasGone(t *testing.T) {
	s := sshtest.New(t)
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	host := connectedTo(t, a, s)

	// The copy comes off the machine, so it is the machine that has to
	// be there for the repeat.
	there := openFilesFromThePlus(t, a, host)
	here := openFilesFromThePlus(t, a, conns.Local)
	from, into := t.TempDir(), t.TempDir()
	putFile(t, from, "one.txt", "the body")
	a.focus(there)
	openAt(t, a, there, filepath.ToSlash(from))
	openAt(t, a, here, into)

	copyTheFirstFile(t, a, a.files.view)
	e := theJobRow(t, a)
	waitFor(t, a, "the copy to finish", func() bool {
		a.refreshJobs()
		return len(a.jobs) == 0
	})
	openTheRow(t, a, e)
	d := awaitModal[*jobDialog](t, a, "the job's dialog", nil)

	if err := a.dropMachine(host); err != nil {
		t.Fatalf("dropMachine: %v", err)
	}
	pressButton(t, a, d.Form, "Repeat")

	n := awaitModal(t, a, "a dialog saying it could not be done again",
		byTitle[*ui.Notice]("Could not copy it again"))
	if !strings.Contains(n.Message(), host) {
		t.Fatalf("it says %q, want which machine is gone", n.Message())
	}
}

// offersButton reports whether the dialog has a button with this title.
func offersButton(d *jobDialog, title string) bool {
	for _, b := range d.Buttons() {
		if b.Title == title {
			return true
		}
	}
	return false
}

// buttonTitles is what the dialog offers, for a failure worth reading.
func buttonTitles(d *jobDialog) []string {
	var out []string
	for _, b := range d.Buttons() {
		out = append(out, b.Title)
	}
	return out
}

// A dialog left open on a finished job says the same thing on every
// frame, so it dirties nothing.
func TestAFinishedJobsDialogSettles(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)

	b, from, _ := browserOnto(t, a, vfs.NewLocal())
	putFile(t, from, "one.txt", "the body")
	left := b.Panes()[0]
	left.Reload()
	waitFor(t, a, "the listing", func() bool { return !left.Busy() })

	copyTheFirstFile(t, a, b)
	e := theJobRow(t, a)
	j := a.jobs[e]
	waitFor(t, a, "the copy to finish", func() bool {
		a.refreshJobs()
		return j.Progress().Done
	})

	openTheRow(t, a, e)
	d := awaitModal[*jobDialog](t, a, "the job's dialog", nil)
	ended := j.Progress().Ended
	a.refreshJobsAt(ended.Add(time.Second))
	was := dialogText(d)
	if !strings.Contains(was, "It finished.") {
		t.Fatalf("the dialog says %q", was)
	}
	// A copy of eight bytes took no measurable time, and the words for
	// that are the connection log's own.
	if !strings.Contains(was, "in under a second") {
		t.Fatalf("the dialog says %q about how long it took", was)
	}

	// The frame the window really draws, onto the layer the dialog has.
	m := a.modals[len(a.modals)-1]
	a.root.DrawModal(m.w, m.g.View())
	m.g.ClearDirty()
	laidOut := d.laidOut

	// A minute later. How long it took is settled, so nothing on the
	// dialog has anything new to say.
	a.refreshJobsAt(ended.Add(time.Minute))
	a.root.DrawModal(m.w, m.g.View())
	if m.g.AnyDirty() {
		t.Fatalf("a frame with nothing new to say dirtied the dialog's layer:\n%s", dialogText(d))
	}
	if d.laidOut != laidOut {
		t.Fatalf("the lines were laid out %d more times with nothing to say", d.laidOut-laidOut)
	}
	if now := dialogText(d); now != was {
		t.Fatalf("the dialog says\n%s\nafter a minute, want\n%s", now, was)
	}
	// It is still drawn, rather than not drawn at all.
	if !strings.Contains(gridRows(m.g), "It finished.") {
		t.Fatalf("the dialog is not on its layer:\n%s", gridRows(m.g))
	}
}

// gridRows reads a grid back as one string.
func gridRows(g *grid.Grid) string {
	cols, rows := g.Size()
	var b strings.Builder
	for y := 0; y < rows; y++ {
		for x := 0; x < cols; x++ {
			r := g.At(x, y).Rune
			if r == 0 {
				r = ' '
			}
			b.WriteRune(r)
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// countingFS is a filesystem that records being closed, for the jobs
// that open one of their own.
type countingFS struct {
	vfs.FS
	closed chan struct{}
	once   sync.Once
}

func newCountingFS() *countingFS {
	return &countingFS{FS: vfs.NewLocal(), closed: make(chan struct{})}
}

func (f *countingFS) Name() string { return "counted" }

func (f *countingFS) Close() error {
	f.once.Do(func() { close(f.closed) })
	return nil
}

// A filesystem a job opened for itself is let go of once the job has
// stopped: nothing else holds it, so nothing else would ever close it.
func TestAJobLetsGoOfTheFilesystemsItOpened(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)

	dir := t.TempDir()
	putFile(t, dir, "one.txt", "the body")
	mine := newCountingFS()
	a.runJob(jobs.Op{
		Kind: jobs.Delete, From: mine, At: dir, Names: []string{"one.txt"},
	}, jobEnd{host: conns.Local}, jobEnd{host: conns.Local}, []vfs.FS{mine})

	j := a.jobs[theJobRow(t, a)]
	<-j.Done()
	if errs := a.closes.waitFor(waitBudget); len(errs) != 0 {
		t.Fatalf("letting go reported %v", errs)
	}
	select {
	case <-mine.closed:
	default:
		t.Fatal("the filesystem the job opened was abandoned rather than closed")
	}
}

// A repeat that cannot reach the far end starts nothing: no second row,
// no second job, and the source it had already opened is let go of.
func TestARepeatThatCannotReachTheFarEndStartsNothing(t *testing.T) {
	s := sshtest.New(t)
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	host := connectedTo(t, a, s)

	here := openFilesFromThePlus(t, a, conns.Local)
	there := openFilesFromThePlus(t, a, host)
	from, into := t.TempDir(), filepath.ToSlash(t.TempDir())
	putFile(t, from, "one.txt", "the body")
	a.focus(here)
	openAt(t, a, here, from)
	openAt(t, a, there, into)

	copyTheFirstFile(t, a, a.files.view)
	e := theJobRow(t, a)
	waitFor(t, a, "the copy to finish", func() bool {
		a.refreshJobs()
		return len(a.jobs) == 0
	})
	openTheRow(t, a, e)
	d := awaitModal[*jobDialog](t, a, "the job's dialog", nil)

	// The machine the copy went to is gone, and this machine is not.
	if err := a.dropMachine(host); err != nil {
		t.Fatalf("dropMachine: %v", err)
	}
	rows := len(a.registry.Groups(time.Now()))
	pressButton(t, a, d.Form, "Repeat")

	n := awaitModal(t, a, "a dialog saying it could not be done again",
		byTitle[*ui.Notice]("Could not copy it again"))
	if !strings.Contains(n.Message(), host) {
		t.Fatalf("it says %q, want which machine is gone", n.Message())
	}
	if len(a.jobs) != 0 {
		t.Fatalf("%d jobs were started by a repeat that could not reach the far end", len(a.jobs))
	}
	if got := len(a.registry.Groups(time.Now())); got != rows {
		t.Fatalf("the sidebar grew from %d groups to %d", rows, got)
	}
}

// The dialog reads the panel's rates and never adds to them: the panel
// owns that map and throws away the rate of a row that has gone, so one
// planted here would come back every frame for ever.
func TestAJobsDialogDoesNotPlantARate(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)

	held := newPausedFS()
	t.Cleanup(held.let)
	b, from, _ := browserOnto(t, a, held)
	putFile(t, from, "one.txt", "the body")
	left := b.Panes()[0]
	left.Reload()
	waitFor(t, a, "the listing", func() bool { return !left.Busy() })

	copyTheFirstFile(t, a, b)
	held.started(t)
	e := theJobRow(t, a)
	openTheRow(t, a, e)
	awaitModal[*jobDialog](t, a, "the job's dialog", nil)

	// The row is closed from the sidebar, and the panel throws its rate
	// away with it. The dialog is still open on the job.
	if err := e.Close(); err != nil {
		t.Fatalf("closing the row: %v", err)
	}
	a.refreshPanel(time.Now())
	if _, ok := a.rates[e]; ok {
		t.Fatal("the panel kept a rate for a row that has gone")
	}

	a.refreshJobsAt(time.Now())
	if _, ok := a.rates[e]; ok {
		t.Fatal("the dialog put a rate back for a row the panel had thrown away")
	}
}

// pressButtonOver presses a dialog button whose work waits on another
// window's answer.
//
// The work is posted rather than done on the spot, so this window's pump
// is run on a goroutine of its own and the other window is pumped here:
// it answers from the goroutine that draws, which is the test one.
// Nothing here touches the window the dialog is on while that runs.
func pressButtonOver(t *testing.T, a, other *testApp, f *ui.Form, title string) {
	t.Helper()
	at := -1
	for i, b := range f.Buttons() {
		if b.Title == title {
			at = i
			break
		}
	}
	if at < 0 {
		t.Fatalf("the dialog has no %q button", title)
	}
	for i := 0; i < len(f.Fields())+len(f.Buttons())+1; i++ {
		if got, isButton := f.Focused(); isButton && got == at {
			sendKey(t, a, press(input.KeyEnter, 0))
			done := make(chan struct{})
			go func() {
				defer close(done)
				a.pump.run()
			}()
			waitFor(t, other, "the window over there to answer "+title, func() bool {
				select {
				case <-done:
					return true
				default:
					return false
				}
			})
			return
		}
		sendKey(t, a, press(input.KeyTab, 0))
	}
	t.Fatalf("the focus never reached the %q button", title)
}

// Repeat on a copy from a machine of a window taken over reads that
// machine again.
//
// The pane is filed under the window, so the job's row says the window,
// and a repeat that opened the window by that name opened the window's
// own disk instead: it read the wrong machine, and the other way round
// it would have written to it. In a test the two are one computer, so
// what tells them apart is that margit is asked for a file session.
func TestRepeatingACopyFromAMachineOverThere(t *testing.T) {
	host, client, addr, margit := aWindowConnectedToMargitOn(t)

	from, into := t.TempDir(), t.TempDir()
	putFile(t, from, "one.txt", "the body")

	there := openFilesFromTheFarPlus(t, client, host, addr, "margit")
	here := openFilesFromThePlus(t, client, conns.Local)
	openAt(t, client, there, overThere(from))
	openAt(t, client, here, into)
	client.focus(there)

	copyTheFirstFile(t, client, client.files.view)
	e := theJobRow(t, client)
	waitFor(t, client, "the copy to finish", func() bool {
		client.refreshJobs()
		return len(client.jobs) == 0
	})
	copied := filepath.Join(into, "one.txt")
	if _, err := os.Stat(copied); err != nil {
		t.Fatalf("the copy did not arrive: %v", err)
	}
	if err := os.Remove(copied); err != nil {
		t.Fatalf("clearing the copy: %v", err)
	}

	sessions := margit.SFTPs()
	openTheRow(t, client, e)
	d := awaitModal[*jobDialog](t, client, "the job's dialog", nil)
	pressButtonOver(t, client, host, d.Form, "Repeat")

	waitFor(t, client, "the copy to be done again", func() bool {
		client.refreshJobs()
		_, err := os.Stat(copied)
		return err == nil
	})
	// Read from margit, which is the only thing that tells the machine
	// apart from the window it is reached through.
	if got := margit.SFTPs(); got <= sessions {
		t.Errorf("margit served %d file sessions, want one more than the %d it had before the repeat",
			got, sessions)
	}
	// And the end the repeat opened is the machine over there, not the
	// window's own disk.
	if got := d.from.far; got.window != windowAt(t, client, addr) || got.host != "margit" {
		t.Errorf("the job's source end is %v, want margit on the window taken over", got)
	}
}
