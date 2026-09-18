package main

import (
	"image"
	"image/color"
	"math"
	"time"

	"github.com/marrasen/gridterm/glyph"
	"github.com/marrasen/gridterm/render"
	"github.com/marrasen/gridterm/ui"
	"github.com/marrasen/gridterm/ui/term"
)

// glowEvery is how often a border pulses, and glowFor how long one pulse
// takes. Between pulses the border sits still, which is what lets the
// window skip three frames in four while a pane is shared.
//
// A pulse rather than a glow that never stops: something moving on
// screen is something being looked at, and a border that breathes for
// ever is one nobody sees after a minute.
const (
	glowEvery = time.Second
	glowFor   = 250 * time.Millisecond
)

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

// clock is the time the window measures itself against.
func (a *app) clock() time.Time {
	if a.now != nil {
		return a.now()
	}
	return time.Now()
}

// frameTime is when this frame began, and the clock for a draw that
// comes before the window's first update.
func (a *app) frameTime() time.Time {
	if a.frameAt.IsZero() {
		return a.clock()
	}
	return a.frameAt
}

// sharedMark is the glowing border round one shared pane, on a layer of
// its own over the pane so the program keeps every row and column.
//
// It has no grid. A border is a shape rather than a character, so it is
// a rule drawn per pixel with rounded corners, and the layer carries
// nothing else.
type sharedMark struct {
	layer *render.Layer

	// rings are the rules, outermost first, one per thing sharing the
	// pane. A ring the pane is too small to hold is left empty.
	rings [2]render.Stroke
}

// sharedHues are the colours a shared pane is marked in, outermost
// first. It is the one place the order is decided, so the border's rings
// and the row's stripe cannot drift apart.
func (a *app) sharedHues(in sharing, step float64) [2]color.RGBA {
	colours := a.borderColours()
	var out [2]color.RGBA
	at := 0
	if in.agent {
		out[at] = glow(colours.agent, step)
		at++
	}
	if in.watched {
		out[at] = glow(colours.watched, step)
	}
	return out
}

// markWidth is how thick a rule round a shared pane is, and markGap how
// far the next one in sits from it, both in pixels.
const (
	markWidth = 2
	markGap   = 3
)

// newSharedMark puts a border on a layer of its own, hidden until it has
// been placed.
func newSharedMark() *sharedMark {
	m := &sharedMark{}
	m.layer = &render.Layer{Hidden: true}
	return m
}

// place puts the rules round the pane's own box, in the window's pixels.
func (m *sharedMark) place(area ui.Rect, src *render.Geometry, met glyph.Metrics) {
	left, width := src.ColBox(area.X, area.X+area.Cols)
	top, height := src.RowBox(area.Y, area.Y+area.Rows)
	m.layer.X, m.layer.Y = 0, 0
	corner := float32(min(met.CellW, met.CellH))
	for i := range m.rings {
		// Each ring inside the last, and half a width in from the pane's
		// edge so the outermost sits on it rather than over it.
		in := int(float32(i)*(markWidth+markGap) + float32(markWidth)/2)
		if width <= in*2 || height <= in*2 {
			// image.Rect turns a box inside out rather than refusing it,
			// so a ring the pane cannot hold would land in the middle of
			// the pane as a blob. Drop it instead.
			m.rings[i] = render.Stroke{}
			continue
		}
		m.rings[i].Rect = image.Rect(left+in, top+in, left+width-in, top+height-in)
		m.rings[i].Corner = max(corner-float32(in), 0)
		m.rings[i].Width = markWidth
	}
	m.layer.Hidden = false
}

// glowAt is how far into a pulse a moment is: 0 at rest, 1 at the top,
// and back to 0. Taken from the clock so every border on screen pulses
// together.
//
// It is exactly 0 for most of each period, and the border drawn from it
// is then the same as the frame before, which is what the compositor
// reads as nothing to do.
func glowAt(now time.Time) float64 {
	into := now.UnixMilli() % int64(glowEvery/time.Millisecond)
	fade := int64(glowFor / time.Millisecond)
	if into >= fade {
		return 0
	}
	// Up and back down over the pulse, on a curve rather than a corner:
	// a border that snapped to bright would read as a flicker.
	return math.Sin(float64(into) / float64(fade) * math.Pi)
}

// draw gives the rules their colours, outermost first.
//
// A ring with no colour is dropped, which is what a pane with one thing
// sharing it has, and so is one the pane was too small to hold.
func (m *sharedMark) draw(hues [2]color.RGBA) {
	m.layer.Strokes = m.layer.Strokes[:0]
	for at, c := range hues {
		ring := m.rings[at]
		ring.Colour = c
		if ring.Empty() {
			continue
		}
		m.layer.Strokes = append(m.layer.Strokes, ring)
	}
}

// glow sets a border colour's alpha for a moment in the pulse, stopping
// short of both ends so the border never goes out and never turns into
// a solid block.
func glow(c color.RGBA, at float64) color.RGBA {
	const dim, bright = 0x50, 0xd0
	c.A = uint8(dim + int(float64(bright-dim)*at+0.5))
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
	step := glowAt(now)
	for pane, m := range a.shared {
		m.draw(a.sharedHues(a.sharedIn(pane), step))
	}
}

// dropShared takes a pane's border away.
func (a *app) dropShared(pane *term.Terminal, m *sharedMark) {
	a.comp.Remove(m.layer)
	delete(a.shared, pane)
}

// sharedEdge is the stripe a shared pane's row carries: the border's
// own colours, one cell each, at the same point in the glow.
//
// A row is marked whether or not the pane is in front, because a pane
// in a tab behind has no border and its row is the only place the user
// can see that somebody else is in it.
func (a *app) sharedEdge(pane *term.Terminal, now time.Time) [2]color.RGBA {
	return a.sharedHues(a.sharedIn(pane), glowAt(now))
}
