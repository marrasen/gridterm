package ui

import (
	"image/color"
	"strings"
	"testing"

	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/input"
)

func newTestDock(t *testing.T, width, cols, rows int) (*Dock, *fake, *fake) {
	t.Helper()
	panel, rest := &fake{name: "panel"}, &fake{name: "rest"}
	d := NewDock(width, panel, rest)
	d.DividerFG, d.DividerBG = fg, bg
	d.Layout(Size{Cols: cols, Rows: rows})
	return d, panel, rest
}

// A panel keeps the width it was set to whatever the window does, which
// is the whole reason Split will not do.
func TestDockKeepsItsWidthWhenTheWindowResizes(t *testing.T) {
	d, panel, rest := newTestDock(t, 24, 100, 30)
	if panel.size.Cols != 24 {
		t.Fatalf("the panel is %d wide, want 24", panel.size.Cols)
	}
	if want := 100 - 24 - 1; rest.size.Cols != want {
		t.Fatalf("the rest is %d wide, want %d", rest.size.Cols, want)
	}

	d.Layout(Size{Cols: 160, Rows: 30})
	if panel.size.Cols != 24 {
		t.Fatalf("after a resize the panel is %d wide, want 24 still", panel.size.Cols)
	}
	if want := 160 - 24 - 1; rest.size.Cols != want {
		t.Fatalf("the rest is %d wide, want %d", rest.size.Cols, want)
	}
}

// A window with no room for both gives it all to the rest: a terminal
// squeezed to nothing beside a panel is worse than no panel.
func TestDockGivesUpWhenThereIsNoRoomForBoth(t *testing.T) {
	d, _, rest := newTestDock(t, 24, 30, 10)
	if got := d.Panel(); got == nil {
		t.Fatal("the panel was thrown away rather than hidden")
	}
	if _, shown := d.ChildArea(d.Panel()); shown {
		t.Fatal("the panel is drawn in a window with no room for it")
	}
	if rest.size.Cols != 30 {
		t.Fatalf("the rest is %d wide, want the whole window", rest.size.Cols)
	}

	// And it comes back when there is room again.
	d.Layout(Size{Cols: 100, Rows: 10})
	if _, shown := d.ChildArea(d.Panel()); !shown {
		t.Fatal("the panel did not come back when there was room")
	}
}

func TestDockCollapseHidesThePanelWithoutLosingIt(t *testing.T) {
	d, panel, rest := newTestDock(t, 24, 100, 30)
	d.SetFocus(true)
	d.Focus(panel)
	if !panel.focus {
		t.Fatal("the panel did not take the keys")
	}

	d.ShowPanel(false)
	if _, shown := d.ChildArea(panel); shown {
		t.Fatal("a hidden panel is still drawn")
	}
	if rest.size.Cols != 100 {
		t.Fatalf("the rest is %d wide with the panel hidden, want the whole window", rest.size.Cols)
	}
	// A hidden panel has to be told it lost the keys, or it goes on
	// drawing a selection and a cursor nobody can see.
	if panel.focus {
		t.Fatal("a hidden panel was not told it lost the keys")
	}
	if !rest.focus {
		t.Fatal("the rest of the window was not given the keys")
	}
	if d.Focused() != rest {
		t.Fatal("focus stayed on a hidden panel")
	}

	d.ShowPanel(true)
	if _, shown := d.ChildArea(panel); !shown {
		t.Fatal("the panel did not come back")
	}
	if d.Panel() != panel {
		t.Fatal("the panel was replaced rather than hidden")
	}
}

