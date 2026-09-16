package ui

import (
	"errors"
	"image/color"
	"strings"
	"testing"

	"github.com/marrasen/gridterm/grid"
)

// statusFG is a colour of its own, so a status drawn in the bar's
// ordinary foreground can be told from one drawn in its own.
var statusFG = color.RGBA{0x00, 0xff, 0x00, 0xff}

// newStatusBar returns the test bar with a status on it, the stage its
// menus are shown on, and a counter of the presses the status took.
func newStatusBar(t *testing.T, status string) (*Menubar, *stage, *int) {
	t.Helper()
	b, st := newTestBar(t, &filler{ch: 'x'})
	b.Status = status
	b.StatusFG = statusFG
	ran := 0
	b.OnStatus = func() error { ran++; return nil }
	return b, st, &ran
}

// drawBarOn draws a bar of the given size and returns the grid.
func drawBarOn(b *Menubar, cols, rows int) *grid.Grid {
	b.Layout(Size{Cols: cols, Rows: rows})
	g := grid.New(cols, rows, fg, bg)
	b.Draw(g.View())
	return g
}

// titlesEnd is the column just past the last title, which is where the
// room for a status starts.
const titlesEnd = 12

func TestMenubarDrawsTheStatusRightAligned(t *testing.T) {
	b, _, _ := newStatusBar(t, "ready")

	g := drawBarOn(b, 40, 20)

	row := rowOf(g, 0)
	at := strings.Index(row, "ready")
	if at < 0 {
		t.Fatalf("bar row = %q, want the status on it", row)
	}
	// One blank cell of margin at the right edge, and nothing past it.
	if want := 40 - statusPad - grid.StringWidth("ready"); at != want {
		t.Errorf("status starts at column %d, want %d", at, want)
	}
	if got := g.At(39, 0).Rune; got != ' ' {
		t.Errorf("last column reads %q, want the margin blank", got)
	}
	// One blank between the status and the last title.
	if got := g.At(33, 0).Rune; got != ' ' {
		t.Errorf("column before the status reads %q, want a blank", got)
	}
	if got := g.At(34, 0).FG; got != statusFG {
		t.Errorf("status foreground = %+v, want its own colour %+v", got, statusFG)
	}
	// The titles are where they always were.
	if !strings.Contains(row, "File") || !strings.Contains(row, "Edit") {
		t.Errorf("bar row = %q, want both titles", row)
	}
}

// TestMenubarStatusWithNoColourOfItsOwnUsesTheBarsForeground checks the
// zero alpha, which is what a program that only sets Status gets.
func TestMenubarStatusWithNoColourOfItsOwnUsesTheBarsForeground(t *testing.T) {
	b, _, _ := newStatusBar(t, "ready")
	b.StatusFG = color.RGBA{}

	g := drawBarOn(b, 40, 20)

	cell := g.At(34, 0)
	if cell.Rune != 'r' {
		t.Fatalf("bar row = %q, want the status on it", rowOf(g, 0))
	}
	if cell.FG != b.Style.FG {
		t.Errorf("status foreground = %+v, want the bar's %+v", cell.FG, b.Style.FG)
	}
}

// TestMenubarWithNoStatusDrawsTheRowAsBefore checks that the empty
// status costs the bar nothing: the row is the one it had before the
// status existed.
func TestMenubarWithNoStatusDrawsTheRowAsBefore(t *testing.T) {
	b, _, _ := newStatusBar(t, "")

	plain := rowOf(drawBarOn(b, 40, 20), 0)

	if got := strings.TrimRight(plain, " "); got != " File  Edit" {
		t.Errorf("bar row = %q, want the titles and nothing else", plain)
	}
	b.Status = "ready"
	if with := rowOf(drawBarOn(b, 40, 20), 0); with == plain {
		t.Fatalf("the status changed nothing: %q", with)
	}
	b.Status = ""
	if again := rowOf(drawBarOn(b, 40, 20), 0); again != plain {
		t.Errorf("bar row = %q after clearing the status, want %q", again, plain)
	}
}

