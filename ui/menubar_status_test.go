package ui

import (
	"errors"
	"image/color"
	"strings"
	"testing"

	"github.com/marrasen/gridterm/grid"
)

// chipFG and chipGround are colours of their own, so a chip drawn in
// them can be told from the bar's own foreground and ground.
var (
	chipFG     = color.RGBA{0x00, 0xff, 0x00, 0xff}
	chipGround = color.RGBA{0x40, 0x00, 0x40, 0xff}
)

// newChipBar returns the test bar with chips on it, the stage its menus
// are shown on, and how many presses each chip has taken.
func newChipBar(t *testing.T, texts ...string) (*Menubar, *stage, []int) {
	t.Helper()
	b, st := newTestBar(t, &filler{ch: 'x'})
	ran := make([]int, len(texts))
	for i, text := range texts {
		b.Chips = append(b.Chips, Chip{
			Text: text, FG: chipFG, BG: chipGround,
			Do: func() error { ran[i]++; return nil },
		})
	}
	return b, st, ran
}

// drawBarOn draws a bar of the given size and returns the grid.
func drawBarOn(b *Menubar, cols, rows int) *grid.Grid {
	b.Layout(Size{Cols: cols, Rows: rows})
	g := grid.New(cols, rows, fg, bg)
	b.Draw(g.View())
	return g
}

// titlesEnd is the column just past the last title, which is where the
// room for the chips starts.
const titlesEnd = 12

// chipWide is how many columns a chip saying this takes, its pad
// counted in.
func chipWide(text string) int { return grid.StringWidth(text) + chipPad*2 }

func TestMenubarDrawsAChipAgainstTheRightEdge(t *testing.T) {
	b, _, _ := newChipBar(t, "ready")

	g := drawBarOn(b, 40, 20)

	row := rowOf(g, 0)
	at := strings.Index(row, "ready")
	if at < 0 {
		t.Fatalf("bar row = %q, want the chip on it", row)
	}
	// One blank cell of margin at the right edge, then the chip's own
	// pad, then what it says.
	if want := 40 - chipMargin - chipWide("ready") + chipPad; at != want {
		t.Errorf("the chip says its word at column %d, want %d", at, want)
	}
	if got := g.At(39, 0).Rune; got != ' ' {
		t.Errorf("last column reads %q, want the margin blank", got)
	}
	if got := g.At(34, 0).FG; got != chipFG {
		t.Errorf("chip foreground = %+v, want its own colour %+v", got, chipFG)
	}
	// The ground runs under the whole chip, pad included, which is what
	// makes it look like something to press.
	for x := 32; x < 39; x++ {
		if got := g.At(x, 0).BG; got != chipGround {
			t.Errorf("column %d sits on %+v, want the chip's ground %+v", x, got, chipGround)
		}
	}
	if got := g.At(31, 0).BG; got == chipGround {
		t.Error("the chip's ground runs on past its left edge")
	}
	if got := g.At(39, 0).BG; got == chipGround {
		t.Error("the chip's ground runs into the margin")
	}
	// The titles are where they always were.
	if !strings.Contains(row, "File") || !strings.Contains(row, "Edit") {
		t.Errorf("bar row = %q, want both titles", row)
	}
}

// TestMenubarAChipWithNoColoursOfItsOwnTakesTheBars checks the zero
// alpha, which is what a program that only sets the text gets.
func TestMenubarAChipWithNoColoursOfItsOwnTakesTheBars(t *testing.T) {
	b, _, _ := newChipBar(t, "ready")
	b.Chips[0].FG = color.RGBA{}
	b.Chips[0].BG = color.RGBA{}

	g := drawBarOn(b, 40, 20)

	cell := g.At(33, 0)
	if cell.Rune != 'r' {
		t.Fatalf("bar row = %q, want the chip on it", rowOf(g, 0))
	}
	if cell.FG != b.Style.FG {
		t.Errorf("chip foreground = %+v, want the bar's %+v", cell.FG, b.Style.FG)
	}
	if want := b.Style.colAt(33, 40); cell.BG != want {
		t.Errorf("chip ground = %+v, want the bar's own at that column %+v", cell.BG, want)
	}
}

