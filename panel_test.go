package main

import (
	"errors"
	"image/color"
	"strings"
	"testing"
	"time"

	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/input"
	"github.com/marrasen/gridterm/internal/sshtest"
	"github.com/marrasen/gridterm/meter"
	"github.com/marrasen/gridterm/remote"
	"github.com/marrasen/gridterm/ui"
	"github.com/marrasen/gridterm/ui/term"
)

// now is a fixed moment, so nothing here waits four seconds to watch a
// connection settle.
var panelNow = time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)

// withPanel gives a test app the panel and the dock, the way main does:
// the sidebar on a grid of its own, drawn and laid out by its region.
func withPanel(t *testing.T, a *testApp) {
	t.Helper()
	a.panel = a.newPanel()
	a.side = a.newSidebar()
	a.dock = a.newDock(a.root.Widget())
	a.sideRegion = newRegion(a.side, grid.New(0, 0, a.colours.FG, a.colours.BG), &a.sideGeo)
	a.dock.PanelElsewhere = true
	a.root.SetWidget(a.dock)
	a.relayout()
	a.placeRegions()
}

// paint draws the window and the sidebar, which are two grids now.
func paint(a *testApp) {
	a.root.Draw(a.g.View())
	a.sideRegion.draw()
}

// windowCell reads a cell by where it is in the window, from whichever
// grid holds it. The sidebar is drawn on its own, at its own row
// heights, so the window's grid has nothing under it.
func windowCell(a *testApp) func(x, y int) grid.Cell {
	return func(x, y int) grid.Cell {
		if r := a.sideRegion; r != nil && !r.rect.Empty() &&
			x >= r.rect.X && x < r.rect.X+r.rect.Cols &&
			y >= r.rect.Y && y < r.rect.Y+r.rect.Rows {
			return r.g.At(x-r.rect.X, y-r.rect.Y)
		}
		return a.g.At(x, y)
	}
}

// sideArea is where the sidebar is in the window, which is the region's
// box rather than the dock's: the region gives up rows to pay for the
// room around the machine names.
func sideArea(a *testApp) (ui.Rect, bool) {
	if a.sideRegion == nil || a.sideRegion.rect.Empty() {
		return ui.Rect{}, false
	}
	return a.sideRegion.rect, true
}

// paneRow is the sidebar row that names a connection, found by what it
// names rather than by where it sits: a heading or a pinned row above it
// would move every position.
func paneRow(t *testing.T, a *testApp, want *conns.Entry) ui.ListRow {
	t.Helper()
	panelText(a, panelNow)
	for _, row := range a.panel.Rows() {
		if e, is := row.Key.(*conns.Entry); is && e == want {
			return row
		}
	}
	t.Fatalf("the sidebar has no row for that connection: %v", panelText(a, panelNow))
	return ui.ListRow{}
}

// unfocusedPaneRow is the sidebar row of a pane that does not have the
// keys, which is the one a test chooses to see something happen.
func unfocusedPaneRow(t *testing.T, a *testApp) ui.ListRow {
	t.Helper()
	focused := a.panes[a.focusedTerminal()]
	panelText(a, panelNow)
	for _, row := range a.panel.Rows() {
		e, is := row.Key.(*conns.Entry)
		if is && e != focused {
			return row
		}
	}
	t.Fatalf("every row on the sidebar names the focused pane: %v", panelText(a, panelNow))
	return ui.ListRow{}
}

// chooseRow puts the sidebar's bar on a row, which is what choosing it
// in the sidebar does.
func chooseRow(t *testing.T, a *testApp, want *conns.Entry) {
	t.Helper()
	panelText(a, time.Now())
	if !a.panel.Select(want) {
		t.Fatalf("the sidebar has no row to choose: %v", panelText(a, time.Now()))
	}
}

// clearTheRow clears whichever row the sidebar has chosen, through the
// menu line that does it.
func clearTheRow(t *testing.T, a *testApp) {
	t.Helper()
	chooseMenuItem(t, openBarMenu(t, a, "Connection"), "conn.close")
}

// panelText returns what the panel is showing, one line per row, with
// the note in brackets.
func panelText(a *testApp, now time.Time) []string {
	a.refreshPanel(now)
	rows := a.panel.Rows()
	out := make([]string, len(rows))
	for i, row := range rows {
		out[i] = strings.TrimSpace(row.Text)
		if row.Note != "" {
			out[i] += " [" + row.Note + "]"
		}
	}
	return out
}

// panelMarks returns the colour of the mark in front of each connection,
// which is its kind icon. A machine's heading is left out: it carries a
// dot rather than an icon, and it is about the connection rather than
// about anything running on it.
func panelMarks(a *testApp, now time.Time) []color.RGBA {
	a.refreshPanel(now)
	var out []color.RGBA
	for _, row := range a.panel.Rows() {
		if !row.Header && row.Icon.Kind != grid.ArtNone {
			out = append(out, row.IconFG)
		}
	}
	return out
}

// onlyPane returns the app's single terminal and its panel entry.
func onlyPane(t *testing.T, a *testApp) *conns.Entry {
	t.Helper()
	if len(a.panes) != 1 {
		t.Fatalf("%d panes, want 1", len(a.panes))
	}
	for _, e := range a.panes {
		return e
	}
	return nil
}

// A pane that opens gets a row, under the machine it is running on.
func TestPanelShowsTheShellThatIsOpen(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)

	got := panelText(a, panelNow)
	if len(got) != 2 {
		t.Fatalf("the panel shows %v, want a heading and a row", got)
	}
	if got[0] != "Local" {
		t.Errorf("the heading is %q, want Local", got[0])
	}
	if got := rowIcon(a, 1); got != grid.Icon(grid.IconTerminal) {
		t.Errorf("the row carries %v, want a terminal", got)
	}
}

// The four states, seen through the panel: opened until something
// moves, active for four seconds, settled after that, closed at the end.
func TestPanelShowsTheFourStates(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	e := onlyPane(t, a)

	green := a.colours.ANSI[2]
	grey := a.colours.ANSI[8]

	if got := panelMarks(a, panelNow)[0]; got != green {
		t.Fatalf("a row with nothing moved is %v, want the green of something that is there", got)
	}

	// While bytes are going past the mark brightens and dims, so it is
	// green but not the steady green of a row that is only there.
	e.Meter.Moved(64, 0, panelNow)
	busy := panelMarks(a, panelNow)[0]
	if busy == grey {
		t.Fatal("a busy row is grey")
	}
	var moved bool
	for step := 0; step < 8; step++ {
		at := panelNow.Add(time.Duration(step) * pulseStep)
		if panelMarks(a, at)[0] != busy {
			moved = true
		}
	}
	if !moved {
		t.Fatal("the mark does not move while bytes are going past")
	}

	// Nothing is told to change it: the same panel, asked about a later
	// moment, is the steady green again.
	later := panelNow.Add(meter.Settle)
	if got := panelMarks(a, later)[0]; got != green {
		t.Fatalf("a settled row is %v, want the steady green", got)
	}

	e.Meter.Close()
	if got := panelMarks(a, later)[0]; got != grey {
		t.Fatalf("a finished row is %v, want grey", got)
	}
}

