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

// The window has a margin down each side and the menu bar has room
// above and below it, and nothing in the middle of a row is padded.
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
	if got := padded(a.g); len(got) != 2 || got[0] != 0 || got[1] != 59 {
		t.Errorf("padded columns are %v, want only the two edges", got)
	}
}

// No column in the middle of a row is padded, whether the sidebar is
// open or shut.
//
// A column with room after it splits every line of text that crosses it,
// and the menu bar, a menu, a dialog and the palette all cross the width
// of the window. The window used to leave half a cell after the
// sidebar's last column, and the gap ran down the inside of anything
// wide enough to reach it.
func TestNothingInTheMiddleOfARowIsPadded(t *testing.T) {
	a := newTestApp(t, 60, 20)
	withPanel(t, a)

	for _, open := range []bool{true, false} {
		a.dock.ShowPanel(open)
		a.padGrid(a.g)

		for _, at := range padded(a.g) {
			if at != 0 && at != 59 {
				t.Errorf("with the sidebar open=%v, column %d has %+v and is not an edge",
					open, at, a.g.ColPad(at))
			}
		}
	}
}

// The room the padding needs is the same whatever the layout does.
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
// with no cell to paint it, so every quarter the padding asks for is
// placed, whatever the layout did with the sidebar.
func TestEveryQuarterSetAsideIsPlaced(t *testing.T) {
	a := newTestApp(t, 60, 20)
	withPanel(t, a)
	a.root.Layout(ui.Rect{Cols: 14, Rows: 20})
	a.g.Resize(14, 20)
	a.padGrid(a.g)

	want, _ := a.padsWanted()
	got := 0
	for _, p := range colPads(a.g) {
		got += int(p.Before) + int(p.After)
	}
	if got != want {
		t.Errorf("the grid was padded by %d quarters, want the %d set aside", got, want)
	}
	// Both margins the same, so the window is not lopsided.
	left, right := a.g.ColPad(0).Before, a.g.ColPad(13).After
	if left != right {
		t.Errorf("the margins are %d and %d quarters, want them even", left, right)
	}
}

// The table holds every pad it is given. The room for each one is set
// aside before the grid is measured, so one quietly dropped would be a
// strip of the window with no cell to paint it.
func TestThePadTableHoldsWhateverItIsGiven(t *testing.T) {
	var table padTable
	for i := range 9 {
		table.add(i, grid.Pad{Before: 1})
	}
	table.add(3, grid.Pad{After: 2})

	got := map[int]grid.Pad{}
	table.apply(9, nil, func(i int, p grid.Pad) { got[i] = p })

	if len(got) != 9 {
		t.Fatalf("%d of 9 pads were written", len(got))
	}
	if got[8] != (grid.Pad{Before: 1}) {
		t.Errorf("the ninth pad is %+v, want it kept", got[8])
	}
	if got[3] != (grid.Pad{Before: 1, After: 2}) {
		t.Errorf("two pads on one column came to %+v", got[3])
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

	// And after the window changes size, which moves the column the
	// right-hand margin is on and drops the padding past the new edge.
	cw, ch := a.renderer.CellSize()
	a.resizeTo(40*cw, 15*ch)

	cols, _ := a.g.Size()
	for x := range cols {
		if got, want := a.modals[0].g.ColPad(x), a.g.ColPad(x); got != want {
			t.Fatalf("after a resize, column %d has %+v on the dialog and %+v on the window",
				x, got, want)
		}
	}
	if gotCols, _ := a.modals[0].g.Size(); gotCols != cols {
		t.Errorf("the dialog's grid is %d columns and the window's is %d", gotCols, cols)
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

	for range 3 {
		a.padGrid(a.g)
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
