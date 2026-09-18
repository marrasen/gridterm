package vt

import (
	"image/color"
	"testing"

	"github.com/marrasen/gridterm/grid"
)

// paper is a light scheme with nothing in common with the dark one, so a
// colour that moved and a colour that did not are easy to tell apart.
func paper() Palette {
	p := Palette{
		FG:        color.RGBA{0x20, 0x22, 0x24, 0xff},
		BG:        color.RGBA{0xf5, 0xf2, 0xe8, 0xff},
		Cursor:    color.RGBA{0x20, 0x22, 0x24, 0xff},
		Selection: color.RGBA{0xcf, 0xd8, 0xe8, 0xff},
	}
	for i := range 16 {
		p.ANSI[i] = color.RGBA{uint8(i), 0x11, 0x22, 0xff}
	}
	p.FillUpper()
	return p
}

// at is the cell the screen draws at x, y.
func (h *harness) at(x, y int) grid.Cell {
	h.t.Helper()
	h.term.Render(h.g)
	return h.g.At(x, y)
}

// A new colour scheme reaches what the program has already printed. The
// window was drawn in one scheme and the pane in another before this.
func TestANewSchemeReachesWhatIsAlreadyOnTheScreen(t *testing.T) {
	h := newHarness(t, 10, 4)
	h.write("hi")
	was := DefaultPalette()

	h.term.Screen().SetPalette(paper())

	c := h.at(0, 0)
	if c.Rune != 'h' {
		t.Fatalf("the top left is %q, not the text that was printed", c.Rune)
	}
	if c.BG != paper().BG || c.FG != paper().FG {
		t.Errorf("the h is %v on %v, want %v on %v (it was %v on %v)",
			c.FG, c.BG, paper().FG, paper().BG, was.FG, was.BG)
	}
}

// Rows the program never wrote take the new scheme too, so the empty
// half of a pane matches the half with text in it.
func TestABlankRowTakesTheNewScheme(t *testing.T) {
	h := newHarness(t, 10, 4)
	h.write("hi")

	h.term.Screen().SetPalette(paper())

	if c := h.at(0, 3); c.BG != paper().BG {
		t.Errorf("the blank row is on %v, want %v", c.BG, paper().BG)
	}
}

// What has scrolled off the top moves as well, so scrolling back does
// not walk into the old scheme.
func TestTheNewSchemeReachesTheScrollback(t *testing.T) {
	h := newHarness(t, 10, 2)
	h.write("one\r\ntwo\r\nthree\r\nfour")
	if h.term.Screen().History() == 0 {
		t.Fatal("nothing scrolled off, so there is nothing to check")
	}

	h.term.Screen().SetPalette(paper())

	h.term.Screen().ScrollView(2)
	c := h.at(0, 0)
	if c.Rune != 'o' {
		t.Fatalf("the top row reads %q, so the view is not in the scrollback", h.line(0))
	}
	if c.BG != paper().BG || c.FG != paper().FG {
		t.Errorf("the scrollback is %v on %v, want %v on %v",
			c.FG, c.BG, paper().FG, paper().BG)
	}
}

// A colour from the scheme's own 256 moves to the same entry of the new
// scheme, rather than staying the old scheme's red.
func TestASchemeColourMovesToTheSameEntry(t *testing.T) {
	h := newHarness(t, 10, 2)
	h.write("\x1b[31mr")

	h.term.Screen().SetPalette(paper())

	c := h.at(0, 0)
	if c.Rune != 'r' {
		t.Fatalf("the top left is %q, not the text that was printed", c.Rune)
	}
	if c.FG != paper().ANSI[1] {
		t.Errorf("the red is %v, want the new scheme's red %v", c.FG, paper().ANSI[1])
	}
}

// A colour the program named outright is left alone: it asked for that
// colour and not for an entry of a scheme.
func TestAColourTheProgramNamedIsLeftAlone(t *testing.T) {
	h := newHarness(t, 10, 2)
	h.write("\x1b[38;2;1;2;3mx")
	want := color.RGBA{1, 2, 3, 0xff}

	h.term.Screen().SetPalette(paper())

	c := h.at(0, 0)
	if c.Rune != 'x' {
		t.Fatalf("the top left is %q, not the text that was printed", c.Rune)
	}
	if c.FG != want {
		t.Errorf("the colour is %v, want the %v the program asked for", c.FG, want)
	}
}

// The screen a full-screen program is drawing on moves with the
// ordinary one, so quitting it does not put the old scheme back.
func TestBothScreensMove(t *testing.T) {
	h := newHarness(t, 10, 2)
	h.write("under")
	h.write("\x1b[?1049h") // the alternate screen, which does not home the cursor
	h.write("\x1b[Hover")

	h.term.Screen().SetPalette(paper())

	c := h.at(0, 0)
	if c.Rune != 'o' {
		t.Fatalf("the top left is %q, so this is not the alternate screen", c.Rune)
	}
	if c.BG != paper().BG || c.FG != paper().FG {
		t.Errorf("the alternate screen is %v on %v, want %v on %v",
			c.FG, c.BG, paper().FG, paper().BG)
	}
	h.write("\x1b[?1049l")
	c = h.at(0, 0)
	if c.Rune != 'u' {
		t.Fatalf("the top left is %q, so this is not the screen underneath", c.Rune)
	}
	if c.BG != paper().BG || c.FG != paper().FG {
		t.Errorf("the screen under it is %v on %v, want %v on %v",
			c.FG, c.BG, paper().FG, paper().BG)
	}
}

