package view

import (
	"image/color"
	"slices"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/anim"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/icon"
	"github.com/marrasen/gunim/input"
	"github.com/marrasen/gunim/paint"
	"github.com/marrasen/gunim/text"
	"github.com/marrasen/gunim/widget"

	"github.com/marrasen/kakel/app"
	"github.com/marrasen/kakel/machines"
)

// A split's chooser: the new half of a split, made at once, before
// anything is put in it. It offers new terminals as buttons, "+
// Terminal" and one for each shell and machine, and the other panes as
// small live pictures of themselves, to move there. What is picked takes
// the chooser's place. Escape, or closing it, gives its half back.

// chooser is the node a chooser pane shows.
type chooser struct {
	w  *Window
	id string
	// buttons are the new things to open, and thumbs the panes to move
	// there; keys is what they were made for, to make them again only
	// when that changes.
	buttons []*widget.Button
	thumbs  []*thumb
	heading *widget.Label
	// made keeps each button and picture by what it offers, so one that
	// stays is the same node from one change to the next, and the
	// keyboard stays on it.
	made map[string]gunim.Node
}

func newChooser(w *Window, id string) *chooser {
	c := &chooser{w: w, id: id, heading: widget.NewLabel("Put here"), made: map[string]gunim.Node{}}
	c.heading.Color = widget.Placeholder
	c.refresh(nil)
	return c
}

// refresh brings the buttons and the pictures up to date with what
// there is to offer: the shells here, the machines reached, the panes
// open. Those new arrive and those gone leave, through u once the
// chooser is in the tree; before, u is nil.
func (c *chooser) refresh(u *gunim.UI) {
	type offer struct {
		key, label string
		in         gunim.Intent
		local      func(*gunim.UI)
	}
	var offers []offer
	add := func(label string, in gunim.Intent) {
		offers = append(offers, offer{key: "new:" + label, label: label, in: in})
	}
	add("Terminal", app.SplitPane{Instead: c.id})
	if len(c.w.shellChoices) > 1 {
		for _, sh := range c.w.shellChoices {
			add(sh.Title, app.SplitPane{Instead: c.id, Shell: sh.ID})
		}
	}
	here := c.w.filesKeyOf(c.id)
	reach := c.w.machines()
	for _, m := range reach {
		if m != here {
			add("Terminal on "+c.w.nameOf(m), app.SplitPane{Instead: c.id, Machine: m, Elsewhere: true})
		}
	}
	// The saved servers not connected to, which cost a sign-in first.
	for _, h := range c.w.saved {
		if !h.Window && !slices.Contains(reach, machines.ID(h.ID)) {
			add("Terminal on "+h.Name, app.SplitPane{Instead: c.id, Machine: machines.ID(h.ID), Elsewhere: true})
		}
	}
	offers = append(offers, offer{key: "new:command", label: "Command…", local: func(u *gunim.UI) {
		c.w.commandDialogAt(here, app.Placement{Instead: c.id}, u)
	}})
	var panes []app.Pane
	from := ""
	for _, p := range c.w.panes {
		if p.ID == c.id {
			from = p.SplitFrom
		}
	}
	for _, p := range c.w.panes {
		if p.ID != c.id && p.ID != from && p.Kind != app.KindChooser {
			panes = append(panes, p)
		}
	}
	was := c.Children()[1:]
	c.buttons = c.buttons[:0]
	for _, o := range offers {
		b, ok := c.made[o.key].(*widget.Button)
		if !ok {
			b = widget.NewButton(o.label)
			b.Icon = icon.Plus
			c.made[o.key] = b
		}
		if o.local != nil {
			b.OnActivate(o.local)
		} else {
			in := o.in
			b.OnActivate(func(u *gunim.UI) { u.Send(c, in) })
		}
		c.buttons = append(c.buttons, b)
	}
	c.thumbs = c.thumbs[:0]
	for _, p := range panes {
		key := "pane:" + p.ID
		t, ok := c.made[key].(*thumb)
		if !ok {
			t = newThumb(c, p)
			c.made[key] = t
		}
		t.retitle(p.Title)
		c.thumbs = append(c.thumbs, t)
	}
	now := c.Children()[1:]
	for key, n := range c.made {
		if !slices.Contains(now, n) {
			delete(c.made, key)
		}
	}
	// Off stage, or not yet on it, the chooser is out of the tree, and
	// its children are read afresh when it goes back in.
	if u == nil || u.Presence(c) == gunim.Exiting {
		return
	}
	focused := u.Focused()
	for _, n := range was {
		if !slices.Contains(now, n) {
			if n == focused {
				u.Focus(c.first())
			}
			u.Remove(n)
		}
	}
	for _, n := range now {
		if !slices.Contains(was, n) {
			u.Insert(c, n)
		}
	}
}

