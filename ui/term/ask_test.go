package term

import (
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/input"
	"github.com/marrasen/gridterm/ui"
	"github.com/marrasen/gridterm/vt"
)

// The question a dead pane asks, and the two answers to it.
const (
	askText = "Connection closed. Reconnect?"
	askYes  = "Yes"
	askNo   = "Close"
)

// picker is a choice that records having been run, and can fail.
type picker struct {
	runs int
	err  error
}

func (p *picker) do() error {
	p.runs++
	return p.err
}

// askTerm builds a terminal with the usual question on it and returns
// the two choices' recorders.
func askTerm(t *testing.T, cols, rows int) (*Terminal, *fakeSession, *picker, *picker) {
	t.Helper()
	term, f := newTestTerm(t, cols, rows, Config{})
	yes, no := &picker{}, &picker{}
	term.Ask(askText,
		Choice{Label: askYes, Do: yes.do},
		Choice{Label: askNo, Do: no.do})
	return term, f, yes, no
}

// fullRow is one row of a grid, blank columns kept, so a column in the
// string is the column on the screen.
func fullRow(g *grid.Grid, y int) string {
	cols, _ := g.Size()
	var b strings.Builder
	for x := 0; x < cols; x++ {
		c := g.At(x, y)
		if c.Rune == 0 {
			b.WriteByte(' ')
			continue
		}
		b.WriteRune(c.Rune)
	}
	return b.String()
}

// choiceSpan is the first and last column a choice covers, the blank
// column each side included.
func choiceSpan(t *testing.T, g *grid.Grid, y int, label string) (first, last int) {
	t.Helper()
	at := strings.Index(fullRow(g, y), label)
	if at < 0 {
		t.Fatalf("choice %q is not on row %d: %q", label, y, fullRow(g, y))
	}
	return at - 1, at + len(label)
}

// clickAt presses the left button at a cell.
func clickAt(t *testing.T, term *Terminal, col, row int) {
	t.Helper()
	if _, err := term.HandleMouse(input.MouseEvent{
		Kind: input.MousePress, Button: input.MouseLeft, Col: col, Row: row,
	}); err != nil {
		t.Fatalf("HandleMouse: %v", err)
	}
}

// press sends a key press to the terminal and hands back its error.
func press(t *testing.T, term *Terminal, key input.Key) error {
	t.Helper()
	_, err := term.HandleKey(input.Event{Kind: input.KeyPress, Key: key})
	return err
}

func TestAskDrawsTheQuestionAndChoicesOnTheLastRow(t *testing.T) {
	term, f, _, _ := askTerm(t, 50, 4)
	f.feed(t, term, "the program said this")

	g := draw(term, 50, 4)

	row := fullRow(g, 3)
	for _, want := range []string{askText, askYes, askNo} {
		if !strings.Contains(row, want) {
			t.Errorf("the last row is %q, which is missing %q", row, want)
		}
	}
	if got := rowText(g, 0); got != "the program said this" {
		t.Errorf("row 0 = %q, want the program's own output", got)
	}
}

// TestAskMarksTheSelectedChoice checks the question is drawn in the
// terminal's palette and marks the choice Enter would pick the way the
// rest of the toolkit does: the colours swapped.
func TestAskMarksTheSelectedChoice(t *testing.T) {
	term, _, _, _ := askTerm(t, 50, 4)
	pal := vt.DefaultPalette()

	g := draw(term, 50, 4)

	yes, _ := choiceSpan(t, g, 3, askYes)
	no, _ := choiceSpan(t, g, 3, askNo)
	if got := g.At(yes+1, 3).BG; got != pal.FG {
		t.Errorf("the selected choice's background is %v, want the palette's foreground %v", got, pal.FG)
	}
	if got := g.At(no+1, 3).BG; got != pal.ANSI[0] {
		t.Errorf("an unpicked choice's background is %v, want the palette's %v", got, pal.ANSI[0])
	}
}

func TestAskEnterRunsTheSelectedChoice(t *testing.T) {
	term, _, yes, no := askTerm(t, 50, 4)

	if err := press(t, term, input.KeyEnter); err != nil {
		t.Fatalf("Enter: %v", err)
	}

	if yes.runs != 1 {
		t.Errorf("the selected choice ran %d times, want once", yes.runs)
	}
	if no.runs != 0 {
		t.Errorf("the other choice ran %d times, want none", no.runs)
	}
}

func TestAskArrowsAndTabMoveTheSelection(t *testing.T) {
	cases := []struct {
		name string
		keys []input.Key
		want string
	}{
		{"right moves on", []input.Key{input.KeyRight}, askNo},
		{"tab moves on", []input.Key{input.KeyTab}, askNo},
		{"right stops at the last", []input.Key{input.KeyRight, input.KeyRight, input.KeyRight, input.KeyRight}, askNo},
		{"left comes back", []input.Key{input.KeyRight, input.KeyLeft}, askYes},
		{"left stops at the first", []input.Key{input.KeyLeft}, askYes},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			term, _, yes, no := askTerm(t, 50, 4)
			for _, k := range c.keys {
				if err := press(t, term, k); err != nil {
					t.Fatalf("%v: %v", k, err)
				}
			}

			if err := press(t, term, input.KeyEnter); err != nil {
				t.Fatalf("Enter: %v", err)
			}

			got := askYes
			if no.runs > 0 {
				got = askNo
			}
			if yes.runs+no.runs != 1 {
				t.Fatalf("choices ran %d and %d times, want one of them once", yes.runs, no.runs)
			}
			if got != c.want {
				t.Errorf("Enter picked %q, want %q", got, c.want)
			}
		})
	}
}

