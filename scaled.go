package main

import (
	"github.com/marrasen/gridterm/glyph"
	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/input"
	"github.com/marrasen/gridterm/render"
	"github.com/marrasen/gridterm/ui"
	"github.com/marrasen/gridterm/ui/term"
)

// mostScaledPixels is the biggest texture a screen sized by a watcher
// may be drawn on, each way.
//
// The size is a watcher's to choose, so it is bounded rather than
// trusted: 4096 is a texture every GPU this runs on can make, and a
// screen past it is left to the tree and clipped, as it was before.
const mostScaledPixels = 4096

// mostPanePixels is the biggest texture a picture of one of this
// window's own panes may be drawn on, which is what the GPU will make
// rather than how far a size is trusted: 8192 is what Direct3D
// guarantees from feature level 10.
const mostPanePixels = 8192

// scaledPane is a pane whose screen is bigger than the room the layout
// has for it, drawn on a grid of its own and blitted to fit.
//
// A held screen is the case: the size is the watcher's, and copied cell
// by cell into the room this window has, the columns and rows past it
// would not be there at all. The picture keeps its shape and is centred
// in that room.
type scaledPane struct {
	pane  *term.Terminal
	full  fullScreen
	g     *grid.Grid
	geo   render.Geometry
	layer *render.Layer

	// boxLeft, boxTop, boxWidth and boxHeight are the room the layout
	// gave the pane, in pixels.
	boxLeft, boxTop, boxWidth, boxHeight int

	// left, top, width and height are where the picture lands inside
	// that room, in pixels.
	left, top, width, height int

	// scale is how much the picture is shrunk to get there.
	scale float64
}

// newScaledPane puts a pane's whole screen on a grid and a layer of its
// own, hidden until it has been placed.
func newScaledPane(pane *term.Terminal, g *grid.Grid) *scaledPane {
	s := &scaledPane{pane: pane, full: fullScreen{pane}, g: g}
	s.layer = &render.Layer{Grid: g, Hidden: true, Geom: &s.geo}
	return s
}

// place sizes the grid to the pane's screen and works out where that
// screen lands in the room the layout gave the pane.
func (s *scaledPane) place(area ui.Rect, geo *render.Geometry, m glyph.Metrics) {
	size := s.pane.Size()
	s.g.Resize(max(size.Cols, 1), max(size.Rows, 1))
	s.geo.Layout(s.g, m)

	s.boxLeft, s.boxWidth = geo.ColBox(area.X, area.X+area.Cols)
	s.boxTop, s.boxHeight = geo.RowBox(area.Y, area.Y+area.Rows)
	w, h := s.geo.Width(), s.geo.Height()
	s.left, s.top, s.scale = fitInside(w, h, s.boxLeft, s.boxTop, s.boxWidth, s.boxHeight)
	s.width, s.height = int(float64(w)*s.scale), int(float64(h)*s.scale)

	s.layer.X, s.layer.Y, s.layer.Scale = s.left, s.top, s.scale
	s.layer.Hidden = false
}

// draw paints the pane's whole screen onto its own grid.
func (s *scaledPane) draw() { ui.DrawApart(s.full, s.g.View()) }

// contains reports whether a pixel is in the room the pane was given.
func (s *scaledPane) contains(px, py int) bool {
	return px >= s.boxLeft && px < s.boxLeft+s.boxWidth &&
		py >= s.boxTop && py < s.boxTop+s.boxHeight
}

// cellAt is which of the pane's own cells a pixel is on, through the
// scale the screen is drawn at.
//
// A pixel in the band around the picture counts as the nearest cell of
// it, the way a drag past the edge of the window selects to the edge.
//
// The pixel is exact, but a move is only read when the pointer changes
// one of the window's cells, so a drag moves the selection a window
// cell at a time however many of the pane's cells that is.
func (s *scaledPane) cellAt(px, py int) (col, row int) {
	cols, rows := s.g.Size()
	if s.scale <= 0 {
		return 0, 0
	}
	x := int(float64(px-s.left) / s.scale)
	y := int(float64(py-s.top) / s.scale)
	col = min(max(s.geo.ColAt(x), 0), max(cols-1, 0))
	row = min(max(s.geo.RowAt(y), 0), max(rows-1, 0))
	return col, row
}