// TestMenubarNarrowBarKeepsEveryTitleAndTrimsTheStatus checks which one
// gives way. The titles are the controls, so the status is cut instead.
func TestMenubarNarrowBarKeepsEveryTitleAndTrimsTheStatus(t *testing.T) {
	b, _, _ := newStatusBar(t, "serving")

	g := drawBarOn(b, 16, 20)

	row := rowOf(g, 0)
	if !strings.Contains(row, "File") || !strings.Contains(row, "Edit") {
		t.Errorf("bar row = %q, want both titles kept whole", row)
	}
	if strings.Contains(row, "serving") {
		t.Errorf("bar row = %q, want the status trimmed", row)
	}
	// Cut from the left, so the end of the status is what survives.
	if !strings.Contains(row, "…ng") {
		t.Errorf("bar row = %q, want the end of the status with the cut marked", row)
	}
	at, text := b.statusAt()
	if at.X < titlesEnd {
		t.Errorf("status starts at column %d, which is inside a title", at.X)
	}
	if got, want := at.X+at.Cols, 16-statusPad; got != want {
		t.Errorf("status ends at column %d, want %d", got, want)
	}
	if got, room := grid.StringWidth(text), 16-statusPad-titlesEnd; got > room {
		t.Errorf("status is %d columns wide, want at most %d", got, room)
	}
}

func TestMenubarClickOnTheStatusRunsIt(t *testing.T) {
	b, st, ran := newStatusBar(t, "ready")
	drawBarOn(b, 40, 20)

	handled, err := b.HandleMouse(pressAt(34, 0))

	if err != nil {
		t.Fatalf("press: %v", err)
	}
	if !handled {
		t.Error("the press on the status was passed on")
	}
	if *ran != 1 {
		t.Errorf("the status ran %d times, want once", *ran)
	}
	if len(st.shown) != 0 {
		t.Error("the press on the status opened a menu")
	}
	// Every column of it, not only the first.
	if _, err := b.HandleMouse(pressAt(38, 0)); err != nil {
		t.Fatalf("press: %v", err)
	}
	if *ran != 2 {
		t.Errorf("the status ran %d times, want the last column to count too", *ran)
	}
}

// TestMenubarClickOnTheStatusWithAMenuOpenClosesItAndRuns checks both
// ways the press arrives: through the open menu, which covers the
// window, and straight at the bar.
func TestMenubarClickOnTheStatusWithAMenuOpenClosesItAndRuns(t *testing.T) {
	b, st, ran := newStatusBar(t, "ready")
	drawBarOn(b, 40, 20)
	b.HandleMouse(pressAt(1, 0))

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
	if *ran != 1 {
		t.Errorf("the status ran %d times, want once", *ran)
	}

	// And the same press handed to the bar itself.
	b.HandleMouse(pressAt(1, 0))
	handled, err = b.HandleMouse(pressAt(36, 0))
	if err != nil {
		t.Fatalf("press: %v", err)
	}
	if !handled {
		t.Error("the press on the status was passed on")
	}
	if b.OpenIndex() != -1 || st.closed != 2 {
		t.Errorf("open = %d and closed %d times, want the menu taken away", b.OpenIndex(), st.closed)
	}
	if *ran != 2 {
		t.Errorf("the status ran %d times, want twice", *ran)
	}
}

// TestMenubarClickOnTheBlankBesideTheStatusMeansNothing checks the gap
// between the titles and the status. It belongs to nobody, as the rest
// of the empty bar does.
func TestMenubarClickOnTheBlankBesideTheStatusMeansNothing(t *testing.T) {
	b, st, ran := newStatusBar(t, "ready")
	drawBarOn(b, 40, 20)

	handled, err := b.HandleMouse(pressAt(20, 0))

	if err != nil {
		t.Fatalf("press: %v", err)
	}
	if handled {
		t.Error("a press on the blank beside the status was claimed")
	}
	if *ran != 0 {
		t.Errorf("the status ran %d times, want not at all", *ran)
	}
	if len(st.shown) != 0 {
		t.Error("a press on the blank opened a menu")
	}
}

// TestMenubarStatusIsMeasuredInColumns checks a status of double-width
// characters. Measured in runes it would be drawn off the right edge.
func TestMenubarStatusIsMeasuredInColumns(t *testing.T) {
	b, _, _ := newStatusBar(t, "世界")

	g := drawBarOn(b, 40, 20)

	start := 40 - statusPad - grid.StringWidth("世界")
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
	if at, _ := b.statusAt(); at.X != start {
		t.Errorf("status starts at column %d, want %d", at.X, start)
	}
}

