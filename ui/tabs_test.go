package ui

import (
	"testing"

	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/input"
)

// named is a widget with a title, the way a terminal has one.
type named struct {
	filler
	title string
}

func (n *named) Title() string { return n.title }

func drawTabs(tb *Tabs, cols, rows int) *grid.Grid {
	g := grid.New(cols, rows, fg, bg)
	tb.Layout(Size{Cols: cols, Rows: rows})
	tb.Draw(g.View())
	return g
}

func TestTabsShowOneChildAtATime(t *testing.T) {
	one, two := &filler{ch: '1'}, &filler{ch: '2'}
	tb := NewTabs(one, two)

	g := drawTabs(tb, 8, 3)

	if got := rowOf(g, 1); got != "11111111" {
		t.Errorf("row 1 = %q, want the first tab", got)
	}
	if got := rowOf(g, 2); got != "11111111" {
		t.Errorf("row 2 = %q, want the first tab", got)
	}
	// The hidden tab is not drawn, but it is still told how much room it
	// has: a program running in it is writing output sized to whatever
	// it was last told, and bringing the tab forward cannot undo that.
	if got := two.size; got != (Size{Cols: 8, Rows: 2}) {
		t.Errorf("the hidden tab has %+v, want the body's size", got)
	}
	if two.drawn {
		t.Error("the hidden tab was drawn")
	}
}

func TestTabsStripShowsEveryLabel(t *testing.T) {
	tb := NewTabs(&named{title: "one"}, &named{title: "two"})

	g := drawTabs(tb, 12, 3)

	if got := rowOf(g, 0); got != " one  two   " {
		t.Errorf("strip = %q, want both labels", got)
	}
}

// TestTabsLabelFallsBackToThePosition checks a tab whose widget has no
// title yet, which is every shell until its program sets one.
func TestTabsLabelFallsBackToThePosition(t *testing.T) {
	tb := NewTabs(&named{title: ""}, &filler{ch: 'x'})

	g := drawTabs(tb, 8, 3)

	if got := rowOf(g, 0); got != " 1  2   " {
		t.Errorf("strip = %q, want positions where there are no titles", got)
	}
}

// TestTabsLabelCanBeOverridden checks the hook an app uses to name tabs
// its own way.
func TestTabsLabelCanBeOverridden(t *testing.T) {
	tb := NewTabs(&filler{ch: 'a'}, &filler{ch: 'b'})
	tb.Label = func(_ Widget, i int) string {
		return string(rune('A' + i))
	}

	g := drawTabs(tb, 8, 3)

	if got := rowOf(g, 0); got != " A  B   " {
		t.Errorf("strip = %q, want the labels the app chose", got)
	}
}

// TestTabsLabelThatDoesNotFitIsNotDrawn checks a strip too narrow for
// every tab. A label half drawn would be worse than one missing.
func TestTabsLabelThatDoesNotFitIsNotDrawn(t *testing.T) {
	tb := NewTabs(&named{title: "first"}, &named{title: "second"})

	g := drawTabs(tb, 9, 3)

	if got := rowOf(g, 0); got != " first   " {
		t.Errorf("strip = %q, want only the label that fits", got)
	}
	// And it cannot be clicked either.
	tb.HandleMouse(input.MouseEvent{Kind: input.MousePress, Button: input.MouseLeft, Col: 8})
	if tb.Focused() != tb.Children()[0] {
		t.Error("clicking past the last label that fits selected something")
	}
}

// TestTabsTooShortForAStrip checks the sizes a layout passes through
// while it settles. With no room for both, the tab gets everything.
func TestTabsTooShortForAStrip(t *testing.T) {
	for _, tc := range []struct {
		rows      int
		wantRows  int
		wantStrip bool
	}{
		{rows: 0, wantRows: 0},
		{rows: 1, wantRows: 1},
		{rows: 2, wantRows: 1, wantStrip: true},
		{rows: 5, wantRows: 4, wantStrip: true},
	} {
		kid := &filler{ch: 'k'}
		tb := NewTabs(kid)

		tb.Layout(Size{Cols: 8, Rows: tc.rows})

		if kid.size.Rows != tc.wantRows {
			t.Errorf("%d rows: the tab got %d, want %d", tc.rows, kid.size.Rows, tc.wantRows)
		}
		if got := !tb.strip().Empty(); got != tc.wantStrip {
			t.Errorf("%d rows: strip shown = %v, want %v", tc.rows, got, tc.wantStrip)
		}
	}
}