// A connection's row draws its kind icon in the colour the state dot had,
// and no dot beside it: one mark says both what the connection is and
// whether it is open.
func TestAConnectionRowShowsItsKindInTheStateColour(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	e := onlyPane(t, a)
	// The keys are in the pane, not the sidebar, so nothing washes the
	// mark to the row's own colour.
	a.panel.SetFocus(false)

	at := windowCell(a)
	iconAt := func(now time.Time) grid.Cell {
		a.refreshPanel(now)
		paint(a)
		area, shown := sideArea(a)
		if !shown {
			t.Fatal("the sidebar is not on screen")
		}
		// The heading is row 0 and the shell under it is row 1. The icon
		// goes where the text would start, two columns in.
		return at(area.X+2, area.Y+1)
	}

	settled := iconAt(panelNow.Add(meter.Settle))
	if settled.Art != grid.Icon(grid.IconTerminal) {
		t.Fatalf("the row carries %v, want the terminal icon", settled.Art)
	}
	if want := a.colours.ANSI[2]; settled.FG != want {
		t.Fatalf("the icon is %v, want the green of something that is there", settled.FG)
	}

	// And nothing is drawn in the column the dot used to have.
	area, _ := sideArea(a)
	if got := at(area.X, area.Y+1).Rune; got == dot {
		t.Fatal("the row still draws a dot beside its icon")
	}

	e.Meter.Close()
	closed := iconAt(panelNow.Add(meter.Settle))
	if closed.Art != grid.Icon(grid.IconTerminal) {
		t.Fatalf("a finished row carries %v, want the terminal icon still", closed.Art)
	}
	if want := a.colours.ANSI[8]; closed.FG != want {
		t.Fatalf("a finished row's icon is %v, want grey", closed.FG)
	}
}

// A sidebar dragged as narrow as it goes still says what state its rows
// are in. The icon is drawn while there is room for it and the dot comes
// back when there is not, and both carry the state's colour.
func TestANarrowSidebarStillShowsTheState(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	e := onlyPane(t, a)
	a.panel.SetFocus(false)
	// A short note, which a row carries when its connection ended with a
	// short reason. It and the run take the room the icon wanted.
	e.Note = "EOF"
	e.Meter.Moved(64*1024, 0, panelNow)
	now := panelNow.Add(meter.RateWindow)
	a.refreshPanel(panelNow)

	// In as far as the dock will let it go.
	a.dock.Width = 1
	a.relayout()
	a.placeRegions()
	a.refreshPanel(now)
	at := windowCell(a)
	paint(a)

	area, shown := sideArea(a)
	if !shown {
		t.Fatal("the sidebar is not on screen")
	}
	row := a.panel.RowTop(e)
	if row < 0 {
		t.Fatalf("the connection has no row on screen: %v", panelText(a, now))
	}
	want := a.stateFG(e.State(now), now)

	var drew bool
	for x := area.X; x < area.X+area.Cols; x++ {
		c := at(x, area.Y+row)
		if c.Art.Kind != grid.ArtIcon {
			continue
		}
		drew = true
		if c.FG != want {
			t.Fatalf("the icon is %v, want the state's %v", c.FG, want)
		}
	}
	if drew {
		return
	}
	// No room for it, so the dot stands in.
	got := at(area.X, area.Y+row)
	if got.Rune != dot {
		t.Fatalf("a sidebar %d wide holds %q where the mark goes, want the dot",
			area.Cols, got.Rune)
	}
	if got.FG != want {
		t.Fatalf("the mark is %v, want the state's %v", got.FG, want)
	}
}

// The pulse moves the active row's icon and nothing else on the sidebar.
//
// A row that changes colour every frame is a row that dirties itself
// every frame, and the pulse is the one place that is wanted.
func TestThePulseMovesOnlyTheActiveRowsIcon(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	if err := a.openPane(); err != nil {
		t.Fatalf("openPane: %v", err)
	}
	a.panel.SetFocus(false)
	busy := a.panes[a.focusedTerminal()]
	busy.Meter.Moved(64, 0, panelNow)

	a.refreshPanel(panelNow)
	// How far down the sidebar's box the busy row is drawn, asked of the
	// list rather than counted here: a pinned row or a heading above it
	// would move it.
	row := a.panel.RowTop(busy)
	if row < 0 {
		t.Fatalf("the busy connection is not on screen: %v", panelText(a, panelNow))
	}

	at := windowCell(a)
	// Every part of the cell that is drawn: a grid.Cell carries a slice
	// and cannot be compared with ==, so the combining marks come along
	// as a string.
	type look struct {
		rune   rune
		comb   string
		fg, bg color.RGBA
		attr   grid.Attr
		width  uint8
		art    grid.Art
	}
	snap := func(now time.Time) map[[2]int]look {
		a.refreshPanel(now)
		paint(a)
		area, shown := sideArea(a)
		if !shown {
			t.Fatal("the sidebar is not on screen")
		}
		out := map[[2]int]look{}
		for y := area.Y; y < area.Y+area.Rows; y++ {
			for x := area.X; x < area.X+area.Cols; x++ {
				c := at(x, y)
				out[[2]int{x, y}] = look{
					c.Rune, string(c.Comb), c.FG, c.BG, c.Attr, c.Width, c.Art,
				}
			}
		}
		return out
	}

	before := snap(panelNow)
	after := snap(panelNow.Add(pulseStep))

	area, _ := sideArea(a)
	want := [2]int{area.X + 2, area.Y + row}
	var moved int
	for where, cell := range before {
		if cell == after[where] {
			continue
		}
		moved++
		if where != want {
			t.Fatalf("the cell at %v changed a pulse step later, and only the active row's icon should",
				where)
		}
	}
	if moved != 1 {
		t.Fatalf("%d cells changed a pulse step later, want the active row's icon", moved)
	}
}

// The column between the sidebar and the panes is blank -- a line there
// read as a bar between the two rather than as the edge of either -- and
// it is still what the divider is dragged by.
func TestTheSidebarDividerIsBlankAndStillDrags(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	a.refreshPanel(panelNow)
	paint(a)

	// The dock's own idea of where the panel is, so the column read is
	// the divider rather than whatever the sidebar's grid ends at. The
	// divider is drawn on the window's grid: the sidebar has a grid of
	// its own and the column belongs to neither half.
	area, shown := a.dock.ChildArea(a.side)
	if !shown {
		t.Fatal("the sidebar is not on screen")
	}
	col := area.X + area.Cols
	for y := area.Y; y < area.Y+area.Rows; y++ {
		if got := a.g.At(col, y).Rune; got != ' ' {
			t.Fatalf("the divider column holds %q at row %d, want a blank", got, y)
		}
	}

	// Grabbed at the column, moved right, let go: the panel is that much
	// wider.
	was := a.dock.Width
	took, err := a.root.HandleMouse(input.MouseEvent{
		Kind: input.MousePress, Button: input.MouseLeft, Col: col, Row: area.Y + 1,
	})
	if err != nil {
		t.Fatalf("pressing on the divider: %v", err)
	}
	if !took {
		t.Fatal("a press on the divider was not taken, so no drag can follow")
	}
	if _, err := a.root.HandleMouse(input.MouseEvent{
		Kind: input.MouseMove, Col: col + 6, Row: area.Y + 1,
	}); err != nil {
		t.Fatalf("dragging the divider: %v", err)
	}
	if _, err := a.root.HandleMouse(input.MouseEvent{
		Kind: input.MouseRelease, Button: input.MouseLeft, Col: col + 6, Row: area.Y + 1,
	}); err != nil {
		t.Fatalf("letting the divider go: %v", err)
	}
	if a.dock.Width != was+6 {
		t.Fatalf("the panel is %d wide after the drag, want %d", a.dock.Width, was+6)
	}
}

