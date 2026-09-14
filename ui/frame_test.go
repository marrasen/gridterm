package ui

import (
	"image/color"
	"strings"
	"testing"

	"github.com/marrasen/gridterm/grid"
)

// framed is a colour a test can tell from the rest.
var (
	rule   = color.RGBA{R: 200, G: 200, B: 200, A: 255}
	shade  = color.RGBA{A: 0x60}
	ground = color.RGBA{R: 20, G: 20, B: 40, A: 255}
)

// A rule is drawn all the way round, with corners at the corners.
func TestFrameDrawsARuleRoundTheBox(t *testing.T) {
	g := grid.New(20, 10, color.RGBA{}, color.RGBA{})
	box := Rect{X: 3, Y: 2, Cols: 8, Rows: 5}
	drawFrame(g.View(), box, rule, ground)

	corners := map[[2]int]rune{
		{box.X, box.Y}:                               frameTopLeft,
		{box.X + box.Cols - 1, box.Y}:                frameTopRight,
		{box.X, box.Y + box.Rows - 1}:                frameBottomLeft,
		{box.X + box.Cols - 1, box.Y + box.Rows - 1}: frameBottomRight,
	}
	for at, want := range corners {
		if got := g.At(at[0], at[1]).Rune; got != want {
			t.Errorf("corner %d,%d = %q, want %q", at[0], at[1], got, want)
		}
	}
	for x := box.X + 1; x < box.X+box.Cols-1; x++ {
		for _, y := range []int{box.Y, box.Y + box.Rows - 1} {
			if got := g.At(x, y).Rune; got != frameAcross {
				t.Fatalf("%d,%d = %q, want the rule across", x, y, got)
			}
		}
	}
	for y := box.Y + 1; y < box.Y+box.Rows-1; y++ {
		for _, x := range []int{box.X, box.X + box.Cols - 1} {
			if got := g.At(x, y).Rune; got != frameDown {
				t.Fatalf("%d,%d = %q, want the rule down", x, y, got)
			}
		}
	}
	// And nothing outside it.
	if isFrame(g.At(box.X-1, box.Y).Rune) {
		t.Errorf("the column left of the box holds a rule")
	}
}

// isFrame reports whether a rune is part of the rule.
func isFrame(r rune) bool {
	switch r {
	case frameTopLeft, frameTopRight, frameBottomLeft, frameBottomRight,
		frameAcross, frameDown:
		return true
	}
	return false
}

// A box with no room for a rule gets none rather than a corner drawn
// over a corner.
func TestFrameNeedsRoomForItself(t *testing.T) {
	for _, box := range []Rect{
		{X: 1, Y: 1, Cols: 1, Rows: 5},
		{X: 1, Y: 1, Cols: 5, Rows: 1},
		{},
	} {
		g := grid.New(20, 10, color.RGBA{}, color.RGBA{})
		drawFrame(g.View(), box, rule, ground)
		for y := 0; y < 10; y++ {
			for x := 0; x < 20; x++ {
				if got := g.At(x, y).Rune; isFrame(got) {
					t.Fatalf("box %+v drew %q at %d,%d", box, got, x, y)
				}
			}
		}
	}
}

// A rule with no colour is left out, for a caller that wants none.
func TestFrameWithNoColourDrawsNothing(t *testing.T) {
	g := grid.New(20, 10, color.RGBA{}, color.RGBA{})
	drawFrame(g.View(), Rect{X: 3, Y: 2, Cols: 8, Rows: 5}, color.RGBA{}, ground)
	if got := g.At(3, 2).Rune; isFrame(got) {
		t.Fatalf("it drew %q with no colour to draw in", got)
	}
}