func TestTabsNoColumnsGivesNothing(t *testing.T) {
	kid := &filler{ch: 'k'}
	tb := NewTabs(kid)

	tb.Layout(Size{Cols: 0, Rows: 5})

	if !kid.size.Empty() {
		t.Errorf("the tab got %+v, want nothing", kid.size)
	}
}

func TestTabsClickSelects(t *testing.T) {
	one, two := &named{title: "one"}, &named{title: "two"}
	tb := NewTabs(one, two)
	tb.SetFocus(true)
	tb.Layout(Size{Cols: 12, Rows: 3})

	handled, err := tb.HandleMouse(input.MouseEvent{
		Kind: input.MousePress, Button: input.MouseLeft, Col: 7, Row: 0,
	})

	if err != nil {
		t.Fatalf("HandleMouse: %v", err)
	}
	if !handled {
		t.Error("a click on a label was not handled")
	}
	if tb.Focused() != Widget(two) {
		t.Error("clicking a label did not select its tab")
	}
	if !two.focused || one.focused {
		t.Errorf("focus = %v and %v, want the second tab only", one.focused, two.focused)
	}
}

// TestTabsClickOnTheEmptyStripSelectsNothing checks that the space to
// the right of the last label belongs to no tab.
func TestTabsClickOnTheEmptyStripSelectsNothing(t *testing.T) {
	one, two := &named{title: "one"}, &named{title: "two"}
	tb := NewTabs(one, two)
	tb.SetFocus(true)
	tb.Layout(Size{Cols: 20, Rows: 3})

	handled, _ := tb.HandleMouse(input.MouseEvent{
		Kind: input.MousePress, Button: input.MouseLeft, Col: 18, Row: 0,
	})

	if handled {
		t.Error("a click on the empty strip was handled")
	}
	if tb.Focused() != Widget(one) {
		t.Error("a click on the empty strip changed the tab")
	}
}

// TestTabsWheelOverTheStripSelectsNothing checks that scrolling does not
// switch tabs by accident.
func TestTabsWheelOverTheStripSelectsNothing(t *testing.T) {
	one, two := &named{title: "one"}, &named{title: "two"}
	tb := NewTabs(one, two)
	tb.SetFocus(true)
	tb.Layout(Size{Cols: 12, Rows: 3})

	tb.HandleMouse(input.MouseEvent{
		Kind: input.MousePress, Button: input.MouseWheelUp, Col: 7, Row: 0,
	})

	if tb.Focused() != Widget(one) {
		t.Error("a wheel notch over a label selected its tab")
	}
}

func TestTabsMouseReachesTheTabBeingShown(t *testing.T) {
	one, two := &filler{ch: '1'}, &filler{ch: '2'}
	tb := NewTabs(one, two)
	tb.Layout(Size{Cols: 8, Rows: 4})

	tb.HandleMouse(input.MouseEvent{
		Kind: input.MousePress, Button: input.MouseLeft, Col: 3, Row: 2,
	})

	if len(two.seen) != 0 {
		t.Error("the hidden tab saw a mouse event")
	}
	if len(one.seen) != 1 {
		t.Fatalf("the shown tab saw %d events, want 1", len(one.seen))
	}
	// The strip is not part of the tab's own coordinates.
	if got := one.seen[0]; got.Col != 3 || got.Row != 1 {
		t.Errorf("the tab was told %d,%d, want 3,1", got.Col, got.Row)
	}
}

func TestTabsKeysGoToTheTabBeingShown(t *testing.T) {
	one := &filler{ch: '1', takes: input.KeyQ}
	two := &filler{ch: '2', takes: input.KeyQ}
	tb := NewTabs(one, two)
	tb.SetFocus(true)

	handled, err := tb.HandleKey(press(input.KeyQ, 0))

	if err != nil {
		t.Fatalf("HandleKey: %v", err)
	}
	if !handled || len(two.keys) != 0 {
		t.Errorf("handled = %v, hidden tab saw %v", handled, two.keys)
	}
}

