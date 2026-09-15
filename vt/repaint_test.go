package vt

import (
	"image/color"
	"strings"
	"testing"

	"github.com/marrasen/gridterm/grid"
)

// drawn runs text through an emulator and gives back its screen.
func drawn(t *testing.T, cols, rows int, text string) *grid.Grid {
	t.Helper()
	term := New(cols, rows, DefaultPalette(), 100, Callbacks{})
	if _, err := term.Write([]byte(text)); err != nil {
		t.Fatalf("write: %v", err)
	}
	g := grid.New(cols, rows, DefaultPalette().FG, DefaultPalette().BG)
	term.Render(g)
	return g
}

// A screen written as escape sequences and read back again is the same
// screen. That is the whole job: somebody who was not watching it being
// drawn is shown what is already there.
func TestAScreenSurvivesBeingWrittenAndReadBack(t *testing.T) {
	for _, text := range []string{
		"hello",
		"one\r\ntwo\r\nthree",
		"\x1b[31mred\x1b[0m and \x1b[1mbold\x1b[0m",
		"\x1b[44;33mon blue\x1b[0m",
		"\x1b[4munderlined\x1b[0m \x1b[7mreversed\x1b[0m",
		"tab\there",
		"\x1b[2;5Hplaced",
		"漢字 and text",
		"e\u0301clair",
	} {
		was := drawn(t, 20, 5, text)

		again := drawn(t, 20, 5, Repaint(was, Screenful{Wrap: true}))

		if diff := gridDiff(was, again); diff != "" {
			t.Errorf("%q came back different:\n%s", text, diff)
		}
	}
}

// The cursor comes back where the program left it.
func TestTheCursorComesBackWhereItWas(t *testing.T) {
	was := drawn(t, 20, 5, "abc\r\nde")

	again := drawn(t, 20, 5, Repaint(was, Screenful{Wrap: true}))

	if got, want := again.Cursor(), was.Cursor(); got.X != want.X || got.Y != want.Y {
		t.Errorf("the cursor came back at %d,%d, want %d,%d", got.X, got.Y, want.X, want.Y)
	}
}

// A hidden cursor stays hidden.
func TestAHiddenCursorStaysHidden(t *testing.T) {
	was := drawn(t, 20, 5, "abc\x1b[?25l")
	if was.Cursor().Visible {
		t.Fatal("the cursor was not hidden to begin with")
	}

	again := drawn(t, 20, 5, Repaint(was, Screenful{Wrap: true}))

	if again.Cursor().Visible {
		t.Error("it came back visible")
	}
	// And in the right place. A hidden cursor is still somewhere, and
	// the moment the program shows it again without moving it, one left
	// after the last thing written would appear in the wrong place.
	if got, want := again.Cursor(), was.Cursor(); got.X != want.X || got.Y != want.Y {
		t.Errorf("it came back at %d,%d, want %d,%d", got.X, got.Y, want.X, want.Y)
	}
}

// An empty screen is written short: a screenful of spaces costs a
// kilobyte to say nothing.
func TestAnEmptyScreenIsWrittenShort(t *testing.T) {
	g := drawn(t, 80, 24, "")

	if got := len(Repaint(g, Screenful{Wrap: true})); got > 200 {
		t.Errorf("an empty screen took %d bytes", got)
	}
}

// And a full one does not repeat itself: a run of cells drawn the same
// way says so once.
func TestARunOfOneStyleIsSaidOnce(t *testing.T) {
	g := drawn(t, 40, 3, "\x1b[31m"+strings.Repeat("x", 40))

	if got := strings.Count(Repaint(g, Screenful{Wrap: true}), "38;2;"); got != 1 {
		t.Errorf("a row of one colour set it %d times", got)
	}
}

