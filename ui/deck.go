package ui

import (
	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/input"
)

// Deck shows one of its children at a time, the way a deck of cards
// shows the one on top.
//
// Unlike a split it can hold any number of children, and unlike a split
// it carries on when one is removed. Both are containers, so the same
// code arranges either.
type Deck struct {
	// Keep stops it standing aside when it is down to one pane or none. A
	// window that holds all its panes in one needs it to still be there
	// for the next one, rather than being replaced by whatever pane
	// happened to be left.
	Keep bool

	kids     []Widget
	active   Widget
	size     Size
	hasFocus bool
}

// NewDeck shows the first of the given widgets, ignoring any nil ones.
func NewDeck(kids ...Widget) *Deck {
	d := &Deck{}
	for _, kid := range kids {
		if kid != nil && !contains(d.kids, kid) {
			d.kids = append(d.kids, kid)
		}
	}
	if len(d.kids) > 0 {
		d.active = d.kids[0]
	}
	return d
}

// Add puts a widget at the end and shows it.
func (d *Deck) Add(w Widget) {
	if w == nil || contains(d.kids, w) {
		return
	}
	d.kids = append(d.kids, w)
	d.Focus(w)
	d.Layout(d.size)
}

// Children returns the panes, in the order they were put in. It is the
// deck's own slice: read it, do not write to it. See Container.
func (d *Deck) Children() []Widget { return d.kids }

// Focused returns the pane being shown.
func (d *Deck) Focused() Widget { return d.active }

// Focus brings one of the panes forward, reporting whether it is one.
func (d *Deck) Focus(w Widget) bool {
	if !contains(d.kids, w) {
		return false
	}
	if w == d.active {
		return true
	}
	// Only pass the change on while this holds focus itself, or a
	// pane is told it lost focus it never had.
	if d.hasFocus {
		SetFocus(d.active, false)
	}
	d.active = w
	if d.hasFocus {
		SetFocus(w, true)
	}
	return true
}

// Replace swaps one pane for another, reporting whether old was there.
//
// A widget already there is refused: the same pane twice would give
// Remove two answers and leave a ghost behind.
func (d *Deck) Replace(old, new Widget) bool {
	if new == nil || !contains(d.kids, old) || (new != old && contains(d.kids, new)) {
		return false
	}
	for i, kid := range d.kids {
		if kid != old {
			continue
		}
		d.kids[i] = new
		break
	}
	if d.active == old {
		if d.hasFocus {
			SetFocus(old, false)
		}
		d.active = new
		if d.hasFocus {
			SetFocus(new, true)
		}
	}
	if !d.size.Empty() {
		d.Layout(d.size)
	}
	return true
}

// Remove takes a pane out. A deck with one pane left has nothing to
// choose between, so that pane is reported as what should stand in the
// deck's place; with none left nothing is reported. Keep set reports the
// deck either way and stays where it is.
func (d *Deck) Remove(w Widget) (Widget, bool) {
	at := -1
	for i, kid := range d.kids {
		if kid == w {
			at = i
			break
		}
	}
	if at < 0 {
		return nil, false
	}
	copy(d.kids[at:], d.kids[at+1:])
	// Clear the slot the shift vacated, or the array behind the slice
	// keeps the pane it just gave up alive, along with its scrollback.
	d.kids[len(d.kids)-1] = nil
	d.kids = d.kids[:len(d.kids)-1]

	if d.active == w {
		if d.hasFocus {
			SetFocus(w, false)
		}
		d.active = nil
		if len(d.kids) > 0 {
			// The pane to its right, or the new last one.
			d.active = d.kids[min(at, len(d.kids)-1)]
			if d.hasFocus {
				SetFocus(d.active, true)
			}
		}
	}

	if !d.Keep {
		switch len(d.kids) {
		case 0:
			return nil, true
		case 1:
			return d.kids[0], true
		}
	}
	d.Layout(d.size)
	return d, true
}

// ChildArea returns where a pane is shown. Only the active one has an
// area: the rest are not on screen at all.
func (d *Deck) ChildArea(w Widget) (Rect, bool) {
	// Being the active pane already means being one.
	if w == nil || w != d.active {
		return Rect{}, false
	}
	body := d.body()
	if body.Empty() {
		return Rect{}, false
	}
	return body, true
}

// Layout tells every pane how much room it has, not just the one being
// shown.
//
// A hidden pane still has a program running in it, writing output sized
// to whatever it was last told. Leaving it at the size it started with
// would have a background build wrap its lines at the wrong column, and
// bringing the pane forward does not undo that.
func (d *Deck) Layout(size Size) {
	d.size = size
	body := d.body()
	if body.Empty() {
		return
	}
	for _, kid := range d.kids {
		kid.Layout(body.Size())
	}
}

// Draw paints the pane being shown.
func (d *Deck) Draw(v grid.View) {
	if body := d.body(); !body.Empty() && d.active != nil {
		d.active.Draw(body.In(v))
	}
}

// SetFocus passes focus on to the pane being shown.
func (d *Deck) SetFocus(on bool) {
	if d.hasFocus == on {
		return
	}
	d.hasFocus = on
	SetFocus(d.active, on)
}

// HandleKey offers the key to the pane being shown. It has no keys of
// its own: moving between panes is a command, so it can be rebound and
// can appear in a menu.
func (d *Deck) HandleKey(ev input.Event) (bool, error) {
	return HandleKey(d.active, ev)
}

// HandleMouse passes the event to the pane being shown.
func (d *Deck) HandleMouse(ev input.MouseEvent) (bool, error) {
	body := d.body()
	if d.active == nil || !body.Contains(ev.Col, ev.Row) {
		return false, nil
	}
	ev.Col, ev.Row = body.Local(ev.Col, ev.Row)
	return HandleMouse(d.active, ev)
}

// body returns where the pane being shown goes, which is all the room
// there is.
func (d *Deck) body() Rect {
	if d.size.Empty() {
		return Rect{}
	}
	return Rect{Cols: d.size.Cols, Rows: d.size.Rows}
}
