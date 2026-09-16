package ui

import (
	"image/color"
	"strings"
	"testing"

	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/input"
)

// filler draws its name over every cell it is given, so a test can see
// which pane owns which part of the grid.
type filler struct {
	ch      rune
	size    Size
	focused bool
	// focusLog keeps every SetFocus value, for checking the contract a
	// container has to keep.
	focusLog []bool
	seen     []input.MouseEvent
	keys     []input.Key
	takes    input.Key
	// drawn records that Draw was called, for telling a widget that was
	// only sized from one that is on screen.
	drawn bool
}

func (f *filler) Layout(size Size) { f.size = size }
func (f *filler) Draw(v grid.View) {
	f.drawn = true
	v.Fill(grid.Cell{Rune: f.ch, FG: fg, BG: bg, Width: 1})
}
func (f *filler) SetFocus(on bool) {
	f.focused = on
	f.focusLog = append(f.focusLog, on)
}
func (f *filler) HandleKey(ev input.Event) (bool, error) {
	f.keys = append(f.keys, ev.Key)
	return ev.Key == f.takes, nil
}
func (f *filler) HandleMouse(ev input.MouseEvent) (bool, error) {
	f.seen = append(f.seen, ev)
	return true, nil
}

func sizeOf(v grid.View) Size {
	cols, rows := v.Size()
	return Size{Cols: cols, Rows: rows}
}

// rowOf reads a row back as a string. The second half of a double-width
// character is skipped: it carries no rune of its own.
func rowOf(g *grid.Grid, y int) string {
	cols, _ := g.Size()
	var b strings.Builder
	for x := 0; x < cols; x++ {
		c := g.At(x, y)
		if c.Width == 0 {
			continue
		}
		b.WriteRune(c.Rune)
	}
	return b.String()
}

func drawSplit(s *Split, cols, rows int) *grid.Grid {
	g := grid.New(cols, rows, fg, bg)
	s.Layout(Size{Cols: cols, Rows: rows})
	s.Draw(g.View())
	return g
}

func TestSplitColumnsDividesTheArea(t *testing.T) {
	a, b := &filler{ch: 'a'}, &filler{ch: 'b'}
	s := NewSplit(Columns, a, b)
	s.DividerFG = fg

	g := drawSplit(s, 11, 2)

	if got := rowOf(g, 0); got != "aaaaa│bbbbb" {
		t.Errorf("row 0 = %q, want %q", got, "aaaaa│bbbbb")
	}
	if a.size != (Size{Cols: 5, Rows: 2}) || b.size != (Size{Cols: 5, Rows: 2}) {
		t.Errorf("sizes = %+v and %+v, want 5x2 each", a.size, b.size)
	}
}

func TestSplitRowsDividesTheArea(t *testing.T) {
	a, b := &filler{ch: 'a'}, &filler{ch: 'b'}
	s := NewSplit(Rows, a, b)
	s.DividerFG = fg

	g := drawSplit(s, 4, 5)

	for _, tc := range []struct {
		row  int
		want string
	}{{0, "aaaa"}, {1, "aaaa"}, {2, "────"}, {3, "bbbb"}, {4, "bbbb"}} {
		if got := rowOf(g, tc.row); got != tc.want {
			t.Errorf("row %d = %q, want %q", tc.row, got, tc.want)
		}
	}
}

// TestSplitWeight checks that the share moves where it is told and that
// neither pane is ever squeezed out entirely.
func TestSplitWeight(t *testing.T) {
	for _, tc := range []struct {
		weight float64
		wantA  int
		wantB  int
	}{
		{weight: 0.5, wantA: 5, wantB: 5},
		{weight: 0.25, wantA: 3, wantB: 7},
		{weight: 0.8, wantA: 8, wantB: 2},
		// Neither end may take everything: a pane with no cells cannot
		// be clicked back into existence.
		{weight: 0, wantA: 1, wantB: 9},
		{weight: 1, wantA: 9, wantB: 1},
		{weight: -5, wantA: 1, wantB: 9},
		{weight: 5, wantA: 9, wantB: 1},
	} {
		a, b := &filler{ch: 'a'}, &filler{ch: 'b'}
		s := NewSplit(Columns, a, b)
		s.Weight = tc.weight

		s.Layout(Size{Cols: 11, Rows: 1})

		if a.size.Cols != tc.wantA || b.size.Cols != tc.wantB {
			t.Errorf("weight %v: %d and %d columns, want %d and %d",
				tc.weight, a.size.Cols, b.size.Cols, tc.wantA, tc.wantB)
		}
	}
}

// TestSplitTooSmallForTwo checks the sizes a layout passes through while
// it settles. A split with no room for a divider and a cell each side
// gives everything to the first pane rather than showing two of nothing.
func TestSplitTooSmallForTwo(t *testing.T) {
	for _, tc := range []struct {
		cols  int
		wantA int
		wantB int
	}{
		{cols: 0, wantA: 0, wantB: 0},
		{cols: 1, wantA: 1, wantB: 0},
		{cols: 2, wantA: 2, wantB: 0},
		{cols: 3, wantA: 1, wantB: 1},
	} {
		a, b := &filler{ch: 'a'}, &filler{ch: 'b'}
		s := NewSplit(Columns, a, b)

		s.Layout(Size{Cols: tc.cols, Rows: 2})

		if a.size.Cols != tc.wantA || b.size.Cols != tc.wantB {
			t.Errorf("%d columns: got %d and %d, want %d and %d",
				tc.cols, a.size.Cols, b.size.Cols, tc.wantA, tc.wantB)
		}
	}
}

// TestSplitNoRowsGivesNothing checks the other axis of a degenerate
// area.
func TestSplitNoRowsGivesNothing(t *testing.T) {
	a, b := &filler{ch: 'a'}, &filler{ch: 'b'}
	s := NewSplit(Columns, a, b)

	s.Layout(Size{Cols: 20, Rows: 0})

	if !a.size.Empty() || !b.size.Empty() {
		t.Errorf("sizes = %+v and %+v, want both empty", a.size, b.size)
	}
}

// TestSplitDividerWithNoColourIsBlank checks that a split can show a
// plain gap rather than a line.
func TestSplitDividerWithNoColourIsBlank(t *testing.T) {
	s := NewSplit(Columns, &filler{ch: 'a'}, &filler{ch: 'b'})

	g := drawSplit(s, 11, 1)

	if got := rowOf(g, 0); got != "aaaaa bbbbb" {
		t.Errorf("row 0 = %q, want a blank divider", got)
	}
}

func TestSplitKeysGoToTheFocusedPane(t *testing.T) {
	a, b := &filler{ch: 'a', takes: input.KeyQ}, &filler{ch: 'b', takes: input.KeyQ}
	s := NewSplit(Columns, a, b)
	s.SetFocus(true)

	handled, err := s.HandleKey(press(input.KeyQ, 0))

	if err != nil {
		t.Fatalf("HandleKey: %v", err)
	}
	if !handled {
		t.Error("the focused pane did not get the key")
	}
	if len(b.keys) != 0 {
		t.Errorf("the unfocused pane saw %v", b.keys)
	}

	s.Focus(b)
	s.HandleKey(press(input.KeyQ, 0))
	if len(b.keys) != 1 {
		t.Error("the newly focused pane did not get the key")
	}
}

