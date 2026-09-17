package ui

import "testing"

// cursorName is what a shape is called, so a failure says which arrow
// was wanted rather than which number.
func cursorName(c Cursor) string {
	switch c {
	case CursorEWResize:
		return "the east-west arrow"
	case CursorNSResize:
		return "the north-south arrow"
	}
	return "the ordinary pointer"
}

// dockDividerAt is the column the dock's divider has to be on: the one
// just past the panel.
//
// Worked out from the children rather than read off the divider itself. A
// divider a cell out would sit on a column one of the children is drawn
// on, and a test that asked the divider where it was would agree with it
// and find nothing.
func dockDividerAt(t *testing.T, d *Dock, panel Widget) int {
	t.Helper()
	at, ok := d.ChildArea(panel)
	if !ok {
		t.Fatal("the dock gave the panel no room, so this proves nothing")
	}
	return at.X + at.Cols
}

// The column between the panel and the rest is dragged sideways, so the
// pointer over it says so. Everywhere else is the ordinary pointer.
func TestCursorOverTheDockDivider(t *testing.T) {
	d, panel, rest := newTestDock(t, 24, 100, 30)
	r := rootOver(d, 100, 30)
	divider := dockDividerAt(t, d, panel)

	if got := r.CursorAt(divider, 10); got != CursorEWResize {
		t.Errorf("over the divider the pointer is %s, want the east-west arrow", cursorName(got))
	}
	panelAt, _ := d.ChildArea(panel)
	if got := r.CursorAt(panelAt.X+panelAt.Cols-1, 10); got != CursorDefault {
		t.Errorf("over the panel's last column the pointer is %s, want the ordinary one",
			cursorName(got))
	}
	// The first cell of the rest, which is where a divider a column out
	// would be standing.
	restAt, ok := d.ChildArea(rest)
	if !ok {
		t.Fatal("the dock gave the rest no room")
	}
	if got := r.CursorAt(restAt.X, 10); got != CursorDefault {
		t.Errorf("over the rest's first column the pointer is %s, want the ordinary one",
			cursorName(got))
	}
}

// A dock with its panel hidden has no divider, so the column one would be
// on answers the ordinary pointer.
func TestCursorOverAHiddenDockDividerIsTheOrdinaryPointer(t *testing.T) {
	d, panel, _ := newTestDock(t, 24, 100, 30)
	r := rootOver(d, 100, 30)
	divider := dockDividerAt(t, d, panel)
	if got := r.CursorAt(divider, 10); got != CursorEWResize {
		t.Fatalf("over the divider the pointer is %s, so this fixture is wrong", cursorName(got))
	}

	d.Collapsed = true
	r.Layout(Rect{Cols: 100, Rows: 30})

	if got := r.CursorAt(divider, 10); got != CursorDefault {
		t.Errorf("with the panel hidden the pointer at the divider's column is %s,"+
			" want the ordinary one", cursorName(got))
	}
}

// A split into columns has a divider dragged sideways; one into rows has
// a divider dragged up and down.
func TestCursorOverASplitDividerFollowsTheWayItDivides(t *testing.T) {
	for _, c := range []struct {
		name string
		dir  Dir
		want Cursor
	}{
		{"columns", Columns, CursorEWResize},
		{"rows", Rows, CursorNSResize},
	} {
		t.Run(c.name, func(t *testing.T) {
			a, b := &filler{ch: 'a'}, &filler{ch: 'b'}
			s := NewSplit(c.dir, a, b)
			r := rootOver(s, 20, 20)
			col, row := splitDividerAt(t, s, a)

			if got := r.CursorAt(col, row); got != c.want {
				t.Errorf("over the divider the pointer is %s, want %s",
					cursorName(got), cursorName(c.want))
			}
			// The last cell of the first pane and the first cell of the
			// second one: the two cells a divider a step out would be
			// standing on.
			ra, ok := s.ChildArea(a)
			if !ok {
				t.Fatal("the split gave the first pane no room")
			}
			if got := r.CursorAt(ra.X+ra.Cols-1, ra.Y+ra.Rows-1); got != CursorDefault {
				t.Errorf("over the first pane the pointer is %s, want the ordinary one",
					cursorName(got))
			}
			rb, ok := s.ChildArea(b)
			if !ok {
				t.Fatal("the split gave the second pane no room")
			}
			if got := r.CursorAt(rb.X, rb.Y); got != CursorDefault {
				t.Errorf("over the second pane the pointer is %s, want the ordinary one",
					cursorName(got))
			}
		})
	}
}