// TestMenubarWithNoChipsDrawsTheRowAsBefore checks that an empty list
// costs the bar nothing: the row is the one it had before chips existed.
func TestMenubarWithNoChipsDrawsTheRowAsBefore(t *testing.T) {
	b, _, _ := newChipBar(t)

	plain := rowOf(drawBarOn(b, 40, 20), 0)

	if got := strings.TrimRight(plain, " "); got != " File  Edit" {
		t.Errorf("bar row = %q, want the titles and nothing else", plain)
	}
	b.Chips = []Chip{{Text: "ready"}}
	if with := rowOf(drawBarOn(b, 40, 20), 0); with == plain {
		t.Fatalf("the chip changed nothing: %q", with)
	}
	b.Chips = nil
	if again := rowOf(drawBarOn(b, 40, 20), 0); again != plain {
		t.Errorf("bar row = %q after taking the chip away, want %q", again, plain)
	}
}

// TestMenubarChipsReadLeftToRightWithTheLastAtTheEdge checks the order:
// the chips are drawn in the order they were given, and the group sits
// against the right edge.
func TestMenubarChipsReadLeftToRightWithTheLastAtTheEdge(t *testing.T) {
	b, _, _ := newChipBar(t, "ready", "busy")

	g := drawBarOn(b, 40, 20)

	row := rowOf(g, 0)
	first, second := strings.Index(row, "ready"), strings.Index(row, "busy")
	if first < 0 || second < 0 {
		t.Fatalf("bar row = %q, want both chips on it", row)
	}
	if first > second {
		t.Errorf("bar row = %q, want the first chip given to be the one on the left", row)
	}
	// The last chip ends where one chip on its own would.
	if want := 40 - chipMargin - chipPad - grid.StringWidth("busy"); second != want {
		t.Errorf("the last chip says its word at column %d, want %d", second, want)
	}
	// A blank of the bar's own between the two grounds, so they read as
	// two things rather than one wide one.
	gap := first + grid.StringWidth("ready") + chipPad
	if got := g.At(gap, 0).BG; got == chipGround {
		t.Errorf("column %d sits on the chip ground, want the bar's own between the two", gap)
	}
	if got := gap + chipGap; got != second-chipPad {
		t.Errorf("the second chip starts at column %d, want %d", second-chipPad, got)
	}
}

// TestMenubarFitsTheChipsItCanFromTheRight checks which one gives way.
// The titles are the controls, so a chip goes first, and the chip by the
// edge keeps its place.
func TestMenubarFitsTheChipsItCanFromTheRight(t *testing.T) {
	b, _, ran := newChipBar(t, "ready", "busy")

	g := drawBarOn(b, titlesEnd+chipMargin+chipWide("busy"), 20)

	row := rowOf(g, 0)
	if !strings.Contains(row, "File") || !strings.Contains(row, "Edit") {
		t.Errorf("bar row = %q, want both titles kept whole", row)
	}
	if !strings.Contains(row, "busy") {
		t.Errorf("bar row = %q, want the chip by the edge kept", row)
	}
	// Whole or not at all: no head of it, and no mark that it was cut.
	if strings.Contains(row, "read") || strings.Contains(row, "…") {
		t.Errorf("bar row = %q, want the chip that will not fit dropped whole", row)
	}
	chips := b.chipsAt()
	if len(chips) != 1 || chips[0].Text != "busy" {
		t.Fatalf("the bar laid out %+v, want the last chip alone", chips)
	}
	if chips[0].at.X < titlesEnd {
		t.Errorf("the chip starts at column %d, which is inside a title", chips[0].at.X)
	}
	// Still against the right edge, and still no wider than the room
	// there was.
	cols := titlesEnd + chipMargin + chipWide("busy")
	if got, want := chips[0].at.X+chips[0].at.Cols, cols-chipMargin; got != want {
		t.Errorf("the chip ends at column %d, want %d", got, want)
	}
	if room := cols - chipMargin - titlesEnd; chips[0].at.Cols > room {
		t.Errorf("the chip is %d columns wide, want at most %d", chips[0].at.Cols, room)
	}
	// The dropped chip takes no press, whoever presses where it was.
	if _, err := b.HandleMouse(pressAt(titlesEnd, 0)); err != nil {
		t.Fatalf("press: %v", err)
	}
	if ran[0] != 0 {
		t.Errorf("the dropped chip ran %d times, want not at all", ran[0])
	}
}

