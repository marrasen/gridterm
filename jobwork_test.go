package main

import (
	"context"
	"errors"
	"image/color"
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
	"github.com/marrasen/gridterm/meter"
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
	d := theJobPane(t, a)
	a.refreshJobs()

	said := jobPaneText(d)
	for _, want := range []string{"Copy", "one.txt", "0 of 1 file", "0 B of 8 B"} {
		if !strings.Contains(said, want) {
			t.Errorf("the pane says\n%s\nwant %q somewhere in it", said, want)
		}
	}
	// And it offers to stop the copy while it is running.
	if !offersChoice(d, btnCancel) {
		t.Errorf("the pane offers %v while the copy runs", choiceTitles(d))
	}
	// Nothing modal: the window can go on being used while a copy runs.
	if got := a.root.Modal(); got != nil {
		t.Errorf("it put %T over the window", got)
	}
	// The row goes to the pane it already opened rather than a twin.
	openTheRow(t, a, theJobRow(t, a))
	if n := len(a.jobPanes); n != 1 {
		t.Fatalf("the window holds %d panes for one job", n)
	}
	if got := ui.FocusedLeaf(a.root.Widget()); got != ui.Widget(d) {
		t.Errorf("choosing the row again focused %T, want the pane it opened", got)
	}
}

// The pane keeps its shape as the counts in it grow, so a copy does not
// shuffle about while the user is reading it.
//
// A pane cannot re-centre itself the way a dialog box could, but the
// rows it draws on can still move: a number that grew a digit must not
// push the bar or the buttons anywhere.
func TestAJobsPaneKeepsItsShapeAsItCounts(t *testing.T) {
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
	d := theJobPane(t, a)

	// Two readings of the same copy, one further along than the other.
	// The numbers are the test's own: what a job does with a real file
	// is not what this is about.
	started := time.Now()
	early := jobs.Progress{
		Files: 40, Bytes: 9_000_000, BytesDone: 8,
		Current: "one.txt", Started: started,
	}
	later := jobs.Progress{
		Files: 40, FilesDone: 12, Bytes: 9_000_000, BytesDone: 3_200_000,
		Current: "one.txt", Started: started,
	}

	was := rowsWithAnythingOn(jobPaneDrawn(d, early, started))
	now := rowsWithAnythingOn(jobPaneDrawn(d, later, started))
	if !slices.Equal(was, now) {
		t.Errorf("the pane drew on rows %v and then %v as the numbers grew:\n%s",
			was, now, jobPaneDrawn(d, later, started))
	}
	if said := jobPaneDrawn(d, later, started); !strings.Contains(said, "12 of 40 files") {
		t.Errorf("the pane says\n%s\nwant the larger numbers", said)
	}
}

