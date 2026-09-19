package files

import (
	"errors"
	"image/color"
	"strconv"
	"strings"
	"testing"

	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/input"
	"github.com/marrasen/gridterm/ui"
)

// readerStyle is colours a test can tell apart.
func readerStyle() Style {
	return Style{
		FG:       color.RGBA{R: 0xc0, G: 0xc0, B: 0xc0, A: 0xff},
		BG:       color.RGBA{R: 0x10, G: 0x10, B: 0x10, A: 0xff},
		HeaderFG: color.RGBA{G: 0xff, A: 0xff},
		NoteFG:   color.RGBA{R: 0x80, G: 0x80, B: 0x80, A: 0xff},
		ErrorFG:  color.RGBA{R: 0xff, A: 0xff},
		KeyFG:    color.RGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff},
		OffBG:    color.RGBA{R: 0x30, G: 0x30, B: 0x30, A: 0xff},
	}
}

// aReader is a reader holding n numbered lines, laid out and drawn once.
func aReader(t *testing.T, n, cols, rows int) *Reader {
	t.Helper()
	lines := make([]string, n)
	for i := range lines {
		lines[i] = "line " + strings.Repeat("x", i%3) + strconv.Itoa(i+1)
	}
	r := NewReader("notes.txt", "/tmp/notes.txt")
	r.Style = readerStyle()
	r.Read = func(then func([]string, bool, error)) { then(lines, false, nil) }
	r.Layout(ui.Size{Cols: cols, Rows: rows})
	r.Open()
	return r
}

// drawReader paints a reader onto a grid and returns it.
func drawReader(r *Reader, cols, rows int) *grid.Grid {
	g := grid.New(cols, rows, color.RGBA{}, color.RGBA{})
	r.Layout(ui.Size{Cols: cols, Rows: rows})
	r.Draw(g.View())
	return g
}

// readerRow reads one row of a drawn reader back as a string.
func readerRow(g *grid.Grid, y int) string {
	cols, _ := g.Size()
	return strings.TrimRight(rowText(g, y, cols), " ")
}

// A reader shows the first screenful of the file, under its name.
func TestAReaderShowsTheFirstScreenful(t *testing.T) {
	r := aReader(t, 100, 40, 10)

	g := drawReader(r, 40, 10)

	if got := readerRow(g, 0); !strings.HasPrefix(got, "notes.txt") {
		t.Errorf("the top row is %q, want the file's name", got)
	}
	// Eight rows of the file: ten less the name and the bar.
	if got := readerRow(g, 1); !strings.HasPrefix(got, "line 1") {
		t.Errorf("the first line is %q, want the first line of the file", got)
	}
	if got := readerRow(g, 8); !strings.HasPrefix(got, "line ") {
		t.Errorf("the last row above the bar is %q, want a line of the file", got)
	}
	if got := readerRow(g, 9); !strings.Contains(got, "Close") {
		t.Errorf("the bottom row is %q, want the bar of keys", got)
	}
}

// Scrolling moves through the file and stops at both ends.
func TestScrollingStopsAtBothEnds(t *testing.T) {
	r := aReader(t, 100, 40, 10)

	r.Scroll(-5)
	if got := r.Top(); got != 0 {
		t.Errorf("scrolling up from the top landed on line %d, want the first", got)
	}

	r.ScrollPages(1)
	if got := r.Top(); got != 8 {
		t.Errorf("a page down landed on line %d, want the eight rows it shows", got)
	}

	r.End()
	if got, want := r.Top(), 100-8; got != want {
		t.Errorf("the end is line %d, want %d: the last screenful, not the last line", got, want)
	}
	r.Scroll(50)
	if got, want := r.Top(), 100-8; got != want {
		t.Errorf("scrolling past the end landed on %d, want %d", got, want)
	}
}

// A file shorter than the screen does not scroll at all.
func TestAShortFileDoesNotScroll(t *testing.T) {
	r := aReader(t, 3, 40, 10)

	r.End()

	if got := r.Top(); got != 0 {
		t.Errorf("the end of a three-line file is line %d, want the first", got)
	}
	if !r.AtEnd() {
		t.Error("a file that fits is not at its end")
	}
}

// The top row says where in the file the reader is.
func TestTheTopRowSaysWhereInTheFileThisIs(t *testing.T) {
	r := aReader(t, 100, 40, 10)

	g := drawReader(r, 40, 10)

	if got := readerRow(g, 0); !strings.Contains(got, "1-8 of 100") {
		t.Errorf("the top row is %q, want it to say which lines these are", got)
	}
}

// A file longer than the reader holds says so, rather than pretending
// the file ends where the reading stopped.
func TestAFileLongerThanTheReaderHoldsSaysSo(t *testing.T) {
	r := NewReader("big.log", "/tmp/big.log")
	r.Style = readerStyle()
	r.Read = func(then func([]string, bool, error)) { then([]string{"one", "two"}, true, nil) }
	r.Layout(ui.Size{Cols: 40, Rows: 10})
	r.Open()

	g := drawReader(r, 40, 10)

	if !r.Cut() {
		t.Error("the reader does not know the file was cut")
	}
	if got := readerRow(g, 0); !strings.Contains(got, "+") {
		t.Errorf("the top row is %q, want it to say there is more of the file", got)
	}
}