func TestDockFocusMovesBetweenTheTwo(t *testing.T) {
	d, panel, rest := newTestDock(t, 24, 100, 30)
	d.SetFocus(true)

	if d.Focused() != rest {
		t.Fatal("a new dock does not start on the rest of the window")
	}
	if !d.Focus(panel) {
		t.Fatal("focus would not move to the panel")
	}
	if !panel.focus || rest.focus {
		t.Fatalf("focus is panel=%v rest=%v", panel.focus, rest.focus)
	}
	if !d.Focus(rest) {
		t.Fatal("focus would not move back")
	}
	if panel.focus || !rest.focus {
		t.Fatalf("focus is panel=%v rest=%v", panel.focus, rest.focus)
	}
	if d.Focus(&fake{name: "stranger"}) {
		t.Fatal("focus moved to something that is not in the dock")
	}
}

// The contract says SetFocus is never called twice with the same value.
func TestDockDoesNotTellAChildTheSameThingTwice(t *testing.T) {
	d, panel, _ := newTestDock(t, 24, 100, 30)
	d.SetFocus(true)
	d.Focus(panel)
	d.Focus(panel)
	d.SetFocus(false)

	for i := 1; i < len(panel.focused); i++ {
		if panel.focused[i] == panel.focused[i-1] {
			t.Fatalf("the panel was told %v twice in a row: %v", panel.focused[i], panel.focused)
		}
	}
}

// A hidden panel cannot take the keys.
func TestDockWillNotFocusAHiddenPanel(t *testing.T) {
	d, panel, rest := newTestDock(t, 24, 100, 30)
	d.SetFocus(true)
	d.ShowPanel(false)
	if d.Focus(panel) {
		t.Fatal("focus moved to a hidden panel")
	}
	if d.Focused() != rest {
		t.Fatal("focus is not on the rest of the window")
	}
}

func TestDockKeysGoToWhicheverHalfHasFocus(t *testing.T) {
	d, panel, rest := newTestDock(t, 24, 100, 30)
	panel.takes, rest.takes = input.KeyA, input.KeyA
	d.SetFocus(true)

	d.HandleKey(press(input.KeyA, 0))
	if len(rest.seen) != 1 || len(panel.seen) != 0 {
		t.Fatalf("the key went to panel=%d rest=%d", len(panel.seen), len(rest.seen))
	}
	d.Focus(panel)
	d.HandleKey(press(input.KeyA, 0))
	if len(panel.seen) != 1 {
		t.Fatalf("the key did not reach the panel")
	}
}

// A click goes to the half it landed on, in that half's own
// coordinates, and takes the keys with it.
func TestDockRoutesTheMouseToWhatWasClicked(t *testing.T) {
	d, panel, rest := newTestDock(t, 24, 100, 30)
	d.SetFocus(true)

	d.HandleMouse(input.MouseEvent{
		Kind: input.MousePress, Button: input.MouseLeft, Col: 5, Row: 7,
	})
	if len(panel.clicks) != 1 || len(rest.clicks) != 0 {
		t.Fatalf("the click went to panel=%d rest=%d", len(panel.clicks), len(rest.clicks))
	}
	if got := panel.clicks[0]; got.Col != 5 || got.Row != 7 {
		t.Errorf("the panel was handed column %d row %d, want 5 and 7", got.Col, got.Row)
	}
	if d.Focused() != Widget(panel) {
		t.Error("clicking the panel did not take the keys")
	}

	area, _ := d.ChildArea(rest)
	d.HandleMouse(input.MouseEvent{
		Kind: input.MousePress, Button: input.MouseLeft, Col: area.X + 3, Row: 7,
	})
	if len(rest.clicks) != 1 {
		t.Fatalf("the click went to rest=%d", len(rest.clicks))
	}
	if got := rest.clicks[0]; got.Col != 3 {
		t.Errorf("the rest was handed column %d, want it in its own coordinates", got.Col)
	}
	if d.Focused() != Widget(rest) {
		t.Error("clicking the rest did not take the keys back")
	}
}

