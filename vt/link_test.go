package vt

import (
	"strconv"
	"testing"

	"github.com/marrasen/gridterm/grid"
)

// renderOf draws a terminal onto a grid a test can read.
func renderOf(t *testing.T, term *Terminal) *grid.Grid {
	t.Helper()
	g := grid.New(40, 5, DefaultPalette().FG, DefaultPalette().BG)
	term.Render(g)
	return g
}

// linkUnder is the address under a cell of the rendered screen.
func linkUnder(t *testing.T, term *Terminal, col, row int) string {
	t.Helper()
	g := renderOf(t, term)
	return term.LinkURL(g.At(col, row).Link)
}

// A program puts a link under its text with OSC 8, and the cells it
// prints carry it.
func TestOSC8PutsALinkUnderTheText(t *testing.T) {
	term := New(40, 5, DefaultPalette(), 10, Callbacks{})

	feed(t, term, "\x1b]8;;https://example.com/a\x07click\x1b]8;;\x07 plain")

	if got := linkUnder(t, term, 0, 0); got != "https://example.com/a" {
		t.Errorf("the first cell has %q, want the address", got)
	}
	if got := linkUnder(t, term, 4, 0); got != "https://example.com/a" {
		t.Errorf("the last cell of the word has %q, want the address", got)
	}
}

// The link ends where the program says it does.
func TestOSC8EndsWhereItIsClosed(t *testing.T) {
	term := New(40, 5, DefaultPalette(), 10, Callbacks{})

	feed(t, term, "\x1b]8;;https://example.com/a\x07click\x1b]8;;\x07plain")

	if got := linkUnder(t, term, 5, 0); got != "" {
		t.Errorf("the text after the link has %q, want no link", got)
	}
}

// The same address twice is one entry, so a listing of fifty links to
// one page does not cost fifty strings.
func TestTheSameAddressTwiceIsOneEntry(t *testing.T) {
	term := New(40, 5, DefaultPalette(), 10, Callbacks{})

	feed(t, term, "\x1b]8;;https://example.com/a\x07one\x1b]8;;\x07 ")
	feed(t, term, "\x1b]8;;https://example.com/a\x07two\x1b]8;;\x07")

	first := renderOf(t, term).At(0, 0).Link
	second := renderOf(t, term).At(4, 0).Link
	if first == 0 || first != second {
		t.Errorf("the two runs carry %d and %d, want one number for one address", first, second)
	}
}

// The parameters before the address carry an id the program uses to
// join two runs of one link. Nothing here needs it, and it must not
// be mistaken for the address.
func TestOSC8IgnoresTheParametersBeforeTheAddress(t *testing.T) {
	term := New(40, 5, DefaultPalette(), 10, Callbacks{})

	feed(t, term, "\x1b]8;id=42;https://example.com/a\x07click")

	if got := linkUnder(t, term, 0, 0); got != "https://example.com/a" {
		t.Errorf("it read %q, want the address after the parameters", got)
	}
}

// An address holding a semicolon survives the parameter split.
func TestOSC8KeepsASemicolonInTheAddress(t *testing.T) {
	term := New(40, 5, DefaultPalette(), 10, Callbacks{})

	feed(t, term, "\x1b]8;;https://example.com/a;b\x07click")

	if got := linkUnder(t, term, 0, 0); got != "https://example.com/a;b" {
		t.Errorf("it read %q, want the semicolon kept", got)
	}
}

// Only the schemes a browser is the right answer for. A program in a
// pane may be anything, and a link is a thing the user clicks.
func TestOSC8TakesOnlyTheSchemesWorthOpening(t *testing.T) {
	for _, tc := range []struct {
		uri  string
		take bool
	}{
		{"https://example.com", true},
		{"http://example.com", true},
		{"mailto:marcus@example.com", true},
		{"ftp://example.com/f", true},
		{"file:///etc/passwd", false},
		{"javascript:alert(1)", false},
		{"data:text/html,<script>", false},
		{"vbscript:x", false},
		{"ms-msdt:/id", false},
		{"no-scheme-at-all", false},
	} {
		term := New(40, 5, DefaultPalette(), 10, Callbacks{})
		feed(t, term, "\x1b]8;;"+tc.uri+"\x07x")

		got := linkUnder(t, term, 0, 0)
		if tc.take && got != tc.uri {
			t.Errorf("%q was not taken, want it under the text", tc.uri)
		}
		if !tc.take && got != "" {
			t.Errorf("%q was taken as %q, want it left alone", tc.uri, got)
		}
	}
}

// A number this terminal does not know names no address, so a cell
// from somewhere else cannot point at one of these.
func TestAnUnknownLinkNumberNamesNothing(t *testing.T) {
	term := New(40, 5, DefaultPalette(), 10, Callbacks{})
	feed(t, term, "\x1b]8;;https://example.com\x07x")

	if got := term.LinkURL(9999); got != "" {
		t.Errorf("a number nothing set names %q", got)
	}
	if got := term.LinkURL(0); got != "" {
		t.Errorf("no link at all names %q", got)
	}
}

// Past the cap a new address is not taken, and the text is still
// printed: the words are there to read and to copy.
func TestPastTheCapTheTextIsStillPrinted(t *testing.T) {
	term := New(40, 5, DefaultPalette(), 10, Callbacks{})
	for i := range MostLinks + 5 {
		feed(t, term, "\x1b]8;;https://example.com/"+strconv.Itoa(i)+"\x07")
	}

	feed(t, term, "past")

	if got := linkUnder(t, term, 0, 0); got != "" {
		t.Errorf("past the cap a cell has %q, want no link", got)
	}
	if got := renderOf(t, term).At(0, 0).Rune; got != 'p' {
		t.Errorf("the text past the cap reads %q, want it printed anyway", got)
	}
}

// A reset forgets the links, the same as it forgets the title.
func TestAResetForgetsTheLinks(t *testing.T) {
	term := New(40, 5, DefaultPalette(), 10, Callbacks{})
	feed(t, term, "\x1b]8;;https://example.com\x07x")

	feed(t, term, "\x1bc")

	if got := term.LinkURL(1); got != "" {
		t.Errorf("after a reset the first link is still %q", got)
	}
}

// Erasing writes a fresh cell, so a link does not leak into the blank
// space a clear leaves behind.
func TestErasedSpaceCarriesNoLink(t *testing.T) {
	term := New(40, 5, DefaultPalette(), 10, Callbacks{})
	feed(t, term, "\x1b]8;;https://example.com\x07link")

	feed(t, term, "\x1b[2J")

	if got := linkUnder(t, term, 0, 0); got != "" {
		t.Errorf("the cleared screen has %q under it", got)
	}
}
