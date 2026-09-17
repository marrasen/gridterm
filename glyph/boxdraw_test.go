package glyph

import "testing"

// A line-drawing character says which sides it reaches out on, so the
// renderer can carry each arm across whatever the grid leaves between
// cells.
func TestALineSaysWhichSidesItReachesOn(t *testing.T) {
	for _, c := range []struct {
		r                            rune
		left, right, up, down, isBox bool
	}{
		{'─', true, true, false, false, true},
		{'━', true, true, false, false, true},
		{'│', false, false, true, true, true},
		{'┃', false, false, true, true, true},
		{'┼', true, true, true, true, true},
		// The corners reach on two sides only. One that said it reached
		// across would be carried over the padding on the side it has no
		// arm on, and would sit beside the edge it should meet.
		{'┌', false, true, false, true, true},
		{'┐', true, false, false, true, true},
		{'└', false, true, true, false, true},
		{'┘', true, false, true, false, true},
		// A tee reaches on three.
		{'┬', true, true, false, true, true},
		{'┴', true, true, true, false, true},
		{'├', false, true, true, true, true},
		{'┤', true, false, true, true, true},
		{'═', true, true, false, false, true},
		{'║', false, false, true, true, true},
		// Not lines: a block, a letter, a space.
		{'█', false, false, false, false, false},
		{'A', false, false, false, false, false},
		{' ', false, false, false, false, false},
	} {
		left, right, up, down, ok := Arms(c.r)
		if ok != c.isBox {
			t.Errorf("%q: is a line %v, want %v", c.r, ok, c.isBox)
			continue
		}
		if left != c.left || right != c.right || up != c.up || down != c.down {
			t.Errorf("%q reaches left=%v right=%v up=%v down=%v, want %v %v %v %v",
				c.r, left, right, up, down, c.left, c.right, c.up, c.down)
		}
	}
}
