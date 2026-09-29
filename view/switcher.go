package view

import (
	"image/color"
	"math"
	"slices"
	"time"

	"github.com/marrasen/kakel/app"
	"github.com/marrasen/kakel/ui"
	"github.com/marrasen/kakel/winkeys"

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
	switcherRing = theme.Color("kakel.switcher.ring", color.NRGBA{R: 0x5e, G: 0x9c, B: 0xff, A: 0xff})
)

// switcher is the overview, over the window while it is open.
type switcher struct {
	anim.Group
	w     *Window
	panes []app.Pane
	tiles []*tile
	hot   int
	in    *anim.Float
	// picked is the pane chosen, growing to fill the stage; -1 while
	// the choice is open.
	picked int
	size   geom.Size
	laid   bool
	// landed is when the pane picked was asked onto the stage; zero
	// until then.
	landed time.Time
	// stageDrawn is what the stage draws, kept while the switcher is
	// open, and held at the pick: the stage as it was, for the pane
	// picked to grow over while the stage underneath already shows it.
	// stageAt is where the stage stood then.
	stageDrawn *gunim.Drawing
	stageAt    geom.Rect
	// mates are the panes that share the picked pane's split, which grow
	// into their places beside it, so the split comes together as it
	// lands rather than appearing once it has.
	mates map[*tile]bool
	// pressed is the tile the pointer went down on, at pressAt, which
	// is picked when the pointer comes up there, and carried is the
	// tile dragged out of it, held grab from its top left corner.
	pressed *tile
	pressAt geom.Point
	carried *tile
	grab    geom.Point
}

type tile struct {
	id    string
	title string
	box   *anim.Rect
	fade  *anim.Float
	ring  *anim.Float
	label text.Run
}

func newSwitcher(w *Window, panes []app.Pane, focus string, u *gunim.UI) *switcher {
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
		if r, ok := w.standsAt(p.ID, u); ok {
			t.box.Jump(r)
			t.fade.Jump(1)
		}
	}
	return s
}

// natural returns the size a pane's screen has on stage, or the size
// it was last drawn at, or the whole of size for a pane never drawn.
func (s *switcher) natural(t *tile, size geom.Size) geom.Size { return s.w.natural(t.id, size) }

// natural is the size pane id draws at, from its terminal's grid or
// its last drawing, or size when it has neither.
func (w *Window) natural(id string, size geom.Size) geom.Size {
	if term, ok := w.terms[id]; ok {
		cell := term.cells.CellSize()
		cols, rows := term.cells.GridSize()
		if n := geom.Sz(cell.W*float32(cols), cell.H*float32(rows)); n.W > 0 && n.H > 0 {
			return n
		}
	}
	if d := w.drawings[id]; d != nil && !d.Recording().Empty() {
		if n := d.Size(); n.W > 0 && n.H > 0 {
			return n
		}
	}
	return size
}

// paintMiniature draws pane id shrunk into r, keeping its shape: a
// terminal live, any other pane as it was last drawn. It reports false
// when there is nothing of it to draw.
func (w *Window) paintMiniature(p *paint.Painter, f gunim.Frame, id string, r geom.Rect, size geom.Size) bool {
	term, live := w.terms[id]
	d := w.drawings[id]
	nat := w.natural(id, size)
	if nat.W <= 0 || nat.H <= 0 || !live && (d == nil || d.Recording().Empty()) {
		return false
	}
	scale := min(r.Size().W/nat.W, r.Size().H/nat.H)
	defer p.Layer(paint.LayerOpts{Bounds: r, Opacity: 1, Clip: true})()
	defer p.Push(paint.Translate(r.Min))()
	defer p.Push(paint.Scale(scale, geom.Point{}))()
	if live {
		term.cells.Paint(p, f, nat, gunim.Children{})
	} else {
		p.Replay(d.Recording())
	}
	return true
}

// standsAt is where pane id stands on stage now, if it is on stage.
func (w *Window) standsAt(id string, u *gunim.UI) (geom.Rect, bool) {
	if term, ok := w.terms[id]; ok {
		if r, ok := u.Bounds(term); ok {
			return r, true
		}
	}
	if n := w.drawn[id]; n != nil {
		return u.Bounds(n)
	}
	return geom.Rect{}, false
}

