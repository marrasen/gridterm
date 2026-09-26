package main

import (
	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/geom"
	gi "github.com/marrasen/gunim/input"
	"github.com/marrasen/gunim/paint"
	"github.com/marrasen/gunim/widget"
)

// keyBar is the strip of keys along the foot of a file pane, as
// gridterm's file manager has it: each key with what it does, a click
// on one pressing it, and dimmed where it does nothing here.
type keyBar struct {
	// pressed takes a key the bar was clicked on.
	pressed func(gi.KeyPress, *gunim.UI)
	keys   []barKey
	labels []*widget.Label
	// boxes are where each key was laid out, and shown how many fit.
	boxes []geom.Rect
	shown int
}

// barKey is one key on the bar: what it is called, the press it
// stands for, and whether it does anything now.
type barKey struct {
	name  string
	press gi.KeyPress
	on    func() bool
}

func newKeyBar(keys ...barKey) *keyBar {
	b := &keyBar{keys: keys}
	for _, k := range keys {
		l := widget.NewLabel(k.name)
		l.Size, l.MaxLines = smallText, 1
		b.labels = append(b.labels, l)
	}
	return b
}

// Children implements [gunim.Composite].
func (b *keyBar) Children() []gunim.Node {
	out := make([]gunim.Node, len(b.labels))
	for i, l := range b.labels {
		out[i] = l
	}
	return out
}

// keyBarHeight, keyGap and keyPad are the bar's measures.
const keyBarHeight, keyGap, keyPad = 28, 4, 8

// Layout implements [gunim.Node]: the keys from the start, as many as
// fit.
func (b *keyBar) Layout(c gunim.Constraints, _ gunim.Frame, kids gunim.Children) geom.Size {
	own := c.Constrain(geom.Sz(c.Max.W, keyBarHeight))
	b.boxes, b.shown = b.boxes[:0], 0
	x := float32(keyGap)
	i := 0
	for k := range kids.All {
		s := k.Layout(gunim.Constraints{Max: geom.Sz(own.W, keyBarHeight)})
		w := s.W + 2*keyPad
		box := geom.Rc(x, 3, w, keyBarHeight-6)
		if x+w > own.W {
			// Past the end: laid out off to the side, and not drawn.
			k.Place(geom.Pt(own.W+1, 0))
			b.boxes = append(b.boxes, geom.Rect{})
			i++
			continue
		}
		k.Place(geom.Pt(x+keyPad, (keyBarHeight-s.H)/2))
		b.boxes = append(b.boxes, box)
		b.shown++
		x += w + keyGap
		i++
	}
	return own
}

// Paint implements [gunim.Node].
func (b *keyBar) Paint(p *paint.Painter, f gunim.Frame, box geom.Size, kids gunim.Children) {
	th := f.Theme
	i := 0
	for k := range kids.All {
		if i < len(b.boxes) && !b.boxes[i].Empty() {
			on := b.keys[i].on == nil || b.keys[i].on()
			if on {
				p.RRect(b.boxes[i], 5, paint.Solid(widget.FieldFill.Get(th)))
				b.labels[i].Color = widget.Ink
			} else {
				b.labels[i].Color = faint
			}
			k.Paint(p)
		}
		i++
	}
}

// Handle implements [gunim.Handler]: a click presses the key clicked,
// when it does anything here.
func (b *keyBar) Handle(e gi.Event, u *gunim.UI) bool {
	down, ok := e.(gi.PointerDown)
	if !ok {
		return false
	}
	for i, r := range b.boxes {
		if r.Empty() || !r.Contains(down.Pos) {
			continue
		}
		if k := b.keys[i]; (k.on == nil || k.on()) && b.pressed != nil {
			b.pressed(k.press, u)
		}
		return true
	}
	return false
}

// errLine is the line under a file pane's path saying its folder could
// not be read, as gridterm's has it: short, and a click shows the whole
// reason, which a line trimmed to the pane would cut.
type errLine struct {
	b     *browser
	label *widget.Label
}

// Children implements [gunim.Composite].
func (l *errLine) Children() []gunim.Node { return []gunim.Node{l.label} }

// Layout implements [gunim.Node]: no room at all while there is
// nothing to say.
func (l *errLine) Layout(c gunim.Constraints, _ gunim.Frame, kids gunim.Children) geom.Size {
	if l.label.Text == "" {
		kids.At(0).Layout(gunim.Constraints{})
		return geom.Size{}
	}
	s := kids.At(0).Layout(gunim.Constraints{Max: geom.Sz(c.Max.W-2*keyPad, c.Max.H)})
	kids.At(0).Place(geom.Pt(keyPad, 2))
	return c.Constrain(geom.Sz(c.Max.W, s.H+4))
}

// Paint implements [gunim.Node].
func (l *errLine) Paint(p *paint.Painter, _ gunim.Frame, _ geom.Size, kids gunim.Children) {
	if l.label.Text != "" {
		kids.At(0).Paint(p)
	}
}

// Handle implements [gunim.Handler]: a click shows why.
func (l *errLine) Handle(e gi.Event, u *gunim.UI) bool {
	if _, ok := e.(gi.PointerDown); !ok || l.b.st.Err == "" {
		return false
	}
	d := widget.NewDialog("Couldn't read the folder")
	d.Body = widget.NewLabel(l.b.st.Path + "\n\n" + l.b.st.Err)
	d.SetButtons("OK", "")
	d.Accept, d.Dismiss = DialogClosed{}, DialogClosed{}
	l.b.w.openDialog(d, u)
	return true
}
