package ui

import (
	"image/color"
	"math"
	"strings"
	"testing"

	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/vt"
)

// A row with a fill washes the ground of that share of its width, and
// everything it drew stays on top of it.
func TestListFillsPartOfARowsGround(t *testing.T) {
	filled := color.RGBA{R: 20, G: 40, B: 90, A: 255}
	l := newTestList(t, []ListRow{
		{Text: "one.txt to halfway", Depth: 1, Note: "0 of 1", Fill: 0.5, Key: 1},
	}, 40, 4)
	l.Style.FillBG = filled
	// Without the keys, so the row keeps the list's own ground and the
	// fill is the only thing changing it.
	l.SetFocus(false)
	g := drawList(l, 40, 4)

	for x := range 20 {
		if got := g.At(x, 0).BG; got != filled {
			t.Fatalf("column %d is on %v, want the fill %v", x, got, filled)
		}
	}
	for x := 20; x < 40; x++ {
		if got := g.At(x, 0).BG; got != l.Style.BG {
			t.Fatalf("column %d is on %v, want the row's own ground %v", x, got, l.Style.BG)
		}
	}
	// The text is still drawn, in the colour the row draws it in.
	if row := rowOf(g, 0); !strings.Contains(row, "one.txt to halfway") {
		t.Fatalf("row = %q, want the text over the fill", row)
	}
	if got := g.At(2, 0); got.Rune != 'o' || got.FG != l.Style.FG {
		t.Fatalf("the first letter is %q in %v, want it in the row's colour", got.Rune, got.FG)
	}
	// And so is the note, which is in the half the fill does not cover.
	if got := g.At(33, 0); got.Rune != '0' {
		t.Fatalf("the note starts with %q", got.Rune)
	}

	// A row that asks for no fill is left as it was.
	l.SetRows([]ListRow{{Text: "one.txt to halfway", Depth: 1, Note: "done", Key: 1}})
	g = drawList(l, 40, 4)
	for x := range 40 {
		if got := g.At(x, 0).BG; got != l.Style.BG {
			t.Fatalf("a row with no fill is on %v at column %d", got, x)
		}
	}
}

// The selected row keeps its own ground and still shows the fill, blended
// half way between the two: the row has to read as the selected one, and
// the fill has to read at all.
func TestListFillsTheSelectedRowWithoutLosingIt(t *testing.T) {
	filled := color.RGBA{R: 20, G: 40, B: 90, A: 255}
	l := newTestList(t, []ListRow{
		{Text: "one.txt to halfway", Depth: 1, Fill: 0.5, Key: 1},
	}, 40, 4)
	l.Style.FillBG = filled
	g := drawList(l, 40, 4)
	if at := l.SelectedIndex(); at != 0 {
		t.Fatalf("the bar is on row %d, so this proves nothing", at)
	}

	near, far := g.At(0, 0).BG, g.At(39, 0).BG
	if far != l.Style.SelectedBG {
		t.Fatalf("the unfilled half of the selected row is on %v, want %v", far, l.Style.SelectedBG)
	}
	// Half way between the row's ground and the fill pulled towards the
	// row's own text colour, pinned rather than only "different": a blend
	// that crept towards either end would lose the selection or the fill,
	// and both have to read.
	want := grid.Blend(l.Style.SelectedBG, grid.Blend(filled, l.Style.SelectedFG, 1, 3), 1, 2)
	if near != want {
		t.Fatalf("the filled half of the selected row is on %v, want %v", near, want)
	}
}

// panelStyle is the sidebar's own colours, taken from the default palette
// the way newPanel builds them.
//
// The fill is drawn in these, so this is the palette it has to read in: a
// fill that reads on colours made up for a test proves nothing.
func panelStyle() ListStyle {
	p := vt.DefaultPalette()
	return ListStyle{
		FG:         p.FG,
		BG:         grid.Blend(p.BG, p.ANSI[4], 1, 20),
		BGEnd:      grid.Blend(p.BG, p.ANSI[4], 1, 8),
		SelectedFG: p.BG,
		SelectedBG: p.FG,
		CurrentFG:  p.FG,
		CurrentBG:  grid.Blend(p.BG, p.FG, 1, 6),
		NoteFG:     p.ANSI[8],
		HeaderFG:   p.ANSI[6],
		FillBG:     grid.Blend(p.BG, p.ANSI[4], 1, 3),
	}
}

// fillReads is the contrast a filled part of a row has to reach against
// the rest of that row, and textReads what the text on it has to reach.
// Both are the WCAG ratios: 1.5 for a change of ground, 4.5 for text.
const (
	fillReads = 1.5
	textReads = 4.5
)

