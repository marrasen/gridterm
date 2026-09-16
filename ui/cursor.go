package ui

// Cursor is a shape the mouse pointer takes.
//
// The toolkit names the shape rather than setting it: nothing here
// knows what kind of window it is drawn in. Whoever runs the window
// asks the tree and tells the window.
type Cursor int

const (
	// CursorDefault is the ordinary pointer.
	CursorDefault Cursor = iota

	// CursorEWResize is the east-west arrow, over something dragged
	// sideways.
	CursorEWResize

	// CursorNSResize is the north-south arrow, over something dragged up
	// and down.
	CursorNSResize
)

// CursorSource is a widget that says which pointer belongs over a point
// of it.
//
// Col and Row are in the widget's own coordinates, the way a mouse
// event is. A widget that implements this answers for everything under
// that point as well, so a container has to ask its own children with
// CursorAt.
//
// Returning false leaves the answer to whoever asked, which means the
// ordinary pointer.
type CursorSource interface {
	Widget
	CursorAt(col, row int) (Cursor, bool)
}

// CursorAt is the pointer over a point of a widget, in that widget's own
// coordinates.
//
// A container with nothing to say is walked through, so a divider under
// a stack of plain containers is still found.
func CursorAt(w Widget, col, row int) (Cursor, bool) {
	switch v := w.(type) {
	case CursorSource:
		return v.CursorAt(col, row)
	case Container:
		for _, child := range v.Children() {
			area, ok := v.ChildArea(child)
			if !ok || !area.Contains(col, row) {
				continue
			}
			x, y := area.Local(col, row)
			return CursorAt(child, x, y)
		}
	}
	return CursorDefault, false
}