// TestMenubarStatusSetToTheSameValueLeavesTheLayerClean is the
// idle-frame rule with a status on the bar.
func TestMenubarStatusSetToTheSameValueLeavesTheLayerClean(t *testing.T) {
	b, _, _ := newStatusBar(t, "ready")
	g := drawBarOn(b, 40, 20)

	g.ClearDirty()
	for i := 0; i < 2; i++ {
		b.Status = "ready"
		b.Draw(g.View())
		if g.RowDirty(0) {
			t.Fatalf("draw %d with an unchanged status dirtied the bar's row", i)
		}
	}

	b.Status = "busy"
	b.Draw(g.View())
	if !g.RowDirty(0) {
		t.Error("a new status did not change the bar")
	}
}

func TestMenubarStatusErrorReachesTheCaller(t *testing.T) {
	boom := errors.New("boom")
	b, _, _ := newStatusBar(t, "ready")
	b.OnStatus = func() error { return boom }
	drawBarOn(b, 40, 20)

	_, err := b.HandleMouse(pressAt(34, 0))

	if !errors.Is(err, boom) {
		t.Errorf("press returned %v, want the status's own error", err)
	}
}

// TestMenubarStatusErrorThroughAnOpenMenuReachesTheCaller checks the
// other way a press arrives. The open menu covers the window, so the
// press comes in through it, and what the status failed with has to come
// back out the same way.
func TestMenubarStatusErrorThroughAnOpenMenuReachesTheCaller(t *testing.T) {
	boom := errors.New("boom")
	b, st, _ := newStatusBar(t, "ready")
	b.OnStatus = func() error { return boom }
	drawBarOn(b, 40, 20)
	b.HandleMouse(pressAt(1, 0))

	_, err := st.top().HandleMouse(pressAt(36, 0))

	if !errors.Is(err, boom) {
		t.Errorf("the press through the menu returned %v, want the status's own error", err)
	}
	if b.OpenIndex() != -1 {
		t.Errorf("open = %d, want the menu closed as well", b.OpenIndex())
	}
}

// TestMenubarAStatusTrimmedToNothingIsNotDrawn checks the narrowest bar
// there is room on. One column left over holds the mark that something
// was cut and nothing else, which says nothing and would still take the
// press.
func TestMenubarAStatusTrimmedToNothingIsNotDrawn(t *testing.T) {
	b, _, ran := newStatusBar(t, "serving")

	g := drawBarOn(b, titlesEnd+statusPad+1, 20)

	if at, text := b.statusAt(); !at.Empty() || text != "" {
		t.Errorf("the status is at %+v reading %q, want nowhere", at, text)
	}
	if row := rowOf(g, 0); strings.Contains(row, "…") {
		t.Errorf("bar row = %q, want no mark where none of the status fits", row)
	}
	handled, err := b.HandleMouse(pressAt(titlesEnd, 0))
	if err != nil {
		t.Fatalf("press: %v", err)
	}
	if handled {
		t.Error("the press on the column the status would have had was claimed")
	}
	if *ran != 0 {
		t.Errorf("the status ran %d times, want not at all", *ran)
	}
}

// TestMenubarStatusStartsExactlyWhereTheTitlesEnd checks the bar width
// where the room is the status's own width: the two are touching, with
// the last title's own pad as the blank between them.
func TestMenubarStatusStartsExactlyWhereTheTitlesEnd(t *testing.T) {
	b, _, _ := newStatusBar(t, "ready")
	width := grid.StringWidth("ready")

	g := drawBarOn(b, titlesEnd+statusPad+width, 20)

	at, text := b.statusAt()
	if text != "ready" {
		t.Fatalf("the status reads %q, want the whole of it: the room is exactly its width", text)
	}
	if at.X != titlesEnd {
		t.Errorf("the status starts at column %d, want the last title's end %d", at.X, titlesEnd)
	}
	if got, want := at.X+at.Cols, titlesEnd+width; got != want {
		t.Errorf("the status ends at column %d, want %d", got, want)
	}
	// And drawn there, with the last title whole behind it.
	if got := g.At(titlesEnd, 0).Rune; got != 'r' {
		t.Errorf("column %d reads %q, want the status to start there: %q",
			titlesEnd, got, rowOf(g, 0))
	}
	if !strings.Contains(rowOf(g, 0), "Edit") {
		t.Errorf("bar row = %q, want the last title kept whole", rowOf(g, 0))
	}
}