func TestSplitFocusMovesBetweenPanes(t *testing.T) {
	a, b := &filler{ch: 'a'}, &filler{ch: 'b'}
	s := NewSplit(Columns, a, b)
	s.SetFocus(true)

	if !a.focused || b.focused {
		t.Fatalf("focus = %v and %v, want the first pane only", a.focused, b.focused)
	}
	if !s.Focus(b) {
		t.Fatal("Focus refused a child")
	}
	if a.focused || !b.focused {
		t.Errorf("focus = %v and %v, want the second pane only", a.focused, b.focused)
	}
	if s.Focus(&filler{}) {
		t.Error("Focus accepted a widget that is not a child")
	}
}

// TestSplitWithoutFocusDoesNotFocusItsChildren checks the rule a
// container has to keep: a child must not be told it gained focus the
// split itself does not have.
func TestSplitWithoutFocusDoesNotFocusItsChildren(t *testing.T) {
	a, b := &filler{ch: 'a'}, &filler{ch: 'b'}
	s := NewSplit(Columns, a, b)

	s.Focus(b)

	if a.focused || b.focused {
		t.Errorf("focus = %v and %v, want neither: the split has no focus itself",
			a.focused, b.focused)
	}

	s.SetFocus(true)
	if !b.focused {
		t.Error("focus arriving did not reach the child that was selected")
	}
}

func TestSplitClickFocusesThePaneUnderThePointer(t *testing.T) {
	a, b := &filler{ch: 'a'}, &filler{ch: 'b'}
	s := NewSplit(Columns, a, b)
	s.SetFocus(true)
	s.Layout(Size{Cols: 11, Rows: 2})

	s.HandleMouse(input.MouseEvent{Kind: input.MousePress, Button: input.MouseLeft, Col: 8, Row: 1})

	if !b.focused {
		t.Error("clicking a pane did not focus it")
	}
	if len(b.seen) != 1 {
		t.Fatalf("the pane saw %d events, want 1", len(b.seen))
	}
	// The pane is told where the click landed in its own coordinates.
	if got := b.seen[0]; got.Col != 2 || got.Row != 1 {
		t.Errorf("click at %d,%d, want 2,1", got.Col, got.Row)
	}
}

// TestSplitClickOnTheDividerReachesNoPane checks that the divider is not
// mistaken for either pane. The split keeps the press for itself.
func TestSplitClickOnTheDividerReachesNoPane(t *testing.T) {
	a, b := &filler{ch: 'a'}, &filler{ch: 'b'}
	s := NewSplit(Columns, a, b)
	s.SetFocus(true)
	s.Layout(Size{Cols: 11, Rows: 2})

	handled, err := s.HandleMouse(input.MouseEvent{
		Kind: input.MousePress, Button: input.MouseLeft, Col: 5, Row: 0,
	})

	if err != nil {
		t.Fatalf("HandleMouse: %v", err)
	}
	if !handled {
		t.Error("the press on the divider was not taken, so no drag can follow")
	}
	if len(a.seen) != 0 || len(b.seen) != 0 {
		t.Error("a click on the divider was routed to a pane")
	}
}

func TestSplitReplace(t *testing.T) {
	a, b := &filler{ch: 'a'}, &filler{ch: 'b'}
	s := NewSplit(Columns, a, b)
	s.SetFocus(true)
	s.Layout(Size{Cols: 11, Rows: 2})

	next := &filler{ch: 'c'}
	if !s.Replace(a, next) {
		t.Fatal("Replace refused a child that was there")
	}
	if s.Children()[0] != next {
		t.Error("the child was not replaced")
	}
	if s.Focused() != next {
		t.Error("focus did not follow the replacement")
	}
	if !next.focused {
		t.Error("the replacement was not told it has focus")
	}
	if next.size.Cols != 5 {
		t.Errorf("the replacement was given %d columns, want 5: it was never laid out",
			next.size.Cols)
	}
	if s.Replace(&filler{}, &filler{}) {
		t.Error("Replace accepted a widget that is not a child")
	}
	if s.Replace(next, nil) {
		t.Error("Replace accepted nil")
	}
}

func TestSplitOther(t *testing.T) {
	a, b := &filler{ch: 'a'}, &filler{ch: 'b'}
	s := NewSplit(Columns, a, b)

	if s.Other(a) != b || s.Other(b) != a {
		t.Error("Other did not return the opposite child")
	}
	if s.Other(&filler{}) != nil {
		t.Error("Other returned something for a widget that is not a child")
	}
}

func TestFocusedLeafWalksDown(t *testing.T) {
	deep, other := &filler{ch: 'd'}, &filler{ch: 'o'}
	inner := NewSplit(Rows, other, deep)
	outer := NewSplit(Columns, &filler{ch: 'x'}, inner)
	outer.SetFocus(true)

	if got := FocusedLeaf(outer); got != outer.Children()[0] {
		t.Errorf("leaf = %v, want the first pane", got)
	}

	outer.Focus(inner)
	inner.Focus(deep)

	if got := FocusedLeaf(outer); got != deep {
		t.Errorf("leaf = %v, want the deepest focused pane", got)
	}
	// A widget that is not a container is its own leaf.
	if got := FocusedLeaf(deep); got != deep {
		t.Errorf("leaf of a plain widget = %v, want itself", got)
	}
}

func TestLeavesInLayoutOrder(t *testing.T) {
	one, two, three := &filler{ch: '1'}, &filler{ch: '2'}, &filler{ch: '3'}
	tree := NewSplit(Columns, one, NewSplit(Rows, two, three))

	got := Leaves(tree)

	want := []Widget{one, two, three}
	if len(got) != len(want) {
		t.Fatalf("got %d leaves, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("leaves out of order: %v", got)
		}
	}
	if got := Leaves(one); len(got) != 1 || got[0] != one {
		t.Errorf("leaves of a plain widget = %v, want just itself", got)
	}
}

func TestParentOf(t *testing.T) {
	deep, other := &filler{ch: 'd'}, &filler{ch: 'o'}
	inner := NewSplit(Rows, other, deep)
	top := &filler{ch: 't'}
	outer := NewSplit(Columns, top, inner)

	if got := ParentOf(outer, deep); got != Container(inner) {
		t.Errorf("parent of the deep pane = %v, want the inner split", got)
	}
	if got := ParentOf(outer, top); got != Container(outer) {
		t.Errorf("parent of the top pane = %v, want the outer split", got)
	}
	if got := ParentOf(outer, inner); got != Container(outer) {
		t.Errorf("parent of the inner split = %v, want the outer split", got)
	}
	if got := ParentOf(outer, outer); got != nil {
		t.Errorf("parent of the root = %v, want nil", got)
	}
	if got := ParentOf(outer, &filler{}); got != nil {
		t.Errorf("parent of a stranger = %v, want nil", got)
	}
	if got := ParentOf(top, deep); got != nil {
		t.Errorf("parent searched from a plain widget = %v, want nil", got)
	}
}

