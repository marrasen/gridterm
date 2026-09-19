package files

import (
	"strconv"
	"strings"
	"testing"

	"github.com/marrasen/gridterm/input"
	"github.com/marrasen/gridterm/ui"
)

// aFileOf is a reader holding the lines given, laid out and read.
func aFileOf(t *testing.T, cols, rows int, lines ...string) *Reader {
	t.Helper()
	r := NewReader("notes.txt", "/tmp/notes.txt")
	r.Style = readerStyle()
	r.Read = func(then func([]string, bool, error)) { then(lines, false, nil) }
	r.Layout(ui.Size{Cols: cols, Rows: rows})
	r.Open()
	return r
}

// typed sends a line of characters to a reader, the way a keyboard does.
func typed(t *testing.T, r *Reader, s string) {
	t.Helper()
	for _, c := range s {
		if _, err := r.HandleKey(input.Event{
			Kind: input.Text, Rune: c, NormalText: true,
		}); err != nil {
			t.Fatalf("typing %q: %v", c, err)
		}
	}
}

// numbered is a file of n lines, each saying which it is.
func numbered(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = "line " + strconv.Itoa(i+1)
	}
	return out
}

// Typing "/" asks what to look for, and Enter goes to it.
func TestSearchingGoesToTheLine(t *testing.T) {
	r := aFileOf(t, 40, 10, append(numbered(40), "the needle is here")...)

	typed(t, r, "/needle")

	if what, got, on := r.Asking(); !on || what != "/" || got != "needle" {
		t.Fatalf("it is asking %q%q (on=%v), want a search for needle", what, got, on)
	}
	press(t, r, input.KeyEnter)

	if _, _, on := r.Asking(); on {
		t.Error("it is still asking after Enter")
	}
	// The line is on screen, which is what a search is for.
	if got := r.Top(); got > 40 || got+r.rows() <= 40 {
		t.Errorf("the reader is on lines %d-%d, and the match is line 41", got+1, got+r.rows())
	}
}

// The search wraps, so a pattern above where the reader is is found.
func TestSearchingWrapsRound(t *testing.T) {
	lines := append([]string{"the needle is here"}, numbered(40)...)
	r := aFileOf(t, 40, 10, lines...)
	r.End()

	typed(t, r, "/needle")
	press(t, r, input.KeyEnter)

	if got := r.Top(); got != 0 {
		t.Errorf("the reader is on line %d, want the first, where the match is", got+1)
	}
}

// Searching ignores case, which is what a reader wants far more often
// than the other way round.
func TestSearchingIgnoresCase(t *testing.T) {
	r := aFileOf(t, 40, 10, append(numbered(30), "The Needle")...)

	typed(t, r, "/NEEDLE")
	press(t, r, input.KeyEnter)

	if got := r.Find(); got != "NEEDLE" {
		t.Errorf("it is looking for %q", got)
	}
	if got := r.Top(); got+r.rows() <= 30 {
		t.Errorf("the reader is on line %d and the match is line 31", got+1)
	}
}

// n goes to the next match and N to the one before.
func TestNAndShiftNStepThroughTheMatches(t *testing.T) {
	lines := []string{"needle", "one", "needle", "two", "needle"}
	r := aFileOf(t, 40, 4, lines...)

	typed(t, r, "/needle")
	press(t, r, input.KeyEnter)
	first := r.Top()
	typed(t, r, "n")
	second := r.Top()

	if second == first {
		t.Errorf("n stayed on line %d, want the next match", first+1)
	}
	typed(t, r, "N")
	if got := r.Top(); got != first {
		t.Errorf("N went to line %d, want back to %d", got+1, first+1)
	}
}

// A search with nothing to find says so rather than moving silently.
func TestASearchWithNothingToFindSaysSo(t *testing.T) {
	r := aFileOf(t, 40, 10, numbered(20)...)

	typed(t, r, "/haystack")
	press(t, r, input.KeyEnter)

	g := drawReader(r, 40, 10)
	if got := readerRow(g, 9); !strings.Contains(got, "haystack") {
		t.Errorf("the bottom row is %q, want it to say what was not found", got)
	}
	// And the next key takes the word away, so it is read once.
	press(t, r, input.KeyDown)
	g = drawReader(r, 40, 10)
	if got := readerRow(g, 9); strings.Contains(got, "haystack") {
		t.Errorf("the bottom row still reads %q after a key", got)
	}
}