// TestMenubarAChipThatWillNotFitAtAllIsNotDrawn checks the narrowest bar
// there is room on. One column left over would hold a chip cut in half,
// which says nothing and would still take the press.
func TestMenubarAChipThatWillNotFitAtAllIsNotDrawn(t *testing.T) {
	b, _, ran := newChipBar(t, "serving")

	g := drawBarOn(b, titlesEnd+chipMargin+1, 20)

	if chips := b.chipsAt(); len(chips) != 0 {
		t.Errorf("the bar laid out %+v, want nothing", chips)
	}
	if row := rowOf(g, 0); strings.Contains(row, "s") && !strings.Contains(row, "Edit") {
		t.Errorf("bar row = %q, want nothing of the chip", row)
	}
	handled, err := b.HandleMouse(pressAt(titlesEnd, 0))
	if err != nil {
		t.Fatalf("press: %v", err)
	}
	if handled {
		t.Error("the press on the column the chip would have had was claimed")
	}
	if ran[0] != 0 {
		t.Errorf("the chip ran %d times, want not at all", ran[0])
	}
}

// TestMenubarAChipWithNothingOnItIsNotDrawn checks the empty text. It
// would be a ground with no word on it, and it would still take the
// press.
func TestMenubarAChipWithNothingOnItIsNotDrawn(t *testing.T) {
	b, _, ran := newChipBar(t, "", "busy")

	g := drawBarOn(b, 40, 20)

	chips := b.chipsAt()
	if len(chips) != 1 || chips[0].Text != "busy" {
		t.Fatalf("the bar laid out %+v, want the chip with a word on it alone", chips)
	}
	// The one chip left sits where a single chip does, with no room kept
	// for the empty one.
	start := 40 - chipMargin - chipWide("busy")
	if chips[0].at.X != start {
		t.Errorf("the chip starts at column %d, want %d", chips[0].at.X, start)
	}
	if got := strings.Index(rowOf(g, 0), "busy"); got != start+chipPad {
		t.Errorf("the chip says its word at column %d, want %d", got, start+chipPad)
	}
	if _, err := b.HandleMouse(pressAt(start-1, 0)); err != nil {
		t.Fatalf("press: %v", err)
	}
	if ran[0] != 0 {
		t.Errorf("the empty chip ran %d times, want not at all", ran[0])
	}
}

// TestMenubarKeepsAChipThatStillFitsBesideOneThatDoesNot is the reason
// the chips are fitted from the right rather than dropped from the left.
//
// A wide chip by the edge must not take a narrow one with it: the bar
// would go blank on a window that had room to say something.
func TestMenubarKeepsAChipThatStillFitsBesideOneThatDoesNot(t *testing.T) {
	b, _, ran := newChipBar(t, "hi", "sharing with an agent")

	// Room for the narrow chip and nowhere near the wide one.
	cols := titlesEnd + chipMargin + chipWide("hi")
	g := drawBarOn(b, cols, 20)

	chips := b.chipsAt()
	if len(chips) != 1 || chips[0].Text != "hi" {
		t.Fatalf("the bar laid out %+v, want the chip that fits", chips)
	}
	if got, want := chips[0].at.X, cols-chipMargin-chipWide("hi"); got != want {
		t.Errorf("the chip starts at column %d, want %d", got, want)
	}
	if row := rowOf(g, 0); !strings.Contains(row, "hi") {
		t.Errorf("bar row = %q, want the chip that fits drawn", row)
	}
	// And it is the one that takes the press.
	if _, err := b.HandleMouse(pressAt(chips[0].at.X, 0)); err != nil {
		t.Fatalf("press: %v", err)
	}
	if ran[0] != 1 || ran[1] != 0 {
		t.Errorf("the chips ran %d and %d times, want the one that fits alone", ran[0], ran[1])
	}
}

func TestMenubarClickOnAChipRunsIt(t *testing.T) {
	b, st, ran := newChipBar(t, "ready")
	drawBarOn(b, 40, 20)

	handled, err := b.HandleMouse(pressAt(34, 0))

	if err != nil {
		t.Fatalf("press: %v", err)
	}
	if !handled {
		t.Error("the press on the chip was passed on")
	}
	if ran[0] != 1 {
		t.Errorf("the chip ran %d times, want once", ran[0])
	}
	if len(st.shown) != 0 {
		t.Error("the press on the chip opened a menu")
	}
	// Every column of it, the pad each side counted in: the ground is
	// what the user is pressing.
	for _, col := range []int{32, 38} {
		if _, err := b.HandleMouse(pressAt(col, 0)); err != nil {
			t.Fatalf("press at %d: %v", col, err)
		}
	}
	if ran[0] != 3 {
		t.Errorf("the chip ran %d times, want the pad each side to count too", ran[0])
	}
}