// A window's heading is drawn in its own colour, which is what tells it
// apart from a machine without reading the name.
func TestTheHeadingOfAWindowIsDrawnInItsOwnColour(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	if err := a.book.Put(remote.Host{
		Name: "statio", Address: "10.0.0.5", Port: 2222, Window: true,
	}, ""); err != nil {
		t.Fatalf("Put: %v", err)
	}
	a.refreshServers()
	a.refreshPanel(panelNow)
	if !a.about("statio").serves {
		t.Fatal("the saved window is not known as one")
	}

	at := windowCell(a)
	paint(a)
	area, shown := sideArea(a)
	if !shown {
		t.Fatal("the sidebar is not on screen")
	}
	row := a.panel.RowTop(hostKey("statio"))
	if row < 0 {
		t.Fatalf("the window has no heading on screen: %v", panelText(a, panelNow))
	}
	// The heading is indented like the rows under it, so its text starts
	// two columns in.
	if got := at(area.X+2, area.Y+row).FG; got != a.colours.ANSI[5] {
		t.Fatalf("the window's heading is drawn in %v, want the %v a window gets",
			got, a.colours.ANSI[5])
	}
	// And a machine's heading is not: this one is the local machine.
	local := a.panel.RowTop(hostKey(conns.Local))
	if local < 0 {
		t.Fatal("there is no heading for the local machine")
	}
	if got := at(area.X+2, area.Y+local).FG; got == a.colours.ANSI[5] {
		t.Fatal("a machine's heading is drawn in the colour a window gets")
	}
}

// What a row is doing is the mark's business, so the words beside it are
// only what the mark cannot say.
func TestPanelSaysNoStateInWords(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	e := onlyPane(t, a)

	// Each of the four states in turn, and the row never names one.
	at := []time.Time{panelNow, panelNow, panelNow.Add(meter.Settle), panelNow}
	want := []meter.State{meter.Opened, meter.Active, meter.Settled, meter.Closed}
	for i, state := range want {
		switch state {
		case meter.Active:
			e.Meter.Moved(1, 0, panelNow)
		case meter.Closed:
			e.Meter.Close()
		}
		if got := e.State(at[i]); got != state {
			t.Fatalf("the row is %v, want %v", got, state)
		}
		row := panelText(a, at[i])[1]
		for _, word := range []string{"opened", "active", "settled", "closed"} {
			if strings.Contains(row, word) {
				t.Fatalf("a %v row reads %q, which says its state in words", state, row)
			}
		}
		// And it still says what it is, so the test is not passing on an
		// empty row.
		if got := rowIcon(a, 1); got != grid.Icon(grid.IconTerminal) {
			t.Fatalf("a %v row carries %v, want a terminal", state, got)
		}
	}
}

// The sidebar marks the row for whatever is in front even while the keys
// are somewhere else: it is the list of what is open, so it has to say
// which one is being looked at.
func TestTheSidebarMarksThePaneInFront(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	if err := a.openPane(); err != nil {
		t.Fatalf("openPane: %v", err)
	}
	// The keys are in a pane, not in the sidebar.
	a.panel.SetFocus(false)
	a.refreshPanel(panelNow)
	at := windowCell(a)
	paint(a)

	area, shown := sideArea(a)
	if !shown {
		t.Fatal("the sidebar is not on screen")
	}
	row := a.panel.SelectedIndex()
	if row < 0 {
		t.Fatal("nothing is selected")
	}
	marked := at(area.X+1, area.Y+row).BG
	if want := grid.Blend(a.colours.BG, a.colours.FG, 1, 6); marked != want {
		t.Fatalf("the row in front is drawn on %v, want %v", marked, want)
	}
	// And the rows around it are not.
	for y := 0; y < area.Rows-1; y++ {
		if y == row {
			continue
		}
		if got := at(area.X+1, area.Y+y).BG; got == marked {
			t.Fatalf("row %d is marked as well", y)
		}
	}
}

// A connection moving a lot draws the run rather than naming a speed.
//
// The number took the widest part of the row and pushed out the one
// thing that says which connection this is.
func TestPanelDrawsTheRunRatherThanASpeed(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	e := onlyPane(t, a)

	e.Meter.Moved(1, 0, panelNow)
	panelText(a, panelNow) // the first sample, with nothing to compare to
	e.Meter.Moved(64*1024, 0, panelNow.Add(time.Second))

	at := panelNow.Add(time.Second)
	if got := panelText(a, at)[1]; strings.Contains(got, "/s") {
		t.Fatalf("row = %q, want no speed in words", got)
	}
	if got := rowArt(a, e); got.Kind != grid.ArtGraph {
		t.Fatalf("a busy row carries %v, want the run drawn", got)
	}
}

// What the program in a pane called the window is what the panel shows.
func TestPanelShowsWhatTheProgramCalledItself(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	var pane *term.Terminal
	for p := range a.panes {
		pane = p
	}
	a.setTitle(t, 0, pane, "vim README.md")

	got := panelText(a, panelNow)[1]
	if !strings.Contains(got, "vim README.md") {
		t.Fatalf("row = %q, want the title in it", got)
	}
}

// A machine with nothing open on it is not a heading worth keeping, and
// this machine is always there so a local shell has somewhere to go.
func TestPanelGroupsByMachine(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)

	e := &conns.Entry{Host: "margit", Kind: conns.Tunnel, Label: ":5432", Meter: meter.New()}
	a.registry.Add(e)
	got := panelText(a, panelNow)
	if len(got) != 4 {
		t.Fatalf("the panel shows %v, want two headings and two rows", got)
	}
	if got[2] != "margit" {
		t.Errorf("the second heading is %q, want margit", got[2])
	}
	if !strings.Contains(got[3], ":5432") {
		t.Errorf("the tunnel row is %q", got[3])
	}

	a.registry.Drop(e)
	if got := panelText(a, panelNow); len(got) != 2 {
		t.Fatalf("the panel shows %v after the tunnel went, want the machine gone too", got)
	}
}

// Closing a pane takes its row with it, and nothing is left holding the
// rate it was measured with.
func TestPanelRowGoesWithItsPane(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	if err := a.openPane(); err != nil {
		t.Fatalf("openPane: %v", err)
	}
	if got := panelText(a, panelNow); len(got) != 3 {
		t.Fatalf("the panel shows %v, want a heading and two rows", got)
	}

	// Give each a rate to remember.
	for _, e := range a.panes {
		e.Meter.Moved(1, 0, panelNow)
	}
	panelText(a, panelNow)

	if err := a.closeFocused(); err != nil {
		t.Fatalf("closeFocused: %v", err)
	}
	if got := panelText(a, panelNow); len(got) != 2 {
		t.Fatalf("the panel shows %v after a pane closed", got)
	}
	if len(a.rates) > len(a.panes) {
		t.Fatalf("%d rates are held for %d panes", len(a.rates), len(a.panes))
	}
}

