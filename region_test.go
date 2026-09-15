package main

import (
	"fmt"
	"testing"

	"github.com/marrasen/gridterm/input"

	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/ui"
)

// withRegion gives a test app the tree the window has -- the menu bar
// over the sidebar and the stage -- and the sidebar on a grid of its
// own, the way main wires it.
func withRegion(t *testing.T, a *testApp) {
	t.Helper()
	a.panel = a.newPanel()
	a.side = a.newSidebar()
	a.dock = ui.NewDock(panelWidth, a.side, a.root.Widget())
	a.sideRegion = newRegion(a.side, grid.New(0, 0, a.colours.FG, a.colours.BG), &a.sideGeo)
	a.dock.PanelElsewhere = true
	a.bar = a.newMenubar(a.dock)
	a.root.SetWidget(a.bar)
	cw, ch := a.renderer.CellSize()
	a.resizeTo(90*cw, 30*ch)
	a.applyPads()
	// Rows in the panel, so it has machine names to put room around and
	// the region has something to pay for.
	a.refreshPanel(panelNow)
	a.placeRegions()
}

// The sidebar sits under the menu bar, and its grid is the size the
// tree gave it.
func TestTheSidebarRegionIsWhereTheTreePutIt(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withRegion(t, a)

	area := a.sidebarArea()
	if area.X != 0 || area.Y != 1 {
		t.Errorf("the sidebar is at %d,%d, want the top left under the menu bar", area.X, area.Y)
	}
	if area.Cols != panelWidth {
		t.Errorf("the sidebar is %d columns, want %d", area.Cols, panelWidth)
	}
	cols, rows := a.sideRegion.g.Size()
	// Fewer rows than the box: some of its height pays for the room
	// around the machine names.
	if cols != area.Cols || rows > area.Rows || rows < 1 {
		t.Errorf("its grid is %dx%d, want %d columns and at most %d rows",
			cols, rows, area.Cols, area.Rows)
	}
}

// The region fills the hole the window left for it, to the pixel: a
// grid narrower than its columns would show a strip of whatever the
// window last painted there, beside the divider.
func TestTheSidebarRegionFillsItsHole(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withRegion(t, a)

	area := a.sidebarArea()
	left, width := a.geo.ColBox(area.X, area.X+area.Cols)
	top, height := a.geo.RowBox(area.Y, area.Y+area.Rows)

	if a.sideRegion.left != left || a.sideRegion.top != top {
		t.Errorf("the region is at %d,%d, want %d,%d",
			a.sideRegion.left, a.sideRegion.top, left, top)
	}
	if a.sideGeo.Width() != width || a.sideGeo.Height() != height {
		t.Errorf("the region's grid is %dx%d pixels, want the %dx%d hole",
			a.sideGeo.Width(), a.sideGeo.Height(), width, height)
	}
	if a.sideRegion.layer.X != left || a.sideRegion.layer.Y != top {
		t.Errorf("its layer is at %d,%d, want %d,%d",
			a.sideRegion.layer.X, a.sideRegion.layer.Y, left, top)
	}
}

// The window's own padding carries into the region, or the margin down
// the left of the window would stop where the sidebar starts.
func TestTheRegionKeepsTheWindowsPadding(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withRegion(t, a)

	area := a.sidebarArea()
	for x := 0; x < area.Cols; x++ {
		if got, want := a.sideRegion.g.ColPad(x), a.g.ColPad(area.X+x); got != want {
			t.Errorf("column %d has %+v in the region and %+v in the window", x, got, want)
		}
	}
}

