package main

import (
	"github.com/hajimehoshi/ebiten/v2"

	"github.com/marrasen/gridterm/ui"
)

// updatePointer draws the mouse pointer as a resize arrow over anything
// that can be dragged to resize, and as the ordinary pointer elsewhere.
//
// Once a frame, and only when the answer changed: an idle window asks
// the tree for nothing and tells ebiten nothing.
func (a *app) updatePointer() {
	px, py := ebiten.CursorPosition()
	want := a.pointerCursor(px, py)
	if want == a.pointerShape {
		return
	}
	a.pointerShape = want
	setCursorShape(want)
}

// pointerCursor is the pointer over a pixel of the window.
//
// The cell it asks about is the one a click there would go to, the
// sidebar's own rows and all, so the arrow appears over exactly what
// can be grabbed. Asking through a.cellAt also records where the pointer
// is, which is how a click on a pane drawn scaled is routed. Over the
// sidebar the row is the region's own rather than the window's, which
// nothing inside the sidebar minds: none of it answers a cursor yet.
func (a *app) pointerCursor(px, py int) ui.Cursor {
	col, row := a.cellAt(px, py)
	switch {
	case a.scaledHeld.Held() || a.scaledUnder() != nil:
		// A pane drawn scaled takes the event itself, and has nothing to
		// drag.
		return ui.CursorDefault
	case !a.root.Held() && !a.root.Area().Contains(col, row):
		// Off the window, which the pointer is while it is over another
		// one. A drag is the exception: the window keeps the mouse until
		// the button comes up, so the pointer is still ours to draw.
		return ui.CursorDefault
	}
	return a.root.CursorAt(col, row)
}

// setCursorShape tells the window which pointer to draw.
func setCursorShape(c ui.Cursor) { ebiten.SetCursorShape(shapeOf(c)) }

// shapeOf is ebiten's name for a pointer.
func shapeOf(c ui.Cursor) ebiten.CursorShapeType {
	switch c {
	case ui.CursorEWResize:
		return ebiten.CursorShapeEWResize
	case ui.CursorNSResize:
		return ebiten.CursorShapeNSResize
	}
	return ebiten.CursorShapeDefault
}
