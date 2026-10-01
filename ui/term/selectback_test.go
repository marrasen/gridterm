package term

import (
	"fmt"
	"strings"
	"testing"

	"github.com/marrasen/kakel/input"
)

// numbered feeds lines "line 0" to "line n-1", each on a row of its
// own, the last with no newline after it.
func numbered(t *testing.T, term *Terminal, f *fakeSession, n int) {
	t.Helper()
	var b strings.Builder
	for i := range n {
		if i > 0 {
			b.WriteString("\r\n")
		}
		fmt.Fprintf(&b, "line %d", i)
	}
	f.feed(t, term, b.String())
}

// A selection stays on its text as the view scrolls, rather than on the
// rows of the screen.
func TestASelectionScrollsWithItsText(t *testing.T) {
	term, f := newTestTerm(t, 20, 4, Config{WriteClipboard: func(string) {}})
	numbered(t, term, f, 10)
	draw(term, 20, 4)
	// The screen shows lines 6 to 9; select line 7.
	mouseTo(t, term, input.MouseEvent{Kind: input.MousePress, Button: input.MouseLeft, Row: 1})
	mouseTo(t, term, input.MouseEvent{Kind: input.MouseMove, Button: input.MouseLeft, Col: 5, Row: 1})
	mouseTo(t, term, input.MouseEvent{Kind: input.MouseRelease, Button: input.MouseLeft, Col: 5, Row: 1})
	if got := term.SelectionText(); got != "line 7" {
		t.Fatalf("selected %q", got)
	}
	term.ScrollView(2)
	draw(term, 20, 4)
	sel := term.g.Selection()
	if !sel.Active || sel.Anchor.Y != 3 || sel.Cursor.Y != 3 {
		t.Fatalf("scrolled back two lines, the highlight is on %+v, want row 3", sel)
	}
	if got := term.SelectionText(); got != "line 7" {
		t.Fatalf("scrolled, the selection reads %q", got)
	}
	// Output moves the text up, and the selection with it.
	term.ScrollView(-2)
	f.feed(t, term, "\r\nline 10")
	draw(term, 20, 4)
	if sel := term.g.Selection(); sel.Anchor.Y != 0 {
		t.Fatalf("a line on, the highlight is on row %d, want 0", sel.Anchor.Y)
	}
}

// A drag that scrolls back with the wheel selects into history, and
// reads every line it covers, off the screen or not.
func TestASelectionReachesIntoHistory(t *testing.T) {
	term, f := newTestTerm(t, 20, 4, Config{WriteClipboard: func(string) {}})
	numbered(t, term, f, 10)
	draw(term, 20, 4)
	// From the end of line 9, up into history with the wheel, three
	// lines a notch.
	mouseTo(t, term, input.MouseEvent{Kind: input.MousePress, Button: input.MouseLeft, Col: 6, Row: 3})
	mouseTo(t, term, input.MouseEvent{Kind: input.MouseMove, Button: input.MouseLeft, Col: 0, Row: 0})
	for range 2 {
		mouseTo(t, term, input.MouseEvent{Kind: input.MousePress, Button: input.MouseWheelUp})
	}
	mouseTo(t, term, input.MouseEvent{Kind: input.MouseRelease, Button: input.MouseLeft, Col: 0, Row: 0})
	got := term.SelectionText()
	if !strings.HasPrefix(got, "line ") || !strings.HasSuffix(got, "line 9") || strings.Count(got, "\n") < 6 {
		t.Fatalf("selected %q, want from a line in history to line 9", got)
	}
	// Back at the live screen, a drag held past the top edge, as the
	// window scrolls for it, goes on to the oldest line.
	term.ScrollView(-100)
	mouseTo(t, term, input.MouseEvent{Kind: input.MousePress, Button: input.MouseLeft, Col: 6, Row: 3})
	mouseTo(t, term, input.MouseEvent{Kind: input.MouseMove, Button: input.MouseLeft, Col: 0, Row: 0})
	term.DragScroll(100)
	mouseTo(t, term, input.MouseEvent{Kind: input.MouseRelease, Button: input.MouseLeft, Col: 0, Row: 0})
	if got := term.SelectionText(); !strings.HasPrefix(got, "line 0\n") || !strings.HasSuffix(got, "line 9") {
		t.Fatalf("dragged to the top, selected %q", got)
	}
}

// Select All takes every line history keeps and the screen's.
func TestSelectAllTakesTheScrollback(t *testing.T) {
	var copied string
	term, f := newTestTerm(t, 20, 4, Config{WriteClipboard: func(s string) { copied = s }})
	numbered(t, term, f, 10)
	draw(term, 20, 4)
	term.SelectAll()
	if !term.Copy() {
		t.Fatal("Select All selected nothing")
	}
	var want []string
	for i := range 10 {
		want = append(want, fmt.Sprintf("line %d", i))
	}
	if copied != strings.Join(want, "\n") {
		t.Fatalf("copied %q", copied)
	}
}
