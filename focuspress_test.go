package main

import (
	"strings"
	"testing"

	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/input"
	"github.com/marrasen/gridterm/ui"
	"github.com/marrasen/gridterm/ui/term"
)

// mouseOn sends one mouse event on a cell of a widget, in that widget's
// own coordinates, through the routing every event in the window goes
// through.
func mouseOn(t *testing.T, a *testApp, w ui.Widget, kind input.MouseKind,
	button input.MouseButton, col, row int) bool {
	t.Helper()
	area, ok := a.paneArea(w)
	if !ok {
		t.Fatalf("%T is not on screen", w)
	}
	took, err := a.routeMouse(input.MouseEvent{
		Kind: kind, Button: button, Col: area.X + col, Row: area.Y + row,
	})
	if err != nil {
		t.Fatalf("the %v on %T failed: %v", kind, w, err)
	}
	return took
}

// pressOn presses the left button on a cell of a widget.
func pressOn(t *testing.T, a *testApp, w ui.Widget, col, row int) bool {
	t.Helper()
	return mouseOn(t, a, w, input.MousePress, input.MouseLeft, col, row)
}

// releaseOn lets the left button up on a cell of a widget.
func releaseOn(t *testing.T, a *testApp, w ui.Widget, col, row int) {
	t.Helper()
	mouseOn(t, a, w, input.MouseRelease, input.MouseLeft, col, row)
}

// printOnPane gives a shell one line to print and waits for it to reach
// the pane's screen.
func printOnPane(t *testing.T, a *testApp, which int, pane *term.Terminal, text string) {
	t.Helper()
	runes := []rune(text)
	a.shells[which].out <- []byte(text)
	waitFor(t, a, "the shell's text to reach the pane", func() bool {
		return screenOf(pane).At(len(runes)-1, 0).Rune == runes[len(runes)-1]
	})
}

// terminalsInTheTree returns the panes on screen, in the order they are
// laid out: the left half of a split first.
func terminalsInTheTree(t *testing.T, a *testApp, want int) []*term.Terminal {
	t.Helper()
	var out []*term.Terminal
	for _, leaf := range ui.Leaves(a.root.Widget()) {
		if pane, ok := leaf.(*term.Terminal); ok {
			out = append(out, pane)
		}
	}
	if len(out) != want {
		t.Fatalf("%d panes in the tree, want %d", len(out), want)
	}
	return out
}

// pressReport is what a program in the oldest mouse mode is sent for a
// left press on one of its cells.
func pressReport(col, row int) string {
	return string([]byte{0x1b, '[', 'M', 32, byte(col + 33), byte(row + 33)})
}

// A press on a pane that does not have the keys moves them there and
// does nothing else. The press the user meant as "look at this" would
// otherwise also start a selection in it.
func TestAPressOnAPaneWithoutTheKeysOnlyMovesThem(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	pane := onlyPaneOn(t, a)
	printOnPane(t, a, 0, pane, "hello")
	if err := a.focusPanel(); err != nil {
		t.Fatalf("focus the sidebar: %v", err)
	}

	if !pressOn(t, a, pane, 1, 0) {
		t.Fatal("nothing took the press on the pane")
	}

	if !pane.Focused() {
		t.Error("the press did not move the keys to the pane")
	}
	if got := pane.SelectionText(); got != "" {
		t.Errorf("the press selected %q, want nothing: it only moved the keys", got)
	}

	// And the next press is the pane's own.
	releaseOn(t, a, pane, 1, 0)
	pressOn(t, a, pane, 1, 0)
	if got := pane.SelectionText(); got != "e" {
		t.Errorf("the second press selected %q, want the cell under it, %q", got, "e")
	}
}

