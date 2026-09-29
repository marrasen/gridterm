package main

import (
	"math"

	"github.com/marrasen/kakel/app"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/anim"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/input"
	"github.com/marrasen/gunim/paint"
	"github.com/marrasen/gunim/widget"
)

// Dragging a pane out of the switcher, to another of kakel's windows or
// out of them all. The tile lifts off and follows the pointer as a
// picture of the pane; a window it is over lights up, saying it takes
// it. Let go there, the pane moves in; let go over no window, it opens
// a window of its own where it was let go.

// dragSlop is how far the pointer goes, pressed on a tile, before the
// pane lifts off rather than being picked.
const dragSlop = 6

// moved reports whether p has gone further than dragSlop from from.
func moved(p, from geom.Point) bool {
	d := p.Sub(from)
	return math.Hypot(float64(d.X), float64(d.Y)) > dragSlop
}

// movesHere is what a window answers a pane dragged over it: a drop
// there moves the pane in.
const movesHere = "moves here"

// carry lifts the tile pressed off the switcher, to follow the pointer
// at at.
func (s *switcher) carry(at geom.Point, u *gunim.UI) {
	t := s.pressed
	s.carried = t
	r := t.box.Value()
	s.grab = at.Sub(r.Min)
	t.fade.Animate(0.25, widget.Quick.Get(u.Theme()))
	g := &paneGhost{s: s, t: t, size: r.Size(), lit: anim.NewFloat(0)}
	g.Add(g.lit)
	u.StartDrag(s, app.PaneDrag{Pane: t.id, Window: s.w.winID}, g, s.grab)
	u.Invalidate()
}

// dragEnded hears how the drag of a tile ended: taken by another
// window, which has the pane now; let go outside every window, which
// opens one for it; or neither, and the tile goes back to its place.
func (s *switcher) dragEnded(e input.DragEnd, u *gunim.UI) {
	t := s.carried
	s.pressed, s.carried = nil, nil
	if t == nil {
		return
	}
	switch {
	case e.Taken:
		s.lose(t, u)
	case e.Out && len(s.tiles) > 1:
		// The pane's top left corner where the picture's was, and the
		// window as large as this one.
		u.Send(s, app.PaneToNewWindow{Pane: t.id, At: e.At.Sub(s.grab), Size: s.size})
		s.lose(t, u)
	default:
		// Let go over this window, or outside it with nothing else
		// here to leave behind: it goes back.
		t.fade.Animate(1, widget.Settle.Get(u.Theme()))
		u.Invalidate()
	}
}

// lose takes tile t off the switcher, the rest closing up. With none
// left, the switcher closes.
func (s *switcher) lose(t *tile, u *gunim.UI) {
	i := -1
	for k, o := range s.tiles {
		if o == t {
			i = k
		}
	}
	if i < 0 {
		return
	}
	s.tiles = append(s.tiles[:i:i], s.tiles[i+1:]...)
	s.panes = append(s.panes[:i:i], s.panes[i+1:]...)
	if len(s.tiles) == 0 {
		s.cancel(u)
		return
	}
	s.light(min(s.hot, len(s.tiles)-1), u)
	u.Invalidate()
}

// paneGhost is the picture of a pane that follows the pointer while it
// is dragged, ringed while it is over a window that takes it.
type paneGhost struct {
	anim.Group
	s    *switcher
	t    *tile
	size geom.Size
	lit  *anim.Float
}

// Layout implements [gunim.Node].
func (g *paneGhost) Layout(gunim.Constraints, gunim.Frame, gunim.Children) geom.Size { return g.size }

// Paint implements [gunim.Node].
func (g *paneGhost) Paint(p *paint.Painter, f gunim.Frame, box geom.Size, _ gunim.Children) {
	g.s.paintPane(p, f, g.t, geom.Rect{Max: box.Point()}, 0.92, 1, g.lit.Value())
}

// Handle implements [gunim.Handler]: the window under the pointer says
// whether it takes the pane.
func (g *paneGhost) Handle(e input.Event, u *gunim.UI) bool {
	if a, ok := e.(input.DragAnswer); ok {
		to := float32(0)
		if a.Answer == movesHere {
			to = 1
		}
		g.lit.Animate(to, widget.Quick.Get(u.Theme()))
		u.Invalidate()
		return true
	}
	return false
}

// paneDrop takes a pane dragged over the window from another of
// kakel's windows: the window lights up while it is over it, and a
// drop moves it in.
func (w *window) paneDrop(e input.Event, u *gunim.UI) bool {
	switch e := e.(type) {
	case input.DragOver:
		if d, ok := e.Data.(app.PaneDrag); !ok || d.Window == w.winID {
			return false
		}
		u.AnswerDrag(movesHere)
		w.dropLit.Animate(1, widget.Quick.Get(u.Theme()))
		u.Invalidate()
		return true
	case input.DragLeave:
		w.dropLit.Animate(0, widget.Quick.Get(u.Theme()))
		u.Invalidate()
		return false
	case input.Drop:
		d, ok := e.Data.(app.PaneDrag)
		if !ok || d.Window == w.winID {
			return false
		}
		w.dropLit.Animate(0, widget.Settle.Get(u.Theme()))
		u.Send(w, app.PaneToWindow{Pane: d.Pane})
		u.Invalidate()
		return true
	}
	return false
}

// paintDropLit rings the window, and tints it, while a pane from
// another window is over it.
func (w *window) paintDropLit(p *paint.Painter, f gunim.Frame, box geom.Size) {
	on := min(max(w.dropLit.Value(), 0), 1)
	if on < 0.01 {
		return
	}
	c := switcherRing.Get(f.Theme)
	tint := c
	tint.A = uint8(0x22 * on)
	c.A = uint8(float32(c.A) * on)
	r := geom.Rect{Max: box.Point()}
	p.RRect(r, 0, paint.Solid(tint))
	p.RRectStroke(r.Inset(geom.Uniform(2)), 6, paint.Fill{}, paint.Stroke{Width: 3, Color: c})
}
