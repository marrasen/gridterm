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
	hues [2]color.RGBA
}

// sharedHues are the colours a shared pane is marked in, outermost
// first. It is the one place the order is decided, so the border's rings
// and the row's stripe cannot drift apart.
func (a *app) sharedHues(in sharing, step int) [2]color.RGBA {
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
func (m *sharedMark) draw(hues [2]color.RGBA) {
	want := drawnMark{area: m.area, hues: hues}
	if want == m.was {
		return
	}
	m.was = want
	m.g.Clear()
	for at, c := range hues {
		if c.A == 0 {
			continue
		}
		m.ring(at, c)
	}
}

// markThick is how many pixels thick the rule round a shared pane is.
const markThick = 2

// ring paints one border, at cells in from the edge.
func (m *sharedMark) ring(in int, c color.RGBA) {
	cols, rows := m.g.Size()
	if cols <= in*2 || rows <= in*2 {
		return
	}
	// A rule inside the cell rather than the whole cell filled: a border
	// a character wide and a character tall reads as a bar around the
	// pane.
	// Added to whatever the cell already carries: a ring one cell wide
	// or one cell tall is the same cell on both sides, and replacing
	// would leave it with only the last one.
	edge := func(x, y int, sides uint64) {
		was := m.g.At(x, y)
		if was.Art.Kind == grid.ArtEdge {
			sides |= was.Art.Sides()
		}
		m.g.Set(x, y, grid.Cell{Rune: ' ', FG: c, Width: 1, Art: grid.Edges(sides, markThick)})
	}
	for x := in; x < cols-in; x++ {
		edge(x, in, grid.EdgeTop)
		edge(x, rows-1-in, grid.EdgeBottom)
	}
	for y := in; y < rows-in; y++ {
		edge(in, y, grid.EdgeLeft)
		edge(cols-1-in, y, grid.EdgeRight)
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