// A shell that has gone stays on the panel: what a command did after it
// stopped is worth reading. Clearing is what takes it off.
func TestPanelKeepsAFinishedConnectionUntilItIsCleared(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	e := onlyPane(t, a)
	e.Meter.Close()

	if got := panelText(a, panelNow); len(got) != 2 {
		t.Fatalf("the panel shows %v, want the finished row kept", got)
	}
	if err := a.clearFinished(); err != nil {
		t.Fatalf("clearFinished: %v", err)
	}
	if got := panelText(a, panelNow); len(got) != 1 {
		t.Fatalf("the panel shows %v after clearing, want the heading alone", got)
	}
}

// Choosing a row puts what it names in front of the user.
func TestPanelActivatingARowRevealsIt(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	if err := a.openPane(); err != nil {
		t.Fatalf("openPane: %v", err)
	}
	// The pane that does not have the keys.
	first := unfocusedPaneRow(t, a)
	want := first.Key.(*conns.Entry)
	a.panel.Select(want)
	if err := a.revealRow(first); err != nil {
		t.Fatalf("reveal: %v", err)
	}

	got := a.panes[a.focusedTerminal()]
	if got != want {
		t.Fatal("choosing a row did not focus the pane it names")
	}
}

// The panel can close what it has selected, which is how a connection
// with no pane of its own is ever ended.
func TestPanelClosesTheSelectedConnection(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	if err := a.openPane(); err != nil {
		t.Fatalf("openPane: %v", err)
	}
	e := unfocusedPaneRow(t, a).Key.(*conns.Entry)
	a.panel.Select(e)
	if err := a.closeSelectedConnection(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if len(a.panes) != 1 {
		t.Fatalf("%d panes, want the other one closed", len(a.panes))
	}
	checkTree(t, a)
}

// A connection the panel cannot close says so rather than doing nothing.
func TestPanelSaysWhenAConnectionCannotBeClosed(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	e := &conns.Entry{Host: "margit", Kind: conns.Tunnel, Label: "held open"}
	a.registry.Add(e)
	panelText(a, panelNow)

	a.panel.Select(e)
	if err := a.closeSelectedConnection(); err == nil {
		t.Fatal("a connection with no way to close reported success")
	}
}

func TestPanelOpensAndCloses(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	a.dock.Collapsed = true

	if err := a.togglePanel(); err != nil {
		t.Fatalf("toggle: %v", err)
	}
	if a.dock.Collapsed {
		t.Fatal("the panel did not open")
	}
	if err := a.togglePanel(); err != nil {
		t.Fatalf("toggle: %v", err)
	}
	if !a.dock.Collapsed {
		t.Fatal("the panel did not close")
	}
}

// A hidden panel is not worth building. The window draws sixty times a
// second and nobody can see it.
func TestPanelBuildsNothingWhileHidden(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	if got := panelText(a, panelNow); len(got) == 0 {
		t.Fatal("the panel showed nothing while open")
	}

	a.dock.Collapsed = true
	a.panel.SetRows(nil)
	a.refreshPanel(panelNow)
	if got := a.panel.Rows(); len(got) != 0 {
		t.Fatalf("a hidden panel built %d rows", len(got))
	}

	// And it comes back the moment it is shown.
	if err := a.showPanel(true); err != nil {
		t.Fatalf("showPanel: %v", err)
	}
	if got := panelText(a, panelNow); len(got) == 0 {
		t.Fatal("the panel stayed empty after it was shown")
	}
}

// Going to the panel opens it first: a command that puts the keys
// somewhere invisible is a command that loses them.
func TestFocusPanelOpensItFirst(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	if err := a.focusPanel(); err != nil {
		t.Fatalf("focusPanel: %v", err)
	}
	if a.dock.Collapsed {
		t.Fatal("the panel is still hidden")
	}
	if a.dock.Focused() != ui.Widget(a.side) {
		t.Fatal("the keys did not go to the panel")
	}
}

// The panel takes the arrow keys while it has the focus, and the pane
// underneath does not see them.
func TestPanelTakesTheKeysWhileItHasThem(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	if err := a.openPane(); err != nil {
		t.Fatalf("openPane: %v", err)
	}
	panelText(a, panelNow)
	if err := a.focusPanel(); err != nil {
		t.Fatalf("focusPanel: %v", err)
	}
	// The bar follows whatever the stage is showing, which is the tab
	// just opened: the last row. Back to the top, so Down has somewhere
	// to go.
	a.panel.Move(-len(a.panel.Rows()))

	first, _ := a.panel.Selected()
	if _, err := a.root.HandleKey(press(input.KeyDown, 0)); err != nil {
		t.Fatalf("down: %v", err)
	}
	second, _ := a.panel.Selected()
	if first.Key == second.Key {
		t.Fatal("Down did not move the panel selection")
	}
	// And the shell was not typed into.
	for _, shell := range a.shells {
		if shell.sentText() != "" {
			t.Fatalf("the shell was sent %q while the panel had the keys", shell.sentText())
		}
	}
}

// An idle panel leaves its layer alone, which is what keeps a window
// with nothing happening from redrawing itself sixty times a second.
func TestPanelDoesNotDirtyAnIdleFrame(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	e := onlyPane(t, a)
	e.Meter.Moved(10, 0, panelNow)

	a.refreshPanel(panelNow)
	paint(a)
	a.sideRegion.g.ClearDirty()

	// A second frame at the same moment changes nothing.
	a.refreshPanel(panelNow)
	paint(a)
	if a.sideRegion.g.AnyDirty() {
		t.Fatal("an idle panel dirtied the layer")
	}

	// And the moment the state changes, it does: the mark goes from the
	// one that moves to the one that does not.
	a.refreshPanel(panelNow.Add(meter.Settle))
	paint(a)
	if !a.sideRegion.g.AnyDirty() {
		t.Fatal("a row that changed from active to settled did not redraw")
	}

	// Nor does time passing, once nothing is moving any more. The mark
	// brightens and dims while bytes are going past, so a panel that
	// kept pulsing after a connection settled would redraw for as long
	// as the window was open.
	for step := 1; step <= 8; step++ {
		at := panelNow.Add(meter.Settle + time.Duration(step)*pulseStep)
		a.refreshPanel(at)
		paint(a)
		a.sideRegion.g.ClearDirty()
	}
	for step := 1; step <= 8; step++ {
		at := panelNow.Add(2*meter.Settle + time.Duration(step)*pulseStep)
		a.refreshPanel(at)
		paint(a)
		if a.sideRegion.g.AnyDirty() {
			t.Fatalf("a settled panel dirtied the layer %v later", at.Sub(panelNow))
		}
	}
}

// The bytes a shell actually sends are what makes its row active. The
// other state tests drive the meter directly; this one goes through the
// session the terminal is reading.
func TestPanelSeesBytesFromTheShellItself(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	e := onlyPane(t, a)

	if got := e.State(time.Now()); got != meter.Opened {
		t.Fatalf("a shell that has said nothing is %v, want opened", got)
	}
	a.shells[0].out <- []byte("hello")
	waitFor(t, a, "the row to say the shell is active", func() bool { return e.State(time.Now()) == meter.Active })
}

// A shell that has gone says so rather than settling and looking merely
// quiet.
func TestPanelSeesTheShellGo(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	e := onlyPane(t, a)

	_ = a.shells[0].Close()
	waitFor(t, a, "the row to say the shell has gone", func() bool { return e.State(time.Now()) == meter.Closed })
}

// Enter on the panel has to do something. Calling revealRow by hand
// tests the function, not that anything is wired to it.
func TestPanelEnterRevealsThroughTheList(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	if err := a.openPane(); err != nil {
		t.Fatalf("openPane: %v", err)
	}
	if err := a.focusPanel(); err != nil {
		t.Fatalf("focusPanel: %v", err)
	}

	// The pane that does not have the keys.
	want := unfocusedPaneRow(t, a).Key.(*conns.Entry)
	a.panel.Select(want)
	if _, err := a.root.HandleKey(press(input.KeyEnter, 0)); err != nil {
		t.Fatalf("enter: %v", err)
	}
	if got := a.panes[a.focusedTerminal()]; got != want {
		t.Fatal("Enter on the panel did not focus the pane the row names")
	}
}

// A connection that is still being made says so, in its own words.
func TestPanelSaysAConnectionIsOnItsWay(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	a.registry.Add(&conns.Entry{
		Host: "margit", Kind: conns.Terminal, Label: "connecting", Note: "opening",
	})

	var found bool
	for _, row := range panelText(a, panelNow) {
		if strings.Contains(row, "connecting") && strings.Contains(row, "[opening]") {
			found = true
		}
	}
	if !found {
		t.Fatalf("the panel shows %v, want a row saying it is opening", panelText(a, panelNow))
	}
}

// A finished row has to read as finished rather than as one more thing
// running.
func TestPanelDimsAFinishedRow(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	e := onlyPane(t, a)

	running := paneRow(t, a, e).FG

	e.Meter.Close()
	finished := paneRow(t, a, e).FG

	if finished == running {
		t.Fatal("a finished row is drawn the same as a running one")
	}
	if finished.A == 0 {
		t.Fatal("a finished row has no colour of its own")
	}
}

// A shell that ends on its own leaves its row behind, greyed to say the
// shell has gone, and leaves its pane with it so that what it printed
// can still be read.
func TestPanelKeepsTheRowOfAShellThatEndedOnItsOwn(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	first, ok := onlyPaneWidget(t, a).(*term.Terminal)
	if !ok {
		t.Fatal("the window opened on something that is not a terminal")
	}
	if err := a.openPane(); err != nil {
		t.Fatalf("openPane: %v", err)
	}
	panelText(a, panelNow)

	// The shell on the first pane goes.
	_ = a.shells[0].Close()
	waitFor(t, a, "the window to see the shell end", func() bool {
		a.reapExited()
		return a.Ended(first)
	})

	var closed int
	for _, mark := range panelMarks(a, panelNow) {
		if mark == a.colours.ANSI[8] {
			closed++
		}
	}
	if closed != 1 {
		t.Fatalf("the panel shows %v, want the shell that ended left behind",
			panelText(a, panelNow))
	}
	if a.panes[first] == nil {
		t.Fatal("the pane went with its shell")
	}

	// And clearing takes the row and its pane off.
	if err := a.clearFinished(); err != nil {
		t.Fatalf("clearFinished: %v", err)
	}
	if a.panes[first] != nil {
		t.Fatal("clearing left the pane open with no row to reach it by")
	}
	for _, row := range panelText(a, panelNow) {
		if strings.Contains(row, "[closed]") {
			t.Fatalf("clearing left %q", row)
		}
	}
}

// Closing the row of a shell that ended takes the row off the panel and
// the pane with it, which is the user saying they have read it.
//
// The row kept a cross and lost its close, so the one command on the
// menu answered "Terminal cannot be closed from here" while the row of a
// connection that dropped closed perfectly well.
func TestClosingTheRowOfAShellThatEndedTakesItOff(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	first, ok := onlyPaneWidget(t, a).(*term.Terminal)
	if !ok {
		t.Fatal("the window opened on something that is not a terminal")
	}
	if err := a.openPane(); err != nil {
		t.Fatalf("openPane: %v", err)
	}
	panelText(a, panelNow)
	stopped := a.panes[first]

	// The shell on the first pane goes, and its row stays behind.
	_ = a.shells[0].Close()
	waitFor(t, a, "the window to see the shell end", func() bool {
		a.reapExited()
		return a.Ended(first)
	})

	a.panel.Select(stopped)
	if err := a.closeSelectedConnection(); err != nil {
		t.Fatalf("closing the row of a shell that ended: %v", err)
	}

	if a.panes[first] != nil {
		t.Error("closing the row left the pane open")
	}
	panelText(a, panelNow)
	for _, row := range a.panel.Rows() {
		if row.Key == any(stopped) {
			t.Errorf("the row is still on the panel: %v", panelText(a, panelNow))
		}
	}
}

// Clearing the last row under a machine takes its commands with it.
//
// The commands are built from what the sidebar holds, and clearing is
// the one path that empties a machine without closing anything. A window
// that kept the commands offered a terminal on a machine it no longer
// showed at all.
func TestClearingTheLastRowOnAMachineTakesItsCommands(t *testing.T) {
	s := sshtest.New(t)
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)

	a.connect(serverConfig(t, s))
	waitForPanes(t, a, 2)
	host := serverConfig(t, s).Target()
	want := termPrefix + remote.CommandName(host)
	if _, ok := a.root.Commands.Lookup(want); !ok {
		t.Fatalf("no %q command while %s is connected", want, host)
	}

	// The machine drops, which greys its row and ends the shell on it.
	s.CloseClients()
	waitFor(t, a, "the window to see the machine go", func() bool {
		a.reapExited()
		return a.machines.named(host) == nil
	})

	if err := a.clearFinished(); err != nil {
		t.Fatalf("clearFinished: %v", err)
	}

	for _, row := range panelText(a, panelNow) {
		if strings.Contains(row, host) {
			t.Fatalf("the sidebar still shows %q after clearing", row)
		}
	}
	if _, ok := a.root.Commands.Lookup(want); ok {
		t.Errorf("%q is still registered for a machine the sidebar no longer holds", want)
	}
}