// Dragging the divider is how the panel is resized. Root keeps the
// pointer for whoever took the press, so the whole drag comes back here.
func TestDockDividerCanBeDragged(t *testing.T) {
	d, panel, _ := newTestDock(t, 24, 100, 30)

	took, _ := d.HandleMouse(input.MouseEvent{
		Kind: input.MousePress, Button: input.MouseLeft, Col: 24, Row: 3,
	})
	if !took {
		t.Fatal("a press on the divider was not taken, so no drag can follow")
	}
	d.HandleMouse(input.MouseEvent{Kind: input.MouseMove, Col: 40, Row: 3})
	if d.Width != 40 || panel.size.Cols != 40 {
		t.Fatalf("width = %d and the panel is %d wide, want 40", d.Width, panel.size.Cols)
	}
	d.HandleMouse(input.MouseEvent{Kind: input.MouseRelease, Button: input.MouseLeft, Col: 40, Row: 3})

	// And a move afterwards is nothing to do with the divider.
	d.HandleMouse(input.MouseEvent{Kind: input.MouseMove, Col: 70, Row: 3})
	if d.Width != 40 {
		t.Fatalf("width = %d after the button came up", d.Width)
	}
}

// A drag has to stop somewhere: neither half may be squeezed away.
func TestDockDragStaysWithinWhatTheWindowCanSpare(t *testing.T) {
	d, _, _ := newTestDock(t, 24, 100, 30)
	d.HandleMouse(input.MouseEvent{Kind: input.MousePress, Button: input.MouseLeft, Col: 24, Row: 3})

	d.HandleMouse(input.MouseEvent{Kind: input.MouseMove, Col: -50, Row: 3})
	if d.Width < dockMin {
		t.Fatalf("width = %d, want at least %d", d.Width, dockMin)
	}
	d.HandleMouse(input.MouseEvent{Kind: input.MouseMove, Col: 500, Row: 3})
	if d.Width > 100-dockRest-1 {
		t.Fatalf("width = %d, want room left for the rest of the window", d.Width)
	}
	// Dragged as far as it goes, both halves are still drawn: the
	// clamp and the point where the dock gives up have to meet.
	panel, shown := d.ChildArea(d.Panel())
	if !shown {
		t.Fatal("dragging the divider all the way made the panel vanish")
	}
	if panel.Cols != d.Width {
		t.Fatalf("the panel is %d wide after the drag, want %d", panel.Cols, d.Width)
	}
	if _, shown := d.ChildArea(d.Rest()); !shown {
		t.Fatal("dragging the divider all the way squeezed the rest away")
	}
}

// A drag whose release never comes must not leave the divider stuck to
// the pointer.
func TestDockCancelGestureEndsADrag(t *testing.T) {
	d, _, _ := newTestDock(t, 24, 100, 30)
	d.HandleMouse(input.MouseEvent{Kind: input.MousePress, Button: input.MouseLeft, Col: 24, Row: 3})
	d.CancelGesture()

	d.HandleMouse(input.MouseEvent{Kind: input.MouseMove, Col: 60, Row: 3})
	if d.Width != 24 {
		t.Fatalf("width = %d after the drag was cancelled, want 24", d.Width)
	}
}

func TestDockReplaceAndRemove(t *testing.T) {
	d, panel, rest := newTestDock(t, 24, 100, 30)
	d.SetFocus(true)
	next := &fake{name: "next"}
	if !d.Replace(rest, next) {
		t.Fatal("Replace would not swap the rest")
	}
	// Focus follows: the widget that had the keys has gone.
	if rest.focus || !next.focus {
		t.Errorf("after Replace, focus is old=%v new=%v", rest.focus, next.focus)
	}
	if d.Replace(next, nil) {
		t.Fatal("Replace accepted nothing as a replacement")
	}
	// The same widget in both halves would give Remove two answers.
	if d.Replace(next, panel) {
		t.Fatal("Replace put the same widget in both halves")
	}
	if d.Rest() != next {
		t.Fatal("the rest was not replaced")
	}
	if d.Replace(&fake{}, next) {
		t.Fatal("Replace swapped something that was not there")
	}

	// Losing the panel leaves the dock as its other half, and the keys
	// go with it: the child it held may outlive it.
	d.Focus(panel)
	stands, ok := d.Remove(panel)
	if !ok {
		t.Fatal("Remove would not take the panel out")
	}
	if stands != next {
		t.Fatalf("Remove said %v should stand in its place, want the rest", stands)
	}
	if panel.focus {
		t.Error("the removed panel was not told it lost the keys")
	}
	if !next.focus {
		t.Error("Remove did not hand the keys to what stands in its place")
	}
	if _, ok := d.Remove(&fake{}); ok {
		t.Fatal("Remove took out something that was not there")
	}
}