// A scheme whose default foreground is also one of its 256 entries moves
// to the new default, which is the one nearly every cell came from.
//
// The red comes first so that the default is looked up after another
// colour, which is where it used to go wrong.
func TestTheDefaultWinsWhenASchemeUsesOneColourForBoth(t *testing.T) {
	was := DefaultPalette()
	was.ANSI[7] = was.FG
	h := newHarness(t, 10, 2)
	h.term.Screen().SetPalette(was)
	h.write("\x1b[31mr\x1b[0mx")

	h.term.Screen().SetPalette(paper())

	c := h.at(1, 0)
	if c.Rune != 'x' {
		t.Fatalf("the second cell is %q, not the text that was printed", c.Rune)
	}
	if c.FG != paper().FG {
		t.Errorf("the x is %v, want the new default %v", c.FG, paper().FG)
	}
}

// The pen moves, so what the program prints next is in the new scheme
// without it saying anything.
func TestThePenMoves(t *testing.T) {
	h := newHarness(t, 10, 2)
	h.write("\x1b[31ma")

	h.term.Screen().SetPalette(paper())
	h.write("b")

	c := h.at(1, 0)
	if c.Rune != 'b' {
		t.Fatalf("the second cell is %q, not what was printed next", c.Rune)
	}
	if c.FG != paper().ANSI[1] {
		t.Errorf("what was printed next is %v, want %v", c.FG, paper().ANSI[1])
	}
}

// A cursor the program put away before the change comes back in the new
// scheme, so restoring it does not print in the old one.
func TestASavedCursorMoves(t *testing.T) {
	h := newHarness(t, 10, 2)
	h.write("\x1b[31m\x1b7") // red, then save the cursor with DECSC
	h.write("\x1b[0m")

	h.term.Screen().SetPalette(paper())
	h.write("\x1b8a") // restore it with DECRC and print

	if c := h.at(0, 0); c.FG != paper().ANSI[1] {
		t.Errorf("the restored pen printed in %v, want %v", c.FG, paper().ANSI[1])
	}
}

// A colour with a name of its own moves to that name in the new scheme,
// and not to the cube entry that happens to hold the same colour. Nearly
// every scheme repeats one or two of its named colours in the 240 above
// them, and bright white is the common one.
func TestANamedColourBeatsTheCubeEntryHoldingTheSameColour(t *testing.T) {
	was := DefaultPalette()
	was.ANSI[15] = was.ANSI[231]
	h := newHarness(t, 10, 2)
	h.term.Screen().SetPalette(was)
	h.write("\x1b[97mw") // bright white, which is entry 15

	h.term.Screen().SetPalette(paper())

	c := h.at(0, 0)
	if c.Rune != 'w' {
		t.Fatalf("the top left is %q, not the text that was printed", c.Rune)
	}
	if c.FG != paper().ANSI[15] {
		t.Errorf("bright white is %v, want the new scheme's bright white %v",
			c.FG, paper().ANSI[15])
	}
}

// Text the program coloured with the scheme's own ground colour stays
// text. The two ends of a scheme are told apart by which of a cell's two
// colours they sit in, not by the colour itself.
func TestAForegroundIsNotMovedToTheNewGround(t *testing.T) {
	was := DefaultPalette()
	was.ANSI[0] = was.BG // as a dark scheme usually has it
	h := newHarness(t, 10, 2)
	h.term.Screen().SetPalette(was)
	h.write("\x1b[30mk") // black text

	h.term.Screen().SetPalette(paper())

	c := h.at(0, 0)
	if c.Rune != 'k' {
		t.Fatalf("the top left is %q, not the text that was printed", c.Rune)
	}
	if c.FG != paper().ANSI[0] {
		t.Errorf("the black text is %v, want the new scheme's black %v",
			c.FG, paper().ANSI[0])
	}
}

// A background from the scheme moves too, not only a foreground.
func TestABackgroundFromTheSchemeMoves(t *testing.T) {
	h := newHarness(t, 10, 2)
	h.write("\x1b[41mr") // on red

	h.term.Screen().SetPalette(paper())

	if c := h.at(0, 0); c.BG != paper().ANSI[1] {
		t.Errorf("the red ground is %v, want the new scheme's red %v",
			c.BG, paper().ANSI[1])
	}
}

// The screen is repainted where it stands, so the change shows without
// the program printing anything.
func TestTheChangeShowsWithoutNewOutput(t *testing.T) {
	h := newHarness(t, 10, 2)
	h.write("hi")
	h.at(0, 0) // draw once, so the grid is already up to date

	h.term.Screen().SetPalette(paper())

	if c := h.at(0, 0); c.BG != paper().BG {
		t.Errorf("the screen is still on %v, want %v", c.BG, paper().BG)
	}
}

// A row the screen blanks after the change is in the new scheme, which
// is what the cell blank lines are made of has to follow.
func TestARowBlankedAfterTheChangeIsInTheNewScheme(t *testing.T) {
	h := newHarness(t, 10, 2)
	h.write("one\rrtwo")

	h.term.Screen().SetPalette(paper())
	h.write("\x1b[2J") // erase the screen, the way clear does

	if c := h.at(3, 1); c.BG != paper().BG {
		t.Errorf("a blanked cell is on %v, want %v", c.BG, paper().BG)
	}
}

// Setting the scheme it is already in changes nothing.
func TestTheSameSchemeAgainIsHarmless(t *testing.T) {
	h := newHarness(t, 10, 2)
	h.write("\x1b[31mr")
	h.term.Screen().SetPalette(paper())
	want := h.at(0, 0)

	h.term.Screen().SetPalette(paper())

	if got := h.at(0, 0); got.FG != want.FG || got.BG != want.BG {
		t.Errorf("the cell is now %v on %v, want the %v on %v it was",
			got.FG, got.BG, want.FG, want.BG)
	}
}