// The close-pane key with the panel focused used to detach the panel and
// leave the window with no way to get it back.
func TestClosePaneWillNotTakeThePanelOutOfTheTree(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	if err := a.focusPanel(); err != nil {
		t.Fatalf("focusPanel: %v", err)
	}

	if err := a.closeFocused(); err != nil {
		t.Fatalf("closeFocused: %v", err)
	}
	if a.dock.Panel() != ui.Widget(a.side) {
		t.Fatal("the panel was taken out of the dock")
	}
	if len(a.panes) != 1 {
		t.Fatalf("%d panes, want the one that was there", len(a.panes))
	}
	// And the panel still works.
	if err := a.togglePanel(); err != nil {
		t.Fatalf("toggle: %v", err)
	}
}

// A window too narrow for the panel must not take the keys to somewhere
// that is never drawn: the keyboard would stop working with no
// explanation.
func TestFocusPanelRefusesAWindowWithNoRoom(t *testing.T) {
	a := newTestApp(t, 30, 10)
	withPanel(t, a)
	a.relayout()
	if _, shown := a.dock.ChildArea(a.side); shown {
		t.Fatal("this window has room for the panel after all, so the fixture no longer exercises a window with no room")
	}

	if err := a.focusPanel(); err == nil {
		t.Fatal("the keys went to a panel with no room to be drawn")
	}
	if a.dock.Focused() == ui.Widget(a.side) {
		t.Fatal("the panel has the keys and is not on screen")
	}
}

