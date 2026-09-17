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
	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/input"
	"github.com/marrasen/gridterm/internal/sshtest"
	"github.com/marrasen/gridterm/jobs"
	"github.com/marrasen/gridterm/meter"
	"github.com/marrasen/gridterm/ui"
	"github.com/marrasen/gridterm/ui/term"
	"github.com/marrasen/gridterm/vfs"
)

// halfwayFS is this machine, with the first write to a file let through
// and every write after it held until the test lets go.
//
// It stands for a copy part way through one big file: some of the bytes
// are counted rather than all or none of them, which is the state a
// filled row exists to show.
type halfwayFS struct {
	vfs.FS

	held chan struct{}

	// wrote is closed once one write has gone through, so a test can wait
	// for the copy to be part way rather than guess.
	once  sync.Once
	wrote chan struct{}

	release sync.Once
}

func newHalfwayFS() *halfwayFS {
	return &halfwayFS{
		FS:    vfs.NewLocal(),
		held:  make(chan struct{}),
		wrote: make(chan struct{}),
	}
}

// Name is what the panel calls it.
func (f *halfwayFS) Name() string { return "halfway" }

// Create makes the file, with its writes held after the first one.
func (f *halfwayFS) Create(path string, mode fs.FileMode) (io.WriteCloser, error) {
	w, err := f.FS.Create(path, mode)
	if err != nil {
		return nil, err
	}
	return &halfwayWriter{WriteCloser: w, on: f}, nil
}

// let lets the writes through. It can be called more than once, so a test
// can free a job on its way out without knowing whether it already did.
func (f *halfwayFS) let() { f.release.Do(func() { close(f.held) }) }

// partWay waits for one write to have gone through.
func (f *halfwayFS) partWay(t *testing.T) {
	t.Helper()
	select {
	case <-f.wrote:
	case <-time.After(waitBudget):
		t.Fatal("no job ever wrote to the filesystem")
	}
}

// halfwayWriter writes once and then waits for the test.
type halfwayWriter struct {
	io.WriteCloser

	on   *halfwayFS
	past bool
}

func (w *halfwayWriter) Write(p []byte) (int, error) {
	if w.past {
		<-w.on.held
	}
	n, err := w.WriteCloser.Write(p)
	w.past = true
	w.on.once.Do(func() { close(w.on.wrote) })
	return n, err
}

// A copy part way through one big file fills that share of its row. A
// single file used to sit at "0 of 1" until it was done.
func TestAJobsRowFillsAsTheBytesGo(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)

	held := newHalfwayFS()
	b, from, _ := browserOnto(t, a, held)
	// After the browser, which is what makes the directories the copy
	// runs between. Cleanups run in reverse, so letting the writer go has
	// to be registered last to happen first: a test that failed early
	// would otherwise take the directories away while the writer is still
	// holding a file open in one of them.
	t.Cleanup(held.let)
	// More than one read, so the copy counts some of the file and then
	// stops with the rest of it to go.
	putFile(t, from, "big.bin", strings.Repeat("x", 3*64*1024))
	left := b.Panes()[0]
	left.Reload()
	waitFor(t, a, "the listing", func() bool { return !left.Busy() })

	copyTheFirstFile(t, a, b)
	held.partWay(t)

	e := theJobRow(t, a)
	var fill float64
	waitFor(t, a, "the row to fill", func() bool {
		a.refreshJobs()
		a.refreshPanel(time.Now())
		row, ok := panelRow(a, e)
		fill = row.Fill
		return ok && fill > 0
	})
	if fill >= 1 {
		t.Fatalf("the row is filled %v while the copy is part way through", fill)
	}
	// The note is the count of files, as it was.
	if e.Note != "0 of 1" {
		t.Errorf("the row says %q, want the count of files", e.Note)
	}
	// And a copy that is still going offers nothing to clear, with the
	// pointer on it or away: the cross on hover is a pane's, and a copy
	// is stopped from the menu.
	if row, _ := panelRow(a, e); row.Button != 0 {
		t.Errorf("a running copy's row offers %q", row.Button)
	}
	pointAtRow(t, a, e)
	if row, _ := panelRow(a, e); row.Button != 0 || row.HoverButton != 0 {
		t.Errorf("a running copy's row offers %q with the pointer on it", row.HoverButton)
	}

	held.let()
	waitFor(t, a, "the copy to finish", func() bool {
		a.refreshJobs()
		return len(a.jobs) == 0
	})
	a.refreshPanel(time.Now())
	row, ok := panelRow(a, e)
	if !ok {
		t.Fatalf("the finished copy has no row: %v", panelText(a, time.Now()))
	}
	if row.Fill != 0 {
		t.Errorf("the finished row is filled %v, want nothing", row.Fill)
	}
}

