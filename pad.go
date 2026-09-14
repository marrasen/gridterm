package main

import (
	"github.com/marrasen/gridterm/grid"
)

// What the window's furniture asks for, in quarters of a character.
//
// A grid is not quite a grid any more: a column or a row can have space
// around it, so the window need not look like graph paper to be drawn
// on one.
const (
	// edgePad is the margin down the left and the right of the window.
	edgePad = 2

	// barPad is the room above and below the menu bar's titles.
	barPad = 1

	// sidePad is the gap between the sidebar and the divider beside it.
	sidePad = 2
)

// padsWanted is how much room the window's padding needs, in quarters
// of a cell each way.
//
// It follows nothing but whether the sidebar is meant to be open. The
// grid size is worked out from it, and the sidebar is dropped when the
// window is too narrow for one, so a total that followed the layout
// could take away the column that made the sidebar fit, put it back the
// next frame, and flicker between the two for as long as the window
// stayed that width.
func (a *app) padsWanted() (padX, padY int) {
	padX = 2 * edgePad
	if a.dock != nil && !a.dock.Collapsed {
		padX += sidePad
	}
	return padX, 2 * barPad
}

// applyPads places the window's padding, and works the grid size out
// again when the room that padding needs has changed.
func (a *app) applyPads() {
	padX, padY := a.padsWanted()
	if a.lastPad != [2]int{padX, padY} && a.lastPixels != [2]int{} {
		a.resizeTo(a.lastPixels[0], a.lastPixels[1])
	}
	// Placed again whatever the resize decided: a window whose cell
	// count did not change still has the padding somewhere else, on the
	// sidebar's last column rather than the window's.
	a.padGrids()
}

// padGrids gives the window's grid and every dialog's the same padding.
//
// Every grid gets the same, because a dialog is drawn on its own grid
// over the window's: two that did not agree on where row five sits
// would put the dialog a few pixels off the text it covers.
func (a *app) padGrids() {
	a.padGrid(a.g)
	for _, m := range a.modals {
		a.padGrid(m.g)
	}
}

// padGrid gives one grid the window's padding: a margin down each side,
// room above and below the menu bar, and a gap between the sidebar and
// the divider.
//
// The sidebar's gap goes on the last of its columns when the sidebar is
// drawn, and on the window's right margin when it is not. It is counted
// into the grid size either way, so it is placed either way: room set
// aside and then left unused would leave a strip of the window with no
// cell to paint it.
func (a *app) padGrid(g *grid.Grid) {
	cols, rows := g.Size()
	if cols <= 0 {
		return
	}
	var want padTable
	want.add(0, grid.Pad{Before: edgePad})
	want.add(cols-1, grid.Pad{After: edgePad})
	if at, ok := a.sidebarEdge(cols); ok {
		want.add(at, grid.Pad{After: sidePad})
	}
	want.apply(cols, g.ColPads(), g.SetColPad)

	if rows > 0 {
		var barRow padTable
		barRow.add(0, grid.Pad{Before: barPad, After: barPad})
		barRow.apply(rows, g.RowPads(), g.SetRowPad)
	}
}

// sidebarEdge is the column the gap beside the sidebar goes on, and
// whether there is a sidebar to put one beside.
func (a *app) sidebarEdge(cols int) (int, bool) {
	if a.dock == nil || a.dock.Collapsed || a.side == nil {
		return 0, false
	}
	area, ok := a.dock.ChildArea(a.side)
	if !ok || area.Cols <= 0 {
		// The window is too narrow for a sidebar, so the room set aside
		// for the gap widens the right-hand margin instead.
		return cols - 1, true
	}
	return min(area.X+area.Cols-1, cols-1), true
}

// padTable is the padding one grid is to have, gathered before any of
// it is written. Small and fixed: the window pads its two edges, the
// sidebar and the menu bar, and two of those can land on one column.
type padTable struct {
	at   [4]int
	pads [4]grid.Pad
	n    int
}

// add asks for padding on a column or row, adding to whatever is
// already asked for there.
func (t *padTable) add(i int, p grid.Pad) {
	for k := 0; k < t.n; k++ {
		if t.at[k] == i {
			t.pads[k].Before += p.Before
			t.pads[k].After += p.After
			return
		}
	}
	if t.n == len(t.at) {
		return
	}
	t.at[t.n], t.pads[t.n] = i, p
	t.n++
}

// has reports whether the table asks for anything on a column or row.
func (t *padTable) has(i int) bool {
	for k := 0; k < t.n; k++ {
		if t.at[k] == i {
			return true
		}
	}
	return false
}

// apply writes the table, clearing whatever is padded now and is not in
// it.
//
// have is the table as the grid holds it, which may stop short of n.
// Its length is read once, because set may grow it.
func (t *padTable) apply(n int, have []grid.Pad, set func(int, grid.Pad)) {
	for i := 0; i < len(have) && i < n; i++ {
		if !t.has(i) {
			set(i, grid.Pad{})
		}
	}
	for k := 0; k < t.n; k++ {
		set(t.at[k], t.pads[k])
	}
}
