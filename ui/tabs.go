package ui

import (
	"image/color"
	"strconv"

	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/input"
)

// stripRows is how tall the row of labels is.
const stripRows = 1

// Titled is a widget with a name to show, in a tab strip or a menu.
type Titled interface {
	Widget
	Title() string
}

// Tabs shows one of its children at a time, with a strip of labels above
// them.
//
// Unlike a split it can hold any number of children, and unlike a split
// it carries on when one is removed. Both are containers, so the same
// code arranges either.
type Tabs struct {
	// StripBG fills the strip, and ActiveFG and ActiveBG mark the tab
	// being shown. InactiveFG colours the rest.
	StripBG    color.RGBA
	ActiveFG   color.RGBA
	ActiveBG   color.RGBA
	InactiveFG color.RGBA

	// Label names a tab. The default is the widget's own title, or its
	// position when it has none.
	Label func(w Widget, i int) string

	kids     []Widget
	active   Widget
	size     Size
	hasFocus bool

	// stripHeld is true between a press on a label and its release, so a
	// drag that starts on the strip and wanders into the tab below does
	// not hand that tab a release for a press it never saw. stripButton
	// is the button that started it: tapping another mid-drag must not
	// end the gesture.
	stripHeld   bool
	stripButton input.MouseButton

	// buf keeps the label strip off the layer until it is finished.
	// Filling the strip and then writing the labels over it changes the
	// same cell twice, which would dirty the row on every frame.
	buf buffer
}

// NewTabs shows the first of the given widgets, ignoring any nil ones.
func NewTabs(kids ...Widget) *Tabs {
	t := &Tabs{}
	for _, kid := range kids {
		if kid != nil && !contains(t.kids, kid) {
			t.kids = append(t.kids, kid)
		}
	}
	if len(t.kids) > 0 {
		t.active = t.kids[0]
	}
	return t
}

// Add puts a widget at the end and shows it.
func (t *Tabs) Add(w Widget) {
	if w == nil || contains(t.kids, w) {
		return
	}
	t.kids = append(t.kids, w)
	t.Focus(w)
	t.Layout(t.size)
}

// Children returns the tabs, left to right. The slice is a copy, so a
// caller cannot swap a tab out from under the strip.
func (t *Tabs) Children() []Widget {
	out := make([]Widget, len(t.kids))
	copy(out, t.kids)
	return out
}

// Focused returns the tab being shown.
func (t *Tabs) Focused() Widget { return t.active }

// Focus brings one of the tabs forward, reporting whether it is one.
func (t *Tabs) Focus(w Widget) bool {
	if !contains(t.kids, w) {
		return false
	}
	if w == t.active {
		return true
	}
	// Only pass the change on while this strip holds focus itself, or a
	// tab is told it lost focus it never had.
	if t.hasFocus {
		SetFocus(t.active, false)
	}
	t.active = w
	if t.hasFocus {
		SetFocus(w, true)
	}
	return true
}

// Replace swaps one tab for another, reporting whether old was there.
//
// A widget already in the strip is refused: the same tab twice would
// give Remove two answers and leave the strip holding a ghost.
func (t *Tabs) Replace(old, new Widget) bool {
	if new == nil || !contains(t.kids, old) || (new != old && contains(t.kids, new)) {
		return false
	}
	for i, kid := range t.kids {
		if kid != old {
			continue
		}
		t.kids[i] = new
		break
	}
	if t.active == old {
		if t.hasFocus {
			SetFocus(old, false)
		}
		t.active = new
		if t.hasFocus {
			SetFocus(new, true)
		}
	}
	if !t.size.Empty() {
		t.Layout(t.size)
	}
	return true
}

// Remove takes a tab out. A strip with one tab left has nothing to
// choose between, so it reports that tab as what should stand in its
// place; with none left it reports nothing.
func (t *Tabs) Remove(w Widget) (Widget, bool) {
	at := -1
	for i, kid := range t.kids {
		if kid == w {
			at = i
			break
		}
	}
	if at < 0 {
		return nil, false
	}
	copy(t.kids[at:], t.kids[at+1:])
	// Clear the slot the shift vacated, or the array behind the slice
	// keeps the tab it just gave up alive, along with its scrollback.
	t.kids[len(t.kids)-1] = nil
	t.kids = t.kids[:len(t.kids)-1]

	if t.active == w {
		if t.hasFocus {
			SetFocus(w, false)
		}
		t.active = nil
		if len(t.kids) > 0 {
			// The tab to its right, or the new last one.
			t.active = t.kids[min(at, len(t.kids)-1)]
			if t.hasFocus {
				SetFocus(t.active, true)
			}
		}
	}

	switch len(t.kids) {
	case 0:
		return nil, true
	case 1:
		return t.kids[0], true
	}
	t.Layout(t.size)
	return t, true
}

// ChildArea returns where a tab is shown. Only the active one has an
// area: the rest are not on screen at all.
func (t *Tabs) ChildArea(w Widget) (Rect, bool) {
	// Being the active tab already means being a tab.
	if w == nil || w != t.active {
		return Rect{}, false
	}
	body := t.body()
	if body.Empty() {
		return Rect{}, false
	}
	return body, true
}

