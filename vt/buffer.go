package vt

import "github.com/marrasen/gridterm/grid"

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
//
// revive caps how many lines a taller window may take back out of
// history. A screen that has been cleared takes back none of what the
// clear put behind it: a window dragged taller must not undo a clear.
func (b *buffer) resize(cols, rows int, fill grid.Cell, cursorY, revive int) int {
	b.fill = fill
	if cols == b.cols && rows == len(b.lines) {
		return 0
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

	shift := 0
	switch {
	case rows > len(b.lines):
		// Pull lines back out of scrollback before inventing blank ones,
		// so making a window taller reveals what scrolled off rather
		// than adding blank space.
		want := rows - len(b.lines)
		take := min(min(want, len(b.scrollback)), max(revive, 0))
		if take > 0 {
			revived := b.scrollback[len(b.scrollback)-take:]
			b.scrollback = b.scrollback[:len(b.scrollback)-take]
			b.lines = append(append([]line{}, revived...), b.lines...)
			shift = take
		}
		for len(b.lines) < rows {
			b.lines = append(b.lines, b.blankLine())
		}
	case rows < len(b.lines):
		// Drop from the bottom where possible: those rows are below the
		// cursor and usually blank. Only take from the top when the
		// cursor would otherwise fall off the new screen, and push what
		// is taken into history so output is not destroyed.
		drop := len(b.lines) - rows
		fromTop := max(cursorY-rows+1, 0)
		fromTop = min(fromTop, drop)
		for _, l := range b.lines[:fromTop] {
			b.pushScrollback(l)
		}
		b.lines = append([]line{}, b.lines[fromTop:len(b.lines)-(drop-fromTop)]...)
		shift = -fromTop
	}
	return shift
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

// pushScrollback appends a line to history and reports whether it was
// kept, so the caller can keep a scrolled-back view pinned to the same
// text.
func (b *buffer) pushScrollback(l line) bool {
	if b.maxScroll <= 0 {
		return false
	}
	b.scrollback = append(b.scrollback, l)
	if len(b.scrollback) > b.maxScroll+scrollSlack {
		// Trim in batches. Compacting on every line past the limit
		// memmoves the whole history for each line of output, which is
		// the one part of this design that scales the wrong way under a
		// flood.
		drop := len(b.scrollback) - b.maxScroll
		clear(b.scrollback[:drop])
		b.scrollback = append(b.scrollback[:0], b.scrollback[drop:]...)
	}
	return true
}

// scrollSlack is how far history may overshoot its limit before being
// compacted, so trimming costs one memmove per slack lines rather than
// one per line.
const scrollSlack = 256

// scrollUp moves lines [top,bot] up by n, discarding the top n, and
// reports how many of them left the top of the screen and how many of
// those history kept.
//
// Lines only leave the top when the region is the whole screen. A
// program scrolling a smaller region — one that has reserved a status
// line, say — is redrawing, and pushing those lines into history would
// fill it with fragments of its own interface.
//
// The two counts differ when there is no history to keep them in. Lines
// that left are what names a line; lines that were kept are what a
// scrolled-back view has to be moved along by.
func (b *buffer) scrollUp(top, bot, n int, history bool, fill grid.Cell) (left, kept int) {
	b.fill = fill
	if n <= 0 || top < 0 || bot >= len(b.lines) || top > bot {
		return 0, 0
	}
	n = min(n, bot-top+1)
	whole := top == 0 && bot == len(b.lines)-1
	if history && whole {
		left = n
	}
	for i := 0; i < n; i++ {
		if history && whole && b.pushScrollback(b.lines[top+i]) {
			kept++
		}
	}
	copy(b.lines[top:bot+1-n], b.lines[top+n:bot+1])
	for i := bot + 1 - n; i <= bot; i++ {
		b.lines[i] = b.blankLine()
	}
	return left, kept
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
