package ui

import (
	"image/color"

	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/input"
)

// Dir is how a split divides its area.
type Dir uint8

const (
	// Columns puts the children side by side, divided by a column.
	Columns Dir = iota
	// Rows stacks the children, divided by a row.
	Rows
)

// dividerRune is what separates the two halves. The atlas draws box
// characters to the exact cell size, so a run of them joins up.
func (d Dir) dividerRune() rune {
	if d == Rows {
		return '─'
	}
	return '│'
}

// cursor is the arrow for a divider that divides this way: sideways for
// a split into columns, up and down for one into rows.
func (d Dir) cursor() Cursor {
	if d == Rows {
		return CursorNSResize
	}
	return CursorEWResize
}

// Split shows two widgets side by side or one above the other, with a
// divider between them.
//
// A child is itself a widget, so a Split whose child is another Split is
// how a layout of any shape is built.
//
// The divider can be dragged. A press on it is taken, so Root keeps the
// pointer until the button comes up, however far it has wandered. A
// divider with no colour is still the handle: the cell is there to grab
// whether or not a line is drawn in it.
type Split struct {
	// Weight is the first child's share of the room left after the
	// divider, from 0 to 1. Each child always keeps at least one cell.
	Weight float64

	// DividerFG and DividerBG colour the divider. A zero-alpha
	// foreground draws no line, leaving a blank gap.
	DividerFG color.RGBA
	DividerBG color.RGBA

	dir      Dir
	a, b     Widget
	focused  Widget
	size     Size
	hasFocus bool

	// dragging records that the divider is being moved, and dragButton
	// which button has to come up to end it.
	dragging   bool
	dragButton input.MouseButton
}

// NewSplit divides its area between two widgets, evenly to begin with.
// The first child takes focus.
func NewSplit(dir Dir, a, b Widget) *Split {
	s := &Split{Weight: 0.5, dir: dir, a: a, b: b, focused: a}
	return s
}

// Dir returns which way the split divides.
func (s *Split) Dir() Dir { return s.dir }

// Children returns the two halves, first then second.
func (s *Split) Children() []Widget { return []Widget{s.a, s.b} }

// Focused returns the child that receives events.
func (s *Split) Focused() Widget { return s.focused }

// Other returns the child that is not w, for finding what a pane leaves
// behind when it closes.
func (s *Split) Other(w Widget) Widget {
	if w == s.a {
		return s.b
	}
	if w == s.b {
		return s.a
	}
	return nil
}

// Focus moves focus to one of the children, reporting whether it is one.
func (s *Split) Focus(w Widget) bool {
	if w != s.a && w != s.b {
		return false
	}
	if w == s.focused {
		return true
	}
	// Only pass the change on while this split holds focus itself, or a
	// child is told it lost focus it never had.
	if s.hasFocus {
		SetFocus(s.focused, false)
	}
	s.focused = w
	if s.hasFocus {
		SetFocus(w, true)
	}
	return true
}

// Replace swaps one child for another, reporting whether old was there.
// Focus follows: replacing the focused child focuses its replacement,
// and the old child is told focus left.
func (s *Split) Replace(old, new Widget) bool {
	if new == nil || (old != s.a && old != s.b) {
		return false
	}
	// Both halves the same widget would give Remove two answers and
	// leave the split holding a ghost. Replacing a child with itself is
	// harmless and stays allowed.
	if new != old && (new == s.a || new == s.b) {
		return false
	}
	if old == s.a {
		s.a = new
	} else {
		s.b = new
	}
	if s.focused == old {
		if s.hasFocus {
			SetFocus(old, false)
		}
		s.focused = new
		if s.hasFocus {
			SetFocus(new, true)
		}
	}
	// A split that has never been laid out has no size to pass on, and
	// telling the new child it has none would resize it to nothing.
	if !s.size.Empty() {
		s.Layout(s.size)
	}
	return true
}

