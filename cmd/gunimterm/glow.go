package main

import (
	"image/color"
	"math"
	"time"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/paint"

	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/vt"
)

// A pane shared with an agent, or watched from another window, has a
// ring round it that glows slowly, as in gridterm: so whoever sits at
// the window can tell at a glance which panes somebody else can see.
// Shared both ways, it has two rings, the agent's outside.

// glowEvery is how long one glow takes, bright and back.
const glowEvery = 3 * time.Second

// Marks are the colours of the rings, from the theme's terminal
// colours: an agent's, and another window's.
type Marks struct{ Agent, Watched color.NRGBA }

// marksOf are the rings' colours in a palette, as gridterm picks them.
func marksOf(p vt.Palette) Marks {
	// Lifted towards whichever of black and white the ground is not.
	black, white := color.RGBA{A: 0xff}, color.RGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	far := black
	if grid.Contrast(white, p.BG) >= grid.Contrast(black, p.BG) {
		far = white
	}
	nrgba := func(c color.RGBA) color.NRGBA { return color.NRGBA{R: c.R, G: c.G, B: c.B, A: 0xff} }
	return Marks{Agent: nrgba(p.ANSI[6]), Watched: nrgba(grid.Blend(p.ANSI[9], far, 2, 5))}
}

// glowAt is how bright the glow is at now, from 0 to 1 and back once
// every glowEvery.
func glowAt(now time.Time) float64 {
	period := float64(glowEvery / time.Millisecond)
	turn := float64(now.UnixMilli()%int64(period)) / period
	return (1 - math.Cos(turn*2*math.Pi)) / 2
}

// markWidth is a ring's width, and markGap the room between two.
const markWidth, markGap = 2, 3

// shared reports whether the terminal is shared, and how.
func (t *term) shared() (agent, watched bool) {
	return t.agent, t.sh.t.Watched() > 0
}

// paintRings draws the rings round a shared terminal.
func (t *term) paintRings(p *paint.Painter, f gunim.Frame, box geom.Size) {
	agent, watched := t.shared()
	if !agent && !watched {
		return
	}
	var hues []color.NRGBA
	if agent {
		hues = append(hues, t.marks.Agent)
	}
	if watched {
		hues = append(hues, t.marks.Watched)
	}
	const dim, bright = 0x70, 0x90
	a := uint8(dim + int(float64(bright-dim)*glowAt(f.Now)+0.5))
	corner := min(t.cells.CellSize().W, t.cells.CellSize().H)
	for i, c := range hues {
		in := float32(i)*(markWidth+markGap) + markWidth/2.0
		if box.W <= 2*in || box.H <= 2*in {
			continue
		}
		c.A = a
		r := geom.Rc(in, in, box.W-2*in, box.H-2*in)
		p.RRectStroke(r, max(corner-in, 0), paint.Fill{}, paint.Stroke{Width: markWidth, Color: c})
	}
}

// glow keeps frames coming while any terminal is shared, a few a
// second, which is all a slow glow needs.
func (w *window) glow(u *gunim.UI) {
	if w.glowing {
		return
	}
	any := false
	for _, t := range w.terms {
		if agent, watched := t.shared(); agent || watched {
			any = true
		}
	}
	if !any {
		return
	}
	w.glowing = true
	u.After(glowStep, func(u *gunim.UI) {
		w.glowing = false
		u.Invalidate()
		w.glow(u)
	})
}

// glowStep is how often a glowing ring is drawn again.
const glowStep = 50 * time.Millisecond
