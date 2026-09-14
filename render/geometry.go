package render

import (
	"sort"

	"github.com/marrasen/gridterm/glyph"
	"github.com/marrasen/gridterm/grid"
)

// Geometry is where a grid's columns and rows land in pixels.
//
// Everything drawn from a grid goes through here rather than
// multiplying a column by the cell width, because a column or a row can
// ask for space around it: the sidebar sits half a character in from
// the edge of the window, and the menu bar has room above its titles.
//
// Each column and row has two boxes. The outer one covers the padding
// as well, and is what a background, a rule or the cursor fills, so
// that padding takes the colour of what it pads and no seam shows. The
// inner one is the cell itself, where the glyph goes.
//
// Layout reuses its slices, so a geometry laid out every frame does not
// allocate. One is not safe for use from two goroutines.
type Geometry struct {
	cols, rows   []span
	cellW, cellH int
	ascent       int
}

// span is one column or row: where its outer box starts, how wide that
// box is, and where the cell sits inside it.
type span struct {
	at, size, in int
}

// Layout measures a grid in pixels.
func (geo *Geometry) Layout(g *grid.Grid, m glyph.Metrics) {
	cols, rows := 0, 0
	var colPads, rowPads []grid.Pad
	if g != nil {
		cols, rows = g.Size()
		colPads, rowPads = g.ColPads(), g.RowPads()
	}
	geo.cellW, geo.cellH, geo.ascent = max(m.CellW, 1), max(m.CellH, 1), m.Ascent
	geo.cols = layoutSpans(geo.cols[:0], cols, geo.cellW, colPads)
	geo.rows = layoutSpans(geo.rows[:0], rows, geo.cellH, rowPads)
}

// layoutSpans places n columns or rows of the given size, each with
// whatever padding its table asks for.
func layoutSpans(dst []span, n, size int, pads []grid.Pad) []span {
	quarters, at := 0, 0
	for i := 0; i < n; i++ {
		p := padAt(pads, i)
		in := padPixels(i, quarters+int(p.Before), size)
		quarters += int(p.Before) + int(p.After)
		end := padPixels(i+1, quarters, size)
		dst = append(dst, span{at: at, size: end - at, in: in})
		at = end
	}
	return dst
}

// padPixels is where a column begins: whole cells, plus every quarter
// of padding before it turned into pixels in one go.
//
// In one go because the room the padding needs is worked out the same
// way, before there is a grid to lay out. A division per pad loses up
// to a pixel each time, and the pixels lost add up to room the window
// set aside and the grid never uses: a strip of the window with no cell
// to paint it.
func padPixels(cells, quarters, size int) int {
	return cells*size + quarters*size/grid.PadUnit
}

// padAt reads a pad table that may stop short of the grid.
func padAt(pads []grid.Pad, i int) grid.Pad {
	if i < 0 || i >= len(pads) {
		return grid.Pad{}
	}
	return pads[i]
}

// CellW and CellH are the pixel size of one cell, padding aside.
func (geo *Geometry) CellW() int { return geo.cellW }
func (geo *Geometry) CellH() int { return geo.cellH }

// Ascent is how far the baseline sits below the top of a cell.
func (geo *Geometry) Ascent() int { return geo.ascent }

// Cols and Rows are how many the geometry was laid out for.
func (geo *Geometry) Cols() int { return len(geo.cols) }
func (geo *Geometry) Rows() int { return len(geo.rows) }

// CellX and CellY are where one cell's own box starts, which is where
// its glyph is drawn from.
func (geo *Geometry) CellX(x int) int { return cellStart(geo.cols, x, geo.cellW) }
func (geo *Geometry) CellY(y int) int { return cellStart(geo.rows, y, geo.cellH) }

// ColBox is the outer box of the columns from x0 up to but not
// including x1, padding included.
func (geo *Geometry) ColBox(x0, x1 int) (at, size int) { return box(geo.cols, x0, x1, geo.cellW) }

// RowBox is the outer box of the rows from y0 up to but not including
// y1, padding included.
func (geo *Geometry) RowBox(y0, y1 int) (at, size int) { return box(geo.rows, y0, y1, geo.cellH) }

