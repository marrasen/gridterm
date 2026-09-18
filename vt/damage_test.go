package vt

import (
	"strings"
	"testing"

	"github.com/marrasen/gridterm/grid"
)

// rowText reads one row of a grid back as text.
func rowText(g *grid.Grid, y int) string {
	cols, _ := g.Size()
	var sb strings.Builder
	for x := range cols {
		c := g.At(x, y)
		if c.Width == 0 {
			continue
		}
		sb.WriteRune(c.Rune)
	}
	return strings.TrimRight(sb.String(), " ")
}

// mark writes a word of its own into every row of a grid, so a row the
// next render skips can be told from one it wrote.
func mark(g *grid.Grid, word string) {
	cols, rows := g.Size()
	for y := range rows {
		for x := range cols {
			c := g.At(x, y)
			if x < len(word) {
				c.Rune = rune(word[x])
			} else {
				c.Rune = ' '
			}
			c.Comb = nil
			c.Width = 1
			g.Set(x, y, c)
		}
	}
}

// Render writes the rows the emulator touched and leaves the rest alone.
//
// A screen where one line changes is most of what a terminal does, and
// writing every cell of it every frame was the cost the rest of the
// window paid for that. The grid a render goes to is that render's own,
// which is what lets a row be left as it was.
func TestRenderWritesOnlyTheRowsThatChanged(t *testing.T) {
	term := New(20, 4, DefaultPalette(), 100, Callbacks{})
	g := grid.New(20, 4, DefaultPalette().FG, DefaultPalette().BG)
	if _, err := term.Write([]byte("one\x1b[2;1Htwo")); err != nil {
		t.Fatal(err)
	}
	term.Render(g)

	// Something else writes over the whole grid, and then one row of the
	// screen changes.
	mark(g, "stale")
	if _, err := term.Write([]byte("\x1b[2;1Htwo again")); err != nil {
		t.Fatal(err)
	}
	term.Render(g)

	if got := rowText(g, 1); got != "two again" {
		t.Errorf("the row that changed reads %q", got)
	}
	if got := rowText(g, 0); got != "stale" {
		t.Errorf("a row nothing touched was written again: it reads %q", got)
	}
}

// A grid that this screen has not drawn into gets every row. What it is
// missing is not something the screen can know.
func TestRenderingIntoAnotherGridWritesEverything(t *testing.T) {
	term := New(20, 4, DefaultPalette(), 100, Callbacks{})
	first := grid.New(20, 4, DefaultPalette().FG, DefaultPalette().BG)
	if _, err := term.Write([]byte("one\x1b[2;1Htwo")); err != nil {
		t.Fatal(err)
	}
	term.Render(first)

	other := grid.New(20, 4, DefaultPalette().FG, DefaultPalette().BG)
	mark(other, "stale")
	term.Render(other)

	if got := rowText(other, 0); got != "one" {
		t.Errorf("row 0 of the other grid reads %q", got)
	}
	if got := rowText(other, 1); got != "two" {
		t.Errorf("row 1 of the other grid reads %q", got)
	}
	if got := rowText(other, 2); got != "" {
		t.Errorf("row 2 of the other grid reads %q", got)
	}
}

