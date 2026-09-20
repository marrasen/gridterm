package term

import (
	"strings"
	"testing"

	"github.com/marrasen/gridterm/grid"
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

// A bare address printed by a program is followable, even though
// nothing said it was a link. Most output does not use OSC 8.
func TestABareAddressInOutputIsFollowable(t *testing.T) {
	var followed []string
	term, f := newTestTerm(t, 60, 5, Config{
		OnLink: func(at string) { followed = append(followed, at) },
	})
	f.feed(t, term, "see https://example.com/page for more\r\n")
	draw(term, 60, 5)

	mousePress(t, term, input.MouseEvent{
		Kind: input.MousePress, Button: input.MouseLeft,
		Col: 10, Row: 0, Mods: input.ModCtrl,
	})

	if len(followed) != 1 || followed[0] != "https://example.com/page" {
		t.Errorf("it followed %v, want the address in the line", followed)
	}
}

// What a program said beats what the text looks like: an OSC 8 link
// whose words happen to be an address follows the one it declared.
func TestWhatTheProgramSaidBeatsTheGuess(t *testing.T) {
	var followed []string
	term, f := newTestTerm(t, 60, 5, Config{
		OnLink: func(at string) { followed = append(followed, at) },
	})
	f.feed(t, term, "\x1b]8;;https://declared.example\x07https://printed.example\x1b]8;;\x07")
	draw(term, 60, 5)

	mousePress(t, term, input.MouseEvent{
		Kind: input.MousePress, Button: input.MouseLeft,
		Col: 5, Row: 0, Mods: input.ModCtrl,
	})

	if len(followed) != 1 || followed[0] != "https://declared.example" {
		t.Errorf("it followed %v, want the address the program declared", followed)
	}
}

// The link under the pointer is underlined, so the user can see what
// a click would follow.
func TestTheLinkUnderThePointerIsUnderlined(t *testing.T) {
	term, _ := aLinkedPane(t, 40, 5)

	term.SetHover(2, 0, input.ModCtrl)

	g := draw(term, 40, 5)
	for x := range 5 {
		if g.At(x, 0).Attr&grid.AttrUnderline == 0 {
			t.Errorf("column %d of the link is not underlined", x)
		}
	}
	if g.At(6, 0).Attr&grid.AttrUnderline != 0 {
		t.Error("the text after the link is underlined too")
	}
}

// Nothing is underlined without ctrl, because without it a press
// means something else.
func TestNothingIsUnderlinedWithoutCtrl(t *testing.T) {
	term, _ := aLinkedPane(t, 40, 5)

	term.SetHover(2, 0, 0)

	g := draw(term, 40, 5)
	if g.At(0, 0).Attr&grid.AttrUnderline != 0 {
		t.Error("the link is underlined with no ctrl held")
	}
}

// Where the link goes is written along the bottom, the way a browser
// writes it: a program can put any address under any words.
func TestWhereTheLinkGoesIsShown(t *testing.T) {
	term, _ := aLinkedPane(t, 40, 6)

	term.SetHover(2, 0, input.ModCtrl)

	if got := term.HoveredLink(); got != "https://example.com/a" {
		t.Errorf("it says the link goes to %q", got)
	}
	if got := rowText(draw(term, 40, 6), 5); !strings.Contains(got, "https://example.com/a") {
		t.Errorf("the bottom row says %q, want the address", got)
	}
}

// And along the top when the link itself is along the bottom, rather
// than covering the thing being pointed at.
func TestTheAddressMovesOffTheLinkItNames(t *testing.T) {
	var followed []string
	term, f := newTestTerm(t, 40, 3, Config{
		OnLink: func(at string) { followed = append(followed, at) },
	})
	f.feed(t, term, "\r\n\r\nhttps://example.com/down")
	draw(term, 40, 3)

	term.SetHover(2, 2, input.ModCtrl)

	g := draw(term, 40, 3)
	if got := rowText(g, 0); !strings.Contains(got, "https://example.com/down") {
		t.Errorf("the top row says %q, want the address moved off the link", got)
	}
	if got := rowText(g, 2); !strings.Contains(got, "https://example.com/down") {
		t.Errorf("the link's own row says %q, want the link still there", got)
	}
}

// The pointer leaving takes the marking with it.
func TestThePointerLeavingTakesTheMarkingAway(t *testing.T) {
	term, _ := aLinkedPane(t, 40, 5)
	term.SetHover(2, 0, input.ModCtrl)
	if term.HoveredLink() == "" {
		t.Fatal("nothing is hovered, so this proves nothing")
	}

	term.SetHover(2, -1, input.ModCtrl)

	if got := term.HoveredLink(); got != "" {
		t.Errorf("it still says %q after the pointer left", got)
	}
	if draw(term, 40, 5).At(0, 0).Attr&grid.AttrUnderline != 0 {
		t.Error("the link is still underlined after the pointer left")
	}
}