// grid returns each tile's place in a box of size: rows and columns as
// near square as the count allows. Every pane shrinks by the same
// amount, keeping its own shape, so text reads the same size in each.
func (s *switcher) grid(size geom.Size) []geom.Rect {
	n := len(s.tiles)
	if n == 0 {
		return nil
	}
	cols := int(math.Ceil(math.Sqrt(float64(n))))
	rows := (n + cols - 1) / cols
	const margin, gap, label = 40, 24, 22
	cw := (size.W - 2*margin - float32(cols-1)*gap) / float32(cols)
	ch := (size.H-2*margin-float32(rows-1)*gap)/float32(rows) - label
	scale := float32(1)
	nat := make([]geom.Size, n)
	for i, t := range s.tiles {
		nat[i] = s.natural(t, size)
		scale = min(scale, cw/nat[i].W, ch/nat[i].H)
	}
	out := make([]geom.Rect, n)
	for i := range out {
		c, r := i%cols, i/cols
		tw, th := nat[i].W*scale, nat[i].H*scale
		x := margin + float32(c)*(cw+gap) + (cw-tw)/2
		y := margin + float32(r)*(ch+label+gap) + label + (ch-th)/2
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
	return !s.in.Active() && !s.moving() && s.onStage(time.Now())
}

// onStage reports whether the pane picked is on the stage, so the
// switcher can go without the stage before it showing for a frame. A
// pane that never gets there lets the switcher go after landWait.
func (s *switcher) onStage(now time.Time) bool {
	if s.picked < 0 || s.picked >= len(s.tiles) {
		return true
	}
	if s.landed.IsZero() {
		return false
	}
	return s.w.focused == s.tiles[s.picked].id || now.Sub(s.landed) > landWait
}

// landWait is how long the switcher waits for the pane picked to come
// on stage before it goes anyway.
const landWait = 500 * time.Millisecond

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
			if t != s.carried {
				t.fade.Animate(1, motion)
			}
		}
		s.laid = true
	}
	return c.Max
}

