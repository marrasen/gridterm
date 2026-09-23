// Drawing a pane that is not a terminal: the pen every row is written
// through, the choices along the bottom, and the blocks a bar and a run
// are drawn with.
//
// Two panes use this -- the one on a piece of file work and the one on
// what this window is serving -- and both have the same shape: some
// lines about a thing that changes on its own, and a row of buttons.
package main

import (
	"image/color"

	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/ui"
)

// A pane draws every frame, and a job's numbers move on their own, so
// the cheap frame matters: a cell written twice in one frame -- blanked
// and then written over -- counts as changed even when it ends up the
// way it started, and a pane that did that would repaint itself for as
// long as it was open.
//
// So every row is written exactly once, through a pen that knows how
// far along it is and blanks the rest.
type rowPen struct {
	v  grid.View
	bg color.RGBA
	y  int

	// at is the column the next write starts at, and cols the width of
	// the row.
	at, cols int
}

// pen starts a row.
func (p *jobPane) pen(v grid.View, y int) *rowPen {
	cols, _ := v.Size()
	return &rowPen{v: v, bg: p.app.colours.BG, y: y, cols: cols}
}

// skip leaves n blank columns.
func (r *rowPen) skip(n int) {
	for i := 0; i < n && r.at < r.cols; i++ {
		r.v.Set(r.at, r.y, grid.Cell{Rune: ' ', BG: r.bg, Width: 1})
		r.at++
	}
}

// write puts text down, cut to what is left of the row.
func (r *rowPen) write(text string, fg color.RGBA) {
	if r.at >= r.cols || text == "" {
		return
	}
	text = grid.TrimTail(text, r.cols-r.at)
	r.v.SetString(r.at, r.y, text, fg, r.bg, 0)
	r.at += grid.StringWidth(text)
}

// writeOn puts text down on a ground of its own, for a button.
func (r *rowPen) writeOn(text string, fg, bg color.RGBA) {
	if r.at >= r.cols || text == "" {
		return
	}
	text = grid.TrimTail(text, r.cols-r.at)
	r.v.SetString(r.at, r.y, text, fg, bg, 0)
	r.at += grid.StringWidth(text)
}

// cell puts one character down, for a bar or a graph.
func (r *rowPen) cell(c grid.Cell) {
	if r.at >= r.cols {
		return
	}
	c.BG = r.bg
	c.Width = 1
	r.v.Set(r.at, r.y, c)
	r.at++
}

// right writes text against the right-hand edge, blanking what is
// between here and there.
func (r *rowPen) right(text string, fg color.RGBA, margin int) {
	w := grid.StringWidth(text)
	if at := r.cols - margin - w; at > r.at {
		r.skip(at - r.at)
		r.write(text, fg)
	}
}

// rest blanks the row to its end.
func (r *rowPen) rest() { r.skip(r.cols - r.at) }

// blank writes an empty row.
func (p *jobPane) blank(v grid.View, y int) { p.pen(v, y).rest() }

// The characters a bar and a run are drawn with.
//
// gridterm draws these itself rather than looking them up in a font --
// see glyph/boxdraw.go -- so they fill their cell exactly and a bar has
// no seam where one cell meets the next. The eighths are what let a bar
// move by less than a whole cell, which is the difference between a bar
// that creeps and one that jumps.
const (
	barFull  = '█'
	barTrack = '░'
)

// barEighths are the part-full blocks, from one eighth to seven.
var barEighths = []rune{'▏', '▎', '▍', '▌', '▋', '▊', '▉'}

// runHeights are the bars of a graph, from one eighth of a cell to a
// full one.
var runHeights = []rune{'▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}

// drawBar paints a bar filled to share, in eighths of a cell.
func drawBar(pen *rowPen, cols int, share float64, fill, track color.RGBA) {
	if cols <= 0 {
		return
	}
	share = min(max(share, 0), 1)
	// In eighths, so the bar moves on a screen where one cell is a
	// whole percent or more.
	eighths := int(share * float64(cols) * 8)
	whole := eighths / 8
	part := eighths % 8
	for x := range cols {
		switch {
		case x < whole:
			pen.cell(grid.Cell{Rune: barFull, FG: fill})
		case x == whole && part > 0:
			pen.cell(grid.Cell{Rune: barEighths[part-1], FG: fill})
		default:
			pen.cell(grid.Cell{Rune: barTrack, FG: track})
		}
	}
}

// drawRun paints speeds as a column chart, newest on the right.
//
// Eight steps to a row, so a graph four rows tall has thirty-two
// heights to draw a second at. The tallest second is the top of the
// chart: what a run says is its shape, and a scale fixed to some
// absolute speed would draw every ordinary copy as a flat line.
func drawRun(pen *rowPen, cols int, past []uint64, most uint64, y, rows int, fg color.RGBA) {
	if cols <= 0 || rows <= 0 || most == 0 {
		return
	}
	if len(past) > cols {
		past = past[len(past)-cols:]
	}
	// Against the right-hand edge, so the newest second is always in
	// the same place and the run grows leftwards as it lengthens.
	pen.skip(cols - len(past))
	// Counted from the bottom row up.
	below := (rows - 1 - y) * 8
	for _, speed := range past {
		eighths := int(float64(speed) / float64(most) * float64(rows*8))
		switch {
		case eighths >= below+8:
			pen.cell(grid.Cell{Rune: barFull, FG: fg})
		case eighths > below:
			pen.cell(grid.Cell{Rune: runHeights[eighths-below-1], FG: fg})
		default:
			pen.skip(1)
		}
	}
}

// choice is one thing along the bottom of the pane: a button, or the
// tick that keeps this copy.
type choice struct {
	title string

	// tick says this is a box rather than a button, and on whether it
	// is ticked now.
	tick bool
	on   bool
}

// width is how many columns the choice takes when it is drawn.
func (c choice) width() int {
	if c.tick {
		return grid.StringWidth(c.label())
	}
	return ui.ButtonWidth(c.title)
}

// label is a tick as it is drawn: the box, then what it keeps.
func (c choice) label() string {
	box := "[ ] "
	if c.on {
		box = "[×] "
	}
	return box + c.title
}

// placeChoices works out where each choice starts, centred as a row
// with a blank column between them.
func placeChoices(into []int, choices []choice, cols int) []int {
	at := into[:0]
	wide := 0
	for i, c := range choices {
		if i > 0 {
			wide++
		}
		wide += c.width()
	}
	x := max((cols-wide)/2, 0)
	for _, c := range choices {
		if x+c.width() > cols {
			at = append(at, -1)
			continue
		}
		at = append(at, x)
		x += c.width() + 1
	}
	return at
}
