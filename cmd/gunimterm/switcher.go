package main

import (
	"image/color"
	"math"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/anim"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/input"
	"github.com/marrasen/gunim/paint"
	"github.com/marrasen/gunim/text"
	"github.com/marrasen/gunim/theme"
	"github.com/marrasen/gunim/widget"
)

// The pane switcher: every pane shown live, shrunk into a grid over the
// window. The panes on stage shrink from where they stand into their
// places; the others fade in. The arrows or the pointer move the ring,
// and the pane picked grows back to fill the stage.

var (
	switcherScrim = theme.Color("gunimterm.switcher.scrim", color.NRGBA{R: 0x0c, G: 0x0e, B: 0x12, A: 0xe8})
	switcherRing  = theme.Color("gunimterm.switcher.ring", color.NRGBA{R: 0x5e, G: 0x9c, B: 0xff, A: 0xff})
)

// switcher is the overview, over the window while it is open.
type switcher struct {
	anim.Group
	w     *window
	panes []Pane
	tiles []*tile
	hot   int
	in    *anim.Float
	// picked is the pane chosen, growing to fill the stage; -1 while
	// the choice is open.
	picked int
	size   geom.Size
	laid   bool
}

type tile struct {
	id    string
	title string
	box   *anim.Rect
	fade  *anim.Float
	ring  *anim.Float
	label text.Run
}

func newSwitcher(w *window, panes []Pane, focus string, u *gunim.UI) *switcher {
	s := &switcher{w: w, panes: panes, in: anim.NewFloat(0), picked: -1}
	s.Add(s.in)
	for i, p := range panes {
		t := &tile{id: p.ID, title: p.Title, box: anim.NewRect(geom.Rect{}), fade: anim.NewFloat(0), ring: anim.NewFloat(0)}
		t.label = text.Default().Shape(p.Title, 13)
		s.tiles = append(s.tiles, t)
		s.Add(t.box, t.fade, t.ring)
		if p.ID == focus {
			s.hot = i
		}
		// A pane on stage starts where it stands; the rest start in
		// their places, faded.
		if term, ok := w.terms[p.ID]; ok {
			if r, ok := u.Bounds(term); ok {
				t.box.Jump(r)
				t.fade.Jump(1)
			}
		}
	}
	return s
}

// grid returns each tile's place in a box of size: rows and columns as
// near square as the count allows, each keeping the window's shape.
func (s *switcher) grid(size geom.Size) []geom.Rect {
	n := len(s.tiles)
	if n == 0 {
		return nil
	}
	cols := int(math.Ceil(math.Sqrt(float64(n))))
	rows := (n + cols - 1) / cols
	const margin, gap, label = 40, 24, 22
	cw := (size.W - 2*margin - float32(cols-1)*gap) / float32(cols)
	ch := (size.H - 2*margin - float32(rows-1)*gap) / float32(rows)
	// Keep the window's shape inside each cell.
	aspect := size.W / max(size.H, 1)
	tw, th := cw, cw/aspect
	if th > ch-label {
		th = ch - label
		tw = th * aspect
	}
	out := make([]geom.Rect, n)
	for i := range out {
		c, r := i%cols, i/cols
		x := margin + float32(c)*(cw+gap) + (cw-tw)/2
		y := margin + float32(r)*(ch+gap) + label + (ch-label-th)/2
		out[i] = geom.Rc(x, y, tw, th)
	}
	return out
}

// Transition implements [gunim.Transitioner]: the overview fades in and
// out.
func (s *switcher) Transition(p gunim.Presence, f gunim.Frame) bool {
	switch p {
	case gunim.Entering:
		s.in.Animate(1, widget.Settle.Get(f.Theme))
	case gunim.Exiting:
		s.in.Animate(0, widget.Settle.Get(f.Theme))
	case gunim.Present:
	}
	return !s.in.Active() && !s.moving()
}

func (s *switcher) moving() bool {
	for _, t := range s.tiles {
		if t.box.Active() {
			return true
		}
	}
	return false
}

// Layout implements [gunim.Node]: the overview covers the window.
func (s *switcher) Layout(c gunim.Constraints, f gunim.Frame, _ gunim.Children) geom.Size {
	s.size = c.Max
	if s.picked < 0 {
		motion := widget.Settle.Get(f.Theme)
		for i, r := range s.grid(c.Max) {
			t := s.tiles[i]
			if !s.laid && t.fade.Value() == 0 {
				// Fading in, it starts a little smaller in its place.
				t.box.Jump(geom.Rect{Min: r.Center(), Max: r.Center()}.Inset(geom.Uniform(-min(r.Size().W, r.Size().H) * 0.45)))
			}
			t.box.Animate(r, motion)
			t.fade.Animate(1, motion)
		}
		s.laid = true
	}
	return c.Max
}

// Paint implements [gunim.Node].
func (s *switcher) Paint(p *paint.Painter, f gunim.Frame, box geom.Size, _ gunim.Children) {
	th := f.Theme
	t := min(max(s.in.Value(), 0), 1)
	scrim := switcherScrim.Get(th)
	scrim.A = uint8(float32(scrim.A) * t)
	p.RRect(geom.Rect{Max: box.Point()}, 0, paint.Solid(scrim))
	for i, tl := range s.tiles {
		if i == s.picked {
			continue
		}
		s.paintTile(p, f, tl, t)
	}
	// The pane picked grows over the rest.
	if s.picked >= 0 && s.picked < len(s.tiles) {
		s.paintTile(p, f, s.tiles[s.picked], 1)
	}
}

