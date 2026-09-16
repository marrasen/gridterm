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

// The column between the panel and the rest is dragged sideways, so the
// pointer over it says so. Everywhere else is the ordinary pointer.
func TestCursorOverTheDockDivider(t *testing.T) {
	d, _, _ := newTestDock(t, 24, 100, 30)
	r := rootOver(d, 100, 30)
	_, _, divider := d.rects()

	if got := r.CursorAt(divider.X, 10); got != CursorEWResize {
		t.Errorf("over the divider the pointer is %s, want the east-west arrow", cursorName(got))
	}
	if got := r.CursorAt(divider.X-2, 10); got != CursorDefault {
		t.Errorf("over the panel the pointer is %s, want the ordinary one", cursorName(got))
	}
	if got := r.CursorAt(divider.X+8, 10); got != CursorDefault {
		t.Errorf("over the rest the pointer is %s, want the ordinary one", cursorName(got))
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
			_, _, divider := s.rects()

			if got := r.CursorAt(divider.X, divider.Y); got != c.want {
				t.Errorf("over the divider the pointer is %s, want %s",
					cursorName(got), cursorName(c.want))
			}
			// One cell into the first pane, along the axis the divider
			// lies across.
			at, _, _ := s.rects()
			if got := r.CursorAt(at.X+at.Cols-1, at.Y+at.Rows-1); got != CursorDefault {
				t.Errorf("over a pane the pointer is %s, want the ordinary one", cursorName(got))
			}
		})
	}
}

// A drag belongs to the divider that started it, so the pointer keeps
// the resize arrow however far it wanders, and gives it up when the
// button comes up.
func TestCursorKeepsTheArrowWhileTheDockDividerIsDragged(t *testing.T) {
	d, _, _ := newTestDock(t, 24, 100, 30)
	r := rootOver(d, 100, 30)
	_, _, divider := d.rects()

	if _, err := r.HandleMouse(pressAt(divider.X, 10)); err != nil {
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
	s := NewSplit(Rows, &filler{ch: 'a'}, &filler{ch: 'b'})
	r := rootOver(s, 10, 10)
	_, _, divider := s.rects()

	if _, err := r.HandleMouse(pressAt(3, divider.Y)); err != nil {
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
	d, _, _ := newTestDock(t, 24, 100, 30)
	r := rootOver(d, 100, 30)
	_, _, divider := d.rects()
	if got := r.CursorAt(divider.X, 10); got != CursorEWResize {
		t.Fatalf("over the divider the pointer is %s, so this fixture is wrong", cursorName(got))
	}

	r.PushModal(&fake{name: "dialog"})
	if got := r.CursorAt(divider.X, 10); got != CursorDefault {
		t.Errorf("under a dialog the pointer is %s, want the ordinary one", cursorName(got))
	}

	r.PopModal()
	if got := r.CursorAt(divider.X, 10); got != CursorEWResize {
		t.Errorf("once the dialog has gone the pointer is %s, want the east-west arrow",
			cursorName(got))
	}
}

// The shape of the real window: a menu bar over a dock, with the panes
// on a tab strip beside the sidebar. Neither the bar nor the strip has
// anything to say about the pointer, so a divider is only found if they
// are walked through.
func TestCursorFindsADividerUnderPlainContainers(t *testing.T) {
	s := NewSplit(Columns, &filler{ch: 'a'}, &filler{ch: 'b'})
	d := NewDock(20, &fake{name: "panel"}, NewTabs(s))
	r := rootOver(NewMenubar(nil, nil, d), 100, 30)

	dockAt, ok := r.AreaOf(d)
	if !ok {
		t.Fatal("the dock is not in the tree")
	}
	_, _, dockDiv := d.rects()
	if got := r.CursorAt(dockAt.X+dockDiv.X, dockAt.Y+dockDiv.Y); got != CursorEWResize {
		t.Errorf("over the dock divider the pointer is %s, want the east-west arrow",
			cursorName(got))
	}

	splitAt, ok := r.AreaOf(s)
	if !ok {
		t.Fatal("the split is not in the tree")
	}
	_, _, splitDiv := s.rects()
	if got := r.CursorAt(splitAt.X+splitDiv.X, splitAt.Y+splitDiv.Y); got != CursorEWResize {
		t.Errorf("over the split divider the pointer is %s, want the east-west arrow",
			cursorName(got))
	}
	if got := r.CursorAt(splitAt.X, splitAt.Y); got != CursorDefault {
		t.Errorf("over a pane the pointer is %s, want the ordinary one", cursorName(got))
	}
}