// Escape takes the question back, and backspacing out of it does too.
func TestAQuestionCanBeTakenBack(t *testing.T) {
	r := aFileOf(t, 40, 10, numbered(20)...)

	typed(t, r, "/needle")
	press(t, r, input.KeyEscape)
	if _, _, on := r.Asking(); on {
		t.Error("Escape left the question up")
	}

	typed(t, r, "/ab")
	press(t, r, input.KeyBackspace)
	press(t, r, input.KeyBackspace)
	if _, _, on := r.Asking(); !on {
		t.Error("backspacing to nothing took the question away too early")
	}
	press(t, r, input.KeyBackspace)
	if _, _, on := r.Asking(); on {
		t.Error("backspacing out of an empty question left it up")
	}
}

// Typing ":" and a number goes to that line, counting from one.
func TestGoingToALineNumber(t *testing.T) {
	r := aFileOf(t, 40, 10, numbered(100)...)

	typed(t, r, ":50")
	press(t, r, input.KeyEnter)

	if got := r.Top(); got != 49 {
		t.Errorf("it went to line %d, want line 50 at the top", got+1)
	}
}

// A line number past the end goes to the end rather than nowhere.
func TestALineNumberPastTheEndGoesToTheEnd(t *testing.T) {
	r := aFileOf(t, 40, 10, numbered(20)...)

	typed(t, r, ":900")
	press(t, r, input.KeyEnter)

	if !r.AtEnd() {
		t.Errorf("it went to line %d, want the end of the file", r.Top()+1)
	}
}

// Something that is not a number says so.
func TestSomethingThatIsNotALineNumberSaysSo(t *testing.T) {
	r := aFileOf(t, 40, 10, numbered(20)...)

	typed(t, r, ":fifty")
	press(t, r, input.KeyEnter)

	g := drawReader(r, 40, 10)
	if got := readerRow(g, 9); !strings.Contains(got, "fifty") {
		t.Errorf("the bottom row is %q, want it to say what was typed", got)
	}
	if got := r.Top(); got != 0 {
		t.Errorf("it moved to line %d for something that is not a number", got+1)
	}
}

// A match already on screen is left where it is: a search that jumped
// every time would move the page under a match the user can see.
func TestAMatchAlreadyOnScreenDoesNotMoveThePage(t *testing.T) {
	lines := append([]string{"one", "the needle", "three"}, numbered(50)...)
	r := aFileOf(t, 40, 10, lines...)

	typed(t, r, "/needle")
	press(t, r, input.KeyEnter)

	if got := r.Top(); got != 0 {
		t.Errorf("the page moved to line %d for a match already on screen", got+1)
	}
}

// What was searched for is marked out where it falls on a line.
func TestTheMatchIsMarkedOut(t *testing.T) {
	r := aFileOf(t, 40, 10, "nothing here", "a needle in it")

	typed(t, r, "/needle")
	press(t, r, input.KeyEnter)
	g := drawReader(r, 40, 10)

	// "a " then the match: the third column of the second line.
	if got := g.At(2, 2).BG; got != r.Style.SelectedBG {
		t.Errorf("the match sits on %v, want the marked-out ground %v", got, r.Style.SelectedBG)
	}
	// And what is not the match is not marked out.
	if got := g.At(0, 2).BG; got == r.Style.SelectedBG {
		t.Error("the whole line is marked out, want only the match")
	}
}

// While a question is up, the keys that move through the file are typed
// into it rather than acted on.
func TestKeysGoIntoTheQuestionWhileItIsUp(t *testing.T) {
	r := aFileOf(t, 40, 10, numbered(100)...)
	r.Scroll(20)
	was := r.Top()

	typed(t, r, "/n")
	press(t, r, input.KeyDown)

	if got := r.Top(); got != was {
		t.Errorf("the page moved to line %d while a question was up", got+1)
	}
}