// clickClear presses the middle of the cell a row's button is drawn in,
// the way a user presses the × at the end of a finished row: a pixel,
// through the app's own mapping from pixels to cells.
//
// The pixel rather than the cell, because the sidebar is drawn on a grid
// of its own at its own row heights. Counting window rows down from the
// top of it names a row it is not drawing there, and a regression in that
// mapping would go unseen.
//
// It presses that column whether or not the row draws anything there, so a
// test can watch what a row with no × does with the press.
func clickClear(t *testing.T, a *testApp, key any) {
	t.Helper()
	a.refreshPanel(time.Now())
	a.placeRegions()
	area, ok := a.root.AreaOf(a.side)
	if !ok {
		t.Fatal("the sidebar is not in the tree")
	}
	// Asked of the list rather than worked out here, so the test and the
	// list cannot drift apart about where the button goes.
	col := a.panel.ButtonCol()
	if col < 0 {
		t.Fatalf("the sidebar is %d columns wide, too narrow to draw a button", area.Cols)
	}
	y := a.panel.RowTop(key)
	if y < 0 {
		t.Fatalf("no row for %v: %v", key, panelText(a, time.Now()))
	}
	x, wide := a.sideGeo.ColBox(col, col+1)
	top, high := a.sideGeo.RowBox(y, y+1)
	at, on := a.cellAt(a.sideRegion.left+x+wide/2, a.sideRegion.top+top+high/2)
	took, err := a.root.HandleMouse(input.MouseEvent{
		Kind: input.MousePress, Button: input.MouseLeft, Col: at, Row: on,
	})
	if err != nil {
		t.Fatalf("the press on the × failed: %v", err)
	}
	if !took {
		t.Fatal("the press on the × travelled on")
	}
}

// A connection that dropped carries a × at the end of its row, and
// clicking it takes the row off the panel.
func TestTheCrossClearsADroppedConnection(t *testing.T) {
	s := sshtest.New(t)
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)

	a.connect(serverConfig(t, s))
	waitForPanes(t, a, 2)
	host := serverConfig(t, s).Target()
	row := serverRow(t, a, host)

	// A connection that is still up offers nothing to clear. Asked of the
	// pane running on it, which is a row the sidebar really draws: the
	// machine's own entry is the heading above its rows, so asking about
	// that one proves nothing.
	a.refreshPanel(time.Now())
	live := liveRowUnder(t, a, host)
	drawn, ok := panelRow(a, live)
	if !ok {
		t.Fatalf("the pane on the machine has no row: %v", panelText(a, time.Now()))
	}
	if drawn.Button != 0 {
		t.Errorf("a live row offers %q", drawn.Button)
	}

	// The network goes away.
	s.CloseClients()
	waitFor(t, a, "the connection to be given up on", func() bool {
		return a.machines.named(host) == nil
	})

	a.refreshPanel(time.Now())
	drawn, ok = panelRow(a, row)
	if !ok {
		t.Fatalf("the dropped connection has no row: %v", panelText(a, time.Now()))
	}
	if drawn.Button != clearButton {
		t.Fatalf("the greyed row offers %q, want the ×", drawn.Button)
	}

	clickClear(t, a, row)
	a.refreshPanel(time.Now())
	if _, ok := panelRow(a, row); ok {
		t.Errorf("the row is still on the panel: %v", panelText(a, time.Now()))
	}
}