// A file that could not be read says why, in place of the lines.
func TestAFileThatWouldNotReadSaysWhy(t *testing.T) {
	r := NewReader("gone.txt", "/tmp/gone.txt")
	r.Style = readerStyle()
	r.Read = func(then func([]string, bool, error)) {
		then(nil, false, errors.New("no such file"))
	}
	r.Layout(ui.Size{Cols: 40, Rows: 10})
	r.Open()

	g := drawReader(r, 40, 10)

	if r.Err() == nil {
		t.Fatal("the reader kept no reason")
	}
	if got := readerRow(g, 1); !strings.Contains(got, "no such file") {
		t.Errorf("the second row is %q, want why the file would not read", got)
	}
}

// A reread keeps what is on screen until the answer arrives, so a pane
// does not blink empty.
func TestARereadKeepsWhatIsOnScreenUntilItAnswers(t *testing.T) {
	r := aReader(t, 100, 40, 10)
	var answer func([]string, bool, error)
	r.Read = func(then func([]string, bool, error)) { answer = then }

	r.Open()

	if got := r.Lines(); got != 100 {
		t.Errorf("a reader waiting on a reread holds %d lines, want the 100 it had", got)
	}
	if !r.Busy() {
		t.Error("a reader waiting on a reread does not say it is busy")
	}
	answer([]string{"one"}, false, nil)
	if got := r.Lines(); got != 1 {
		t.Errorf("the reread left %d lines, want the one it answered with", got)
	}
	if r.Busy() {
		t.Error("the reader is still busy after its answer")
	}
}

// A second reread while one is out is left alone, or a file that reads
// slowly would have one read a frame out on it.
func TestASecondRereadWhileOneIsOutIsLeftAlone(t *testing.T) {
	r := aReader(t, 10, 40, 10)
	asked := 0
	r.Read = func(then func([]string, bool, error)) { asked++ }

	r.Open()
	r.Open()

	if asked != 1 {
		t.Errorf("it asked %d times, want the one read", asked)
	}
}

// Every key a reader does not act on is still swallowed: it is not a
// terminal, and a letter typed into one must not reach a shell.
func TestAReaderSwallowsEveryKey(t *testing.T) {
	r := aReader(t, 100, 40, 10)

	for _, ev := range []input.Event{
		{Kind: input.KeyPress, Key: input.KeyA},
		{Kind: input.KeyPress, Key: input.KeyEnter},
	} {
		took, err := r.HandleKey(ev)
		if err != nil {
			t.Fatalf("%v: %v", ev.Key, err)
		}
		if !took {
			t.Errorf("%v went past the reader", ev.Key)
		}
	}
}

// The keys that move through the file do.
func TestTheKeysMoveThroughTheFile(t *testing.T) {
	r := aReader(t, 100, 40, 10)

	press(t, r, input.KeyDown)
	if got := r.Top(); got != 1 {
		t.Errorf("down went to line %d, want the second", got)
	}
	press(t, r, input.KeyPageDown)
	if got := r.Top(); got != 9 {
		t.Errorf("page down went to line %d, want a screenful on", got)
	}
	press(t, r, input.KeyEnd)
	if !r.AtEnd() {
		t.Error("End did not reach the end")
	}
	press(t, r, input.KeyHome)
	if got := r.Top(); got != 0 {
		t.Errorf("Home went to line %d, want the first", got)
	}
}

// Closing asks whoever owns the pane to put it away.
func TestClosingAsksTheOwner(t *testing.T) {
	r := aReader(t, 10, 40, 10)
	closed := 0
	r.OnClose = func() { closed++ }

	if _, err := r.HandleKey(input.Event{
		Kind: input.KeyPress, Key: input.KeyD, Mods: input.ModCtrl,
	}); err != nil {
		t.Fatalf("close: %v", err)
	}

	if closed != 1 {
		t.Errorf("it asked to close %d times, want once", closed)
	}
}

// A line wider than the pane can be scrolled sideways.
func TestALineWiderThanThePaneScrollsSideways(t *testing.T) {
	r := NewReader("wide.txt", "/tmp/wide.txt")
	r.Style = readerStyle()
	r.Read = func(then func([]string, bool, error)) {
		then([]string{"abcdefghijklmnopqrstuvwxyz"}, false, nil)
	}
	r.Layout(ui.Size{Cols: 10, Rows: 5})
	r.Open()

	r.Sideways(4)
	g := drawReader(r, 10, 5)

	if got := readerRow(g, 1); !strings.HasPrefix(got, "efgh") {
		t.Errorf("the line reads %q, want it moved four columns across", got)
	}
	// And back, no further than the left edge.
	r.Sideways(-99)
	g = drawReader(r, 10, 5)
	if got := readerRow(g, 1); !strings.HasPrefix(got, "abcd") {
		t.Errorf("the line reads %q, want it back at the start", got)
	}
}
