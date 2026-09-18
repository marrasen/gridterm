package ui

import (
	"testing"

	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/input"
)

// TestMenubarDrawnTwiceLeavesTheLayerClean is the idle-frame rule. The
// bar fills its row and then writes the titles over it, which changes
// the same cell twice. Done straight onto the layer, the row is dirty on
// every frame and the compositor can never skip one.
func TestMenubarDrawnTwiceLeavesTheLayerClean(t *testing.T) {
	b, _ := newTestBar(t, &filler{ch: 'x'})
	g := grid.New(40, 20, fg, bg)
	b.Draw(g.View())

	g.ClearDirty()
	for i := range 2 {
		b.Draw(g.View())
		if g.RowDirty(0) {
			t.Fatalf("draw %d of an unchanged bar dirtied its row", i)
		}
	}

	// And a real change still gets through.
	b.HandleMouse(pressAt(1, 0))
	b.Draw(g.View())
	if !g.RowDirty(0) {
		t.Error("opening a menu did not change the bar")
	}
}

// TestMenubarTitleHasASpaceBeforeIt checks the title is drawn inside its
// label rather than against the edge of it, so two titles beside each
// other do not read as one word.
func TestMenubarTitleHasASpaceBeforeIt(t *testing.T) {
	b, _ := newTestBar(t, &filler{ch: 'x'})
	g := grid.New(40, 20, fg, bg)

	b.Draw(g.View())

	if got := g.At(0, 0).Rune; got != ' ' {
		t.Errorf("column 0 = %q, want a space before the first title", got)
	}
	if got := g.At(1, 0).Rune; got != 'F' {
		t.Errorf("column 1 = %q, want the title to start there", got)
	}
}

// TestMenubarStopsAtTheFirstTitleThatDoesNotFit checks that a narrow bar
// does not skip a long title and draw a shorter one after it. The
// titles would then be in a different order from the menus behind them.
func TestMenubarStopsAtTheFirstTitleThatDoesNotFit(t *testing.T) {
	b, _ := newTestBar(t, &filler{ch: 'x'})
	b.Menus = []MenuDef{
		{Title: "AAAA", Items: items("copy")}, // 6 columns
		{Title: "BBBB", Items: items("copy")}, // 6 more, which do not fit
		{Title: "C", Items: items("copy")},    // 3, which would
	}
	b.Layout(Size{Cols: 10, Rows: 20})

	got := b.labels()

	if got[0].Empty() {
		t.Error("the first title does not fit in 10 columns")
	}
	if !got[1].Empty() {
		t.Errorf("the second title was placed at %+v with no room for it", got[1])
	}
	if !got[2].Empty() {
		t.Errorf("the third title was drawn at %+v, past one that did not fit", got[2])
	}
}

// TestMenubarMenuForATitleWithNoRoomHangsUnderTheBar checks a title too
// far along to be drawn. Its menu still opens by keyboard, and it must
// hang under the bar rather than over it.
func TestMenubarMenuForATitleWithNoRoomHangsUnderTheBar(t *testing.T) {
	b, st := newTestBar(t, &filler{ch: 'x'})
	b.Menus = []MenuDef{
		{Title: "AAAAAAAA", Items: items("copy")},
		{Title: "BBBBBBBB", Items: items("copy", "paste")},
	}
	b.Layout(Size{Cols: 12, Rows: 20})
	if !b.labels()[1].Empty() {
		t.Fatal("the second title fits, so this proves nothing")
	}

	if !b.Open(1) {
		t.Fatal("a title with no room could not be opened")
	}

	box := st.top().box()
	if box.Empty() {
		t.Fatal("the menu has no box")
	}
	if box.Y < barRows {
		t.Errorf("menu box = %+v, want it under the bar at row %d", box, barRows)
	}
}

// TestMenubarReleaseOverATitleOpensNothing checks that only a press
// opens a menu. A button-up is the end of a gesture that began
// somewhere else.
func TestMenubarReleaseOverATitleOpensNothing(t *testing.T) {
	b, st := newTestBar(t, &filler{ch: 'x'})

	handled, _ := b.HandleMouse(releaseAt(1, 0))

	if handled {
		t.Error("a release over a title was claimed")
	}
	if len(st.shown) != 0 || b.OpenIndex() != -1 {
		t.Error("a release opened a menu")
	}
}

// TestMenubarCloseIsSafeFromItsOwnCloser checks the re-entrancy the doc
// comment on Close describes: the bar forgets the menu before tearing it
// down, so a closer that asks the bar to close again does nothing.
func TestMenubarCloseIsSafeFromItsOwnCloser(t *testing.T) {
	b, _ := newTestBar(t, &filler{ch: 'x'})
	closed := 0
	b.Present = func(m *Menu) func() {
		m.Layout(Size{Cols: 40, Rows: 20})
		return func() {
			closed++
			b.Close() // the app tearing a stack down calls back in
		}
	}
	b.Open(0)

	b.Close()

	if closed != 1 {
		t.Errorf("the menu was torn down %d times, want once", closed)
	}
	if b.OpenIndex() != -1 {
		t.Errorf("open = %d, want nothing open", b.OpenIndex())
	}
}