// A copy that has finished carries the same ×, and clicking it takes the
// row off the panel without cancelling anything or saying a word.
func TestTheCrossClearsAFinishedCopy(t *testing.T) {
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
	waitFor(t, a, "the copy to finish", func() bool {
		a.refreshJobs()
		return len(a.jobs) == 0
	})

	a.refreshPanel(time.Now())
	drawn, ok := panelRow(a, e)
	if !ok {
		t.Fatalf("the finished copy has no row: %v", panelText(a, time.Now()))
	}
	if drawn.Button != clearButton {
		t.Fatalf("the finished copy's row offers %q, want the ×", drawn.Button)
	}

	clickClear(t, a, e)
	a.refreshPanel(time.Now())
	if _, ok := panelRow(a, e); ok {
		t.Errorf("the row is still on the panel: %v", panelText(a, time.Now()))
	}
	if n := len(a.queue.Jobs()); n != 0 {
		t.Errorf("the queue still holds %d jobs", n)
	}
	// Nothing was said: the copy had already finished, so clearing its
	// row is not a cancel and there is nothing to report.
	if m := a.root.Modal(); m != nil {
		t.Errorf("clearing the row put %T on screen", m)
	}
}

// liveRowUnder is the row of something still running on a machine.
//
// Not the connection to the machine itself: that entry is the heading
// above its rows and is never drawn as one of them, so a test asking
// about it proves nothing.
func liveRowUnder(t *testing.T, a *testApp, host string) *conns.Entry {
	t.Helper()
	for _, e := range a.rowsUnder(host) {
		if e.Kind != conns.Server {
			return e
		}
	}
	t.Fatalf("nothing but the connection itself is open on %q: %v", host, panelText(a, time.Now()))
	return nil
}

// The panel really paints the fill: the first cells of a job's row are on
// the style's fill colour while the copy is part way through, and the rest
// of the row is on the ground it is on when the copy has finished.
//
// Every other test reads the row the panel built rather than the cells it
// drew, so the fill colour could be taken off the panel's style and
// nothing would notice.
func TestAFilledRowIsPaintedInTheFillColour(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	if a.panel.Style.FillBG.A == 0 {
		t.Fatal("the panel has no fill colour, so a row filling up would show nothing")
	}

	held := newHalfwayFS()
	b, from, _ := browserOnto(t, a, held)
	t.Cleanup(held.let)
	putFile(t, from, "big.bin", strings.Repeat("x", 3*64*1024))
	left := b.Panes()[0]
	left.Reload()
	waitFor(t, a, "the listing", func() bool { return !left.Busy() })

	copyTheFirstFile(t, a, b)
	held.partWay(t)
	e := theJobRow(t, a)

	at := windowCell(a)
	area, shown := sideArea(a)
	if !shown {
		t.Fatal("the sidebar is not on screen")
	}
	var near, far grid.Cell
	waitFor(t, a, "the row to be painted in the fill colour", func() bool {
		a.refreshJobs()
		a.refreshPanel(time.Now())
		paint(a)
		y := a.panel.RowTop(e)
		if y < 0 {
			return false
		}
		near = at(area.X, area.Y+y)
		far = at(area.X+area.Cols-1, area.Y+y)
		return near.BG == a.panel.Style.FillBG
	})
	if far.BG == near.BG {
		t.Fatalf("the whole row is on the fill %v while the copy is part way through", far.BG)
	}

	// The copy finishes, the row fills nothing, and the cell that was on
	// the fill is on the ground the far end of the row was on all along.
	held.let()
	waitFor(t, a, "the copy to finish", func() bool {
		a.refreshJobs()
		return len(a.jobs) == 0
	})
	a.refreshPanel(time.Now())
	paint(a)
	y := a.panel.RowTop(e)
	if y < 0 {
		t.Fatalf("the finished copy has no row: %v", panelText(a, time.Now()))
	}
	if got := at(area.X, area.Y+y).BG; got != far.BG {
		t.Errorf("the first cell of the finished row is on %v, want the row's ground %v", got, far.BG)
	}
}

