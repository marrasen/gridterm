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
	} {
		was := drawn(t, 20, 5, text)

		again := drawn(t, 20, 5, Repaint(was))

		if diff := gridDiff(was, again); diff != "" {
			t.Errorf("%q came back different:\n%s", text, diff)
		}
	}
}

// The cursor comes back where the program left it.
func TestTheCursorComesBackWhereItWas(t *testing.T) {
	was := drawn(t, 20, 5, "abc\r\nde")

	again := drawn(t, 20, 5, Repaint(was))

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

	again := drawn(t, 20, 5, Repaint(was))

	if again.Cursor().Visible {
		t.Error("it came back visible")
	}
}

// An empty screen is written short: a screenful of spaces costs a
// kilobyte to say nothing.
func TestAnEmptyScreenIsWrittenShort(t *testing.T) {
	g := drawn(t, 80, 24, "")

	if got := len(Repaint(g)); got > 200 {
		t.Errorf("an empty screen took %d bytes", got)
	}
}

// And a full one does not repeat itself: a run of cells drawn the same
// way says so once.
func TestARunOfOneStyleIsSaidOnce(t *testing.T) {
	g := drawn(t, 40, 3, "\x1b[31m"+strings.Repeat("x", 40))

	if got := strings.Count(Repaint(g), "38;2;"); got != 1 {
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