// splitDividerAt is the cell the split's divider has to be on: the one
// just past the first child, along the axis the split divides.
//
// Worked out from that child rather than read off the divider itself, for
// the reason dockDividerAt gives.
func splitDividerAt(t *testing.T, s *Split, first Widget) (col, row int) {
	t.Helper()
	at, ok := s.ChildArea(first)
	if !ok {
		t.Fatal("the split gave the first child no room, so this proves nothing")
	}
	if s.dir == Rows {
		return at.X, at.Y + at.Rows
	}
	return at.X + at.Cols, at.Y
}

// A drag belongs to the divider that started it, so the pointer keeps
// the resize arrow however far it wanders, and gives it up when the
// button comes up.
func TestCursorKeepsTheArrowWhileTheDockDividerIsDragged(t *testing.T) {
	d, panel, _ := newTestDock(t, 24, 100, 30)
	r := rootOver(d, 100, 30)
	dividerX := dockDividerAt(t, d, panel)

	if _, err := r.HandleMouse(pressAt(dividerX, 10)); err != nil {
		t.Fatalf("press on the divider: %v", err)
	}
	// Past the width the dock will go to, so the pointer ends up off the
	// divider it is dragging.
	if _, err := r.HandleMouse(moveTo(95, 10)); err != nil {
		t.Fatalf("drag: %v", err)
	}
	if _, _, moved := d.rects(); moved.Contains(95, 10) {
		t.Fatal("the pointer is still on the divider, so this proves nothing")
	}
	if got := r.CursorAt(95, 10); got != CursorEWResize {
		t.Errorf("mid-drag the pointer is %s, want the east-west arrow", cursorName(got))
	}

	if _, err := r.HandleMouse(releaseAt(95, 10)); err != nil {
		t.Fatalf("release: %v", err)
	}
	if got := r.CursorAt(95, 10); got != CursorDefault {
		t.Errorf("after the release the pointer is %s, want the ordinary one", cursorName(got))
	}
}

// The same for a split, which also has to keep the arrow that belongs to
// the way it divides.
func TestCursorKeepsTheArrowWhileASplitDividerIsDragged(t *testing.T) {
	a := &filler{ch: 'a'}
	s := NewSplit(Rows, a, &filler{ch: 'b'})
	r := rootOver(s, 10, 10)
	_, dividerY := splitDividerAt(t, s, a)

	if _, err := r.HandleMouse(pressAt(3, dividerY)); err != nil {
		t.Fatalf("press on the divider: %v", err)
	}
	if _, err := r.HandleMouse(moveTo(3, 9)); err != nil {
		t.Fatalf("drag: %v", err)
	}
	if _, _, moved := s.rects(); moved.Contains(3, 9) {
		t.Fatal("the pointer is still on the divider, so this proves nothing")
	}
	if got := r.CursorAt(3, 9); got != CursorNSResize {
		t.Errorf("mid-drag the pointer is %s, want the north-south arrow", cursorName(got))
	}

	if _, err := r.HandleMouse(releaseAt(3, 9)); err != nil {
		t.Fatalf("release: %v", err)
	}
	if got := r.CursorAt(3, 9); got != CursorDefault {
		t.Errorf("after the release the pointer is %s, want the ordinary one", cursorName(got))
	}
}

// A dialog is over the tree, so nothing in the tree can be dragged and
// the pointer says nothing about it.
func TestCursorUnderADialogIsTheOrdinaryPointer(t *testing.T) {
	d, panel, _ := newTestDock(t, 24, 100, 30)
	r := rootOver(d, 100, 30)
	dividerX := dockDividerAt(t, d, panel)
	if got := r.CursorAt(dividerX, 10); got != CursorEWResize {
		t.Fatalf("over the divider the pointer is %s, so this fixture is wrong", cursorName(got))
	}

	r.PushModal(&fake{name: "dialog"})
	if got := r.CursorAt(dividerX, 10); got != CursorDefault {
		t.Errorf("under a dialog the pointer is %s, want the ordinary one", cursorName(got))
	}

	r.PopModal()
	if got := r.CursorAt(dividerX, 10); got != CursorEWResize {
		t.Errorf("once the dialog has gone the pointer is %s, want the east-west arrow",
			cursorName(got))
	}
}