// pointAtRow puts the pointer on a sidebar row, the way moving the mouse
// there does, and builds the rows again so they are drawn for it.
//
// The pixel rather than the cell, for the reason clickClear gives: the
// sidebar is drawn on a grid of its own at its own row heights.
func pointAtRow(t *testing.T, a *testApp, key any) {
	t.Helper()
	a.refreshPanel(time.Now())
	a.placeRegions()
	y := a.panel.RowTop(key)
	if y < 0 {
		t.Fatalf("no row for %v: %v", key, panelText(a, time.Now()))
	}
	top, high := a.sideGeo.RowBox(y, y+1)
	a.cellAt(a.sideRegion.left+1, a.sideRegion.top+top+high/2)
	a.refreshPanel(time.Now())
	if got := a.panel.Hover(); got < 0 {
		t.Fatalf("the pointer is on no row after being put on %v", key)
	}
}

// pointAway takes the pointer off the sidebar.
func pointAway(t *testing.T, a *testApp) {
	t.Helper()
	a.placeRegions()
	a.cellAt(a.sideRegion.left+a.sideRegion.width+1, a.sideRegion.top)
	a.refreshPanel(time.Now())
}

// A pane's row carries a × while the pointer is on it, and pressing it
// closes the pane and the row together.
func TestTheCrossOnAPaneRowClosesThePane(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)

	first, ok := onlyPaneWidget(t, a).(*term.Terminal)
	if !ok {
		t.Fatal("the window opened on something that is not a terminal")
	}
	e := a.panes[first]
	// A second pane, so closing the first does not close the window.
	if err := a.openPane(); err != nil {
		t.Fatalf("openPane: %v", err)
	}

	pointAtRow(t, a, e)
	drawn, ok := panelRow(a, e)
	if !ok {
		t.Fatalf("the pane has no row: %v", panelText(a, time.Now()))
	}
	if drawn.HoverButton == 0 {
		t.Fatal("a pane's row carries nothing to offer under the pointer")
	}
	// And the pointer is on that row, which is what makes the list draw
	// it. The row carries the same character whatever the pointer does.
	if got, want := a.panel.Hover(), a.panel.RowTop(e); got != want {
		t.Fatalf("the pointer is on row %d, want the pane's row %d", got, want)
	}

	clickClear(t, a, e)
	if a.panes[first] != nil {
		t.Fatal("the press left the pane open")
	}
	a.refreshPanel(time.Now())
	if _, ok := panelRow(a, e); ok {
		t.Errorf("the row is still on the panel: %v", panelText(a, time.Now()))
	}
}

// A file pane's row carries the cross too. The manager holds a pane per
// machine and the sidebar gives each one a row, so each is closed from
// its own.
func TestTheCrossOnAFilePaneRowClosesThatPane(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	p := openFilesFromThePlus(t, a, conns.Local)
	waitFor(t, a, "the pane to land somewhere", func() bool { return p.At() != "" })

	e := a.files.rows[p]
	if e == nil {
		t.Fatal("the file pane has no row")
	}
	pointAtRow(t, a, e)
	clickClear(t, a, e)

	if a.files != nil && a.files.rows[p] != nil {
		t.Fatal("the press left the file pane open")
	}
	a.refreshPanel(time.Now())
	if _, ok := panelRow(a, e); ok {
		t.Errorf("the row is still on the panel: %v", panelText(a, time.Now()))
	}
}

// The cross goes when the window does. ebiten hands back the last place
// it saw the pointer, so a window that lost the focus would keep drawing
// a cross under a pointer that is somewhere else.
func TestNoCrossOnceTheWindowHasLostThePointer(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	first, ok := onlyPaneWidget(t, a).(*term.Terminal)
	if !ok {
		t.Fatal("the window opened on something that is not a terminal")
	}
	e := a.panes[first]
	if err := a.openPane(); err != nil {
		t.Fatalf("openPane: %v", err)
	}

	pointAtRow(t, a, e)
	a.pointerGone = true
	a.refreshPanel(time.Now())
	if got := a.panel.Hover(); got >= 0 {
		t.Errorf("the pointer is on row %d with the window not under it", got)
	}

	// So the press lands on the row and puts the pane in front.
	clickClear(t, a, e)
	if a.panes[first] == nil {
		t.Fatal("the press took the pane away")
	}
}

