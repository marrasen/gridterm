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

// The room a list asks for is counted from the headings it holds, not
// from the ones it happens to be showing.
//
// What it asks for is what decides how many rows it gets, so an answer
// that looked at the rows would change them, and change its own answer
// with them the next time round.
func TestTheRoomAListWantsDoesNotFollowItsLayout(t *testing.T) {
	l := spacedList(4)

	l.Layout(Size{Cols: 20, Rows: 20})
	roomy := l.RoomWanted(20)
	l.Layout(Size{Cols: 20, Rows: 3})
	cramped := l.RoomWanted(20)

	if roomy != cramped {
		t.Errorf("a list laid out for 20 rows wants %d quarters and one laid out for 3 wants %d",
			roomy, cramped)
	}
	if want := 4 * 2; roomy != want {
		t.Errorf("four headings want %d quarters, want %d", roomy, want)
	}
}

// A list of nothing but headings cannot spend the whole panel on the
// gaps between them.
func TestTheRoomAListWantsIsCapped(t *testing.T) {
	l := spacedList(40)

	if got, want := l.RoomWanted(12), 12*grid.PadUnit/4; got != want {
		t.Errorf("forty headings in twelve rows want %d quarters, want %d capped", got, want)
	}
}

// A list that was given no header padding wants nothing and marks
// nothing, so a list on the window's own grid is left alone.
func TestAListWithoutHeaderPaddingWantsNothing(t *testing.T) {
	l := spacedList(3)
	l.Style.HeaderPad = grid.Pad{}
	l.Layout(Size{Cols: 20, Rows: 10})

	if got := l.RoomWanted(10); got != 0 {
		t.Errorf("it wants %d quarters", got)
	}
	if got := l.RowPads(); got != nil {
		t.Errorf("it marked %v", got)
	}
}

// The room goes on the rows the list is drawing headings on, one entry
// per row of its box.
func TestTheRoomGoesOnTheHeadings(t *testing.T) {
	l := spacedList(3)
	l.Layout(Size{Cols: 20, Rows: 6})

	pads := l.RowPads()
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

// Scrolled down, the room follows the headings to wherever they are
// drawn rather than staying where they were.
func TestTheRoomFollowsTheHeadingsWhenTheListScrolls(t *testing.T) {
	l := spacedList(3)
	l.Layout(Size{Cols: 20, Rows: 4})
	l.scrollBy(1)

	pads := l.RowPads()
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

// A box with no rows in it asks for nothing: there is nothing to put
// room around.
func TestAnEmptyBoxWantsNoRoom(t *testing.T) {
	l := spacedList(3)

	if got := l.RoomWanted(0); got != 0 {
		t.Errorf("a box of no rows wants %d quarters", got)
	}
}
