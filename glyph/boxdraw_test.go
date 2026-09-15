package glyph

import "testing"

// A line-drawing character says which way it reaches, so the renderer
// can draw it across whatever the grid leaves between cells.
func TestALineSaysWhichWayItReaches(t *testing.T) {
	for _, c := range []struct {
		r                   rune
		across, down, isBox bool
	}{
		{'─', true, false, true},
		{'━', true, false, true},
		{'│', false, true, true},
		{'┃', false, true, true},
		{'┼', true, true, true},
		{'┌', true, true, true},
		{'└', true, true, true},
		{'═', true, false, true},
		{'║', false, true, true},
		// Not lines: a block, a letter, a space.
		{'█', false, false, false},
		{'A', false, false, false},
		{' ', false, false, false},
	} {
		across, down, ok := Reaches(c.r)
		if ok != c.isBox {
			t.Errorf("%q: is a line %v, want %v", c.r, ok, c.isBox)
			continue
		}
		if across != c.across || down != c.down {
			t.Errorf("%q reaches across=%v down=%v, want across=%v down=%v",
				c.r, across, down, c.across, c.down)
		}
	}
}
