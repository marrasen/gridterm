package main

import (
	"image/color"
	"time"

	"github.com/marrasen/gridterm/glyph"
	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/render"
	"github.com/marrasen/gridterm/ui"
	"github.com/marrasen/gridterm/ui/term"
)

// glowStep is how long one step of a border's glow lasts, slow so an
// idle window still skips most of its frames.
const glowStep = 250 * time.Millisecond

// glowSteps is how many steps the glow takes from dim to bright.
const glowSteps = 6

// sharing is who has a pane besides the user.
type sharing struct {
	agent   bool
	watched bool
}

// any reports whether the pane is shared at all.
func (s sharing) any() bool { return s.agent || s.watched }

// sharedIn says who has a pane besides the user, which is what its
// border says.
func (a *app) sharedIn(pane *term.Terminal) sharing {
	return sharing{
		agent:   a.agents.of(pane) != nil && !a.ended[pane],
		watched: pane.Watched() > 0,
	}
}

// markColours are the two colours a border is drawn in.
type markColours struct{ agent, watched color.RGBA }

// borderColours are the colours a shared pane's borders take: the same
// two the menu bar says an agent and a remote window in.
func (a *app) borderColours() markColours {
	return markColours{agent: statusAgentFG(a.colours), watched: statusTakenFG(a.colours)}
}

// clock is the time this frame's glow is measured against.
func (a *app) clock() time.Time {
	if a.now != nil {
		return a.now()
	}
	return time.Now()
}

// sharedMark is the glowing border round one shared pane, on a layer of
// its own over the pane so the program keeps every row and column.
type sharedMark struct {
	g     *grid.Grid
	geo   render.Geometry
	layer *render.Layer

	// area is where the pane was when the border was last placed.
	area ui.Rect

	// was is what the border last drew, so a frame that would draw the
	// same thing draws nothing.
	was drawnMark
}

// drawnMark is everything a border's picture depends on.
type drawnMark struct {
	area ui.Rect
	in   sharing
	hues markColours
	step int
}

// newSharedMark puts a border on a grid and a layer of its own, hidden
// until it has been placed.
func newSharedMark() *sharedMark {
	m := &sharedMark{g: grid.New(1, 1, color.RGBA{}, color.RGBA{})}
	m.layer = &render.Layer{Grid: m.g, Hidden: true, Transparent: true, Geom: &m.geo}
	return m
}

// place sizes the grid to the pane's room and puts the layer over it,
// taking the columns from the window so the ring lands on the pane's
// own edge rather than a fraction of a cell inside it.
func (m *sharedMark) place(area ui.Rect, src *render.Geometry, met glyph.Metrics) {
	m.area = area
	m.g.Resize(max(area.Cols, 1), max(area.Rows, 1))
	m.geo.Layout(m.g, met)
	m.geo.TakeCols(src, area.X, area.X+area.Cols)
	left, _ := src.ColBox(area.X, area.X+area.Cols)
	top, height := src.RowBox(area.Y, area.Y+area.Rows)
	m.geo.FitRows(height)
	m.layer.X, m.layer.Y = left, top
	m.layer.Hidden = false
}

// glowAt is how far through the glow a moment is, taken from the clock
// so every border on screen glows in step.
func glowAt(now time.Time) int {
	at := int(now.UnixMilli()/int64(glowStep/time.Millisecond)) % (glowSteps * 2)
	if at >= glowSteps {
		at = glowSteps*2 - at
	}
	return at
}

// draw paints the border, and leaves the grid alone when the picture
// has not changed.
func (m *sharedMark) draw(in sharing, now time.Time, hues markColours) {
	want := drawnMark{area: m.area, in: in, hues: hues, step: glowAt(now)}
	if want == m.was {
		return
	}
	m.was = want
	m.g.Clear()
	ring := 0
	if in.agent {
		m.ring(ring, glow(hues.agent, want.step))
		ring++
	}
	if in.watched {
		m.ring(ring, glow(hues.watched, want.step))
	}
}

// ring paints one border, at cells in from the edge.
func (m *sharedMark) ring(in int, c color.RGBA) {
	cols, rows := m.g.Size()
	if cols <= in*2 || rows <= in*2 {
		return
	}
	cell := grid.Cell{Rune: ' ', FG: c, BG: c, Width: 1}
	for x := in; x < cols-in; x++ {
		m.g.Set(x, in, cell)
		m.g.Set(x, rows-1-in, cell)
	}
	for y := in; y < rows-in; y++ {
		m.g.Set(in, y, cell)
		m.g.Set(cols-1-in, y, cell)
	}
}

// glow sets a border colour's alpha for one step of the glow, stopping
// short of both ends so the border never goes out and never turns into
// a solid block.
func glow(c color.RGBA, step int) color.RGBA {
	const dim, bright = 0x50, 0xd0
	c.A = uint8(dim + (bright-dim)*(step+1)/(glowSteps+2))
	return c
}

// placeShared gives every shared pane a border, and takes it away again
// when the pane stops being shared.
func (a *app) placeShared() {
	if a.comp == nil || a.g == nil {
		return
	}
	for pane, m := range a.shared {
		if _, live := a.panes[pane]; !live {
			a.dropShared(pane, m)
		}
	}
	met := a.renderer.Metrics()
	for pane := range a.panes {
		in := a.sharedIn(pane)
		m := a.shared[pane]
		if !in.any() {
			if m != nil {
				a.dropShared(pane, m)
			}
			continue
		}
		area, shown := a.paneArea(pane)
		if !shown {
			if m != nil {
				a.dropShared(pane, m)
			}
			continue
		}
		if m == nil {
			m = newSharedMark()
			a.shared[pane] = m
			a.addUnderModals(m.layer)
		}
		m.place(area, &a.geo, met)
		// A screen drawn on a layer of its own is opaque, and it goes on
		// the stack whenever the pane's size is taken -- which can be
		// after the border. Put the border back on top of it.
		if s := a.scaled[pane]; s != nil && !drawnAfter(a.comp, m.layer, s.layer) {
			a.comp.Remove(m.layer)
			a.addUnderModals(m.layer)
		}
	}
}

// drawnAfter reports whether one layer is drawn after another.
func drawnAfter(c *render.Compositor, l, under *render.Layer) bool {
	seen := false
	for _, have := range c.Layers() {
		switch have {
		case under:
			seen = true
		case l:
			return seen
		}
	}
	return false
}

// drawShared paints each border onto its own grid.
func (a *app) drawShared(now time.Time) {
	hues := a.borderColours()
	for pane, m := range a.shared {
		m.draw(a.sharedIn(pane), now, hues)
	}
}

// dropShared takes a pane's border away.
func (a *app) dropShared(pane *term.Terminal, m *sharedMark) {
	a.comp.Remove(m.layer)
	delete(a.shared, pane)
}
