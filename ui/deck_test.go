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

func drawDeck(d *Deck, cols, rows int) *grid.Grid {
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

	g := drawDeck(d, 12, 4)

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

// A deck one row tall gives the pane in front that row. The sizes a
// layout passes through while it settles used to be covered by the test
// that measured the strip.
func TestDeckWithOneRowGivesItToThePaneInFront(t *testing.T) {
	for _, rows := range []int{0, 1, 2, 5} {
		one, two := &filler{ch: '1'}, &filler{ch: '2'}
		d := NewDeck(one, two)
		d.Layout(Size{Cols: 8, Rows: rows})

		want := Size{Cols: 8, Rows: rows}
		if rows == 0 {
			// Nowhere to draw, so nobody is told anything.
			want = Size{}
		}
		if got := one.size; got != want {
			t.Errorf("%d rows: the pane in front has %+v, want %+v", rows, got, want)
		}
		if got := two.size; got != want {
			t.Errorf("%d rows: the hidden pane has %+v, want %+v", rows, got, want)
		}
	}
}

func TestDeckShowsOneChildAtATime(t *testing.T) {
	one, two := &filler{ch: '1'}, &filler{ch: '2'}
	tb := NewDeck(one, two)

	g := drawDeck(tb, 8, 3)

	for y := 0; y < 3; y++ {
		if got := rowOf(g, y); got != "11111111" {
			t.Errorf("row %d = %q, want the pane in front", y, got)
		}
	}
	// The hidden pane is not drawn, but it is still told how much room it
	// has: a program running in it is writing output sized to whatever
	// it was last told, and bringing the pane forward cannot undo that.
	if got := two.size; got != (Size{Cols: 8, Rows: 3}) {
		t.Errorf("the hidden pane has %+v, want the body's size", got)
	}
	if two.drawn {
		t.Error("the hidden pane was drawn")
	}
}

func TestDeckNoColumnsGivesNothing(t *testing.T) {
	kid := &filler{ch: 'k'}
	tb := NewDeck(kid)

	tb.Layout(Size{Cols: 0, Rows: 5})

	if !kid.size.Empty() {
		t.Errorf("the pane got %+v, want nothing", kid.size)
	}
}

func TestDeckMouseReachesThePaneInFront(t *testing.T) {
	one, two := &filler{ch: '1'}, &filler{ch: '2'}
	tb := NewDeck(one, two)
	tb.Layout(Size{Cols: 8, Rows: 4})

	tb.HandleMouse(input.MouseEvent{
		Kind: input.MousePress, Button: input.MouseLeft, Col: 3, Row: 2,
	})

	if len(two.seen) != 0 {
		t.Error("the hidden pane saw a mouse event")
	}
	if len(one.seen) != 1 {
		t.Fatalf("the pane in front saw %d events, want 1", len(one.seen))
	}
	// The pane fills the whole thing, so its own coordinates are the
	// ones the event arrived with.
	if got := one.seen[0]; got.Col != 3 || got.Row != 2 {
		t.Errorf("the pane was told %d,%d, want 3,2", got.Col, got.Row)
	}
}

func TestDeckKeysGoToThePaneInFront(t *testing.T) {
	one := &filler{ch: '1', takes: input.KeyQ}
	two := &filler{ch: '2', takes: input.KeyQ}
	tb := NewDeck(one, two)
	tb.SetFocus(true)

	handled, err := tb.HandleKey(press(input.KeyQ, 0))

	if err != nil {
		t.Fatalf("HandleKey: %v", err)
	}
	if !handled || len(two.keys) != 0 {
		t.Errorf("handled = %v, hidden pane saw %v", handled, two.keys)
	}
}

func TestDeckAdd(t *testing.T) {
	one := &filler{ch: '1'}
	tb := NewDeck(one)
	tb.SetFocus(true)
	tb.Layout(Size{Cols: 8, Rows: 4})

	two := &filler{ch: '2'}
	tb.Add(two)

	if got := tb.Children(); len(got) != 2 || got[1] != Widget(two) {
		t.Fatalf("children = %v, want the new pane at the end", got)
	}
	if tb.Focused() != Widget(two) {
		t.Error("the new pane was not shown")
	}
	if two.size.Rows != 4 {
		t.Errorf("the new pane got %+v, want the body's size", two.size)
	}
	// Adding the same widget twice must not put it in twice.
	tb.Add(two)
	tb.Add(nil)
	if got := len(tb.Children()); got != 2 {
		t.Errorf("%d children, want 2", got)
	}
}

func TestDeckRemove(t *testing.T) {
	one, two, three := &filler{ch: '1'}, &filler{ch: '2'}, &filler{ch: '3'}
	tb := NewDeck(one, two, three)
	tb.SetFocus(true)
	tb.Layout(Size{Cols: 12, Rows: 4})

	// Removing the pane in front selects the one to its right.
	stands, ok := tb.Remove(two)
	if !ok || stands != Widget(tb) {
		t.Fatalf("Remove = %v, %v, want the deck to carry on", stands, ok)
	}
	if got := tb.Children(); len(got) != 2 {
		t.Fatalf("%d children, want 2", len(got))
	}

	tb.Focus(three)
	stands, ok = tb.Remove(three)
	if !ok || stands != Widget(one) {
		t.Fatalf("Remove = %v, %v, want the last pane left", stands, ok)
	}
	stands, ok = tb.Remove(one)
	if !ok || stands != nil {
		t.Fatalf("Remove = %v, %v, want nothing left", stands, ok)
	}
	if _, ok := tb.Remove(&filler{}); ok {
		t.Error("Remove accepted a widget that is not a pane")
	}
}

// TestDeckRemovingTheOneInFrontSelectsItsNeighbour checks which pane comes
// forward, which is what a user notices.
func TestDeckRemovingTheOneInFrontSelectsItsNeighbour(t *testing.T) {
	one, two, three := &filler{ch: '1'}, &filler{ch: '2'}, &filler{ch: '3'}
	tb := NewDeck(one, two, three)
	tb.SetFocus(true)
	tb.Focus(two)

	tb.Remove(two)

	if tb.Focused() != Widget(three) {
		t.Error("removing a pane did not select the one to its right")
	}

	// Removing the last one selects the one to its left instead.
	tb.Focus(three)
	tb.Remove(three)
	if tb.Focused() != Widget(one) {
		t.Error("removing the last pane did not select the one before it")
	}
}

// TestDeckRemovingAHiddenPaneLeavesTheOneInFrontAlone checks that closing a
// pane you are not looking at does not move you.
func TestDeckRemovingAHiddenPaneLeavesTheOneInFrontAlone(t *testing.T) {
	one, two, three := &named{title: "1"}, &named{title: "2"}, &named{title: "3"}
	tb := NewDeck(one, two, three)
	tb.SetFocus(true)
	tb.Focus(three)
	before := len(three.focusLog)

	tb.Remove(one)

	if tb.Focused() != Widget(three) {
		t.Error("closing another pane moved which one is shown")
	}
	if got := len(three.focusLog); got != before {
		t.Error("the pane in front was told about focus when nothing about it changed")
	}
}

// TestDeckFocusContract checks the rule every container has to keep: a
// pane is never told the same thing twice, and never told focus left when
// it never had it.
func TestDeckFocusContract(t *testing.T) {
	one, two := &recorder{}, &recorder{}
	tb := NewDeck(one, two)

	// With no focus of its own, nothing reaches the panes.
	tb.Focus(two)
	if len(one.focus) != 0 || len(two.focus) != 0 {
		t.Fatalf("panes heard %v and %v before the deck had focus", one.focus, two.focus)
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
			t.Errorf("the %s pane heard %v, want no value twice in a row", name, got)
		}
	}
}

