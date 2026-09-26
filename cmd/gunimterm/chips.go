package main

import (
	"image/color"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/geom"
	gi "github.com/marrasen/gunim/input"
	"github.com/marrasen/gunim/paint"
	"github.com/marrasen/gunim/widget"
)

// The chips at the end of the menu bar, as gridterm has them: what the
// window is doing for someone else, a click away from its dialog.
// "Agent Share" while panes are shared with an agent, and "Serving"
// while the window is served, with how many windows are watching.

// chip is one chip: what it says, its colour, and what a click does.
type chip struct {
	text   string
	colour color.NRGBA
	do     func(*gunim.UI)
}

// chipBar shows the chips.
type chipBar struct {
	chips  []chip
	labels []*widget.Label
	boxes  []geom.Rect
}

// mostChips is how many chips the bar holds. Its labels are made with
// it, since the window takes a node's children as it is mounted.
const mostChips = 4

func newChipBar() *chipBar {
	b := &chipBar{}
	for range mostChips {
		l := widget.NewLabel("")
		l.Size, l.MaxLines = smallText, 1
		b.labels = append(b.labels, l)
	}
	return b
}

// show puts chips on the bar.
func (b *chipBar) show(chips []chip) {
	b.chips = chips[:min(len(chips), mostChips)]
	for i, l := range b.labels {
		l.SetText("")
		if i < len(b.chips) {
			l.SetText(b.chips[i].text)
		}
	}
}

// Children implements [gunim.Composite].
func (b *chipBar) Children() []gunim.Node {
	out := make([]gunim.Node, len(b.labels))
	for i, l := range b.labels {
		out[i] = l
	}
	return out
}

// chipPad and chipGap are a chip's room.
const chipPad, chipGap, chipHeight = 10, 6, 28

// Layout implements [gunim.Node]: the chips side by side, as wide as
// they need.
func (b *chipBar) Layout(c gunim.Constraints, _ gunim.Frame, kids gunim.Children) geom.Size {
	// No taller than the menu bar beside it, which sets the row.
	h := float32(chipHeight)
	if c.Max.H > 0 {
		h = min(h, c.Max.H)
	}
	b.boxes = b.boxes[:0]
	x := float32(0)
	i := 0
	for k := range kids.All {
		if i >= len(b.chips) {
			k.Layout(gunim.Constraints{})
			continue
		}
		i++
		s := k.Layout(gunim.Constraints{Max: geom.Sz(300, h)})
		box := geom.Rc(x, (h-s.H)/2-3, s.W+2*chipPad, min(s.H+6, h))
		k.Place(geom.Pt(x+chipPad, (h-s.H)/2))
		b.boxes = append(b.boxes, box)
		x += box.Size().W + chipGap
	}
	if x > 0 {
		x += chipGap
	}
	return c.Constrain(geom.Sz(x, h))
}

// Paint implements [gunim.Node].
func (b *chipBar) Paint(p *paint.Painter, _ gunim.Frame, _ geom.Size, kids gunim.Children) {
	i := 0
	for k := range kids.All {
		if i >= len(b.boxes) {
			break
		}
		{
			fill := b.chips[i].colour
			fill.A = 0x30
			p.RRect(b.boxes[i], b.boxes[i].Size().H/2, paint.Solid(fill))
		}
		k.Paint(p)
		i++
	}
}

// Handle implements [gunim.Handler]: a click on a chip does what it
// offers.
func (b *chipBar) Handle(e gi.Event, u *gunim.UI) bool {
	down, ok := e.(gi.PointerDown)
	if !ok {
		return false
	}
	for i, r := range b.boxes {
		if r.Contains(down.Pos) && b.chips[i].do != nil {
			b.chips[i].do(u)
			return true
		}
	}
	return false
}

// showChips puts on the bar what the window is doing for others.
func (w *window) showChips(st State) {
	var chips []chip
	if st.Share.Code != "" && len(st.Share.Panes) > 0 {
		chips = append(chips, chip{text: "Agent Share", colour: st.Marks.Agent, do: func(u *gunim.UI) { w.shareDialog(w.share, u) }})
	}
	if st.Serving.On {
		text := "Serving"
		if n := len(st.Serving.Clients); n > 0 {
			text += " · " + itoa(n)
		}
		chips = append(chips, chip{text: text, colour: st.Marks.Watched, do: func(u *gunim.UI) { w.servingDialog(w.serving, u) }})
	}
	w.chips.show(chips)
}
