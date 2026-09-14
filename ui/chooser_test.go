package ui

import (
	"errors"
	"image/color"
	"strings"
	"testing"

	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/input"
)

// chooserStyled colours a chooser so a test can tell its parts apart.
func chooserStyled() ChooserStyle {
	return ChooserStyle{
		FG: fg, BG: bg, TitleFG: fg,
		SelectedFG: bg, SelectedBG: fg, NoteFG: fg,
	}
}

// newTestChooser returns a chooser of three lines and what each took.
func newTestChooser(t *testing.T) (*Chooser, *[]string, *int) {
	t.Helper()
	var took []string
	closed := 0
	c := NewChooser("Split with…", func() { closed++ })
	c.Style = chooserStyled()
	for _, name := range []string{"New terminal", "Move vim", "Terminal on margit"} {
		at := name
		c.Add(at, "here", func() error { took = append(took, at); return nil })
	}
	c.Layout(Size{Cols: 60, Rows: 20})
	return c, &took, &closed
}

// drawChooser paints a chooser onto a see-through grid, the way its own
// layer is made.
func drawChooser(c *Chooser, cols, rows int) *grid.Grid {
	g := grid.New(cols, rows, color.RGBA{}, color.RGBA{})
	c.Layout(Size{Cols: cols, Rows: rows})
	c.Draw(g.View())
	return g
}

// A chooser opens on its first line, so Enter straight away takes the
// answer it leads with.
func TestChooserOpensOnItsFirstLine(t *testing.T) {
	c, took, closed := newTestChooser(t)

	if _, err := c.HandleKey(press(input.KeyEnter, 0)); err != nil {
		t.Fatalf("Enter: %v", err)
	}
	if len(*took) != 1 || (*took)[0] != "New terminal" {
		t.Fatalf("it took %v, want the first line", *took)
	}
	if *closed != 1 {
		t.Fatalf("it closed %d times, want once", *closed)
	}
}

// The arrows move down the list and Enter takes what they landed on.
func TestChooserArrowsAndEnter(t *testing.T) {
	c, took, _ := newTestChooser(t)

	c.HandleKey(press(input.KeyDown, 0))
	c.HandleKey(press(input.KeyDown, 0))
	c.HandleKey(press(input.KeyEnter, 0))

	if len(*took) != 1 || (*took)[0] != "Terminal on margit" {
		t.Fatalf("it took %v, want the third line", *took)
	}
}

// Escape leaves without taking anything.
func TestChooserEscapeTakesNothing(t *testing.T) {
	c, took, closed := newTestChooser(t)

	if took2, err := c.HandleKey(press(input.KeyEscape, 0)); !took2 || err != nil {
		t.Fatalf("Escape took %v, err %v", took2, err)
	}
	if len(*took) != 0 {
		t.Fatalf("Escape took %v", *took)
	}
	if *closed != 1 {
		t.Fatalf("it closed %d times, want once", *closed)
	}
}

// It goes away before it does the thing: what was picked may open a
// dialog of its own, and closing one takes everything stacked above it.
func TestChooserClosesBeforeItActs(t *testing.T) {
	var order []string
	c := NewChooser("Pick", func() { order = append(order, "closed") })
	c.Style = chooserStyled()
	c.Add("one", "", func() error { order = append(order, "did"); return nil })
	c.Layout(Size{Cols: 40, Rows: 12})

	c.HandleKey(press(input.KeyEnter, 0))
	if len(order) != 2 || order[0] != "closed" || order[1] != "did" {
		t.Fatalf("it went %v, want it closed first", order)
	}
}

// A failure from what was picked reaches whoever asked.
func TestChooserReportsAFailure(t *testing.T) {
	boom := errors.New("no room to split")
	c := NewChooser("Pick", func() {})
	c.Style = chooserStyled()
	c.Add("one", "", func() error { return boom })
	c.Layout(Size{Cols: 40, Rows: 12})

	_, err := c.HandleKey(press(input.KeyEnter, 0))
	if !errors.Is(err, boom) {
		t.Fatalf("it reported %v, want the failure", err)
	}
}

