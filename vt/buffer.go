package vt

import "github.com/marcus/gridterm/grid"

// line is one row of cells. Rows are separate slices rather than one
// flat array so scrolling is a pointer rotate instead of a copy of the
// whole screen — the difference shows up immediately under `yes`.
type line []grid.Cell

// buffer is one screen's worth of lines, plus the scrollback behind it.
// A terminal has two: the primary buffer, which keeps history, and the
// alternate buffer used by full-screen programs, which does not.
type buffer struct {
	lines      []line
	scrollback []line
	maxScroll  int // 0 disables history, as on the alternate buffer
	cols       int

	// fill is the cell new and cleared lines are made of. The zero Cell
	// is deliberately never used: it has Width 0, which the renderer
	// reads as the continuation of a double-width character, and zero
	// colours, which draw as transparent black.
	fill grid.Cell
}

func newBuffer(cols, rows, maxScroll int, fill grid.Cell) *buffer {
	b := &buffer{maxScroll: maxScroll, cols: cols, fill: fill}
	b.lines = make([]line, rows)
	for i := range b.lines {
		b.lines[i] = b.blankLine()
	}
	return b
}

func (b *buffer) rows() int { return len(b.lines) }

// blankLine allocates a row of fill cells.
func (b *buffer) blankLine() line {
	l := make(line, b.cols)
	for i := range l {
		l[i] = b.fill
	}
	return l
}

// resize changes the dimensions, keeping the top-left overlap. Lines are
// truncated or extended in place; rows are added at the bottom and
// removed from the top, so growing a window reveals scrollback rather
// than pushing the prompt down.
func (b *buffer) resize(cols, rows int, fill grid.Cell) {
	b.fill = fill
	if cols == b.cols && rows == len(b.lines) {
		return
	}
	if cols != b.cols {
		b.cols = cols
		for i, l := range b.lines {
			b.lines[i] = resizeLine(l, cols, fill)
		}
		for i, l := range b.scrollback {
			b.scrollback[i] = resizeLine(l, cols, fill)
		}
	}

	switch {
	case rows > len(b.lines):
		// Pull lines back out of scrollback before inventing blank ones.
		want := rows - len(b.lines)
		take := min(want, len(b.scrollback))
		if take > 0 {
			revived := b.scrollback[len(b.scrollback)-take:]
			b.scrollback = b.scrollback[:len(b.scrollback)-take]
			b.lines = append(append([]line{}, revived...), b.lines...)
		}
		for len(b.lines) < rows {
			b.lines = append(b.lines, b.blankLine())
		}
	case rows < len(b.lines):
		// Drop from the top, pushing the dropped lines into history so
		// shrinking a window does not destroy output.
		drop := len(b.lines) - rows
		for _, l := range b.lines[:drop] {
			b.pushScrollback(l)
		}
		b.lines = append([]line{}, b.lines[drop:]...)
	}
}

// resizeLine truncates or pads a row to cols, blanking any half of a
// double-width character left dangling at the new right edge.
func resizeLine(l line, cols int, fill grid.Cell) line {
	out := make(line, cols)
	n := min(cols, len(l))
	copy(out, l[:n])
	for i := n; i < cols; i++ {
		out[i] = fill
	}
	if cols > 0 {
		if out[cols-1].Width == 2 {
			out[cols-1] = fill
		}
		if out[0].Width == 0 {
			out[0] = fill
		}
	}
	return out
}

func (b *buffer) pushScrollback(l line) {
	if b.maxScroll <= 0 {
		return
	}
	b.scrollback = append(b.scrollback, l)
	if len(b.scrollback) > b.maxScroll {
		// Drop the oldest. Copying down rather than re-slicing keeps the
		// backing array from growing without bound.
		drop := len(b.scrollback) - b.maxScroll
		b.scrollback = append(b.scrollback[:0], b.scrollback[drop:]...)
	}
}

// scrollUp moves lines [top,bot] up by n, discarding the top n. When the
// region starts at row 0 and history is enabled, the discarded lines go
// to scrollback; a scroll inside a smaller region is a full-screen
// program redrawing and is not history.
func (b *buffer) scrollUp(top, bot, n int, history bool, fill grid.Cell) {
	b.fill = fill
	if n <= 0 || top < 0 || bot >= len(b.lines) || top > bot {
		return
	}
	n = min(n, bot-top+1)
	for i := 0; i < n; i++ {
		l := b.lines[top+i]
		if history && top == 0 {
			b.pushScrollback(l)
		}
	}
	copy(b.lines[top:bot+1-n], b.lines[top+n:bot+1])
	for i := bot + 1 - n; i <= bot; i++ {
		b.lines[i] = b.blankLine()
	}
}

// scrollDown moves lines [top,bot] down by n, discarding the bottom n.
// Nothing enters scrollback: history only grows off the top.
func (b *buffer) scrollDown(top, bot, n int, fill grid.Cell) {
	b.fill = fill
	if n <= 0 || top < 0 || bot >= len(b.lines) || top > bot {
		return
	}
	n = min(n, bot-top+1)
	copy(b.lines[top+n:bot+1], b.lines[top:bot+1-n])
	for i := top; i < top+n; i++ {
		b.lines[i] = b.blankLine()
	}
}

// view returns the row at visible position y, offset rows back into
// scrollback. It returns nil when the offset walks off the top.
func (b *buffer) view(y, offset int) line {
	i := y - offset
	if i >= 0 {
		if i < len(b.lines) {
			return b.lines[i]
		}
		return nil
	}
	j := len(b.scrollback) + i
	if j < 0 || j >= len(b.scrollback) {
		return nil
	}
	return b.scrollback[j]
}
