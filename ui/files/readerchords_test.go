package files

import (
	"strings"
	"testing"

	"github.com/marrasen/gridterm/input"
	"github.com/marrasen/gridterm/ui"
)

// chordKey presses a key with whatever modifiers are named.
func chordKey(t *testing.T, r *Reader, key input.Key, mods input.Mods) {
	t.Helper()
	if _, err := r.HandleKey(input.Event{Kind: input.KeyPress, Key: key, Mods: mods}); err != nil {
		t.Fatalf("key %v with %v: %v", key, mods, err)
	}
}

// Ctrl+X says why it did nothing. Ctrl+C copies here, so it is the next
// thing a hand tries, and a key that goes quiet reads as a broken one.
func TestCtrlXSaysThereIsNothingToCut(t *testing.T) {
	r := aReaderOf(t, 60, 8, "hello there", "second line")

	chordKey(t, r, input.KeyX, input.ModCtrl)

	g := drawReader(r, 60, 8)
	if got := readerRow(g, 7); !strings.Contains(got, "nothing to cut") {
		t.Errorf("the bottom row is %q, want it to say there is nothing to cut", got)
	}
}

// What Ctrl+X says goes as soon as the user presses anything else.
func TestTheNextKeyTakesTheCutMessageAway(t *testing.T) {
	r := aReaderOf(t, 60, 8, "hello there", "second line")
	chordKey(t, r, input.KeyX, input.ModCtrl)

	chordKey(t, r, input.KeyDown, 0)

	g := drawReader(r, 60, 8)
	if got := readerRow(g, 7); strings.Contains(got, "nothing to cut") {
		t.Errorf("the bottom row is still %q after another key", got)
	}
}

// The window opens its help on Ctrl+Shift+H and splits the pane on
// Ctrl+Shift+D. A reader must not act on either: the bar offers Ctrl+H
// and Ctrl+D, and Ctrl+Shift is not Ctrl.
func TestTheReaderDeclinesAChordTheBarNeverOffered(t *testing.T) {
	both := input.ModCtrl | input.ModShift
	cases := []struct {
		what string
		key  input.Key
		mods input.Mods
	}{
		{"Ctrl+Shift+H", input.KeyH, both},
		{"Ctrl+Alt+H", input.KeyH, input.ModCtrl | input.ModAlt},
		{"Ctrl+Shift+R", input.KeyR, both},
		{"Ctrl+Shift+F", input.KeyF, both},
		{"Ctrl+Shift+D", input.KeyD, both},
		{"Ctrl+Shift+A", input.KeyA, both},
	}
	for _, tc := range cases {
		t.Run(tc.what, func(t *testing.T) {
			reads, closed, copied := 0, false, ""
			r := NewReader("notes.txt", "/tmp/notes.txt")
			r.Style = readerStyle()
			r.Read = func(then func([]string, bool, error)) {
				reads++
				then([]string{"hello there", "second line"}, false, nil)
			}
			r.OnClose = func() { closed = true }
			r.OnCopy = func(text string) { copied = text }
			r.Layout(ui.Size{Cols: 60, Rows: 8})
			r.Open()
			was := reads

			chordKey(t, r, tc.key, tc.mods)

			if r.Hexed() {
				t.Error("it turned hex on")
			}
			if r.Following() {
				t.Error("it started following the file")
			}
			if reads != was {
				t.Errorf("it read the file %d more times", reads-was)
			}
			if closed {
				t.Error("it closed the reader")
			}
			if r.SelectedText() != "" || copied != "" {
				t.Errorf("it picked out %q and copied %q", r.SelectedText(), copied)
			}
		})
	}
}

// Ctrl+Shift+C is the window's copy, and the reader has its own Ctrl+C.
// Text already picked out must not go to the reader's clipboard as well.
func TestCtrlShiftCDoesNotCopyTwice(t *testing.T) {
	r := aReaderOf(t, 60, 8, "hello there", "second line")
	copied := ""
	r.OnCopy = func(text string) { copied = text }
	drag(t, r, 0, 1, 4, 1)

	chordKey(t, r, input.KeyC, input.ModCtrl|input.ModShift)

	if copied != "" {
		t.Errorf("the reader copied %q on the window's own copy key", copied)
	}
}

// The same rule on a picture, which has its own three keys.
func TestAPictureDeclinesAChordTheBarNeverOffered(t *testing.T) {
	r := aPictureFile(t, 64, 64, 40, 10)

	chordKey(t, r, input.KeyH, input.ModCtrl|input.ModShift)

	if !r.ShowsAPicture() {
		t.Error("Ctrl+Shift+H showed the picture as bytes")
	}
}

// The keys the bar does offer still work, so the rule above did not
// take the reader's own chords with it.
func TestThePlainCtrlKeysStillWork(t *testing.T) {
	r := aReaderOf(t, 60, 8, "hello there", "second line")

	chordKey(t, r, input.KeyH, input.ModCtrl)
	if !r.Hexed() {
		t.Error("Ctrl+H did not turn hex on")
	}

	closed := false
	r.OnClose = func() { closed = true }
	chordKey(t, r, input.KeyD, input.ModCtrl)
	if !closed {
		t.Error("Ctrl+D did not close the reader")
	}
}