func (s *switcher) paintTile(p *paint.Painter, f gunim.Frame, tl *tile, t float32) {
	th := f.Theme
	r := tl.box.Value()
	if r.Size().W < 2 || r.Size().H < 2 {
		return
	}
	alpha := min(max(tl.fade.Value(), 0), 1)
	close := p.Layer(paint.LayerOpts{Bounds: r.Inset(geom.Uniform(-30)), Opacity: alpha})
	defer close()
	p.ShadowRRect(r, 6, paint.Solid(widget.Background.Get(th)), paint.Shadow{Offset: geom.Pt(0, 4), Blur: 18, Color: color.NRGBA{A: uint8(0x90 * t)}})
	if term, ok := s.w.terms[tl.id]; ok {
		natural := term.cells.CellSize()
		cols, rows := term.cells.GridSize()
		gw, gh := natural.W*float32(cols), natural.H*float32(rows)
		if gw > 0 && gh > 0 {
			scale := min(r.Size().W/gw, r.Size().H/gh)
			func() {
				defer p.Layer(paint.LayerOpts{Bounds: r, Opacity: 1, Clip: true})()
				defer p.Push(paint.Translate(r.Min))()
				defer p.Push(paint.Scale(scale, geom.Point{}))()
				term.cells.Paint(p, f, geom.Sz(gw, gh), gunim.Children{})
			}()
		}
	}
	if on := min(max(tl.ring.Value(), 0), 1); on > 0.01 {
		ring := switcherRing.Get(th)
		ring.A = uint8(float32(ring.A) * on)
		grow := 3 * on
		p.RRectStroke(r.Inset(geom.Uniform(-grow)), 6+grow, paint.Fill{}, paint.Stroke{Width: 2, Color: ring})
	}
	if s.picked < 0 {
		ink := widget.Ink.Get(th)
		ink.A = uint8(float32(ink.A) * t)
		tl.label.Paint(p, geom.Pt(r.Min.X, r.Min.Y-tl.label.Height()-6), ink)
	}
}

// light puts the ring on tile i.
func (s *switcher) light(i int, u *gunim.UI) {
	s.hot = i
	for k, t := range s.tiles {
		to := float32(0)
		if k == i {
			to = 1
		}
		t.ring.Animate(to, widget.Quick.Get(u.Theme()))
	}
	u.Invalidate()
}

// pick chooses tile i: it grows to fill the stage, and the overview
// fades away.
func (s *switcher) pick(i int, u *gunim.UI) {
	if s.picked >= 0 || i < 0 || i >= len(s.tiles) {
		return
	}
	s.picked = i
	t := s.tiles[i]
	stage := geom.Rect{Max: s.size.Point()}
	if r, ok := u.Bounds(s.w.stage); ok {
		stage = r
	}
	t.ring.Animate(0, widget.Quick.Get(u.Theme()))
	t.box.Animate(stage, widget.Settle.Get(u.Theme()))
	u.Send(s.w, FocusPane{Pane: t.id})
	s.w.closeSwitcher(false, u)
}

// cancel closes the overview without a choice; the panes on stage go
// back where they stood.
func (s *switcher) cancel(u *gunim.UI) {
	if s.picked >= 0 {
		return
	}
	for _, t := range s.tiles {
		if term, ok := s.w.terms[t.id]; ok {
			if r, ok := u.Bounds(term); ok {
				t.box.Animate(r, widget.Settle.Get(u.Theme()))
				continue
			}
		}
		t.fade.Animate(0, widget.Settle.Get(u.Theme()))
	}
	s.picked = len(s.tiles) // the choice is closed
	s.w.closeSwitcher(true, u)
}

// Focusable implements [gunim.Focusable].
func (s *switcher) Focusable() bool { return s.picked < 0 }

// Handle implements [gunim.Handler].
func (s *switcher) Handle(e input.Event, u *gunim.UI) bool {
	cols := int(math.Ceil(math.Sqrt(float64(len(s.tiles)))))
	switch e := e.(type) {
	case input.KeyPress:
		switch e.Key {
		case input.KeyLeft:
			s.light(max(0, s.hot-1), u)
		case input.KeyRight, input.KeyTab:
			s.light(min(len(s.tiles)-1, s.hot+1), u)
		case input.KeyUp:
			s.light(max(0, s.hot-cols), u)
		case input.KeyDown:
			s.light(min(len(s.tiles)-1, s.hot+cols), u)
		case input.KeyEnter, input.KeySpace:
			s.pick(s.hot, u)
		case input.KeyEscape:
			s.cancel(u)
		}
		return true
	case input.PointerMove:
		if i := s.tileAt(e.Pos); i >= 0 && i != s.hot {
			s.light(i, u)
		}
		return true
	case input.PointerDown:
		if i := s.tileAt(e.Pos); i >= 0 {
			s.pick(i, u)
		} else {
			s.cancel(u)
		}
		return true
	case input.TextInput, input.KeyRelease, input.PointerUp:
		return true
	}
	return false
}

func (s *switcher) tileAt(p geom.Point) int {
	for i, t := range s.tiles {
		if t.box.Value().Contains(p) {
			return i
		}
	}
	return -1
}
