package term

import (
	"image/color"
	"testing"

	"github.com/marrasen/kakel/grid"
	"github.com/marrasen/kakel/input"
)

// A screen drawn into its own view a frame at a time, copying only the
// rows that changed, looks as one copied whole does: after output, with
// a question over the last row and once it has gone, and with a link
// underlined and then not.
func TestDrawingOnlyChangedRowsLooksLikeDrawingAll(t *testing.T) {
	const cols, rows = 30, 6
	followed := 0
	term, f := newTestTerm(t, cols, rows, Config{OnLink: func(string) { followed++ }})
	own := grid.New(cols, rows, color.RGBA{}, color.RGBA{})
	same := func(step string) {
		t.Helper()
		term.DrawScreen(own.View())
		whole := grid.New(cols, rows, color.RGBA{}, color.RGBA{})
		term.draw(whole.View(), true)
		for y := range rows {
			for x := range cols {
				if a, b := own.At(x, y), whole.At(x, y); !sameCell(a, b) {
					t.Fatalf("%s: cell %d,%d is %+v, drawn whole %+v", step, x, y, a, b)
				}
			}
		}
	}
	f.feed(t, term, "one\r\ntwo\r\n\x1b]8;;https://example.com/a\x07click\x1b]8;;\x07 here\r\nfour")
	same("after output")
	term.Ask("Reconnect?", Choice{Label: "Yes"}, Choice{Label: "No"})
	same("with a question")
	if err := term.askPick(0); err != nil {
		t.Fatal(err)
	}
	same("once the question has gone")
	term.SetHover(2, 2, input.ModCtrl)
	same("with the link underlined")
	term.SetHover(2, 2, 0)
	same("with the link no longer underlined")
	f.feed(t, term, "\x1b[2;1Hchanged\x1b[31m red")
	same("after a row changed")
	f.feed(t, term, "\x1b[2J")
	same("after the screen cleared")
}

// sameCell reports whether two cells draw the same.
func sameCell(a, b grid.Cell) bool {
	return a.Rune == b.Rune && a.Width == b.Width && a.FG == b.FG && a.BG == b.BG && a.Attr == b.Attr && string(a.Comb) == string(b.Comb)
}