func TestTabsAdd(t *testing.T) {
	one := &filler{ch: '1'}
	tb := NewTabs(one)
	tb.SetFocus(true)
	tb.Layout(Size{Cols: 8, Rows: 4})

	two := &filler{ch: '2'}
	tb.Add(two)

	if got := tb.Children(); len(got) != 2 || got[1] != Widget(two) {
		t.Fatalf("children = %v, want the new tab at the end", got)
	}
	if tb.Focused() != Widget(two) {
		t.Error("the new tab was not shown")
	}
	if two.size.Rows != 3 {
		t.Errorf("the new tab got %+v, want the body's size", two.size)
	}
	// Adding the same widget twice must not put it in twice.
	tb.Add(two)
	tb.Add(nil)
	if got := len(tb.Children()); got != 2 {
		t.Errorf("%d children, want 2", got)
	}
}

func TestTabsRemove(t *testing.T) {
	one, two, three := &filler{ch: '1'}, &filler{ch: '2'}, &filler{ch: '3'}
	tb := NewTabs(one, two, three)
	tb.SetFocus(true)
	tb.Layout(Size{Cols: 12, Rows: 4})

	// Removing the tab being shown selects the one to its right.
	stands, ok := tb.Remove(two)
	if !ok || stands != Widget(tb) {
		t.Fatalf("Remove = %v, %v, want the strip to carry on", stands, ok)
	}
	if got := tb.Children(); len(got) != 2 {
		t.Fatalf("%d children, want 2", len(got))
	}

	tb.Focus(three)
	stands, ok = tb.Remove(three)
	if !ok || stands != Widget(one) {
		t.Fatalf("Remove = %v, %v, want the last tab left", stands, ok)
	}
	stands, ok = tb.Remove(one)
	if !ok || stands != nil {
		t.Fatalf("Remove = %v, %v, want nothing left", stands, ok)
	}
	if _, ok := tb.Remove(&filler{}); ok {
		t.Error("Remove accepted a widget that is not a tab")
	}
}

// TestTabsRemovingTheShownTabSelectsItsNeighbour checks which tab comes
// forward, which is what a user notices.
func TestTabsRemovingTheShownTabSelectsItsNeighbour(t *testing.T) {
	one, two, three := &filler{ch: '1'}, &filler{ch: '2'}, &filler{ch: '3'}
	tb := NewTabs(one, two, three)
	tb.SetFocus(true)
	tb.Focus(two)

	tb.Remove(two)

	if tb.Focused() != Widget(three) {
		t.Error("removing a tab did not select the one to its right")
	}

	// Removing the last one selects the one to its left instead.
	tb.Focus(three)
	tb.Remove(three)
	if tb.Focused() != Widget(one) {
		t.Error("removing the last tab did not select the one before it")
	}
}

// TestTabsRemovingAHiddenTabLeavesTheShownOneAlone checks that closing a
// tab you are not looking at does not move you.
func TestTabsRemovingAHiddenTabLeavesTheShownOneAlone(t *testing.T) {
	one, two, three := &named{title: "1"}, &named{title: "2"}, &named{title: "3"}
	tb := NewTabs(one, two, three)
	tb.SetFocus(true)
	tb.Focus(three)
	before := len(three.focusLog)

	tb.Remove(one)

	if tb.Focused() != Widget(three) {
		t.Error("closing another tab moved which one is shown")
	}
	if got := len(three.focusLog); got != before {
		t.Error("the shown tab was told about focus when nothing about it changed")
	}
}