// TestSplitOfSplitsDrawsEveryPane checks a tree of any shape reaches the
// grid in the right places.
func TestSplitOfSplitsDrawsEveryPane(t *testing.T) {
	left := &filler{ch: 'l'}
	topRight, bottomRight := &filler{ch: 't'}, &filler{ch: 'b'}
	// Each split carries its own divider colour, so a nested one has to
	// be given it too.
	inner := NewSplit(Rows, topRight, bottomRight)
	inner.DividerFG = fg
	s := NewSplit(Columns, left, inner)
	s.DividerFG = fg

	g := drawSplit(s, 9, 5)

	for _, tc := range []struct {
		row  int
		want string
	}{
		{0, "llll│tttt"},
		{1, "llll│tttt"},
		{2, "llll│────"},
		{3, "llll│bbbb"},
		{4, "llll│bbbb"},
	} {
		if got := rowOf(g, tc.row); got != tc.want {
			t.Errorf("row %d = %q, want %q", tc.row, got, tc.want)
		}
	}
}

// TestSplitCursorComesFromTheFocusedPaneOnly checks the rule a shared
// grid needs, through a container: whichever pane is drawn last must not
// be able to take the cursor.
func TestSplitCursorComesFromTheFocusedPaneOnly(t *testing.T) {
	want := grid.Cursor{X: 1, Y: 0, Visible: true}
	for _, focusSecond := range []bool{false, true} {
		a := &cursorPane{at: want}
		b := &cursorPane{at: grid.Cursor{X: 3, Y: 1, Visible: true}}
		s := NewSplit(Columns, a, b)
		s.SetFocus(true)
		if focusSecond {
			s.Focus(b)
		}
		g := grid.New(11, 2, fg, bg)
		s.Layout(Size{Cols: 11, Rows: 2})
		g.ResetCursorClaim()

		s.Draw(g.View())

		if !g.CursorClaimed() {
			t.Fatal("no pane claimed the cursor")
		}
		got := g.Cursor()
		if focusSecond && got.X < 6 {
			t.Errorf("cursor at %d, want it in the second pane", got.X)
		}
		if !focusSecond && got != want {
			t.Errorf("cursor = %+v, want the first pane's %+v", got, want)
		}
	}
}

// cursorPane places a cursor only while it has focus, which is what
// every widget on a shared grid has to do.
type cursorPane struct {
	at      grid.Cursor
	focused bool
}

func (*cursorPane) Layout(Size) {}
func (c *cursorPane) Draw(v grid.View) {
	v.Fill(grid.Cell{Rune: '.', FG: color.RGBA{}, BG: bg, Width: 1})
	if c.focused {
		v.SetCursor(c.at)
	}
}
func (c *cursorPane) SetFocus(on bool) { c.focused = on }

// TestSplitFocusDoesNotRepeatItself checks the two guards that keep the
// Focusable contract. Focus is called on every split in the chain every
// time it moves, so telling a pane what it already knows would be a
// blink on every keystroke.
func TestSplitFocusDoesNotRepeatItself(t *testing.T) {
	a, b := &recorder{}, &recorder{}
	s := NewSplit(Columns, a, b)
	s.SetFocus(true)

	s.Focus(a) // already focused
	s.Focus(b)
	s.Focus(b) // already focused
	s.Focus(a)

	if got := a.focus; !alternating(got) {
		t.Errorf("first pane saw %v, want no value twice in a row", got)
	}
	if got := b.focus; !alternating(got) {
		t.Errorf("second pane saw %v, want no value twice in a row", got)
	}
}

// TestSplitFocusWithoutFocusTellsNobody checks the other half of the
// same rule: a split with no focus must not tell a child it lost focus
// it never had.
func TestSplitFocusWithoutFocusTellsNobody(t *testing.T) {
	a, b := &recorder{}, &recorder{}
	s := NewSplit(Columns, a, b)

	s.Focus(b)
	s.Focus(a)

	if len(a.focus) != 0 || len(b.focus) != 0 {
		t.Errorf("panes saw %v and %v, want nothing: the split has no focus",
			a.focus, b.focus)
	}
}

// TestSplitReplaceKeepsTheFocusContract checks the case every split
// makes: the focused pane is replaced by a container holding it, so it
// would be told it gained focus it already had.
func TestSplitReplaceKeepsTheFocusContract(t *testing.T) {
	pane, other := &recorder{}, &recorder{}
	outer := NewSplit(Columns, pane, other)
	outer.SetFocus(true)
	outer.Layout(Size{Cols: 11, Rows: 2})
	if got := pane.focus; len(got) != 1 || !got[0] {
		t.Fatalf("pane saw %v, want it focused once to begin with", got)
	}

	// What splitting does: wrap the focused pane in a split of its own.
	outer.Replace(pane, NewSplit(Rows, pane, &recorder{}))

	if got := pane.focus; !alternating(got) {
		t.Errorf("pane saw %v, want no value twice in a row", got)
	}
	if got := pane.focus; !got[len(got)-1] {
		t.Errorf("pane saw %v, want it focused at the end", got)
	}
}

// TestSplitWheelDoesNotStealFocus checks that scrolling over a pane you
// are not typing in leaves focus where it is.
func TestSplitWheelDoesNotStealFocus(t *testing.T) {
	a, b := &filler{ch: 'a'}, &filler{ch: 'b'}
	s := NewSplit(Columns, a, b)
	s.SetFocus(true)
	s.Layout(Size{Cols: 11, Rows: 2})

	s.HandleMouse(input.MouseEvent{
		Kind: input.MousePress, Button: input.MouseWheelUp, Col: 8, Row: 1,
	})

	if b.focused {
		t.Error("a wheel notch over the other pane took focus")
	}
	if len(b.seen) != 1 {
		t.Error("the wheel notch did not reach the pane under the pointer")
	}
}

// TestSplitSqueezedPaneKeepsItsSize checks that a window too narrow to
// show both panes does not resize the hidden one to nothing. A terminal
// told it has one column reflows its scrollback, and widening the window
// again does not bring it back.
func TestSplitSqueezedPaneKeepsItsSize(t *testing.T) {
	a, b := &filler{ch: 'a'}, &filler{ch: 'b'}
	s := NewSplit(Columns, a, b)
	s.Layout(Size{Cols: 11, Rows: 2})
	was := b.size

	s.Layout(Size{Cols: 2, Rows: 2})

	if b.size != was {
		t.Errorf("the squeezed pane was resized to %+v, want it left at %+v", b.size, was)
	}
	s.Layout(Size{Cols: 11, Rows: 2})
	if b.size != was {
		t.Errorf("the pane came back at %+v, want %+v", b.size, was)
	}
}