// Layout tells every tab how much room it has, not just the one being
// shown.
//
// A hidden tab still has a program running in it, writing output sized
// to whatever it was last told. Leaving it at the size it started with
// would have a background build wrap its lines at the wrong column, and
// bringing the tab forward does not undo that.
func (t *Tabs) Layout(size Size) {
	t.size = size
	body := t.body()
	if body.Empty() {
		return
	}
	for _, kid := range t.kids {
		kid.Layout(body.Size())
	}
}

// Draw paints the strip and the tab being shown.
func (t *Tabs) Draw(v grid.View) {
	t.drawStrip(v)
	if body := t.body(); !body.Empty() && t.active != nil {
		t.active.Draw(body.In(v))
	}
}

// SetFocus passes focus on to the tab being shown.
func (t *Tabs) SetFocus(on bool) {
	if t.hasFocus == on {
		return
	}
	t.hasFocus = on
	SetFocus(t.active, on)
}

// HandleKey offers the key to the tab being shown. A strip has no keys
// of its own: moving between tabs is a command, so it can be rebound and
// can appear in a menu.
func (t *Tabs) HandleKey(ev input.Event) (bool, error) {
	return HandleKey(t.active, ev)
}

// HandleMouse selects a tab clicked in the strip, or passes the event to
// the tab being shown.
func (t *Tabs) HandleMouse(ev input.MouseEvent) (bool, error) {
	// A gesture that began on a label belongs to the strip until the
	// button comes up, wherever the pointer has gone since.
	if t.stripHeld {
		if ev.Kind == input.MouseRelease && ev.Button == t.stripButton {
			t.stripHeld = false
		}
		return true, nil
	}

	if strip := t.strip(); strip.Contains(ev.Col, ev.Row) {
		if ev.Kind != input.MousePress || ev.Button.IsWheel() {
			return false, nil
		}
		for i, label := range t.labels() {
			if label.Contains(ev.Col, ev.Row) {
				t.Focus(t.kids[i])
				t.stripHeld, t.stripButton = true, ev.Button
				return true, nil
			}
		}
		// The empty part of the strip belongs to nothing.
		return false, nil
	}

	body := t.body()
	if t.active == nil || !body.Contains(ev.Col, ev.Row) {
		return false, nil
	}
	ev.Col, ev.Row = body.Local(ev.Col, ev.Row)
	return HandleMouse(t.active, ev)
}

// CancelGesture lets go of a click on a label whose release will never
// arrive, because a dialog opened over it or the strip left the screen.
func (t *Tabs) CancelGesture() { t.stripHeld = false }

// strip returns the row of labels, which is empty when there is no room
// for both it and a tab under it.
func (t *Tabs) strip() Rect {
	if t.size.Cols <= 0 || t.size.Rows <= stripRows {
		return Rect{}
	}
	return Rect{Cols: t.size.Cols, Rows: stripRows}
}

// body returns where the tab being shown goes.
func (t *Tabs) body() Rect {
	if t.size.Empty() {
		return Rect{}
	}
	top := t.strip().Rows
	return Rect{Y: top, Cols: t.size.Cols, Rows: t.size.Rows - top}
}

// labels returns where each tab's label goes, in the strip's own
// coordinates. A label that does not fit is empty, and an empty label is
// drawn nowhere and cannot be clicked.
func (t *Tabs) labels() []Rect {
	out := make([]Rect, len(t.kids))
	strip := t.strip()
	if strip.Empty() {
		return out
	}
	at := 0
	for i, kid := range t.kids {
		// Measured in columns, not runes: a CJK title takes two columns
		// a character, and a label sized by rune count would be drawn
		// with its end cut off.
		width := grid.StringWidth(t.labelOf(kid, i)) + 2 // a space each side
		if at+width > strip.Cols {
			// No room for this one or any after it.
			break
		}
		out[i] = Rect{X: at, Cols: width, Rows: stripRows}
		at += width
	}
	return out
}

// labelOf names one tab.
func (t *Tabs) labelOf(w Widget, i int) string {
	if t.Label != nil {
		return t.Label(w, i)
	}
	if titled, ok := w.(Titled); ok {
		if title := titled.Title(); title != "" {
			return title
		}
	}
	return strconv.Itoa(i + 1)
}

// drawStrip paints the labels.
func (t *Tabs) drawStrip(v grid.View) {
	if strip := t.strip(); !strip.Empty() {
		t.buf.draw(strip.In(v), t.paintStrip)
	}
}

// paintStrip draws the labels into a row of their own.
func (t *Tabs) paintStrip(row grid.View) {
	row.Fill(grid.Cell{Rune: ' ', FG: t.InactiveFG, BG: t.StripBG, Width: 1})

	for i, label := range t.labels() {
		if label.Empty() {
			continue
		}
		fg, bg := t.InactiveFG, t.StripBG
		if t.kids[i] == t.active {
			fg, bg = t.ActiveFG, t.ActiveBG
		}
		cell := label.In(row)
		cell.Fill(grid.Cell{Rune: ' ', FG: fg, BG: bg, Width: 1})
		cell.SetString(1, 0, t.labelOf(t.kids[i], i), fg, bg, 0)
	}
}
