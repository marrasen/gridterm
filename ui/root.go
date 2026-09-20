package ui

import (
	"errors"
	"fmt"
	"slices"

	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/input"
)

// Root holds the widget tree and decides who sees a key.
//
// Two stages of binding rather than one, because some widgets consume
// everything. A terminal is the obvious case: it has a meaning for every
// key, so a binding that only ran after it would never run at all.
// Accelerators are the shortcuts that win regardless - close, quit, open
// the palette. Ordinary bindings are the ones a focused widget may take
// for itself first.
//
// With no dialog open a key goes to the accelerators, then the widget
// tree, then the ordinary bindings. A dialog changes the first two
// round: the modal sees the key before the accelerators do. Suspending
// the usual meanings is what makes it modal, and a dialog that could not
// keep Escape for itself would be no use. The widget a modal has to beat
// is code the program author wrote; the widget an accelerator has to
// beat is a terminal that answers to everything.
type Root struct {
	// Commands is the registry bindings name. A nil registry means no
	// binding runs.
	Commands *Commands

	// Accelerators run before the widget tree and cannot be swallowed.
	// Keys run after it, so a widget can take them for itself. Either
	// may be nil.
	Accelerators *Keymap
	Keys         *Keymap

	widget Widget
	area   Rect

	// modals is the dialog stack, topmost last. A modal takes the keys
	// the tree would otherwise get.
	modals []Widget

	// held keeps the pointer for whoever took a press.
	held MouseCapture
}

// SetWidget puts a widget in the tree, sizing it to the current area and
// giving it focus if nothing is over it.
func (r *Root) SetWidget(w Widget) {
	if r.widget == w {
		return
	}
	// Only take focus away if the old widget actually had it. Under an
	// open modal it did not, and saying so twice breaks the contract
	// Focusable documents.
	if len(r.modals) == 0 {
		SetFocus(r.widget, false)
	}
	r.widget = w
	r.held.Abandon()
	if w == nil {
		return
	}
	// Not while the root has no area of its own. Telling a widget it has
	// no room is not the same as telling it nothing: a terminal resizes
	// its shell to one column and reflows the scrollback, and the next
	// Layout cannot undo that.
	if !r.area.Empty() {
		w.Layout(r.area.Size())
	}
	if len(r.modals) == 0 {
		SetFocus(w, true)
	}
}

// Widget returns the widget in the tree.
func (r *Root) Widget() Widget { return r.widget }

// Layout sets the area of its parent that the tree fills.
func (r *Root) Layout(area Rect) {
	r.area = area
	if area.Empty() {
		return
	}
	if r.widget != nil {
		r.widget.Layout(area.Size())
	}
	// A modal is told the whole area and places itself within it.
	for _, m := range r.modals {
		m.Layout(area.Size())
	}
}

// Area returns the area the tree fills.
func (r *Root) Area() Rect { return r.area }

// Draw paints the widget tree into the tree's own area of v.
//
// Modals are not drawn here: each is its own layer, so that the tree
// underneath is not repainted to uncover one.
func (r *Root) Draw(v grid.View) {
	drawCursorOwner(r.area.In(v), func(area grid.View) {
		if r.widget != nil {
			r.widget.Draw(area)
		}
	})
}

// DrawModal paints one dialog from the stack into its own view. Each is
// its own layer, so a caller draws them itself rather than through Draw,
// and this is how the cursor gets the same treatment there.
func (r *Root) DrawModal(m Widget, v grid.View) { DrawApart(m, v) }

// DrawApart paints a widget from the tree onto a view of its own.
//
// It is for anything the tree does not paint where it sits: a dialog on
// its own layer, or a panel on a grid of its own. The cursor is settled
// the same way it is for the tree, because a grid has one cursor and no
// idea who owns it: a widget that placed one and then stopped would
// otherwise leave it on that grid for good.
func DrawApart(w Widget, v grid.View) {
	if w == nil {
		return
	}
	drawCursorOwner(v, w.Draw)
}

// drawCursorOwner runs one pass of drawing and settles who holds the
// cursor.
//
// The grid has one cursor and no idea who owns it. Hiding it first and
// letting the focused widget write it back changes the same slot twice
// and dirties a row on every idle frame, so it is hidden afterwards, and
// only when nobody placed one.
func drawCursorOwner(v grid.View, draw func(grid.View)) {
	v.ResetCursorClaim()
	draw(v)
	if !v.CursorClaimed() {
		v.SetCursor(grid.Cursor{})
	}
}

// PushModal puts a dialog on top. It takes focus and every key the tree
// would have seen, until it is popped. A dialog already on the stack is
// refused rather than stacked on itself.
//
// It reports whether the dialog went on, so a caller keeping a stack of
// its own does not drift out of step with this one.
func (r *Root) PushModal(w Widget) bool {
	if w == nil {
		return false
	}
	if slices.Contains(r.modals, w) {
		return false
	}
	SetFocus(r.top(), false)
	// Same on the way in: whatever was mid-drag is not getting its
	// release, and nothing else may have it either.
	r.held.Abandon()
	r.modals = append(r.modals, w)
	if !r.area.Empty() {
		w.Layout(r.area.Size())
	}
	SetFocus(w, true)
	return true
}