// TestMenubarClickOnAChipPicksTheOneUnderIt checks that two chips take
// their own presses and not each other's.
func TestMenubarClickOnAChipPicksTheOneUnderIt(t *testing.T) {
	b, _, ran := newChipBar(t, "ready", "busy")
	drawBarOn(b, 40, 20)

	// Worked out by hand rather than read off the layout: both chips
	// against the right edge of a 40-column bar, "busy" last.
	if _, err := b.HandleMouse(pressAt(34, 0)); err != nil {
		t.Fatalf("press: %v", err)
	}
	if ran[1] != 1 {
		t.Errorf("the chip by the edge ran %d times, want once", ran[1])
	}
	if _, err := b.HandleMouse(pressAt(26, 0)); err != nil {
		t.Fatalf("press: %v", err)
	}
	if ran[0] != 1 {
		t.Errorf("the chip beside it ran %d times, want once", ran[0])
	}

	chips := b.chipsAt()
	if len(chips) != 2 {
		t.Fatalf("the bar laid out %+v, want both chips", chips)
	}

	// The blank between them belongs to neither.
	handled, err := b.HandleMouse(pressAt(chips[0].at.X+chips[0].at.Cols, 0))
	if err != nil {
		t.Fatalf("press: %v", err)
	}
	if handled {
		t.Error("the press on the blank between two chips was claimed")
	}
	if ran[0] != 1 || ran[1] != 1 {
		t.Errorf("the chips ran %d and %d times after the blank, want once each", ran[0], ran[1])
	}
}

// TestMenubarClickOnAChipWithAMenuOpenClosesItAndRuns checks both ways
// the press arrives: through the open menu, which covers the window, and
// straight at the bar.
func TestMenubarClickOnAChipWithAMenuOpenClosesItAndRuns(t *testing.T) {
	b, st, ran := newChipBar(t, "ready")
	drawBarOn(b, 40, 20)
	mouseTo(t, b, pressAt(1, 0))

	handled, err := st.top().HandleMouse(pressAt(36, 0))

	if err != nil {
		t.Fatalf("press: %v", err)
	}
	if !handled {
		t.Error("the press reached past the menu")
	}
	if b.OpenIndex() != -1 {
		t.Errorf("open = %d, want the menu closed", b.OpenIndex())
	}
	if st.closed != 1 {
		t.Errorf("closed %d times, want once", st.closed)
	}
	if ran[0] != 1 {
		t.Errorf("the chip ran %d times, want once", ran[0])
	}

	// And the same press handed to the bar itself.
	mouseTo(t, b, pressAt(1, 0))
	handled, err = b.HandleMouse(pressAt(36, 0))
	if err != nil {
		t.Fatalf("press: %v", err)
	}
	if !handled {
		t.Error("the press on the chip was passed on")
	}
	if b.OpenIndex() != -1 || st.closed != 2 {
		t.Errorf("open = %d and closed %d times, want the menu taken away", b.OpenIndex(), st.closed)
	}
	if ran[0] != 2 {
		t.Errorf("the chip ran %d times, want twice", ran[0])
	}
}

// TestMenubarClickOnTheBlankBesideTheChipsMeansNothing checks the gap
// between the titles and the chips. It belongs to nobody, as the rest of
// the empty bar does.
func TestMenubarClickOnTheBlankBesideTheChipsMeansNothing(t *testing.T) {
	b, st, ran := newChipBar(t, "ready")
	drawBarOn(b, 40, 20)

	handled, err := b.HandleMouse(pressAt(20, 0))

	if err != nil {
		t.Fatalf("press: %v", err)
	}
	if handled {
		t.Error("a press on the blank beside the chip was claimed")
	}
	if ran[0] != 0 {
		t.Errorf("the chip ran %d times, want not at all", ran[0])
	}
	if len(st.shown) != 0 {
		t.Error("a press on the blank opened a menu")
	}
}

// TestMenubarAChipIsMeasuredInColumns checks a chip of double-width
// characters. Measured in runes it would be drawn off the right edge.
func TestMenubarAChipIsMeasuredInColumns(t *testing.T) {
	b, _, _ := newChipBar(t, "世界")

	g := drawBarOn(b, 40, 20)

	start := 40 - chipMargin - chipWide("世界") + chipPad
	if got := g.At(start, 0); got.Rune != '世' || got.Width != 2 {
		t.Errorf("column %d holds %+v, want the first character on two columns", start, got)
	}
	if got := g.At(start+1, 0).Width; got != 0 {
		t.Errorf("column %d has width %d, want the other half of the character", start+1, got)
	}
	if got := g.At(start+2, 0).Rune; got != '界' {
		t.Errorf("column %d reads %q, want the second character", start+2, got)
	}
	if got := g.At(39, 0).Rune; got != ' ' {
		t.Errorf("last column reads %q, want the margin blank", got)
	}
	if chips := b.chipsAt(); len(chips) != 1 || chips[0].at.X != start-chipPad {
		t.Errorf("the bar laid out %+v, want one chip starting at column %d", chips, start-chipPad)
	}
}

