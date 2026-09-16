package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/marrasen/gridterm/input"
)

// A screen too big for a texture is left to the tree, clipped, rather
// than asking the GPU for an image it cannot make.
func TestAHeldScreenTooBigForATextureIsDrawnInTheTree(t *testing.T) {
	host, client, hostPane := aHeldScreen(t, 140, 44)
	frame(t, host)
	if host.scaled[hostPane] == nil {
		t.Fatal("the screen is not drawn on a layer of its own to begin with")
	}

	// The window watching it is dragged out wider than any texture, and
	// the screen here follows the size it asks for.
	cw, ch := host.renderer.CellSize()
	tooWide := mostScaledPixels/cw + 200
	client.resizeTo(tooWide*cw, 20*ch)
	waitFor(t, host, "the screen here to take the size over there", func() bool {
		return hostPane.Size().Cols > mostScaledPixels/cw
	}, client)
	frame(t, host)

	if host.scaled[hostPane] != nil {
		t.Errorf("a screen of %v was given a layer of its own", hostPane.Size())
	}
	if hostPane.Elsewhere() {
		t.Error("the pane leaves its room blank with nothing drawing it elsewhere")
	}
	// And the row still says the size, which is all the user has to go
	// on for a screen this window can only show a corner of.
	want := fmt.Sprintf("at %dx%d", hostPane.Size().Cols, hostPane.Size().Rows)
	if lines := panelText(host, panelNow); !hasLineWith(lines, want) {
		t.Errorf("the panel says %v, want a row saying %q", lines, want)
	}
}

// A click beside a scaled screen still reaches the tree.
func TestAClickBesideAScaledScreenReachesTheTree(t *testing.T) {
	host, _, hostPane := aHeldScreen(t, 140, 44)
	frame(t, host)

	// The dock's divider, which takes a press so it can be dragged.
	area := host.sidebarArea()
	px, py := pixelOfWindowCell(host, area.X+area.Cols, area.Y+1)
	if !mouseAt(t, host, input.MousePress, input.MouseLeft, px, py) {
		t.Fatal("nothing in the tree took the press on the divider")
	}

	if !host.root.Held() {
		t.Error("the tree is not holding the pointer, so the press went somewhere else")
	}
	if host.scaledHeld.Held() {
		t.Error("the scaled pane took a press that landed on the divider")
	}
	if got := hostPane.SelectionText(); got != "" {
		t.Errorf("the pane selected %q from a press beside it", got)
	}
}

// A dialog takes a click over a scaled screen, the same as it takes one
// anywhere else: it is drawn on the window's own grid, over the pane.
func TestADialogOverAScaledScreenTakesTheClick(t *testing.T) {
	host, _, hostPane := aHeldScreen(t, 140, 44)
	fillScreen(t, host, 0, hostPane)
	frame(t, host)
	s := host.scaled[hostPane]
	host.showForm(host.newConfirm("Really?", []string{"A question."}), nil)

	onCell(t, host, s, input.MousePress, hostPane.Size().Cols-2, 2)

	if host.scaledHeld.Held() {
		t.Error("the pane took a press with a dialog over it")
	}
	if got := hostPane.SelectionText(); got != "" {
		t.Errorf("the pane selected %q with a dialog over it", got)
	}
}

// A drag that leaves the room the pane was given goes on selecting, to
// the edge of the screen it is drawn from.
func TestADragOffAScaledScreenKeepsSelecting(t *testing.T) {
	host, _, hostPane := aHeldScreen(t, 140, 44)
	fillScreen(t, host, 0, hostPane)
	frame(t, host)
	s := host.scaled[hostPane]
	row, from := 3, 8

	onCell(t, host, s, input.MousePress, from, row)
	// Away to the left of the window, over the sidebar: the pointer is
	// the pane's until the button comes up.
	_, py := pixelOfCell(s, from, row)
	mouseAt(t, host, input.MouseMove, input.MouseLeft, 0, py)

	want := ""
	for x := 0; x <= from; x++ {
		want += string(cellRune(x, row))
	}
	if got := hostPane.SelectionText(); got != want {
		t.Errorf("the drag selected %q, want %q: to the first column of that row", got, want)
	}
}