// TestTabsFocusContract checks the rule every container has to keep: a
// tab is never told the same thing twice, and never told focus left when
// it never had it.
func TestTabsFocusContract(t *testing.T) {
	one, two := &recorder{}, &recorder{}
	tb := NewTabs(one, two)

	// With no focus of its own, nothing reaches the tabs.
	tb.Focus(two)
	if len(one.focus) != 0 || len(two.focus) != 0 {
		t.Fatalf("tabs heard %v and %v before the strip had focus", one.focus, two.focus)
	}

	tb.SetFocus(true)
	tb.SetFocus(true)
	tb.Focus(two)
	tb.Focus(one)
	tb.Focus(one)
	tb.SetFocus(false)
	tb.SetFocus(false)

	for name, got := range map[string][]bool{"first": one.focus, "second": two.focus} {
		if !alternating(got) {
			t.Errorf("the %s tab heard %v, want no value twice in a row", name, got)
		}
	}
}

func TestTabsReplace(t *testing.T) {
	one, two := &recorder{}, &recorder{}
	tb := NewTabs(one, two)
	tb.SetFocus(true)
	tb.Layout(Size{Cols: 12, Rows: 4})

	next := &recorder{}
	if !tb.Replace(one, next) {
		t.Fatal("Replace refused a tab that was there")
	}
	if tb.Children()[0] != Widget(next) {
		t.Error("the tab was not replaced")
	}
	if tb.Focused() != Widget(next) {
		t.Error("the replacement is not the tab being shown")
	}
	if next.size.Rows != 3 {
		t.Errorf("the replacement got %+v, want the body's size", next.size)
	}
	if !alternating(one.focus) {
		t.Errorf("the replaced tab heard %v, want no value twice in a row", one.focus)
	}
	if tb.Replace(&filler{}, &filler{}) {
		t.Error("Replace accepted a widget that is not a tab")
	}
	if tb.Replace(next, nil) {
		t.Error("Replace accepted nil")
	}
}

// TestTabsChildAreaMatchesLayout checks the rule the interface states:
// where a container says a child is has to be where it put it.
func TestTabsChildAreaMatchesLayout(t *testing.T) {
	one, two := &filler{ch: '1'}, &filler{ch: '2'}
	tb := NewTabs(one, two)
	tb.Layout(Size{Cols: 10, Rows: 5})

	area, ok := tb.ChildArea(one)
	if !ok {
		t.Fatal("the tab being shown has no area")
	}
	if area.Size() != one.size {
		t.Errorf("ChildArea says %+v but Layout gave %+v", area.Size(), one.size)
	}
	if area.Y != stripRows {
		t.Errorf("the tab starts at row %d, want it below the strip", area.Y)
	}

	// A tab that is not being shown is not on screen at all.
	if _, ok := tb.ChildArea(two); ok {
		t.Error("a hidden tab reported an area")
	}
	if _, ok := tb.ChildArea(&filler{}); ok {
		t.Error("a widget that is not a tab reported an area")
	}
}

// TestTabsInATreeWorkThroughTheHelpers checks that the generic tree
// code handles a strip as happily as a split.
func TestTabsInATreeWorkThroughTheHelpers(t *testing.T) {
	left := &filler{ch: 'l'}
	one, two := &filler{ch: '1'}, &filler{ch: '2'}
	tb := NewTabs(one, two)
	root := NewSplit(Columns, left, tb)
	root.SetFocus(true)
	whole := Rect{Cols: 21, Rows: 5}
	root.Layout(whole.Size())

	if got := Leaves(root); len(got) != 3 {
		t.Errorf("leaves = %v, want every tab counted", got)
	}
	if got := ParentOf(root, two); got != Container(tb) {
		t.Errorf("parent of a tab = %v, want the strip", got)
	}

	root.Focus(tb)
	tb.Focus(two)
	if got := FocusedLeaf(root); got != Widget(two) {
		t.Errorf("focused leaf = %v, want the tab being shown", got)
	}

	// The tab being shown is below the strip and to the right of the
	// divider.
	area, ok := AreaOf(root, whole, two)
	if !ok {
		t.Fatal("AreaOf did not find the tab")
	}
	if area.X != 11 || area.Y != stripRows {
		t.Errorf("the tab sits at %d,%d, want past the divider and below the strip",
			area.X, area.Y)
	}
	if _, ok := AreaOf(root, whole, one); ok {
		t.Error("a hidden tab reported a place on screen")
	}
}