// The shape of the real window: a menu bar over a dock, with the panes
// on a tab strip beside the sidebar. Neither the bar nor the strip has
// anything to say about the pointer, so a divider is only found if they
// are walked through.
func TestCursorFindsADividerUnderPlainContainers(t *testing.T) {
	a := &filler{ch: 'a'}
	s := NewSplit(Columns, a, &filler{ch: 'b'})
	panel := &fake{name: "panel"}
	d := NewDock(20, panel, NewDeck(s))
	r := rootOver(NewMenubar(nil, nil, d), 100, 30)

	dockAt, ok := r.AreaOf(d)
	if !ok {
		t.Fatal("the dock is not in the tree")
	}
	dockDiv := dockDividerAt(t, d, panel)
	if got := r.CursorAt(dockAt.X+dockDiv, dockAt.Y); got != CursorEWResize {
		t.Errorf("over the dock divider the pointer is %s, want the east-west arrow",
			cursorName(got))
	}

	splitAt, ok := r.AreaOf(s)
	if !ok {
		t.Fatal("the split is not in the tree")
	}
	divCol, divRow := splitDividerAt(t, s, a)
	if got := r.CursorAt(splitAt.X+divCol, splitAt.Y+divRow); got != CursorEWResize {
		t.Errorf("over the split divider the pointer is %s, want the east-west arrow",
			cursorName(got))
	}
	if got := r.CursorAt(splitAt.X, splitAt.Y); got != CursorDefault {
		t.Errorf("over a pane the pointer is %s, want the ordinary one", cursorName(got))
	}
}

// A drag on a divider inside a dock keeps the arrow too.
//
// The split is not at the window's left edge, so the pointer mid-drag is
// only answered right if the point is put into the split's own
// coordinates first.
func TestCursorKeepsTheArrowWhileASplitUnderADockIsDragged(t *testing.T) {
	a := &filler{ch: 'a'}
	s := NewSplit(Rows, a, &filler{ch: 'b'})
	d := NewDock(20, &fake{name: "panel"}, s)
	r := rootOver(d, 100, 30)

	at, ok := r.AreaOf(s)
	if !ok {
		t.Fatal("the split is not in the tree")
	}
	if at.X == 0 {
		t.Fatal("the split starts at the window's left edge, so this proves nothing")
	}
	divCol, divRow := splitDividerAt(t, s, a)
	col, row := at.X+divCol+2, at.Y+divRow
	if got := r.CursorAt(col, row); got != CursorNSResize {
		t.Fatalf("over the divider the pointer is %s, so this fixture is wrong", cursorName(got))
	}

	if _, err := r.HandleMouse(pressAt(col, row)); err != nil {
		t.Fatalf("press on the divider: %v", err)
	}
	// Down to the bottom of the split, which the divider cannot follow all
	// the way: it stops short so the second pane keeps a row.
	bottom := at.Y + at.Rows - 1
	if _, err := r.HandleMouse(moveTo(col, bottom)); err != nil {
		t.Fatalf("drag: %v", err)
	}
	if _, _, moved := s.rects(); moved.Contains(col-at.X, bottom-at.Y) {
		t.Fatal("the pointer is still on the divider, so this proves nothing")
	}
	if got := r.CursorAt(col, bottom); got != CursorNSResize {
		t.Errorf("mid-drag the pointer is %s, want the north-south arrow", cursorName(got))
	}

	if _, err := r.HandleMouse(releaseAt(col, bottom)); err != nil {
		t.Fatalf("release: %v", err)
	}
	if got := r.CursorAt(col, bottom); got != CursorDefault {
		t.Errorf("after the release the pointer is %s, want the ordinary one", cursorName(got))
	}
}