// The watcher letting go mid-drag ends the drag. The pane goes back to
// the tree, and nothing carries on selecting there.
func TestLettingGoMidDragEndsTheDrag(t *testing.T) {
	host, client, hostPane := aHeldScreen(t, 140, 44)
	fillScreen(t, host, 0, hostPane)
	frame(t, host)
	s := host.scaled[hostPane]
	row, from := 3, 8
	onCell(t, host, s, input.MousePress, from, row)

	if err := client.closePane(newestPane(t, client)); err != nil {
		t.Fatalf("stop watching: %v", err)
	}
	waitFor(t, host, "the screen to go back to its own size", func() bool {
		return !hostPane.Held()
	}, client)
	frame(t, host)

	// The release belongs to nobody: the press it answers was taken by a
	// pane nothing draws scaled any more.
	_, py := pixelOfCell(s, from, row)
	if mouseAt(t, host, input.MouseMove, input.MouseLeft, 0, py) {
		t.Error("something took a move from a drag that had already ended")
	}
	onCell(t, host, s, input.MouseRelease, from, row)

	// And the pane, back in the tree, is not still dragging out a
	// selection there.
	area, _ := host.paneArea(hostPane)
	px, py := pixelOfWindowCell(host, area.X+5, area.Y+5)
	mouseAt(t, host, input.MouseMove, input.MouseLeft, px, py)
	// Still the one cell the press anchored, rather than the run to the
	// first column the move would have made of it. What is in that cell
	// is not the question: the screen was resized when the size came
	// back.
	if got := hostPane.SelectionText(); len([]rune(got)) > 1 {
		t.Errorf("the selection grew to %q after the drag ended, want the one cell it had", got)
	}
}

// A key after a click on a scaled screen reaches the shell: the click
// moved focus to the pane, the way it does in the tree.
func TestAKeyAfterAClickOnAScaledScreenReachesTheShell(t *testing.T) {
	host, _, hostPane := aHeldScreen(t, 140, 44)
	fillScreen(t, host, 0, hostPane)
	frame(t, host)
	s := host.scaled[hostPane]
	// Focus starts on the sidebar, so the click is what moves it.
	if err := host.focusPanel(); err != nil {
		t.Fatalf("focus the sidebar: %v", err)
	}

	onCell(t, host, s, input.MousePress, 4, 4)
	onCell(t, host, s, input.MouseRelease, 4, 4)
	sendKey(t, host, input.Event{Kind: input.Text, Rune: 'x', NormalText: true})

	waitFor(t, host, "the shell to be sent what was typed", func() bool {
		return strings.HasSuffix(host.shells[0].sentText(), "x")
	})
}

// The last cell of a scaled screen can be clicked, which is the corner
// the room the window has for it never reached.
func TestTheLastCellOfAScaledScreenIsReachable(t *testing.T) {
	host, _, hostPane := aHeldScreen(t, 140, 44)
	fillScreen(t, host, 0, hostPane)
	frame(t, host)
	s := host.scaled[hostPane]
	size := hostPane.Size()

	onCell(t, host, s, input.MousePress, size.Cols-1, size.Rows-1)

	want := string(cellRune(size.Cols-1, size.Rows-1))
	if got := hostPane.SelectionText(); got != want {
		t.Errorf("the press landed on %q, want the last cell's %q", got, want)
	}
}

// A release the tree is still owed does not reach a scaled screen.
//
// A dialog opening over a drag abandons the widget that took the press
// while the button is still down. Handing that release to the pane would
// report a button-up to a program that never saw the press.
func TestAReleaseOwedToTheTreeDoesNotReachAScaledScreen(t *testing.T) {
	host, _, hostPane := aHeldScreen(t, 140, 44)
	fillScreen(t, host, 0, hostPane)
	frame(t, host)
	s := host.scaled[hostPane]

	// A press the tree takes, and a dialog over it, which abandons the
	// widget holding the pointer.
	area := host.sidebarArea()
	px, py := pixelOfWindowCell(host, area.X+area.Cols, area.Y+1)
	if !mouseAt(t, host, input.MousePress, input.MouseLeft, px, py) {
		t.Fatal("nothing in the tree took the press on the divider")
	}
	host.showForm(host.newConfirm("Really?", []string{"A question."}), nil)
	sendKey(t, host, press(input.KeyEscape, 0))
	if host.root.Modal() != nil {
		t.Fatal("the dialog is still up, so the pane would be covered anyway")
	}
	if !host.root.Held() {
		t.Fatal("the tree is owed no release, so this is no longer the case it is about")
	}

	if onCell(t, host, s, input.MouseRelease, 4, 4) {
		t.Error("the pane was handed a release the tree was owed")
	}
	if got := hostPane.SelectionText(); got != "" {
		t.Errorf("the pane selected %q from a release nobody pressed for", got)
	}
}