// Remove takes one child out. A split with one child has nothing to
// divide, so it reports the other child as what should stand in its
// place.
func (s *Split) Remove(w Widget) (Widget, bool) {
	other := s.Other(w)
	if other == nil {
		return nil, false
	}
	// Focus moves off properly rather than being cleared, or the split
	// still names the child it just gave up and tells it a second time
	// that focus left when the split itself is torn down.
	s.Focus(other)
	return other, true
}

// ChildArea returns where a child sits, or false when it is not a child
// or the split is too small to show it.
func (s *Split) ChildArea(w Widget) (Rect, bool) {
	ra, rb, _ := s.rects()
	switch {
	case w != nil && w == s.a && !ra.Empty():
		return ra, true
	case w != nil && w == s.b && !rb.Empty():
		return rb, true
	}
	return Rect{}, false
}

// Layout divides the area and passes each share on.
func (s *Split) Layout(size Size) {
	s.size = size
	ra, rb, _ := s.rects()
	layoutIfVisible(s.a, ra)
	layoutIfVisible(s.b, rb)
}

// layoutIfVisible passes a size on, unless there is no room at all.
//
// A pane squeezed out by a narrow window keeps the size it had. Telling
// a terminal it has no columns reflows its scrollback into one, and
// widening the window again does not bring it back.
func layoutIfVisible(w Widget, area Rect) {
	if w == nil || area.Empty() {
		return
	}
	w.Layout(area.Size())
}

// Draw paints both halves and the divider between them.
func (s *Split) Draw(v grid.View) {
	ra, rb, rd := s.rects()
	if s.a != nil {
		s.a.Draw(ra.In(v))
	}
	if s.b != nil {
		s.b.Draw(rb.In(v))
	}
	if rd.Empty() {
		return
	}
	line := rd.In(v)
	cell := grid.Cell{Rune: s.dir.dividerRune(), FG: s.DividerFG, BG: s.DividerBG, Width: 1}
	if s.DividerFG.A == 0 {
		cell.Rune = ' '
	}
	cols, rows := line.Size()
	for y := 0; y < rows; y++ {
		for x := 0; x < cols; x++ {
			line.Set(x, y, cell)
		}
	}
}

// SetFocus passes focus on to whichever child holds it.
func (s *Split) SetFocus(on bool) {
	if s.hasFocus == on {
		return
	}
	s.hasFocus = on
	SetFocus(s.focused, on)
}

// HandleKey offers the key to the focused child. A split has no keys of
// its own: moving between panes is a command, so it can be rebound and
// can appear in a menu.
func (s *Split) HandleKey(ev input.Event) (bool, error) {
	return HandleKey(s.focused, ev)
}

// HandleMouse sends the event to the pane under the pointer, in that
// pane's own coordinates, focuses a pane that was clicked, and moves the
// divider when that is what was grabbed.
//
// A drag that wanders out of the pane it began in is not this split's
// problem: the root holds the pointer for the pane that took the press
// and delivers to it directly.
//
// A press on the divider is a gesture the split keeps for itself. Taking
// it is what makes the drag work: Root holds the pointer for whoever
// took the press, so every move and the release come back here however
// far the pointer has gone.
func (s *Split) HandleMouse(ev input.MouseEvent) (bool, error) {
	ra, rb, rd := s.rects()

	if s.dragging {
		switch {
		case ev.Kind == input.MouseRelease:
			// Only the button that started the drag ends it. Another one
			// coming up proves nothing: the first may still be down.
			if ev.Button == s.dragButton {
				s.dragging = false
			}
		case !ev.Button.IsWheel():
			s.dragTo(ev.Col, ev.Row)
		}
		return true, nil
	}
	if ev.Kind == input.MousePress && !ev.Button.IsWheel() &&
		!rd.Empty() && rd.Contains(ev.Col, ev.Row) {
		s.dragging, s.dragButton = true, ev.Button
		return true, nil
	}

	var target Widget
	var area Rect
	switch {
	case s.a != nil && ra.Contains(ev.Col, ev.Row):
		target, area = s.a, ra
	case s.b != nil && rb.Contains(ev.Col, ev.Row):
		target, area = s.b, rb
	default:
		// The divider, or nothing.
		return false, nil
	}
	// Clicking a pane is how the mouse moves focus.
	if ev.Kind == input.MousePress && !ev.Button.IsWheel() {
		s.Focus(target)
	}

	ev.Col, ev.Row = area.Local(ev.Col, ev.Row)
	return HandleMouse(target, ev)
}