// Clicking a terminal takes the keys back from the panel. Without it the
// user types into something they are not looking at.
func TestClickingATerminalTakesTheKeysBack(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	if err := a.focusPanel(); err != nil {
		t.Fatalf("focusPanel: %v", err)
	}
	if a.dock.Focused() != ui.Widget(a.side) {
		t.Fatal("the panel does not have the keys")
	}

	area, shown := a.dock.ChildArea(a.dock.Rest())
	if !shown {
		t.Fatal("the rest of the window is not drawn")
	}
	if _, err := a.root.HandleMouse(input.MouseEvent{
		Kind: input.MousePress, Button: input.MouseLeft,
		Col: area.X + 2, Row: area.Y + 2,
	}); err != nil {
		t.Fatalf("click: %v", err)
	}
	if a.dock.Focused() == ui.Widget(a.side) {
		t.Fatal("clicking the terminal left the keys on the panel")
	}

	// And typing reaches the shell again.
	sendKey(t, a, input1('x'))
	waitFor(t, a, "the shell to be sent what was typed", func() bool { return strings.Contains(a.shells[0].sentText(), "x") })
}

// A speed shown when a connection wakes up is the speed now, not the
// average over however long it was quiet.
func TestPanelSpeedIsNotAveragedOverTheQuietSpell(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	e := onlyPane(t, a)

	e.Meter.Moved(1<<20, 0, panelNow)
	panelText(a, panelNow)

	// A minute of nothing, with the panel drawing throughout.
	quiet := panelNow.Add(time.Minute)
	for at := panelNow.Add(time.Second); !at.After(quiet); at = at.Add(time.Second) {
		panelText(a, at)
	}

	// And then a megabyte in one second.
	e.Meter.Moved(1<<20, 0, quiet.Add(time.Second))
	panelText(a, quiet.Add(time.Second))

	rate := a.rates[e]
	if rate == nil {
		t.Fatal("the row has no rate")
	}
	past := rate.Past()
	got := past[len(past)-1]
	if want := uint64(1 << 20); got < want*9/10 || got > want*11/10 {
		t.Fatalf("the last second is %d bytes, want about %d: the quiet spell was averaged in",
			got, want)
	}
}

// The sidebar has a ground of its own, shading down the list, so it
// reads as part of the window's frame rather than as one more thing
// running in it.
func TestThePanelHasItsOwnGround(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)

	if a.panel.Style.BG == a.colours.BG {
		t.Fatal("the sidebar is the same colour as everything else")
	}
	if a.panel.Style.BGEnd == a.panel.Style.BG {
		t.Fatal("the sidebar does not shade at all")
	}

	// And what it draws really is two colours, top and bottom.
	a.refreshPanel(panelNow)
	at := windowCell(a)
	paint(a)
	area, shown := sideArea(a)
	if !shown {
		t.Fatal("the sidebar is not on screen")
	}
	top := at(area.X, area.Y)
	// The row above the last one: the last is the pinned line, which is
	// painted in one colour of its own and says nothing about whether
	// the list above it shades at all.
	bottom := at(area.X, area.Y+area.Rows-2)
	if top.BG == bottom.BG {
		t.Fatalf("the sidebar is one flat colour: %v", top.BG)
	}
}

// sidebarRow reads the pinned row at the bottom of the sidebar.
func sidebarRow(t *testing.T, a *testApp) string {
	t.Helper()
	a.refreshPanel(panelNow)
	at := windowCell(a)
	paint(a)
	area, shown := sideArea(a)
	if !shown {
		t.Fatal("the sidebar is not on screen")
	}
	var b strings.Builder
	for x := area.X; x < area.X+area.Cols; x++ {
		c := at(x, area.Y+area.Rows-1)
		if c.Rune == 0 {
			b.WriteByte(' ')
			continue
		}
		b.WriteRune(c.Rune)
	}
	return strings.TrimSpace(b.String())
}

// The pinned row is written afresh every frame, so it is still there
// after the window changes height.
//
// A row that remembered what it drew and skipped an unchanged frame
// would be left behind at the height it was drawn at, and the row it
// belongs on would stay blank.
func TestThePinnedRowSurvivesAResize(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	if got := sidebarRow(t, a); !strings.Contains(got, "Connect") {
		t.Fatalf("the pinned row reads %q", got)
	}

	a.lastSize = [2]int{80, 40}
	a.g = grid.New(80, 40, a.colours.FG, a.colours.BG)
	a.root.Layout(ui.Rect{Cols: 80, Rows: 40})
	if got := sidebarRow(t, a); !strings.Contains(got, "Connect") {
		t.Fatalf("after growing the window the pinned row reads %q", got)
	}
}

// And after something else has written over it, which is what the panes
// do while the sidebar is hidden.
func TestThePinnedRowSurvivesBeingPaintedOver(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	sidebarRow(t, a)

	area, shown := sideArea(a)
	if !shown {
		t.Fatal("the sidebar is not on screen")
	}
	for x := area.X; x < area.X+area.Cols; x++ {
		a.sideRegion.g.View().Set(x, area.Rows-1,
			grid.Cell{Rune: 'x', FG: a.colours.FG, BG: a.colours.BG, Width: 1})
	}
	if got := sidebarRow(t, a); !strings.Contains(got, "Connect") {
		t.Fatalf("the pinned row reads %q after something wrote over it", got)
	}
}

// A press on the pinned row goes through the command registry, so a
// failure is shown rather than logged where nobody will look.
func TestThePinnedRowGoesThroughTheCommands(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	paint(a)

	var ran int
	if !a.root.Commands.Unregister("server.connect") {
		t.Fatal("server.connect is not a command")
	}
	a.root.Commands.MustRegister(a.reporting(ui.Command{
		ID: "server.connect", Title: "Connect to a server",
		Run: func() error { ran++; return errors.New("nothing to connect to") },
	}))

	area, _ := sideArea(a)
	took, err := a.root.HandleMouse(input.MouseEvent{
		Kind: input.MousePress, Button: input.MouseLeft,
		Col: area.X + 2, Row: area.Y + area.Rows - 1,
	})
	if !took || err != nil {
		t.Fatalf("took %v, err %v", took, err)
	}
	if ran != 1 {
		t.Fatalf("the row ran the command %d times", ran)
	}
	n, ok := a.root.Modal().(*ui.Notice)
	if !ok {
		t.Fatalf("the failure showed %T, want a dialog", a.root.Modal())
	}
	if !strings.Contains(n.Message(), "nothing to connect to") {
		t.Fatalf("the dialog says %q", n.Message())
	}
}