// TestDetachThroughTabs checks the close path through a strip: it
// carries on, then collapses to its last tab, then goes altogether.
func TestDetachThroughTabs(t *testing.T) {
	one, two, three := &filler{ch: '1'}, &filler{ch: '2'}, &filler{ch: '3'}
	tb := NewTabs(one, two, three)
	beside := &filler{ch: 'b'}
	root := Widget(NewSplit(Columns, tb, beside))

	root, ok := Detach(root, two)
	if !ok {
		t.Fatal("Detach refused a tab")
	}
	if got := Leaves(root); len(got) != 3 {
		t.Errorf("leaves = %v, want the strip carrying on with two tabs", got)
	}

	root, ok = Detach(root, three)
	if !ok {
		t.Fatal("Detach refused the second tab")
	}
	if got := Leaves(root); len(got) != 2 {
		t.Errorf("leaves = %v, want the strip collapsed into its last tab", got)
	}
	if ParentOf(root, one) == Container(tb) {
		t.Error("the strip is still in the tree with one tab in it")
	}

	root, ok = Detach(root, one)
	if !ok {
		t.Fatal("Detach refused the last tab")
	}
	if root != Widget(beside) {
		t.Errorf("root = %v, want the pane beside the strip", root)
	}
}

// TestTabsLabelMeasuresColumnsNotRunes checks a title whose characters
// are not one column wide. Sized by rune count, a CJK label is drawn
// with its end cut off and its clickable area is the wrong width.
func TestTabsLabelMeasuresColumnsNotRunes(t *testing.T) {
	wide, plain := &named{title: "日本"}, &named{title: "ab"}
	tb := NewTabs(wide, plain)
	tb.SetFocus(true)

	g := drawTabs(tb, 14, 3)

	if got := rowOf(g, 0); got != " 日本  ab     " {
		t.Errorf("strip = %q, want the wide title whole and the next label clear of it", got)
	}
	// The second label starts where the first one really ends.
	tb.HandleMouse(input.MouseEvent{Kind: input.MousePress, Button: input.MouseLeft, Col: 7})
	if tb.Focused() != Widget(plain) {
		t.Error("clicking the second label selected something else")
	}
	// And a click inside the wide label still finds the first tab.
	tb.HandleMouse(input.MouseEvent{Kind: input.MouseRelease, Button: input.MouseLeft, Col: 7})
	tb.HandleMouse(input.MouseEvent{Kind: input.MousePress, Button: input.MouseLeft, Col: 3})
	if tb.Focused() != Widget(wide) {
		t.Error("clicking inside the wide label did not select its tab")
	}
}

// TestTabsLabelFitBoundary pins where a label stops fitting, and that a
// short label after one that did not fit is dropped too rather than
// jumping the queue.
func TestTabsLabelFitBoundary(t *testing.T) {
	for _, tc := range []struct {
		cols int
		want string
	}{
		// " first " is exactly seven columns.
		{cols: 6, want: "      "},
		{cols: 7, want: " first "},
	} {
		tb := NewTabs(&named{title: "first"})

		g := drawTabs(tb, tc.cols, 3)

		if got := rowOf(g, 0); got != tc.want {
			t.Errorf("%d columns: strip = %q, want %q", tc.cols, got, tc.want)
		}
	}

	// A label that does not fit stops the ones after it, however short.
	tb := NewTabs(&named{title: "first"}, &named{title: "toolong"}, &named{title: "x"})
	g := drawTabs(tb, 12, 3)
	if got := rowOf(g, 0); got != " first      " {
		t.Errorf("strip = %q, want the run to stop at the first label that did not fit", got)
	}
}

// TestTabsLayoutWithNoRoomTellsNobody checks the guard that stops a
// strip squeezed to nothing telling every shell it has no columns. A
// terminal told that reflows its scrollback, and growing back does not
// undo it.
func TestTabsLayoutWithNoRoomTellsNobody(t *testing.T) {
	one, two := &filler{ch: '1'}, &filler{ch: '2'}
	tb := NewTabs(one, two)
	tb.Layout(Size{Cols: 10, Rows: 4})
	was := one.size

	tb.Layout(Size{Cols: 0, Rows: 0})

	if one.size != was || two.size != was {
		t.Errorf("tabs resized to %+v and %+v, want them left at %+v",
			one.size, two.size, was)
	}
}