// A click takes the line it landed on, and one on the title or the rule
// takes nothing.
func TestChooserClicks(t *testing.T) {
	c, took, closed := newTestChooser(t)
	drawChooser(c, 60, 20)

	lines := c.lines()
	for _, at := range []struct {
		x, y int
		why  string
	}{
		{c.box().X, c.box().Y, "the rule"},
		{lines.X, lines.Y - 1, "the blank under the title"},
		{lines.X, lines.Y - 2, "the title"},
	} {
		c.HandleMouse(pressAt(at.x, at.y))
		if len(*took) != 0 {
			t.Fatalf("a press on %s took %v", at.why, *took)
		}
		if *closed != 0 {
			t.Fatalf("a press on %s closed it", at.why)
		}
	}

	c.HandleMouse(pressAt(lines.X+1, lines.Y+1))
	if len(*took) != 1 || (*took)[0] != "Move vim" {
		t.Fatalf("clicking the second line took %v", *took)
	}
}

// A press outside it leaves without taking anything.
func TestChooserPressOutsideCloses(t *testing.T) {
	c, took, closed := newTestChooser(t)
	drawChooser(c, 60, 20)

	c.HandleMouse(pressAt(0, 0))
	if len(*took) != 0 {
		t.Fatalf("a press outside took %v", *took)
	}
	if *closed != 1 {
		t.Fatalf("it closed %d times", *closed)
	}
}

// What it draws is the title and every line, each with its note.
func TestChooserDrawsItsLines(t *testing.T) {
	c, _, _ := newTestChooser(t)
	c.Style.BorderFG = fg
	g := drawChooser(c, 60, 20)

	box := c.box()
	if got := g.At(box.X, box.Y).Rune; got != frameTopLeft {
		t.Errorf("the top left is %q, want the rule", got)
	}
	whole := ""
	for y := box.Y; y < box.Y+box.Rows; y++ {
		whole += rowOf(g, y) + "\n"
	}
	for _, want := range []string{"Split with", "New terminal", "Move vim", "Terminal on margit", "here"} {
		if !strings.Contains(whole, want) {
			t.Errorf("it drew %q, missing %q", whole, want)
		}
	}
}

// A window with no room for it shows nothing, and it takes nothing but
// Escape: Enter would otherwise take a line nobody has read.
func TestAChooserWithNoRoom(t *testing.T) {
	for _, rows := range []int{1, 2, 4} {
		c, took, closed := newTestChooser(t)
		c.Layout(Size{Cols: 60, Rows: rows})
		if got := c.box(); !got.Empty() {
			t.Fatalf("in %d rows the box is %+v, want none", rows, got)
		}
		if got, _ := c.HandleKey(press(input.KeyEnter, 0)); !got {
			t.Fatalf("in %d rows Enter travelled on", rows)
		}
		if len(*took) != 0 {
			t.Fatalf("in %d rows Enter took %v", rows, *took)
		}
		c.HandleKey(press(input.KeyEscape, 0))
		if *closed != 1 {
			t.Fatalf("in %d rows Escape closed it %d times", rows, *closed)
		}
	}
}

// A chooser nobody is touching leaves its layer alone.
func TestAnIdleChooserDirtiesNothing(t *testing.T) {
	c, _, _ := newTestChooser(t)
	c.Style.BorderFG = fg
	c.Style.ShadowBG = color.RGBA{A: 0x60}

	g := grid.New(60, 20, color.RGBA{}, color.RGBA{})
	c.Layout(Size{Cols: 60, Rows: 20})
	c.Draw(g.View())
	g.ClearDirty()
	for i := 0; i < 5; i++ {
		c.Draw(g.View())
	}
	if g.AnyDirty() {
		t.Fatal("an idle chooser dirtied its layer")
	}
}

// A chooser lets go of the keys when it is told to.
//
// It is the top modal while it is up, but a dialog pushed over it takes
// the keys: a chooser still drawing an active bar under that dialog
// would say two things have them.
func TestAChooserToldItLostTheKeysStopsMarkingItsLine(t *testing.T) {
	c, _, _ := newTestChooser(t)
	g := drawChooser(c, 60, 20)

	lines := c.lines()
	if got := g.At(lines.X, lines.Y).BG; got != c.Style.SelectedBG {
		t.Fatalf("with the keys the first line is %v, want it marked", got)
	}

	c.SetFocus(false)
	g = drawChooser(c, 60, 20)
	if got := g.At(lines.X, lines.Y).BG; got == c.Style.SelectedBG {
		t.Fatal("a chooser that lost the keys still marks its line")
	}

	// And it takes them back.
	c.SetFocus(true)
	g = drawChooser(c, 60, 20)
	if got := g.At(lines.X, lines.Y).BG; got != c.Style.SelectedBG {
		t.Fatalf("the line is %v once the keys come back", got)
	}
}
