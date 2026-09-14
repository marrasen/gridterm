package ui

import (
	"errors"
	"fmt"

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
	if w == nil {
		return
	}
	w.Layout(r.area.Size())
	if len(r.modals) == 0 {
		SetFocus(w, true)
	}
}

// Widget returns the widget in the tree.
func (r *Root) Widget() Widget { return r.widget }

// Layout sets the area of its parent that the tree fills.
func (r *Root) Layout(area Rect) {
	r.area = area
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
	if r.widget != nil {
		r.widget.Draw(r.area.In(v))
	}
}

// PushModal puts a dialog on top. It takes focus and every key the tree
// would have seen, until it is popped. A dialog already on the stack is
// ignored rather than stacked on itself.
func (r *Root) PushModal(w Widget) {
	if w == nil {
		return
	}
	for _, have := range r.modals {
		if have == w {
			return
		}
	}
	SetFocus(r.top(), false)
	r.modals = append(r.modals, w)
	w.Layout(r.area.Size())
	SetFocus(w, true)
}

// PopModal takes the topmost dialog off and returns it, giving focus
// back to whatever was under it.
func (r *Root) PopModal() Widget {
	if len(r.modals) == 0 {
		return nil
	}
	top := r.modals[len(r.modals)-1]
	SetFocus(top, false)
	r.modals[len(r.modals)-1] = nil
	r.modals = r.modals[:len(r.modals)-1]
	SetFocus(r.top(), true)
	return top
}

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
// Each modal draws itself into its own layer. A caller placing that
// layer against the tree's own grid has to apply Area first, the way
// Draw does for the tree.
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

// run looks a chord up in one keymap and runs what it finds.
//
// A binding naming a command that is not registered does not consume the
// key. Bindings outlive the panes and tabs that register commands, and a
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