// TestTabsFocusOnTheShownTabIsSilent checks the guard that stops a click
// on the tab already showing blinking focus off and on. Focus runs on
// every container in the chain every time it moves.
func TestTabsFocusOnTheShownTabIsSilent(t *testing.T) {
	one, two := &recorder{}, &recorder{}
	tb := NewTabs(one, two)
	tb.SetFocus(true)
	before := len(one.focus)

	tb.Focus(one)
	tb.Focus(one)

	if got := len(one.focus); got != before {
		t.Errorf("the shown tab heard %d more times, want none", got-before)
	}
	if len(two.focus) != 0 {
		t.Errorf("the hidden tab heard %v", two.focus)
	}
}

// TestTabsFocusRefusesAStranger checks that only a tab of this strip can
// be brought forward.
func TestTabsFocusRefusesAStranger(t *testing.T) {
	one := &filler{ch: '1'}
	tb := NewTabs(one)
	tb.SetFocus(true)

	if tb.Focus(&filler{ch: 'x'}) {
		t.Error("Focus accepted a widget that is not a tab")
	}
	if tb.Focused() != Widget(one) {
		t.Error("refusing a stranger changed which tab is shown")
	}
}

// TestTabsChildAreaWithNoRoom checks that a strip too small to show
// anything reports no area, which is what tells the rest of the toolkit
// the tab is not on screen.
func TestTabsChildAreaWithNoRoom(t *testing.T) {
	one := &filler{ch: '1'}
	tb := NewTabs(one)
	tb.Layout(Size{})

	if _, ok := tb.ChildArea(one); ok {
		t.Error("a strip with no room reported its tab as shown")
	}
}

// TestTabsReplaceRefusesADuplicate checks the guard against holding one
// widget twice, which would give Remove two answers.
func TestTabsReplaceRefusesADuplicate(t *testing.T) {
	one, two := &filler{ch: '1'}, &filler{ch: '2'}
	tb := NewTabs(one, two)

	if tb.Replace(one, two) {
		t.Error("Replace put the same widget in twice")
	}
	if got := tb.Children(); got[0] != Widget(one) || got[1] != Widget(two) {
		t.Errorf("tabs = %v, want them untouched", got)
	}
	// Replacing a tab with itself is harmless and stays allowed.
	if !tb.Replace(one, one) {
		t.Error("Replace refused a tab with itself")
	}
}

// TestTabsChildrenIsACopy checks that a caller cannot swap a tab out
// from under the strip.
func TestTabsChildrenIsACopy(t *testing.T) {
	one, two := &filler{ch: '1'}, &filler{ch: '2'}
	tb := NewTabs(one, two)

	got := tb.Children()
	got[0] = &filler{ch: 'x'}

	if again := tb.Children(); again[0] != Widget(one) {
		t.Error("changing the returned slice changed the strip")
	}
}

// TestTabsRemoveClearsTheSlot checks that the array behind the slice
// does not keep the tab it just gave up, along with its scrollback.
func TestTabsRemoveClearsTheSlot(t *testing.T) {
	one, two, three := &filler{ch: '1'}, &filler{ch: '2'}, &filler{ch: '3'}
	tb := NewTabs(one, two, three)

	tb.Remove(three)

	// Reach past the length into the array the slice still owns.
	tail := tb.kids[:cap(tb.kids)]
	if tail[2] != nil {
		t.Error("the removed tab is still reachable through the slice's own array")
	}
}

// TestTabsNilChildrenAreIgnored checks the exported constructor against
// a caller passing nothing useful.
func TestTabsNilChildrenAreIgnored(t *testing.T) {
	one := &filler{ch: '1'}
	tb := NewTabs(nil, one, nil, one)

	if got := tb.Children(); len(got) != 1 || got[0] != Widget(one) {
		t.Errorf("tabs = %v, want just the one real widget", got)
	}
	// None of these may panic.
	tb.Layout(Size{Cols: 8, Rows: 3})
	tb.Draw(grid.New(8, 3, fg, bg).View())
}

