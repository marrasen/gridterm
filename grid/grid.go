// Package grid holds the character grid: the model a terminal-style UI
// draws into. It knows nothing about GPUs, fonts or escape sequences, so
// it is testable without a display.
package grid

import (
	"image/color"
	"slices"

	"github.com/rivo/uniseg"
)

// Attr is a bitfield of per-cell text attributes.
type Attr uint8

const (
	AttrBold Attr = 1 << iota
	AttrUnderline
	AttrReverse
	AttrItalic
	AttrStrike
	AttrDim
	AttrBlink
	AttrHidden
)

// Cell is one character position.
//
// Width is how many columns the cell occupies: 1 for ordinary text, 2
// for a double-width character such as most CJK and emoji, and 0 for the
// column a double-width character spills into. A width-0 cell carries
// the same colours as its lead cell so that background runs still merge,
// but it draws no glyph of its own.
type Cell struct {
	Rune  rune
	Comb  []rune // combining marks drawn over Rune, if any
	FG    color.RGBA
	BG    color.RGBA
	Attr  Attr
	Width uint8
}

// Equal reports whether two cells would draw identically. Cell contains
// a slice, so it cannot be compared with ==.
func (c Cell) Equal(o Cell) bool {
	return c.Rune == o.Rune &&
		c.FG == o.FG &&
		c.BG == o.BG &&
		c.Attr == o.Attr &&
		c.Width == o.Width &&
		slices.Equal(c.Comb, o.Comb)
}

// CursorStyle selects how the cursor is drawn.
type CursorStyle uint8

const (
	CursorBlock CursorStyle = iota
	CursorUnderline
	CursorBar
)

// Cursor is the text cursor's position and appearance. A cursor outside
// the grid bounds is simply not drawn, which saves callers from clamping
// during a resize.
type Cursor struct {
	X, Y    int
	Visible bool
	Style   CursorStyle
}