// first is what takes the keyboard as the chooser is put on stage.
func (c *chooser) first() gunim.Node {
	if len(c.buttons) > 0 {
		return c.buttons[0]
	}
	return c
}

// Children implements [gunim.Composite].
func (c *chooser) Children() []gunim.Node {
	out := []gunim.Node{c.heading}
	for _, b := range c.buttons {
		out = append(out, b)
	}
	for _, t := range c.thumbs {
		out = append(out, t)
	}
	return out
}

// Focusable implements [gunim.Focusable], so Escape reaches the chooser
// with nothing in it focused.
func (c *chooser) Focusable() bool { return true }

// focusables are what the arrows and Tab move through, in order.
func (c *chooser) focusables() []gunim.Node {
	var out []gunim.Node
	for _, b := range c.buttons {
		out = append(out, b)
	}
	for _, t := range c.thumbs {
		out = append(out, t)
	}
	return out
}

// Handle implements [gunim.Handler]: Escape gives the half back, and
// Tab and the arrows move among what is offered.
func (c *chooser) Handle(e input.Event, u *gunim.UI) bool {
	k, ok := e.(input.KeyPress)
	if !ok {
		return false
	}
	step := 0
	switch {
	case k.Key == input.KeyEscape:
		u.Send(c, app.ClosePane{Pane: c.id})
		return true
	case k.Key == input.KeyTab && k.Mods.Has(input.ModShift), k.Key == input.KeyLeft, k.Key == input.KeyUp:
		step = -1
	case k.Key == input.KeyTab && k.Mods == 0, k.Key == input.KeyRight, k.Key == input.KeyDown:
		step = 1
	default:
		return false
	}
	all := c.focusables()
	if len(all) == 0 {
		return true
	}
	at := slices.Index(all, u.Focused())
	next := 0
	if at >= 0 {
		next = (at + step + len(all)) % len(all)
	}
	u.Focus(all[next])
	return true
}

// Measures of the chooser.
const (
	chooserPad   = 16
	chooserGap   = 8
	thumbMost    = 220
	thumbLeast   = 120
	thumbCaption = 20
)

// Layout implements [gunim.Node]: the heading, the buttons in rows that
// wrap, and under them the pictures in a grid, each as wide as fits.
func (c *chooser) Layout(cs gunim.Constraints, _ gunim.Frame, kids gunim.Children) geom.Size {
	box := cs.Max
	width := max(box.W-2*chooserPad, 0)
	// The children come in the order they arrived, with those on their
	// way out among them: each is placed by what it is.
	byNode := map[gunim.Node]gunim.Child{}
	for i := range kids.Len() {
		byNode[kids.At(i).Node()] = kids.At(i)
	}
	y := float32(chooserPad)
	if k, ok := byNode[c.heading]; ok {
		h := k.Layout(gunim.Constraints{Max: geom.Sz(width, box.H)})
		k.Place(geom.Pt(chooserPad, y))
		y += h.H + chooserGap
	}
	x, row := float32(chooserPad), float32(0)
	for _, b := range c.buttons {
		k, ok := byNode[b]
		if !ok {
			continue
		}
		s := k.Layout(gunim.Constraints{Max: geom.Sz(width, box.H)})
		if x > chooserPad && x+s.W > chooserPad+width {
			x, y = chooserPad, y+row+chooserGap
			row = 0
		}
		k.Place(geom.Pt(x, y))
		x += s.W + chooserGap
		row = max(row, s.H)
	}
	y += row + 2*chooserGap
	if n := len(c.thumbs); n > 0 {
		cols := max(1, int((width+chooserGap)/(thumbLeast+chooserGap)))
		cols = min(cols, n)
		tw := min(thumbMost, (width-float32(cols-1)*chooserGap)/float32(cols))
		th := tw*0.62 + thumbCaption
		for i, t := range c.thumbs {
			k, ok := byNode[t]
			if !ok {
				continue
			}
			k.Layout(gunim.Tight(geom.Sz(tw, th)))
			k.Place(geom.Pt(chooserPad+float32(i%cols)*(tw+chooserGap), y+float32(i/cols)*(th+chooserGap)))
		}
	}
	return box
}

