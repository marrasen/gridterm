// Package ui is a widget toolkit for text-mode interfaces: panes,
// decks, menus and dialogs drawn as characters on a grid.
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
// after-binding sees it. A widget that acts on a key must return true,
// or whatever the key is bound to runs as well.
//
// What to do with a key the widget does not recognise depends on what
// the widget is on the screen for:
//
//   - A widget that shares the screen -- a pane, a list, a menu
//     drop-down, the palette -- returns false, or it swallows the
//     shortcuts of everything around it. A menu that took F10 would be
//     a menu that cannot be closed with the key that opened it.
//   - A modal that covers the screen -- a form, a notice, a chooser --
//     returns true for every key it does not use. Behind it is a pane
//     the user cannot see, and Root.HandleKey offers a declined key to
//     the accelerators: the paste chord would type into a shell hidden
//     under the dialog, and the menu chord would open a menu over it.
//     The way out is a chord the window hands the dialog itself, the
//     way it hands a notice its copy chord.
//
// A modal must also act only on the chords it offers. Ctrl+Tab is not
// Tab, and moving a dialog's focus on it answers a key the user pressed
// for something else; isPlainKey is the test.
//
// The error is for a widget that ran something and it failed. Returning
// one does not change whether the event was consumed.
type KeyHandler interface {
	Widget
	HandleKey(ev input.Event) (bool, error)
}

// ChordClaimer is a widget that takes some chords before the window's
// accelerators do.
//
// An accelerator runs before any widget sees the key, which is what
// stops a terminal swallowing the window's own shortcuts. A widget with
// a meaning of its own for one of those chords says so here, and while
// it has the focus the key reaches it instead.
//
// Claim as little as possible. Every chord claimed is one the window's
// shortcut stops working on, and the user is given no sign of it.
type ChordClaimer interface {
	Widget

	// ClaimsChord reports whether this widget wants the key itself. It
	// is asked only while the widget has the focus, and a widget that
	// claims a chord must then handle it.
	ClaimsChord(ev input.Event) bool
}

// isPlainKey reports whether an event is a key a dialog offers: no
// modifier, or the one chord that is spelled with one, Shift+Tab.
func isPlainKey(ev input.Event) bool {
	return ev.Mods == 0 || ev.Key == input.KeyTab && ev.Mods == input.ModShift
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

// RowSpacer is a widget that wants room around some of its rows.
//
// RowPads answers for a box of the given height, one entry per row from
// the top of it. An answer shorter than the box says the rows past it
// want nothing.
//
// It must answer without changing the widget. Padding is room the grid
// gains, so a row of room is a row the widget does not get: the height
// and the room have to be settled together, and the widget is asked
// more than once, for different heights, while that happens.
//
// The slice may be reused between calls, so read it before asking
// again.
//
// Only a widget on a grid of its own can have this: a grid has one set
// of row heights, so a widget sharing one with a terminal would put its
// gaps through the terminal's lines as well.
type RowSpacer interface {
	RowPads(rows int) []grid.Pad
}

// Sized is a widget that reports the size it draws at.
//
// For a widget a container laid out, that is the size it was given. A
// Deck lays out every pane it holds, hidden ones as well, so a pane
// behind another answers with the size it has now.
type Sized interface {
	Widget
	Size() Size
}

// Boxed is a widget that fills only part of the view it is given and
// says which part, in that view's own coordinates.
//
// A dialog placed in the middle of the window is the case: the view it
// draws through covers everything, and only the box is the dialog. It
// lets whatever is showing the widget treat that part differently --
// putting frosted glass behind it, say -- without knowing how the widget
// decided where to sit.
//
// An empty Rect means there is nothing on screen.
type Boxed interface {
	Widget
	Box() Rect
}

// GestureCanceller is a widget that keeps something between a press and
// its release: a selection being dragged out, a strip holding a click on
// one of its labels.
//
// CancelGesture says the release is never coming. A dialog opening, or
// the widget leaving the screen, ends a gesture without one, and only
// whatever routes the mouse knows that has happened. A widget left
// waiting carries on as though the button were still down.
type GestureCanceller interface {
	Widget
	CancelGesture()
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

// FocusesFirst is a widget that takes a left press moving the keys to it
// as nothing but that move.
//
// Answer true where a press does something as well, such as starting a
// selection or opening what is under the pointer. The container then
// keeps that press, and the widget's own first press is the next one.
// A press that moves no keys is delivered whatever this answers.
type FocusesFirst interface {
	Widget
	FocusesFirst() bool
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
// without knowing what kind of container each node is, so a split, a
// deck and anything written later all work with the same code.
type Container interface {
	Widget

	// Children returns the children, in layout order. A container with
	// children must report one of them as Focused.
	//
	// The slice belongs to the container. A caller may read it and pass
	// it on, and must not write to it or keep it past the next call: an
	// implementation is free to hand back the same backing array every
	// time, and every one here does. The tree is walked once a frame
	// per pane, so a slice made each time is a slice made sixty times a
	// second for nothing.
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
	// An area it does report must be the one Layout used for that child.
	// Anything asking where a widget is on screen -- routing a drag,
	// placing a menu under the word that opened it -- believes this, and
	// a container that works it out twice has two chances to disagree
	// with itself.
	//
	// A child that is laid out but not on screen, such as a pane that is
	// not the one showing, reports false: it has a size but nowhere to
	// be clicked.
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

// Waiting reports whether the capture is still waiting for b to come up.
//
// A press of that same button proves its release was lost, which is the
// only safe reason to let go early. A press of any other button proves
// nothing: the first one may well still be down.
func (c *MouseCapture) Waiting(b input.MouseButton) bool {
	return c.down && c.button == b
}

// Abandon gives up on the widget but keeps waiting for the button, for
// when the widget has gone and its release belongs to nobody.
func (c *MouseCapture) Abandon() {
	c.cancel()
	c.w = nil
}

// Release drops the capture entirely, for when the tree is rearranged
// under it and the next press should start afresh.
func (c *MouseCapture) Release() {
	c.cancel()
	c.w, c.down = nil, false
}

// cancel tells the holder its release is not coming, so it does not go
// on behaving as though the button were still down.
func (c *MouseCapture) cancel() {
	if g, ok := c.w.(GestureCanceller); ok {
		g.CancelGesture()
	}
}

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
