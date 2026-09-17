package main

import (
	"testing"

	"github.com/marrasen/gridterm/ui"
)

// pointerName is what a shape is called, so a failure says which arrow
// was wanted rather than which number.
func pointerName(c ui.Cursor) string {
	switch c {
	case ui.CursorEWResize:
		return "the east-west arrow"
	case ui.CursorNSResize:
		return "the north-south arrow"
	}
	return "the ordinary pointer"
}

// The column between the sidebar and the panes is what widens the
// sidebar, so the pointer over it is the sideways arrow. The sidebar
// itself and the pane beside it are the ordinary pointer.
func TestThePointerOverTheSidebarDivider(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)

	side, ok := sideArea(a)
	if !ok {
		t.Fatal("the window has no room for a sidebar, so this proves nothing")
	}
	divider := side.X + side.Cols

	px, py := pixelOfWindowCell(a, divider, side.Y+2)
	if got := a.pointerCursor(px, py); got != ui.CursorEWResize {
		t.Errorf("over the divider the pointer is %s, want the east-west arrow", pointerName(got))
	}

	px, py = pixelOfWindowCell(a, side.X+1, side.Y+2)
	if got := a.pointerCursor(px, py); got != ui.CursorDefault {
		t.Errorf("over the sidebar the pointer is %s, want the ordinary one", pointerName(got))
	}

	px, py = pixelOfWindowCell(a, divider+8, side.Y+2)
	if got := a.pointerCursor(px, py); got != ui.CursorDefault {
		t.Errorf("over a pane the pointer is %s, want the ordinary one", pointerName(got))
	}

	// Off the window, which the pointer is while it is over another one.
	if got := a.pointerCursor(-20, -20); got != ui.CursorDefault {
		t.Errorf("outside the window the pointer is %s, want the ordinary one", pointerName(got))
	}
}

// A split into columns has a divider dragged sideways, and one into rows
// a divider dragged up and down. Both sit under the tab strip, which has
// nothing to say about the pointer.
func TestThePointerOverASplitDivider(t *testing.T) {
	for _, c := range []struct {
		name string
		dir  ui.Dir
		want ui.Cursor
	}{
		{"columns", ui.Columns, ui.CursorEWResize},
		{"rows", ui.Rows, ui.CursorNSResize},
	} {
		t.Run(c.name, func(t *testing.T) {
			a := newTestApp(t, 160, 48)
			withDialogs(t, a)
			withPanel(t, a)
			if err := a.openPane(); err != nil {
				t.Fatalf("openPane: %v", err)
			}
			next := a.focusedTerminal()
			current := otherTerminal(t, a, next)
			// The tab the split goes in has to be the one showing, or
			// the pane it divides is nowhere on screen.
			a.focus(current)
			if err := a.splitWith(c.dir, current, next); err != nil {
				t.Fatalf("splitWith: %v", err)
			}
			a.placeRegions()

			first, shown := a.paneArea(current)
			if !shown {
				t.Fatal("the first pane is not in the tree")
			}
			// The cell just past the first pane, along the axis the
			// divider lies across.
			col, row := first.X+first.Cols, first.Y
			if c.dir == ui.Rows {
				col, row = first.X, first.Y+first.Rows
			}

			px, py := pixelOfWindowCell(a, col, row)
			if got := a.pointerCursor(px, py); got != c.want {
				t.Errorf("over the divider the pointer is %s, want %s",
					pointerName(got), pointerName(c.want))
			}

			px, py = pixelOfWindowCell(a, first.X+1, first.Y+1)
			if got := a.pointerCursor(px, py); got != ui.CursorDefault {
				t.Errorf("over a pane the pointer is %s, want the ordinary one", pointerName(got))
			}
		})
	}
}
