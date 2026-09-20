package files

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marrasen/gridterm/input"
	"github.com/marrasen/gridterm/ui"
	"github.com/marrasen/gridterm/vfs"
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

// A read says how far it has got as it goes, so a file coming down a
// slow connection shows it is arriving rather than only that it
// started.
func TestAWatchedReadSaysHowFarItHasGot(t *testing.T) {
	dir := t.TempDir()
	at := filepath.Join(dir, "big.log")
	body := strings.Repeat("a line of text\n", 20000)
	if err := os.WriteFile(at, []byte(body), 0o600); err != nil {
		t.Fatalf("write the file: %v", err)
	}

	var seen []int64
	lines, cut, err := ReadFileWatched(vfs.NewLocal(), at, func(read int64) {
		seen = append(seen, read)
	})

	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if cut {
		t.Fatal("the file was cut, so the counts are not the whole of it")
	}
	if len(lines) != 20000 {
		t.Fatalf("it read %d lines, want 20000", len(lines))
	}
	if len(seen) < 2 {
		t.Fatalf("the watcher was told %d times, want it told as the read went", len(seen))
	}
	for i := 1; i < len(seen); i++ {
		if seen[i] <= seen[i-1] {
			t.Fatalf("the count went %d then %d, want it only to grow", seen[i-1], seen[i])
		}
	}
	if got, want := seen[len(seen)-1], int64(len(body)); got != want {
		t.Errorf("the last count was %d, want the whole file at %d", got, want)
	}
}

// A read nobody is watching still works, which is what ReadFile is.
func TestAReadNobodyIsWatchingStillWorks(t *testing.T) {
	dir := t.TempDir()
	at := filepath.Join(dir, "small.txt")
	if err := os.WriteFile(at, []byte("one\ntwo\n"), 0o600); err != nil {
		t.Fatalf("write the file: %v", err)
	}

	lines, _, err := ReadFile(vfs.NewLocal(), at)

	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(lines) != 2 {
		t.Errorf("it read %d lines, want 2", len(lines))
	}
}

// What the pane says while a read is arriving: how far of how much.
func TestItSaysHowFarOfHowMuchHasArrived(t *testing.T) {
	r, _ := aSlowReader(t, 44, 8)
	r.Expect = 4_400_000

	r.ReadSoFar(2_000_000)

	if got := readerRow(drawReader(r, 44, 8), 0); !strings.Contains(got, "1.9 of 4.2 MB") {
		t.Errorf("the top row is %q, want it to say how far of how much", got)
	}
}

// Both halves are written in the same unit, so the two numbers can be
// read against each other at a glance.
func TestBothHalvesOfTheProgressAreInOneUnit(t *testing.T) {
	r, _ := aSlowReader(t, 60, 8)
	r.Expect = 900_000_000

	r.ReadSoFar(120_000_000)

	got := readerRow(drawReader(r, 60, 8), 0)
	if !strings.Contains(got, "114 of 858 MB") {
		t.Errorf("the top row is %q, want both numbers in megabytes", got)
	}
}

// A file that grew since it was listed reads past its own size. The
// total is dropped rather than shown as less than what has arrived.
func TestAFileThatGrewDropsTheTotalRatherThanLying(t *testing.T) {
	r, _ := aSlowReader(t, 44, 8)
	r.Expect = 1000

	r.ReadSoFar(5000)

	got := readerRow(drawReader(r, 44, 8), 0)
	if strings.Contains(got, "of 1000 B") {
		t.Errorf("the top row is %q, want it not to claim a total it has passed", got)
	}
	if !strings.Contains(got, "4.9 kB") {
		t.Errorf("the top row is %q, want it to say what has arrived", got)
	}
}

// A count arriving after the read came back is ignored, so a late one
// cannot put the pane back to reading.
func TestACountAfterTheReadCameBackIsIgnored(t *testing.T) {
	r, answer := aSlowReader(t, 44, 8)
	r.Expect = 4_400_000
	answer("one", "two")

	r.ReadSoFar(2_000_000)

	if got := r.SoFar(); got != 0 {
		t.Errorf("the reader took a count of %d after the read came back", got)
	}
	if got := readerRow(drawReader(r, 44, 8), 0); strings.Contains(got, "reading") {
		t.Errorf("the top row is %q, want the read to have finished", got)
	}
}

