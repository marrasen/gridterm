package ui

import (
	"image/color"
	"strings"
	"testing"
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

	for x := 0; x < 20; x++ {
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
	for x := 0; x < 40; x++ {
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
	if near == far {
		t.Fatalf("the filled half of the selected row is on %v as well, so the fill says nothing", near)
	}
	if near == filled {
		t.Fatalf("the filled half is the plain fill %v, so the row stops reading as the selected one", near)
	}
}