// Losing the rest of the window leaves a dock that is only a panel,
// which is not worth keeping.
func TestDockRemovingTheRestLeavesThePanel(t *testing.T) {
	d, panel, rest := newTestDock(t, 24, 100, 30)
	stands, ok := d.Remove(rest)
	if !ok {
		t.Fatal("Remove would not take the rest out")
	}
	if stands != panel {
		t.Fatalf("Remove said %v should stand in its place, want the panel", stands)
	}
}

// Where a container says a child is has to be where it drew it, or a
// click is routed to one thing and the user is looking at another.
func TestDockChildAreasMatchWhatIsDrawn(t *testing.T) {
	d, panel, rest := newTestDock(t, 24, 100, 30)
	g := grid.New(100, 30, color.RGBA{}, color.RGBA{})
	d.Draw(g.View())

	panelArea, ok := d.ChildArea(panel)
	if !ok {
		t.Fatal("the panel has no area")
	}
	restArea, ok := d.ChildArea(rest)
	if !ok {
		t.Fatal("the rest has no area")
	}
	if panelArea.X != 0 || panelArea.Cols != 24 {
		t.Errorf("the panel is at %+v", panelArea)
	}
	if restArea.X != 25 {
		t.Errorf("the rest is at %+v, want it past the divider", restArea)
	}
	// fake writes its own name at the top left of whatever view it got.
	// Read by column, not by byte: the divider is three bytes wide.
	row := []rune(rowOf(g, 0))
	if got := string(row[panelArea.X:]); !strings.HasPrefix(got, "panel") {
		t.Errorf("the panel was drawn at %q, not where ChildArea says it is", got)
	}
	if got := string(row[restArea.X:]); !strings.HasPrefix(got, "rest") {
		t.Errorf("the rest was drawn at %q, not where ChildArea says it is", got)
	}
	// The divider is drawn between them and belongs to neither.
	if got := g.At(24, 0).Rune; got != '│' {
		t.Errorf("the divider column holds %q, want a line", got)
	}
	if got, ok := d.ChildArea(nil); ok || got != (Rect{}) {
		t.Errorf("ChildArea(nil) = %+v, %v", got, ok)
	}
}

// A divider with no colour is a gap rather than a line, the way a split
// with no colour is.
func TestDockDividerWithNoColourIsBlank(t *testing.T) {
	d, _, _ := newTestDock(t, 24, 100, 30)
	d.DividerFG = color.RGBA{}
	g := grid.New(100, 30, color.RGBA{}, color.RGBA{})
	d.Draw(g.View())
	if got := g.At(24, 0).Rune; got != ' ' {
		t.Fatalf("the divider column holds %q, want a blank", got)
	}
}

// TestDockDragIgnoresAnotherButtonComingUp checks that only the button
// that started the drag ends it. Another one coming up proves nothing:
// the first may still be down, and the pointer is still the dock's.
func TestDockDragIgnoresAnotherButtonComingUp(t *testing.T) {
	d, panel, _ := newTestDock(t, 24, 100, 30)
	r := rootOver(d, 100, 30)

	r.HandleMouse(pressAt(24, 3))
	r.HandleMouse(moveTo(30, 3))
	for _, ev := range rightTap(30, 3) {
		r.HandleMouse(ev)
	}
	r.HandleMouse(moveTo(40, 3))

	if d.Width != 40 || panel.size.Cols != 40 {
		t.Fatalf("width = %d and the panel is %d wide, want the drag to carry on to 40",
			d.Width, panel.size.Cols)
	}
	// And the left button still ends it.
	r.HandleMouse(releaseAt(40, 3))
	r.HandleMouse(moveTo(70, 3))
	if d.Width != 40 {
		t.Fatalf("width = %d after the button came up, want 40", d.Width)
	}
}