func TestDetachCollapsesTheSplitAbove(t *testing.T) {
	a, b := &filler{ch: 'a'}, &filler{ch: 'b'}
	s := NewSplit(Columns, a, b)

	root, ok := Detach(s, a)

	if !ok {
		t.Fatal("Detach refused a widget that was in the tree")
	}
	if root != Widget(b) {
		t.Errorf("root = %v, want the surviving pane: the split had nothing left to divide", root)
	}
}

func TestDetachDeepInATree(t *testing.T) {
	one, two, three := &filler{ch: '1'}, &filler{ch: '2'}, &filler{ch: '3'}
	inner := NewSplit(Rows, two, three)
	outer := NewSplit(Columns, one, inner)

	root, ok := Detach(outer, two)

	if !ok || root != Widget(outer) {
		t.Fatalf("root = %v, %v, want the tree to stand", root, ok)
	}
	if got := Leaves(outer); len(got) != 2 || got[0] != one || got[1] != three {
		t.Errorf("leaves = %v, want the inner split collapsed into its survivor", got)
	}
}

func TestDetachTheLastWidget(t *testing.T) {
	only := &filler{ch: 'o'}

	root, ok := Detach(only, only)

	if !ok {
		t.Fatal("Detach refused the root")
	}
	if root != nil {
		t.Errorf("root = %v, want nothing left", root)
	}
}

func TestDetachSomethingNotInTheTree(t *testing.T) {
	a, b := &filler{ch: 'a'}, &filler{ch: 'b'}
	s := NewSplit(Columns, a, b)

	root, ok := Detach(s, &filler{ch: 'x'})

	if ok {
		t.Error("Detach claimed to remove a stranger")
	}
	if root != Widget(s) {
		t.Errorf("root = %v, want the tree untouched", root)
	}
}

// TestFocusedLeafAndLeavesAgree checks that the pane cycle and the close
// path cannot land on widgets the other does not know about.
func TestFocusedLeafAndLeavesAgree(t *testing.T) {
	for _, tc := range []struct {
		name string
		root Widget
	}{
		{"a plain widget", &filler{ch: 'a'}},
		{"a split", NewSplit(Columns, &filler{ch: 'a'}, &filler{ch: 'b'})},
		{"nested splits", NewSplit(Columns,
			&filler{ch: 'a'}, NewSplit(Rows, &filler{ch: 'b'}, &filler{ch: 'c'}))},
		{"a container with nothing in it", &emptyContainer{}},
		{"a container reporting no focused child", &looseContainer{
			kids: []Widget{&filler{ch: 'a'}, &filler{ch: 'b'}},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			leaf := FocusedLeaf(tc.root)

			for _, l := range Leaves(tc.root) {
				if l == leaf {
					return
				}
			}
			t.Errorf("FocusedLeaf gave %v, which Leaves does not list", leaf)
		})
	}
}

// emptyContainer is a container with no children, which a tab strip
// becomes when its last tab closes.
type emptyContainer struct{}

func (*emptyContainer) Layout(Size)                  {}
func (*emptyContainer) Draw(grid.View)               {}
func (*emptyContainer) Children() []Widget           { return nil }
func (*emptyContainer) Focused() Widget              { return nil }
func (*emptyContainer) Focus(Widget) bool            { return false }
func (*emptyContainer) Replace(_, _ Widget) bool     { return false }
func (*emptyContainer) Remove(Widget) (Widget, bool) { return nil, false }

// looseContainer has children but names none of them focused, which the
// helpers still have to cope with.
type looseContainer struct{ kids []Widget }

func (*looseContainer) Layout(Size)                   {}
func (*looseContainer) Draw(grid.View)                {}
func (c *looseContainer) Children() []Widget          { return c.kids }
func (*looseContainer) Focused() Widget               { return nil }
func (*looseContainer) Focus(Widget) bool             { return false }
func (*looseContainer) Replace(_, _ Widget) bool      { return false }
func (*looseContainer) Remove(Widget) (Widget, bool)  { return nil, false }
func (*looseContainer) ChildArea(Widget) (Rect, bool) { return Rect{}, false }

// recorder keeps every SetFocus value it was given.
type recorder struct {
	focus []bool
	size  Size
}

func (r *recorder) Layout(size Size) { r.size = size }
func (*recorder) Draw(grid.View)     {}
func (r *recorder) SetFocus(on bool) { r.focus = append(r.focus, on) }

// alternating reports whether no value appears twice in a row, which is
// what the Focusable contract promises.
func alternating(vals []bool) bool {
	for i := 1; i < len(vals); i++ {
		if vals[i] == vals[i-1] {
			return false
		}
	}
	return true
}

// stack is a container that really removes children and can end up
// empty, the way a tab strip does. A Split never empties, so without one
// of these the branches of Detach that handle emptying are never taken.
type stack struct {
	kids    []Widget
	focused Widget
	// lie makes Remove report something it may not, so Detach can be
	// attacked with an answer that cannot be true.
	lie func(removed Widget) (Widget, bool)
}

func newStack(kids ...Widget) *stack {
	s := &stack{kids: kids}
	if len(kids) > 0 {
		s.focused = kids[0]
	}
	return s
}

func (*stack) Layout(Size)          {}
func (*stack) Draw(grid.View)       {}
func (s *stack) Children() []Widget { return s.kids }
func (s *stack) Focused() Widget    { return s.focused }

func (s *stack) Focus(w Widget) bool {
	if !contains(s.kids, w) {
		return false
	}
	s.focused = w
	return true
}

func (s *stack) Replace(old, new Widget) bool {
	for i, kid := range s.kids {
		if kid != old {
			continue
		}
		s.kids[i] = new
		if s.focused == old {
			s.focused = new
		}
		return true
	}
	return false
}

func (s *stack) Remove(w Widget) (Widget, bool) {
	if s.lie != nil {
		return s.lie(w)
	}
	for i, kid := range s.kids {
		if kid != w {
			continue
		}
		s.kids = append(s.kids[:i], s.kids[i+1:]...)
		if s.focused == w {
			s.focused = nil
			if len(s.kids) > 0 {
				s.focused = s.kids[0]
			}
		}
		switch len(s.kids) {
		case 0:
			return nil, true
		case 1:
			return s.kids[0], true
		}
		return s, true
	}
	return nil, false
}

func (s *stack) ChildArea(w Widget) (Rect, bool) {
	if !contains(s.kids, w) {
		return Rect{}, false
	}
	return Rect{Cols: 10, Rows: 4}, true
}

// TestDetachThroughAContainerThatCarriesOn checks the branch a split can
// never reach: a container with children to spare stays in the tree.
func TestDetachThroughAContainerThatCarriesOn(t *testing.T) {
	one, two, three := &filler{ch: '1'}, &filler{ch: '2'}, &filler{ch: '3'}
	st := newStack(one, two, three)

	root, ok := Detach(st, two)

	if !ok || root != Widget(st) {
		t.Fatalf("root = %v, %v, want the container to stand", root, ok)
	}
	if got := Leaves(st); len(got) != 2 || got[0] != one || got[1] != three {
		t.Errorf("leaves = %v, want the other two", got)
	}
}