func TestDeckReplace(t *testing.T) {
	one, two := &recorder{}, &recorder{}
	tb := NewDeck(one, two)
	tb.SetFocus(true)
	tb.Layout(Size{Cols: 12, Rows: 4})

	next := &recorder{}
	if !tb.Replace(one, next) {
		t.Fatal("Replace refused a pane that was there")
	}
	if tb.Children()[0] != Widget(next) {
		t.Error("the pane was not replaced")
	}
	if tb.Focused() != Widget(next) {
		t.Error("the replacement is not the pane in front")
	}
	if next.size.Rows != 4 {
		t.Errorf("the replacement got %+v, want the body's size", next.size)
	}
	if !alternating(one.focus) {
		t.Errorf("the replaced pane heard %v, want no value twice in a row", one.focus)
	}
	if tb.Replace(&filler{}, &filler{}) {
		t.Error("Replace accepted a widget that is not a pane")
	}
	if tb.Replace(next, nil) {
		t.Error("Replace accepted nil")
	}
}

// TestDeckChildAreaMatchesLayout checks the rule the interface states:
// where a container says a child is has to be where it put it.
func TestDeckChildAreaMatchesLayout(t *testing.T) {
	one, two := &filler{ch: '1'}, &filler{ch: '2'}
	tb := NewDeck(one, two)
	tb.Layout(Size{Cols: 10, Rows: 5})

	area, ok := tb.ChildArea(one)
	if !ok {
		t.Fatal("the pane in front has no area")
	}
	if area.Size() != one.size {
		t.Errorf("ChildArea says %+v but Layout gave %+v", area.Size(), one.size)
	}
	if area.Y != 0 {
		t.Errorf("the pane starts at row %d, want the top", area.Y)
	}

	// A pane that is not being shown is not on screen at all.
	if _, ok := tb.ChildArea(two); ok {
		t.Error("a hidden pane reported an area")
	}
	if _, ok := tb.ChildArea(&filler{}); ok {
		t.Error("a widget that is not a pane reported an area")
	}
}