// Grid is a rectangular buffer of cells with per-row damage tracking.
// Damage lets the renderer redraw only the rows that changed, which is
// what keeps a mostly-idle screen cheap.
type Grid struct {
	cols, rows int
	cells      []Cell
	dirty      []bool
	allDirty   bool
	cursor     Cursor

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

// Blank returns an empty cell in the default colours.
func (g *Grid) Blank() Cell {
	return Cell{Rune: ' ', FG: g.DefaultFG, BG: g.DefaultBG, Width: 1}
}

// Resize changes the grid dimensions, preserving the top-left overlap.
// Growing fills the new area with blanks in the default colours.
func (g *Grid) Resize(cols, rows int) {
	cols = max(cols, 0)
	rows = max(rows, 0)
	if cols == g.cols && rows == g.rows {
		return
	}
	next := make([]Cell, cols*rows)
	blank := g.Blank()
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

	// Narrowing can cut a double-width character in half. Repair the
	// whole row rather than just its edges: blanking one broken pair can
	// expose another, and a lead cell with no continuation makes
	// clearWideAt blank an innocent neighbour later on.
	for y := 0; y < copyRows; y++ {
		repairWidths(g.cells[y*cols:(y+1)*cols], blank)
	}
}

// repairWidths blanks any half of a double-width character whose partner
// is missing, leaving the row's width invariant intact.
func repairWidths(row []Cell, blank Cell) {
	for x := range row {
		switch row[x].Width {
		case 2:
			if x+1 >= len(row) || row[x+1].Width != 0 {
				row[x] = blank
			}
		case 0:
			if x == 0 || row[x-1].Width != 2 {
				row[x] = blank
			}
		}
	}
}

// Clear fills the whole grid with blanks in the default colours.
func (g *Grid) Clear() {
	blank := g.Blank()
	for i := range g.cells {
		g.cells[i] = blank
	}
	g.allDirty = true
}

// At returns the cell at x,y. Out-of-range coordinates return a blank
// cell rather than panicking, so drawing code can be sloppy at edges.
func (g *Grid) At(x, y int) Cell {
	if !g.inBounds(x, y) {
		return g.Blank()
	}
	return g.cells[y*g.cols+x]
}

// Set writes a cell and marks its row dirty. Writing a cell identical to
// the one already there does not dirty the row: an idle repaint of
// unchanged content costs nothing.
//
// Set is the low-level primitive and does not maintain the width
// invariant between a lead cell and its continuation. Callers writing
// double-width characters should use SetWide.
func (g *Grid) Set(x, y int, c Cell) {
	if !g.inBounds(x, y) {
		return
	}
	i := y*g.cols + x
	if g.cells[i].Equal(c) {
		return
	}
	g.cells[i] = c
	g.dirty[y] = true
}

// SetWide writes a cell that may occupy two columns, keeping the lead
// and continuation cells consistent. It reports the number of columns
// consumed, which is 0 when the cell does not fit.
//
// Overwriting either half of an existing double-width character blanks
// the other half first; leaving it behind would draw a stray glyph.
func (g *Grid) SetWide(x, y int, c Cell) int {
	if !g.inBounds(x, y) {
		return 0
	}
	w := min(max(int(c.Width), 1), 2)
	if x+w > g.cols {
		return 0
	}
	c.Width = uint8(w)
	cont := Cell{FG: c.FG, BG: c.BG, Attr: c.Attr, Width: 0}

	// Check for equality before touching anything. clearWideAt writes a
	// blank over the continuation cell, which would dirty the row even
	// when the character being written is the one already there — and an
	// idle screen full of CJK would then repaint every frame.
	if g.cells[y*g.cols+x].Equal(c) &&
		(w == 1 || g.cells[y*g.cols+x+1].Equal(cont)) {
		return w
	}

	g.clearWideAt(x, y)
	if w == 2 {
		g.clearWideAt(x+1, y)
	}
	g.Set(x, y, c)
	if w == 2 {
		g.Set(x+1, y, cont)
	}
	return w
}

// clearWideAt blanks the other half of any double-width character
// covering column x, so no continuation is left without its lead or the
// reverse.
func (g *Grid) clearWideAt(x, y int) {
	if !g.inBounds(x, y) {
		return
	}
	switch g.cells[y*g.cols+x].Width {
	case 2:
		g.Set(x+1, y, g.Blank())
	case 0:
		g.Set(x-1, y, g.Blank())
	}
}

// SetString writes s starting at x,y in one style, stopping at the row
// end. It returns the column just past the last cluster written.
//
// Text is split into grapheme clusters, so a base character and its
// combining marks share one cell, and double-width clusters take two.
func (g *Grid) SetString(x, y int, s string, fg, bg color.RGBA, attr Attr) int {
	state := -1
	for len(s) > 0 {
		var cluster string
		var width int
		cluster, s, width, state = uniseg.FirstGraphemeClusterInString(s, state)
		if x >= g.cols {
			break
		}
		c := ClusterCell(cluster, width)
		c.FG, c.BG, c.Attr = fg, bg, attr
		n := g.SetWide(x, y, c)
		if n == 0 {
			// A double-width cluster with one column left. Blank the
			// column rather than leaving whatever was under it, and
			// report the row as full.
			g.Set(x, y, Cell{Rune: ' ', FG: fg, BG: bg, Attr: attr, Width: 1})
			return g.cols
		}
		x += n
	}
	return x
}

// ClusterCell builds a cell from one grapheme cluster and its display
// width. Width is clamped to 1 or 2: a terminal grid has no room for
// anything else, and a zero-width cluster still has to land somewhere.
//
// Only true zero-width marks join the base rune. The remaining runes of
// an emoji ZWJ sequence or a regional-indicator flag are full-size
// glyphs; stacking them in one cell draws them on top of each other, so
// they are dropped and only the first is shown.
func ClusterCell(cluster string, width int) Cell {
	c := Cell{Rune: ' ', Width: 1}
	if width >= 2 {
		c.Width = 2
	}
	first := true
	for _, r := range cluster {
		if first {
			c.Rune = printable(r)
			first = false
			continue
		}
		if RuneWidth(r) == 0 {
			c.Comb = append(c.Comb, r)
		}
	}
	return c
}

// printable maps control characters to a space. They have no glyph, and
// letting one reach the atlas would cache a miss under a rune the caller
// never meant to display.
func printable(r rune) rune {
	if r < 0x20 || r == 0x7f {
		return ' '
	}
	return r
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
	blank := g.Blank()
	for i := (g.rows - n) * g.cols; i < len(g.cells); i++ {
		g.cells[i] = blank
	}
	g.allDirty = true
}

// Cursor returns the current cursor.
func (g *Grid) Cursor() Cursor { return g.cursor }

// SetCursor moves the cursor, dirtying both the row it left and the row
// it arrived at so the old cell is repainted without it.
func (g *Grid) SetCursor(c Cursor) {
	if c == g.cursor {
		return
	}
	old := g.cursor
	g.cursor = c
	if old.Visible {
		g.dirtyRow(old.Y)
	}
	if c.Visible {
		g.dirtyRow(c.Y)
	}
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
	return slices.Contains(g.dirty, true)
}

// ClearDirty marks the whole grid clean. The renderer calls this after
// a successful frame.
func (g *Grid) ClearDirty() {
	g.allDirty = false
	clear(g.dirty)
}

// MarkAllDirty forces a full repaint on the next frame, for when
// something outside the cell contents changed (window resize, theme).
func (g *Grid) MarkAllDirty() { g.allDirty = true }

func (g *Grid) dirtyRow(y int) {
	if y >= 0 && y < g.rows {
		g.dirty[y] = true
	}
}

func (g *Grid) inBounds(x, y int) bool {
	return x >= 0 && y >= 0 && x < g.cols && y < g.rows
}

// RuneWidth returns how many columns r occupies: 0 for a combining
// mark, 2 for a double-width character, 1 otherwise.
//
// Every part of the program measures width through this one function.
// A terminal that disagrees with its own grid about how wide a character
// is will corrupt the screen, so there is deliberately no second source.
func RuneWidth(r rune) int {
	switch w := uniseg.StringWidth(string(r)); {
	case w <= 0:
		return 0
	case w >= 2:
		return 2
	default:
		return 1
	}
}
