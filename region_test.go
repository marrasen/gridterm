package main

import (
	"testing"

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
	a.dock.PanelDrawnElsewhere = true
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