// A pane closed by a key in the same frame is not closed again by the
// cross. The rows that stand for a pane are a frame old.
func TestTheCrossSaysNothingAboutAPaneAKeyHasClosed(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	first, ok := onlyPaneWidget(t, a).(*term.Terminal)
	if !ok {
		t.Fatal("the window opened on something that is not a terminal")
	}
	e := a.panes[first]
	if err := a.openPane(); err != nil {
		t.Fatalf("openPane: %v", err)
	}

	pointAtRow(t, a, e)
	// Where the cross is, worked out while the pane is still there.
	col, y := a.panel.ButtonCol(), a.panel.RowTop(e)
	if col < 0 || y < 0 {
		t.Fatalf("no cross to press: column %d, row %d", col, y)
	}
	x, wide := a.sideGeo.ColBox(col, col+1)
	top, high := a.sideGeo.RowBox(y, y+1)
	at, on := a.cellAt(a.sideRegion.left+x+wide/2, a.sideRegion.top+top+high/2)

	if err := a.closePane(first); err != nil {
		t.Fatalf("closePane: %v", err)
	}
	// No refreshPanel between the two, which is the frame a key and a
	// press share.
	if _, err := a.root.HandleMouse(input.MouseEvent{
		Kind: input.MousePress, Button: input.MouseLeft, Col: at, Row: on,
	}); err != nil {
		t.Fatalf("the press on the ×: %v", err)
	}

	noNoticeOpens(t, a, "the cross on a pane that had already closed")
}

// With the pointer somewhere else the row carries no ×, so a transcript
// is never one stray click away. The press lands on the row instead and
// puts the pane in front.
func TestAPaneRowCarriesNoCrossWithThePointerAway(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)

	first, ok := onlyPaneWidget(t, a).(*term.Terminal)
	if !ok {
		t.Fatal("the window opened on something that is not a terminal")
	}
	e := a.panes[first]
	if err := a.openPane(); err != nil {
		t.Fatalf("openPane: %v", err)
	}

	pointAway(t, a)
	clickClear(t, a, e)
	if a.panes[first] == nil {
		t.Fatal("the press took the pane away")
	}
	if a.focusedTerminal() != first {
		t.Error("the press did not put the pane in front")
	}
}

// A shell that ended by itself keeps its pane, and its row carries no ×
// unless the pointer is on it.
//
// The pane is still there to read. Pressing where the × would be puts
// that pane in front instead, which is what a press on any other row
// does. "Clear finished connections" is what takes it away.
func TestTheRowOfAShellThatEndedCarriesNoCrossUntilHovered(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)

	first, ok := onlyPaneWidget(t, a).(*term.Terminal)
	if !ok {
		t.Fatal("the window opened on something that is not a terminal")
	}
	e := a.panes[first]
	// A second pane, so the one that ends is not the one in front.
	if err := a.splitHere(ui.Columns); err != nil {
		t.Fatalf("splitHere: %v", err)
	}

	// The shell in the first pane ends on its own.
	if err := a.shells[0].Close(); err != nil {
		t.Fatalf("ending the shell: %v", err)
	}
	waitFor(t, a, "the window to see the shell end", func() bool {
		a.reapExited()
		return a.Ended(first)
	})

	a.refreshPanel(time.Now())
	drawn, ok := panelRow(a, e)
	if !ok {
		t.Fatalf("the shell that ended has no row: %v", panelText(a, time.Now()))
	}
	if drawn.Button != 0 {
		t.Fatalf("the greyed row offers %q with no pointer on it", drawn.Button)
	}

	// The press lands on the row rather than on a button, so the pane
	// comes to the front and stays open.
	clickClear(t, a, e)
	if a.panes[first] == nil {
		t.Fatal("the press took the pane away")
	}
	if a.focusedTerminal() != first {
		t.Error("the press did not put the pane in front")
	}

	// And clearing finished connections is what takes it away.
	if err := a.clearFinished(); err != nil {
		t.Fatalf("clearFinished: %v", err)
	}
	a.refreshPanel(time.Now())
	if _, ok := panelRow(a, e); ok {
		t.Errorf("the row is still on the panel: %v", panelText(a, time.Now()))
	}
}