// PopModal takes the topmost dialog off and returns it, giving focus
// back to whatever was under it.
func (r *Root) PopModal() Widget {
	if len(r.modals) == 0 {
		return nil
	}
	top := r.modals[len(r.modals)-1]
	SetFocus(top, false)
	// The button is still down and its release belongs to the dialog
	// that is going. Abandoning rather than releasing keeps swallowing
	// until it comes up, so the tree underneath is not handed a
	// button-up for a press it never saw.
	r.held.Abandon()
	r.modals[len(r.modals)-1] = nil
	r.modals = r.modals[:len(r.modals)-1]
	SetFocus(r.top(), true)
	return top
}

// Holding returns the widget the pointer was captured by, or nil when
// nothing holds it. A drag belongs to whoever took the press, wherever
// the pointer has since gone.
func (r *Root) Holding() Widget { return r.held.Holder() }

// Held reports whether a button is down that the tree is keeping the
// pointer for, whether or not the widget that took the press is still
// there.
//
// Holding is not the same question: a widget that left the tree is
// abandoned and its release is owed to nobody, which is a release
// nothing else may have either.
func (r *Root) Held() bool { return r.held.Held() }

// Modal returns the topmost dialog, or nil when none is open.
func (r *Root) Modal() Widget {
	if len(r.modals) == 0 {
		return nil
	}
	return r.modals[len(r.modals)-1]
}

// Modals returns the dialog stack, bottom first. The slice is a copy, so
// a caller can draw every layer without the stack shifting underneath.
//
// Each modal draws itself into its own layer, so a caller draws them
// with DrawModal rather than through Draw.
func (r *Root) Modals() []Widget {
	if len(r.modals) == 0 {
		return nil
	}
	out := make([]Widget, len(r.modals))
	copy(out, r.modals)
	return out
}

// HandleKey offers a key to whoever should see it and reports whether
// anything took it. An unhandled key is the caller's to deal with: in a
// terminal that means sending it to the shell.
//
// Being handled and having failed are separate answers. A widget can
// report that something it ran failed and still pass the key on, and a
// binding naming a command that has gone reports that without eating
// the key. Every failure on the way is joined into the error, so the
// error means "something failed while routing this key" rather than
// "what the key did failed".
func (r *Root) HandleKey(ev input.Event) (handled bool, err error) {
	chord := ChordOf(ev)

	// took keeps every failure rather than the first, because the stage
	// that declined the key and the stage that acted on it can both
	// fail, and it is the one that acted whose failure matters.
	var failed error
	took := func(t bool, e error) bool {
		failed = errors.Join(failed, e)
		return t
	}
	if m := r.Modal(); m != nil {
		if took(HandleKey(m, ev)) {
			return true, failed
		}
		if took(r.run(r.Accelerators, chord)) {
			return true, failed
		}
	} else {
		// Before the accelerators, because an accelerator runs before
		// any widget sees the key and a widget that claimed a chord
		// would never get it.
		if took(r.toClaimer(ev)) {
			return true, failed
		}
		if took(r.run(r.Accelerators, chord)) {
			return true, failed
		}
		if took(HandleKey(r.widget, ev)) {
			return true, failed
		}
	}
	if took(r.run(r.Keys, chord)) {
		return true, failed
	}
	return false, failed
}

// toClaimer offers a key to the focused widget when that widget has
// claimed the chord, and reports whether it took it.
func (r *Root) toClaimer(ev input.Event) (bool, error) {
	if r.widget == nil {
		return false, nil
	}
	w := FocusedLeaf(r.widget)
	c, ok := w.(ChordClaimer)
	if !ok || !c.ClaimsChord(ev) {
		return false, nil
	}
	return HandleKey(w, ev)
}

