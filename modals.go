package main

import (
	"fmt"
	"image/color"

	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/render"
	"github.com/marrasen/gridterm/ui"
)

// modal is one dialog on the stack together with the layer it draws on.
type modal struct {
	w     ui.Widget
	g     *grid.Grid
	layer *render.Layer

	// onHidden tells whoever opened the dialog that it has gone. The
	// close function is not enough on its own: a dialog can go because
	// one underneath it was taken away, and nobody called its close.
	onHidden func()
}

// showModal puts a dialog on the stack with a layer of its own and
// returns the function that takes both away.
//
// A layer each rather than one shared: a dialog paints its whole view so
// that a box which shrank leaves nothing of the old one behind, and two
// dialogs doing that on one layer would each wipe the other.
func (a *app) showModal(w ui.Widget, onHidden func()) func() {
	// No colours at all: the layer sits over the widget tree, and a
	// background with any alpha would blank the window behind it. A
	// dialog paints its own box opaquely.
	g := grid.New(a.lastSize[0], a.lastSize[1], color.RGBA{}, color.RGBA{})
	m := &modal{
		w:        w,
		g:        g,
		layer:    &render.Layer{Grid: g, Transparent: true},
		onHidden: onHidden,
	}
	if !a.root.PushModal(w) {
		// Already on the stack. Keeping a second record of it here would
		// leave a dialog drawn on a layer that nothing routes keys to.
		return func() {}
	}
	a.modals = append(a.modals, m)
	a.comp.Add(m.layer)
	// No markDirty: the tree has not changed. Adding a layer changes the
	// compositor's placements, which is what makes it re-blit everything
	// from the textures it already has.
	return func() { a.hideModal(m) }
}

// hideModal takes a dialog and its layer away, along with anything
// stacked on top of it: the stack is ordered, and a dialog cannot be
// pulled out from under the one covering it.
//
// A dialog that is not on the stack is already gone, so closing one
// twice does nothing the second time.
func (a *app) hideModal(m *modal) {
	at := -1
	for i, have := range a.modals {
		if have == m {
			at = i
			break
		}
	}
	if at < 0 {
		return
	}
	// Copied before the slice is cut, or clearing the slots would empty
	// the list of who to tell.
	doomed := make([]*modal, len(a.modals)-at)
	copy(doomed, a.modals[at:])

	for i := len(a.modals) - 1; i >= at; i-- {
		if got := a.root.PopModal(); got != a.modals[i].w {
			// The two stacks have to hold the same dialogs in the same
			// order. Carrying on would draw one nothing routes keys to.
			a.logError(fmt.Errorf("modal stack out of step: popped %T, expected %T",
				got, a.modals[i].w))
		}
		a.comp.Remove(a.modals[i].layer)
		a.modals[i] = nil
	}
	a.modals = a.modals[:at]

	// Told once the stack has settled, topmost first, so an owner that
	// opens something else in reply is not building on a stack still
	// being taken apart.
	for i := len(doomed) - 1; i >= 0; i-- {
		if doomed[i].onHidden != nil {
			doomed[i].onHidden()
		}
	}
}

// drawModals paints each dialog onto its own layer.
func (a *app) drawModals() {
	for _, m := range a.modals {
		a.root.DrawModal(m.w, m.g.View())
	}
}

// resizeModals follows the window. A dialog places itself within the
// area it is given, so a layer left at the old size would measure it for
// a window that is not there any more.
func (a *app) resizeModals(cols, rows int) {
	for _, m := range a.modals {
		m.g.Resize(cols, rows)
	}
}