// rowsWithAnythingOn is which rows of a drawing have text on them.
func rowsWithAnythingOn(drawn string) []int {
	var out []int
	for i, line := range strings.Split(drawn, "\n") {
		if strings.TrimSpace(line) != "" {
			out = append(out, i)
		}
	}
	return out
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
	d := theJobPane(t, a)

	pressChoice(t, d, btnCancel)
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
	pressButton(t, a, ask, btnDelete)

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
	d := theJobPane(t, a)
	a.refreshJobs()
	if offersChoice(d, btnRepeat) {
		t.Fatalf("a finished delete offers %v", choiceTitles(d))
	}
	if !strings.Contains(jobPaneText(d), "It finished.") {
		t.Fatalf("the dialog says\n%s", jobPaneText(d))
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
	d := theJobPane(t, a)

	// The finger was on Cancel when it finished.
	jobPaneText(d)
	if got := focusedChoice(d); got != btnCancel {
		t.Fatalf("the focus is on %q, want Cancel", got)
	}
	held.let()
	waitFor(t, a, "the copy to finish", func() bool {
		a.refreshJobs()
		return strings.Contains(jobPaneText(d), "It finished.")
	})
	if said := jobPaneText(d); !strings.Contains(said, "1 file") {
		t.Errorf("the finished pane says\n%s\nwant what it did", said)
	}
	// And the focus moved off where Cancel was, so Enter cannot start a
	// repeat the user never asked for: Repeat is drawn where Cancel was.
	if got := focusedChoice(d); got != btnClose {
		t.Fatalf("the focus is on %q, want Close", got)
	}

	// The file is copied again from the dialog, with nothing to find at
	// either end by hand.
	copied := filepath.Join(into, "one.txt")
	if err := os.Remove(copied); err != nil {
		t.Fatalf("clearing the copy: %v", err)
	}
	if !offersChoice(d, btnRepeat) {
		t.Fatalf("the finished dialog offers %v", choiceTitles(d))
	}
	pressChoice(t, d, btnRepeat)

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
	d := theJobPane(t, a)

	pressChoice(t, d, btnCancel)
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
	d := theJobPane(t, a)

	if err := a.dropMachine(host); err != nil {
		t.Fatalf("dropMachine: %v", err)
	}
	pressChoice(t, d, btnRepeat)

	n := awaitModal(t, a, "a dialog saying it could not be done again",
		byTitle[*ui.Notice]("Could not copy it again"))
	if !strings.Contains(n.Message(), host) {
		t.Fatalf("it says %q, want which machine is gone", n.Message())
	}
}

// offersChoice reports whether the pane offers this along its bottom.
func offersChoice(p *jobPane, title string) bool {
	for _, c := range p.choices() {
		if c.title == title {
			return true
		}
	}
	return false
}

// choiceTitles is what the pane offers, for a failure worth reading.
func choiceTitles(p *jobPane) []string {
	var out []string
	for _, c := range p.choices() {
		out = append(out, c.title)
	}
	return out
}

// pressChoice presses one of the pane's buttons, or sets its box, by
// the name it draws.
func pressChoice(t *testing.T, p *jobPane, title string) {
	t.Helper()
	for i, c := range p.choices() {
		if c.title != title {
			continue
		}
		if err := p.press(i); err != nil {
			t.Fatalf("press %q: %v", title, err)
		}
		return
	}
	t.Fatalf("the pane offers %v, with no %q", choiceTitles(p), title)
}

// theJobPane is the pane the window has open on a piece of file work.
func theJobPane(t *testing.T, a *testApp) *jobPane {
	t.Helper()
	var found *jobPane
	waitFor(t, a, "the pane on the file work", func() bool {
		if len(a.jobPanes) == 0 {
			return false
		}
		found = a.jobPanes[len(a.jobPanes)-1]
		return true
	})
	return found
}

// jobPaneText is what the pane draws, one line per row.
func jobPaneText(p *jobPane) string {
	return jobPaneDrawn(p, p.job.Progress(), time.Now())
}

// jobPaneDrawn is what the pane draws for one reading of a job.
func jobPaneDrawn(p *jobPane, prog jobs.Progress, now time.Time) string {
	cols, rows := 80, 24
	g := grid.New(cols, rows, color.RGBA{}, color.RGBA{})
	p.Layout(ui.Size{Cols: cols, Rows: rows})
	p.draw(g.View(), prog, now)
	var b strings.Builder
	for y := range rows {
		line := ""
		for x := range cols {
			r := g.At(x, y).Rune
			if r == 0 {
				r = ' '
			}
			line += string(r)
		}
		b.WriteString(strings.TrimRight(line, " ") + "\n")
	}
	return b.String()
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
	d := theJobPane(t, a)
	ended := j.Progress().Ended
	a.refreshJobsAt(ended.Add(time.Second))
	was := jobPaneText(d)
	if !strings.Contains(was, "It finished.") {
		t.Fatalf("the dialog says %q", was)
	}
	// A copy of eight bytes took no measurable time, and the words for
	// that are the connection log's own.
	if !strings.Contains(was, "in under a second") {
		t.Fatalf("the dialog says %q about how long it took", was)
	}

	// The frame the window really draws, onto a grid of its own.
	g := grid.New(80, 24, color.RGBA{}, color.RGBA{})
	d.Layout(ui.Size{Cols: 80, Rows: 24})
	d.draw(g.View(), j.Progress(), ended.Add(time.Second))
	g.ClearDirty()

	// A minute later. How long it took is settled, so nothing on the
	// pane has anything new to say.
	a.refreshJobsAt(ended.Add(time.Minute))
	d.draw(g.View(), j.Progress(), ended.Add(time.Minute))
	if g.AnyDirty() {
		t.Fatalf("a frame with nothing new to say dirtied the pane:\n%s", jobPaneText(d))
	}
	if now := jobPaneText(d); now != was {
		t.Fatalf("the pane says\n%s\nafter a minute, want\n%s", now, was)
	}
	// It is still drawn, rather than not drawn at all.
	if !strings.Contains(gridRows(g), "It finished.") {
		t.Fatalf("the pane drew nothing:\n%s", gridRows(g))
	}
}

// gridRows reads a grid back as one string.
func gridRows(g *grid.Grid) string {
	cols, rows := g.Size()
	var b strings.Builder
	for y := range rows {
		for x := range cols {
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
	d := theJobPane(t, a)

	// The machine the copy went to is gone, and this machine is not.
	if err := a.dropMachine(host); err != nil {
		t.Fatalf("dropMachine: %v", err)
	}
	rows := len(a.registry.Groups(time.Now()))
	pressChoice(t, d, btnRepeat)

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
	theJobPane(t, a)

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
//
// other must never be the window the dialog is on. Pumping it here would
// run its work on this goroutine while the button's work is running on
// the other one, and two goroutines in one window is the race this is
// built to avoid.
func pressChoiceOver(t *testing.T, a, other *testApp, p *jobPane, title string) {
	t.Helper()
	at := -1
	for i, c := range p.choices() {
		if c.title == title {
			at = i
			break
		}
	}
	if at < 0 {
		t.Fatalf("the pane offers %v, with no %q", choiceTitles(p), title)
	}
	// On a goroutine, because pressing this reaches the window over
	// there and that window only answers while it is being pumped.
	done := make(chan struct{})
	var err error
	go func() {
		defer close(done)
		err = p.press(at)
	}()
	waitFor(t, other, "the window over there to answer "+title, func() bool {
		select {
		case <-done:
			return true
		default:
			return false
		}
	}, a)
	if err != nil {
		t.Fatalf("press %q: %v", title, err)
	}
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
	d := theJobPane(t, client)
	pressChoiceOver(t, client, host, d, btnRepeat)

	waitFor(t, client, "the copy to be done again", func() bool {
		client.refreshJobs()
		_, err := os.Stat(copied)
		return err == nil
	})
	// Read from margit, which is the only thing that tells the machine
	// apart from the window it is reached through. Exactly one more
	// session: one on the other end would be the repeat reading margit
	// where it should be writing here.
	if got := margit.SFTPs(); got != sessions+1 {
		t.Errorf("margit served %d file sessions, want one more than the %d it had before the repeat",
			got, sessions)
	}
	// And the end the repeat opened is the machine over there, not the
	// window's own disk.
	if got := d.from.far; got.window != windowAt(t, client, addr) || got.host != "margit" {
		t.Errorf("the job's source end is %v, want margit on the window taken over", got)
	}
	// The other end is this machine and no window, so the copy went the
	// way it went the first time.
	if got := d.to; got.far.window != nil || got.host != conns.Local {
		t.Errorf("the job's destination end is %+v, want this machine with no window", got)
	}
}

// Repeat after the window has been renamed files the new row under the
// name the window has now.
//
// Where a row goes is worked out when the repeat opens its ends, not when
// the job first ran. One filed under the old name is drawn under a
// heading nothing is held at, which is to say nowhere.
func TestRepeatingACopyAfterTheWindowIsRenamedFilesItUnderTheNewName(t *testing.T) {
	host, client, addr, keyFile := aServingWindowConnectedToMargit(t)

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
	if e.Host != addr {
		t.Fatalf("the copy's row is filed under %q, want the window %q", e.Host, addr)
	}

	// Renamed between the copy and its repeat, which is what moves the
	// window's rows to another heading.
	saveWindowFromTheDialog(t, client, "office", addr, keyFile)
	if client.windows.named("office") == nil {
		t.Fatalf("the window is held under %v, want office", client.windows.names())
	}

	openTheRow(t, client, e)
	d := theJobPane(t, client)
	pressChoiceOver(t, client, host, d, btnRepeat)

	again := theJobRow(t, client)
	if again == e {
		t.Fatal("the repeat put no new row on the sidebar")
	}
	if again.Host != "office" {
		t.Errorf("the repeated copy's row is filed under %q, want office", again.Host)
	}
	// Drawn under the window's heading rather than nowhere.
	client.refreshPanel(panelNow)
	window := rowAt(client, hostKey("office"))
	mine := rowAt(client, again)
	if window < 0 || mine < 0 || window >= mine {
		t.Errorf("office is drawn at %d and the repeated copy's row at %d: %v",
			window, mine, panelText(client, panelNow))
	}
}

// A piece of file work is filed on the sidebar by what it does, so a
// copy, a move and a delete each carry their own picture.
func TestAJobRowIsFiledByWhatItDoes(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	into := t.TempDir()
	here := jobEnd{host: conns.Local}

	for kind, want := range map[jobs.Kind]conns.Kind{
		jobs.Copy:   conns.Copy,
		jobs.Move:   conns.Move,
		jobs.Delete: conns.Delete,
	} {
		at := aDroppedFile(t, kind.String()+".txt", 64)
		op := jobs.Op{
			Kind: kind,
			From: vfs.NewLocal(), At: filepath.Dir(at), Names: []string{filepath.Base(at)},
			To: vfs.NewLocal(), Into: into,
		}
		j := a.runJob(op, here, here, nil)
		if j == nil {
			t.Fatalf("a %v did not start", kind)
		}
		row := theRowFor(t, a, j)
		if row.Kind != want {
			t.Errorf("a %v has a %v row, want a %v", kind, row.Kind, want)
		}
		if got := icon(row.Kind); got != icon(want) {
			t.Errorf("a %v carries %v, want %v", kind, got, icon(want))
		}
	}
}

// theRowFor is the sidebar row a job has.
func theRowFor(t *testing.T, a *testApp, j *jobs.Job) *conns.Entry {
	t.Helper()
	for e, held := range a.jobs {
		if held == j {
			return e
		}
	}
	t.Fatal("the job has no row on the sidebar")
	return nil
}

// focusedChoice is what the keyboard is on along the bottom of a pane.
func focusedChoice(p *jobPane) string {
	choices := p.choices()
	if p.at < 0 || p.at >= len(choices) {
		return ""
	}
	return choices[p.at].title
}

// ticked reports whether one of the pane's boxes is ticked now.
//
// Asked afresh rather than remembered: the box says what the saved list
// holds, and the same copy can be taken off it from another pane.
func ticked(p *jobPane, title string) bool {
	for _, c := range p.choices() {
		if c.title == title {
			return c.tick && c.on
		}
	}
	return false
}

// The pane leaves nothing of the old layout behind when it shrinks.
//
// The run goes when the job finishes and everything under it moves up,
// so a row that is not written again is a row still carrying whatever
// was there before -- a name from the list, drawn one row below the
// list it belongs to.
func TestAJobsPaneLeavesNothingBehindWhenItShrinks(t *testing.T) {
	a, _ := aCopyWindow(t)
	// Several names, so there is a list under the bar at all.
	at, into := t.TempDir(), t.TempDir()
	names := []string{"file3.bin", "file4.bin", "file5.bin"}
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(at, name), []byte("x"), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	here := jobEnd{host: conns.Local}
	j := a.runJob(jobs.Op{
		Kind: jobs.Copy,
		From: vfs.NewLocal(), At: at, Names: names,
		To: vfs.NewLocal(), Into: into,
	}, here, here, nil)
	waitFor(t, a, "the copy to finish", func() bool { return j.Progress().Done })
	a.showJobPane(j, theRowFor(t, a, j), here, here)
	d := theJobPane(t, a)
	started := time.Now()

	// Part way through, with the list of names under the bar.
	running := jobs.Progress{
		Files: 3, FilesDone: 1, Bytes: 500, BytesDone: 300,
		Current: "file4.bin", Started: started,
	}
	// A run to draw, which is what makes the pane taller while the copy
	// is going and shorter when it stops.
	rate := &meter.Rate{}
	a.rates[d.entry] = rate
	for i := range 5 {
		d.entry.Meter.Moved(1_000_000, 0, started.Add(time.Duration(i)*time.Second))
		rate.Sample(d.entry.Meter, started.Add(time.Duration(i)*time.Second))
	}
	g := grid.New(80, 24, color.RGBA{}, color.RGBA{})
	d.Layout(ui.Size{Cols: 80, Rows: 24})
	d.draw(g.View(), running, started.Add(4*time.Second))
	if !strings.Contains(gridRows(g), string(barFull)) {
		t.Fatalf("the running pane drew no run, so nothing will move:\n%s", gridRows(g))
	}

	// And then finished, which takes a line away and moves the rest up.
	done := running
	done.Done, done.FilesDone, done.BytesDone = true, 3, 500
	done.Current, done.Ended = "", started.Add(5*time.Second)
	d.draw(g.View(), done, started.Add(5*time.Second))

	drawn := gridRows(g)
	// Three names, and no fourth row left over from the taller layout.
	if got := strings.Count(drawn, ".bin"); got != len(names) {
		t.Errorf("the pane draws %d names, want %d:\n%s", got, len(names), drawn)
	}
	if strings.Contains(drawn, "> ") {
		t.Errorf("the pane still marks a name as the one being worked on:\n%s", drawn)
	}
}