// gridDiff says where two screens differ, or nothing when they match.
func gridDiff(a, b *grid.Grid) string {
	cols, rows := a.Size()
	var out strings.Builder
	for y := 0; y < rows; y++ {
		for x := 0; x < cols; x++ {
			ca, cb := a.At(x, y), b.At(x, y)
			if sameToLookAt(ca, cb) {
				continue
			}
			out.WriteString(
				"at " + itoa(x) + "," + itoa(y) + ": " +
					show(ca) + " became " + show(cb) + "\n")
		}
	}
	return out.String()
}

// sameToLookAt reports whether two cells would be drawn the same.
//
// The foreground of a cell with no character in it is not compared:
// nothing is drawn in it, so a red space and a plain one are the same
// picture, and a screen written out need not carry a colour that will
// never be seen.
func sameToLookAt(a, b grid.Cell) bool {
	if a.Rune != b.Rune || a.BG != b.BG || a.Attr != b.Attr || a.Width != b.Width {
		return false
	}
	// What sits on the letter is part of the picture: without it a word
	// comes back spelled differently.
	if len(a.Comb) != len(b.Comb) {
		return false
	}
	for i, r := range a.Comb {
		if b.Comb[i] != r {
			return false
		}
	}
	if a.Rune == ' ' || a.Rune == 0 {
		return true
	}
	return a.FG == b.FG
}

func show(c grid.Cell) string {
	return string(c.Rune) + " fg" + colourOf(c.FG) + " bg" + colourOf(c.BG) +
		" attr" + itoa(int(c.Attr))
}

