package ui

// listState is the user's place in a scrolled list: the row picked out
// and the row drawn first.
//
// A list longer than its box needs both. Moving only the selection lets
// it walk off the bottom of what is on screen, and scrolling on its own
// leaves the bar pointing at a row nobody can see. Each list paints its
// own rows; only the place in them is here.
type listState struct {
	at, top int

	// count is how many rows the list has and rows how many of them fit
	// in its box. They are asked afresh every time rather than kept: a
	// list is rebuilt from whatever it is showing, and its box is laid
	// out again whenever the window changes size.
	count, rows func() int

	// selectable reports whether the bar may land on a row. A nil one
	// means every row can be chosen.
	selectable func(int) bool
}

// can reports whether the bar may land on a row.
func (s *listState) can(at int) bool {
	if at < 0 || at >= s.count() {
		return false
	}
	return s.selectable == nil || s.selectable(at)
}

// first returns the first row that can be chosen, or -1.
func (s *listState) first() int { return s.nextFrom(-1, 1) }

// last returns the last row that can be chosen, or -1.
func (s *listState) last() int { return s.nextFrom(s.count(), -1) }

// nextFrom returns the next row that can be chosen in a direction, or -1
// when there is none.
func (s *listState) nextFrom(from, step int) int {
	if step == 0 {
		return -1
	}
	count := s.count()
	for at := from + step; at >= 0 && at < count; at += step {
		if s.can(at) {
			return at
		}
	}
	return -1
}

// moveTo puts the bar on one row and brings it into view. A row that
// cannot be chosen leaves the bar where it is, and the view comes back
// to it either way.
func (s *listState) moveTo(at int) {
	if s.can(at) {
		s.at = at
	}
	s.ensureVisible()
}

// move steps the bar that many rows it can land on, stopping at the ends
// rather than wrapping: a list you can run off the end of is hard to aim
// at.
func (s *listState) move(by int) {
	if by == 0 {
		s.ensureVisible()
		return
	}
	step := 1
	if by < 0 {
		step, by = -1, -by
	}
	at := s.at
	for ; by > 0; by-- {
		next := s.nextFrom(at, step)
		if next < 0 {
			break
		}
		at = next
	}
	s.moveTo(at)
}

// page steps the bar a boxful in a direction, keeping one row of what
// was on screen so the eye has something to land on.
func (s *listState) page(by int) {
	s.move(by * max(s.rows()-1, 1))
}

// clamp keeps what is drawn within the list, for one that has shrunk or
// a box that has changed size. The bar is left where the user put it,
// which is what the wheel needs.
func (s *listState) clamp() {
	rows := s.rows()
	if rows <= 0 {
		s.top = 0
		return
	}
	s.top = min(max(s.top, 0), max(s.count()-rows, 0))
}

// ensureVisible scrolls until the row the bar is on is in the box, so
// Enter always acts on something the user can see.
func (s *listState) ensureVisible() {
	s.clamp()
	rows := s.rows()
	if rows <= 0 || s.at < 0 {
		return
	}
	if s.at < s.top {
		s.top = s.at
	}
	if s.at >= s.top+rows {
		s.top = s.at - rows + 1
	}
}