// fitInside is where a picture of w by h pixels lands inside a box, and
// how much it is shrunk to get there.
//
// It keeps the picture's shape and centres it, and never blows it up:
// the room can be a pixel or two wider than the screen needs, and
// stretching the text to fill that would be worse than the band.
func fitInside(w, h, left, top, width, height int) (x, y int, scale float64) {
	if w <= 0 || h <= 0 {
		return left, top, 1
	}
	scale = min(float64(width)/float64(w), float64(height)/float64(h), 1)
	return left + (width-int(float64(w)*scale))/2,
		top + (height-int(float64(h)*scale))/2,
		scale
}

// fullScreen draws a terminal's whole screen rather than the blank the
// tree gets while the host is drawing it.
type fullScreen struct{ *term.Terminal }

func (f fullScreen) Draw(v grid.View) { f.DrawScreen(v) }

// placeScaled gives every pane whose screen is bigger than its room a
// grid and a layer of its own, and takes them away again when the screen
// fits.
//
// Every frame, because the size of a held screen is set by somebody on
// another machine and changes when they resize their window.
func (a *app) placeScaled() {
	// Nothing to place before the window has a grid and a compositor to
	// put a layer on.
	if a.comp == nil || a.g == nil {
		return
	}
	for pane, s := range a.scaled {
		if _, live := a.panes[pane]; !live {
			a.dropScaled(pane, s)
		}
	}
	cellW, cellH := a.renderer.CellSize()
	for pane := range a.panes {
		s := a.scaled[pane]
		// The cheap questions first: an idle window asks the tree where
		// nothing is, and walking it allocates.
		if !overflows(pane) || !fitsATexture(pane.Size(), cellW, cellH) {
			if s != nil {
				a.dropScaled(pane, s)
			}
			continue
		}
		area, shown := a.paneArea(pane)
		if !shown {
			if s != nil {
				a.dropScaled(pane, s)
			}
			continue
		}
		if s == nil {
			s = newScaledPane(pane, grid.New(0, 0, a.colours.FG, a.colours.BG))
			a.scaled[pane] = s
			a.addUnderModals(s.layer)
			pane.SetElsewhere(true)
			a.markDirty()
		}
		s.place(area, &a.geo, a.renderer.Metrics())
	}
}

// drawScaled paints each of those screens onto its own grid.
func (a *app) drawScaled() {
	for _, s := range a.scaled {
		s.draw()
	}
}

// dropScaled gives the pane back to the tree and takes its layer away.
func (a *app) dropScaled(pane *term.Terminal, s *scaledPane) {
	pane.SetElsewhere(false)
	a.comp.Remove(s.layer)
	delete(a.scaled, pane)
	if a.scaledHeld.Holder() == ui.Widget(pane) {
		// Its release is not coming, and nothing else may have it.
		a.scaledHeld.Abandon()
	}
	a.markDirty()
}

// overflows reports whether a pane's screen is bigger than the room the
// layout has for it, which only a held screen is.
func overflows(pane *term.Terminal) bool {
	// The room the screen is drawn in rather than the pane's own: a line
	// above the screen takes a row, and a screen that no longer fits
	// under it has to be drawn on a layer of its own.
	size, box := pane.Size(), pane.ScreenRoom()
	return pane.Held() && (size.Cols > box.Cols || size.Rows > box.Rows)
}

// fitsAPaneTexture reports whether a picture of one of this window's own
// panes can be drawn on a texture.
func fitsAPaneTexture(size ui.Size, cellW, cellH int) bool {
	// Under rather than up to: ebiten pads an image by a pixel before
	// it goes on an atlas, and one exactly this wide would not fit.
	return size.Cols*cellW < mostPanePixels && size.Rows*cellH < mostPanePixels
}

// fitsATexture reports whether a screen that size can be drawn on a
// texture of its own.
func fitsATexture(size ui.Size, cellW, cellH int) bool {
	return size.Cols*cellW <= mostScaledPixels && size.Rows*cellH <= mostScaledPixels
}

// paneArea is where a pane sits in the window, in the window's cells.
func (a *app) paneArea(pane ui.Widget) (ui.Rect, bool) {
	if a.root.Widget() == nil {
		return ui.Rect{}, false
	}
	cols, rows := a.g.Size()
	return ui.AreaOf(a.root.Widget(), ui.Rect{Cols: cols, Rows: rows}, pane)
}

