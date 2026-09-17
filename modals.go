package main

import (
	"fmt"
	"image"
	"image/color"

	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/render"
	"github.com/marrasen/gridterm/ui"
)

// How the glass behind a dialog looks: how far the blur reaches, how
// much of the window's own background is laid over it, how far the
// colour is lifted back after the blur averaged it away, and how much
// noise and rim light finish it.
const (
	frostRadius     = 7
	frostTint       = 0xc4
	frostSaturation = 1.4
	frostGrain      = 0.04
	frostEdge       = 0.07
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
	// The same padding as the window under it, before the first frame
	// rather than on the next one: two grids that did not agree on where
	// a row sits would draw the dialog a few pixels off the text it
	// covers, and the dialog would then jump into place.
	a.padGrid(g)
	m := &modal{
		w:        w,
		g:        g,
		layer:    &render.Layer{Grid: g, Transparent: true, Frost: a.frost()},
		onHidden: onHidden,
	}
	// A drag on a pane drawn scaled is routed outside the tree, so
	// PushModal does not reach it: its release is not coming either.
	a.scaledHeld.Abandon()
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

// drawModals paints each dialog onto its own layer and moves the glass
// behind it to wherever the dialog has put itself.
//
// The panel follows the dialog every frame rather than being placed
// once: a palette box shrinks as the query narrows it, and a menu moves
// when the window is resized.
func (a *app) drawModals() {
	cw, ch := a.renderer.CellSize()
	a.renderer.Measure(a.g, &a.geo)
	for _, m := range a.modals {
		a.root.DrawModal(m.w, m.g.View())
		if m.layer.Frost == nil {
			continue
		}
		m.layer.Frost.Rect = image.Rectangle{}
		boxed, ok := m.w.(ui.Boxed)
		if !ok {
			continue
		}
		if box := boxed.Box(); !box.Empty() {
			// The cells, not their outer boxes: the glass has to land on the
			// same pixels the dialog's glyphs do.
			left, width := a.geo.CellsX(box.X, box.X+box.Cols)
			top, height := a.geo.CellsY(box.Y, box.Y+box.Rows)
			m.layer.Frost.Rect = image.Rect(left, top, left+width, top+height)
			// The corner is measured in cells too, so changing the font
			// size with a dialog open keeps it in proportion.
			m.layer.Frost.Corner = float32(min(cw, ch))
		}
	}
}

// frost describes the glass behind a dialog, in the window's colours.
//
// The dialog itself draws no background: the panel is the background, so
// the pane behind shows through it blurred. Without the tint the text
// would sit straight on a blurred picture of a shell and be unreadable.
func (a *app) frost() *render.Frost {
	cw, ch := a.renderer.CellSize()
	tint := a.colours.BG
	tint.A = frostTint
	return &render.Frost{
		Radius:     frostRadius,
		Corner:     float32(min(cw, ch)),
		Tint:       tint,
		Saturation: frostSaturation,
		Grain:      frostGrain,
		Edge:       frostEdge,
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