// TestAskClickPicksTheChoiceUnderIt checks both ends of each choice, so
// a click is worked out from where the choices were drawn rather than
// from a guess at their widths.
func TestAskClickPicksTheChoiceUnderIt(t *testing.T) {
	for _, label := range []string{askYes, askNo} {
		for _, end := range []string{"first column", "last column"} {
			t.Run(label+" "+end, func(t *testing.T) {
				term, _, yes, no := askTerm(t, 50, 4)
				g := draw(term, 50, 4)
				first, last := choiceSpan(t, g, 3, label)
				col := first
				if end == "last column" {
					col = last
				}

				clickAt(t, term, col, 3)

				ran, other := yes, no
				if label == askNo {
					ran, other = no, yes
				}
				if ran.runs != 1 {
					t.Errorf("clicking column %d ran %q %d times, want once", col, label, ran.runs)
				}
				if other.runs != 0 {
					t.Errorf("clicking column %d also ran the other choice", col)
				}
			})
		}
	}
}

func TestAskClickElsewhereOnTheRowPicksNothing(t *testing.T) {
	term, _, yes, no := askTerm(t, 50, 4)
	g := draw(term, 50, 4)
	first, _ := choiceSpan(t, g, 3, askYes)

	// The question's own text, and the gap between the two choices.
	clickAt(t, term, 2, 3)
	clickAt(t, term, first-1, 3)

	if yes.runs+no.runs != 0 {
		t.Errorf("a click off the choices ran one: %d and %d", yes.runs, no.runs)
	}
	if term.Asking() != askText {
		t.Errorf("the question is %q, want it still up", term.Asking())
	}
}

func TestAskPickingTakesTheQuestionAway(t *testing.T) {
	term, _, _, _ := askTerm(t, 50, 4)
	if term.Asking() != askText {
		t.Fatalf("the question is %q before anything was picked, want it up", term.Asking())
	}

	if err := press(t, term, input.KeyEnter); err != nil {
		t.Fatalf("Enter: %v", err)
	}

	if term.Asking() != "" {
		t.Errorf("the question is %q, want it gone", term.Asking())
	}
	if row := fullRow(draw(term, 50, 4), 3); strings.Contains(row, askYes) {
		t.Errorf("the last row is %q, want the question gone from it", row)
	}
}

// TestAskAChoiceThatFailedLeavesTheQuestionUp is what lets a reconnect
// that did not work be tried again.
func TestAskAChoiceThatFailedLeavesTheQuestionUp(t *testing.T) {
	term, _, yes, _ := askTerm(t, 50, 4)
	broke := errors.New("the machine did not answer")
	yes.err = broke

	err := press(t, term, input.KeyEnter)

	if !errors.Is(err, broke) {
		t.Errorf("HandleKey returned %v, want the choice's own error", err)
	}
	if term.Asking() != askText {
		t.Errorf("the question is %q, want it still up", term.Asking())
	}
	if err := press(t, term, input.KeyEnter); !errors.Is(err, broke) {
		t.Errorf("the second try returned %v, want the choice to run again", err)
	}
	if yes.runs != 2 {
		t.Errorf("the choice ran %d times, want twice", yes.runs)
	}
}

func TestAskAgainReplacesTheQuestion(t *testing.T) {
	term, _, yes, _ := askTerm(t, 50, 4)
	later := &picker{}

	term.Ask("The machine has gone. Try again?", Choice{Label: "Retry", Do: later.do})

	row := fullRow(draw(term, 50, 4), 3)
	if strings.Contains(row, askYes) || !strings.Contains(row, "Retry") {
		t.Errorf("the last row is %q, want only the second question on it", row)
	}
	if err := press(t, term, input.KeyEnter); err != nil {
		t.Fatalf("Enter: %v", err)
	}
	if later.runs != 1 || yes.runs != 0 {
		t.Errorf("choices ran %d and %d times, want only the second question's", later.runs, yes.runs)
	}
}

func TestAskWithNoQuestionTakesItAway(t *testing.T) {
	term, f, _, _ := askTerm(t, 50, 4)
	f.feed(t, term, "\r\n\r\n\r\nthe last row is the program's")
	if row := fullRow(draw(term, 50, 4), 3); !strings.Contains(row, askText) {
		t.Fatalf("the last row is %q before the question went, want the question on it", row)
	}

	term.Ask("")

	if term.Asking() != "" {
		t.Errorf("the question is %q, want it gone", term.Asking())
	}
	if got := rowText(draw(term, 50, 4), 3); got != "the last row is the program's" {
		t.Errorf("row 3 = %q, want the program's own output back", got)
	}
}

