package grid

import (
	"image/color"

	"github.com/rivo/uniseg"
)

// View is a rectangular region of a Grid with its own origin. A widget
// draws through a view in its own coordinates, starting at 0,0, and the
// view offsets and clips every write, so the widget never learns where
// on the grid it sits.
//
// No write through a view changes a cell outside it, with one exception.
// A double-width character already straddling an edge has its outside
// half blanked when the half inside is overwritten, because a lead or a
// continuation left on its own draws as a stray glyph.
//
// The guarantee is about cells, not pixels. A glyph wider than its cell
// still paints over its neighbour; clipping that is the renderer's job.
//
// A view holds the region it was built with, so it is only valid until
// the grid is resized. Build views during layout, not once at startup. A
// stale view is safe: writes past the grid are dropped, but Size and the
// column SetString returns are the old numbers.
//
// The zero View draws nothing and is safe to use.
type View struct {
	g          *Grid
	x0, y0     int // the view's origin, in grid coordinates
	cols, rows int
}

// View returns a view covering the whole grid.
func (g *Grid) View() View {
	return View{g: g, cols: g.cols, rows: g.rows}
}

// Sub returns the part of v inside the given rectangle, in v's own
// coordinates. The rectangle is clipped to v, so a child can ask for
// more room than its parent has and still get a usable view.
func (v View) Sub(x, y, cols, rows int) View {
	x0, y0 := max(x, 0), max(y, 0)
	x1, y1 := clampEnd(x, cols, v.cols), clampEnd(y, rows, v.rows)
	if x1 <= x0 || y1 <= y0 {
		return View{g: v.g}
	}
	return View{
		g:    v.g,
		x0:   v.x0 + x0,
		y0:   v.y0 + y0,
		cols: x1 - x0,
		rows: y1 - y0,
	}
}

// clampEnd returns start+size capped at limit, without overflowing when
// a caller passes a huge size to mean "the rest of the row".
func clampEnd(start, size, limit int) int {
	if size <= 0 {
		return start
	}
	if start > limit-size {
		return limit
	}
	return start + size
}

// Size returns the view's dimensions in cells.
func (v View) Size() (cols, rows int) { return v.cols, v.rows }

// At returns the cell at x,y in view coordinates. A point outside the
// view returns a blank cell rather than panicking, matching Grid.At.
func (v View) At(x, y int) Cell {
	if !v.inBounds(x, y) {
		return v.blank()
	}
	return v.g.At(v.x0+x, v.y0+y)
}

// Set writes a cell at x,y in view coordinates, ignoring a point outside
// the view. A cell whose double-width partner would fall outside the
// view is written as single-width instead.
//
// Set is the low-level primitive and does not maintain the width
// invariant between a lead cell and its continuation. Callers writing
// double-width characters should use SetWide.
func (v View) Set(x, y int, c Cell) {
	if !v.inBounds(x, y) {
		return
	}
	// Left alone, a lead in the last column or a continuation in the
	// first would claim a partner outside the view, and the next repair
	// would blank a cell the neighbour owns.
	if (c.Width == 2 && x == v.cols-1) || (c.Width == 0 && x == 0) {
		c.Width = 1
	}
	v.g.Set(v.x0+x, v.y0+y, c)
}

// SetWide writes a cell that may occupy two columns, keeping the lead
// and continuation cells consistent. It reports the number of columns
// consumed, which is 0 when the cell does not fit.
//
// A double-width cell in the view's last column does not fit, so a wide
// character can never spill into whatever is drawn beside the view.
func (v View) SetWide(x, y int, c Cell) int {
	if !v.inBounds(x, y) {
		return 0
	}
	w := min(max(int(c.Width), 1), 2)
	if x+w > v.cols {
		return 0
	}
	return v.g.SetWide(v.x0+x, v.y0+y, c)
}

// SetString writes s starting at x,y in one style, stopping at the view
// edge. It returns the column just past the last cluster written.
//
// Text is split into grapheme clusters, so a base character and its
// combining marks share one cell, and double-width clusters take two.
func (v View) SetString(x, y int, s string, fg, bg color.RGBA, attr Attr) int {
	state := -1
	for len(s) > 0 {
		var cluster string
		var width int
		cluster, s, width, state = uniseg.FirstGraphemeClusterInString(s, state)
		if x >= v.cols {
			break
		}
		c := ClusterCell(cluster, width)
		c.FG, c.BG, c.Attr = fg, bg, attr
		n := v.SetWide(x, y, c)
		if n == 0 {
			// A double-width cluster with one column left. Blank the
			// column through SetWide, which repairs the other half of
			// anything already there, and report the row as full.
			v.SetWide(x, y, Cell{Rune: ' ', FG: fg, BG: bg, Attr: attr, Width: 1})
			return v.cols
		}
		x += n
	}
	return x
}

// Fill writes c to every cell of the view as a single-width cell.
func (v View) Fill(c Cell) {
	if v.g == nil || v.cols <= 0 || v.rows <= 0 {
		return
	}
	c.Width = 1
	c.Comb = nil
	v.repairEdges()
	for y := 0; y < v.rows; y++ {
		for x := 0; x < v.cols; x++ {
			v.g.Set(v.x0+x, v.y0+y, c)
		}
	}
}

// Clear fills the view with blanks in the grid's default colours.
func (v View) Clear() { v.Fill(v.blank()) }

// repairEdges blanks the half of a double-width character that lies just
// outside the view when the half inside is about to be overwritten. Fill
// writes cells directly, so nothing else would repair them.
func (v View) repairEdges() {
	last := v.x0 + v.cols - 1
	for y := v.y0; y < v.y0+v.rows; y++ {
		v.g.clearWideAt(v.x0, y)
		v.g.clearWideAt(last, y)
	}
}

// SetCursor places the grid cursor at a point in view coordinates. A
// point outside the view hides the cursor rather than drawing it over
// whatever sits next to the view.
//
// A grid has one cursor and no idea who owns it, so only the widget
// holding focus may call this. An unfocused one that did would take the
// cursor from whoever has it, and writing a hidden cursor takes it just
// as surely as writing a visible one. Clearing the cursor belongs to
// whatever draws the widgets, once per frame.
func (v View) SetCursor(c Cursor) {
	if v.g == nil {
		return
	}
	if !v.inBounds(c.X, c.Y) {
		c.Visible = false
	}
	c.X, c.Y = v.x0+c.X, v.y0+c.Y
	v.g.SetCursor(c)
}

// CursorClaimed reports whether a widget placed the cursor since the
// claim was last reset.
func (v View) CursorClaimed() bool { return v.g != nil && v.g.CursorClaimed() }

// ResetCursorClaim forgets who placed the cursor. Whatever draws the
// widgets calls this before a pass and hides the cursor afterwards if
// nobody claimed it.
func (v View) ResetCursorClaim() {
	if v.g != nil {
		v.g.ResetCursorClaim()
	}
}

func (v View) inBounds(x, y int) bool {
	return v.g != nil && x >= 0 && y >= 0 && x < v.cols && y < v.rows
}

// blank returns the grid's blank cell, or a colourless one for the zero
// View, which has no grid to ask.
func (v View) blank() Cell {
	if v.g == nil {
		return Cell{Rune: ' ', Width: 1}
	}
	return v.g.Blank()
}
