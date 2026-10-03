package term

import (
	"fmt"
	"strings"
	"testing"
)

// trueColourFrame is a frame of an animation as termflix writes it: a
// synchronized update setting both colours of every cell, on a screen
// of cols by rows. Each cell holds letter, so a screen shows which
// frame it is from.
func trueColourFrame(cols, rows int, letter byte) []byte {
	var f strings.Builder
	f.WriteString("\x1b[?2026h\x1b[H")
	for y := range rows {
		fmt.Fprintf(&f, "\x1b[%d;1H", y+1)
		for x := range cols {
			fmt.Fprintf(&f, "\x1b[38;2;%d;%d;160m\x1b[48;2;40;%d;%dm%c", (x+y)%256, x%256, y%256, (x*y)%256, letter)
		}
	}
	f.WriteString("\x1b[?2026l")
	return []byte(f.String())
}

// A frame of a full-screen animation on a wide monitor, 638 by 93 cells
// at true colour, is two megabytes. It is held whole however many reads
// it takes: until its end arrives, the screen shows the frame before.
// Held to a megabyte, the top of the next frame showed over the bottom
// of the last, torn across, on every frame.
func TestAFrameOfAWideScreenIsHeldWhole(t *testing.T) {
	const cols, rows = 638, 93
	term, f := newTestTerm(t, cols, rows, Config{})
	// readAll hands b to the terminal a read at a time, as a session
	// would, and waits for it to deal with each.
	readAll := func(b []byte) {
		for len(b) > 0 {
			n := min(readChunk, len(b))
			before := f.reads.Load()
			f.out <- b[:n]
			b = b[n:]
			// Dealt with once the terminal reads again.
			waitFor(t, func() bool { return f.reads.Load() > before })
		}
	}
	readAll(trueColourFrame(cols, rows, 'A'))

	next := trueColourFrame(cols, rows, 'B')
	if len(next) < 2<<20 {
		t.Fatalf("the frame is %d bytes, want one over two megabytes", len(next))
	}
	end := len(next) - len("\x1b[?2026l")
	readAll(next[:end])
	g := draw(term, cols, rows)
	for y := range rows {
		if row := rowText(g, y); row != strings.Repeat("A", cols) {
			t.Fatalf("before the frame ended, row %d showed %.40q…, want the frame before", y, row)
		}
	}

	readAll(next[end:])
	g = draw(term, cols, rows)
	for y := range rows {
		if row := rowText(g, y); row != strings.Repeat("B", cols) {
			t.Fatalf("once the frame ended, row %d showed %.40q…, want the frame", y, row)
		}
	}
}
