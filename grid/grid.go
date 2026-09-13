// Package grid holds the character grid: the model a terminal-style UI
// draws into. It knows nothing about GPUs, fonts or escape sequences, so
// it is testable without a display.
package grid

import "image/color"

// Attr is a bitfield of per-cell text attributes.
type Attr uint8

const (
	AttrBold Attr = 1 << iota
	AttrUnderline
	AttrReverse
)

// Cell is one character position.
type Cell struct {
	Rune rune
	FG   color.RGBA
	BG   color.RGBA
	Attr Attr
}

// Grid is a rectangular buffer of cells with per-row damage tracking.
// Damage lets the renderer redraw only the rows that changed, which is
// what keeps a mostly-idle screen cheap.
type Grid struct {
	cols, rows int
	cells      []Cell
	dirty      []bool
	allDirty   bool

	// DefaultFG and DefaultBG fill cells cleared by Clear and Resize.
	DefaultFG color.RGBA
	DefaultBG color.RGBA
}

// New returns a grid of the given size, filled with spaces.
func New(cols, rows int, fg, bg color.RGBA) *Grid {
	g := &Grid{DefaultFG: fg, DefaultBG: bg}
	g.Resize(cols, rows)
	return g
}

// Size returns the grid dimensions in cells.
func (g *Grid) Size() (cols, rows int) { return g.cols, g.rows }

// Resize changes the grid dimensions, preserving the top-left overlap.
// Growing fills the new area with blanks in the default colours.
func (g *Grid) Resize(cols, rows int) {
	if cols < 0 {
		cols = 0
	}
	if rows < 0 {
		rows = 0
	}
	if cols == g.cols && rows == g.rows {
		return
	}
	next := make([]Cell, cols*rows)
	blank := Cell{Rune: ' ', FG: g.DefaultFG, BG: g.DefaultBG}
	for i := range next {
		next[i] = blank
	}
	// Copy the overlapping region so a resize does not blank the screen.
	copyCols := min(cols, g.cols)
	copyRows := min(rows, g.rows)
	for y := 0; y < copyRows; y++ {
		copy(next[y*cols:y*cols+copyCols], g.cells[y*g.cols:y*g.cols+copyCols])
	}
	g.cells = next
	g.cols, g.rows = cols, rows
	g.dirty = make([]bool, rows)
	g.allDirty = true
}

// Clear fills the whole grid with blanks in the default colours.
func (g *Grid) Clear() {
	blank := Cell{Rune: ' ', FG: g.DefaultFG, BG: g.DefaultBG}
	for i := range g.cells {
		g.cells[i] = blank
	}
	g.allDirty = true
}

// At returns the cell at x,y. Out-of-range coordinates return a blank
// cell rather than panicking, so drawing code can be sloppy at edges.
func (g *Grid) At(x, y int) Cell {
	if !g.inBounds(x, y) {
		return Cell{Rune: ' ', FG: g.DefaultFG, BG: g.DefaultBG}
	}
	return g.cells[y*g.cols+x]
}

// Set writes a cell and marks its row dirty. Writing a cell identical to
// the one already there does not dirty the row: an idle repaint of
// unchanged content costs nothing.
func (g *Grid) Set(x, y int, c Cell) {
	if !g.inBounds(x, y) {
		return
	}
	i := y*g.cols + x
	if g.cells[i] == c {
		return
	}
	g.cells[i] = c
	g.dirty[y] = true
}

// SetString writes s starting at x,y in one style, stopping at the row
// end. It returns the column just past the last rune written.
func (g *Grid) SetString(x, y int, s string, fg, bg color.RGBA, attr Attr) int {
	for _, r := range s {
		if x >= g.cols {
			break
		}
		g.Set(x, y, Cell{Rune: r, FG: fg, BG: bg, Attr: attr})
		x++
	}
	return x
}

// ScrollUp moves every row up by n, discarding the top n rows and
// filling the bottom with blanks. This is the hot path in a terminal
// under load, so it is a slice copy rather than a per-cell loop.
func (g *Grid) ScrollUp(n int) {
	if n <= 0 || g.rows == 0 {
		return
	}
	if n >= g.rows {
		g.Clear()
		return
	}
	copy(g.cells, g.cells[n*g.cols:])
	blank := Cell{Rune: ' ', FG: g.DefaultFG, BG: g.DefaultBG}
	for i := (g.rows - n) * g.cols; i < len(g.cells); i++ {
		g.cells[i] = blank
	}
	g.allDirty = true
}

// BGRuns calls fn for each maximal horizontal run of cells in row y that
// share a background colour, as [x0,x1). AttrReverse swaps the cell's
// foreground and background, so it is resolved here rather than by the
// caller.
//
// A terminal row is usually one background colour end to end, so this
// normally collapses a whole row into a single rectangle.
func (g *Grid) BGRuns(y int, fn func(x0, x1 int, c color.RGBA)) {
	if y < 0 || y >= g.rows || g.cols == 0 {
		return
	}
	start := 0
	cur := g.bgOf(0, y)
	for x := 1; x < g.cols; x++ {
		c := g.bgOf(x, y)
		if c != cur {
			fn(start, x, cur)
			start, cur = x, c
		}
	}
	fn(start, g.cols, cur)
}

// bgOf returns the effective background of a cell, honouring AttrReverse.
func (g *Grid) bgOf(x, y int) color.RGBA {
	c := g.cells[y*g.cols+x]
	if c.Attr&AttrReverse != 0 {
		return c.FG
	}
	return c.BG
}

// FGOf returns the effective foreground of a cell, honouring AttrReverse.
func (g *Grid) FGOf(x, y int) color.RGBA {
	c := g.At(x, y)
	if c.Attr&AttrReverse != 0 {
		return c.BG
	}
	return c.FG
}

// RowDirty reports whether row y changed since the last ClearDirty.
func (g *Grid) RowDirty(y int) bool {
	if g.allDirty {
		return true
	}
	if y < 0 || y >= g.rows {
		return false
	}
	return g.dirty[y]
}

// AnyDirty reports whether anything changed since the last ClearDirty.
func (g *Grid) AnyDirty() bool {
	if g.allDirty {
		return true
	}
	for _, d := range g.dirty {
		if d {
			return true
		}
	}
	return false
}

// ClearDirty marks the whole grid clean. The renderer calls this after
// a successful frame.
func (g *Grid) ClearDirty() {
	g.allDirty = false
	for i := range g.dirty {
		g.dirty[i] = false
	}
}

// MarkAllDirty forces a full repaint on the next frame, for when
// something outside the cell contents changed (window resize, theme).
func (g *Grid) MarkAllDirty() { g.allDirty = true }

func (g *Grid) inBounds(x, y int) bool {
	return x >= 0 && y >= 0 && x < g.cols && y < g.rows
}
