package grid

import (
	"image/color"
	"testing"
)

var (
	fg = color.RGBA{0xff, 0xff, 0xff, 0xff}
	bg = color.RGBA{0x00, 0x00, 0x00, 0xff}
)

func TestSetMarksOnlyItsOwnRowDirty(t *testing.T) {
	g := New(10, 4, fg, bg)
	g.ClearDirty()

	g.Set(3, 2, Cell{Rune: 'x', FG: fg, BG: bg})

	for y := 0; y < 4; y++ {
		if got, want := g.RowDirty(y), y == 2; got != want {
			t.Errorf("RowDirty(%d) = %v, want %v", y, got, want)
		}
	}
}

func TestSetIdenticalCellDoesNotDirty(t *testing.T) {
	g := New(10, 4, fg, bg)
	c := Cell{Rune: 'x', FG: fg, BG: bg}
	g.Set(3, 2, c)
	g.ClearDirty()

	g.Set(3, 2, c)

	if g.AnyDirty() {
		t.Fatal("rewriting an identical cell dirtied the grid; " +
			"an idle screen would repaint every frame")
	}
}

func TestSetOutOfBoundsIsIgnored(t *testing.T) {
	g := New(4, 2, fg, bg)
	g.ClearDirty()

	g.Set(-1, 0, Cell{Rune: 'a'})
	g.Set(0, -1, Cell{Rune: 'a'})
	g.Set(4, 0, Cell{Rune: 'a'})
	g.Set(0, 2, Cell{Rune: 'a'})

	if g.AnyDirty() {
		t.Fatal("an out-of-bounds write dirtied the grid")
	}
	if got := g.At(0, 0).Rune; got != ' ' {
		t.Fatalf("At(0,0).Rune = %q, want space", got)
	}
}

func TestSetStringStopsAtRowEnd(t *testing.T) {
	g := New(4, 1, fg, bg)

	end := g.SetString(2, 0, "abcd", fg, bg, 0)

	if end != 4 {
		t.Errorf("end column = %d, want 4", end)
	}
	if got := g.At(2, 0).Rune; got != 'a' {
		t.Errorf("At(2,0) = %q, want 'a'", got)
	}
	if got := g.At(3, 0).Rune; got != 'b' {
		t.Errorf("At(3,0) = %q, want 'b'", got)
	}
}

func TestSetStringHandlesMultibyteRunes(t *testing.T) {
	g := New(4, 1, fg, bg)

	g.SetString(0, 0, "åäö", fg, bg, 0)

	for i, want := range []rune{'å', 'ä', 'ö'} {
		if got := g.At(i, 0).Rune; got != want {
			t.Errorf("At(%d,0) = %q, want %q", i, got, want)
		}
	}
}

func TestScrollUp(t *testing.T) {
	g := New(2, 4, fg, bg)
	for y := 0; y < 4; y++ {
		g.SetString(0, y, string(rune('0'+y)), fg, bg, 0)
	}

	g.ScrollUp(2)

	if got := g.At(0, 0).Rune; got != '2' {
		t.Errorf("row 0 = %q, want '2'", got)
	}
	if got := g.At(0, 1).Rune; got != '3' {
		t.Errorf("row 1 = %q, want '3'", got)
	}
	if got := g.At(0, 2).Rune; got != ' ' {
		t.Errorf("row 2 = %q, want blank", got)
	}
}

func TestScrollUpBeyondHeightClears(t *testing.T) {
	g := New(2, 3, fg, bg)
	g.SetString(0, 0, "ab", fg, bg, 0)

	g.ScrollUp(99)

	if got := g.At(0, 0).Rune; got != ' ' {
		t.Errorf("At(0,0) = %q, want blank", got)
	}
}

func TestResizePreservesTopLeftOverlap(t *testing.T) {
	g := New(4, 4, fg, bg)
	g.SetString(0, 0, "abcd", fg, bg, 0)
	g.SetString(0, 3, "wxyz", fg, bg, 0)

	g.Resize(2, 2)

	if got := g.At(0, 0).Rune; got != 'a' {
		t.Errorf("At(0,0) = %q, want 'a'", got)
	}
	if got := g.At(1, 0).Rune; got != 'b' {
		t.Errorf("At(1,0) = %q, want 'b'", got)
	}
	if c, r := g.Size(); c != 2 || r != 2 {
		t.Errorf("Size() = %dx%d, want 2x2", c, r)
	}
}

func TestResizeGrowingFillsWithBlanks(t *testing.T) {
	g := New(2, 2, fg, bg)
	g.SetString(0, 0, "ab", fg, bg, 0)

	g.Resize(4, 4)

	if got := g.At(0, 0).Rune; got != 'a' {
		t.Errorf("At(0,0) = %q, want 'a'", got)
	}
	if got := g.At(3, 3); got.Rune != ' ' || got.BG != bg {
		t.Errorf("At(3,3) = %+v, want a blank in the default colours", got)
	}
}

