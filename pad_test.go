package main

import (
	"testing"

	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/ui"
)

// colPads returns the padding on a grid's columns, one entry per column
// so a test can say where it is rather than how long the table is.
func colPads(g *grid.Grid) []grid.Pad {
	cols, _ := g.Size()
	out := make([]grid.Pad, cols)
	for x := range out {
		out[x] = g.ColPad(x)
	}
	return out
}

// padded returns the columns with any padding on them.
func padded(g *grid.Grid) []int {
	var out []int
	for x, p := range colPads(g) {
		if !p.Empty() {
			out = append(out, x)
		}
	}
	return out
}

// The window has a margin down each side, the menu bar has room above
// and below it, and the sidebar has a gap before the divider.
func TestTheWindowPadsItsOwnFurniture(t *testing.T) {
	a := newTestApp(t, 60, 20)
	withPanel(t, a)
	a.padGrid(a.g)

	if got := a.g.ColPad(0); got.Before != edgePad {
		t.Errorf("column 0 has %+v, want a margin of %d", got, edgePad)
	}
	if got := a.g.ColPad(59); got.After != edgePad {
		t.Errorf("the last column has %+v, want a margin of %d", got, edgePad)
	}
	if got := a.g.RowPad(0); got != (grid.Pad{Before: barPad, After: barPad}) {
		t.Errorf("the menu bar's row has %+v", got)
	}
	area, ok := a.dock.ChildArea(a.side)
	if !ok {
		t.Fatal("the sidebar has no room, so there is nothing to pad")
	}
	edge := area.X + area.Cols - 1
	if got := a.g.ColPad(edge); got.After != sidePad {
		t.Errorf("the sidebar's last column %d has %+v, want a gap of %d",
			edge, got, sidePad)
	}
}

// Closing the sidebar takes its gap away and leaves the window's own
// margins where they were.
func TestClosingTheSidebarTakesItsGapAway(t *testing.T) {
	a := newTestApp(t, 60, 20)
	withPanel(t, a)
	a.padGrid(a.g)
	area, _ := a.dock.ChildArea(a.side)
	edge := area.X + area.Cols - 1

	a.dock.ShowPanel(false)
	a.padGrid(a.g)

	if got := a.g.ColPad(edge); !got.Empty() {
		t.Errorf("column %d still has %+v with the sidebar closed", edge, got)
	}
	if got := padded(a.g); len(got) != 2 || got[0] != 0 || got[1] != 59 {
		t.Errorf("padded columns are %v, want only the two edges", got)
	}
}

// The room the padding needs follows nothing but whether the sidebar is
// meant to be open.
//
// The grid size is worked out from it, and the sidebar is dropped when
// the window is too narrow for one. A total that followed the layout
// could take away the column that made the sidebar fit, put it back the
// next frame, and flicker between the two for as long as the window
// stayed that width.
func TestTheRoomPaddingNeedsDoesNotFollowTheLayout(t *testing.T) {
	a := newTestApp(t, 60, 20)
	withPanel(t, a)
	wideX, wideY := a.padsWanted()

	// Too narrow for a sidebar beside a terminal, so the dock drops it.
	a.root.Layout(ui.Rect{Cols: 14, Rows: 20})
	if _, ok := a.dock.ChildArea(a.side); ok {
		t.Fatal("the sidebar still has room at 14 columns")
	}
	gotX, gotY := a.padsWanted()

	if gotX != wideX || gotY != wideY {
		t.Errorf("padding needs %d,%d in a narrow window and %d,%d in a wide one",
			gotX, gotY, wideX, wideY)
	}
}

// Room set aside and then left unused would leave a strip of the window
// with no cell to paint it, so a sidebar with nowhere to go hands its
// gap to the window's right margin.
func TestASidebarWithNoRoomGivesItsGapToTheMargin(t *testing.T) {
	a := newTestApp(t, 60, 20)
	withPanel(t, a)
	a.root.Layout(ui.Rect{Cols: 14, Rows: 20})
	a.g.Resize(14, 20)
	a.padGrid(a.g)

	want := 2*edgePad + sidePad
	got := 0
	for _, p := range colPads(a.g) {
		got += int(p.Before) + int(p.After)
	}
	if got != want {
		t.Errorf("the grid was padded by %d quarters, want the %d set aside", got, want)
	}
}

// A dialog draws on its own grid over the window's. Two that did not
// agree on where a row sits would put the dialog a few pixels off the
// text it covers.
func TestADialogIsPaddedLikeTheWindow(t *testing.T) {
	a := newTestApp(t, 60, 20)
	withPanel(t, a)
	withDialogs(t, a)
	a.padGrids()

	f := a.newConfirm("Really?", []string{"A question."})
	a.showForm(f, nil)
	if len(a.modals) != 1 {
		t.Fatalf("%d dialogs are up, want one", len(a.modals))
	}

	for x, want := range colPads(a.g) {
		if got := a.modals[0].g.ColPad(x); got != want {
			t.Fatalf("column %d has %+v on the dialog and %+v on the window", x, got, want)
		}
	}
	if got, want := a.modals[0].g.RowPad(0), a.g.RowPad(0); got != want {
		t.Errorf("row 0 has %+v on the dialog and %+v on the window", got, want)
	}
}

// Writing the padding again writes nothing. It is done every frame, and
// a grid that redrew itself each time would never let an idle window go
// quiet.
func TestPaddingTheSameWayTwiceRedrawsNothing(t *testing.T) {
	a := newTestApp(t, 60, 20)
	withPanel(t, a)
	a.padGrid(a.g)
	a.g.ClearDirty()
	was := a.g.PadGeneration()

	for i := 0; i < 3; i++ {
		a.padGrid(a.g)
	}

	if a.g.PadGeneration() != was {
		t.Errorf("the generation moved to %d from %d", a.g.PadGeneration(), was)
	}
	if a.g.AnyDirty() {
		t.Error("padding that did not change dirtied the grid")
	}
}

// The window is measured in the cells that are left once the padding
// has had its room, or the last column falls off the right-hand edge.
func TestTheGridIsMeasuredInWhatThePaddingLeaves(t *testing.T) {
	a := newTestApp(t, 60, 20)
	withPanel(t, a)
	cw, ch := a.renderer.CellSize()

	a.resizeTo(100*cw, 40*ch)

	padX, padY := a.padsWanted()
	wantCols := (100*cw - padX*cw/grid.PadUnit) / cw
	wantRows := (40*ch - padY*ch/grid.PadUnit) / ch
	if cols, rows := a.g.Size(); cols != wantCols || rows != wantRows {
		t.Errorf("the grid is %dx%d cells, want %dx%d", cols, rows, wantCols, wantRows)
	}
}