// TestDeckInATreeWorkThroughTheHelpers checks that the generic tree
// code handles a strip as happily as a split.
func TestDeckInATreeWorkThroughTheHelpers(t *testing.T) {
	left := &filler{ch: 'l'}
	one, two := &filler{ch: '1'}, &filler{ch: '2'}
	tb := NewDeck(one, two)
	root := NewSplit(Columns, left, tb)
	root.SetFocus(true)
	whole := Rect{Cols: 21, Rows: 5}
	root.Layout(whole.Size())

	if got := Leaves(root); len(got) != 3 {
		t.Errorf("leaves = %v, want every pane counted", got)
	}
	if got := ParentOf(root, two); got != Container(tb) {
		t.Errorf("parent of a pane = %v, want the strip", got)
	}

	root.Focus(tb)
	tb.Focus(two)
	if got := FocusedLeaf(root); got != Widget(two) {
		t.Errorf("focused leaf = %v, want the pane in front", got)
	}

	// The pane being shown is below the strip and to the right of the
	// divider.
	area, ok := AreaOf(root, whole, two)
	if !ok {
		t.Fatal("AreaOf did not find the pane")
	}
	if area.X != 11 || area.Y != 0 {
		t.Errorf("the pane sits at %d,%d, want past the divider and below the strip",
			area.X, area.Y)
	}
	if _, ok := AreaOf(root, whole, one); ok {
		t.Error("a hidden pane reported a place on screen")
	}
}

// TestDetachThroughADeck checks the close path through a strip: it
// carries on, then collapses to its last pane, then goes altogether.
func TestDetachThroughADeck(t *testing.T) {
	one, two, three := &filler{ch: '1'}, &filler{ch: '2'}, &filler{ch: '3'}
	tb := NewDeck(one, two, three)
	beside := &filler{ch: 'b'}
	root := Widget(NewSplit(Columns, tb, beside))

	root, ok := Detach(root, two)
	if !ok {
		t.Fatal("Detach refused a pane")
	}
	if got := Leaves(root); len(got) != 3 {
		t.Errorf("leaves = %v, want the strip carrying on with two panes", got)
	}

	root, ok = Detach(root, three)
	if !ok {
		t.Fatal("Detach refused the second pane")
	}
	if got := Leaves(root); len(got) != 2 {
		t.Errorf("leaves = %v, want the strip collapsed into its last pane", got)
	}
	if ParentOf(root, one) == Container(tb) {
		t.Error("the strip is still in the tree with one pane in it")
	}

	root, ok = Detach(root, one)
	if !ok {
		t.Fatal("Detach refused the last pane")
	}
	if root != Widget(beside) {
		t.Errorf("root = %v, want the pane beside the strip", root)
	}
}

// TestDeckLayoutWithNoRoomTellsNobody checks the guard that stops a
// strip squeezed to nothing telling every shell it has no columns. A
// terminal told that reflows its scrollback, and growing back does not
// undo it.
func TestDeckLayoutWithNoRoomTellsNobody(t *testing.T) {
	one, two := &filler{ch: '1'}, &filler{ch: '2'}
	tb := NewDeck(one, two)
	tb.Layout(Size{Cols: 10, Rows: 4})
	was := one.size

	tb.Layout(Size{Cols: 0, Rows: 0})

	if one.size != was || two.size != was {
		t.Errorf("panes resized to %+v and %+v, want them left at %+v",
			one.size, two.size, was)
	}
}

// TestDeckFocusOnThePaneInFrontIsSilent checks the guard that stops a click
// on the pane already showing blinking focus off and on. Focus runs on
// every container in the chain every time it moves.
func TestDeckFocusOnThePaneInFrontIsSilent(t *testing.T) {
	one, two := &recorder{}, &recorder{}
	tb := NewDeck(one, two)
	tb.SetFocus(true)
	before := len(one.focus)

	tb.Focus(one)
	tb.Focus(one)

	if got := len(one.focus); got != before {
		t.Errorf("the pane in front heard %d more times, want none", got-before)
	}
	if len(two.focus) != 0 {
		t.Errorf("the hidden pane heard %v", two.focus)
	}
}

