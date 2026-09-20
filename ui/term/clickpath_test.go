package term

import (
	"testing"

	"github.com/marrasen/gridterm/input"
)

// Ctrl and a click on a path a program printed opens it.
func TestCtrlClickOnAPathOpensIt(t *testing.T) {
	var opened string
	var openedDir bool
	var openedLine int
	term, f := newTestTerm(t, 60, 6, Config{
		FindPath: func(text, dir string) (string, bool, bool) {
			if text == `C:\Users\marcus\notes.txt` {
				return text, false, true
			}
			return "", false, false
		},
		OnPath: func(at string, isDir bool, line int) {
			opened, openedDir, openedLine = at, isDir, line
		},
	})
	f.feed(t, term, `see C:\Users\marcus\notes.txt for it`)
	draw(term, 60, 6)

	took, err := term.HandleMouse(input.MouseEvent{
		Kind: input.MousePress, Button: input.MouseLeft,
		Col: 10, Row: 0, Mods: input.ModCtrl,
	})

	if err != nil {
		t.Fatalf("the press: %v", err)
	}
	if !took {
		t.Error("the press was not taken")
	}
	if opened != `C:\Users\marcus\notes.txt` {
		t.Errorf("it opened %q", opened)
	}
	if openedDir || openedLine != 0 {
		t.Errorf("it opened it as dir=%v line=%d", openedDir, openedLine)
	}
}

// The cursor keeps off the row the address is written on. A shell
// sitting at its prompt on the bottom row puts the cursor exactly
// where a browser writes where a link goes.
func TestTheCursorKeepsOffTheAddress(t *testing.T) {
	term, f := newTestTerm(t, 40, 4, Config{OnLink: func(string) {}})
	term.SetFocus(true)
	// Down to the bottom row, so the prompt is exactly where the
	// address gets written.
	f.feed(t, term, "see https://example.com/page\r\n\r\n\r\n$ ")
	draw(term, 40, 4)
	if cur := draw(term, 40, 4).Cursor(); cur.Y != 3 {
		t.Fatalf("the cursor is on row %d, want the bottom row", cur.Y)
	}

	term.SetHover(8, 0, input.ModCtrl)
	g := draw(term, 40, 4)

	if got := term.HoveredLink(); got != "https://example.com/page" {
		t.Fatalf("the pointer is over %q", got)
	}
	if cur := g.Cursor(); cur.Visible {
		t.Error("the cursor is still drawn on the row the address is written on")
	}
}

// A cursor anywhere else is left alone.
func TestTheCursorElsewhereIsLeftAlone(t *testing.T) {
	term, f := newTestTerm(t, 40, 4, Config{OnLink: func(string) {}})
	term.SetFocus(true)
	f.feed(t, term, "see https://example.com/page\r\n$ ")

	term.SetHover(8, 0, input.ModCtrl)
	g := draw(term, 40, 4)

	if cur := g.Cursor(); !cur.Visible {
		t.Error("the cursor went out although the address is on another row")
	}
}