// The wheel scrolls the list wherever the pointer is, including the
// bottom row: that is where somebody scrolling to the end of a long
// list will have put it.
func TestTheWheelWorksOverThePinnedRow(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	var rows []ui.ListRow
	for i := 0; i < 60; i++ {
		rows = append(rows, ui.ListRow{Text: "row", Key: i})
	}
	a.panel.SetRows(rows)
	paint(a)

	area, _ := sideArea(a)
	was := a.panel.RowTop(0)
	took, err := a.side.HandleMouse(input.MouseEvent{
		Kind: input.MousePress, Button: input.MouseWheelDown,
		Col: 1, Row: area.Rows - 1,
	})
	if !took || err != nil {
		t.Fatalf("took %v, err %v", took, err)
	}
	if got := a.panel.RowTop(0); got == was {
		t.Fatalf("the wheel over the bottom row did not scroll: still at %d", got)
	}
}

// The pinned row is clicked where it was drawn.
//
// A widget is given a view that may be shorter than the size it was laid
// out for, and a row measured against the promise rather than against
// the view is a row nobody can press.
func TestThePinnedRowIsClickedWhereItIsDrawn(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	var pressed int
	a.side.Press = func() error { pressed++; return nil }

	a.side.Layout(ui.Size{Cols: panelWidth, Rows: 24})
	g := grid.New(panelWidth, 10, a.colours.FG, a.colours.BG)
	a.side.Draw(g.View())

	if _, err := a.side.HandleMouse(input.MouseEvent{
		Kind: input.MousePress, Button: input.MouseLeft, Col: 2, Row: 9,
	}); err != nil {
		t.Fatalf("the press failed: %v", err)
	}
	if pressed != 1 {
		t.Fatalf("a press on the bottom row of the view ran it %d times", pressed)
	}
	if _, err := a.side.HandleMouse(input.MouseEvent{
		Kind: input.MousePress, Button: input.MouseLeft, Col: 2, Row: 23,
	}); err != nil {
		t.Fatalf("the press failed: %v", err)
	}
	if pressed != 1 {
		t.Fatal("a press below the view ran the pinned row")
	}
}

// headerRows returns the machine names the sidebar is showing, in order,
// and whether each carries a plus.
func headerRows(a *testApp, now time.Time) []string {
	a.refreshPanel(now)
	var out []string
	for _, row := range a.panel.Rows() {
		if !row.Header {
			continue
		}
		name := strings.TrimSpace(row.Text)
		if row.Button != 0 {
			name += " +"
		}
		out = append(out, name)
	}
	return out
}

// A saved server is on the sidebar before anything is connected to it,
// with the plus that opens the connection.
//
// It is the only way in: a machine nothing has reached yet has no rows
// of its own, so without its name on the list there is nothing to click.
func TestASavedServerIsOnTheSidebarWithNothingConnected(t *testing.T) {
	s := sshtest.New(t)
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	saveHost(t, a, "margit", s, "")
	saveHost(t, a, "web1", s, "")

	got := headerRows(a, panelNow)
	want := []string{"Local +", "margit +", "web1 +"}
	if len(got) != len(want) {
		t.Fatalf("the sidebar shows %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("the sidebar shows %v, want %v", got, want)
		}
	}
	if a.machines.count() != 0 {
		t.Fatalf("%d machines are connected, and none should be", a.machines.count())
	}
}

// The connection itself has no row. The machine is the heading above its
// rows, so one for the connection as well says the same thing twice.
func TestTheConnectionItselfHasNoRow(t *testing.T) {
	s := sshtest.New(t)
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	a.connect(serverConfig(t, s))
	waitForPanes(t, a, 2)
	host := serverConfig(t, s).Target()

	// The registry still holds it: it is what closing the machine acts
	// on, and what says the machine is there at all.
	if serverRow(t, a, host) == nil {
		t.Fatal("the connection is not in the registry")
	}
	for _, row := range panelText(a, panelNow) {
		if strings.Contains(row, "Server") {
			t.Fatalf("the sidebar shows %q", row)
		}
	}
	// Two rows under the machine's name would be the connection and the
	// terminal; there is one.
	var under int
	var seen bool
	for _, row := range a.panel.Rows() {
		if row.Header {
			seen = strings.TrimSpace(row.Text) == host
			continue
		}
		if seen {
			under++
		}
	}
	if under != 1 {
		t.Fatalf("%d rows under %s, want the terminal alone", under, host)
	}
}

// The heading carries the dot the connection's own row used to, so a
// machine with nothing open on it still says whether it is connected.
func TestTheHeadingSaysWhetherTheMachineIsConnected(t *testing.T) {
	s := sshtest.New(t)
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	pinServers(t, a, s)
	saveHost(t, a, "margit", s, "")

	a.refreshPanel(panelNow)
	for _, row := range a.panel.Rows() {
		if !row.Header || strings.TrimSpace(row.Text) != "margit" {
			continue
		}
		// A blank rather than nothing: the dot's column is kept, so the
		// name does not shift sideways when something connects.
		if row.Mark != ' ' {
			t.Fatalf("an unconnected machine is marked %q", row.Mark)
		}
	}

	if err := a.connectSaved("margit"); err != nil {
		t.Fatalf("connectSaved: %v", err)
	}
	waitForPanes(t, a, 2)

	a.refreshPanel(panelNow)
	var found bool
	for _, row := range a.panel.Rows() {
		if !row.Header || strings.TrimSpace(row.Text) != "margit" {
			continue
		}
		found = true
		if row.Mark == 0 {
			t.Fatal("a connected machine has no dot")
		}
		if row.MarkFG != a.colours.ANSI[2] {
			t.Fatalf("the dot is %v, want the green of something that is there", row.MarkFG)
		}
	}
	if !found {
		t.Fatalf("no heading for margit: %v", headerRows(a, panelNow))
	}

	// And it is painted, not only recorded: a heading with no room in
	// front of it has nowhere to put a dot, and the row would come out
	// looking exactly like an unconnected one.
	at := windowCell(a)
	paint(a)
	area, shown := sideArea(a)
	if !shown {
		t.Fatal("the sidebar is not on screen")
	}
	var painted bool
	for y := area.Y; y < area.Y+area.Rows; y++ {
		if strings.Contains(sidebarText(a, y, area), "margit") &&
			at(area.X, y).Rune == dot {
			painted = true
		}
	}
	if !painted {
		t.Fatal("the heading's dot was not drawn")
	}
}

// sidebarText reads one row of the sidebar back.
func sidebarText(a *testApp, y int, area ui.Rect) string {
	at := windowCell(a)
	var b strings.Builder
	for x := area.X; x < area.X+area.Cols; x++ {
		c := at(x, y)
		if c.Rune == 0 {
			b.WriteByte(' ')
			continue
		}
		b.WriteRune(c.Rune)
	}
	return b.String()
}