// The dock points the keys at the pane under the pointer rather than
// only at the half it is in.
//
// Focus travels down from the half, and the path it takes may end at
// another pane entirely: the split below it hands the keys to whichever
// half it was already pointing at. A press on the other one has to land
// there.
func TestDockMovesTheKeysToThePaneUnderThePointer(t *testing.T) {
	panel := &fake{name: "panel"}
	left, right := &picky{name: "left", first: true},
		&picky{name: "right", first: true}
	d := NewDock(10, panel, NewSplit(Columns, left, right))
	r := rootOver(d, 41, 4)
	d.Focus(panel)
	// What the left pane had been told before the press, so the test
	// asks only about the press itself.
	told := len(left.focused)

	// The right-hand pane, which is not the one the split is pointing
	// at while the panel has the keys.
	if took, err := r.HandleMouse(pressAt(35, 1)); err != nil || !took {
		t.Fatalf("the press was taken=%v: %v", took, err)
	}

	if got := FocusedLeaf(d); got != Widget(right) {
		t.Error("the keys went somewhere other than the pane under the pointer")
	}
	if !right.focus {
		t.Error("the pane under the pointer was not told it has the keys")
	}
	if len(left.clicks)+len(right.clicks) != 0 {
		t.Error("the press was delivered as well as moving the keys")
	}
	// Deepest first, so the keys travel down a path already pointing at
	// the right pane. The other way round the left pane would be told it
	// gained and then lost them, for a click that never touched it.
	if got := left.focused[told:]; len(got) != 0 {
		t.Errorf("the pane that was not clicked was told %v about the keys, want nothing", got)
	}
}

// The dock keeps the press itself when there is no split below it to do
// the job. Its other half is the pane, and the dock is the only
// container between the keys and it.
func TestDockKeepsThePressThatMovesTheKeysToItsOtherHalf(t *testing.T) {
	panel := &fake{name: "panel"}
	rest := &picky{name: "rest", first: true}
	d := NewDock(10, panel, rest)
	r := rootOver(d, 41, 4)
	d.Focus(panel)

	if took, err := r.HandleMouse(pressAt(35, 1)); err != nil || !took {
		t.Fatalf("the press was taken=%v: %v", took, err)
	}

	if !rest.focus {
		t.Error("the press did not move the keys to the pane beside the panel")
	}
	if len(rest.clicks) != 0 {
		t.Errorf("the pane was handed %d presses as well, want none", len(rest.clicks))
	}

	// And the next press is the pane's own.
	r.HandleMouse(releaseAt(35, 1))
	r.HandleMouse(pressAt(35, 1))
	if len(rest.clicks) == 0 {
		t.Error("the second press was kept as well, so the pane is never clicked")
	}
}

// A pane that answers FocusesFirst with false gets the press that moves
// the keys to it, the same as a widget that does not answer at all.
func TestDockDeliversThePressToAPaneThatDoesNotAskToBeSpared(t *testing.T) {
	panel := &fake{name: "panel"}
	rest := &picky{name: "rest"}
	d := NewDock(10, panel, rest)
	r := rootOver(d, 41, 4)
	d.Focus(panel)

	if took, err := r.HandleMouse(pressAt(35, 1)); err != nil || !took {
		t.Fatalf("the press was taken=%v: %v", took, err)
	}

	if !rest.focus {
		t.Error("the press did not move the keys to the pane beside the panel")
	}
	if len(rest.clicks) != 1 {
		t.Errorf("the pane was handed %d presses, want the one that moved the keys",
			len(rest.clicks))
	}
}