// Rereading starts the count again rather than carrying the last
// read's total into this one.
func TestARereadStartsTheCountAgain(t *testing.T) {
	r, answer := aSlowReader(t, 44, 8)
	r.ReadSoFar(2_000_000)
	answer("one")

	r.Open()

	if got := r.SoFar(); got != 0 {
		t.Errorf("a reread started at %d bytes, want it to start again", got)
	}
}

// Ctrl+S asks where to put it, filled in with the suggestion, so the
// user edits a path rather than typing one.
func TestCtrlSAsksWhereToSaveIt(t *testing.T) {
	r := aReaderOf(t, 44, 8, "one", "two")
	r.OnSave = func(string, []string) error { return nil }
	r.SaveAs = "/home/marcus/kept.txt"

	chordKey(t, r, input.KeyS, input.ModCtrl)

	what, typed, on := r.Asking()
	if !on {
		t.Fatal("Ctrl+S asked nothing")
	}
	if !strings.Contains(what, "save") {
		t.Errorf("it asked %q, want it to say what it wants", what)
	}
	if typed != "/home/marcus/kept.txt" {
		t.Errorf("it started with %q, want the suggestion filled in", typed)
	}
}

// Answering writes the lines the reader holds, to the path typed.
func TestAnsweringTheSaveQuestionWritesTheLines(t *testing.T) {
	r := aReaderOf(t, 44, 8, "one", "two", "three")
	var at string
	var got []string
	r.OnSave = func(path string, lines []string) error {
		at, got = path, lines
		return nil
	}
	r.SaveAs = "/tmp/kept.txt"
	chordKey(t, r, input.KeyS, input.ModCtrl)

	chordKey(t, r, input.KeyEnter, 0)

	if at != "/tmp/kept.txt" {
		t.Errorf("it saved to %q", at)
	}
	if strings.Join(got, ",") != "one,two,three" {
		t.Errorf("it saved %v, want the lines the reader holds", got)
	}
	if line := readerRow(drawReader(r, 44, 8), 7); !strings.Contains(line, "saved 3 lines") {
		t.Errorf("the bottom row is %q, want it to say what it did", line)
	}
}

// A save that fails says why, on the row the bar was on.
func TestASaveThatFailsSaysWhy(t *testing.T) {
	r := aReaderOf(t, 44, 8, "one")
	r.OnSave = func(string, []string) error { return errors.New("the disk is full") }
	r.SaveAs = "/tmp/kept.txt"
	chordKey(t, r, input.KeyS, input.ModCtrl)

	chordKey(t, r, input.KeyEnter, 0)

	if line := readerRow(drawReader(r, 44, 8), 7); !strings.Contains(line, "the disk is full") {
		t.Errorf("the bottom row is %q, want it to say why", line)
	}
}

// A reader with nowhere to write says so rather than going quiet, and
// its bar does not offer a key that does nothing.
func TestAReaderWithNowhereToWriteSaysSo(t *testing.T) {
	r := aReaderOf(t, 60, 8, "one")

	chordKey(t, r, input.KeyS, input.ModCtrl)

	if _, _, on := r.Asking(); on {
		t.Error("it asked where to save a file that is already a file")
	}
	line := readerRow(drawReader(r, 60, 8), 7)
	if !strings.Contains(line, "nothing to save") {
		t.Errorf("the bottom row is %q, want it to say why", line)
	}
	if strings.Contains(readerRow(drawReader(r, 60, 8), 7), "^S") {
		t.Error("the bar offers Save on a reader with nowhere to write")
	}
}

// The bar offers Save on a reader that has somewhere to write.
func TestTheBarOffersSaveWhenThereIsSomewhereToWrite(t *testing.T) {
	r := aReaderOf(t, 80, 8, "one")
	r.OnSave = func(string, []string) error { return nil }

	if line := readerRow(drawReader(r, 80, 8), 7); !strings.Contains(line, "Save") {
		t.Errorf("the bar is %q, want Save on it", line)
	}
}

// Escape takes the question away without writing anything.
func TestEscapeLeavesTheSaveQuestion(t *testing.T) {
	r := aReaderOf(t, 44, 8, "one")
	saved := false
	r.OnSave = func(string, []string) error { saved = true; return nil }
	chordKey(t, r, input.KeyS, input.ModCtrl)

	chordKey(t, r, input.KeyEscape, 0)

	if _, _, on := r.Asking(); on {
		t.Error("Escape left the question up")
	}
	if saved {
		t.Error("Escape saved the file anyway")
	}
}