// A click inside the sidebar is measured by the sidebar's rows.
//
// That is the whole point of a region: its rows are not the window's,
// so asking the window which row a pixel is on would name a row the
// sidebar is not drawing there.
func TestAClickInTheSidebarIsMeasuredByItsOwnRows(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withRegion(t, a)
	area := a.sidebarArea()

	// Room above the region's third row, which moves every row under it
	// down without moving the window's.
	a.sideRegion.g.SetRowPad(2, grid.Pad{Before: 2})
	a.sideRegion.measure(&a.geo, a.renderer.Metrics(), &a.sideGeo)

	// The middle of the region's fifth row.
	top, height := a.sideGeo.RowBox(4, 5)
	px := a.sideRegion.left + 2
	py := a.sideRegion.top + top + height/2

	col, row := a.cellAt(px, py)
	if col != area.X || row != area.Y+4 {
		t.Errorf("the pixel is on cell %d,%d, want %d,%d", col, row, area.X, area.Y+4)
	}
	if _, was := a.geo.CellAt(px, py); was == row {
		t.Error("the window would have named the same row, so the test proves nothing")
	}
}

// A click outside the sidebar is measured by the window, and one in the
// menu bar above it is outside.
func TestAClickOutsideTheSidebarIsMeasuredByTheWindow(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withRegion(t, a)

	for _, at := range [][2]int{
		{a.sideRegion.left + 2, a.sideRegion.top - 1},                      // the menu bar
		{a.sideRegion.left + a.sideRegion.width + 2, a.sideRegion.top + 4}, // the terminal
	} {
		gotCol, gotRow := a.cellAt(at[0], at[1])
		wantCol, wantRow := a.geo.CellAt(at[0], at[1])
		if gotCol != wantCol || gotRow != wantRow {
			t.Errorf("the pixel %v is on cell %d,%d, want the window's %d,%d",
				at, gotCol, gotRow, wantCol, wantRow)
		}
	}
}

// A closed sidebar has no region, and clicks go to the window.
func TestAClosedSidebarHasNoRegion(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withRegion(t, a)
	was := [2]int{a.sideRegion.left + 2, a.sideRegion.top + 2}

	a.dock.ShowPanel(false)
	a.applyPads()
	a.placeRegions()

	if !a.sideRegion.layer.Hidden {
		t.Error("the region is still drawn with the sidebar closed")
	}
	if a.sideRegion.contains(was[0], was[1]) {
		t.Error("a pixel is still inside a region that is not there")
	}
	gotCol, gotRow := a.cellAt(was[0], was[1])
	wantCol, wantRow := a.geo.CellAt(was[0], was[1])
	if gotCol != wantCol || gotRow != wantRow {
		t.Errorf("the pixel is on cell %d,%d, want the window's %d,%d",
			gotCol, gotRow, wantCol, wantRow)
	}
}

// machineRows fills the panel with server names, each with a connection
// under it.
func machineRows(a *testApp, n int) {
	var rows []ui.ListRow
	for i := 0; i < n; i++ {
		rows = append(rows,
			ui.ListRow{Text: fmt.Sprintf("host%02d", i), Header: true, Key: [2]int{i, 0}},
			ui.ListRow{Text: "a shell", Key: [2]int{i, 1}})
	}
	a.panel.SetRows(rows)
	a.placeRegions()
}

// roomSpent is how much room the region put around its rows.
func roomSpent(a *testApp) int {
	_, rows := a.sideRegion.g.Size()
	spent := 0
	for y := 0; y < rows; y++ {
		p := a.sideRegion.g.RowPad(y)
		spent += int(p.Before) + int(p.After)
	}
	return spent
}

// The rows the region gives up are exactly the room it spends, whatever
// the sidebar holds.
//
// They were not. The room was counted from every heading the list held,
// including the ones scrolled out of sight, and only the headings on
// screen were given any; the rest piled up under the last row. With
// fourteen servers the sidebar lost a quarter of its rows and the
// "Connect to server" line at the foot of it became a band six cells
// tall.
func TestTheRegionGivesUpOnlyTheRowsItSpends(t *testing.T) {
	for _, n := range []int{1, 4, 6, 14, 20, 40} {
		a := newTestApp(t, 90, 30)
		withRegion(t, a)
		machineRows(a, n)

		box, ok := a.dock.ChildArea(a.side)
		if !ok {
			t.Fatal("the sidebar has no room")
		}
		_, rows := a.sideRegion.g.Size()
		if want := (box.Rows - rows) * grid.PadUnit; roomSpent(a) != want {
			t.Errorf("%d servers: gave up %d rows for %d quarters of room",
				n, box.Rows-rows, roomSpent(a))
		}
		// And no room was dumped on the last row, which is the pinned
		// one: room on it draws as a band of its own colour rather than
		// as a gap.
		if got := a.sideRegion.g.RowPad(rows - 1); !got.Empty() {
			t.Errorf("%d servers: the pinned row carries %+v", n, got)
		}
	}
}