// The same inside a split: the half that does not have the keys takes a
// press as nothing but the move.
func TestAPressOnTheOtherHalfOfASplitOnlyMovesTheKeys(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	if err := a.splitHere(ui.Columns); err != nil {
		t.Fatalf("split: %v", err)
	}
	panes := terminalsInTheTree(t, a, 2)
	left, right := panes[0], panes[1]
	// Both shells at once: which shell feeds which pane is not this
	// test's business.
	for i := range a.shells {
		a.shells[i].out <- []byte("hello")
	}
	waitFor(t, a, "both panes to show what their shells said", func() bool {
		return screenOf(left).At(1, 0).Rune == 'e' && screenOf(right).At(1, 0).Rune == 'e'
	})
	a.focus(left)

	if !pressOn(t, a, right, 1, 0) {
		t.Fatal("nothing took the press on the right-hand pane")
	}

	if !right.Focused() {
		t.Error("the press did not move the keys to the right-hand pane")
	}
	if got := right.SelectionText(); got != "" {
		t.Errorf("the press selected %q there, want nothing: it only moved the keys", got)
	}

	releaseOn(t, a, right, 1, 0)
	pressOn(t, a, right, 1, 0)
	if got := right.SelectionText(); got != "e" {
		t.Errorf("the second press selected %q, want the cell under it, %q", got, "e")
	}
}

// The sidebar has the keys, and the press lands on the half of a split
// that did not have them either. The keys have two moves to make, and
// both have to end on the pane under the pointer: the dock hands them to
// the rest of the window, and the rest hands them down a path of its own
// that starts out pointing at the other half.
func TestAPressFromTheSidebarLandsOnTheHalfItWasMeantFor(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	if err := a.splitHere(ui.Columns); err != nil {
		t.Fatalf("split: %v", err)
	}
	panes := terminalsInTheTree(t, a, 2)
	left, right := panes[0], panes[1]
	for i := range a.shells {
		a.shells[i].out <- []byte("hello")
	}
	waitFor(t, a, "both panes to show what their shells said", func() bool {
		return screenOf(left).At(1, 0).Rune == 'e' && screenOf(right).At(1, 0).Rune == 'e'
	})
	a.focus(left)
	if err := a.focusPanel(); err != nil {
		t.Fatalf("focus the sidebar: %v", err)
	}

	if !pressOn(t, a, right, 1, 0) {
		t.Fatal("nothing took the press on the right-hand pane")
	}

	if got := ui.FocusedLeaf(a.root.Widget()); got != ui.Widget(right) {
		t.Errorf("the keys reach %T, want the pane under the pointer", got)
	}
	if !right.Focused() || left.Focused() {
		t.Errorf("right has the keys=%v and left has them=%v, want the right-hand pane alone",
			right.Focused(), left.Focused())
	}
	if got := right.SelectionText(); got != "" {
		t.Errorf("the press selected %q there, want nothing: it only moved the keys", got)
	}
}

// A press on the pane that already has the keys selects straight away.
// Nothing is swallowed when there is no focus to move.
func TestAPressOnThePaneWithTheKeysSelectsAtOnce(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	pane := onlyPaneOn(t, a)
	printOnPane(t, a, 0, pane, "hello")
	a.focus(pane)

	if !pressOn(t, a, pane, 1, 0) {
		t.Fatal("nothing took the press on the pane")
	}

	if got := pane.SelectionText(); got != "e" {
		t.Errorf("the press selected %q, want the cell under it, %q", got, "e")
	}
}

// A file pane takes a press the same way: the first one moves the keys
// and opens nothing, so a click meant to look at the browser does not
// also walk into a directory.
func TestAPressOnAFilePaneWithoutTheKeysOnlyMovesThem(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	b, left, _ := onlyBrowser(t, a)
	pane := b.view.Panes()[0]
	if err := a.focusPanel(); err != nil {
		t.Fatalf("focus the sidebar: %v", err)
	}

	// The first row of the listing, which goes up out of the directory.
	// Two rows above it name the machine and say where the pane is.
	if !pressOn(t, a, pane, 1, 2) {
		t.Fatal("nothing took the press on the file pane")
	}

	if !pane.Focused() {
		t.Error("the press did not move the keys to the file pane")
	}
	if got := pane.At(); got != left {
		t.Errorf("the press opened %q, want the pane still showing %q", got, left)
	}

	releaseOn(t, a, pane, 1, 2)
	pressOn(t, a, pane, 1, 2)
	waitFor(t, a, "the second press to go up out of the directory", func() bool {
		return pane.At() != left && !pane.Busy()
	})
}

