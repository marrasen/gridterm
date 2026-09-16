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
	"github.com/marrasen/gridterm/input"
	"github.com/marrasen/gridterm/internal/sshtest"
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
}

func newPausedFS() *pausedFS {
	return &pausedFS{
		FS:      vfs.NewLocal(),
		held:    make(chan struct{}),
		writing: make(chan struct{}),
	}
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
	for _, want := range []string{"Copying one.txt", "0 of 1 file", "8 B"} {
		if !strings.Contains(said, want) {
			t.Errorf("the dialog says\n%s\nwant a line with %q", said, want)
		}
	}
	// And it offers to stop the copy while it is running.
	if !offersButton(d, "Cancel") {
		t.Errorf("the dialog offers %v while the copy runs", buttonTitles(d))
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

	held.let()
	waitFor(t, a, "the copy to finish", func() bool {
		a.refreshJobs()
		return strings.Contains(dialogText(d), "It finished.")
	})
	if said := dialogText(d); !strings.Contains(said, "1 file") {
		t.Errorf("the finished dialog says\n%s\nwant what it did", said)
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
	a.refreshJobs()
	was := dialogText(d)
	if !strings.Contains(was, "It finished.") {
		t.Fatalf("the dialog says %q", was)
	}
	// Two frames a second apart. How long it took is settled, so the
	// lines are the same however long the dialog is left open.
	d.refresh(j.Progress().Ended.Add(time.Second))
	d.refresh(j.Progress().Ended.Add(time.Minute))
	if now := dialogText(d); now != was {
		t.Fatalf("the dialog says\n%s\nafter a minute, want\n%s", now, was)
	}
}