// Every heading on screen gets its room, or the sidebar changes rhythm
// halfway down.
func TestEveryHeadingOnScreenGetsItsRoom(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withRegion(t, a)
	machineRows(a, 40)

	_, rows := a.sideRegion.g.Size()
	headings := 0
	for y := 0; y < rows; y++ {
		if !a.sideRegion.g.RowPad(y).Empty() {
			headings++
		}
	}
	// The rows alternate heading and connection, so half of them are
	// headings and every one of them is spaced.
	if want := rows / 2; headings != want {
		t.Errorf("%d of the %d headings on screen have room around them", headings, want)
	}
}

// The sidebar never paints past the box it was given, however little of
// it there is.
func TestTheRegionNeverOverflowsItsBox(t *testing.T) {
	for _, rows := range []int{3, 4, 5, 8, 12, 30} {
		a := newTestApp(t, 90, rows)
		withRegion(t, a)
		machineRows(a, 8)

		if over := a.sideGeo.Height() - a.sideRegion.height; over != 0 {
			t.Errorf("a %d row window: the sidebar is %d pixels out of its box", rows, over)
		}
	}
}

// Laying the tree out again does not move a scrolled sidebar.
//
// It did. The dock laid the panel out for the whole box and the region
// laid it out again for the rows it really has, and the list clamped
// its scroll to the larger count in between: one drag of the divider
// scrolled a sidebar at the end of its list back up by seven rows.
func TestRelayingOutTheTreeDoesNotMoveTheSidebar(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withRegion(t, a)
	machineRows(a, 40)
	for i := 0; i < 60; i++ {
		if _, err := a.panel.HandleKey(input.Event{Kind: input.KeyPress, Key: input.KeyDown}); err != nil {
			t.Fatalf("down: %v", err)
		}
	}
	a.placeRegions()
	was := a.panel.RowTop(a.panel.Rows()[len(a.panel.Rows())-1].Key)

	cols, rows := a.g.Size()
	a.root.Layout(ui.Rect{Cols: cols, Rows: rows})
	a.placeRegions()

	got := a.panel.RowTop(a.panel.Rows()[len(a.panel.Rows())-1].Key)
	if got != was {
		t.Errorf("the last row moved from %d to %d", was, got)
	}
}

// The region gives up rows to pay for the room around the ones it
// keeps, and its grid still fills its box to the pixel.
func TestTheRegionPaysForItsRoomInRows(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withRegion(t, a)
	area, ok := a.dock.ChildArea(a.side)
	if !ok {
		t.Fatal("the sidebar has no room")
	}

	rows := a.sideRegion.rect.Rows
	if rows >= area.Rows {
		t.Errorf("the region kept all %d of its rows, so it paid for no room", rows)
	}
	if got, _ := a.sideRegion.g.Size(); got != a.sideRegion.rect.Cols {
		t.Errorf("its grid is %d columns and its box is %d", got, a.sideRegion.rect.Cols)
	}
	if got := a.sideGeo.Height(); got != a.sideRegion.height {
		t.Errorf("its grid is %d pixels tall and its box is %d", got, a.sideRegion.height)
	}
}