// A connection says what it is with a little picture rather than with
// the word for what kind it is: the word was the widest thing on the row
// and the same on every one of them.
func TestARowSaysWhatItIsWithAnIcon(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)

	if got := rowIcon(a, 1); got != grid.Icon(grid.IconTerminal) {
		t.Fatalf("the row carries %v, want a terminal", got)
	}
	got := panelText(a, panelNow)[1]
	for _, word := range []string{"Terminal", "Files", "Tunnel", "Command"} {
		if strings.Contains(got, word) {
			t.Fatalf("the row is %q, which still names the kind", got)
		}
	}
	// And the picture is not a character in the text, so nothing copies
	// it out or measures the row by it.
	if strings.TrimSpace(got) != "" {
		t.Fatalf("the row reads %q, and its shell has not named itself yet", got)
	}
}

// rowIcon is the picture on one row of the panel.
func rowIcon(a *testApp, at int) grid.Art {
	a.refreshPanel(panelNow)
	rows := a.panel.Rows()
	if at < 0 || at >= len(rows) {
		return grid.Art{}
	}
	return rows[at].Icon
}

// The bar follows whatever the stage is showing, so the sidebar is the
// list of what is open and says which one is in front.
func TestTheBarFollowsWhatTheStageShows(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	first := onlyPane(t, a)

	a.refreshPanel(panelNow)
	if got, _ := a.panel.Selected(); got.Key != any(first) {
		t.Fatal("the bar is not on the pane the window opened with")
	}

	if err := a.openPane(); err != nil {
		t.Fatalf("openPane: %v", err)
	}
	next := a.panes[a.focusedTerminal()]
	a.refreshPanel(panelNow)
	if got, _ := a.panel.Selected(); got.Key != any(next) {
		t.Fatal("the bar did not follow the tab that was opened")
	}

	// And in between it is the user's: moving it does not snap back.
	a.panel.Move(-1)
	was, _ := a.panel.Selected()
	a.refreshPanel(panelNow)
	if got, _ := a.panel.Selected(); got.Key != was.Key {
		t.Fatal("the bar snapped back to the pane in front")
	}
}

// A busy row shows the shape of the last few seconds, which is the
// answer to "is this going" that one speed does not give.
func TestABusyRowShowsItsRun(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	e := onlyPane(t, a)

	// Nothing has moved, so there is no shape to show. A run of nothing
	// but zeroes would draw a flat line under every quiet row.
	at := panelNow
	for i := 0; i < meter.Samples+2; i++ {
		at = at.Add(meter.RateWindow)
		a.refreshPanel(at)
	}
	if got := rowArt(a, e); got.Kind != grid.ArtNone {
		t.Fatalf("a row that has moved nothing for %d seconds carries %v",
			meter.Samples, got)
	}

	// A run of seconds, each busier than the last.
	for i := 1; i <= 4; i++ {
		e.Meter.Moved(i*1000, 0, at)
		at = at.Add(meter.RateWindow)
		a.refreshPanel(at)
	}

	art := rowArt(a, e)
	if art.Kind != grid.ArtGraph {
		t.Fatalf("a busy row carries %v, want a graph", art)
	}
	// The run got faster, so the bars do too, and the last is full.
	var last, full int
	for i := 0; i < grid.ArtGraphBars; i++ {
		if h := art.Bar(i); h > 0 {
			if h < last {
				t.Fatalf("the bars go %v, want them rising", barsOf(art))
			}
			last = h
		}
	}
	full = art.Bar(grid.ArtGraphBars - 1)
	if full != grid.ArtGraphMax {
		t.Fatalf("the busiest second is %d, want the full height: %v", full, barsOf(art))
	}
}

// rowArt returns the art on the row for one connection.
func rowArt(a *testApp, e *conns.Entry) grid.Art {
	for _, row := range a.panel.Rows() {
		if row.Key == any(e) {
			return row.Art
		}
	}
	return grid.Art{}
}

// barsOf reads a graph back as heights, for a failure worth reading.
func barsOf(art grid.Art) []int {
	out := make([]int, grid.ArtGraphBars)
	for i := range out {
		out[i] = art.Bar(i)
	}
	return out
}

// The graph is drawn in a cell of its own, before the note, so the two
// do not land on top of one another.
func TestTheGraphAndTheSpeedBothFit(t *testing.T) {
	l := ui.NewList()
	l.Style = a4Style()
	l.SetRows([]ui.ListRow{{
		Text: "one", Note: "12 kB/s", Art: grid.Graph([]int{1, 2, 3}), Key: 1,
	}})
	l.Layout(ui.Size{Cols: 40, Rows: 4})
	g := grid.New(40, 4, color.RGBA{}, color.RGBA{})
	l.Draw(g.View())

	var at = -1
	for x := 0; x < 40; x++ {
		if g.At(x, 0).Art.Kind == grid.ArtGraph {
			at = x
		}
	}
	if at < 0 {
		t.Fatal("the graph is not drawn at all")
	}
	var note string
	for x := 0; x < 40; x++ {
		c := g.At(x, 0)
		if c.Rune == 0 {
			note += " "
			continue
		}
		note += string(c.Rune)
	}
	if !strings.Contains(note, "12 kB/s") {
		t.Fatalf("the row reads %q, want the speed as well", note)
	}
	// The graph's own cell carries no letter.
	if got := g.At(at, 0).Rune; got != ' ' && got != 0 {
		t.Fatalf("the graph's cell also holds %q", got)
	}
}

// a4Style colours a list for a test that only cares about what is where.
func a4Style() ui.ListStyle {
	white := color.RGBA{R: 255, G: 255, B: 255, A: 255}
	return ui.ListStyle{FG: white, BG: color.RGBA{A: 255}, NoteFG: white}
}

// Each kind of connection gets its own picture, so a row says what it is
// without spending words on it.
func TestEachKindHasItsOwnIcon(t *testing.T) {
	for kind, want := range map[conns.Kind]grid.Art{
		conns.Terminal: grid.Icon(grid.IconTerminal),
		conns.Command:  grid.Icon(grid.IconCommand),
		conns.Files:    grid.Icon(grid.IconFiles),
		conns.Tunnel:   grid.Icon(grid.IconTunnel),
	} {
		if got := icon(kind); got != want {
			t.Errorf("%v carries %v, want %v", kind, got, want)
		}
	}
}

// The picture is drawn on the row, not only recorded: a cell of the
// sidebar carries it.
func TestTheIconIsDrawnOnTheRow(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	a.refreshPanel(panelNow)
	at := windowCell(a)
	paint(a)

	area, shown := sideArea(a)
	if !shown {
		t.Fatal("the sidebar is not on screen")
	}
	var found bool
	for y := area.Y; y < area.Y+area.Rows; y++ {
		for x := area.X; x < area.X+area.Cols; x++ {
			if at(x, y).Art == grid.Icon(grid.IconTerminal) {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("no cell of the sidebar carries the terminal picture")
	}
}