// addUnderModals puts a layer on the stack below any dialog that is
// open, because Add puts it on top and a pane must not cover a dialog.
func (a *app) addUnderModals(l *render.Layer) {
	a.comp.Add(l)
	if len(a.modals) == 0 {
		return
	}
	layers := a.comp.Layers()
	at := len(layers) - 1
	if at < 0 || layers[at] != l {
		return
	}
	first := at
	for i, have := range layers[:at] {
		if a.isModalLayer(have) {
			first = i
			break
		}
	}
	copy(layers[first+1:at+1], layers[first:at])
	layers[first] = l
}

// isModalLayer reports whether a layer belongs to a dialog.
func (a *app) isModalLayer(l *render.Layer) bool {
	for _, m := range a.modals {
		if m.layer == l {
			return true
		}
	}
	return false
}

// routeMouse sends one mouse event to whoever should have it: the scaled
// pane holding the pointer, one under the pointer, or the widget tree.
//
// A pane drawn scaled is routed here rather than through the tree,
// because the tree can only name a cell inside the room it gave the
// pane and the screen has more cells than that.
func (a *app) routeMouse(ev input.MouseEvent) (bool, error) {
	if a.scaledHeld.Held() {
		if s := a.holdingScaled(); s != nil {
			return a.deliverScaled(s, ev, true)
		}
		// The pane that took the press is not drawn scaled any more. Its
		// release belongs to nobody, the same rule Root keeps for a
		// widget that left the tree.
		switch {
		case ev.Button.IsWheel():
		case ev.Kind == input.MousePress && a.scaledHeld.Waiting(ev.Button):
			// Pressing a button that is supposedly already down proves
			// its release was lost.
			a.scaledHeld.Release()
		default:
			a.scaledHeld.Take(nil, ev)
			return false, nil
		}
	}
	s := a.scaledUnder()
	if s == nil {
		return a.root.HandleMouse(ev)
	}
	return a.deliverScaled(s, ev, false)
}

// deliverScaled maps the pointer through the scale and hands the event
// to the pane.
//
// held says the pane already has the pointer, in which case it keeps it
// until the button it took comes up. A fresh press is only taken if the
// pane acted on it, so a button it declines starts no drag.
func (a *app) deliverScaled(s *scaledPane, ev input.MouseEvent, held bool) (bool, error) {
	local := ev
	local.Col, local.Row = s.cellAt(a.pointer[0], a.pointer[1])
	if held {
		a.scaledHeld.Take(s.pane, local)
		return ui.HandleMouse(s.pane, local)
	}
	starts := ev.Kind == input.MousePress && !ev.Button.IsWheel()
	if starts && ui.FocusedLeaf(a.root.Widget()) != ui.Widget(s.pane) {
		// Clicking a pane is how the mouse moves focus, the same as in
		// the tree, whether or not the pane wants the button.
		a.focus(s.pane)
		// And the same rule the tree keeps, because the user cannot tell
		// a scaled pane from an ordinary one: a left press that only
		// moves the keys stops there.
		if ev.Button == input.MouseLeft && s.pane.FocusesFirst() {
			return true, nil
		}
	}
	handled, err := ui.HandleMouse(s.pane, local)
	if starts && handled {
		a.scaledHeld.Take(s.pane, local)
	}
	return handled, err
}

// holdingScaled is the scaled pane the pointer is being kept for, or nil
// when the pane it was kept for is not drawn scaled any more.
func (a *app) holdingScaled() *scaledPane {
	pane, ok := a.scaledHeld.Holder().(*term.Terminal)
	if !ok {
		return nil
	}
	return a.scaled[pane]
}

// scaledUnder is the scaled pane the pointer is over, or nil when the
// tree should have the event.
func (a *app) scaledUnder() *scaledPane {
	switch {
	case len(a.scaled) == 0:
		return nil
	case a.root.Modal() != nil:
		// A dialog is drawn on the window's own grid, over the pane as
		// much as anywhere else.
		return nil
	case a.root.Held():
		// A drag that began somewhere else belongs to whoever took the
		// press, and a release the tree is still owed belongs to nobody.
		return nil
	}
	for _, s := range a.scaled {
		if s.contains(a.pointer[0], a.pointer[1]) {
			return s
		}
	}
	return nil
}