// The room the region set aside is all spent. Room set aside and then
// left unused would be a strip at the foot of the sidebar with no cell
// to paint it.
func TestTheRegionSpendsTheRoomItSetAside(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withRegion(t, a)

	rows := a.sideRegion.rect.Rows
	spent := 0
	for y := 0; y < rows; y++ {
		p := a.sideRegion.g.RowPad(y)
		spent += int(p.Before) + int(p.After)
	}
	lost := 0
	if area, ok := a.dock.ChildArea(a.side); ok {
		lost = area.Rows - rows
	}
	if want := lost * grid.PadUnit; spent != want {
		t.Errorf("the region spent %d quarters of the %d rows it gave up", spent, want)
	}
}

// The machine names are the rows with room around them.
func TestTheServerNamesAreWhatGetsTheRoom(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withRegion(t, a)
	a.refreshPanel(panelNow)
	a.sideRegion.place(a.sidebarArea(), &a.geo, a.g.ColPads())

	rows := a.panel.Rows()
	if len(rows) == 0 {
		t.Fatal("the panel is empty, so there are no names to space")
	}
	headers := 0
	for y, row := range rows {
		if y >= a.sideRegion.rect.Rows {
			break
		}
		got := a.sideRegion.g.RowPad(y)
		if row.Header {
			headers++
			if got.Before == 0 && got.After == 0 {
				t.Errorf("the heading on row %d (%q) has no room around it", y, row.Text)
			}
			continue
		}
		// The last row carries whatever the headings did not use.
		if y != a.sideRegion.rect.Rows-1 && !got.Empty() {
			t.Errorf("row %d (%q) is not a heading but has %+v", y, row.Text, got)
		}
	}
	if headers == 0 {
		t.Fatal("no headings were drawn, so the test proves nothing")
	}
}

// A widget that asks for no room keeps every row its box has.
func TestARegionWithoutSpacingKeepsEveryRow(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withRegion(t, a)
	a.panel.Style.HeaderPad = grid.Pad{}

	area, _ := a.dock.ChildArea(a.side)
	a.sideRegion.place(a.sidebarArea(), &a.geo, a.g.ColPads())

	if a.sideRegion.rect.Rows != area.Rows {
		t.Errorf("the region has %d rows of the %d in its box", a.sideRegion.rect.Rows, area.Rows)
	}
	for y := 0; y < area.Rows; y++ {
		if got := a.sideRegion.g.RowPad(y); !got.Empty() {
			t.Errorf("row %d has %+v with nothing asking for room", y, got)
		}
	}
}

// A menu dropped from a sidebar row points at the row it belongs to.
//
// A menu is a dialog, drawn on the window's own grid with the window's
// row heights. The sidebar's rows are taller, so counting from the top
// of the sidebar in the window's rows put the menu half a cell out for
// the first machine and three cells out by the sixth: it opened over
// the machines above the one whose plus was clicked.
func TestAMenuFromTheSidebarPointsAtItsRow(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withRegion(t, a)
	machineRows(a, 6)

	for y := 0; y < a.sideRegion.rect.Rows; y++ {
		at, height := a.sideGeo.RowBox(y, y+1)
		middle := a.sideRegion.top + at + height/2

		row := a.windowRow(y)

		top, tall := a.geo.RowBox(row, row+1)
		if middle < top || middle >= top+tall {
			t.Fatalf("sidebar row %d is drawn at %d..%d and points at window row %d, which is %d..%d",
				y, at+a.sideRegion.top, at+a.sideRegion.top+height, row, top, top+tall)
		}
	}
}

// A click on a dialog over the sidebar is measured by the window.
//
// The dialog is drawn on the window's grid, so measuring it by the
// sidebar's taller rows would run the line above the one clicked.
func TestAClickOnADialogOverTheSidebarUsesTheWindow(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withRegion(t, a)
	withDialogs(t, a)
	machineRows(a, 6)

	// A pixel well down the sidebar, where the two disagree.
	at, height := a.sideGeo.RowBox(10, 11)
	px := a.sideRegion.left + 2
	py := a.sideRegion.top + at + height/2
	if _, byRegion := a.cellAt(px, py); true {
		_, byWindow := a.geo.CellAt(px, py)
		if byRegion == byWindow {
			t.Fatal("the two agree at this pixel, so the fixture no longer exercises the case")
		}
	}

	f := a.newConfirm("Really?", []string{"A question."})
	a.showForm(f, nil)

	gotCol, gotRow := a.cellAt(px, py)
	wantCol, wantRow := a.geo.CellAt(px, py)
	if gotCol != wantCol || gotRow != wantRow {
		t.Errorf("with a dialog up the pixel is cell %d,%d, want the window's %d,%d",
			gotCol, gotRow, wantCol, wantRow)
	}
}