// A command that has finished keeps its pane, so that what it printed can
// still be read, and its row carries no × unless the pointer is on it:
// a stray click must not take the transcript away.
//
// The row has nothing to clear either. "Clear finished connections"
// closes it, which is the user saying they have read it.
func TestAFinishedCommandsRowHasNoCrossUntilHovered(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)

	pane, ok := onlyPaneWidget(t, a).(*term.Terminal)
	if !ok {
		t.Fatal("the window opened on something that is not a terminal")
	}
	e := a.panes[pane]
	// A command rather than a shell, for the kind the rule was written
	// for. Every pane keeps what it printed now, whatever ran in it.
	e.Kind = conns.Command
	if err := a.shells[0].Close(); err != nil {
		t.Fatalf("ending the command: %v", err)
	}
	waitFor(t, a, "the command to end", func() bool {
		a.reapExited()
		return a.ended[pane]
	})

	a.refreshPanel(time.Now())
	drawn, ok := panelRow(a, e)
	if !ok {
		t.Fatalf("the finished command has no row: %v", panelText(a, time.Now()))
	}
	if drawn.Button != 0 {
		t.Errorf("the finished command's row offers %q, which would throw the transcript away",
			drawn.Button)
	}
	if e.Clear != nil {
		t.Error("the finished command's row says it can be cleared")
	}

	// Pressing the column the × would be in leaves the pane where it is.
	// The row has no button there, so the press is an ordinary press on
	// the row: it puts that pane in front, which is what a press on a row
	// with nothing at its end does anywhere else on the panel.
	clickClear(t, a, e)
	if len(a.panes) != 1 {
		t.Fatalf("the press took the pane away: the window holds %d panes", len(a.panes))
	}
	if _, ok := panelRow(a, e); !ok {
		t.Fatalf("the press took the row away: %v", panelText(a, time.Now()))
	}
	// And so does the button's own path, which is what the press runs.
	if err := a.clearRow(e); err != nil {
		t.Fatalf("clearRow: %v", err)
	}
	if len(a.panes) != 1 {
		t.Fatalf("clearing the row took the pane away: the window holds %d panes", len(a.panes))
	}

	// Clearing finished connections still does close it.
	if err := a.clearFinished(); err != nil {
		t.Fatalf("clearFinished: %v", err)
	}
	if len(a.panes) != 0 {
		t.Errorf("the window still holds %d panes after clearing finished connections", len(a.panes))
	}
	a.refreshPanel(time.Now())
	if _, ok := panelRow(a, e); ok {
		t.Errorf("the row is still on the panel: %v", panelText(a, time.Now()))
	}
}

// A clear that failed is shown under a title naming what was pressed, and
// the panel is drawn again whatever happened.
func TestAClearThatFailsSaysWhatWasPressed(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)

	e := &conns.Entry{Host: conns.Local, Kind: conns.Files, Label: "copy one.txt", Meter: meter.New()}
	e.Meter.Close()
	e.Clear = func() error { return errors.New("the row would not go") }
	a.registry.Add(e)

	a.refreshPanel(time.Now())
	drawn, ok := panelRow(a, e)
	if !ok || drawn.Button != clearButton {
		t.Fatalf("the finished row offers %q, want the ×", drawn.Button)
	}
	clickClear(t, a, e)
	awaitModal(t, a, "a notice about the clear", byTitle[*ui.Notice]("Could not clear that row"))
}

// jobFill is the share of a job that has gone: in bytes where they are
// known, and in files until they are.
func TestJobFillMeasuresWhatHasGone(t *testing.T) {
	cases := []struct {
		what string
		p    jobs.Progress
		want float64
	}{
		{"bytes", jobs.Progress{Files: 1, Bytes: 400, BytesDone: 100}, 0.25},
		{"files, while the bytes are not known", jobs.Progress{Files: 4, FilesDone: 3}, 0.75},
		{"a file bigger than it was counted as", jobs.Progress{Files: 1, Bytes: 100, BytesDone: 250}, 1},
		{"a job that has looked at nothing yet", jobs.Progress{}, 0},
		{"a job that has finished", jobs.Progress{Files: 1, Bytes: 400, BytesDone: 400, Done: true}, 0},
	}
	for _, c := range cases {
		if got := jobFill(c.p); got != c.want {
			t.Errorf("%s fills %v, want %v", c.what, got, c.want)
		}
	}
}