// TestDetachThroughAContainerThatEmpties checks that a container with
// nothing left in it goes too, and keeps going up.
func TestDetachThroughAContainerThatEmpties(t *testing.T) {
	only, beside := &filler{ch: 'o'}, &filler{ch: 'b'}
	st := newStack(only)
	outer := NewSplit(Columns, st, beside)

	root, ok := Detach(outer, only)

	if !ok {
		t.Fatal("Detach refused a widget that was in the tree")
	}
	if root != Widget(beside) {
		t.Errorf("root = %v, want the pane beside it: the empty container and "+
			"the split above it both had nothing left", root)
	}
}

// TestDetachRejectsAnImpossibleAnswer checks that a container cannot
// splice a stranger into the tree, or put back the widget being removed.
func TestDetachRejectsAnImpossibleAnswer(t *testing.T) {
	for _, tc := range []struct {
		name string
		lie  func(target, stranger Widget) func(Widget) (Widget, bool)
	}{
		{
			name: "a widget that is not a child",
			lie: func(_, stranger Widget) func(Widget) (Widget, bool) {
				return func(Widget) (Widget, bool) { return stranger, true }
			},
		},
		{
			name: "the widget being removed",
			lie: func(target, _ Widget) func(Widget) (Widget, bool) {
				return func(Widget) (Widget, bool) { return target, true }
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			target, keep := &filler{ch: 't'}, &filler{ch: 'k'}
			stranger := &filler{ch: 'x'}
			st := newStack(target, keep)
			st.lie = tc.lie(target, stranger)
			outer := NewSplit(Columns, st, &filler{ch: 'o'})

			root, ok := Detach(outer, target)

			if ok {
				t.Error("Detach believed an answer that cannot be true")
			}
			if root != Widget(outer) {
				t.Errorf("root = %v, want the tree untouched", root)
			}
			for _, leaf := range Leaves(outer) {
				if leaf == stranger {
					t.Fatal("a widget that was never in the tree is in it now")
				}
			}
		})
	}
}

// TestSplitRemoveHandsFocusOn checks that a split giving up its focused
// child does not still name it. Something walking the split's focus
// later would tell that child a second time that focus left.
func TestSplitRemoveHandsFocusOn(t *testing.T) {
	going, staying := &recorder{}, &recorder{}
	s := NewSplit(Columns, going, staying)
	s.SetFocus(true)
	if got := going.focus; len(got) != 1 || !got[0] {
		t.Fatalf("the first pane saw %v, want it focused to begin with", got)
	}

	s.Remove(going)
	// What happens next: the split is thrown away and the survivor takes
	// its place.
	s.SetFocus(false)

	if got := going.focus; !alternating(got) {
		t.Errorf("the removed pane saw %v, want no value twice in a row", got)
	}
	if s.Focused() == Widget(going) {
		t.Error("the split still names the child it gave up")
	}
}

// TestAreaOfFindsAPaneInTheTree checks the position a container reports
// for a child, which is how anything asks where a pane actually sits.
func TestAreaOfFindsAPaneInTheTree(t *testing.T) {
	left, top, bottom := &filler{ch: 'l'}, &filler{ch: 't'}, &filler{ch: 'b'}
	inner := NewSplit(Rows, top, bottom)
	outer := NewSplit(Columns, left, inner)
	whole := Rect{Cols: 9, Rows: 5}
	outer.Layout(whole.Size())

	for _, tc := range []struct {
		name string
		want Rect
		of   Widget
	}{
		{name: "left", of: left, want: Rect{Cols: 4, Rows: 5}},
		{name: "top right", of: top, want: Rect{X: 5, Cols: 4, Rows: 2}},
		{name: "bottom right", of: bottom, want: Rect{X: 5, Y: 3, Cols: 4, Rows: 2}},
		{name: "the root itself", of: outer, want: whole},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := AreaOf(outer, whole, tc.of)
			if !ok {
				t.Fatal("AreaOf did not find it")
			}
			if got != tc.want {
				t.Errorf("area = %+v, want %+v", got, tc.want)
			}
		})
	}

	if _, ok := AreaOf(outer, whole, &filler{}); ok {
		t.Error("AreaOf found a widget that is not in the tree")
	}
}

// TestAreaOfSkipsAPaneWithNoRoom checks that a pane a narrow layout
// squeezed out reports no area, which is how anything can tell it is not
// being shown.
func TestAreaOfSkipsAPaneWithNoRoom(t *testing.T) {
	a, b := &filler{ch: 'a'}, &filler{ch: 'b'}
	s := NewSplit(Columns, a, b)
	whole := Rect{Cols: 2, Rows: 4}
	s.Layout(whole.Size())

	if _, ok := AreaOf(s, whole, b); ok {
		t.Error("a pane with nowhere to be drawn reported an area")
	}
	if _, ok := AreaOf(s, whole, a); !ok {
		t.Error("the pane that did get the room reported none")
	}
}

// rootOver builds a root over a widget, laid out to the given size.
func rootOver(w Widget, cols, rows int) *Root {
	r := &Root{}
	r.SetWidget(w)
	r.Layout(Rect{Cols: cols, Rows: rows})
	return r
}

func pressAt(col, row int) input.MouseEvent {
	return input.MouseEvent{Kind: input.MousePress, Button: input.MouseLeft, Col: col, Row: row}
}

func moveTo(col, row int) input.MouseEvent {
	return input.MouseEvent{Kind: input.MouseMove, Button: input.MouseLeft, Col: col, Row: row}
}

func releaseAt(col, row int) input.MouseEvent {
	return input.MouseEvent{Kind: input.MouseRelease, Button: input.MouseLeft, Col: col, Row: row}
}

func kindsOf(f *filler) []input.MouseKind {
	out := make([]input.MouseKind, len(f.seen))
	for i, ev := range f.seen {
		out[i] = ev.Kind
	}
	return out
}

// TestDragStaysWithThePaneItStartedIn checks that a selection dragged
// across the divider does not jump to the other pane.
func TestDragStaysWithThePaneItStartedIn(t *testing.T) {
	a, b := &filler{ch: 'a'}, &filler{ch: 'b'}
	s := NewSplit(Columns, a, b)
	r := rootOver(s, 11, 2)

	r.HandleMouse(pressAt(1, 0))
	r.HandleMouse(moveTo(9, 1))
	r.HandleMouse(releaseAt(9, 1))

	if len(b.seen) != 0 {
		t.Errorf("the other pane saw %d events during the drag", len(b.seen))
	}
	if got := kindsOf(a); len(got) != 3 || got[2] != input.MouseRelease {
		t.Errorf("the dragging pane saw %v, want its press, move and release", got)
	}
	// Dragged past its edge, the pane hears about its own last column.
	if got := a.seen[1]; got.Col != 4 {
		t.Errorf("the drag reached column %d, want it clamped to 4", got.Col)
	}
	if b.focused {
		t.Error("dragging into the other pane focused it")
	}
}

