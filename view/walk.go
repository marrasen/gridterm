package view

import (
	"slices"

	"github.com/marrasen/kakel/app"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/anim"
	"github.com/marrasen/gunim/geom"
	gi "github.com/marrasen/gunim/input"
	"github.com/marrasen/gunim/paint"
	"github.com/marrasen/gunim/widget"
)

// Ctrl+Tab walks the panes in the order they were last used: the first
// press goes back to the pane before, and each press while Ctrl is held
// goes one further, with the list shown in the middle of the window.
// Letting go of Ctrl ends the walk on the pane reached, which is then
// the most recent.

// paneWalk is a walk under way: the panes in the order it goes, and
// how far it has got.
type paneWalk struct {
	order []string
	at    int
}

// noteFocus puts the focused pane first in the order of use. A walk
// passing through panes changes nothing until it ends.
func (w *Window) noteFocus(focus string) {
	if w.walk != nil || !slices.ContainsFunc(w.panes, func(p app.Pane) bool { return p.ID == focus }) {
		return
	}
	if len(w.recent) > 0 && w.recent[0] == focus {
		return
	}
	w.recent = append([]string{focus}, slices.DeleteFunc(w.recent, func(id string) bool { return id == focus })...)
}

// recentPanes is every pane, the most recently used first, and the
// ones never used after them in the sidebar's order.
func (w *Window) recentPanes() []string {
	live := map[string]bool{}
	for _, p := range w.panes {
		live[p.ID] = true
	}
	var out []string
	for _, id := range w.recent {
		if live[id] {
			out = append(out, id)
		}
	}
	for _, p := range w.panes {
		if !slices.Contains(out, p.ID) {
			out = append(out, p.ID)
		}
	}
	return out
}

// walkRecent takes a step of the walk, starting one if none is under
// way.
func (w *Window) walkRecent(step int, u *gunim.UI) {
	if w.walk == nil {
		w.walk = &paneWalk{order: w.recentPanes()}
		// The ring starts round the pane the walk leaves, to slide from.
		if r, ok := w.standsAt(w.focused, u); ok && w.walkMark == nil {
			w.walkMark = newWalkMark(r)
			u.Insert(w, w.walkMark)
		}
	}
	n := len(w.walk.order)
	if n < 2 {
		w.walk = nil
		return
	}
	w.walk.at = ((w.walk.at+step)%n + n) % n
	u.Send(w, app.FocusPane{Pane: w.walk.order[w.walk.at]})
	titles := make([]string, n)
	for i, id := range w.walk.order {
		for _, p := range w.panes {
			if p.ID == id {
				titles[i] = p.Title
			}
		}
	}
	if w.walkList == nil {
		// Given its titles before it goes on screen: a node's children
		// are read as it arrives, and the list's labels are its children.
		w.walkList = newWalkList()
		w.walkList.show(titles, w.walk.at)
		u.Insert(w, w.walkList)
	}
	w.walkList.show(titles, w.walk.at)
	u.Invalidate()
}

// endWalk ends a walk on the pane it reached, which becomes the most
// recent.
func (w *Window) endWalk(u *gunim.UI) {
	if w.walk == nil {
		return
	}
	on := w.walk.order[w.walk.at]
	w.walk = nil
	w.noteFocus(on)
	if w.walkList != nil {
		u.Remove(w.walkList)
		w.walkList = nil
	}
	if w.walkMark != nil {
		u.Remove(w.walkMark)
		w.walkMark = nil
	}
}

// markWalk puts the ring round pane id, where the stage has laid it
// out, sliding from where it was, when the pane sits in a split; alone
// on the stage, the pane needs no pointing out, and the ring fades.
func (w *Window) markWalk(id string, split bool, u *gunim.UI) {
	m := w.walkMark
	if m == nil || w.walk == nil {
		return
	}
	r, ok := w.standsAt(id, u)
	if !ok || !split {
		m.shown.Animate(0, widget.Quick.Get(u.Theme()))
		return
	}
	if m.shown.Target() < 0.5 {
		// Coming back from a pane alone: it starts where the pane is.
		m.box.Jump(r)
	} else {
		m.box.Animate(r, widget.Settle.Get(u.Theme()))
	}
	m.shown.Animate(1, widget.Quick.Get(u.Theme()))
	u.Invalidate()
}

// walkMark is a ring round a pane, which slides from pane to pane as a
// walk reaches them.
type walkMark struct {
	anim.Group
	box   *anim.Rect
	shown *anim.Float
}

func newWalkMark(at geom.Rect) *walkMark {
	m := &walkMark{box: anim.NewRect(at), shown: anim.NewFloat(0)}
	m.Add(m.box, m.shown)
	return m
}

