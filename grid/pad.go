package grid

// PadUnit is how finely a pad divides a cell. Quarters: half a cell and
// a quarter of one are the sizes that read as padding at the sizes a
// terminal font comes in, and whole quarters keep the sums exact.
const PadUnit = 4

// PadMax is the most one side may ask for, in quarters. Four cells is
// far more than padding, and a cap keeps a stray value from making a
// grid wider than a texture can be.
const PadMax = 4 * PadUnit

// Pad is extra space before and after a column or a row, in quarters of
// a cell.
//
// A grid is not quite a grid: a column can sit half a character in from
// the edge of the window, and a row can have room above and below its
// text. The cell keeps its own size, so nothing is squeezed and no
// glyph is drawn off the baseline it was measured for. Padding is space
// the grid gains, not space the cell gives up.
type Pad struct{ Before, After int8 }

// Empty reports whether a pad asks for nothing.
func (p Pad) Empty() bool { return p == Pad{} }

// clamp keeps a pad within what a grid will honour.
func (p Pad) clamp() Pad {
	return Pad{Before: clampPad(p.Before), After: clampPad(p.After)}
}

func clampPad(v int8) int8 { return min(max(v, 0), PadMax) }

// SetColPad gives a column space to its left and right. Setting one
// moves every column after it, so the whole grid is redrawn.
func (g *Grid) SetColPad(x int, p Pad) {
	g.colPad = g.setPad(g.colPad, g.cols, x, p)
}

// SetRowPad gives a row space above and below. Setting one moves every
// row under it, so the whole grid is redrawn.
func (g *Grid) SetRowPad(y int, p Pad) {
	g.rowPad = g.setPad(g.rowPad, g.rows, y, p)
}

// ColPad returns the space around a column.
func (g *Grid) ColPad(x int) Pad { return padAt(g.colPad, x) }

// RowPad returns the space around a row.
func (g *Grid) RowPad(y int) Pad { return padAt(g.rowPad, y) }

// ColPads returns the column padding, which may stop short of the grid
// or be nil when nothing is padded. Do not write to it.
func (g *Grid) ColPads() []Pad { return g.colPad }

// RowPads returns the row padding, which may stop short of the grid or
// be nil when nothing is padded. Do not write to it.
func (g *Grid) RowPads() []Pad { return g.rowPad }

// ClearPads takes the padding off every column and row.
func (g *Grid) ClearPads() {
	if len(g.colPad) == 0 && len(g.rowPad) == 0 {
		return
	}
	g.colPad, g.rowPad = nil, nil
	g.padded()
}

// PadGeneration counts the changes to the padding.
//
// Nothing in the damage flags reports one: padding moves pixels without
// changing a single cell, so anything caching what a grid looked like
// has to watch this instead.
func (g *Grid) PadGeneration() uint64 { return g.padGen }

// padded records that the padding moved.
func (g *Grid) padded() {
	g.padGen++
	g.allDirty = true
}

// setPad writes one entry, growing the table only when there is
// something to say.
func (g *Grid) setPad(pads []Pad, n, i int, p Pad) []Pad {
	if i < 0 || i >= n {
		return pads
	}
	p = p.clamp()
	if padAt(pads, i) == p {
		return pads
	}
	for len(pads) <= i {
		pads = append(pads, Pad{})
	}
	pads[i] = p
	g.padded()
	return pads
}

// padAt reads an entry from a table that may stop short of the grid.
func padAt(pads []Pad, i int) Pad {
	if i < 0 || i >= len(pads) {
		return Pad{}
	}
	return pads[i]
}

// trimPads drops the padding for columns or rows a resize took away,
// reporting whether any of it was asking for space.
func trimPads(pads []Pad, n int) ([]Pad, bool) {
	if len(pads) <= n {
		return pads, false
	}
	moved := false
	for _, p := range pads[n:] {
		if !p.Empty() {
			moved = true
			break
		}
	}
	return pads[:n], moved
}