// TestDragSurvivesThePaneBeingSplit is the case a one-hop capture cannot
// handle. The pane holding the drag is still there, but it is deeper in
// the tree and narrower than it was.
func TestDragSurvivesThePaneBeingSplit(t *testing.T) {
	dragging, other := &filler{ch: 'd'}, &filler{ch: 'o'}
	outer := NewSplit(Columns, dragging, other)
	r := rootOver(outer, 21, 2)

	r.HandleMouse(pressAt(1, 0))
	// Splitting the pane that holds the drag, which is legal while a
	// button is down.
	outer.Replace(dragging, NewSplit(Columns, dragging, &filler{ch: 'n'}))
	r.HandleMouse(moveTo(8, 0))
	r.HandleMouse(releaseAt(8, 0))

	if got := kindsOf(dragging); len(got) != 3 || got[2] != input.MouseRelease {
		t.Errorf("the dragging pane saw %v, want its press, move and release", got)
	}
	for _, leaf := range Leaves(outer) {
		f, ok := leaf.(*filler)
		if !ok || f == dragging {
			continue
		}
		for _, ev := range f.seen {
			if ev.Kind == input.MouseRelease {
				t.Error("a pane got a release for a press it never saw")
			}
		}
	}
}

// TestDragOnAPaneThatGoesAwayReachesNobody checks the other half: when
// the pane holding the drag is destroyed, the release belongs to nothing
// rather than to whoever happens to be under the pointer.
func TestDragOnAPaneThatGoesAwayReachesNobody(t *testing.T) {
	dying, other, third := &filler{ch: 'd'}, &filler{ch: 'o'}, &filler{ch: 't'}
	inner := NewSplit(Rows, third, dying)
	outer := NewSplit(Columns, other, inner)
	r := rootOver(outer, 11, 4)

	r.HandleMouse(pressAt(8, 3))
	if len(dying.seen) != 1 {
		t.Fatalf("the press reached %d events, want 1", len(dying.seen))
	}

	// The pane holding the drag is closed.
	if _, ok := Detach(outer, dying); !ok {
		t.Fatal("Detach refused")
	}
	r.HandleMouse(moveTo(1, 0))
	handled, err := r.HandleMouse(releaseAt(1, 0))

	if err != nil {
		t.Fatalf("HandleMouse: %v", err)
	}
	if handled {
		t.Error("an event for a pane that has gone was reported handled")
	}
	for _, ev := range other.seen {
		if ev.Kind == input.MouseRelease {
			t.Error("a pane got the release meant for one that had gone")
		}
	}
	// And the pointer is free again for the next press.
	r.HandleMouse(pressAt(1, 0))
	if got := kindsOf(other); len(got) == 0 || got[len(got)-1] != input.MousePress {
		t.Errorf("the surviving pane saw %v, want a fresh press to reach it", got)
	}
}

// TestClickOnADividerIsHeldByTheSplit checks that a press on a divider,
// which belongs to no pane, is kept by the split itself. That is what
// makes the whole drag come back to it.
func TestClickOnADividerIsHeldByTheSplit(t *testing.T) {
	a, b := &filler{ch: 'a'}, &filler{ch: 'b'}
	s := NewSplit(Columns, a, b)
	r := rootOver(s, 11, 2)

	if handled, _ := r.HandleMouse(pressAt(5, 0)); !handled {
		t.Error("a press on the divider was not handled")
	}
	if got := r.Holding(); got != Widget(s) {
		t.Errorf("the pointer is held by %v, want the split", got)
	}
	if len(a.seen) != 0 || len(b.seen) != 0 {
		t.Error("the press on the divider reached a pane")
	}

	// And once the button comes up, the next press goes where it landed.
	r.HandleMouse(releaseAt(5, 0))
	r.HandleMouse(pressAt(8, 0))

	if len(b.seen) != 1 {
		t.Errorf("the pane under the second press saw %d events, want 1", len(b.seen))
	}
}

// TestLeafAt finds the deepest widget at a point.
func TestLeafAt(t *testing.T) {
	left, top, bottom := &filler{ch: 'l'}, &filler{ch: 't'}, &filler{ch: 'b'}
	outer := NewSplit(Columns, left, NewSplit(Rows, top, bottom))
	whole := Rect{Cols: 9, Rows: 5}
	outer.Layout(whole.Size())

	for _, tc := range []struct {
		name string
		x, y int
		want Widget
	}{
		{name: "left pane", x: 1, y: 2, want: left},
		{name: "top right", x: 7, y: 0, want: top},
		{name: "bottom right", x: 7, y: 4, want: bottom},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, area, ok := LeafAt(outer, whole, tc.x, tc.y)
			if !ok {
				t.Fatal("LeafAt found nothing")
			}
			if got != tc.want {
				t.Errorf("leaf = %v, want %v", got, tc.want)
			}
			if !area.Contains(tc.x, tc.y) {
				t.Errorf("area %+v does not hold the point", area)
			}
		})
	}

	// The divider is the split's own chrome, so it belongs to the split
	// rather than to either pane. A press there is the split's to keep.
	if got, _, ok := LeafAt(outer, whole, 4, 2); !ok || got != Widget(outer) {
		t.Errorf("the divider belongs to %v, want the split itself", got)
	}
	if _, _, ok := LeafAt(outer, whole, 99, 99); ok {
		t.Error("a point outside the tree found a widget")
	}
}

// TestSplitFocusOnTheFocusedChildIsSilent checks the guard that stops a
// click on the pane you are already in blinking focus off and on. Focus
// runs on every container in the chain every time it moves.
func TestSplitFocusOnTheFocusedChildIsSilent(t *testing.T) {
	a, b := &recorder{}, &recorder{}
	s := NewSplit(Columns, a, b)
	s.SetFocus(true)
	before := len(a.focus)

	s.Focus(a)
	s.Focus(a)

	if got := len(a.focus); got != before {
		t.Errorf("the focused pane heard %d more times, want none", got-before)
	}
}

// TestSplitSetFocusIsIdempotent checks the guard that absorbs a parent
// telling a split what it already knows. Without it the split passes the
// repeat on and a pane below hears the same thing twice.
func TestSplitSetFocusIsIdempotent(t *testing.T) {
	a, b := &recorder{}, &recorder{}
	s := NewSplit(Columns, a, b)

	s.SetFocus(true)
	s.SetFocus(true)
	s.SetFocus(false)
	s.SetFocus(false)

	if got := a.focus; len(got) != 2 || !alternating(got) {
		t.Errorf("the pane heard %v, want exactly one arrival and one departure", got)
	}
}

