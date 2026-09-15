package ui

import "testing"

// stateOver returns a place in a list of count rows showing rows of them
// at a time, with the rows named in skip ones the bar may not land on.
func stateOver(count, rows int, skip ...int) *listState {
	cannot := make(map[int]bool, len(skip))
	for _, i := range skip {
		cannot[i] = true
	}
	return &listState{
		count:      func() int { return count },
		rows:       func() int { return rows },
		selectable: func(i int) bool { return !cannot[i] },
	}
}

// The bar steps over the rows it cannot land on.
func TestTheBarSkipsWhatCannotBeChosen(t *testing.T) {
	s := stateOver(5, 5, 1, 2)
	s.at = 0

	s.move(1)
	if s.at != 3 {
		t.Fatalf("the bar is on row %d, want it past the two it cannot land on", s.at)
	}
	s.move(-1)
	if s.at != 0 {
		t.Fatalf("the bar is on row %d, want it back past them", s.at)
	}
}

// A move that runs off the end stops on the last row it can land on,
// rather than wrapping round to the other end.
func TestTheBarStopsAtTheEnds(t *testing.T) {
	s := stateOver(4, 4)
	s.at = 0

	s.move(10)
	if s.at != 3 {
		t.Fatalf("the bar is on row %d, want the last one", s.at)
	}
	s.move(-10)
	if s.at != 0 {
		t.Fatalf("the bar is on row %d, want the first one", s.at)
	}
}

// Moving the bar scrolls the list until the row it is on is in the box.
func TestMovingTheBarBringsItIntoView(t *testing.T) {
	s := stateOver(20, 5)
	s.at, s.top = 0, 0

	s.move(6)
	if s.at != 6 {
		t.Fatalf("the bar is on row %d, want row 6", s.at)
	}
	if s.top != 2 {
		t.Fatalf("the list is scrolled to row %d, want row 2 so the bar is the last one shown",
			s.top)
	}
	s.move(-6)
	if s.top != 0 {
		t.Fatalf("the list is scrolled to row %d, want it back at the top", s.top)
	}
}

// Paging moves a boxful less one row, so the eye has a row it has
// already read to land on.
func TestPagingKeepsOneRowOfWhatWasOnScreen(t *testing.T) {
	s := stateOver(20, 5)
	s.at, s.top = 0, 0

	s.page(1)
	if s.at != 4 {
		t.Fatalf("the bar is on row %d, want row 4: a box of five less the row kept", s.at)
	}
	s.page(-1)
	if s.at != 0 {
		t.Fatalf("the bar is on row %d, want it back at the top", s.at)
	}
}

// Scrolling is kept inside the list, so a box taller than what is in it
// shows the first row rather than blank lines above it.
func TestScrollingIsKeptInsideTheList(t *testing.T) {
	s := stateOver(3, 10)
	s.at, s.top = 0, 7

	s.clamp()
	if s.top != 0 {
		t.Fatalf("the list is scrolled to row %d, want row 0: every row fits", s.top)
	}

	long := stateOver(20, 5)
	long.top = 99
	long.clamp()
	if long.top != 15 {
		t.Fatalf("the list is scrolled to row %d, want row 15: the last boxful", long.top)
	}
}

// Clamping leaves the bar where the user put it, which is what the wheel
// needs: turning it looks somewhere else without choosing anything.
func TestClampingDoesNotMoveTheBar(t *testing.T) {
	s := stateOver(20, 5)
	s.at, s.top = 0, 0

	s.top += 10
	s.clamp()
	if s.at != 0 {
		t.Fatalf("the bar moved to row %d, want it left on row 0", s.at)
	}
	if s.top != 10 {
		t.Fatalf("the list is scrolled to row %d, want row 10", s.top)
	}
}

// A box with no room in it scrolls to the top rather than somewhere
// worked out from a height of zero.
func TestABoxWithNoRoomShowsTheTop(t *testing.T) {
	s := stateOver(20, 0)
	s.at, s.top = 10, 10

	s.ensureVisible()
	if s.top != 0 {
		t.Fatalf("the list is scrolled to row %d, want row 0", s.top)
	}
}

// Moving to a row that cannot be chosen leaves the bar where it is, and
// still brings that row back into view.
func TestMovingToARowThatCannotBeChosenLeavesTheBar(t *testing.T) {
	s := stateOver(20, 5, 7)
	s.at, s.top = 0, 0

	s.moveTo(7)
	if s.at != 0 {
		t.Fatalf("the bar is on row %d, want it left on row 0", s.at)
	}
	if s.top != 0 {
		t.Fatalf("the list is scrolled to row %d, want it left at the top", s.top)
	}
}

// With nothing that can be chosen, there is no first or last row.
func TestAListWithNothingToChooseHasNoEnds(t *testing.T) {
	s := stateOver(3, 5, 0, 1, 2)

	if at := s.first(); at != -1 {
		t.Errorf("the first row that can be chosen is %d, want none", at)
	}
	if at := s.last(); at != -1 {
		t.Errorf("the last row that can be chosen is %d, want none", at)
	}
}
