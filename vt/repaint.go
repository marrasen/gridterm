package vt

import (
	"fmt"
	"image/color"
	"strings"

	"github.com/marrasen/gridterm/grid"
)

// Repaint writes a grid as the escape sequences that would draw it.
//
// It is how a screen is handed to somebody who was not watching it
// being drawn: a window taken over shows what is already on the far end
// rather than an empty pane waiting for the program to say something.
//
// What comes out is the same picture, not the same records: a space
// keeps whatever background and attributes it had, and loses a
// foreground that was never going to be drawn in.
//
// What it writes is a screen, not a session. The scrollback above it
// does not come, and a program holding state of its own -- a pager part
// way through a file -- is not told anything. It is the picture, which
// is what a terminal is showing anyway.
//
// Colours are written as 24-bit SGR. The grid has already resolved
// whatever the program asked for into an actual colour, so sending the
// palette index it came from would mean resolving it again at the other
// end, against a palette that may not be the same.
func Repaint(g *grid.Grid, m Screenful) string {
	cols, rows := g.Size()
	var b strings.Builder
	// Which buffer first, because switching afterwards would throw the
	// screen away: a program in the alternate buffer is drawn there,
	// and one that has left it is not.
	if m.Alt {
		b.WriteString("\x1b[?1049h")
	} else {
		b.WriteString("\x1b[?1049l")
	}
	// Then the two modes that change what happens to what comes next
	// rather than what is already drawn: whether a long line wraps, and
	// how the arrow keys are spelled back to the program.
	if m.Wrap {
		b.WriteString("\x1b[?7h")
	} else {
		b.WriteString("\x1b[?7l")
	}
	if m.AppCursor {
		b.WriteString("\x1b[?1h")
	} else {
		b.WriteString("\x1b[?1l")
	}
	// Home, and no scrollback of the far end's own left showing through
	// underneath what is written over it.
	b.WriteString("\x1b[H\x1b[2J")

	var last grid.Cell
	first := true
	for y := 0; y < rows; y++ {
		if y > 0 {
			b.WriteString("\r\n")
		}
		// Up to the last cell that would show something. What follows
		// it on the row is what the clear above already left there, and
		// a line of eighty spaces costs eighty bytes to say nothing.
		//
		// Everything up to it is written, blank or not: a space under a
		// style that underlines is an underlined space, so a blank
		// cannot simply take whatever style the cell before it had.
		for x := 0; x <= lastShowing(g, y, cols); x++ {
			c := g.At(x, y)
			if c.Width == 0 {
				// The second half of a double-width character, already
				// written by the first.
				continue
			}
			if first || !sameStyle(c, last) {
				b.WriteString(sgr(c, g))
				last, first = c, false
			}
			if c.Rune == 0 {
				b.WriteByte(' ')
			} else {
				b.WriteRune(c.Rune)
			}
			for _, cb := range c.Comb {
				b.WriteRune(cb)
			}
		}
	}
	b.WriteString("\x1b[0m")

	// The cursor last, so it is left where the program had it rather
	// than after whatever was written last. Put where it belongs even
	// when it is hidden: the moment the program shows it again without
	// moving it, one left at the end of the text would appear in the
	// wrong place.
	cur := g.Cursor()
	fmt.Fprintf(&b, "\x1b[%d;%dH", cur.Y+1, cur.X+1)
	if cur.Visible {
		b.WriteString("\x1b[?25h")
	} else {
		b.WriteString("\x1b[?25l")
	}
	return b.String()
}

// Screenful is what a screen carries that its grid does not.
//
// Only the few that change what is drawn or how the keys are spelled
// back. The scroll region, origin mode, insert mode, bracketed paste
// and the mouse modes do not travel: a program that set one of them set
// it on the terminal it was talking to, and what is sent here is a
// picture of that terminal rather than a copy of it.
type Screenful struct {
	// Alt says the program is drawing in the alternate buffer, which is
	// where a full-screen program draws.
	Alt bool

	// Wrap is DECAWM: whether a line too long for the screen carries on
	// to the next.
	Wrap bool

	// AppCursor is DECCKM, which changes how the arrow keys are spelled
	// back to the program.
	AppCursor bool
}

// lastShowing is the rightmost cell of a row that would show anything,
// or -1 when the row is empty.
func lastShowing(g *grid.Grid, y, cols int) int {
	for x := cols - 1; x >= 0; x-- {
		if !blank(g.At(x, y), g) {
			return x
		}
	}
	return -1
}

// blank reports whether a cell would look the same as one never written
// to, so the end of a row can stop before it.
//
// The foreground is not part of it: nothing is drawn in it on a cell
// with no character, so a red space and a plain one are the same
// picture. The background and the attributes are, because a space can
// be coloured, underlined or reversed and every one of those shows.
func blank(c grid.Cell, g *grid.Grid) bool {
	return (c.Rune == ' ' || c.Rune == 0) && len(c.Comb) == 0 &&
		c.Attr&^grid.AttrDim == 0 && c.BG == g.DefaultBG
}

// sameStyle reports whether two cells are drawn the same way, so the
// escape sequence saying how need not be written again.
func sameStyle(a, b grid.Cell) bool {
	return a.FG == b.FG && a.BG == b.BG && a.Attr == b.Attr
}

// sgr is the escape sequence that sets a cell's colours and attributes.
func sgr(c grid.Cell, g *grid.Grid) string {
	var b strings.Builder
	b.WriteString("\x1b[0")
	for _, set := range []struct {
		attr grid.Attr
		code string
	}{
		{grid.AttrBold, ";1"},
		{grid.AttrDim, ";2"},
		{grid.AttrItalic, ";3"},
		{grid.AttrUnderline, ";4"},
		{grid.AttrBlink, ";5"},
		{grid.AttrReverse, ";7"},
		{grid.AttrHidden, ";8"},
		{grid.AttrStrike, ";9"},
	} {
		if c.Attr&set.attr != 0 {
			b.WriteString(set.code)
		}
	}
	if fg := c.FG; fg.A != 0 && fg != g.DefaultFG {
		writeColour(&b, ";38;2", fg)
	}
	if bg := c.BG; bg.A != 0 && bg != g.DefaultBG {
		writeColour(&b, ";48;2", bg)
	}
	b.WriteByte('m')
	return b.String()
}

// writeColour writes one 24-bit colour onto an SGR sequence.
func writeColour(b *strings.Builder, lead string, c color.RGBA) {
	fmt.Fprintf(b, "%s;%d;%d;%d", lead, c.R, c.G, c.B)
}