// The plus on a sidebar row opens its menu on the first press, whether
// or not the sidebar has the keys. The sidebar is a list of buttons, not
// a pane: a press on one means the button.
func TestThePlusOpensItsMenuWithTheKeysOnAPane(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	a.focus(onlyPaneOn(t, a))

	menu := clickPlus(t, a, conns.Local)

	if len(menu.Items()) == 0 {
		t.Error("the plus opened an empty menu")
	}
}

// A wheel notch is not a press: it scrolls the pane under the pointer
// and leaves the keys where they were.
func TestAWheelNotchScrollsAPaneWithoutTheKeys(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	pane := onlyPaneOn(t, a)
	a.shells[0].out <- []byte(strings.Repeat("a line of it\r\n", 40))
	waitFor(t, a, "the shell to fill the scrollback", func() bool {
		return screenOf(pane).At(0, 0).Rune == 'a'
	})
	if err := a.focusPanel(); err != nil {
		t.Fatalf("focus the sidebar: %v", err)
	}

	if !mouseOn(t, a, pane, input.MousePress, input.MouseWheelUp, 1, 1) {
		t.Fatal("nothing took the wheel notch over the pane")
	}

	if pane.ViewOffset() <= 0 {
		t.Error("the wheel notch did not scroll a pane that does not have the keys")
	}
	if pane.Focused() {
		t.Error("the wheel notch moved the keys, which only a press does")
	}
}

// A press on the dock's divider starts the drag even when the other half
// has the keys. The divider is the dock's own, not the pane's.
func TestAPressOnTheDividerStartsADragWithTheKeysElsewhere(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	pane := onlyPaneOn(t, a)
	a.focus(pane)
	side := a.sidebarArea()
	was := a.dock.Width

	took, err := a.routeMouse(input.MouseEvent{
		Kind: input.MousePress, Button: input.MouseLeft,
		Col: side.X + side.Cols, Row: side.Y + 1,
	})
	if err != nil {
		t.Fatalf("the press on the divider failed: %v", err)
	}
	if !took {
		t.Fatal("nothing took the press on the divider")
	}
	if got := a.root.Holding(); got != ui.Widget(a.dock) {
		t.Errorf("the pointer is held by %T, want the dock that owns the divider", got)
	}

	// And the divider follows the pointer.
	if _, err := a.routeMouse(input.MouseEvent{
		Kind: input.MouseMove, Button: input.MouseLeft,
		Col: side.X + side.Cols + 6, Row: side.Y + 1,
	}); err != nil {
		t.Fatalf("the drag failed: %v", err)
	}
	if a.dock.Width == was {
		t.Errorf("the panel is still %d wide, so the press started no drag", was)
	}
}

// A program that has taken the mouse over hears nothing from the press
// that moves the keys to its pane: the press never reaches the terminal.
// The press after it is reported.
func TestAProgramWatchingTheMouseHearsNothingFromTheFocusingPress(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	pane := onlyPaneOn(t, a)
	// The mode first, then the text: waiting for the text proves the
	// mode was read, because one shell is read in order.
	a.shells[0].out <- []byte("\x1b[?1000h")
	printOnPane(t, a, 0, pane, "hello")
	if err := a.focusPanel(); err != nil {
		t.Fatalf("focus the sidebar: %v", err)
	}

	pressOn(t, a, pane, 1, 0)
	releaseOn(t, a, pane, 1, 0)
	pressOn(t, a, pane, 2, 0)

	waitFor(t, a, "the program to be told about the second press", func() bool {
		return strings.Contains(a.shells[0].sentText(), pressReport(2, 0))
	})
	if got := a.shells[0].sentText(); strings.Contains(got, pressReport(1, 0)) {
		t.Error("the program was told about the press that only moved the keys")
	}
}
