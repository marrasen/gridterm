// Package ui is a widget toolkit for text-mode interfaces: panes,
// tabs, menus and dialogs drawn as characters on a grid.
//
// It knows nothing about terminals. A terminal is one widget among
// others, so nothing here imports an emulator or a shell session. The
// toolkit is for building text-mode programs, not terminals in
// particular.
//
// Nothing here is safe for concurrent use. Drawing and event handling
// run on one goroutine.
package ui

import (
	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/input"
)

// Size is how many cells a widget has to draw in.
type Size struct {
	Cols, Rows int
}

// Empty reports whether there is nothing to draw in.
func (s Size) Empty() bool { return s.Cols <= 0 || s.Rows <= 0 }

// Rect is an area of cells at a position. Containers use it to divide
// themselves up; a child is only ever told its Size, because a widget
// that knew its position could offset by it twice.
type Rect struct {
	X, Y, Cols, Rows int
}

// Size returns the rectangle's dimensions.
func (r Rect) Size() Size { return Size{Cols: r.Cols, Rows: r.Rows} }

// Empty reports whether the rectangle has no cells.
func (r Rect) Empty() bool { return r.Cols <= 0 || r.Rows <= 0 }

// Contains reports whether x,y is inside the rectangle, in the same
// coordinates as X and Y.
func (r Rect) Contains(x, y int) bool {
	return x >= r.X && y >= r.Y && x < r.X+r.Cols && y < r.Y+r.Rows
}

// In returns the part of parent this rectangle covers, with its own
// origin, ready to hand to a child. It is clipped to what the parent
// has, so a child promised more room than exists still gets a usable
// view.
func (r Rect) In(parent grid.View) grid.View {
	return parent.Sub(r.X, r.Y, r.Cols, r.Rows)
}

// Local moves a point from the containing coordinates into this
// rectangle's own, which is what routing a mouse event to a child needs.
func (r Rect) Local(x, y int) (int, int) { return x - r.X, y - r.Y }

// Widget is anything that fills a rectangle of cells and draws into it.
//
// Two methods, deliberately. A widget that wants events implements one
// of the optional interfaces below; one that only shows something does
// not have to, and neither has to change when a new kind of event is
// added.
//
// A widget reaches the rest of the program through whatever it was
// built with. A dialog that closes itself is built with the Root it
// will pop itself from, and a widget that runs commands is built with
// the registry. Nothing is threaded through these methods.
type Widget interface {
	// Layout tells the widget how much room it now has. It is called
	// before the first Draw and again whenever the size changes.
	Layout(size Size)

	// Draw paints the widget, with the view's own origin at 0,0.
	//
	// The view is at most the size Layout last gave, and is smaller when
	// the parent has less room than it promised. A widget must draw
	// correctly in a short view rather than trusting the size it was
	// told.
	Draw(v grid.View)
}

// KeyHandler is a widget that takes keyboard events.
//
// Returning true consumes the event: no widget above it and no
// after-binding sees it. A widget that does not recognise a key must
// return false, or it swallows the shortcuts of everything around it.
// A widget that acts on a key must return true, or whatever the key is
// bound to runs as well.
//
// The error is for a widget that ran something and it failed. Returning
// one does not change whether the event was consumed.
type KeyHandler interface {
	Widget
	HandleKey(ev input.Event) (bool, error)
}

// Focusable is a widget that draws differently when it is the one
// receiving keys, or that has a cursor to show.
//
// SetFocus is called when focus arrives and again when it leaves, never
// twice with the same value. A container has to remember whether it
// holds focus and only pass changes on while it does, or a child it
// switches to is told it lost focus it never had.
type Focusable interface {
	Widget
	SetFocus(on bool)
}

// SetFocus tells a widget that focus arrived or left, if it cares. A
// container passes focus to its active child with this.
func SetFocus(w Widget, on bool) {
	if f, ok := w.(Focusable); ok {
		f.SetFocus(on)
	}
}

// HandleKey offers a key to a widget, if it takes keys at all. A
// container forwards to its active child with this.
func HandleKey(w Widget, ev input.Event) (bool, error) {
	if h, ok := w.(KeyHandler); ok {
		return h.HandleKey(ev)
	}
	return false, nil
}