// HandleMouse offers a mouse event to the topmost modal or else the
// widget tree, translated into that widget's own coordinates.
//
// A press outside the area is ignored. Once a widget has taken a press
// it keeps the pointer until the release, and coordinates are clamped
// to the area, so dragging past the edge selects to the edge instead of
// stranding the drag.
//
// There are no mouse bindings: a click means whatever is under it, so
// there is nothing for a keymap to say about it.
func (r *Root) HandleMouse(ev input.MouseEvent) (bool, error) {
	if r.held.Held() {
		if held := r.held.Holder(); held != nil {
			return r.deliverHeld(held, ev)
		}
		// The widget that took the press has gone. Nothing else may have
		// its release, so the rest of the drag is swallowed.
		//
		// A wheel notch is let through: it has no release to confuse,
		// and scrolling should not stop working because a button is
		// down somewhere.
		switch {
		case ev.Button.IsWheel():
		case ev.Kind == input.MousePress && r.held.Waiting(ev.Button):
			// Pressing a button that is supposedly already down proves
			// its release was lost -- the window never heard it. Letting
			// go here is what stops the pointer being stuck for good.
			r.held.Release()
		default:
			r.held.Take(nil, ev)
			return false, nil
		}
	}

	top := r.top()
	if top == nil {
		return false, nil
	}
	depth := len(r.modals)
	// Only a press that starts something has to land inside. A wheel
	// notch or a motion report in the pixels left over below the last
	// whole row still belongs to the tree.
	starts := ev.Kind == input.MousePress && !ev.Button.IsWheel()
	if starts && !r.area.Contains(ev.Col, ev.Row) {
		return false, nil
	}

	local := ev
	local.Col, local.Row = r.clampInto(r.area, ev.Col, ev.Row)
	local.Col, local.Row = r.area.Local(local.Col, local.Row)
	handled, err := HandleMouse(top, local)
	if starts && handled {
		var leaf Widget
		switch {
		case r.top() == top:
			// Remember the widget itself, not the way down to it.
			// Containers come and go while a button is held, and the path
			// is worked out again for every event that follows.
			if at, _, ok := LeafAt(top, r.area.Size().rect(), local.Col, local.Row); ok {
				leaf = at
			}
		case len(r.modals) > depth:
			// The press opened a dialog, so the rest of the gesture is
			// the dialog's. Without this, pressing a menu title and
			// dragging into the menu highlights nothing: the standard way
			// to use a menu would do nothing at all.
			leaf = r.top()
		}
		// The pointer is taken either way, with nobody holding it when
		// there is nobody to hold it -- a dialog that dismissed itself on
		// this very press. The release is still owed, and giving it to
		// whatever is under the pointer would report a button-up for a
		// press that widget never saw.
		r.held.Take(leaf, local)
	}
	return handled, err
}

// deliverHeld sends an event straight to the widget holding the pointer,
// wherever the tree has since put it.
//
// A widget that has left the tree gets nothing more, and neither does
// anything else: a release belongs to whatever took the press, and
// handing it to whoever happens to be under the pointer would clear a
// selection nobody made and report a button-up no program pressed.
func (r *Root) deliverHeld(held Widget, ev input.MouseEvent) (bool, error) {
	area, shown := AreaOf(r.top(), r.area.Size().rect(), held)
	if !shown {
		r.held.Abandon()
		r.held.Take(nil, ev)
		return false, nil
	}
	local := ev
	local.Col, local.Row = r.area.Local(ev.Col, ev.Row)
	local.Col, local.Row = r.clampInto(area, local.Col, local.Row)
	local.Col, local.Row = area.Local(local.Col, local.Row)
	r.held.Take(held, local)
	return HandleMouse(held, local)
}

// clampInto pulls a point inside an area, so dragging past an edge
// selects to the edge rather than to a column that is not there.
func (r *Root) clampInto(area Rect, x, y int) (int, int) {
	return min(max(x, area.X), area.X+max(area.Cols-1, 0)),
		min(max(y, area.Y), area.Y+max(area.Rows-1, 0))
}

// CursorAt is the pointer over a cell, in the same coordinates a mouse
// event is in.
//
// A dialog on top wants the ordinary pointer: the tree behind it cannot
// be clicked, so nothing in it can be dragged either.
//
// A drag belongs to whoever took the press, so the pointer does too. It
// keeps whatever shape that widget asks for however far it has wandered,
// which is what stops the resize arrow flickering back to an arrow as
// the divider is pulled across the pane beside it.
func (r *Root) CursorAt(col, row int) Cursor {
	if len(r.modals) > 0 || r.widget == nil {
		return CursorDefault
	}
	col, row = r.area.Local(col, row)
	held := r.held.Holder()
	if held == nil {
		c, _ := CursorAt(r.widget, col, row)
		return c
	}
	area, shown := AreaOf(r.widget, r.area.Size().rect(), held)
	if !shown {
		return CursorDefault
	}
	col, row = area.Local(col, row)
	c, _ := CursorAt(held, col, row)
	return c
}

// AreaOf returns where a widget sits in the tree, in the tree's own
// coordinates. It saves a caller having to know the root's area and get
// it wrong.
//
// It searches the tree, not the dialog on top of it. A dialog is drawn
// on a layer of its own, so where the tree puts things is a question
// about the tree whether or not one is open.
func (r *Root) AreaOf(target Widget) (Rect, bool) {
	return AreaOf(r.widget, r.area.Size().rect(), target)
}

// run looks a chord up in one keymap and runs what it finds.
//
// A binding naming a command that is not registered does not consume the
// key. Bindings outlive the panes that register commands, and a
// stale one that swallowed its key would kill that key for good.
func (r *Root) run(keys *Keymap, chord Chord) (bool, error) {
	if keys == nil || r.Commands == nil {
		return false, nil
	}
	id, ok := keys.Lookup(chord)
	if !ok {
		return false, nil
	}
	if _, known := r.Commands.Lookup(id); !known {
		return false, fmt.Errorf("binding %s names no command %q", chord, id)
	}
	return true, r.Commands.Run(id)
}

// top returns the widget that holds focus.
func (r *Root) top() Widget {
	if len(r.modals) > 0 {
		return r.modals[len(r.modals)-1]
	}
	return r.widget
}
