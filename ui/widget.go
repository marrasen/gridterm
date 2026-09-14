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

// rect returns the size as a rectangle at the origin, for the common
// case of asking about a tree in its own coordinates.
func (s Size) rect() Rect { return Rect{Cols: s.Cols, Rows: s.Rows} }

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

// MouseHandler is a widget that takes mouse events.
//
// Col and Row on the event are in the widget's own coordinates, so a
// container translates before forwarding. Returning true consumes the
// event the way KeyHandler does.
type MouseHandler interface {
	Widget
	HandleMouse(ev input.MouseEvent) (bool, error)
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

// Container is a widget made of other widgets.
//
// Implementing it lets the tree helpers walk and rearrange a layout
// without knowing what kind of container each node is, so a split, a tab
// strip and anything written later all work with the same code.
type Container interface {
	Widget

	// Children returns the children, in layout order. A container with
	// children must report one of them as Focused.
	Children() []Widget

	// Focused returns the child that receives events, or nil when there
	// are no children.
	Focused() Widget

	// Focus moves focus to one of the children, reporting whether it is
	// one. Without it nothing could point a chain of containers at a
	// widget without knowing what each one is.
	Focus(w Widget) bool

	// Replace swaps one child for another, reporting whether old was
	// there. Focus follows: replacing the focused child focuses its
	// replacement, and the old child is told focus left.
	Replace(old, new Widget) bool

	// Remove takes a child out and reports what should stand in the
	// container's own place: itself when it can carry on, its last
	// remaining child when removing this one leaves it with nothing to
	// divide, or nil when nothing is left at all.
	//
	// Returning anything but itself is a promise that the container is
	// finished and may be thrown away, so it need not tidy up the slot
	// it is about to lose. It must still hand focus on, because the
	// child it held may outlive it.
	//
	// It reports false when w is not a child.
	Remove(w Widget) (Widget, bool)

	// ChildArea returns where a child sits inside this container, in the
	// container's own coordinates. It reports false when w is not a
	// child, or when there is no room to show it.
	//
	// It must give the same rectangle Layout used for that child.
	// Anything asking where a widget is on screen -- routing a drag,
	// placing a menu under the word that opened it -- believes this, and
	// a container that works it out twice has two chances to disagree
	// with itself.
	ChildArea(w Widget) (Rect, bool)
}

// MouseCapture remembers which widget took a mouse press, so every move
// and release goes to it until the button comes up, however far the
// pointer has wandered.
//
// Without it a drag that leaves a widget never finishes, and the widget
// waits for a release it will not get.
//
// It holds the widget, not the way down to it. Containers come and go
// while a button is held -- a pane splits, a neighbour closes -- and a
// remembered path would be wrong by the next event, where the widget
// itself is still the right answer or is gone entirely.
type MouseCapture struct {
	w      Widget
	button input.MouseButton

	// down stays true while the capturing button is held, even once the
	// widget has gone, so the release it was waiting for is swallowed
	// rather than handed to whoever the pointer has wandered over.
	down bool
}

// Holder returns the widget holding the pointer, or nil. It is nil while
// a button is still down if the widget has left the tree.
func (c *MouseCapture) Holder() Widget { return c.w }

// Held reports whether a button is down that the pointer is being kept
// for, whether or not the widget it was kept for still exists.
func (c *MouseCapture) Held() bool { return c.down }

// Abandon gives up on the widget but keeps waiting for the button, for
// when the widget has gone and its release belongs to nobody.
func (c *MouseCapture) Abandon() { c.w = nil }

// Release drops the capture entirely, for when the tree is rearranged
// under it and the next press should start afresh.
func (c *MouseCapture) Release() { c.w, c.down = nil, false }

// Take records that w took this event, if it is a press that will be
// released. A wheel notch is a press with no release, so taking one
// would never let go.
func (c *MouseCapture) Take(w Widget, ev input.MouseEvent) {
	switch {
	case ev.Kind == input.MousePress && !ev.Button.IsWheel():
		if !c.down {
			c.w, c.button, c.down = w, ev.Button, true
		}
	case ev.Kind == input.MouseRelease && c.down && ev.Button == c.button:
		// Only the button that took the pointer gives it back. Tapping
		// another mid-drag must not end the drag.
		c.w, c.down = nil, false
	}
}

// HandleMouse offers a mouse event to a widget, if it takes them. The
// event must already be in that widget's coordinates.
func HandleMouse(w Widget, ev input.MouseEvent) (bool, error) {
	if h, ok := w.(MouseHandler); ok {
		return h.HandleMouse(ev)
	}
	return false, nil
}
