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
)

// padsWanted is how much room the window's padding needs, in quarters
// of a cell each way.
func (a *app) padsWanted() (padX, padY int) {
	return 2 * edgePad, 2 * barPad
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
// and room above and below the menu bar.
//
// Both margins are at the very edge of the grid. Nothing is padded in
// the middle of a row: a column with room after it splits every line of
// text that crosses it, and the menu bar, a menu, a dialog and the
// palette all cross the width of the window.
func (a *app) padGrid(g *grid.Grid) {
	cols, rows := g.Size()
	if cols <= 0 {
		return
	}
	// The window's own tables rather than two of its own, because a
	// table holds a slice into itself and one built here would go on the
	// heap every frame.
	a.sidePads.reset()
	a.sidePads.add(0, grid.Pad{Before: edgePad})
	a.sidePads.add(cols-1, grid.Pad{After: edgePad})
	a.sidePads.applyCols(g, cols)

	if rows > 0 {
		a.barPads.reset()
		a.barPads.add(0, grid.Pad{Before: barPad, After: barPad})
		a.barPads.applyRows(g, rows)
	}
}

// padEntry is padding asked for on one column or row.
type padEntry struct {
	at  int
	pad grid.Pad
}

// padTable is the padding one grid is to have, gathered before any of
// it is written.
//
// It holds whatever it is given. The room for every pad is set aside
// before the grid is measured, so one the table quietly dropped would
// be a strip of the window with no cell to paint it. The array is the
// usual case -- the window's two edges, the sidebar and the menu bar,
// and two of those can land on one column -- and it grows past that
// rather than losing anything.
type padTable struct {
	room [4]padEntry
	list []padEntry
}

// reset empties the table, keeping the room it has.
func (t *padTable) reset() { t.list = t.room[:0] }

// add asks for padding on a column or row, adding to whatever is
// already asked for there.
func (t *padTable) add(i int, p grid.Pad) {
	if t.list == nil {
		t.list = t.room[:0]
	}
	for k := range t.list {
		if t.list[k].at == i {
			t.list[k].pad.Before += p.Before
			t.list[k].pad.After += p.After
			return
		}
	}
	t.list = append(t.list, padEntry{at: i, pad: p})
}

// has reports whether the table asks for anything on a column or row.
func (t *padTable) has(i int) bool {
	for _, e := range t.list {
		if e.at == i {
			return true
		}
	}
	return false
}

// applyCols writes the table onto a grid's columns, clearing whatever is
// padded now and is not in it.
//
// The padding the grid holds may stop short of n, and its length is read
// once because setting one can grow it.
func (t *padTable) applyCols(g *grid.Grid, n int) {
	for i, have := 0, len(g.ColPads()); i < have && i < n; i++ {
		if !t.has(i) {
			g.SetColPad(i, grid.Pad{})
		}
	}
	for _, e := range t.list {
		g.SetColPad(e.at, e.pad)
	}
}

// applyRows is applyCols for a grid's rows.
//
// Written out rather than shared, because the two setters are methods
// and passing one as a function value costs an allocation a frame.
func (t *padTable) applyRows(g *grid.Grid, n int) {
	for i, have := 0, len(g.RowPads()); i < have && i < n; i++ {
		if !t.has(i) {
			g.SetRowPad(i, grid.Pad{})
		}
	}
	for _, e := range t.list {
		g.SetRowPad(e.at, e.pad)
	}
}
