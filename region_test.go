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
	if cols != area.Cols || rows != area.Rows {
		t.Errorf("its grid is %dx%d, want the %dx%d it was given", cols, rows, area.Cols, area.Rows)
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