// The shadow falls below and to the right, and never on the box itself:
// a cell written twice in one frame is a cell that changed, and a dialog
// nobody is touching has to leave its layer alone.
func TestShadowFallsBesideTheBox(t *testing.T) {
	g := grid.New(20, 10, color.RGBA{}, color.RGBA{})
	box := Rect{X: 3, Y: 2, Cols: 8, Rows: 5}
	drawShadow(g.View(), box, shade)

	for y := 0; y < 10; y++ {
		for x := 0; x < 20; x++ {
			dark := g.At(x, y).BG == shade
			right := x >= box.X+box.Cols && x < box.X+box.Cols+shadowRight &&
				y >= box.Y+shadowDown && y < box.Y+box.Rows+shadowDown
			under := y >= box.Y+box.Rows && y < box.Y+box.Rows+shadowDown &&
				x >= box.X+shadowRight && x < box.X+box.Cols
			if dark != (right || under) {
				t.Fatalf("%d,%d is shadowed %v, want %v", x, y, dark, right || under)
			}
			if dark && box.Contains(x, y) {
				t.Fatalf("%d,%d is inside the box and shadowed", x, y)
			}
		}
	}
}

// A shadow that would fall off the window is cut off rather than
// wrapping round to the other side.
func TestShadowStopsAtTheEdge(t *testing.T) {
	g := grid.New(10, 6, color.RGBA{}, color.RGBA{})
	drawShadow(g.View(), Rect{X: 6, Y: 3, Cols: 4, Rows: 3}, shade)
	for y := 0; y < 6; y++ {
		if got := g.At(0, y).BG; got == shade {
			t.Fatalf("the shadow wrapped round to column 0 on row %d", y)
		}
	}
}

// A shadow with no colour is left out.
func TestShadowWithNoColourDrawsNothing(t *testing.T) {
	g := grid.New(20, 10, color.RGBA{}, color.RGBA{})
	drawShadow(g.View(), Rect{X: 3, Y: 2, Cols: 8, Rows: 5}, color.RGBA{})
	for y := 0; y < 10; y++ {
		for x := 0; x < 20; x++ {
			if g.At(x, y).BG.A != 0 {
				t.Fatalf("it shadowed %d,%d with no colour to shadow in", x, y)
			}
		}
	}
}

// A menu draws its rule where the mouse says the box is, so the line
// under the pointer is the line that runs.
func TestMenuDrawsItsRuleAndKeepsItsLinesStraight(t *testing.T) {
	cmds := testCommands("Copy", "Paste", "Quit")
	m, _ := newTestMenu(t, cmds, items("copy", "paste", "quit"))
	m.Style.BorderFG = rule
	m.Style.ShadowBG = shade
	g := drawMenu(m, 40, 20)

	box := m.box()
	if got := g.At(box.X, box.Y).Rune; got != frameTopLeft {
		t.Fatalf("the menu's top left is %q, want the rule", got)
	}
	// The lines start inside it.
	lines := menuLines(m)
	if !strings.Contains(rowOf(g, lines.Y), "Copy") {
		t.Fatalf("row %d = %q, want the first line", lines.Y, rowOf(g, lines.Y))
	}
	// And a press on that row runs that line.
	m.HandleMouse(moveTo(lines.X+1, lines.Y))
	if got := m.SelectedIndex(); got != 0 {
		t.Fatalf("the pointer on the first line selected %d", got)
	}
	// A press on the rule itself chooses nothing and leaves the menu up.
	m.HandleMouse(moveTo(box.X, box.Y))
	if got := m.SelectedIndex(); got != 0 {
		t.Fatalf("the pointer on the rule selected %d", got)
	}
}

// A menu nobody is touching leaves its layer alone, rule and shadow
// included.
func TestAnIdleMenuDirtiesNothing(t *testing.T) {
	cmds := testCommands("Copy", "Paste", "Quit")
	m, _ := newTestMenu(t, cmds, items("copy", "paste", "quit"))
	m.Style.BorderFG = rule
	m.Style.ShadowBG = shade

	g := grid.New(40, 20, color.RGBA{}, color.RGBA{})
	m.Layout(Size{Cols: 40, Rows: 20})
	m.Draw(g.View())
	g.ClearDirty()
	for i := 0; i < 5; i++ {
		m.Draw(g.View())
	}
	if g.AnyDirty() {
		t.Fatal("an idle menu dirtied its layer")
	}
}
