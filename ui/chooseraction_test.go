package ui

import (
	"testing"

	"github.com/marrasen/gridterm/input"
)

// aChooserWithActions is a chooser of three lines and three buttons,
// recording which button ran on which line.
func aChooserWithActions(t *testing.T) (*Chooser, *[]string, *bool) {
	t.Helper()
	var ran []string
	gone := false
	c := NewChooser("Secrets", func() { gone = true })
	c.Filter = true
	names := []string{"alpha", "beta", "gamma"}
	for _, label := range []string{"Type", "Copy", "Show"} {
		c.Actions = append(c.Actions, ChooserAction{Label: label, Do: func(i int) error {
			ran = append(ran, label+":"+names[i])
			return nil
		}})
	}
	for _, n := range names {
		c.Add(n, "", func() error {
			ran = append(ran, "took:"+n)
			return nil
		})
	}
	c.Layout(Size{Cols: 60, Rows: 24})
	return c, &ran, &gone
}

// key is a plain key press.
func key(k input.Key) input.Event {
	return input.Event{Kind: input.KeyPress, Key: k}
}

// Enter runs the button the arrows have landed on, against the line the
// bar is on.
func TestEnterRunsTheHighlightedAction(t *testing.T) {
	c, ran, gone := aChooserWithActions(t)

	// Down to the second line, right to the second button.
	if _, err := c.HandleKey(key(input.KeyDown)); err != nil {
		t.Fatalf("down: %v", err)
	}
	if _, err := c.HandleKey(key(input.KeyRight)); err != nil {
		t.Fatalf("right: %v", err)
	}
	if _, err := c.HandleKey(key(input.KeyEnter)); err != nil {
		t.Fatalf("enter: %v", err)
	}

	if len(*ran) != 1 || (*ran)[0] != "Copy:beta" {
		t.Errorf("it ran %v, want the second button on the second line", *ran)
	}
	if !*gone {
		t.Error("the chooser stayed up after a button ran")
	}
}

// With buttons, Enter does not take the line the old way.
func TestEnterDoesNotTakeTheLineWhenThereAreButtons(t *testing.T) {
	c, ran, _ := aChooserWithActions(t)

	if _, err := c.HandleKey(key(input.KeyEnter)); err != nil {
		t.Fatalf("enter: %v", err)
	}
	if len(*ran) != 1 || (*ran)[0] != "Type:alpha" {
		t.Errorf("it ran %v, want the first button on the first line", *ran)
	}
}

// The arrows wrap, so the bar goes round rather than sticking at an end.
func TestTheButtonsWrapRound(t *testing.T) {
	c, ran, _ := aChooserWithActions(t)

	// Left from the first lands on the last.
	if _, err := c.HandleKey(key(input.KeyLeft)); err != nil {
		t.Fatalf("left: %v", err)
	}
	if _, err := c.HandleKey(key(input.KeyEnter)); err != nil {
		t.Fatalf("enter: %v", err)
	}
	if len(*ran) != 1 || (*ran)[0] != "Show:alpha" {
		t.Errorf("it ran %v, want the last button", *ran)
	}
}

// A chooser with no buttons is the list it always was: Enter takes the
// line.
func TestWithoutButtonsEnterStillTakesTheLine(t *testing.T) {
	var ran []string
	c := NewChooser("Panes", nil)
	c.Add("one", "", func() error { ran = append(ran, "took:one"); return nil })
	c.Layout(Size{Cols: 40, Rows: 12})

	if _, err := c.HandleKey(key(input.KeyEnter)); err != nil {
		t.Fatalf("enter: %v", err)
	}
	if len(ran) != 1 || ran[0] != "took:one" {
		t.Errorf("it ran %v, want the line taken", ran)
	}
}

// Typing narrows the list, and Enter then runs the button on what is
// left rather than on the line that was first before.
func TestTypingNarrowsAndTheButtonFollows(t *testing.T) {
	c, ran, _ := aChooserWithActions(t)

	for _, r := range "gam" {
		if _, err := c.HandleKey(input.Event{Kind: input.Text, Rune: r}); err != nil {
			t.Fatalf("type %q: %v", r, err)
		}
	}
	if got := c.Query(); got != "gam" {
		t.Fatalf("the filter holds %q", got)
	}
	if _, err := c.HandleKey(key(input.KeyEnter)); err != nil {
		t.Fatalf("enter: %v", err)
	}
	if len(*ran) != 1 || (*ran)[0] != "Type:gamma" {
		t.Errorf("it ran %v, want the button on the only line left", *ran)
	}
}

// The buttons and the filter row take room, so the box is taller than
// the same list without them.
func TestTheButtonsAndFilterTakeRoom(t *testing.T) {
	plain := NewChooser("Panes", nil)
	plain.Add("one", "", func() error { return nil })
	plain.Layout(Size{Cols: 60, Rows: 24})

	full, _, _ := aChooserWithActions(t)

	if plain.Box().Empty() || full.Box().Empty() {
		t.Fatal("one of them has nowhere to draw")
	}
	// Three lines rather than one is two rows; the filter is one more
	// and the bar with its blank row is two.
	if got, want := full.Box().Rows-plain.Box().Rows, 2+1+2; got != want {
		t.Errorf("the full chooser is %d rows taller, want %d", got, want)
	}
}
