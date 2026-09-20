package files

import (
	"strings"

	"github.com/marrasen/gridterm/grid"
)

// spot is a place in a file: a line, and a column across it, columns
// being what the mouse gives and what the reader scrolls by.
type spot struct{ line, col int }

// before reports whether a spot comes first in reading order.
func (s spot) before(o spot) bool {
	return s.line < o.line || (s.line == o.line && s.col < o.col)
}

// span is the stretch of a file between two spots, in either order. Both
// ends are inside it, the way a drag over a terminal takes the cell the
// pointer is on.
type span struct {
	from, to spot
	on       bool
}

// ordered is the span's ends in reading order.
func (s span) ordered() (from, to spot) {
	if s.to.before(s.from) {
		return s.to, s.from
	}
	return s.from, s.to
}

// selCols is the stretch of columns the selection covers on a line,
// with both ends inside it. ok is false for a line it does not reach and
// for one it reaches past the end of.
func (r *Reader) selCols(line int) (from, to int, ok bool) {
	if !r.sel.on {
		return 0, 0, false
	}
	return r.colsOf(r.sel, line)
}

// colsOf is selCols for a span that may not be the one in hand.
func (r *Reader) colsOf(s span, line int) (from, to int, ok bool) {
	if line < 0 || line >= len(r.shown) {
		return 0, 0, false
	}
	first, last := s.ordered()
	if line < first.line || line > last.line {
		return 0, 0, false
	}
	text := r.shown[line]
	from, to = 0, grid.StringWidth(text)-1
	if line == first.line {
		from = max(first.col, 0)
	}
	if line == last.line {
		to = min(last.col, to)
	}
	if to < from {
		return 0, 0, false
	}
	from, to = snapCols(text, from, to)
	return from, to, true
}

// snapCols widens a stretch of columns to whole characters, so a
// double-width one is picked out or left out and never halved.
func snapCols(line string, from, to int) (int, int) {
	if !hasHighByte(line) {
		return from, to
	}
	at := 0
	for _, cluster := range grid.Clusters(line) {
		w := grid.StringWidth(cluster)
		if w > 1 {
			if at < from && from < at+w {
				from = at
			}
			if at <= to && to < at+w-1 {
				to = at + w - 1
			}
		}
		at += w
	}
	return from, to
}

// Selected reports whether any of the file is picked out.
func (r *Reader) Selected() bool { return r.sel.on && r.picking() }

// picking reports whether there is text on screen to pick out: lines,
// rather than a picture or the reason the file could not be read.
func (r *Reader) picking() bool {
	return !r.isPic && r.err == nil && len(r.shown) > 0
}

// settle turns the selection off when it has no text of the file under
// it, which is what a drag that ended past the end of a line leaves.
func (r *Reader) settle() {
	r.sel.on = r.sel.on && r.covers(r.sel)
}

// covers reports whether a span has any text under it.
func (r *Reader) covers(s span) bool {
	first, last := s.ordered()
	for line := max(first.line, 0); line <= min(last.line, len(r.shown)-1); line++ {
		if _, _, ok := r.colsOf(s, line); ok {
			return true
		}
	}
	return false
}

// SelectedText is the text the selection covers, with a newline between
// lines. Empty when nothing is picked out.
func (r *Reader) SelectedText() string {
	if !r.Selected() {
		return ""
	}
	from, to := r.sel.ordered()
	first, last := max(from.line, 0), min(to.line, len(r.shown)-1)
	var sb strings.Builder
	for line := first; line <= last; line++ {
		if line > first {
			sb.WriteByte('\n')
		}
		sb.WriteString(r.selectedOn(line))
	}
	return sb.String()
}

// selectedOn is what the selection covers of one line.
func (r *Reader) selectedOn(line int) string {
	from, to, ok := r.selCols(line)
	if !ok {
		return ""
	}
	rest, cut := grid.CutLeft(r.shown[line], from)
	return grid.Trim(rest, to-cut+1)
}

// SelectAll picks out the whole file.
func (r *Reader) SelectAll() {
	if !r.picking() {
		return
	}
	last := len(r.shown) - 1
	r.sel = span{
		to: spot{line: last, col: max(grid.StringWidth(r.shown[last])-1, 0)},
		on: true,
	}
	r.settle()
}