// A row with a ground of its own still shows the fill, on the colours the
// sidebar really uses.
//
// Measured as a contrast ratio rather than as a blend fraction. The row in
// front is already lifted off the list's ground, so most of the way
// towards the fill still left the two within 1.19:1 of each other, which
// is a fill nobody can see.
func TestListFillReadsOnARowWithAGroundOfItsOwn(t *testing.T) {
	rows := []ListRow{{Text: "one.txt to halfway", Depth: 1, Fill: 0.5, Key: 1}}

	for _, c := range []struct {
		what    string
		focused bool
		fg, bg  color.RGBA
	}{
		{"the row in front", false, panelStyle().CurrentFG, panelStyle().CurrentBG},
		{"the selected row", true, panelStyle().SelectedFG, panelStyle().SelectedBG},
	} {
		l := newTestList(t, rows, 40, 4)
		l.Style = panelStyle()
		// The row in front is the one without the keys: with them it is
		// the selected row, which is the other case.
		l.SetFocus(c.focused)
		l.SetCurrent(1)
		g := drawList(l, 40, 4)

		near, far := g.At(0, 0).BG, g.At(39, 0).BG
		if far != c.bg {
			t.Errorf("the unfilled half of %s is on %v, want its own ground %v", c.what, far, c.bg)
		}
		if got := grid.Contrast(near, far); got < fillReads {
			t.Errorf("the fill on %s is %.2f:1 against that row's ground, want at least %.1f",
				c.what, got, fillReads)
		}
		if got := grid.Contrast(c.fg, near); got < textReads {
			t.Errorf("the text over the fill on %s is %.2f:1, want at least %.1f",
				c.what, got, textReads)
		}
	}
}

// A row asking for more than a row, or for a number that is not one,
// fills the whole width and nothing. Neither reaches the conversion to a
// column count, where an infinity or a NaN is anybody's guess.
func TestListClampsWhatARowAsksToFill(t *testing.T) {
	filled := color.RGBA{R: 20, G: 40, B: 90, A: 255}
	l := newTestList(t, []ListRow{{Text: "one.txt", Depth: 1, Key: 1}}, 40, 4)
	l.Style.FillBG = filled
	l.SetFocus(false)

	whole := []float64{1.5, math.Inf(1)}
	for _, fill := range whole {
		l.SetRows([]ListRow{{Text: "one.txt", Depth: 1, Fill: fill, Key: 1}})
		g := drawList(l, 40, 4)
		for x := range 40 {
			if got := g.At(x, 0).BG; got != filled {
				t.Fatalf("a row asking for %v is on %v at column %d, want the whole row filled", fill, got, x)
			}
		}
	}

	nothing := []float64{math.NaN(), -1, math.Inf(-1)}
	for _, fill := range nothing {
		l.SetRows([]ListRow{{Text: "one.txt", Depth: 1, Fill: fill, Key: 1}})
		g := drawList(l, 40, 4)
		for x := range 40 {
			if got := g.At(x, 0).BG; got != l.Style.BG {
				t.Fatalf("a row asking for %v is on %v at column %d, want nothing filled", fill, got, x)
			}
		}
	}
}

// A double-width character on the edge of the fill is left whole: both of
// its halves keep one ground, so one character is not drawn on two.
func TestListFillLeavesAWideGlyphWhole(t *testing.T) {
	filled := color.RGBA{R: 20, G: 40, B: 90, A: 255}
	// The wide character starts at column 19, so half of 40 columns lands
	// on its far half.
	text := strings.Repeat("a", 19) + "漢字"
	l := newTestList(t, []ListRow{{Text: text, Fill: 0.5, Key: 1}}, 40, 4)
	l.Style.FillBG = filled
	l.SetFocus(false)
	g := drawList(l, 40, 4)

	if got := g.At(19, 0); got.Width != 2 {
		t.Fatalf("column 19 is %q %d wide, so the test is not measuring a wide glyph", got.Rune, got.Width)
	}
	for x := range 19 {
		if got := g.At(x, 0).BG; got != filled {
			t.Fatalf("column %d is on %v, want the fill", x, got)
		}
	}
	for x := 19; x < 21; x++ {
		if got := g.At(x, 0).BG; got != l.Style.BG {
			t.Fatalf("half of the wide glyph, at column %d, is on %v, want one ground for both", x, got)
		}
	}
}

// The fill changes the ground of a cell and nothing else, so a row's art
// is still drawn where the fill covers it.
func TestListFillKeepsARowsArt(t *testing.T) {
	filled := color.RGBA{R: 20, G: 40, B: 90, A: 255}
	art := grid.Icon(grid.IconFiles)
	l := newTestList(t, []ListRow{
		{Text: "one.txt", Depth: 1, Art: art, Fill: 1, Key: 1},
	}, 40, 4)
	l.Style.FillBG = filled
	l.SetFocus(false)
	g := drawList(l, 40, 4)

	var found bool
	for x := range 40 {
		c := g.At(x, 0)
		if c.Art != art {
			continue
		}
		found = true
		if c.BG != filled {
			t.Fatalf("the art is on %v, want the fill", c.BG)
		}
	}
	if !found {
		t.Fatalf("the filled row draws no art: %q", rowOf(g, 0))
	}
}
