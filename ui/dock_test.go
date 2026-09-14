package ui

import (
	"image/color"
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

func TestDockRoutesTheMouseToWhatWasClicked(t *testing.T) {
	d, panel, rest := newTestDock(t, 24, 100, 30)
	// fake takes no mouse events, so this checks the routing through a
	// click landing where each half is.
	if _, shown := d.ChildArea(panel); !shown {
		t.Fatal("the panel is not drawn")
	}
	took, _ := d.HandleMouse(input.MouseEvent{
		Kind: input.MousePress, Button: input.MouseLeft, Col: 5, Row: 5,
	})
	if took {
		t.Error("a click on the panel was taken by the dock rather than offered to the panel")
	}
	area, _ := d.ChildArea(rest)
	took, _ = d.HandleMouse(input.MouseEvent{
		Kind: input.MousePress, Button: input.MouseLeft, Col: area.X + 1, Row: 5,
	})
	if took {
		t.Error("a click on the rest was taken by the dock")
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
	next := &fake{name: "next"}
	if !d.Replace(rest, next) {
		t.Fatal("Replace would not swap the rest")
	}
	if d.Rest() != next {
		t.Fatal("the rest was not replaced")
	}
	if d.Replace(&fake{}, next) {
		t.Fatal("Replace swapped something that was not there")
	}

	// Losing the panel leaves the dock as its other half.
	stands, ok := d.Remove(panel)
	if !ok {
		t.Fatal("Remove would not take the panel out")
	}
	if stands != next {
		t.Fatalf("Remove said %v should stand in its place, want the rest", stands)
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

// Every child has to know where it is, or a drag cannot be routed to it.
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
