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

// Shift and a page key carry the loose end of the selection a screenful
// at a time, the way shift and Down carries it a line.
func TestShiftAndPageDownPicksOutAScreenful(t *testing.T) {
	lines := make([]string, 100)
	for i := range lines {
		lines[i] = "a line of text"
	}
	r := aReaderOf(t, 40, 12, lines...)

	chordKey(t, r, input.KeyPageDown, input.ModShift)

	// Ten rows of the file: twelve less the name and the bar.
	if got, want := len(strings.Split(r.SelectedText(), "\n")), 11; got != want {
		t.Errorf("it picked out %d lines, want %d", got, want)
	}
}

// Shift and PageUp carries it back again, so a page taken by mistake
// can be given back without starting over.
func TestShiftAndPageUpCarriesTheSelectionBack(t *testing.T) {
	lines := make([]string, 100)
	for i := range lines {
		lines[i] = "a line of text"
	}
	r := aReaderOf(t, 40, 12, lines...)
	chordKey(t, r, input.KeyPageDown, input.ModShift)
	chordKey(t, r, input.KeyPageDown, input.ModShift)
	was := len(r.SelectedText())

	chordKey(t, r, input.KeyPageUp, input.ModShift)

	if got := len(r.SelectedText()); got >= was {
		t.Errorf("the selection is %d characters after going back, was %d", got, was)
	}
}

// The reader claims shift and a page key so the window's scroll
// shortcut does not run instead. It claims nothing else.
func TestTheReaderClaimsOnlyShiftAndAPageKey(t *testing.T) {
	r := aReaderOf(t, 40, 12, "hello there", "second line")
	claims := func(key input.Key, mods input.Mods) bool {
		return r.ClaimsChord(input.Event{Kind: input.KeyPress, Key: key, Mods: mods})
	}

	for _, key := range []input.Key{input.KeyPageUp, input.KeyPageDown} {
		if !claims(key, input.ModShift) {
			t.Errorf("the reader does not claim shift and %v", key)
		}
		if claims(key, 0) {
			t.Errorf("the reader claims a plain %v, which the window scrolls with", key)
		}
		if claims(key, input.ModCtrl) {
			t.Errorf("the reader claims ctrl and %v, which walks the sidebar", key)
		}
	}
	if claims(input.KeyDown, input.ModShift) {
		t.Error("the reader claims shift and Down, which no accelerator has")
	}
}

// A reader with nothing in it claims nothing, so the window still
// scrolls the pane rather than the key doing nothing at all.
func TestAnEmptyReaderClaimsNothing(t *testing.T) {
	r := aReaderOf(t, 40, 12)

	if r.ClaimsChord(input.Event{Kind: input.KeyPress, Key: input.KeyPageDown, Mods: input.ModShift}) {
		t.Error("an empty reader claimed shift and PageDown")
	}
}

// aSlowReader is a reader whose file has not come back yet, so it is
// showing what it shows while a read is out.
func aSlowReader(t *testing.T, cols, rows int) (*Reader, func(lines ...string)) {
	t.Helper()
	r := NewReader("notes.log", "/tmp/notes.log")
	r.Style = readerStyle()
	var answer func([]string, bool, error)
	r.Read = func(then func([]string, bool, error)) { answer = then }
	r.Layout(ui.Size{Cols: cols, Rows: rows})
	r.Open()
	if !r.Busy() {
		t.Fatal("the reader is not waiting for a read, so this proves nothing")
	}
	return r, func(lines ...string) { answer(lines, false, nil) }
}

// A file still being read says so rather than saying it is empty. A
// few megabytes down an SSH connection takes long enough that "empty"
// reads as the answer rather than as the question still being asked.
func TestAFileStillBeingReadSaysSoRatherThanEmpty(t *testing.T) {
	r, _ := aSlowReader(t, 40, 8)

	g := drawReader(r, 40, 8)

	top := readerRow(g, 0)
	if strings.Contains(top, "empty") {
		t.Errorf("the top row is %q, want it not to call a file it has not read empty", top)
	}
	if !strings.Contains(top, "reading") {
		t.Errorf("the top row is %q, want it to say the file is being read", top)
	}
}

// It says how big the file is when whoever opened it knew, so the user
// can tell a wait of a second from a wait of a minute.
func TestItSaysHowBigTheFileBeingReadIs(t *testing.T) {
	r, _ := aSlowReader(t, 40, 8)
	r.Expect = 4_400_000

	g := drawReader(r, 40, 8)

	if top := readerRow(g, 0); !strings.Contains(top, "4.2 MB") {
		t.Errorf("the top row is %q, want it to say how big the file is", top)
	}
}

// With no size to quote it still says it is reading. Not every way in
// knows how big the file is.
func TestWithNoSizeItStillSaysItIsReading(t *testing.T) {
	r, _ := aSlowReader(t, 40, 8)

	g := drawReader(r, 40, 8)

	top := readerRow(g, 0)
	if !strings.Contains(top, "reading") {
		t.Errorf("the top row is %q, want it to say the file is being read", top)
	}
	if strings.Contains(top, "0 B") {
		t.Errorf("the top row is %q, want no size rather than a made-up one", top)
	}
}

// A file that really is empty still says so, once it has been read.
func TestAFileThatIsReallyEmptyStillSaysEmpty(t *testing.T) {
	r, answer := aSlowReader(t, 40, 8)
	r.Expect = 0

	answer()

	g := drawReader(r, 40, 8)
	if top := readerRow(g, 0); !strings.Contains(top, "empty") {
		t.Errorf("the top row is %q, want it to say the file is empty", top)
	}
}

// Once the lines arrive the top row goes back to saying where in the
// file the reader is.
func TestOnceTheFileArrivesTheTopRowSaysWhereItIs(t *testing.T) {
	r, answer := aSlowReader(t, 40, 8)
	r.Expect = 4_400_000

	answer("one", "two", "three")

	top := readerRow(drawReader(r, 40, 8), 0)
	if strings.Contains(top, "reading") {
		t.Errorf("the top row is %q, want it to have stopped saying it is reading", top)
	}
	if !strings.Contains(top, "1-3 of 3") {
		t.Errorf("the top row is %q, want it to say where in the file it is", top)
	}
}

// A picture that has not come back says it is being read too, rather
// than leaving the corner blank.
func TestAPictureStillBeingReadSaysSo(t *testing.T) {
	r := NewReader("shot.png", "/tmp/shot.png")
	r.Style = readerStyle()
	r.ReadPic = func(then func(Pic, error)) {}
	r.Expect = 2_200_000
	r.Layout(ui.Size{Cols: 40, Rows: 8})
	r.Open()

	top := readerRow(drawReader(r, 40, 8), 0)

	if !strings.Contains(top, "reading") {
		t.Errorf("the top row is %q, want it to say the picture is being read", top)
	}
}