// CancelGesture gives up a drag whose release is not coming.
func (s *Split) CancelGesture() { s.dragging = false }

// CursorAt returns the resize arrow over the divider, and over
// everything else while the divider is being dragged.
func (s *Split) CursorAt(col, row int) (Cursor, bool) {
	if s.dragging {
		return s.dir.cursor(), true
	}
	ra, rb, rd := s.rects()
	switch {
	case !rd.Empty() && rd.Contains(col, row):
		return s.dir.cursor(), true
	case s.a != nil && ra.Contains(col, row):
		x, y := ra.Local(col, row)
		return CursorAt(s.a, x, y)
	case s.b != nil && rb.Contains(col, row):
		x, y := rb.Local(col, row)
		return CursorAt(s.b, x, y)
	}
	return CursorDefault, false
}

// dragTo puts the divider under a point, as a share of the room the two
// children divide.
//
// Nothing is clamped beyond the weight's own range: rects keeps a cell
// for each child, so a divider dragged past the end stops one cell in.
func (s *Split) dragTo(col, row int) {
	pos, room := col, s.size.Cols-1
	if s.dir == Rows {
		pos, room = row, s.size.Rows-1
	}
	if room <= 0 {
		return
	}
	s.Weight = min(max(float64(pos)/float64(room), 0), 1)
	s.Layout(s.size)
}

// rects returns where the two children and the divider go.
//
// The divider only exists when there is room for it and a cell each
// side. Below that the first child takes what there is, because a split
// too small to show is better than two panes of nothing.
func (s *Split) rects() (a, b, divider Rect) {
	total, across := s.size.Cols, s.size.Rows
	if s.dir == Rows {
		total, across = s.size.Rows, s.size.Cols
	}
	if total <= 0 || across <= 0 {
		return Rect{}, Rect{}, Rect{}
	}
	if total < 3 {
		return s.rect(0, total, across), Rect{}, Rect{}
	}

	room := total - 1
	first := int(float64(room)*s.Weight + 0.5)
	first = min(max(first, 1), room-1)
	return s.rect(0, first, across),
		s.rect(first+1, room-first, across),
		s.rect(first, 1, across)
}

// rect builds a rectangle from a position and length along the split's
// own axis, and the full width of the other one.
func (s *Split) rect(at, length, across int) Rect {
	if length <= 0 {
		return Rect{}
	}
	if s.dir == Rows {
		return Rect{Y: at, Cols: across, Rows: length}
	}
	return Rect{X: at, Cols: length, Rows: across}
}

// FocusedLeaf returns the deepest focused widget under w, which is the
// one keys actually reach.
func FocusedLeaf(w Widget) Widget {
	for {
		c, ok := w.(Container)
		if !ok {
			return w
		}
		// A container reporting no focused child, or one that is not
		// among its children, still has to yield a leaf that Leaves
		// knows about, or the two disagree and the pane cycle lands
		// nowhere.
		children := c.Children()
		if len(children) == 0 {
			return w
		}
		next := c.Focused()
		if !contains(children, next) {
			next = children[0]
		}
		if next == nil {
			return w
		}
		w = next
	}
}

// Leaves returns every widget under root that is not a container, in
// layout order.
func Leaves(root Widget) []Widget {
	var out []Widget
	var walk func(Widget)
	walk = func(w Widget) {
		if w == nil {
			return
		}
		c, ok := w.(Container)
		if !ok {
			out = append(out, w)
			return
		}
		children := c.Children()
		if len(children) == 0 {
			// An empty container is a leaf itself, so FocusedLeaf
			// landing on one still finds it here.
			out = append(out, w)
			return
		}
		for _, child := range children {
			walk(child)
		}
	}
	walk(root)
	return out
}

