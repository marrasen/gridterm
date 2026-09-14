package ui

import (
	"image/color"

	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/input"
)

// How wide a docked panel may be, and the least room the rest may keep.
const (
	dockMin  = 8
	dockRest = 12

	// dockDivider is the column between the two.
	dockDivider = 1
)

// Dock is a panel of a fixed width beside everything else.
//
// Split cannot do this: its Weight is a fraction of whatever room there
// is, so a panel would grow and shrink with the window instead of
// staying the width it was set to. A list of connections wants the width
// its longest line needs, whatever size the window is.
//
// The divider can be dragged. A press on it is taken, so Root keeps the
// pointer until the button comes up, however far it has wandered.
type Dock struct {
	// Width is how many columns the panel gets, not counting the
	// divider. It is clamped to what the window can spare.
	Width int

	// Collapsed hides the panel without forgetting it or what is in it.
	Collapsed bool

	// DividerFG and DividerBG colour the column between the two. A
	// foreground with no alpha leaves a blank gap instead of a line.
	DividerFG, DividerBG color.RGBA

	panel Widget
	rest  Widget

	// onPanel records that the panel has focus rather than the rest.
	onPanel  bool
	hasFocus bool

	size Size

	// dragging records that the divider is being moved, so the drag can
	// be finished or abandoned.
	dragging bool
}

// NewDock puts a panel beside the rest of the window.
func NewDock(width int, panel, rest Widget) *Dock {
	return &Dock{Width: width, panel: panel, rest: rest}
}

// Panel returns the widget in the panel.
func (d *Dock) Panel() Widget { return d.panel }

// Rest returns the widget beside it.
func (d *Dock) Rest() Widget { return d.rest }

// ShowPanel opens or closes the panel, moving focus off it when it goes.
func (d *Dock) ShowPanel(on bool) {
	if d.Collapsed == !on {
		return
	}
	// Moved before the panel is hidden, not after: Focused reports the
	// other half the moment Collapsed is set, so a focus move made then
	// would never tell the panel it had lost the keys.
	if !on && d.onPanel {
		d.Focus(d.rest)
	}
	d.Collapsed = !on
	if !d.size.Empty() {
		d.Layout(d.size)
	}
}

// Children returns the panel and the rest, in the order they are drawn.
func (d *Dock) Children() []Widget {
	var out []Widget
	if d.panel != nil {
		out = append(out, d.panel)
	}
	if d.rest != nil {
		out = append(out, d.rest)
	}
	return out
}

// Focused returns the one receiving keys.
func (d *Dock) Focused() Widget {
	if d.onPanel && d.panel != nil && !d.Collapsed {
		return d.panel
	}
	return d.rest
}

// Focus moves focus to the panel or to the rest, reporting whether w is
// one of them. A hidden panel cannot take it.
func (d *Dock) Focus(w Widget) bool {
	switch {
	case w == nil:
		return false
	case w == d.panel && !d.Collapsed:
		return d.focusPanel(true)
	case w == d.rest:
		return d.focusPanel(false)
	}
	return false
}

// focusPanel moves focus between the two halves.
func (d *Dock) focusPanel(on bool) bool {
	if d.onPanel == on {
		return true
	}
	if d.hasFocus {
		SetFocus(d.Focused(), false)
	}
	d.onPanel = on
	if d.hasFocus {
		SetFocus(d.Focused(), true)
	}
	return true
}

// Replace swaps one of the two for another, reporting whether old was
// there.
func (d *Dock) Replace(old, new Widget) bool {
	if old == nil || new == nil {
		return false
	}
	focused := d.hasFocus && d.Focused() == old
	switch old {
	case d.panel:
		d.panel = new
	case d.rest:
		d.rest = new
	default:
		return false
	}
	if focused {
		SetFocus(old, false)
		SetFocus(new, true)
	}
	if !d.size.Empty() {
		d.Layout(d.size)
	}
	return true
}

// Remove takes one of the two out.
//
// Losing the rest leaves a dock with nothing but a panel, which is not
// worth keeping: the panel stands in its place. Losing the panel leaves
// a dock that is only its other half, so that does too.
func (d *Dock) Remove(w Widget) (Widget, bool) {
	if w == nil {
		return nil, false
	}
	if d.hasFocus && d.Focused() == w {
		SetFocus(w, false)
	}
	switch w {
	case d.panel:
		d.panel = nil
		return d.rest, true
	case d.rest:
		d.rest = nil
		return d.panel, true
	}
	return nil, false
}

