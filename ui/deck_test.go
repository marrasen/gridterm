package ui

import (
	"image/color"
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

func drawTabs(d *Deck, cols, rows int) *grid.Grid {
	g := grid.New(cols, rows, fg, bg)
	d.Layout(Size{Cols: cols, Rows: rows})
	d.Draw(g.View())
	return g
}

// A deck draws nothing of its own: the pane in front gets every row and
// every column. A strip of labels used to take the top row.
func TestDeckDrawsNothingOfItsOwn(t *testing.T) {
	one, two := &named{filler: filler{ch: '1'}, title: "one"}, &named{title: "two"}
	d := NewDeck(one, two)

	g := drawTabs(d, 12, 4)

	for y := 0; y < 4; y++ {
		if got := rowOf(g, y); got != "111111111111" {
			t.Errorf("row %d = %q, want the pane in front", y, got)
		}
	}
	area, ok := d.ChildArea(one)
	if !ok {
		t.Fatal("the pane in front has no area")
	}
	if area != (Rect{Cols: 12, Rows: 4}) {
		t.Errorf("the pane in front has %+v, want all of it", area)
	}
}

func TestTabsShowOneChildAtATime(t *testing.T) {
	one, two := &filler{ch: '1'}, &filler{ch: '2'}
	tb := NewDeck(one, two)

	g := drawTabs(tb, 8, 3)

	for y := 0; y < 3; y++ {
		if got := rowOf(g, y); got != "11111111" {
			t.Errorf("row %d = %q, want the first tab", y, got)
		}
	}
	// The hidden tab is not drawn, but it is still told how much room it
	// has: a program running in it is writing output sized to whatever
	// it was last told, and bringing the tab forward cannot undo that.
	if got := two.size; got != (Size{Cols: 8, Rows: 3}) {
		t.Errorf("the hidden tab has %+v, want the body's size", got)
	}
	if two.drawn {
		t.Error("the hidden tab was drawn")
	}
}

func TestTabsNoColumnsGivesNothing(t *testing.T) {
	kid := &filler{ch: 'k'}
	tb := NewDeck(kid)

	tb.Layout(Size{Cols: 0, Rows: 5})

	if !kid.size.Empty() {
		t.Errorf("the tab got %+v, want nothing", kid.size)
	}
}

func TestTabsMouseReachesTheTabBeingShown(t *testing.T) {
	one, two := &filler{ch: '1'}, &filler{ch: '2'}
	tb := NewDeck(one, two)
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
	// The tab fills the whole thing, so its own coordinates are the
	// ones the event arrived with.
	if got := one.seen[0]; got.Col != 3 || got.Row != 2 {
		t.Errorf("the tab was told %d,%d, want 3,2", got.Col, got.Row)
	}
}

func TestTabsKeysGoToTheTabBeingShown(t *testing.T) {
	one := &filler{ch: '1', takes: input.KeyQ}
	two := &filler{ch: '2', takes: input.KeyQ}
	tb := NewDeck(one, two)
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
	tb := NewDeck(one)
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
	if two.size.Rows != 4 {
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
	tb := NewDeck(one, two, three)
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
	tb := NewDeck(one, two, three)
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
	tb := NewDeck(one, two, three)
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
	tb := NewDeck(one, two)

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
	tb := NewDeck(one, two)
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
	if next.size.Rows != 4 {
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
	tb := NewDeck(one, two)
	tb.Layout(Size{Cols: 10, Rows: 5})

	area, ok := tb.ChildArea(one)
	if !ok {
		t.Fatal("the tab being shown has no area")
	}
	if area.Size() != one.size {
		t.Errorf("ChildArea says %+v but Layout gave %+v", area.Size(), one.size)
	}
	if area.Y != 0 {
		t.Errorf("the tab starts at row %d, want the top", area.Y)
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
	tb := NewDeck(one, two)
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
	if area.X != 11 || area.Y != 0 {
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
	tb := NewDeck(one, two, three)
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

// TestTabsLayoutWithNoRoomTellsNobody checks the guard that stops a
// strip squeezed to nothing telling every shell it has no columns. A
// terminal told that reflows its scrollback, and growing back does not
// undo it.
func TestTabsLayoutWithNoRoomTellsNobody(t *testing.T) {
	one, two := &filler{ch: '1'}, &filler{ch: '2'}
	tb := NewDeck(one, two)
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
	tb := NewDeck(one, two)
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
	tb := NewDeck(one)
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
	tb := NewDeck(one)
	tb.Layout(Size{})

	if _, ok := tb.ChildArea(one); ok {
		t.Error("a strip with no room reported its tab as shown")
	}
}

// TestTabsReplaceRefusesADuplicate checks the guard against holding one
// widget twice, which would give Remove two answers.
func TestTabsReplaceRefusesADuplicate(t *testing.T) {
	one, two := &filler{ch: '1'}, &filler{ch: '2'}
	tb := NewDeck(one, two)

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
	tb := NewDeck(one, two)

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
	tb := NewDeck(one, two, three)

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
	tb := NewDeck(nil, one, nil, one)

	if got := tb.Children(); len(got) != 1 || got[0] != Widget(one) {
		t.Errorf("tabs = %v, want just the one real widget", got)
	}
	// None of these may panic.
	tb.Layout(Size{Cols: 8, Rows: 3})
	tb.Draw(grid.New(8, 3, fg, bg).View())
}

// A strip with Keep set stays where it is, whether it is down to one tab
// or to none.
//
// It is what a window holding all its panes in one place needs: a strip
// that stood aside for its last tab would leave the window with nowhere
// to put the next one.
func TestTabsKeepStaysInTheTree(t *testing.T) {
	one, two := &filler{ch: '1'}, &filler{ch: '2'}
	tb := NewDeck(one, two)
	tb.Keep = true
	tb.SetFocus(true)
	tb.Layout(Size{Cols: 12, Rows: 4})

	stands, ok := tb.Remove(two)
	if !ok || stands != Widget(tb) {
		t.Fatalf("with two tabs Remove = %v, %v, want the strip", stands, ok)
	}
	stands, ok = tb.Remove(one)
	if !ok || stands != Widget(tb) {
		t.Fatalf("with one tab left Remove = %v, %v, want the strip", stands, ok)
	}
	if got := tb.Children(); len(got) != 0 {
		t.Fatalf("%d children left, want none", len(got))
	}
	// And it is still a widget: an empty stage is drawn and laid out
	// like any other, until the next pane goes in it.
	tb.Layout(Size{Cols: 12, Rows: 4})
	g := grid.New(12, 4, color.RGBA{}, color.RGBA{})
	tb.Draw(g.View())
	tb.SetFocus(false)
	if got := tb.Focused(); got != nil {
		t.Fatalf("an empty strip is showing %v", got)
	}

	tb.Add(one)
	if got := tb.Focused(); got != Widget(one) {
		t.Fatalf("the strip is showing %v after a tab went back in it", got)
	}
}

// Detach leaves a kept strip alone, which is what puts the rule to work:
// the tree surgery is what would otherwise replace it.
func TestDetachLeavesAKeptStripInPlace(t *testing.T) {
	one, two := &filler{ch: '1'}, &filler{ch: '2'}
	tb := NewDeck(one, two)
	tb.Keep = true
	tb.Layout(Size{Cols: 12, Rows: 4})

	root, ok := Detach(Widget(tb), two)
	if !ok {
		t.Fatal("Detach refused")
	}
	if root != Widget(tb) {
		t.Fatalf("the root is %T, want the strip", root)
	}
	root, ok = Detach(Widget(tb), one)
	if !ok || root != Widget(tb) {
		t.Fatalf("Detach = %v, %v, want the strip left standing", root, ok)
	}
}
