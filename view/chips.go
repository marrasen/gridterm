package view

import (
	"image/color"
	"strconv"

	"github.com/marrasen/kakel/app"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/geom"
	gi "github.com/marrasen/gunim/input"
	"github.com/marrasen/gunim/paint"
	"github.com/marrasen/gunim/widget"
)

// The chips at the end of the menu bar: what the window is doing for
// someone else, a click away from its dialog.
// "Agent Share" while panes are shared with an agent, and "Serving"
// while the window is served, with how many windows are watching.

// chip is one chip: what it says, its colour, and what a click does.
type chip struct {
	text   string
	colour color.NRGBA
	do     func(*gunim.UI)
	// off, when set, puts an × on the chip that turns off what it says
	// is on. accent colours it in the theme's accent, in place of colour.
	off    func(*gunim.UI)
	accent bool
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

// chipPad and chipGap are a chip's room, and chipCross the room its ×
// takes.
const chipPad, chipGap, chipHeight, chipCross = 10, 6, 28, 14

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
		w := s.W + 2*chipPad
		if b.chips[i-1].off != nil {
			w += chipCross
		}
		// The pill and its words share one middle: the bar's.
		bh := min(s.H+8, h)
		box := geom.Rc(x, (h-bh)/2, w, bh)
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
func (b *chipBar) Paint(p *paint.Painter, f gunim.Frame, _ geom.Size, kids gunim.Children) {
	i := 0
	for k := range kids.All {
		if i >= len(b.boxes) {
			break
		}
		{
			// A pill in the chip's colour, as a server card's state is: a
			// faint tint of it and a thin ring of it, rather than a dark
			// patch.
			c := b.chips[i].colour
			if b.chips[i].accent {
				c = widget.Accent.Get(f.Theme)
			}
			fill, ring := c, c
			fill.A, ring.A = 0x22, 0x66
			p.RRectStroke(b.boxes[i], b.boxes[i].Size().H/2, paint.Solid(fill), paint.Stroke{Width: 1, Color: ring})
		}
		k.Paint(p)
		if b.chips[i].off != nil {
			paintCross(p, b.crossOf(i), widget.Ink.Get(f.Theme))
		}
		i++
	}
}

// crossOf is where chip i's × is.
func (b *chipBar) crossOf(i int) geom.Rect {
	r := b.boxes[i]
	const s = 9
	return geom.Rc(r.Max.X-chipPad-s+2, r.Min.Y+(r.Size().H-s)/2, s, s)
}

// Handle implements [gunim.Handler]: a click on a chip does what it
// offers.
func (b *chipBar) Handle(e gi.Event, u *gunim.UI) bool {
	down, ok := e.(gi.PointerDown)
	if !ok {
		return false
	}
	for i, r := range b.boxes {
		if b.chips[i].off != nil && b.crossOf(i).Inset(geom.Uniform(-4)).Contains(down.Pos) {
			b.chips[i].off(u)
			return true
		}
		if r.Contains(down.Pos) && b.chips[i].do != nil {
			b.chips[i].do(u)
			return true
		}
	}
	return false
}

// showChips puts on the bar what the window is doing for others.
func (w *Window) showChips(st app.State) {
	var chips []chip
	if st.Share.Code != "" && len(st.Share.Panes) > 0 {
		chips = append(chips, chip{text: "Agent Share", colour: st.Marks.Agent, do: func(u *gunim.UI) { w.shareDialog(w.share, u) }})
	}
	if w.typeAll {
		// Said plainly while it lasts, as a key typed where it was not
		// meant is typed in every pane.
		chips = append(chips, chip{text: "Typing in All Panes", accent: true, off: func(u *gunim.UI) { w.setTypeAll(false, u) }})
	}
	if st.Serving.On {
		text := "Serving"
		if n := len(st.Serving.Clients); n > 0 {
			text += " · " + strconv.Itoa(n)
		}
		chips = append(chips, chip{text: text, colour: st.Marks.Watched, do: func(u *gunim.UI) { w.servingDialog(w.serving, u) }})
	}
	w.chips.show(chips)
}