func colourOf(c color.RGBA) string {
	return itoa(int(c.R)) + "/" + itoa(int(c.G)) + "/" + itoa(int(c.B))
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// A mark that sits on the letter before it travels with that letter.
//
// It is part of the cell rather than a cell of its own, so a repaint
// that wrote only the letter would drop the accent and leave a word
// spelled differently at the other end.
func TestACombiningMarkComesWithItsLetter(t *testing.T) {
	was := drawn(t, 20, 3, "éclair")

	out := Repaint(was, Screenful{Wrap: true})

	if !strings.ContainsRune(out, '́') {
		t.Errorf("the mark was not written: %q", out)
	}
	again := drawn(t, 20, 3, out)
	if diff := gridDiff(was, again); diff != "" {
		t.Errorf("it came back different:\n%s", diff)
	}
}

// The modes that change what happens to what comes next travel with
// the screen, and are said before anything is drawn.
func TestTheModesTravelWithTheScreen(t *testing.T) {
	g := drawn(t, 20, 3, "hello")

	for _, c := range []struct {
		what Screenful
		want []string
	}{
		{Screenful{Wrap: true}, []string{"\x1b[?7h", "\x1b[?1l"}},
		{Screenful{}, []string{"\x1b[?7l", "\x1b[?1l"}},
		{Screenful{AppCursor: true}, []string{"\x1b[?7l", "\x1b[?1h"}},
	} {
		out := Repaint(g, c.what)
		drawing := strings.Index(out, "\x1b[H\x1b[2J")
		if drawing < 0 {
			t.Fatalf("%+v never cleared the screen: %q", c.what, out)
		}
		for _, want := range c.want {
			at := strings.Index(out, want)
			if at < 0 {
				t.Errorf("%+v did not say %q: %q", c.what, want, out)
				continue
			}
			// Before anything is drawn. Whether a long line wraps
			// changes what the drawing itself does.
			if at > drawing {
				t.Errorf("%+v said %q after it started drawing: %q", c.what, want, out)
			}
		}
	}
}

// A screen showing a full-screen program carries the screen underneath
// it, so the program quitting leaves what was there behind.
//
// Nothing else would send it. The far end restores its own ordinary
// screen when the program leaves the alternate buffer, and a shell
// coming back to its prompt does not redraw.
func TestTheScreenUnderAFullScreenProgramTravels(t *testing.T) {
	// A prompt, then a full-screen program over it.
	term := New(20, 3, DefaultPalette(), 100, Callbacks{})
	if _, err := term.Write([]byte("at-the-prompt\r\n\x1b[?1049hin-the-program")); err != nil {
		t.Fatalf("write: %v", err)
	}
	live := grid.New(20, 3, DefaultPalette().FG, DefaultPalette().BG)
	term.RenderLive(live)
	under := grid.New(20, 3, DefaultPalette().FG, DefaultPalette().BG)
	if !term.RenderUnder(under) {
		t.Fatal("it says there is no screen underneath")
	}
	full := term.Screenful()
	full.Under = under
	if !full.Alt {
		t.Fatal("it is not in the alternate buffer")
	}

	// Read back by an emulator that was not watching.
	watcher := New(20, 3, DefaultPalette(), 100, Callbacks{})
	if _, err := watcher.Write([]byte(Repaint(live, full))); err != nil {
		t.Fatalf("watcher: %v", err)
	}
	shown := grid.New(20, 3, DefaultPalette().FG, DefaultPalette().BG)
	watcher.RenderLive(shown)
	if got := textOf(shown); !strings.Contains(got, "in-the-program") {
		t.Errorf("the program's screen did not arrive: %q", got)
	}

	// The program quits, on both.
	if _, err := term.Write([]byte("\x1b[?1049l")); err != nil {
		t.Fatalf("quit: %v", err)
	}
	if _, err := watcher.Write([]byte("\x1b[?1049l")); err != nil {
		t.Fatalf("watcher quit: %v", err)
	}
	was := grid.New(20, 3, DefaultPalette().FG, DefaultPalette().BG)
	term.RenderLive(was)
	watcher.RenderLive(shown)
	if diff := gridDiff(was, shown); diff != "" {
		t.Errorf("what was underneath came back different:\n%s", diff)
	}
	if got := textOf(shown); !strings.Contains(got, "at-the-prompt") {
		t.Errorf("the prompt did not come back: %q", got)
	}
}

// A cursor that has filled the last column owes a wrap, and the wrap
// travels with it.
//
// Sent as an ordinary cursor move, the next character would land on top
// of the last one instead of at the start of the next row, and
// everything after it would be a column out.
func TestAWrapThatIsOwedTravels(t *testing.T) {
	// Exactly one row's worth, so the wrap is owed and not yet taken.
	term := New(6, 3, DefaultPalette(), 100, Callbacks{})
	if _, err := term.Write([]byte("abcdef")); err != nil {
		t.Fatalf("write: %v", err)
	}
	live := grid.New(6, 3, DefaultPalette().FG, DefaultPalette().BG)
	term.RenderLive(live)
	full := term.Screenful()
	if !full.WrapNext {
		t.Fatal("no wrap was owed to begin with")
	}

	watcher := New(6, 3, DefaultPalette(), 100, Callbacks{})
	if _, err := watcher.Write([]byte(Repaint(live, full))); err != nil {
		t.Fatalf("watcher: %v", err)
	}

	// One more character, on both.
	if _, err := term.Write([]byte("g")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := watcher.Write([]byte("g")); err != nil {
		t.Fatalf("watcher: %v", err)
	}
	was := grid.New(6, 3, DefaultPalette().FG, DefaultPalette().BG)
	term.RenderLive(was)
	shown := grid.New(6, 3, DefaultPalette().FG, DefaultPalette().BG)
	watcher.RenderLive(shown)
	if diff := gridDiff(was, shown); diff != "" {
		t.Errorf("the next character landed elsewhere:\n%s", diff)
	}
}

// textOf is what a grid is showing, as one string per row.
func textOf(g *grid.Grid) string {
	cols, rows := g.Size()
	var b strings.Builder
	for y := 0; y < rows; y++ {
		for x := 0; x < cols; x++ {
			if r := g.At(x, y).Rune; r != 0 {
				b.WriteRune(r)
			}
		}
		b.WriteByte('\n')
	}
	return b.String()
}