// The region's cells land where the window's do.
//
// Its columns are the window's own, borrowed rather than measured
// again: a window is rarely a whole number of cells across, and the odd
// pixels are shared between its two edges. Laid out on its own the
// region would put that share somewhere else and its text would sit a
// few pixels off the text above it.
func TestTheRegionsCellsLineUpWithTheWindows(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withRegion(t, a)
	area := a.sideRegion.rect

	for x := 0; x < area.Cols; x++ {
		got := a.sideRegion.left + a.sideGeo.CellX(x)
		if want := a.geo.CellX(area.X + x); got != want {
			t.Fatalf("the region's column %d is drawn at %d and the window's at %d", x, got, want)
		}
	}
}

// A drag that began outside the sidebar keeps the window's rows. It
// belongs to whoever took the press, wherever the pointer has gone.
func TestADragFromOutsideKeepsTheWindowsRows(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withRegion(t, a)
	machineRows(a, 6)

	// A pixel well down the sidebar, where the two disagree.
	at, height := a.sideGeo.RowBox(10, 11)
	px := a.sideRegion.left + 2
	py := a.sideRegion.top + at + height/2
	_, byRegion := a.cellAt(px, py)
	_, byWindow := a.geo.CellAt(px, py)
	if byRegion == byWindow {
		t.Fatal("the two agree at this pixel, so the fixture no longer exercises the case")
	}

	// Press in the terminal beside the sidebar, which takes the pointer.
	col, row := a.geo.CellAt(a.sideRegion.left+a.sideRegion.width+20, py)
	if _, err := a.root.HandleMouse(input.MouseEvent{
		Kind: input.MousePress, Button: input.MouseLeft, Col: col, Row: row,
	}); err != nil {
		t.Fatalf("press: %v", err)
	}
	if a.root.Holding() == nil {
		t.Fatal("nothing took the press, so there is no drag to follow")
	}

	if _, got := a.cellAt(px, py); got != byWindow {
		t.Errorf("a drag over the sidebar reports row %d, want the window's %d", got, byWindow)
	}
}

// The window's own grid has nothing under the sidebar. Painting it
// there as well would draw the sidebar twice, once at row heights that
// are not its own.
func TestTheWindowsGridIsBlankUnderTheSidebar(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withRegion(t, a)
	machineRows(a, 6)
	a.root.Draw(a.g.View())

	area := a.sideRegion.rect
	blank := a.g.Blank()
	for y := area.Y; y < area.Y+area.Rows; y++ {
		for x := area.X; x < area.X+area.Cols; x++ {
			if got := a.g.At(x, y); !got.Equal(blank) {
				t.Fatalf("the window painted %q at %d,%d, under the sidebar", got.Rune, x, y)
			}
		}
	}
}

// The pinned row never gets a heading's room, however the list falls.
//
// The sidebar asks the list about its own rows, which stop one short of
// the box: the row pinned to the foot of it is the sidebar's, not the
// list's.
func TestThePinnedRowNeverGetsAHeadingsRoom(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withRegion(t, a)

	// Nothing but headings, so whatever row the pinned one lands on
	// would be one if the sidebar did not stop short.
	var rows []ui.ListRow
	for i := 0; i < 60; i++ {
		rows = append(rows, ui.ListRow{Text: "host", Header: true, Key: i})
	}
	a.panel.SetRows(rows)
	a.placeRegions()

	_, have := a.sideRegion.g.Size()
	if got := a.sideRegion.g.RowPad(have - 1); !got.Empty() {
		t.Errorf("the pinned row has %+v", got)
	}
}