// Paint implements [gunim.Node].
func (c *chooser) Paint(p *paint.Painter, f gunim.Frame, box geom.Size, kids gunim.Children) {
	p.RRect(geom.Rect{Max: box.Point()}, 0, paint.Solid(widget.Background.Get(f.Theme)))
	for i := range kids.Len() {
		kids.At(i).Paint(p)
	}
}

// thumb is a pane offered in a chooser: a small live picture of it and
// its title, which a click or Enter moves into the chooser's place.
type thumb struct {
	anim.Group
	c     *chooser
	id    string
	title string
	label text.Run
	hover *anim.Float
	ring  *anim.Float
}

func newThumb(c *chooser, p app.Pane) *thumb {
	t := &thumb{c: c, id: p.ID, hover: anim.NewFloat(0), ring: anim.NewFloat(0)}
	t.retitle(p.Title)
	t.Add(t.hover, t.ring)
	return t
}

// retitle names the picture after its pane, as the pane is called now.
func (t *thumb) retitle(title string) {
	if title != t.title || t.label.Advance == 0 {
		t.title = title
		t.label = text.Default().Shape(title, 12)
	}
}

// Focusable implements [gunim.Focusable].
func (t *thumb) Focusable() bool { return true }

// pick moves the pane into the chooser's place.
func (t *thumb) pick(u *gunim.UI) {
	u.Send(t, app.MovePane{Pane: t.id, Instead: t.c.id})
}

// Handle implements [gunim.Handler].
func (t *thumb) Handle(e input.Event, u *gunim.UI) bool {
	switch e := e.(type) {
	case input.PointerEnter:
		t.hover.Animate(1, widget.Quick.Get(u.Theme()))
	case input.PointerLeave:
		t.hover.Animate(0, widget.Settle.Get(u.Theme()))
	case input.PointerDown:
		if e.Button == input.ButtonPrimary {
			t.pick(u)
			return true
		}
	case input.FocusRing:
		// Shown as gunim shows a button's: while the keyboard is in use.
		to := float32(0)
		if e.On {
			to = 1
		}
		t.ring.Animate(to, widget.Quick.Get(u.Theme()))
	case input.FocusLost:
		t.ring.Animate(0, widget.Settle.Get(u.Theme()))
	case input.KeyPress:
		if e.Key == input.KeyEnter || e.Key == input.KeyKPEnter || e.Key == input.KeySpace {
			t.pick(u)
			return true
		}
	}
	return false
}

// Layout implements [gunim.Node].
func (t *thumb) Layout(cs gunim.Constraints, _ gunim.Frame, _ gunim.Children) geom.Size {
	return cs.Max
}

// Paint implements [gunim.Node]: the pane shrunk, its title under it,
// and a ring while it has the keyboard or the pointer.
func (t *thumb) Paint(p *paint.Painter, f gunim.Frame, box geom.Size, _ gunim.Children) {
	th := f.Theme
	pic := geom.Rc(0, 0, box.W, box.H-thumbCaption)
	p.ShadowRRect(pic, 6, paint.Solid(widget.Background.Get(th)), paint.Shadow{Offset: geom.Pt(0, 2), Blur: 10, Color: color.NRGBA{A: 0x70}})
	if !t.c.w.paintMiniature(p, f, t.id, pic, t.c.w.size) {
		// Nothing drawn of it yet: its name in the middle.
		ink := widget.Placeholder.Get(th)
		t.label.Paint(p, geom.Pt((pic.Size().W-t.label.Advance)/2, (pic.Size().H-t.label.Height())/2), ink)
	}
	if on := min(max(max(t.ring.Value(), t.hover.Value()), 0), 1); on > 0.01 {
		ring := switcherRing.Get(th)
		ring.A = uint8(float32(ring.A) * on)
		p.RRectStroke(pic.Inset(geom.Uniform(-2)), 8, paint.Fill{}, paint.Stroke{Width: 2, Color: ring})
	}
	ink := widget.Ink.Get(th)
	run := t.label
	defer p.Layer(paint.LayerOpts{Bounds: geom.Rc(0, pic.Max.Y, box.W, thumbCaption), Opacity: 1, Clip: true})()
	run.Paint(p, geom.Pt(0, pic.Max.Y+(thumbCaption-run.Height())/2), ink)
}