// TestMenubarPresentThatClosesLeavesNothingStranded checks a Present
// that takes the menu away again as it shows it. The bar must not be
// left holding a way to tear down a menu that has already gone.
func TestMenubarPresentThatClosesLeavesNothingStranded(t *testing.T) {
	b, _ := newTestBar(t, &filler{ch: 'x'})
	closed := 0
	b.Present = func(m *Menu) func() {
		m.Layout(Size{Cols: 40, Rows: 20})
		b.Close()
		return func() { closed++ }
	}

	b.Open(0)
	b.Close()

	if closed > 1 {
		t.Errorf("the menu was torn down %d times, want at most once", closed)
	}
	// Whatever it decided, the three pieces of state have to agree.
	if (b.OpenIndex() >= 0) != (b.closeOpen != nil) {
		t.Errorf("open = %d but the closer is %v", b.OpenIndex(), b.closeOpen != nil)
	}
	if (b.open != nil) != (b.OpenIndex() >= 0) {
		t.Errorf("open = %d but the menu is %v", b.OpenIndex(), b.open != nil)
	}
}

// TestMenubarStepWithNothingOpenDoesNothing checks the guard. Without
// it, an arrow key would open a menu nobody asked for.
func TestMenubarStepWithNothingOpenDoesNothing(t *testing.T) {
	b, st := newTestBar(t, &filler{ch: 'x'})

	b.step(1)
	b.step(-1)

	if len(st.shown) != 0 {
		t.Errorf("%d menus opened with none showing", len(st.shown))
	}
	if b.OpenIndex() != -1 {
		t.Errorf("open = %d, want nothing open", b.OpenIndex())
	}
}

// TestMenubarChildAreaIsWhatLayoutUsed checks the Container rule: the
// area a container reports must be the one it laid the child out in.
// Working it out twice gives the container two chances to disagree with
// itself, and a drag routed by the wrong one lands in the wrong place.
func TestMenubarChildAreaIsWhatLayoutUsed(t *testing.T) {
	child := &fake{name: "x"}
	b, _ := newTestBar(t, child)

	for _, size := range []Size{{Cols: 40, Rows: 20}, {Cols: 13, Rows: 4}, {Cols: 80, Rows: 30}} {
		b.Layout(size)
		area, ok := b.ChildArea(child)
		if !ok {
			t.Fatalf("at %+v the child has no area", size)
		}
		if area.Size() != child.size {
			t.Errorf("at %+v the child was laid out %+v but is reported at %+v",
				size, child.size, area.Size())
		}
	}
}

// TestMenubarChildAreaIsFalseWithNoRoom checks the other half of the
// contract: a child with nowhere to be drawn reports false rather than
// an empty rectangle.
func TestMenubarChildAreaIsFalseWithNoRoom(t *testing.T) {
	child := &filler{ch: 'x'}
	b, _ := newTestBar(t, child)

	b.Layout(Size{})

	if area, ok := b.ChildArea(child); ok {
		t.Errorf("ChildArea = %+v, true in a window of no size", area)
	}
}

// TestMenubarRemoveTellsTheChildFocusLeft checks the Focusable
// contract. A child taken out while the bar holds focus still thinks it
// has it, and would draw a cursor in a pane that is gone.
func TestMenubarRemoveTellsTheChildFocusLeft(t *testing.T) {
	child := &filler{ch: 'x'}
	b, _ := newTestBar(t, child)
	b.SetFocus(true)
	if !child.focused {
		t.Fatal("the child never had focus")
	}

	b.Remove(child)

	if child.focused {
		t.Error("the child was removed still holding focus")
	}
	if got := b.Children(); len(got) != 0 {
		t.Errorf("Children = %v, want none after the child was removed", got)
	}
}

// TestMenubarSetFocusIsNeverRepeated checks the contract Focusable
// states: a widget is never told the same thing twice.
func TestMenubarSetFocusIsNeverRepeated(t *testing.T) {
	child := &filler{ch: 'x'}
	b, _ := newTestBar(t, child)

	b.SetFocus(true)
	b.SetFocus(true)
	b.SetFocus(false)
	b.SetFocus(false)

	want := []bool{true, false}
	if len(child.focusLog) != len(want) {
		t.Fatalf("the child was told %v, want %v", child.focusLog, want)
	}
	for i, on := range want {
		if child.focusLog[i] != on {
			t.Errorf("told %v, want %v", child.focusLog, want)
		}
	}
}

// TestMenubarReplaceSizesTheNewChild checks that a widget put under the
// bar is told how much room it has. Without it a replacement pane keeps
// whatever size it was built with.
func TestMenubarReplaceSizesTheNewChild(t *testing.T) {
	child := &fake{name: "x"}
	b, _ := newTestBar(t, child)
	next := &fake{name: "y"}

	if !b.Replace(child, next) {
		t.Fatal("Replace refused the child")
	}

	if want := (Size{Cols: 40, Rows: 19}); next.size != want {
		t.Errorf("the replacement was told %+v, want %+v", next.size, want)
	}
}

// TestMenubarKeyWithNoChildIsNotSwallowed checks the bar after its child
// has gone: it must not claim keys it cannot deliver.
func TestMenubarKeyWithNoChildIsNotSwallowed(t *testing.T) {
	child := &filler{ch: 'x'}
	b, _ := newTestBar(t, child)
	b.Remove(child)

	if handled, _ := b.HandleKey(press(input.KeyA, 0)); handled {
		t.Error("a bar with nothing under it swallowed a key")
	}
	if handled, _ := b.HandleMouse(pressAt(5, 5)); handled {
		t.Error("a bar with nothing under it swallowed a click")
	}
}
