package grid

import (
	"image/color"
	"testing"
)

func padGrid(cols, rows int) *Grid {
	return New(cols, rows, color.RGBA{0xff, 0xff, 0xff, 0xff}, color.RGBA{0, 0, 0, 0xff})
}

// Padding moves pixels without touching a cell, so the only way a layer
// hears about it is the generation and the damage flags.
func TestSettingPaddingRedrawsEverything(t *testing.T) {
	g := padGrid(10, 4)
	g.ClearDirty()
	was := g.PadGeneration()

	g.SetColPad(0, Pad{Before: 2})

	if g.PadGeneration() == was {
		t.Error("the generation did not move, so nothing notices the padding")
	}
	if !g.AnyDirty() {
		t.Error("nothing was marked dirty, so the grid is drawn where it used to be")
	}
	if got := g.ColPad(0); got != (Pad{Before: 2}) {
		t.Errorf("column 0 has %+v", got)
	}
}

// Setting the padding it already has changes nothing. It is written
// every frame, and a grid that redrew itself each time would never let
// an idle window go quiet.
func TestSettingTheSamePaddingChangesNothing(t *testing.T) {
	g := padGrid(10, 4)
	g.SetColPad(3, Pad{Before: 1, After: 2})
	g.SetRowPad(0, Pad{After: 1})
	g.ClearDirty()
	was := g.PadGeneration()

	for i := 0; i < 3; i++ {
		g.SetColPad(3, Pad{Before: 1, After: 2})
		g.SetRowPad(0, Pad{After: 1})
	}

	if g.PadGeneration() != was {
		t.Errorf("the generation moved to %d from %d", g.PadGeneration(), was)
	}
	if g.AnyDirty() {
		t.Error("the grid was dirtied by padding that did not change")
	}
}

// Clearing padding that was never set is the same no-op.
func TestClearingNoPaddingChangesNothing(t *testing.T) {
	g := padGrid(10, 4)
	g.ClearDirty()
	was := g.PadGeneration()

	g.ClearPads()

	if g.PadGeneration() != was || g.AnyDirty() {
		t.Error("clearing padding that was not there redrew the grid")
	}
}

// A column or row that is not there cannot be padded: the table is read
// by index, and an entry past the end would be given to whichever
// column took that index after the next resize.
func TestPaddingOutsideTheGridIsRefused(t *testing.T) {
	g := padGrid(4, 2)
	g.ClearDirty()

	g.SetColPad(4, Pad{Before: 2})
	g.SetColPad(-1, Pad{Before: 2})
	g.SetRowPad(2, Pad{Before: 2})

	if g.AnyDirty() || len(g.ColPads()) != 0 || len(g.RowPads()) != 0 {
		t.Errorf("padding landed outside the grid: cols %v rows %v", g.ColPads(), g.RowPads())
	}
}

// A pad is capped. Padding is meant to be a fraction of a character,
// and a stray number would make a grid wider than any texture.
func TestPaddingIsCapped(t *testing.T) {
	g := padGrid(4, 2)
	g.SetColPad(1, Pad{Before: 127, After: -8})

	if got := g.ColPad(1); got != (Pad{Before: PadMax, After: 0}) {
		t.Errorf("column 1 has %+v, want it clamped", got)
	}
}

// Padding for columns a resize took away goes with them, and dropping
// it counts as a change.
func TestResizeDropsPaddingItHasNoColumnFor(t *testing.T) {
	g := padGrid(10, 6)
	g.SetColPad(9, Pad{After: 2})
	g.SetRowPad(5, Pad{After: 1})
	was := g.PadGeneration()

	g.Resize(4, 3)

	if got := g.ColPad(9); got != (Pad{}) {
		t.Errorf("column 9 kept %+v after the grid narrowed to 4", got)
	}
	if len(g.ColPads()) > 4 || len(g.RowPads()) > 3 {
		t.Errorf("the tables outlive the grid: cols %v rows %v", g.ColPads(), g.RowPads())
	}
	if g.PadGeneration() == was {
		t.Error("the generation did not move, so a layer keeps the old measurements")
	}
}

// A resize that keeps every padded column keeps the padding.
func TestResizeKeepsThePaddingItStillHasRoomFor(t *testing.T) {
	g := padGrid(10, 6)
	g.SetColPad(2, Pad{Before: 1})

	g.Resize(20, 6)

	if got := g.ColPad(2); got != (Pad{Before: 1}) {
		t.Errorf("column 2 has %+v after the grid widened", got)
	}
}
