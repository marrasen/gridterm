package term

import (
	"fmt"
	"strings"
	"testing"

	"github.com/marrasen/gridterm/grid"
)

// Reading more lines than the screen holds reaches back into what has
// scrolled off, which is how somebody reads the whole of a long command's
// output.
func TestTextLinesReadsBackThroughHistory(t *testing.T) {
	term, f := newTestTerm(t, 20, 3, Config{})
	var said strings.Builder
	var want []string
	for i := 1; i <= 10; i++ {
		fmt.Fprintf(&said, "line %d\r\n", i)
		want = append(want, fmt.Sprintf("line %d", i))
	}
	// The row the cursor is left sitting on, below the last line.
	want = append(want, "")
	f.feed(t, term, said.String())

	// The screen and nothing above it, which is what a read with no
	// count asks for.
	if got, screen := term.TextLines(0), strings.Join(want[len(want)-3:], "\n"); got != screen {
		t.Errorf("the screen is %q, want %q", got, screen)
	}
	if got := term.Text(); got != term.TextLines(0) {
		t.Errorf("Text is %q and TextLines(0) is %q", got, term.TextLines(0))
	}

	// Six lines, which is twice the screen and into history.
	if got, six := term.TextLines(6), strings.Join(want[len(want)-6:], "\n"); got != six {
		t.Errorf("six lines are %q, want %q", got, six)
	}

	// More lines than there are gives what there is, starting at the
	// oldest line kept.
	if got, all := term.TextLines(500), strings.Join(want, "\n"); got != all {
		t.Errorf("everything is %q, want %q", got, all)
	}
}

// A read that reaches into history leaves the user's own view of it
// where it was.
func TestReadingHistoryDoesNotMoveTheView(t *testing.T) {
	term, f := newTestTerm(t, 20, 3, Config{})
	var said strings.Builder
	for i := 1; i <= 10; i++ {
		fmt.Fprintf(&said, "line %d\r\n", i)
	}
	f.feed(t, term, said.String())

	term.ScrollView(4)
	was := rows(draw(term, 20, 3))
	if got := term.TextLines(8); !strings.Contains(got, "line 1") {
		t.Errorf("eight lines are %q", got)
	}
	if got := viewOffset(term); got != 4 {
		t.Errorf("the view is %d lines back, want 4", got)
	}
	if got := rows(draw(term, 20, 3)); got != was {
		t.Errorf("the view now draws %q, want %q", got, was)
	}
}

// viewOffset is how far back into history the view is.
func viewOffset(term *Terminal) int {
	term.mu.Lock()
	defer term.mu.Unlock()
	return term.term.Screen().ViewOffset()
}

// rows is what a grid draws, as lines of text.
func rows(g *grid.Grid) string {
	_, height := g.Size()
	var out []string
	for y := 0; y < height; y++ {
		out = append(out, rowText(g, y))
	}
	return strings.Join(out, "\n")
}