// Everything that moves the rows under the view redraws all of them.
//
// Each of these changes what every row shows without writing to most of
// them, so a render that only looked at what was written would leave the
// screen showing the row it used to be.
func TestWhatMovesEveryRowRedrawsEveryRow(t *testing.T) {
	cases := []struct {
		what  string
		write string
		want  []string
	}{
		{"a line feed at the bottom", "\x1b[4;1Hpushed\r\n", []string{"two", "three", "pushed", ""}},
		{"scroll up", "\x1b[S", []string{"two", "three", "", ""}},
		{"scroll down", "\x1b[T", []string{"", "one", "two", "three"}},
		{"insert a line", "\x1b[1;1H\x1b[L", []string{"", "one", "two", "three"}},
		{"delete a line", "\x1b[1;1H\x1b[M", []string{"two", "three", "", ""}},
		{"erase the display", "\x1b[2J", []string{"", "", "", ""}},
		{"the alignment pattern", "\x1b#8", []string{
			"EEEEEEEEEEEEEEEEEEEE", "EEEEEEEEEEEEEEEEEEEE",
			"EEEEEEEEEEEEEEEEEEEE", "EEEEEEEEEEEEEEEEEEEE",
		}},
		{"the alternate screen", "\x1b[?1049h", []string{"", "", "", ""}},
		{"scrolling the view back", "", nil},
	}
	for _, c := range cases {
		t.Run(c.what, func(t *testing.T) {
			term := New(20, 4, DefaultPalette(), 100, Callbacks{})
			g := grid.New(20, 4, DefaultPalette().FG, DefaultPalette().BG)
			text := "one\r\ntwo\r\nthree"
			if c.write == "" {
				// The view can only go back over lines that have gone
				// into history, so this one fills the screen first.
				text = "one\r\ntwo\r\nthree\r\nfour\r\nfive"
			}
			if _, err := term.Write([]byte(text)); err != nil {
				t.Fatal(err)
			}
			term.Render(g)
			mark(g, "stale")

			want := c.want
			if c.write == "" {
				// The view scrolling back is not a sequence: it is what
				// the wheel does.
				term.Screen().ScrollView(1)
				want = []string{"one", "two", "three", "four"}
			} else if _, err := term.Write([]byte(c.write)); err != nil {
				t.Fatal(err)
			}
			term.Render(g)

			for y, w := range want {
				if got := rowText(g, y); got != w {
					t.Errorf("row %d reads %q, want %q", y, got, w)
				}
			}
		})
	}
}

// Turning reverse video on swaps the colours of every cell already on
// the screen, without writing to one of them.
func TestReverseVideoRedrawsEveryRow(t *testing.T) {
	term := New(20, 4, DefaultPalette(), 100, Callbacks{})
	g := grid.New(20, 4, DefaultPalette().FG, DefaultPalette().BG)
	if _, err := term.Write([]byte("one\r\ntwo")); err != nil {
		t.Fatal(err)
	}
	term.Render(g)
	was := g.At(0, 0)

	if _, err := term.Write([]byte("\x1b[?5h")); err != nil {
		t.Fatal(err)
	}
	term.Render(g)

	if got := g.At(0, 0); got.FG != was.BG || got.BG != was.FG {
		t.Errorf("the first cell is %v on %v, want the colours swapped", got.FG, got.BG)
	}
}

// A screen that was resized is drawn whole, whatever the emulator wrote
// before the size changed.
func TestResizingRedrawsEveryRow(t *testing.T) {
	term := New(20, 4, DefaultPalette(), 100, Callbacks{})
	g := grid.New(20, 4, DefaultPalette().FG, DefaultPalette().BG)
	if _, err := term.Write([]byte("one\r\ntwo\r\nthree")); err != nil {
		t.Fatal(err)
	}
	term.Render(g)

	term.Resize(20, 6)
	g.Resize(20, 6)
	mark(g, "stale")
	term.Render(g)

	for y, w := range []string{"one", "two", "three", "", "", ""} {
		if got := rowText(g, y); got != w {
			t.Errorf("row %d reads %q, want %q", y, got, w)
		}
	}
}

// A screen handed to a watcher leaves the pane's own grid needing every
// row again.
//
// RenderLive and RenderUnder draw a different view of the same rows into
// somebody else's grid. What they leave behind says nothing about what
// the pane's grid holds, so the next render of that one starts afresh.
func TestRenderingForAWatcherLeavesThePanesGridStale(t *testing.T) {
	for _, what := range []string{"live", "under"} {
		t.Run(what, func(t *testing.T) {
			term := New(20, 4, DefaultPalette(), 100, Callbacks{})
			g := grid.New(20, 4, DefaultPalette().FG, DefaultPalette().BG)
			if _, err := term.Write([]byte("one\x1b[2;1Htwo")); err != nil {
				t.Fatal(err)
			}
			term.Render(g)

			// Somebody watching is handed the screen, into a grid of
			// their own.
			theirs := grid.New(20, 4, DefaultPalette().FG, DefaultPalette().BG)
			if what == "live" {
				term.RenderLive(theirs)
			} else {
				if _, err := term.Write([]byte("\x1b[?1049h")); err != nil {
					t.Fatal(err)
				}
				term.RenderUnder(theirs)
			}

			// And the pane's own grid is written again in full.
			mark(g, "stale")
			term.Render(g)
			if got := rowText(g, 0); got == "stale" {
				t.Error("the pane's grid was left as the watcher found it")
			}
		})
	}
}