// Paint implements [gunim.Node].
func (s *switcher) Paint(p *paint.Painter, f gunim.Frame, box geom.Size, _ gunim.Children) {
	th := f.Theme
	t := min(max(s.in.Value(), 0), 1)
	// The window's own ground, solid once the switcher is in: the panes
	// are shown only as the tiles, not behind them as well.
	scrim := widget.Background.Get(th)
	scrim.A = uint8(float32(scrim.A) * t)
	p.RRect(geom.Rect{Max: box.Point()}, 0, paint.Solid(scrim))
	for i, tl := range s.tiles {
		if i == s.picked || s.mates[tl] {
			continue
		}
		s.paintTile(p, f, tl, t)
	}
	// The pane picked grows over the rest, and over the stage as it
	// was, while the stage underneath already shows it.
	if s.picked >= 0 && s.picked < len(s.tiles) {
		if d := s.stageDrawn; d != nil && !d.Recording().Empty() {
			func() {
				defer p.Layer(paint.LayerOpts{Bounds: s.stageAt, Opacity: 1, Clip: true})()
				defer p.Push(paint.Translate(s.stageAt.Min))()
				p.Replay(d.Recording())
			}()
		}
		for _, tl := range s.tiles {
			if s.mates[tl] {
				s.paintTile(p, f, tl, 1)
			}
		}
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
	s.paintPane(p, f, tl, r, alpha, t, tl.ring.Value())
	if s.picked < 0 {
		ink := widget.Ink.Get(th)
		ink.A = uint8(float32(ink.A) * t)
		ink.A = uint8(float32(ink.A) * alpha)
		tl.label.Paint(p, geom.Pt(r.Min.X, r.Min.Y-tl.label.Height()-6), ink)
	}
}

// paintPane draws tile tl's pane shrunk into r, at alpha, with a
// shadow as dark as shadow says and the ring as lit as ring says.
func (s *switcher) paintPane(p *paint.Painter, f gunim.Frame, tl *tile, r geom.Rect, alpha, shadow, ring float32) {
	th := f.Theme
	close := p.Layer(paint.LayerOpts{Bounds: r.Inset(geom.Uniform(-30)), Opacity: alpha})
	defer close()
	p.ShadowRRect(r, 6, paint.Solid(widget.Background.Get(th)), paint.Shadow{Offset: geom.Pt(0, 4), Blur: 18, Color: color.NRGBA{A: uint8(0x90 * shadow)}})
	// A terminal live, and any other pane as it was last drawn.
	s.w.paintMiniature(p, f, tl.id, r, s.size)
	if on := min(max(ring, 0), 1); on > 0.01 {
		c := switcherRing.Get(th)
		c.A = uint8(float32(c.A) * on)
		grow := 3 * on
		p.RRectStroke(r.Inset(geom.Uniform(-grow)), 6+grow, paint.Fill{}, paint.Stroke{Width: 2, Color: c})
	}
}

// placeIn is where pane id stands when group, the arrangement it is in,
// fills r: split as gunim's Split splits, the first half its share of
// the room less the gap between them. ok is false when id is not in it.
func placeIn(group *app.Box, id string, r geom.Rect, u *gunim.UI) (geom.Rect, bool) {
	switch {
	case group == nil:
		return geom.Rect{}, false
	case group.Pane != "":
		return r, group.Pane == id
	}
	length := r.Size().W
	if group.Vertical {
		length = r.Size().H
	}
	v := min(max(group.Share, 0), 1)
	gap := widget.SplitGap.Get(u.Theme()) * min(max(min(v, 1-v)*20, 0), 1)
	first := float32(int((length-gap)*v + 0.5))
	a, b := r, r
	if group.Vertical {
		a.Max.Y = r.Min.Y + first
		b.Min.Y = a.Max.Y + gap
	} else {
		a.Max.X = r.Min.X + first
		b.Min.X = a.Max.X + gap
	}
	if at, ok := placeIn(group.A, id, a, u); ok {
		return at, true
	}
	return placeIn(group.B, id, b, u)
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
	// Into its place in its split, and the panes beside it into theirs;
	// alone, it fills the stage.
	group := s.w.groups[t.id]
	s.mates = map[*tile]bool{}
	into := stage
	if r, ok := placeIn(group, t.id, stage, u); ok {
		into = r
	}
	t.box.Animate(into, widget.Settle.Get(u.Theme()))
	for k, o := range s.tiles {
		if k == i {
			continue
		}
		if r, ok := placeIn(group, o.id, stage, u); ok {
			s.mates[o] = true
			o.ring.Animate(0, widget.Quick.Get(u.Theme()))
			o.box.Animate(r, widget.Settle.Get(u.Theme()))
		}
	}
	// The rest fade as it grows, shrinking a little where they sit, so
	// none is left standing over the sidebar as the overview goes.
	for k, o := range s.tiles {
		if k == i || s.mates[o] {
			continue
		}
		r := o.box.Target()
		o.fade.Animate(0, widget.Quick.Get(u.Theme()))
		o.box.Animate(r.Inset(geom.Uniform(min(r.Size().W, r.Size().H)*0.08)), widget.Quick.Get(u.Theme()))
	}
	// The pane comes on stage at once, with the keyboard, so it is live
	// the moment it is picked. The stage as it was is held, drawn where
	// the stage stands, for the pane to grow over; once it has grown
	// into place, the switcher goes, and the stage shows the pane.
	u.ForgetDrawing(s.w.stage)
	s.stageAt = stage
	s.landed = time.Now()
	u.Send(s.w, app.FocusPane{Pane: t.id})
	s.w.closeSwitcher(false, u)
}

// cancel closes the overview without a choice; the panes on stage go
// back where they stood.
func (s *switcher) cancel(u *gunim.UI) {
	if s.picked >= 0 {
		return
	}
	for _, t := range s.tiles {
		if r, ok := s.w.standsAt(t.id, u); ok {
			t.box.Animate(r, widget.Settle.Get(u.Theme()))
			continue
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
		case input.KeyEnter, input.KeyKPEnter, input.KeySpace:
			s.pick(s.hot, u)
		case input.KeyEscape:
			s.cancel(u)
		default:
			// Its shortcut again closes it; held down, it does not
			// open and close over and over.
			if ev, ok := winkeys.Event(e); ok && !e.Repeat {
				if id, bound := s.w.keys.Lookup(ui.ChordOf(ev)); bound && id == "view.switcher" {
					s.cancel(u)
				}
			}
		}
		return true
	case input.PointerMove:
		if s.pressed != nil && s.carried == nil && moved(e.Pos, s.pressAt) {
			s.carry(e.Pos, u)
			return true
		}
		if i := s.tileAt(e.Pos); i >= 0 && i != s.hot {
			s.light(i, u)
		}
		return true
	case input.PointerDown:
		if i := s.tileAt(e.Pos); i >= 0 && e.Button == input.ButtonPrimary {
			// Picked as the pointer comes up, unless it drags the pane
			// away first.
			s.pressed, s.pressAt = s.tiles[i], e.Pos
		} else if i < 0 {
			s.cancel(u)
		}
		return true
	case input.PointerUp:
		t := s.pressed
		s.pressed = nil
		if t != nil && s.carried == nil && s.tileAt(e.Pos) == slices.Index(s.tiles, t) {
			s.pick(slices.Index(s.tiles, t), u)
		}
		return true
	case input.DragEnd:
		s.dragEnded(e, u)
		return true
	case input.TextInput, input.KeyRelease:
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