// A second button does not take a scaled screen from the first, and only
// the button that took the pointer gives it back.
func TestASecondButtonDoesNotTakeAScaledScreenFromTheFirst(t *testing.T) {
	host, _, hostPane := aHeldScreen(t, 140, 44)
	fillScreen(t, host, 0, hostPane)
	frame(t, host)
	s := host.scaled[hostPane]
	px, py := pixelOfCell(s, 6, 6)

	// A button the pane has no use for starts nothing: the pointer is
	// taken by a press the pane acted on, not by any press at all.
	if mouseAt(t, host, input.MousePress, input.MouseRight, px, py) {
		t.Error("the pane took a press of a button it has no use for")
	}
	if host.scaledHeld.Held() {
		t.Fatal("a press the pane declined took the pointer")
	}
	mouseAt(t, host, input.MouseRelease, input.MouseRight, px, py)

	mouseAt(t, host, input.MousePress, input.MouseLeft, px, py)
	if !host.scaledHeld.Waiting(input.MouseLeft) {
		t.Fatal("the left button did not take the pointer")
	}

	// The right button is not one the pane acts on, and it does not take
	// the pointer from the left.
	if mouseAt(t, host, input.MousePress, input.MouseRight, px, py) {
		t.Error("the pane took a press of a button it has no use for")
	}
	if !host.scaledHeld.Waiting(input.MouseLeft) {
		t.Error("the right button took the pointer from the left")
	}
	mouseAt(t, host, input.MouseRelease, input.MouseRight, px, py)
	if !host.scaledHeld.Waiting(input.MouseLeft) {
		t.Error("another button coming up ended the drag")
	}

	mouseAt(t, host, input.MouseRelease, input.MouseLeft, px, py)
	if host.scaledHeld.Held() {
		t.Error("the left button came up and the pane still has the pointer")
	}
	if !hostPane.Focused() {
		t.Error("the click did not move focus to the pane")
	}
}

// A dialog opening mid-drag ends the drag on a scaled screen, the way
// PushModal ends one in the tree.
//
// The release is not coming: the dialog has the pointer now. A pane left
// waiting would go on extending the selection the next time the pointer
// crossed it.
func TestADialogOverAScaledScreenEndsADrag(t *testing.T) {
	host, _, hostPane := aHeldScreen(t, 140, 44)
	fillScreen(t, host, 0, hostPane)
	frame(t, host)
	s := host.scaled[hostPane]
	row, from := 3, 8
	onCell(t, host, s, input.MousePress, from, row)

	// Opened and closed from the keyboard, so no click of its own ends
	// anything.
	host.showForm(host.newConfirm("Really?", []string{"A question."}), nil)
	sendKey(t, host, press(input.KeyEscape, 0))
	if host.root.Modal() != nil {
		t.Fatal("the dialog is still up, so the pane would be covered anyway")
	}

	_, py := pixelOfCell(s, from, row)
	if mouseAt(t, host, input.MouseMove, input.MouseLeft, 0, py) {
		t.Error("something took a move from a drag the dialog ended")
	}
	if got := hostPane.SelectionText(); len([]rune(got)) > 1 {
		t.Errorf("the selection grew to %q while a dialog had the pointer", got)
	}
}

// A press on a scaled pane that does not have the keys moves them and
// does nothing else, the same as a press on a pane in the tree.
//
// The user cannot tell the two apart by looking. It is the same pane in
// the same slot, and the only difference is that somebody on another
// machine made their window bigger.
func TestAPressOnAScaledPaneWithoutTheKeysOnlyMovesThem(t *testing.T) {
	host, _, hostPane := aHeldScreen(t, 140, 44)
	fillScreen(t, host, 0, hostPane)
	frame(t, host)
	s := host.scaled[hostPane]
	if err := host.focusPanel(); err != nil {
		t.Fatalf("focus the sidebar: %v", err)
	}

	onCell(t, host, s, input.MousePress, 4, 4)

	if !hostPane.Focused() {
		t.Error("the press did not move the keys to the pane")
	}
	if got := hostPane.SelectionText(); got != "" {
		t.Errorf("the press selected %q, want nothing: it only moved the keys", got)
	}
	if host.scaledHeld.Held() {
		t.Error("a press that only moved the keys took the pointer")
	}

	// And the next press is the pane's own.
	onCell(t, host, s, input.MouseRelease, 4, 4)
	onCell(t, host, s, input.MousePress, 4, 4)
	if got, want := hostPane.SelectionText(), string(cellRune(4, 4)); got != want {
		t.Errorf("the second press selected %q, want the cell under it, %q", got, want)
	}
}