// ChildArea returns where one of the two is drawn.
func (d *Dock) ChildArea(w Widget) (Rect, bool) {
	panel, rest, _ := d.rects()
	switch {
	case w != nil && w == d.panel && !panel.Empty():
		return panel, true
	case w != nil && w == d.rest && !rest.Empty():
		return rest, true
	}
	return Rect{}, false
}

// Layout divides the room between the panel and the rest.
func (d *Dock) Layout(size Size) {
	d.size = size
	panel, rest, _ := d.rects()
	layoutIfVisible(d.panel, panel)
	layoutIfVisible(d.rest, rest)
}

// SetFocus passes focus on to whichever half has it.
func (d *Dock) SetFocus(on bool) {
	d.hasFocus = on
	SetFocus(d.Focused(), on)
}

// Draw paints both halves and the divider between them.
func (d *Dock) Draw(v grid.View) {
	panel, rest, divider := d.rects()
	if !panel.Empty() && d.panel != nil {
		d.panel.Draw(panel.In(v))
	}
	if !rest.Empty() && d.rest != nil {
		d.rest.Draw(rest.In(v))
	}
	if divider.Empty() {
		return
	}
	rune := '│'
	if d.DividerFG.A == 0 {
		rune = ' '
	}
	divider.In(v).Fill(grid.Cell{
		Rune: rune, FG: d.DividerFG, BG: d.DividerBG, Width: 1,
	})
}

// HandleKey passes a key to whichever half has focus.
func (d *Dock) HandleKey(ev input.Event) (bool, error) {
	return HandleKey(d.Focused(), ev)
}

// HandleMouse routes to whichever half was clicked, and drags the
// divider when that is what was grabbed.
//
// Taking the press on the divider is what makes the drag work: Root
// keeps the pointer for whoever took it, so every move and the release
// come back here however far the pointer has gone.
func (d *Dock) HandleMouse(ev input.MouseEvent) (bool, error) {
	panel, rest, divider := d.rects()

	if d.dragging {
		switch {
		case ev.Kind == input.MouseRelease:
			d.dragging = false
		case !ev.Button.IsWheel():
			d.dragTo(ev.Col)
		}
		return true, nil
	}
	if ev.Kind == input.MousePress && !ev.Button.IsWheel() &&
		!divider.Empty() && divider.Contains(ev.Col, ev.Row) {
		d.dragging = true
		return true, nil
	}

	switch {
	case !panel.Empty() && panel.Contains(ev.Col, ev.Row):
		local := ev
		local.Col, local.Row = panel.Local(ev.Col, ev.Row)
		return HandleMouse(d.panel, local)
	case !rest.Empty() && rest.Contains(ev.Col, ev.Row):
		local := ev
		local.Col, local.Row = rest.Local(ev.Col, ev.Row)
		return HandleMouse(d.rest, local)
	}
	return false, nil
}

// CancelGesture gives up a drag whose release is not coming.
func (d *Dock) CancelGesture() { d.dragging = false }

// dragTo moves the divider to a column, within what the window can
// spare.
func (d *Dock) dragTo(col int) {
	d.Width = min(max(col, dockMin), max(d.size.Cols-dockRest-dockDivider, dockMin))
	if !d.size.Empty() {
		d.Layout(d.size)
	}
}

// rects returns where the panel, the rest and the divider go.
//
// A window with no room for both gives it all to the rest: a panel with
// a terminal squeezed to nothing beside it is worse than no panel.
func (d *Dock) rects() (panel, rest, divider Rect) {
	if d.size.Empty() {
		return Rect{}, Rect{}, Rect{}
	}
	whole := Rect{Cols: d.size.Cols, Rows: d.size.Rows}
	if d.Collapsed || d.panel == nil {
		return Rect{}, whole, Rect{}
	}
	width := min(max(d.Width, dockMin), d.size.Cols)
	if d.size.Cols-width-dockDivider < dockRest {
		return Rect{}, whole, Rect{}
	}
	panel = Rect{Cols: width, Rows: d.size.Rows}
	divider = Rect{X: width, Cols: dockDivider, Rows: d.size.Rows}
	rest = Rect{
		X:    width + dockDivider,
		Cols: d.size.Cols - width - dockDivider,
		Rows: d.size.Rows,
	}
	return panel, rest, divider
}