func TestResizeForcesFullRepaint(t *testing.T) {
	g := New(4, 4, fg, bg)
	g.ClearDirty()

	g.Resize(6, 6)

	if !g.RowDirty(5) {
		t.Fatal("a resize left rows clean; the new area would never be painted")
	}
}

func TestBGRunsCollapsesAUniformRow(t *testing.T) {
	g := New(80, 1, fg, bg)

	var runs int
	g.BGRuns(0, func(x0, x1 int, c color.RGBA) { runs++ })

	if runs != 1 {
		t.Fatalf("a uniform row produced %d runs, want 1", runs)
	}
}

func TestBGRunsSplitsOnColourChange(t *testing.T) {
	other := color.RGBA{0x11, 0x22, 0x33, 0xff}
	g := New(6, 1, fg, bg)
	g.Set(2, 0, Cell{Rune: 'a', FG: fg, BG: other})
	g.Set(3, 0, Cell{Rune: 'b', FG: fg, BG: other})

	type run struct {
		x0, x1 int
		c      color.RGBA
	}
	var got []run
	g.BGRuns(0, func(x0, x1 int, c color.RGBA) {
		got = append(got, run{x0, x1, c})
	})

	want := []run{{0, 2, bg}, {2, 4, other}, {4, 6, bg}}
	if len(got) != len(want) {
		t.Fatalf("got %d runs %v, want %d", len(got), got, len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("run %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestBGRunsResolvesReverseVideo(t *testing.T) {
	g := New(2, 1, fg, bg)
	g.Set(0, 0, Cell{Rune: 'a', FG: fg, BG: bg, Attr: AttrReverse})

	var first color.RGBA
	g.BGRuns(0, func(x0, x1 int, c color.RGBA) {
		if x0 == 0 {
			first = c
		}
	})

	if first != fg {
		t.Fatalf("reverse cell background = %v, want the foreground %v", first, fg)
	}
	if got := g.FGOf(0, 0); got != bg {
		t.Fatalf("reverse cell foreground = %v, want the background %v", got, bg)
	}
}

func TestBGRunsOutOfRangeRowIsSilent(t *testing.T) {
	g := New(4, 2, fg, bg)
	g.BGRuns(-1, func(int, int, color.RGBA) {
		t.Fatal("callback ran for a negative row")
	})
	g.BGRuns(2, func(int, int, color.RGBA) {
		t.Fatal("callback ran for a row past the end")
	})
}

func TestSetWideOccupiesTwoColumns(t *testing.T) {
	g := New(6, 1, fg, bg)

	n := g.SetWide(1, 0, Cell{Rune: '世', FG: fg, BG: bg, Width: 2})

	if n != 2 {
		t.Fatalf("SetWide consumed %d columns, want 2", n)
	}
	if got := g.At(1, 0); got.Rune != '世' || got.Width != 2 {
		t.Errorf("lead cell = %+v, want 世 width 2", got)
	}
	if got := g.At(2, 0); got.Width != 0 {
		t.Errorf("continuation cell width = %d, want 0", got.Width)
	}
}

func TestSetWideRefusesToStraddleTheRowEnd(t *testing.T) {
	g := New(4, 1, fg, bg)

	n := g.SetWide(3, 0, Cell{Rune: '世', FG: fg, BG: bg, Width: 2})

	if n != 0 {
		t.Fatalf("SetWide consumed %d columns at the last column, want 0", n)
	}
	if got := g.At(3, 0).Rune; got != ' ' {
		t.Errorf("At(3,0) = %q, want it left blank", got)
	}
}

// Overwriting either half of a double-width character has to clear the
// other half, or a stray glyph is left behind with no lead cell.
func TestSetWideClearsTheOtherHalfOfAnOverwrittenWideCell(t *testing.T) {
	cases := []struct {
		name       string
		overwriteX int
		checkX     int
	}{
		{"overwrite the lead half", 1, 2},
		{"overwrite the continuation half", 2, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := New(6, 1, fg, bg)
			g.SetWide(1, 0, Cell{Rune: '世', FG: fg, BG: bg, Width: 2})

			g.SetWide(tc.overwriteX, 0, Cell{Rune: 'a', FG: fg, BG: bg, Width: 1})

			got := g.At(tc.checkX, 0)
			if got.Rune != ' ' || got.Width != 1 {
				t.Errorf("orphaned half = %+v, want a blank width-1 cell", got)
			}
		})
	}
}

func TestSetStringHandlesWideAndCombining(t *testing.T) {
	g := New(10, 1, fg, bg)

	end := g.SetString(0, 0, "a世éb", fg, bg, 0)

	if end != 5 {
		t.Fatalf("end column = %d, want 5 (1 + 2 + 1 + 1)", end)
	}
	if got := g.At(1, 0); got.Rune != '世' || got.Width != 2 {
		t.Errorf("At(1,0) = %+v, want 世 width 2", got)
	}
	if got := g.At(2, 0).Width; got != 0 {
		t.Errorf("At(2,0).Width = %d, want 0", got)
	}
	if got := g.At(3, 0); got.Rune != 'e' || len(got.Comb) != 1 || got.Comb[0] != '́' {
		t.Errorf("At(3,0) = %+v, want e with one combining mark", got)
	}
	if got := g.At(4, 0).Rune; got != 'b' {
		t.Errorf("At(4,0) = %q, want 'b'", got)
	}
}

func TestCellEqualComparesCombiningMarks(t *testing.T) {
	a := Cell{Rune: 'e', Comb: []rune{'́'}, Width: 1}
	b := Cell{Rune: 'e', Comb: []rune{'́'}, Width: 1}
	c := Cell{Rune: 'e', Comb: []rune{'̂'}, Width: 1}

	if !a.Equal(b) {
		t.Error("cells with identical combining marks compared unequal")
	}
	if a.Equal(c) {
		t.Error("cells with different combining marks compared equal")
	}
}

// Set uses Equal for the idle-repaint check, so a cell differing only in
// its combining marks must still dirty the row.
func TestSetDirtiesOnACombiningMarkChange(t *testing.T) {
	g := New(4, 1, fg, bg)
	g.Set(0, 0, Cell{Rune: 'e', Comb: []rune{'́'}, FG: fg, BG: bg, Width: 1})
	g.ClearDirty()

	g.Set(0, 0, Cell{Rune: 'e', Comb: []rune{'̂'}, FG: fg, BG: bg, Width: 1})

	if !g.AnyDirty() {
		t.Fatal("changing a combining mark left the grid clean")
	}
}

func TestSetCursorDirtiesBothTheOldAndNewRow(t *testing.T) {
	g := New(4, 4, fg, bg)
	g.SetCursor(Cursor{X: 0, Y: 1, Visible: true})
	g.ClearDirty()

	g.SetCursor(Cursor{X: 0, Y: 3, Visible: true})

	if !g.RowDirty(1) {
		t.Error("the row the cursor left is clean; it would keep the old cursor")
	}
	if !g.RowDirty(3) {
		t.Error("the row the cursor arrived at is clean; it would not appear")
	}
	if g.RowDirty(2) {
		t.Error("an untouched row was dirtied")
	}
}

func TestSetCursorUnchangedDoesNotDirty(t *testing.T) {
	g := New(4, 4, fg, bg)
	c := Cursor{X: 1, Y: 1, Visible: true}
	g.SetCursor(c)
	g.ClearDirty()

	g.SetCursor(c)

	if g.AnyDirty() {
		t.Fatal("re-setting the same cursor dirtied the grid")
	}
}

func TestHidingTheCursorDirtiesItsRow(t *testing.T) {
	g := New(4, 4, fg, bg)
	g.SetCursor(Cursor{X: 0, Y: 2, Visible: true})
	g.ClearDirty()

	g.SetCursor(Cursor{X: 0, Y: 2, Visible: false})

	if !g.RowDirty(2) {
		t.Fatal("hiding the cursor left its row clean; it would stay on screen")
	}
}

// Narrowing the grid can cut a double-width character in half. Either
// surviving half draws wrong on its own.
func TestResizeBlanksHalvesOfWideCellsCutByNarrowing(t *testing.T) {
	g := New(6, 1, fg, bg)
	g.SetWide(4, 0, Cell{Rune: '世', FG: fg, BG: bg, Width: 2})

	g.Resize(5, 1)

	if got := g.At(4, 0); got.Width == 2 {
		t.Errorf("At(4,0) = %+v, want the orphaned lead cell blanked", got)
	}
}

func TestResizeBlanksAnOrphanedContinuationAtColumnZero(t *testing.T) {
	g := New(6, 2, fg, bg)
	g.SetWide(0, 0, Cell{Rune: '世', FG: fg, BG: bg, Width: 2})
	// Growing is not the interesting case; force the lead out of view by
	// checking the invariant directly after a resize that keeps column 0.
	g.Set(0, 0, Cell{Rune: 0, FG: fg, BG: bg, Width: 0})

	g.Resize(4, 2)

	if got := g.At(0, 0); got.Width == 0 {
		t.Errorf("At(0,0) = %+v, want the orphaned continuation blanked", got)
	}
}