// contains reports whether w is one of the widgets.
func contains(widgets []Widget, w Widget) bool {
	if w == nil {
		return false
	}
	for _, have := range widgets {
		if have == w {
			return true
		}
	}
	return false
}

// AreaOf returns where target is drawn inside root, given the area root
// itself fills. It reports false when target is not shown.
func AreaOf(root Widget, area Rect, target Widget) (Rect, bool) {
	if root == nil {
		return Rect{}, false
	}
	if root == target {
		return area, true
	}
	c, ok := root.(Container)
	if !ok {
		return Rect{}, false
	}
	for _, child := range c.Children() {
		sub, ok := c.ChildArea(child)
		if !ok {
			continue
		}
		sub.X, sub.Y = sub.X+area.X, sub.Y+area.Y
		if got, ok := AreaOf(child, sub, target); ok {
			return got, true
		}
	}
	return Rect{}, false
}

// LeafAt returns the deepest widget at a point and the area it fills,
// both in the coordinates the given area is in.
//
// A point inside a container but in none of its children -- a split's
// divider, a tab strip's own row -- belongs to the container itself. It
// is that container's own chrome, and a press there is that container's
// to keep until the button comes up.
//
// Which means a container that answers true to a press on its own chrome
// is promising to handle the whole gesture: every move and the release
// come back to it, not to the child under the pointer. A split promises
// that for its divider, which follows the pointer until the button comes
// up. A container with nothing to do with a press on its chrome must
// answer false instead.
func LeafAt(root Widget, area Rect, x, y int) (Widget, Rect, bool) {
	if root == nil || !area.Contains(x, y) {
		return nil, Rect{}, false
	}
	c, ok := root.(Container)
	if !ok {
		return root, area, true
	}
	for _, child := range c.Children() {
		sub, ok := c.ChildArea(child)
		if !ok {
			continue
		}
		sub.X, sub.Y = sub.X+area.X, sub.Y+area.Y
		if w, at, ok := LeafAt(child, sub, x, y); ok {
			return w, at, true
		}
	}
	return root, area, true
}

// Detach takes a widget out of the tree under root and returns the new
// root, which changes when removing it collapses the containers above.
// It returns nil when nothing is left, and reports false when target was
// not in the tree.
//
// Removing a pane is not just removing a node: a split with one child
// left has nothing to divide, so it goes too, and so does whatever that
// leaves empty.
func Detach(root, target Widget) (Widget, bool) {
	if root == nil || root == target {
		return nil, root == target
	}
	parent := ParentOf(root, target)
	if parent == nil {
		return root, false
	}
	stands, ok := parent.Remove(target)
	if !ok {
		return root, false
	}
	// A container may only report itself, one of its children, or
	// nothing. Anything else would splice a stranger into the tree, or
	// put back the widget being removed.
	if stands == target ||
		(stands != nil && stands != Widget(parent) && !contains(parent.Children(), stands)) {
		return root, false
	}
	switch {
	case stands == Widget(parent):
		// The container carries on with the children it has left.
		return root, true
	case stands == nil:
		// Nothing left in it, so the container goes too.
		return Detach(root, parent)
	}
	if grandparent := ParentOf(root, parent); grandparent != nil {
		grandparent.Replace(parent, stands)
		return root, true
	}
	return stands, true
}

// ParentOf returns the container holding target, or nil when target is
// the root or is not in the tree.
func ParentOf(root, target Widget) Container {
	c, ok := root.(Container)
	if !ok {
		return nil
	}
	for _, child := range c.Children() {
		if child == target {
			return c
		}
		if found := ParentOf(child, target); found != nil {
			return found
		}
	}
	return nil
}