// TestMenubarChipsSetToTheSameValueLeavesTheLayerClean is the idle-frame
// rule with chips on the bar.
func TestMenubarChipsSetToTheSameValueLeavesTheLayerClean(t *testing.T) {
	b, _, _ := newChipBar(t, "ready")
	g := drawBarOn(b, 40, 20)

	g.ClearDirty()
	for i := range 2 {
		b.Chips = []Chip{{Text: "ready", FG: chipFG, BG: chipGround}}
		b.Draw(g.View())
		if g.RowDirty(0) {
			t.Fatalf("draw %d with the same chip dirtied the bar's row", i)
		}
	}

	b.Chips = []Chip{{Text: "busy", FG: chipFG, BG: chipGround}}
	b.Draw(g.View())
	if !g.RowDirty(0) {
		t.Error("a new chip did not change the bar")
	}
}

func TestMenubarChipErrorReachesTheCaller(t *testing.T) {
	boom := errors.New("boom")
	b, _, _ := newChipBar(t, "ready")
	b.Chips[0].Do = func() error { return boom }
	drawBarOn(b, 40, 20)

	_, err := b.HandleMouse(pressAt(34, 0))

	if !errors.Is(err, boom) {
		t.Errorf("press returned %v, want the chip's own error", err)
	}
}

// TestMenubarAChipWithNothingToDoStillTakesThePress checks the nil Do,
// which is what a chip that only says something gets.
func TestMenubarAChipWithNothingToDoStillTakesThePress(t *testing.T) {
	b, st, _ := newChipBar(t, "ready")
	b.Chips[0].Do = nil
	drawBarOn(b, 40, 20)

	handled, err := b.HandleMouse(pressAt(34, 0))

	if err != nil {
		t.Fatalf("press: %v", err)
	}
	if !handled {
		t.Error("the press on the chip was passed on")
	}
	if len(st.shown) != 0 {
		t.Error("the press on the chip opened a menu")
	}
}

// TestMenubarChipErrorThroughAnOpenMenuReachesTheCaller checks the other
// way a press arrives. The open menu covers the window, so the press
// comes in through it, and what the chip failed with has to come back
// out the same way.
func TestMenubarChipErrorThroughAnOpenMenuReachesTheCaller(t *testing.T) {
	boom := errors.New("boom")
	b, st, _ := newChipBar(t, "ready")
	b.Chips[0].Do = func() error { return boom }
	drawBarOn(b, 40, 20)
	mouseTo(t, b, pressAt(1, 0))

	_, err := st.top().HandleMouse(pressAt(36, 0))

	if !errors.Is(err, boom) {
		t.Errorf("the press through the menu returned %v, want the chip's own error", err)
	}
	if b.OpenIndex() != -1 {
		t.Errorf("open = %d, want the menu closed as well", b.OpenIndex())
	}
}

// TestMenubarChipsStartExactlyWhereTheTitlesEnd checks the bar width
// where the room is the chip's own width: the two are touching, with the
// last title's own pad as the blank between them.
func TestMenubarChipsStartExactlyWhereTheTitlesEnd(t *testing.T) {
	b, _, _ := newChipBar(t, "ready")
	width := chipWide("ready")

	g := drawBarOn(b, titlesEnd+chipMargin+width, 20)

	chips := b.chipsAt()
	if len(chips) != 1 {
		t.Fatalf("the bar laid out %+v, want the chip: the room is exactly its width", chips)
	}
	if chips[0].at.X != titlesEnd {
		t.Errorf("the chip starts at column %d, want the last title's end %d", chips[0].at.X, titlesEnd)
	}
	if got, want := chips[0].at.X+chips[0].at.Cols, titlesEnd+width; got != want {
		t.Errorf("the chip ends at column %d, want %d", got, want)
	}
	// And drawn there, with the last title whole behind it.
	if got := g.At(titlesEnd+chipPad, 0).Rune; got != 'r' {
		t.Errorf("column %d reads %q, want the chip to say its word there: %q",
			titlesEnd+chipPad, got, rowOf(g, 0))
	}
	if !strings.Contains(rowOf(g, 0), "Edit") {
		t.Errorf("bar row = %q, want the last title kept whole", rowOf(g, 0))
	}
}