// TestDetachRejectsARefusal checks that a container answering "not my
// child" is believed, rather than read as "I am empty now" and thrown
// away along with the children it still has.
func TestDetachRejectsARefusal(t *testing.T) {
	// The target really is a child, so the container's answer is the
	// only thing standing between Detach and the wrong tree.
	keep, also := &filler{ch: 'k'}, &filler{ch: 'a'}
	st := newStack(keep, also)
	st.lie = func(Widget) (Widget, bool) { return nil, false }
	outer := NewSplit(Columns, st, &filler{ch: 'o'})

	root, ok := Detach(outer, keep)

	if ok {
		t.Error("Detach claimed to remove a widget the container refused")
	}
	if root != Widget(outer) {
		t.Errorf("root = %v, want the tree untouched", root)
	}
	if got := Leaves(outer); len(got) != 3 {
		t.Errorf("leaves = %v, want the container and both children still there", got)
	}
}

// TestSplitReplaceRefusesADuplicate checks the guard against holding one
// widget in both halves, which would give Remove two answers.
func TestSplitReplaceRefusesADuplicate(t *testing.T) {
	a, b := &filler{ch: 'a'}, &filler{ch: 'b'}
	s := NewSplit(Columns, a, b)

	if s.Replace(a, b) {
		t.Error("Replace put the same widget in both halves")
	}
	if got := s.Children(); got[0] != Widget(a) || got[1] != Widget(b) {
		t.Errorf("children = %v, want them untouched", got)
	}
	// Replacing a child with itself is harmless and stays allowed.
	if !s.Replace(a, a) {
		t.Error("Replace refused a child with itself")
	}
}

// wheelAt is a notch of the wheel, which has a press and no release.
func wheelAt(col, row int) input.MouseEvent {
	return input.MouseEvent{Kind: input.MousePress, Button: input.MouseWheelUp, Col: col, Row: row}
}

// TestSplitDividerCanBeDragged is the whole gesture the user makes:
// press on the divider, move, let go. Root holds the pointer for the
// split, so every event comes back to it.
func TestSplitDividerCanBeDragged(t *testing.T) {
	a, b := &filler{ch: 'a'}, &filler{ch: 'b'}
	s := NewSplit(Columns, a, b)
	r := rootOver(s, 21, 4)
	if a.size.Cols != 10 || b.size.Cols != 10 {
		t.Fatalf("the halves start %d and %d wide, want the room shared evenly", a.size.Cols, b.size.Cols)
	}

	if took, _ := r.HandleMouse(pressAt(10, 1)); !took {
		t.Fatal("a press on the divider was not taken, so no drag can follow")
	}
	r.HandleMouse(moveTo(5, 1))

	if a.size.Cols != 5 || b.size.Cols != 15 {
		t.Fatalf("the halves are %d and %d wide, want 5 and 15", a.size.Cols, b.size.Cols)
	}
	r.HandleMouse(releaseAt(5, 1))

	// And a move once the button is up is nothing to do with the divider.
	r.HandleMouse(moveTo(17, 1))
	if a.size.Cols != 5 {
		t.Fatalf("the first half is %d wide after the button came up, want 5", a.size.Cols)
	}
}

// TestSplitDividerDragsTheOtherWayToo checks a split that stacks its
// children: the row the pointer is on is what moves the divider.
func TestSplitDividerDragsTheOtherWayToo(t *testing.T) {
	top, bottom := &filler{ch: 't'}, &filler{ch: 'b'}
	s := NewSplit(Rows, top, bottom)
	r := rootOver(s, 8, 21)

	r.HandleMouse(pressAt(3, 10))
	r.HandleMouse(moveTo(3, 15))
	r.HandleMouse(releaseAt(3, 15))

	if top.size.Rows != 15 || bottom.size.Rows != 5 {
		t.Fatalf("the halves are %d and %d rows tall, want 15 and 5", top.size.Rows, bottom.size.Rows)
	}
}

// TestSplitDragKeepsACellForEachHalf checks that a divider dragged off
// the end of the window leaves both panes on screen. A pane squeezed to
// nothing is one the user cannot get back.
func TestSplitDragKeepsACellForEachHalf(t *testing.T) {
	a, b := &filler{ch: 'a'}, &filler{ch: 'b'}
	s := NewSplit(Columns, a, b)
	r := rootOver(s, 21, 4)

	r.HandleMouse(pressAt(10, 1))
	r.HandleMouse(moveTo(-99, 1))
	if a.size.Cols != 1 || b.size.Cols != 19 {
		t.Fatalf("dragged off the left the halves are %d and %d wide, want 1 and 19",
			a.size.Cols, b.size.Cols)
	}
	r.HandleMouse(moveTo(999, 1))
	if a.size.Cols != 19 || b.size.Cols != 1 {
		t.Fatalf("dragged off the right the halves are %d and %d wide, want 19 and 1",
			a.size.Cols, b.size.Cols)
	}
	// Both are still on screen, so both can be grabbed back.
	if _, ok := s.ChildArea(a); !ok {
		t.Error("the first half has nowhere to be drawn")
	}
	if _, ok := s.ChildArea(b); !ok {
		t.Error("the second half has nowhere to be drawn")
	}

	// The root clamps the pointer into the split's area on the way in.
	// The weight keeps its own range anyway, for a caller that does not:
	// it is a share from 0 to 1 whatever point it is handed.
	s.HandleMouse(moveTo(999, 1))
	if s.Weight > 1 {
		t.Errorf("weight = %v, want no more than 1", s.Weight)
	}
	s.HandleMouse(moveTo(-99, 1))
	if s.Weight < 0 {
		t.Errorf("weight = %v, want no less than 0", s.Weight)
	}
}

// TestSplitCancelGestureEndsADrag checks the drag whose release never
// comes: a dialog opening over the window must not leave the divider
// stuck to the pointer.
func TestSplitCancelGestureEndsADrag(t *testing.T) {
	a, b := &filler{ch: 'a'}, &filler{ch: 'b'}
	s := NewSplit(Columns, a, b)
	r := rootOver(s, 21, 4)

	r.HandleMouse(pressAt(10, 1))
	s.CancelGesture()
	r.HandleMouse(moveTo(4, 1))

	if s.Weight != 0.5 || a.size.Cols != 10 {
		t.Fatalf("weight = %v and the first half is %d wide, want the drag to have stopped",
			s.Weight, a.size.Cols)
	}
}

// TestSplitWheelDuringADragDoesNotMoveIt checks that scrolling mid-drag
// is not read as a move. A notch has no release, so acting on one would
// put the divider where the pointer is not.
func TestSplitWheelDuringADragDoesNotMoveIt(t *testing.T) {
	a, b := &filler{ch: 'a'}, &filler{ch: 'b'}
	s := NewSplit(Columns, a, b)
	r := rootOver(s, 21, 4)

	r.HandleMouse(pressAt(10, 1))
	r.HandleMouse(wheelAt(2, 1))

	if s.Weight != 0.5 || a.size.Cols != 10 {
		t.Fatalf("weight = %v and the first half is %d wide, want the wheel ignored",
			s.Weight, a.size.Cols)
	}
	// The drag is still on: the notch did not end it either.
	r.HandleMouse(moveTo(5, 1))
	if a.size.Cols != 5 {
		t.Fatalf("the first half is %d wide, want the drag to carry on after the notch", a.size.Cols)
	}
}