// Fill spreads the pixels a window has over the grid between its two
// edges, so the grid reaches the window on both sides.
//
// A window is rarely a whole number of cells across, and the few pixels
// over are not worth a column of their own. Left alone they are a strip
// with nothing to paint it, which shows as a gash down the edge. Shared
// between the first column and the last, they widen the margin the
// window already has by a pixel or two at each end.
//
// Only a shortfall of less than a cell is taken, so a layer that is
// genuinely smaller than the window keeps its own size.
func (geo *Geometry) Fill(pxW, pxH int) {
	shareOut(geo.cols, pxW, geo.cellW)
	shareOut(geo.rows, pxH, geo.cellH)
}

// shareOut gives the pixels a window has over to the first and the last
// span, half each, if together they come to less than one cell.
func shareOut(spans []span, px, size int) {
	if len(spans) == 0 {
		return
	}
	last := spans[len(spans)-1]
	short := px - (last.at + last.size)
	if short <= 0 || short >= size {
		return
	}
	// Everything moves over by the first half, and then the first span
	// reaches back to the edge it came from.
	head := short / 2
	for i := range spans {
		spans[i].at += head
		spans[i].in += head
	}
	spans[0].at -= head
	spans[0].size += head
	spans[len(spans)-1].size += short - head
}

// Width and Height are the pixel size of the whole grid, padding
// included. Both are at least one, so a texture can be made from them.
func (geo *Geometry) Width() int  { return max(spanEnd(geo.cols, geo.cellW), 1) }
func (geo *Geometry) Height() int { return max(spanEnd(geo.rows, geo.cellH), 1) }

// CellAt is which column and row a pixel falls in.
func (geo *Geometry) CellAt(px, py int) (col, row int) {
	return geo.ColAt(px), geo.RowAt(py)
}

// ColAt and RowAt are which column and row a pixel falls in.
//
// A pixel outside the grid keeps counting in whole cells, so a drag
// that has wandered off the window still says which way it went.
func (geo *Geometry) ColAt(px int) int { return at(geo.cols, px, geo.cellW) }
func (geo *Geometry) RowAt(py int) int { return at(geo.rows, py, geo.cellH) }

// cellStart is where one cell's own box begins.
func cellStart(spans []span, i, size int) int {
	switch {
	case len(spans) == 0:
		return i * size
	case i < 0:
		return spans[0].in + i*size
	case i >= len(spans):
		last := spans[len(spans)-1]
		return last.in + (i-len(spans)+1)*size
	}
	return spans[i].in
}

// box is the outer box covering a run of columns or rows.
func box(spans []span, i0, i1, size int) (at, length int) {
	if i1 <= i0 {
		return boxStart(spans, i0, size), 0
	}
	start := boxStart(spans, i0, size)
	return start, boxStart(spans, i1, size) - start
}

// boxStart is where a column or row's outer box begins, or where the
// grid ends when asked for the one past the last.
func boxStart(spans []span, i, size int) int {
	switch {
	case len(spans) == 0:
		return i * size
	case i < 0:
		return spans[0].at + i*size
	case i >= len(spans):
		last := spans[len(spans)-1]
		return last.at + last.size + (i-len(spans))*size
	}
	return spans[i].at
}

// spanEnd is where the last column or row's box finishes.
func spanEnd(spans []span, size int) int {
	if len(spans) == 0 {
		return 0
	}
	last := spans[len(spans)-1]
	return last.at + last.size
}

// at is which column or row a pixel falls in.
func at(spans []span, px, size int) int {
	if size <= 0 {
		// Nothing has been laid out yet, so every pixel is the first
		// cell. Better than dividing by a cell size of nothing.
		return 0
	}
	if len(spans) == 0 {
		return floorDiv(px, size)
	}
	if px < spans[0].at {
		return floorDiv(px-spans[0].at, size)
	}
	if end := spanEnd(spans, size); px >= end {
		return len(spans) + (px-end)/size
	}
	// The first span starting after the pixel, less one, is the span the
	// pixel is in. Boxes touch with no gap, so there is always one.
	i := sort.Search(len(spans), func(i int) bool { return spans[i].at > px })
	return i - 1
}

// floorDiv divides towards minus infinity, so a pixel left of or above
// the grid counts backwards rather than piling up on zero.
func floorDiv(a, b int) int {
	q := a / b
	if a%b != 0 && (a < 0) != (b < 0) {
		q--
	}
	return q
}
