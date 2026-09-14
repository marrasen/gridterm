package ui

import (
	"image/color"

	"github.com/marrasen/gridterm/grid"
)

// buffer draws a widget through a grid of its own, so every cell of the
// view it was given is written once with its final value.
//
// A dialog that fills a box and then writes text over it changes the
// same cell twice. Drawn straight onto a layer that dirties the layer's
// rows on every frame, and a frame where nothing moved could never be
// skipped.
//
// It also settles what happens where the widget draws nothing. The
// buffer starts with no colours at all, so an untouched cell comes out
// see-through, and a box that shrank leaves nothing of the old one
// standing beside it.
//
// The zero buffer is ready to use.
type buffer struct {
	g *grid.Grid
}

// draw runs paint against a buffer the size of v and copies the result
// out, cursor and all.
func (b *buffer) draw(v grid.View, paint func(grid.View)) {
	cols, rows := v.Size()
	if cols <= 0 || rows <= 0 {
		return
	}
	if b.g == nil {
		b.g = grid.New(cols, rows, color.RGBA{}, color.RGBA{})
	} else {
		b.g.Resize(cols, rows)
	}
	b.g.ResetCursorClaim()
	b.g.View().Fill(grid.Cell{Rune: ' ', Width: 1})
	paint(b.g.View())

	for y := 0; y < rows; y++ {
		for x := 0; x < cols; x++ {
			v.Set(x, y, b.g.At(x, y))
		}
	}
	// The cursor is only passed on when the widget asked for one, or a
	// dialog with nothing to type into would take it from whoever has it.
	if b.g.CursorClaimed() {
		v.SetCursor(b.g.Cursor())
	}
}
