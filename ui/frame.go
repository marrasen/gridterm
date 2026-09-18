package ui

import (
	"image/color"

	"github.com/marrasen/gridterm/grid"
)

// Border picks the characters the rule around a box is drawn with.
type Border uint8

const (
	// BorderSingle is the light box-drawing characters. It is the zero
	// value, so a style that says nothing gets them.
	BorderSingle Border = iota

	// BorderDouble is the double-line ones, which is how a DOS program
	// drew a window.
	BorderDouble
)

// corners are the six characters one kind of rule is made of.
type corners struct {
	topLeft, topRight       rune
	bottomLeft, bottomRight rune
	across, down            rune
}

// runes are the characters a border is drawn with. An unknown border
// gets the single-line ones, which every font has.
func (b Border) runes() corners {
	if b == BorderDouble {
		return corners{'╔', '╗', '╚', '╝', '═', '║'}
	}
	return corners{'┌', '┐', '└', '┘', '─', '│'}
}

// How far the shadow falls. Two columns for one row, because a cell is
// about twice as tall as it is wide: the shadow then falls at the same
// angle whichever way it is measured.
const (
	shadowRight = 2
	shadowDown  = 1
)

// drawShadow darkens the cells below and to the right of a box.
//
// It is drawn before the box, and only outside it, so nothing of the box
// itself is written twice in one frame.
func drawShadow(v grid.View, box Rect, shadow color.RGBA) {
	if shadow.A == 0 || box.Empty() {
		return
	}
	cols, rows := v.Size()
	cell := grid.Cell{Rune: ' ', FG: shadow, BG: shadow, Width: 1}
	set := func(x, y int) {
		if x < 0 || y < 0 || x >= cols || y >= rows {
			return
		}
		if box.Contains(x, y) {
			return
		}
		v.Set(x, y, cell)
	}
	// Down the right-hand side, starting where the box's own top row
	// stops casting one.
	for y := box.Y + shadowDown; y < box.Y+box.Rows+shadowDown; y++ {
		for x := box.X + box.Cols; x < box.X+box.Cols+shadowRight; x++ {
			set(x, y)
		}
	}
	// And along the bottom.
	for y := box.Y + box.Rows; y < box.Y+box.Rows+shadowDown; y++ {
		for x := box.X + shadowRight; x < box.X+box.Cols; x++ {
			set(x, y)
		}
	}
}

// drawFrame paints the rule around a box, in the box's own background.
//
// A dialog over a terminal is otherwise two lots of text with nothing
// between them. The rule is what says where one stops and the other
// starts.
func drawFrame(v grid.View, box Rect, fg, bg color.RGBA, b Border) {
	if fg.A == 0 || box.Cols < 2 || box.Rows < 2 {
		return
	}
	in := box.In(v)
	cols, rows := in.Size()
	if cols < 2 || rows < 2 {
		return
	}
	r := b.runes()
	put := func(x, y int, c rune) {
		in.Set(x, y, grid.Cell{Rune: c, FG: fg, BG: bg, Width: 1})
	}
	for x := 1; x < cols-1; x++ {
		put(x, 0, r.across)
		put(x, rows-1, r.across)
	}
	for y := 1; y < rows-1; y++ {
		put(0, y, r.down)
		put(cols-1, y, r.down)
	}
	put(0, 0, r.topLeft)
	put(cols-1, 0, r.topRight)
	put(0, rows-1, r.bottomLeft)
	put(cols-1, rows-1, r.bottomRight)
}
