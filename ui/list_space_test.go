package ui

import (
	"testing"

	"github.com/marrasen/gridterm/grid"
)

// spacedList is a list of machines, each with a connection under it.
func spacedList(machines int) *List {
	l := NewList()
	l.Style.HeaderPad = grid.Pad{Before: 1, After: 1}
	var rows []ListRow
	for i := 0; i < machines; i++ {
		rows = append(rows,
			ListRow{Text: "machine", Header: true, Key: [2]int{i, 0}},
			ListRow{Text: "a shell", Key: [2]int{i, 1}})
	}
	l.SetRows(rows)
	return l
}

// quarters is how much room a set of pads asks for.
func quarters(pads []grid.Pad) int {
	n := 0
	for _, p := range pads {
		n += int(p.Before) + int(p.After)
	}
	return n
}

// The room goes on the rows the list would be drawing headings on, one
// entry per row of the box it was asked about.
func TestTheRoomGoesOnTheHeadings(t *testing.T) {
	l := spacedList(3)

	pads := l.RowPads(6)

	if len(pads) != 6 {
		t.Fatalf("%d entries for a box of 6 rows", len(pads))
	}
	for y, p := range pads {
		want := grid.Pad{}
		if y%2 == 0 {
			want = grid.Pad{Before: 1, After: 1}
		}
		if p != want {
			t.Errorf("row %d has %+v, want %+v", y, p, want)
		}
	}
}

// Only the headings that would be on screen at that height get room. A
// row given up for a gap nobody can see is a row of the list thrown
// away for nothing.
func TestOnlyTheHeadingsOnScreenGetRoom(t *testing.T) {
	l := spacedList(20)

	if got, want := quarters(l.RowPads(6)), 3*2; got != want {
		t.Errorf("a box of 6 rows wants %d quarters, want %d for the three headings in it",
			got, want)
	}
	if got, want := quarters(l.RowPads(40)), 20*2; got != want {
		t.Errorf("a box of 40 rows wants %d quarters, want %d", got, want)
	}
}

// Asking changes nothing. The height is settled by asking several
// times over, and a question that moved the list would settle on an
// answer to a different question.
func TestAskingForRoomChangesNothing(t *testing.T) {
	l := spacedList(20)
	l.Layout(Size{Cols: 20, Rows: 8})
	l.scrollBy(6)
	top, at := l.top, l.at

	for rows := 1; rows <= 40; rows++ {
		l.RowPads(rows)
	}

	if l.top != top || l.at != at {
		t.Errorf("the list moved to top %d selection %d, from top %d selection %d",
			l.top, l.at, top, at)
	}
	if got := l.size.Rows; got != 8 {
		t.Errorf("the list's box became %d rows", got)
	}
}

// Scrolled down, the room follows the headings to wherever they would
// be drawn rather than staying where they were.
func TestTheRoomFollowsTheHeadingsWhenTheListScrolls(t *testing.T) {
	l := spacedList(3)
	l.Layout(Size{Cols: 20, Rows: 4})
	l.scrollBy(1)

	pads := l.RowPads(4)
	if len(pads) != 4 {
		t.Fatalf("%d entries for a box of 4 rows", len(pads))
	}
	// One row down, the headings are on the odd rows.
	for y, p := range pads {
		want := grid.Pad{}
		if y%2 == 1 {
			want = grid.Pad{Before: 1, After: 1}
		}
		if p != want {
			t.Errorf("row %d has %+v, want %+v", y, p, want)
		}
	}
}

// A box taller than the list starts at the top of it, however far down
// the list was scrolled: there is nothing below to scroll to.
func TestABoxTallerThanTheListStartsAtTheTop(t *testing.T) {
	l := spacedList(3)
	l.Layout(Size{Cols: 20, Rows: 2})
	l.scrollBy(4)

	pads := l.RowPads(20)

	if len(pads) != 20 {
		t.Fatalf("%d entries for a box of 20 rows", len(pads))
	}
	if pads[0] != (grid.Pad{Before: 1, After: 1}) {
		t.Errorf("row 0 has %+v, want the first heading", pads[0])
	}
	if got, want := quarters(pads), 3*2; got != want {
		t.Errorf("it wants %d quarters, want %d for all three headings", got, want)
	}
}

// A list that was given no header padding marks nothing, so a list on
// the window's own grid is left alone.
func TestAListWithoutHeaderPaddingWantsNothing(t *testing.T) {
	l := spacedList(3)
	l.Style.HeaderPad = grid.Pad{}

	if got := l.RowPads(10); got != nil {
		t.Errorf("it marked %v", got)
	}
}

// A box with no rows in it wants nothing: there is nothing to put room
// around.
func TestAnEmptyBoxWantsNoRoom(t *testing.T) {
	l := spacedList(3)

	if got := l.RowPads(0); got != nil {
		t.Errorf("a box of no rows wants %v", got)
	}
}