// TestTabsPressOnALabelKeepsTheGesture checks that a drag begun on a
// label and released over the tab below does not hand that tab a release
// for a press it never saw. A program with mouse tracking on would act
// on it.
func TestTabsPressOnALabelKeepsTheGesture(t *testing.T) {
	one, two := &named{title: "one"}, &named{title: "two"}
	tb := NewTabs(one, two)
	r := rootOver(tb, 14, 4)

	r.HandleMouse(pressAt(7, 0))
	r.HandleMouse(moveTo(3, 2))
	r.HandleMouse(releaseAt(3, 2))

	if tb.Focused() != Widget(two) {
		t.Error("the press on the label did not select its tab")
	}
	for _, ev := range two.seen {
		if ev.Kind == input.MouseRelease {
			t.Error("the tab got a release for a press that landed on a label")
		}
	}
	// And the strip lets go afterwards, so the next press works.
	r.HandleMouse(pressAt(3, 2))
	if len(two.seen) == 0 {
		t.Error("the tab never saw the press that came after the drag")
	}
}

// TestTabsCancelGestureAfterADialogOpens checks the case that leaves a
// strip stuck. A dialog opening ends the gesture without a release, and
// a strip still holding it would swallow every click after that: the tab
// would never be clickable again.
func TestTabsCancelGestureAfterADialogOpens(t *testing.T) {
	one, two := &named{title: "one"}, &named{title: "two"}
	tb := NewTabs(one, two)
	r := rootOver(tb, 14, 4)

	r.HandleMouse(pressAt(7, 0))
	if !tb.stripHeld {
		t.Fatal("the press on the label did not start a gesture")
	}

	// A dialog opens between the press and the release.
	r.PushModal(&fake{name: "dialog"})

	if tb.stripHeld {
		t.Error("the strip is still holding a gesture whose release will never come")
	}
	// And the strip works again afterwards.
	r.PopModal()
	r.HandleMouse(pressAt(3, 0))
	if tb.Focused() != Widget(one) {
		t.Error("a click on the first label did nothing: the strip is stuck")
	}
}

// TestTabsCancelGestureWhenTheStripLeavesTheScreen checks the other way
// a release goes missing: the widget holding it is no longer drawn.
func TestTabsCancelGestureWhenTheStripLeavesTheScreen(t *testing.T) {
	one, two := &named{title: "one"}, &named{title: "two"}
	tb := NewTabs(one, two)
	beside := &filler{ch: 'b'}
	// The strip is the second half, so narrowing the window squeezes it
	// out while leaving the tree alone.
	outer := NewSplit(Columns, beside, tb)
	r := rootOver(outer, 30, 4)

	r.HandleMouse(pressAt(22, 0))
	if !tb.stripHeld {
		t.Fatal("the press on the label did not start a gesture")
	}

	// The window narrows until the strip has nowhere to be drawn.
	r.Layout(Rect{Cols: 2, Rows: 4})
	r.HandleMouse(moveTo(1, 2))

	if tb.stripHeld {
		t.Error("the strip is still holding a gesture it can never finish")
	}
}

// TestTabsStripGestureIsButtonAware checks that tapping another button
// mid-drag does not end the one that started on the label.
func TestTabsStripGestureIsButtonAware(t *testing.T) {
	one, two := &named{title: "one"}, &named{title: "two"}
	tb := NewTabs(one, two)
	r := rootOver(tb, 14, 4)

	r.HandleMouse(pressAt(7, 0))
	r.HandleMouse(input.MouseEvent{
		Kind: input.MouseRelease, Button: input.MouseMiddle, Col: 3, Row: 2,
	})
	r.HandleMouse(releaseAt(3, 2))

	for _, ev := range two.seen {
		if ev.Kind == input.MouseRelease {
			t.Error("the tab got a release for a press that landed on a label")
		}
	}
	if tb.stripHeld {
		t.Error("the strip did not let go on its own button's release")
	}
}