func TestAskSurvivesAResize(t *testing.T) {
	term, f, _, no := askTerm(t, 50, 4)
	endProgram(t, term, f)

	term.Layout(ui.Size{Cols: 60, Rows: 9})

	g := draw(term, 60, 9)
	if row := fullRow(g, 8); !strings.Contains(row, askText) {
		t.Errorf("the new last row is %q, want the question on it", row)
	}
	first, _ := choiceSpan(t, g, 8, askNo)
	clickAt(t, term, first, 8)
	if no.runs != 1 {
		t.Errorf("a click on the new last row ran the choice %d times, want once", no.runs)
	}
}

func TestRestartClearsTheQuestion(t *testing.T) {
	term, f, _, _ := askTerm(t, 50, 4)
	endProgram(t, term, f)
	if term.Asking() != askText {
		t.Fatalf("the question is %q before the restart, want it up", term.Asking())
	}

	restart(t, term)

	if term.Asking() != "" {
		t.Errorf("the question is %q, want a live pane to have none", term.Asking())
	}
	if row := fullRow(draw(term, 50, 4), 3); strings.Contains(row, askText) {
		t.Errorf("the last row is %q, want the question gone", row)
	}
}

// TestAskIsNotInTheTranscript checks the question is the window talking
// over the screen, not a line the program printed: it is not in what a
// reader is given, and scrolling back leaves it where it is.
func TestAskIsNotInTheTranscript(t *testing.T) {
	term, f, _, _ := askTerm(t, 50, 4)
	for i := range 12 {
		f.feed(t, term, "line "+string(rune('a'+i))+"\r\n")
	}

	if got := term.ReadLines(40).Text; strings.Contains(got, askText) {
		t.Errorf("the transcript has the question in it:\n%s", got)
	}

	term.ScrollView(6)
	g := draw(term, 50, 4)
	if row := fullRow(g, 3); !strings.Contains(row, askText) {
		t.Errorf("after scrolling the last row is %q, want the question still on it", row)
	}
	if got := rowText(g, 0); !strings.Contains(got, "line") {
		t.Errorf("row 0 = %q, want a line from the scrollback", got)
	}
}

// TestAskTakesTheKeysFromTheProgram checks nothing typed at a question
// reaches the session under it.
func TestAskTakesTheKeysFromTheProgram(t *testing.T) {
	term, f, _, _ := askTerm(t, 50, 4)

	for _, ev := range []input.Event{
		{Kind: input.Text, Rune: 'a', NormalText: true},
		{Kind: input.KeyPress, Key: input.KeyLeft},
		{Kind: input.KeyPress, Key: input.KeyEscape},
		{Kind: input.KeyPress, Key: input.KeyC, Mods: input.ModCtrl},
	} {
		if _, err := term.HandleKey(ev); err != nil {
			t.Fatalf("HandleKey %+v: %v", ev, err)
		}
	}
	if term.Asking() != askText {
		t.Errorf("the question is %q, want Escape to have left it alone", term.Asking())
	}

	// A mark typed once the question has gone. The queue keeps its
	// order, so anything the keys above sent would be in front of it.
	term.Ask("")
	if _, err := term.HandleKey(input.Event{Kind: input.Text, Rune: 'z', NormalText: true}); err != nil {
		t.Fatalf("HandleKey: %v", err)
	}

	waitFor(t, func() bool { return f.sentText() != "" })
	if got := f.sentText(); got != "z" {
		t.Errorf("the session was sent %q, want only the key typed after the question went", got)
	}
}

// screenWatcher is somebody looking at this terminal from another
// machine, keeping everything it was sent.
type screenWatcher struct {
	mu   sync.Mutex
	sent string
}

func (w *screenWatcher) Screen(p []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.sent = string(p)
	return nil
}

func (w *screenWatcher) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.sent += string(p)
	return len(p), nil
}

func (w *screenWatcher) Ended() {}

func (w *screenWatcher) text() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.sent
}

// TestAskIsNotSentToAWatcher checks the question is this window talking
// to this user: somebody on another machine is shown the program, not
// the remark this window is making about it.
func TestAskIsNotSentToAWatcher(t *testing.T) {
	term, f, _, _ := askTerm(t, 50, 4)
	f.feed(t, term, "the program said this")
	if term.Asking() != askText {
		t.Fatalf("the question is %q, want it up before anyone watches", term.Asking())
	}

	w := &screenWatcher{}
	stop, err := term.Watch(w)
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	defer stop()
	f.feed(t, term, "\r\nand this")
	draw(term, 50, 4)

	got := w.text()
	for _, unwanted := range []string{askText, askYes, askNo} {
		if strings.Contains(got, unwanted) {
			t.Errorf("a watcher was sent %q, which is this window's question:\n%q", unwanted, got)
		}
	}
	if !strings.Contains(got, "the program said this") {
		t.Errorf("a watcher was not sent the program's own output:\n%q", got)
	}
}