// TestSplitPressBesideTheDividerStillReachesThePane checks that the
// handle is the divider and nothing more: the column next to it belongs
// to the pane.
func TestSplitPressBesideTheDividerStillReachesThePane(t *testing.T) {
	a, b := &filler{ch: 'a'}, &filler{ch: 'b'}
	s := NewSplit(Columns, a, b)
	r := rootOver(s, 21, 4)

	r.HandleMouse(pressAt(9, 1))
	r.HandleMouse(moveTo(9, 2))
	r.HandleMouse(releaseAt(9, 2))

	if got := kindsOf(a); len(got) != 3 {
		t.Fatalf("the pane beside the divider saw %v, want its press, move and release", got)
	}
	if s.Weight != 0.5 {
		t.Errorf("weight = %v, want a press beside the divider to leave it alone", s.Weight)
	}
}

// TestSplitWeightSurvivesReplace checks that swapping a pane keeps the
// width the user dragged it to. A pane that reconnects should not put
// the divider back in the middle.
func TestSplitWeightSurvivesReplace(t *testing.T) {
	a, b := &filler{ch: 'a'}, &filler{ch: 'b'}
	s := NewSplit(Columns, a, b)
	r := rootOver(s, 21, 4)

	r.HandleMouse(pressAt(10, 1))
	r.HandleMouse(moveTo(5, 1))
	r.HandleMouse(releaseAt(5, 1))
	want := s.Weight

	next := &filler{ch: 'c'}
	if !s.Replace(b, next) {
		t.Fatal("Replace refused a child that was there")
	}

	if s.Weight != want {
		t.Errorf("weight = %v after Replace, want %v", s.Weight, want)
	}
	if a.size.Cols != 5 || next.size.Cols != 15 {
		t.Errorf("the halves are %d and %d wide, want the width the drag left",
			a.size.Cols, next.size.Cols)
	}
}

// TestSplitDragRedrawsBothHalvesAndThenSettles checks the damage a drag
// makes. Both panes moved, so both are drawn again, and the frame after
// writes nothing: a divider that is not moving must not repaint the
// window.
func TestSplitDragRedrawsBothHalvesAndThenSettles(t *testing.T) {
	a, b := &filler{ch: 'a'}, &filler{ch: 'b'}
	s := NewSplit(Columns, a, b)
	s.DividerFG = fg
	r := rootOver(s, 21, 4)
	g := grid.New(21, 4, fg, bg)
	s.Draw(g.View())
	g.ClearDirty()

	a.drawn, b.drawn = false, false
	r.HandleMouse(pressAt(10, 1))
	r.HandleMouse(moveTo(5, 1))
	r.HandleMouse(releaseAt(5, 1))
	s.Draw(g.View())

	if !a.drawn || !b.drawn {
		t.Fatalf("after the drag a was drawn=%v and b drawn=%v, want both", a.drawn, b.drawn)
	}
	if a.size.Cols != 5 || b.size.Cols != 15 {
		t.Fatalf("the halves are %d and %d wide, want both laid out again", a.size.Cols, b.size.Cols)
	}
	if !g.AnyDirty() {
		t.Fatal("the drag changed the layout and dirtied nothing")
	}
	if got := rowOf(g, 0); got != "aaaaa│bbbbbbbbbbbbbbb" {
		t.Errorf("the row reads %q, want the divider where it was dragged", got)
	}

	// And an idle frame after it writes nothing.
	g.ClearDirty()
	s.Draw(g.View())
	if g.AnyDirty() {
		t.Error("an idle frame after the drag dirtied the grid")
	}
}

// rightTap is the other button pressed and let go mid-drag, which must
// not be read as the end of the drag.
func rightTap(col, row int) []input.MouseEvent {
	return []input.MouseEvent{
		{Kind: input.MousePress, Button: input.MouseRight, Col: col, Row: row},
		{Kind: input.MouseRelease, Button: input.MouseRight, Col: col, Row: row},
	}
}

// TestSplitDragIgnoresAnotherButtonComingUp checks that only the button
// that started the drag ends it. Another one coming up proves nothing:
// the first may still be down, and the pointer is still the split's.
func TestSplitDragIgnoresAnotherButtonComingUp(t *testing.T) {
	a, b := &filler{ch: 'a'}, &filler{ch: 'b'}
	s := NewSplit(Columns, a, b)
	r := rootOver(s, 21, 4)

	r.HandleMouse(pressAt(10, 1))
	r.HandleMouse(moveTo(8, 1))
	for _, ev := range rightTap(8, 1) {
		r.HandleMouse(ev)
	}
	r.HandleMouse(moveTo(5, 1))

	if a.size.Cols != 5 || b.size.Cols != 15 {
		t.Fatalf("the halves are %d and %d wide, want the drag to carry on to 5 and 15",
			a.size.Cols, b.size.Cols)
	}
	// And the left button still ends it.
	r.HandleMouse(releaseAt(5, 1))
	r.HandleMouse(moveTo(15, 1))
	if a.size.Cols != 5 {
		t.Fatalf("the first half is %d wide after the button came up, want 5", a.size.Cols)
	}
}

// A press on the half without the keys moves them and goes no further,
// for a child that asks for that. The press after it is the child's.
func TestSplitKeepsThePressThatMovesTheKeys(t *testing.T) {
	a, b := &picky{fake: fake{name: "a"}, first: true}, &picky{fake: fake{name: "b"}, first: true}
	s := NewSplit(Columns, a, b)
	r := rootOver(s, 21, 4)
	s.SetFocus(true)

	if took, err := r.HandleMouse(pressAt(15, 1)); err != nil || !took {
		t.Fatalf("the press was taken=%v: %v", took, err)
	}

	if s.Focused() != Widget(b) {
		t.Error("the press did not move the keys to the half it landed in")
	}
	if !b.focus {
		t.Error("the half the press landed in was not told it has the keys")
	}
	if len(b.clicks) != 0 {
		t.Errorf("the half was handed %d presses as well, want none", len(b.clicks))
	}

	r.HandleMouse(releaseAt(15, 1))
	r.HandleMouse(pressAt(15, 1))
	if len(b.clicks) == 0 {
		t.Error("the second press was kept as well, so the half is never clicked")
	}
}

// A child that does not ask to be spared gets the press that moves the
// keys. The sidebar is one: its rows are buttons, and a press on one
// means the button whether or not the sidebar has the keys.
func TestSplitDeliversThePressToAChildThatWantsIt(t *testing.T) {
	a, b := &fake{name: "a"}, &fake{name: "b"}
	s := NewSplit(Columns, a, b)
	r := rootOver(s, 21, 4)
	s.SetFocus(true)

	r.HandleMouse(pressAt(15, 1))

	if s.Focused() != Widget(b) {
		t.Error("the press did not move the keys to the half it landed in")
	}
	if len(b.clicks) != 1 {
		t.Errorf("the half was handed %d presses, want the one that moved the keys", len(b.clicks))
	}
}
