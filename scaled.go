package main

import (
	"github.com/marrasen/gridterm/glyph"
	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/input"
	"github.com/marrasen/gridterm/render"
	"github.com/marrasen/gridterm/ui"
	"github.com/marrasen/gridterm/ui/term"
)

// scaledPane is a pane whose screen is bigger than the room the layout
// has for it, drawn on a grid of its own and blitted to fit.
//
// A held screen is the case: somebody watching from another machine set
// the size, so the screen is the shape their window wants and this one
// draws it in whatever room its own layout gives that pane. Copied cell
// by cell into that room, the columns and rows past it would simply not
// be there.
//
// The picture keeps its shape and is centred in the room, so what is
// left over is a band of the same width at each end rather than all of
// it at one.
type scaledPane struct {
	pane  *term.Terminal
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
	s := &scaledPane{pane: pane, g: g}
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
	// Never above 1: the room can be a pixel or two wider than the
	// screen needs -- the window shares out the pixels it has over --
	// and blowing the text up to fill that would be worse than the band.
	s.scale = min(float64(s.boxWidth)/float64(w), float64(s.boxHeight)/float64(h), 1)
	s.width, s.height = int(float64(w)*s.scale), int(float64(h)*s.scale)
	s.left = s.boxLeft + (s.boxWidth-s.width)/2
	s.top = s.boxTop + (s.boxHeight-s.height)/2

	s.layer.X, s.layer.Y, s.layer.Scale = s.left, s.top, s.scale
	s.layer.Hidden = false
}

// draw paints the pane's whole screen onto its own grid.
func (s *scaledPane) draw() { ui.DrawApart(fullScreen{s.pane}, s.g.View()) }

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

// fullScreen draws a terminal's whole screen rather than the blank the
// tree gets while the host is drawing it.
type fullScreen struct{ *term.Terminal }

func (f fullScreen) Draw(v grid.View) { f.DrawScreen(v) }

// placeScaled gives every pane whose screen is bigger than its room a
// grid and a layer of its own, and takes them away again when the screen
// fits.
//
// Every frame rather than on a resize: the size of a held screen is set
// by somebody on another machine, and it changes when they resize their
// window rather than when anything happens here.
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
	for pane := range a.panes {
		s := a.scaled[pane]
		area, shown := a.paneArea(pane)
		if !shown || !overflows(pane) {
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
	if a.scaledHeld == s {
		// The press it took will never be released by it.
		pane.CancelGesture()
		a.scaledHeld = nil
	}
	a.markDirty()
}

// overflows reports whether a pane's screen is bigger than the room the
// layout has for it, which only a held screen is.
func overflows(pane *term.Terminal) bool {
	size, box := pane.Size(), pane.Box()
	return pane.Held() && (size.Cols > box.Cols || size.Rows > box.Rows)
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
// open.
//
// Add puts it on top, which is where a pane's layer must not be: a pane
// that started being drawn on one while a dialog was up would be drawn
// over the dialog.
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

// routeMouse sends one mouse event to whoever should have it.
//
// A pane drawn scaled has more cells than the room the tree gave it, and
// the tree can only name a cell inside that room, so the pointer is
// mapped through the scale here and the event goes straight to the pane.
func (a *app) routeMouse(ev input.MouseEvent) (bool, error) {
	s := a.scaledFor()
	if s == nil {
		return a.root.HandleMouse(ev)
	}
	local := ev
	local.Col, local.Row = s.cellAt(a.pointer[0], a.pointer[1])
	switch {
	case ev.Kind == input.MousePress && !ev.Button.IsWheel():
		a.scaledHeld, a.scaledButton = s, ev.Button
		// Clicking a pane is how the mouse moves focus, the same as in
		// the tree.
		if ui.FocusedLeaf(a.root.Widget()) != ui.Widget(s.pane) {
			a.focus(s.pane)
		}
	case ev.Kind == input.MouseRelease && a.scaledHeld == s && a.scaledButton == ev.Button:
		a.scaledHeld = nil
	}
	return ui.HandleMouse(s.pane, local)
}

// scaledFor is the scaled pane an event belongs to, or nil when the tree
// should have it.
//
// A press on one keeps the pointer until the button comes up, the way
// Root keeps it for the widget that took a press: a drag that has
// wandered off the pane still belongs to it.
func (a *app) scaledFor() *scaledPane {
	if held := a.scaledHeld; held != nil {
		if a.scaled[held.pane] == held && a.root.Modal() == nil {
			return held
		}
		// A dialog opened over the pane, so the release is not coming.
		held.pane.CancelGesture()
		a.scaledHeld = nil
		return nil
	}
	switch {
	case len(a.scaled) == 0:
		return nil
	case a.root.Modal() != nil:
		// A dialog is drawn on the window's own grid, over the pane as
		// much as anywhere else.
		return nil
	case a.root.Holding() != nil:
		// A drag that began somewhere else belongs to whoever took the
		// press.
		return nil
	}
	for _, s := range a.scaled {
		if s.contains(a.pointer[0], a.pointer[1]) {
			return s
		}
	}
	return nil
}