// TestDeckFocusRefusesAStranger checks that only a pane of this strip can
// be brought forward.
func TestDeckFocusRefusesAStranger(t *testing.T) {
	one := &filler{ch: '1'}
	tb := NewDeck(one)
	tb.SetFocus(true)

	if tb.Focus(&filler{ch: 'x'}) {
		t.Error("Focus accepted a widget that is not a pane")
	}
	if tb.Focused() != Widget(one) {
		t.Error("refusing a stranger changed which pane is shown")
	}
}

// TestDeckChildAreaWithNoRoom checks that a strip too small to show
// anything reports no area, which is what tells the rest of the toolkit
// the pane is not on screen.
func TestDeckChildAreaWithNoRoom(t *testing.T) {
	one := &filler{ch: '1'}
	tb := NewDeck(one)
	tb.Layout(Size{})

	if _, ok := tb.ChildArea(one); ok {
		t.Error("a strip with no room reported its pane as shown")
	}
}

// TestDeckReplaceRefusesADuplicate checks the guard against holding one
// widget twice, which would give Remove two answers.
func TestDeckReplaceRefusesADuplicate(t *testing.T) {
	one, two := &filler{ch: '1'}, &filler{ch: '2'}
	tb := NewDeck(one, two)

	if tb.Replace(one, two) {
		t.Error("Replace put the same widget in twice")
	}
	if got := tb.Children(); got[0] != Widget(one) || got[1] != Widget(two) {
		t.Errorf("panes = %v, want them untouched", got)
	}
	// Replacing a pane with itself is harmless and stays allowed.
	if !tb.Replace(one, one) {
		t.Error("Replace refused a pane with itself")
	}
}

// TestDeckChildrenIsACopy checks that a caller cannot swap a pane out
// from under the strip.
func TestDeckChildrenIsACopy(t *testing.T) {
	one, two := &filler{ch: '1'}, &filler{ch: '2'}
	tb := NewDeck(one, two)

	got := tb.Children()
	got[0] = &filler{ch: 'x'}

	if again := tb.Children(); again[0] != Widget(one) {
		t.Error("changing the returned slice changed the strip")
	}
}

// TestDeckRemoveClearsTheSlot checks that the array behind the slice
// does not keep the pane it just gave up, along with its scrollback.
func TestDeckRemoveClearsTheSlot(t *testing.T) {
	one, two, three := &filler{ch: '1'}, &filler{ch: '2'}, &filler{ch: '3'}
	tb := NewDeck(one, two, three)

	tb.Remove(three)

	// Reach past the length into the array the slice still owns.
	tail := tb.kids[:cap(tb.kids)]
	if tail[2] != nil {
		t.Error("the removed pane is still reachable through the slice's own array")
	}
}

// TestDeckNilChildrenAreIgnored checks the exported constructor against
// a caller passing nothing useful.
func TestDeckNilChildrenAreIgnored(t *testing.T) {
	one := &filler{ch: '1'}
	tb := NewDeck(nil, one, nil, one)

	if got := tb.Children(); len(got) != 1 || got[0] != Widget(one) {
		t.Errorf("panes = %v, want just the one real widget", got)
	}
	// None of these may panic.
	tb.Layout(Size{Cols: 8, Rows: 3})
	tb.Draw(grid.New(8, 3, fg, bg).View())
}

// A strip with Keep set stays where it is, whether it is down to one pane
// or to none.
//
// It is what a window holding all its panes in one place needs: a strip
// that stood aside for its last pane would leave the window with nowhere
// to put the next one.
func TestDeckKeepStaysInTheTree(t *testing.T) {
	one, two := &filler{ch: '1'}, &filler{ch: '2'}
	tb := NewDeck(one, two)
	tb.Keep = true
	tb.SetFocus(true)
	tb.Layout(Size{Cols: 12, Rows: 4})

	stands, ok := tb.Remove(two)
	if !ok || stands != Widget(tb) {
		t.Fatalf("with two panes Remove = %v, %v, want the strip", stands, ok)
	}
	stands, ok = tb.Remove(one)
	if !ok || stands != Widget(tb) {
		t.Fatalf("with one pane left Remove = %v, %v, want the strip", stands, ok)
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
		t.Fatalf("the strip is showing %v after a pane went back in it", got)
	}
}

// Detach leaves a kept strip alone, which is what puts the rule to work:
// the tree surgery is what would otherwise replace it.
func TestDetachLeavesAKeptDeckInPlace(t *testing.T) {
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
