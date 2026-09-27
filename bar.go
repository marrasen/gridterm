package main

import (
	"slices"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/paint"
	"github.com/marrasen/gunim/widget"
)

// buttonBar is a strip along a pane: a line of text at the start, and
// the buttons that fit what the pane shows now at the end.
type buttonBar struct {
	label *widget.Label
	// shown are the buttons in the bar now.
	shown []gunim.Node
}

func newButtonBar() *buttonBar {
	b := &buttonBar{label: widget.NewLabel("")}
	b.label.Size, b.label.Color, b.label.MaxLines = smallText, faint, 1
	return b
}

// set says text, beside buttons: those new to the bar arrive, and
// those it had and has no more go.
func (b *buttonBar) set(text string, u *gunim.UI, buttons ...*widget.Button) {
	b.label.SetText(text)
	want := make([]gunim.Node, len(buttons))
	for i, x := range buttons {
		want[i] = x
	}
	for _, n := range b.shown {
		if !slices.Contains(want, n) {
			u.Remove(n)
		}
	}
	for _, n := range want {
		if !slices.Contains(b.shown, n) {
			u.Insert(b, n)
		}
	}
	b.shown = want
}

// Children implements [gunim.Composite].
func (b *buttonBar) Children() []gunim.Node { return append([]gunim.Node{b.label}, b.shown...) }

// Layout implements [gunim.Node]: the label at the start, the buttons
// at the end, in the order they were given.
func (b *buttonBar) Layout(c gunim.Constraints, _ gunim.Frame, kids gunim.Children) geom.Size {
	const padX, gap, height = 12, 8, 44
	// The children arrive in the order they were inserted, which is
	// not the order asked for; each is placed by its place in shown.
	sizes := map[gunim.Node]geom.Size{}
	byNode := map[gunim.Node]gunim.Child{}
	for i := 1; i < kids.Len(); i++ {
		k := kids.At(i)
		byNode[k.Node()] = k
		sizes[k.Node()] = k.Layout(gunim.Constraints{Max: geom.Sz(c.Max.W, height)})
	}
	x := c.Max.W - padX
	for i := len(b.shown) - 1; i >= 0; i-- {
		k, ok := byNode[b.shown[i]]
		if !ok {
			continue
		}
		s := sizes[b.shown[i]]
		x -= s.W
		k.Place(geom.Pt(x, (height-s.H)/2))
		x -= gap
	}
	k := kids.At(0)
	s := k.Layout(gunim.Constraints{Max: geom.Sz(max(0, x-padX), height)})
	k.Place(geom.Pt(padX, (height-s.H)/2))
	return c.Constrain(geom.Sz(c.Max.W, height))
}

// Paint implements [gunim.Node].
func (b *buttonBar) Paint(p *paint.Painter, f gunim.Frame, box geom.Size, kids gunim.Children) {
	p.RRect(geom.Rect{Max: box.Point()}, 0, paint.Solid(widget.MenuFill.Get(f.Theme)))
	p.RRect(geom.Rect{Max: geom.Pt(box.W, 1)}, 0, paint.Solid(widget.MenuBorder.Get(f.Theme)))
	for k := range kids.All {
		k.Paint(p)
	}
}

// captioned is a pane under a line naming it, for a window that shows
// pane titles.
type captioned struct {
	pane  gunim.Node
	label *widget.Label
}

func newCaptioned(pane gunim.Node) *captioned {
	l := widget.NewLabel("")
	l.Size, l.Color, l.MaxLines = smallText, faint, 1
	return &captioned{pane: pane, label: l}
}

// captionHeight is the height of the line over a pane.
const captionHeight = 22

// Children implements [gunim.Composite].
func (c *captioned) Children() []gunim.Node { return []gunim.Node{c.label, c.pane} }

// Layout implements [gunim.Node]: the line along the top, and the pane
// in the rest.
func (c *captioned) Layout(cs gunim.Constraints, _ gunim.Frame, kids gunim.Children) geom.Size {
	const padX = 10
	l := kids.At(0)
	s := l.Layout(gunim.Constraints{Max: geom.Sz(max(0, cs.Max.W-2*padX), captionHeight)})
	l.Place(geom.Pt(padX, (captionHeight-s.H)/2))
	p := kids.At(1)
	p.Layout(gunim.Tight(geom.Sz(cs.Max.W, max(0, cs.Max.H-captionHeight))))
	p.Place(geom.Pt(0, captionHeight))
	return cs.Max
}

// Paint implements [gunim.Node].
func (c *captioned) Paint(p *paint.Painter, f gunim.Frame, box geom.Size, kids gunim.Children) {
	p.RRect(geom.Rect{Max: geom.Pt(box.W, captionHeight)}, 0, paint.Solid(widget.MenuFill.Get(f.Theme)))
	for k := range kids.All {
		k.Paint(p)
	}
}