// Layout implements [gunim.Node]: the ring is drawn in the window's
// space, over the whole of it.
func (m *walkMark) Layout(c gunim.Constraints, _ gunim.Frame, _ gunim.Children) geom.Size {
	return c.Max
}

// Paint implements [gunim.Node].
func (m *walkMark) Paint(p *paint.Painter, f gunim.Frame, _ geom.Size, _ gunim.Children) {
	on := min(max(m.shown.Value(), 0), 1)
	if on < 0.01 {
		return
	}
	c := widget.Accent.Get(f.Theme)
	c.A = uint8(float32(c.A) * on)
	p.RRectStroke(m.box.Value().Inset(geom.Uniform(1.5)), 6, paint.Fill{}, paint.Stroke{Width: 3, Color: c})
}

// Transition implements [gunim.Transitioner]: the ring fades as the
// walk ends.
func (m *walkMark) Transition(pr gunim.Presence, f gunim.Frame) bool {
	if pr == gunim.Exiting {
		m.shown.Animate(0, widget.Settle.Get(f.Theme))
	}
	return !m.shown.Active()
}

// walkList is the list of panes shown during a walk, the one reached
// marked.
type walkList struct {
	anim.Group
	in     *anim.Float
	labels []*widget.Label
	at     int
	// box is the card, and rows the row of each pane, as last laid out.
	box  geom.Rect
	rows []geom.Rect
}

func newWalkList() *walkList {
	l := &walkList{in: anim.NewFloat(0)}
	l.Add(l.in)
	return l
}

// show lists titles, the one at marked.
func (l *walkList) show(titles []string, at int) {
	for len(l.labels) < len(titles) {
		lb := widget.NewLabel("")
		lb.MaxLines = 1
		l.labels = append(l.labels, lb)
	}
	l.labels = l.labels[:len(titles)]
	for i, t := range titles {
		l.labels[i].SetText(t)
	}
	l.at = at
}

// walkWidth, walkRow and walkPad are the list's size: a card's width, a
// row per pane, and room around them.
const walkWidth, walkRow, walkPad = 360, 30, 8

// Children implements [gunim.Composite].
func (l *walkList) Children() []gunim.Node {
	out := make([]gunim.Node, len(l.labels))
	for i, lb := range l.labels {
		out[i] = lb
	}
	return out
}

// Layout implements [gunim.Node]. The list takes the whole window, and
// sits in its middle.
func (l *walkList) Layout(c gunim.Constraints, _ gunim.Frame, kids gunim.Children) geom.Size {
	width := min(walkWidth, c.Max.W)
	height := min(float32(len(l.labels))*walkRow+2*walkPad, c.Max.H)
	l.box = geom.Rc((c.Max.W-width)/2, (c.Max.H-height)/2, width, height)
	l.rows = l.rows[:0]
	i := 0
	for k := range kids.All {
		row := geom.Rc(l.box.Min.X+walkPad, l.box.Min.Y+walkPad+float32(i)*walkRow, width-2*walkPad, walkRow)
		l.rows = append(l.rows, row)
		s := k.Layout(gunim.Constraints{Max: geom.Sz(row.Size().W-2*walkPad, walkRow)})
		k.Place(geom.Pt(row.Min.X+walkPad, row.Min.Y+(walkRow-s.H)/2))
		i++
	}
	return c.Max
}

// Paint implements [gunim.Node].
func (l *walkList) Paint(p *paint.Painter, f gunim.Frame, _ geom.Size, kids gunim.Children) {
	t := l.in.Value()
	if t <= 0 {
		return
	}
	th := f.Theme
	defer p.Layer(paint.LayerOpts{Bounds: l.box.Inset(geom.Uniform(-24)), Opacity: min(t, 1)})()
	radius := widget.CardRadius.Get(th)
	p.RRect(l.box, radius, paint.Solid(widget.CardFill.Get(th)))
	if l.at < len(l.rows) {
		p.RRect(l.rows[l.at], radius/2, paint.Solid(widget.Selection.Get(th)))
	}
	for k := range kids.All {
		k.Paint(p)
	}
}

// Transition implements [gunim.Transitioner]: the list fades in, and
// out once the walk ends.
func (l *walkList) Transition(pr gunim.Presence, f gunim.Frame) bool {
	switch pr {
	case gunim.Entering:
		l.in.Animate(1, widget.Quick.Get(f.Theme))
	case gunim.Exiting:
		l.in.Animate(0, widget.Settle.Get(f.Theme))
	case gunim.Present:
	}
	return !l.in.Active()
}

// walkKey reports whether a key release ends a walk: Ctrl let go.
func walkKey(e gi.KeyRelease) bool {
	return e.Key == gi.KeyLeftControl || e.Key == gi.KeyRightControl
}