// ClearSelection takes the highlight off.
func (r *Reader) ClearSelection() { r.sel = span{} }

// Copy hands the selection to the window. It reports whether there was
// anything to copy.
func (r *Reader) Copy() bool {
	text := r.SelectedText()
	if text == "" || r.OnCopy == nil {
		return false
	}
	r.OnCopy(text)
	return true
}

// extendPages carries the loose end of the selection a screenful at a
// time, which is the page keys' share of the same move.
func (r *Reader) extendPages(pages int) {
	r.extend(pages*max(r.rows(), 1), 0)
}

// extend moves the loose end of the selection by lines and columns, and
// starts one at the top left of the view when there is none.
func (r *Reader) extend(lines, cols int) {
	if !r.picking() {
		return
	}
	if !r.sel.on {
		at := spot{line: r.top, col: r.left}
		r.sel = span{from: at, to: at, on: true}
	}
	r.sel.to.line = min(max(r.sel.to.line+lines, 0), max(len(r.shown)-1, 0))
	r.sel.to.col = r.onLine(r.sel.to.line, r.sel.to.col+cols)
	r.settle()
	r.showSpot(r.sel.to)
}

// onLine pulls a column onto a line, so the loose end of a selection
// never sits past the end of what it is on.
func (r *Reader) onLine(line, col int) int {
	return min(max(col, 0), max(r.lineWidth(line)-1, 0))
}

// extendTo moves the loose end of the selection to a column of the line
// it is already on: the start of that line, or its end.
func (r *Reader) extendTo(col int) {
	if !r.picking() {
		return
	}
	if !r.sel.on {
		at := spot{line: r.top, col: r.left}
		r.sel = span{from: at, to: at, on: true}
	}
	if col < 0 {
		col = max(r.lineWidth(r.sel.to.line)-1, 0)
	}
	r.sel.to.col = r.onLine(r.sel.to.line, col)
	r.settle()
	r.showSpot(r.sel.to)
}

// lineWidth is how many columns a line of the file takes, and zero for a
// line that is not there.
func (r *Reader) lineWidth(line int) int {
	if line < 0 || line >= len(r.shown) {
		return 0
	}
	return grid.StringWidth(r.shown[line])
}

// showSpot scrolls so a place in the file is on screen.
func (r *Reader) showSpot(at spot) {
	switch {
	case at.line < r.top:
		r.top = at.line
	case at.line >= r.top+r.rows():
		r.top = at.line - r.rows() + 1
	}
	r.clampTop()
	switch {
	case at.col < r.left:
		r.left = at.col
	case at.col >= r.left+r.size.Cols:
		r.left = at.col - r.size.Cols + 1
	}
	r.left = max(min(r.left, r.lastLeft()), 0)
}

// clampSel drops a selection whose text has gone, which is what a file
// that shrank past it leaves. Pulling it onto the last line instead
// would move the highlight onto text nobody picked.
func (r *Reader) clampSel() {
	if !r.sel.on {
		return
	}
	last := len(r.shown) - 1
	if r.sel.from.line > last || r.sel.to.line > last {
		r.sel = span{}
		return
	}
	r.settle()
}

// spotAt is where in the file a mouse event points, clamped to the lines
// on screen and to the lines there are.
//
// A drag is clamped into the pane rather than stopped at its edge, so
// without this a drag onto the bar picks out a line below the last one
// drawn.
func (r *Reader) spotAt(col, row int) spot {
	line := r.top + min(max(row-1, 0), max(r.rows()-1, 0))
	return spot{line: min(line, max(len(r.shown)-1, 0)), col: max(col, 0) + r.left}
}

// inBody reports whether a row of the pane holds a line of the file.
func (r *Reader) inBody(row int) bool {
	return row >= 1 && row < r.size.Rows-1
}

// markSelected paints the selection behind the part of a line that is on
// screen.
func (r *Reader) markSelected(v grid.View, y, line, cols int) {
	from, to, ok := r.selCols(line)
	if !ok {
		return
	}
	for x := max(from-r.left, 0); x <= min(to-r.left, cols-1); x++ {
		c := v.At(x, y)
		c.FG, c.BG = r.Style.SelectedFG, r.Style.SelectedBG
		v.Set(x, y, c)
	}
}
