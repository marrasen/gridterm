package term

import (
	"testing"

	"github.com/marrasen/gridterm/input"
	"github.com/marrasen/gridterm/ui"
)

// aLinkedPane returns a terminal showing one word with a hyperlink
// under it, and what following a link told the window.
func aLinkedPane(t *testing.T, cols, rows int) (*Terminal, *[]string) {
	t.Helper()
	var followed []string
	term, f := newTestTerm(t, cols, rows, Config{
		OnLink: func(at string) { followed = append(followed, at) },
	})
	f.feed(t, term, "\x1b]8;;https://example.com/a\x07click\x1b]8;;\x07 plain")
	// Drawn, because the cells a click asks about are the ones on
	// screen and the screen is filled when the pane is drawn. In
	// the window a frame always comes before a press.
	draw(term, cols, rows)
	return term, &followed
}

// mousePress hands one mouse event to the terminal.
func mousePress(t *testing.T, term *Terminal, ev input.MouseEvent) {
	t.Helper()
	if _, err := term.HandleMouse(ev); err != nil {
		t.Fatalf("the press: %v", err)
	}
}

// Ctrl and a press follows the link under the pointer, which is what
// every other terminal does. A plain press picks text out, and a link
// the user could not select around would be worse than one they hold
// a key for.
func TestCtrlClickFollowsALink(t *testing.T) {
	term, followed := aLinkedPane(t, 40, 5)

	mousePress(t, term, input.MouseEvent{
		Kind: input.MousePress, Button: input.MouseLeft,
		Col: 2, Row: 0, Mods: input.ModCtrl,
	})

	if len(*followed) != 1 || (*followed)[0] != "https://example.com/a" {
		t.Errorf("it followed %v, want the one address", *followed)
	}
}

// A plain press picks text out rather than following anything.
func TestAPlainClickDoesNotFollowALink(t *testing.T) {
	term, followed := aLinkedPane(t, 40, 5)

	mousePress(t, term, input.MouseEvent{
		Kind: input.MousePress, Button: input.MouseLeft, Col: 2, Row: 0,
	})

	if len(*followed) != 0 {
		t.Errorf("a plain press followed %v", *followed)
	}
}

// Ctrl and a press where there is no link starts a selection, the way
// a press does. The key is not a mode.
func TestCtrlClickOffALinkStillSelects(t *testing.T) {
	term, followed := aLinkedPane(t, 40, 5)

	mousePress(t, term, input.MouseEvent{
		Kind: input.MousePress, Button: input.MouseLeft,
		Col: 8, Row: 0, Mods: input.ModCtrl,
	})

	if len(*followed) != 0 {
		t.Errorf("it followed %v from a cell with no link", *followed)
	}
	if !term.g.Selection().Active {
		t.Error("ctrl and a press off a link did not start a selection")
	}
}

// The pointer says a link can be followed, but only while ctrl is
// held: without it the press means something else.
func TestThePointerSaysWhenALinkCanBeFollowed(t *testing.T) {
	term, _ := aLinkedPane(t, 40, 5)

	got, on := term.CursorAt(2, 0, input.ModCtrl)
	if !on || got != ui.CursorPointing {
		t.Errorf("over a link with ctrl the pointer is %v (%v), want the hand", got, on)
	}

	if _, on := term.CursorAt(2, 0, 0); on {
		t.Error("over a link without ctrl the pointer says it can be followed")
	}
	if _, on := term.CursorAt(8, 0, input.ModCtrl); on {
		t.Error("over plain text the pointer says a link can be followed")
	}
}

// A pane with nowhere to send a link says nothing about following
// one, and a press over it picks text out as usual.
func TestAPaneWithNowhereToSendALinkFollowsNothing(t *testing.T) {
	term, f := newTestTerm(t, 40, 5, Config{})
	f.feed(t, term, "\x1b]8;;https://example.com/a\x07click")
	draw(term, 40, 5)

	if _, on := term.CursorAt(2, 0, input.ModCtrl); on {
		t.Error("it offers to follow a link with nowhere to send it")
	}
	mousePress(t, term, input.MouseEvent{
		Kind: input.MousePress, Button: input.MouseLeft,
		Col: 2, Row: 0, Mods: input.ModCtrl,
	})
	if !term.g.Selection().Active {
		t.Error("the press did not pick text out instead")
	}
}

// The address under a cell is readable, and cells outside the screen
// have none rather than answering for a cell that is not there.
func TestTheAddressUnderACellIsReadable(t *testing.T) {
	term, _ := aLinkedPane(t, 40, 5)

	if got := term.LinkAt(0, 0); got != "https://example.com/a" {
		t.Errorf("the first cell has %q", got)
	}
	for _, at := range [][2]int{{-1, 0}, {0, -1}, {999, 0}, {0, 999}} {
		if got := term.LinkAt(at[0], at[1]); got != "" {
			t.Errorf("the cell at %v has %q, want nothing", at, got)
		}
	}
}
