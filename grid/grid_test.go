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

	for y := range 4 {
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
	for y := range 4 {
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

// A cursor that starts or stops blinking looks different without
// moving, so the change has to reach the renderer and dirty the row it
// sits on.
func TestAskingTheCursorToBlinkDirtiesItsRow(t *testing.T) {
	g := New(4, 4, fg, bg)
	g.SetCursor(Cursor{X: 1, Y: 2, Visible: true})
	g.ClearDirty()

	g.SetCursor(Cursor{X: 1, Y: 2, Visible: true, Blink: true})

	if !g.Cursor().Blink {
		t.Fatal("the grid did not keep the cursor's blink")
	}
	if !g.RowDirty(2) {
		t.Error("the cursor's row is clean, so nothing would redraw it")
	}
	for _, y := range []int{0, 1, 3} {
		if g.RowDirty(y) {
			t.Errorf("row %d was dirtied and the cursor is not on it", y)
		}
	}

	// And asking for the same thing again costs nothing.
	g.ClearDirty()
	g.SetCursor(Cursor{X: 1, Y: 2, Visible: true, Blink: true})
	if g.AnyDirty() {
		t.Error("re-setting the same blinking cursor dirtied the grid")
	}
}

// MarkRowDirty is how a caller says a row looks different for a reason
// no cell of it records, such as a blinking cursor changing phase.
func TestMarkRowDirtyMarksOneRow(t *testing.T) {
	g := New(4, 4, fg, bg)
	g.ClearDirty()

	g.MarkRowDirty(2)

	if !g.RowDirty(2) {
		t.Fatal("the marked row is clean")
	}
	for _, y := range []int{0, 1, 3} {
		if g.RowDirty(y) {
			t.Errorf("row %d was dirtied too", y)
		}
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
// wantWidthInvariant asserts that every double-width character in the
// grid still has both halves. A lone half draws a stray glyph, and a
// lone continuation makes later writes blank an innocent neighbour.
func wantWidthInvariant(t *testing.T, g *Grid) {
	t.Helper()
	cols, rows := g.Size()
	for y := range rows {
		for x := range cols {
			switch g.At(x, y).Width {
			case 2:
				if x+1 >= cols || g.At(x+1, y).Width != 0 {
					t.Errorf("lead cell at %d,%d has no continuation", x, y)
				}
			case 0:
				if x == 0 || g.At(x-1, y).Width != 2 {
					t.Errorf("continuation at %d,%d has no lead", x, y)
				}
			}
		}
	}
}

func TestResizeBlanksHalvesOfWideCellsCutByNarrowing(t *testing.T) {
	g := New(6, 1, fg, bg)
	g.SetWide(4, 0, Cell{Rune: '世', FG: fg, BG: bg, Width: 2})

	g.Resize(5, 1)

	if got := g.At(4, 0); got.Rune != ' ' || got.Width != 1 {
		t.Errorf("At(4,0) = %+v, want a blank width-1 cell", got)
	}
	wantWidthInvariant(t, g)
}

// Repairing one broken pair can expose another, so the fix-up has to
// scan the row rather than check its edges.
func TestResizeRepairsEveryBrokenPairInARow(t *testing.T) {
	g := New(8, 1, fg, bg)
	// A continuation at column 0 with no lead, and a lead at the far end
	// whose continuation the narrowing will cut off.
	g.Set(0, 0, Cell{Rune: 0, FG: fg, BG: bg, Width: 0})
	g.Set(1, 0, Cell{Rune: 0, FG: fg, BG: bg, Width: 0})
	g.SetWide(5, 0, Cell{Rune: '世', FG: fg, BG: bg, Width: 2})

	g.Resize(6, 1)

	wantWidthInvariant(t, g)
}

// A row left with a lead cell but no continuation makes the next write
// at the neighbouring column blank the wrong cell.
func TestRepairedRowDoesNotEatTheNextCharacterWritten(t *testing.T) {
	g := New(6, 1, fg, bg)
	g.SetWide(0, 0, Cell{Rune: '世', FG: fg, BG: bg, Width: 2})
	g.Set(0, 0, Cell{Rune: 0, FG: fg, BG: bg, Width: 0}) // break it

	g.Resize(4, 1)
	g.SetString(0, 0, "ab", fg, bg, 0)

	if got := g.At(0, 0).Rune; got != 'a' {
		t.Errorf("At(0,0) = %q, want 'a'; writing 'b' blanked its neighbour", got)
	}
	if got := g.At(1, 0).Rune; got != 'b' {
		t.Errorf("At(1,0) = %q, want 'b'", got)
	}
}

// Rewriting the same wide character must not dirty the row, or a screen
// full of CJK repaints every frame.
func TestSetWideIdenticalContentDoesNotDirty(t *testing.T) {
	g := New(6, 1, fg, bg)
	c := Cell{Rune: '世', FG: fg, BG: bg, Width: 2}
	g.SetWide(1, 0, c)
	g.ClearDirty()

	g.SetWide(1, 0, c)

	if g.AnyDirty() {
		t.Fatal("rewriting an identical wide cell dirtied the grid")
	}
}

func TestSetStringIdenticalWideContentDoesNotDirty(t *testing.T) {
	g := New(10, 1, fg, bg)
	g.SetString(0, 0, "a世b", fg, bg, 0)
	g.ClearDirty()

	g.SetString(0, 0, "a世b", fg, bg, 0)

	if g.AnyDirty() {
		t.Fatal("rewriting an identical string containing a wide cell dirtied the grid")
	}
}

func TestSetWideClampsAnOutOfRangeWidth(t *testing.T) {
	g := New(6, 1, fg, bg)

	n := g.SetWide(0, 0, Cell{Rune: 'x', FG: fg, BG: bg, Width: 5})

	if n != 2 {
		t.Errorf("SetWide reported %d columns for width 5, want 2", n)
	}
	wantWidthInvariant(t, g)
}

// A cluster that cannot fit must not abandon the rest of the string
// silently, and must not leave stale content under itself.
func TestSetStringWideClusterAtTheRowEndBlanksAndStops(t *testing.T) {
	g := New(3, 1, fg, bg)
	g.SetString(0, 0, "XXX", fg, bg, 0)

	end := g.SetString(0, 0, "ab世cd", fg, bg, 0)

	if end != 3 {
		t.Errorf("end column = %d, want 3 (the row is full)", end)
	}
	if got := g.At(2, 0).Rune; got != ' ' {
		t.Errorf("At(2,0) = %q, want a blank, not stale content", got)
	}
}

// Only real zero-width marks stack on a base rune. The trailing runes of
// an emoji ZWJ sequence are full glyphs and would draw on top of it.
func TestClusterCellKeepsOnlyZeroWidthMarks(t *testing.T) {
	c := ClusterCell("\U0001F468‍\U0001F469", 2)
	for _, r := range c.Comb {
		if RuneWidth(r) != 0 {
			t.Errorf("Comb holds %U, which is %d columns wide", r, RuneWidth(r))
		}
	}
}

func TestClusterCellReplacesControlCharacters(t *testing.T) {
	if got := ClusterCell("\t", 1).Rune; got != ' ' {
		t.Errorf("tab became %q, want a space", got)
	}
}

func TestCombiningMarkOnASpaceIsKept(t *testing.T) {
	g := New(4, 1, fg, bg)
	g.SetString(0, 0, " ́", fg, bg, 0)
	if got := g.At(0, 0); got.Rune != ' ' || len(got.Comb) != 1 {
		t.Fatalf("At(0,0) = %+v, want a space carrying one mark", got)
	}
}

func TestSelectionContainsFlowingRange(t *testing.T) {
	s := Selection{Anchor: Point{2, 1}, Cursor: Point{3, 3}, Active: true}
	cases := []struct {
		x, y int
		want bool
	}{
		{1, 1, false}, // before the start on the first row
		{2, 1, true},
		{9, 1, true}, // the rest of the first row
		{0, 2, true}, // all of a middle row
		{9, 2, true},
		{3, 3, true},
		{4, 3, false}, // past the end on the last row
		{0, 0, false}, // a row above
		{0, 4, false}, // a row below
	}
	for _, tc := range cases {
		if got := s.Contains(tc.x, tc.y); got != tc.want {
			t.Errorf("Contains(%d,%d) = %v, want %v", tc.x, tc.y, got, tc.want)
		}
	}
}

// Dragging backwards selects the same cells as dragging forwards.
func TestSelectionIsDirectionIndependent(t *testing.T) {
	fwd := Selection{Anchor: Point{2, 1}, Cursor: Point{3, 3}, Active: true}
	back := Selection{Anchor: Point{3, 3}, Cursor: Point{2, 1}, Active: true}
	for y := range 5 {
		for x := range 10 {
			if fwd.Contains(x, y) != back.Contains(x, y) {
				t.Fatalf("at %d,%d: forwards %v, backwards %v",
					x, y, fwd.Contains(x, y), back.Contains(x, y))
			}
		}
	}
}

func TestBlockSelectionCoversTheRectangle(t *testing.T) {
	s := Selection{Anchor: Point{2, 1}, Cursor: Point{4, 3}, Active: true, Block: true}
	if !s.Contains(3, 2) {
		t.Error("inside the rectangle reported as outside")
	}
	if s.Contains(5, 2) {
		t.Error("right of the rectangle reported as inside")
	}
	if s.Contains(1, 2) {
		t.Error("left of the rectangle reported as inside")
	}
}

func TestInactiveSelectionContainsNothing(t *testing.T) {
	s := Selection{Anchor: Point{0, 0}, Cursor: Point{9, 9}}
	if s.Contains(1, 1) {
		t.Fatal("an inactive selection reported a cell as selected")
	}
}

func TestSetSelectionDirtiesTheRowsItCovers(t *testing.T) {
	g := New(10, 5, fg, bg)
	g.ClearDirty()

	g.SetSelection(Selection{Anchor: Point{0, 1}, Cursor: Point{9, 2}, Active: true})

	for y := range 5 {
		want := y == 1 || y == 2
		if got := g.RowDirty(y); got != want {
			t.Errorf("row %d dirty = %v, want %v", y, got, want)
		}
	}
}

// Clearing a selection has to repaint the rows it used to cover, or the
// highlight stays on screen.
func TestClearSelectionDirtiesTheOldRows(t *testing.T) {
	g := New(10, 5, fg, bg)
	g.SetSelection(Selection{Anchor: Point{0, 3}, Cursor: Point{9, 3}, Active: true})
	g.ClearDirty()

	g.ClearSelection()

	if !g.RowDirty(3) {
		t.Fatal("the row the selection left is clean; the highlight would stay")
	}
}

func TestSelectionChangesTheBackground(t *testing.T) {
	g := New(4, 1, fg, bg)
	g.SetSelection(Selection{Anchor: Point{1, 0}, Cursor: Point{2, 0}, Active: true})

	var runs []color.RGBA
	g.BGRuns(0, func(x0, x1 int, c color.RGBA) { runs = append(runs, c) })

	if len(runs) != 3 {
		t.Fatalf("got %d background runs, want 3 (before, selected, after)", len(runs))
	}
	if runs[1] != g.SelectionBG {
		t.Errorf("selected run = %v, want the selection colour %v", runs[1], g.SelectionBG)
	}
}

func TestSelectedText(t *testing.T) {
	g := New(10, 3, fg, bg)
	g.SetString(0, 0, "hello", fg, bg, 0)
	g.SetString(0, 1, "world", fg, bg, 0)

	g.SetSelection(Selection{Anchor: Point{2, 0}, Cursor: Point{2, 1}, Active: true})

	if got, want := g.SelectedText(), "llo\nwor"; got != want {
		t.Fatalf("SelectedText() = %q, want %q", got, want)
	}
}

// Trailing blanks are trimmed per row, which is what makes a selected
// command line paste back as a command line.
func TestSelectedTextTrimsTrailingBlanks(t *testing.T) {
	g := New(20, 2, fg, bg)
	g.SetString(0, 0, "ls -l", fg, bg, 0)

	g.SetSelection(Selection{Anchor: Point{0, 0}, Cursor: Point{19, 0}, Active: true})

	if got, want := g.SelectedText(), "ls -l"; got != want {
		t.Fatalf("SelectedText() = %q, want %q", got, want)
	}
}

// The continuation half of a wide character has no rune of its own; its
// lead cell already contributed one.
func TestSelectedTextCountsAWideCharacterOnce(t *testing.T) {
	g := New(10, 1, fg, bg)
	g.SetString(0, 0, "a世b", fg, bg, 0)

	g.SetSelection(Selection{Anchor: Point{0, 0}, Cursor: Point{4, 0}, Active: true})

	if got, want := g.SelectedText(), "a世b"; got != want {
		t.Fatalf("SelectedText() = %q, want %q", got, want)
	}
}

func TestSelectedTextIsEmptyWhenInactive(t *testing.T) {
	g := New(10, 1, fg, bg)
	g.SetString(0, 0, "hello", fg, bg, 0)
	if got := g.SelectedText(); got != "" {
		t.Fatalf("SelectedText() = %q with no selection, want empty", got)
	}
}

// TestStringWidthMatchesWhatSetStringSpends checks the measure a caller
// sizing a label relies on. Asking for display width instead would come
// up short for a control character or a zero-width one, and the label
// would be drawn with its end clipped off.
func TestStringWidthMatchesWhatSetStringSpends(t *testing.T) {
	for _, s := range []string{
		"", "abc", "日本", "a日b", "éx", "́abc",
		"a\tb", "\x07bell", "x​z", "🚀 go",
	} {
		g := New(40, 1, fg, bg)

		spent := g.SetString(0, 0, s, fg, bg, 0)

		if got := StringWidth(s); got != spent {
			t.Errorf("StringWidth(%q) = %d, but SetString spent %d columns", s, got, spent)
		}
	}
}

// A run shorter than the cell holds fills from the right, so the newest
// second is always in the same place as the graph lengthens.
func TestGraphFillsFromTheRight(t *testing.T) {
	art := Graph([]int{3, 7})
	for i := range ArtGraphBars - 2 {
		if got := art.Bar(i); got != 0 {
			t.Fatalf("bar %d of a two-second run is %d, want nothing", i, got)
		}
	}
	if got := art.Bar(ArtGraphBars - 2); got != 3 {
		t.Errorf("the older second is at %d", got)
	}
	if got := art.Bar(ArtGraphBars - 1); got != 7 {
		t.Errorf("the newest second is %d, want it last", got)
	}
}

// Art is packed into the cell and read back the same, so a row can carry
// a small drawing without a table beside it.
func TestGraphPacksItsBars(t *testing.T) {
	want := []int{0, 1, 2, 3, 15, 8, 0, 4, 9, 15, 1, 6}
	art := Graph(want)
	if art.Kind != ArtGraph {
		t.Fatalf("kind = %v", art.Kind)
	}
	for i, h := range want {
		if got := art.Bar(i); got != h {
			t.Fatalf("bar %d = %d, want %d", i, got, h)
		}
	}
	// Past the end there is nothing.
	if got := art.Bar(ArtGraphBars); got != 0 {
		t.Errorf("bar past the end = %d", got)
	}
	if got := art.Bar(-1); got != 0 {
		t.Errorf("bar before the start = %d", got)
	}
}

// A height taller than the cell can show is cut to what it can, and bars
// past the end are dropped rather than running into the next one.
func TestGraphClampsWhatItIsGiven(t *testing.T) {
	art := Graph([]int{99, -4})
	// Right aligned: two bars land in the last two places.
	if got := art.Bar(ArtGraphBars - 2); got != ArtGraphMax {
		t.Errorf("a bar taller than the cell packs as %d, want %d", got, ArtGraphMax)
	}
	if got := art.Bar(ArtGraphBars - 1); got != 0 {
		t.Errorf("a bar below nothing packs as %d", got)
	}

	long := make([]int, ArtGraphBars+8)
	for i := range long {
		long[i] = ArtGraphMax
	}
	full := Graph(long)
	for i := range ArtGraphBars {
		if got := full.Bar(i); got != ArtGraphMax {
			t.Fatalf("bar %d = %d", i, got)
		}
	}
}

// Art that has changed is a cell that has changed, which is what keeps
// the grid's damage tracking covering it without being told.
func TestArtCountsAsAChange(t *testing.T) {
	g := New(4, 2, fg, bg)
	g.Set(0, 0, Cell{Rune: ' ', Width: 1, Art: Graph([]int{1, 2, 3})})
	g.ClearDirty()

	// The same art again changes nothing.
	g.Set(0, 0, Cell{Rune: ' ', Width: 1, Art: Graph([]int{1, 2, 3})})
	if g.AnyDirty() {
		t.Fatal("writing the same art dirtied the row")
	}
	// A different run does.
	g.Set(0, 0, Cell{Rune: ' ', Width: 1, Art: Graph([]int{1, 2, 4})})
	if !g.RowDirty(0) {
		t.Fatal("changing the art left the row clean")
	}
	g.ClearDirty()
	// And taking it away does.
	g.Set(0, 0, Cell{Rune: ' ', Width: 1})
	if !g.RowDirty(0) {
		t.Fatal("taking the art away left the row clean")
	}
}

// A graph says how many of its bars were really measured, so a run of
// two seconds is not drawn as ten quiet ones and two busy.
func TestGraphSaysHowManyBarsAreReal(t *testing.T) {
	if got := Graph([]int{3, 7}).Bars(); got != 2 {
		t.Errorf("a two-second run says %d bars", got)
	}
	long := make([]int, ArtGraphBars*2)
	if got := Graph(long).Bars(); got != ArtGraphBars {
		t.Errorf("a run longer than the cell says %d bars, want %d", got, ArtGraphBars)
	}
	if got := Graph(nil); got.Kind != ArtNone {
		t.Errorf("a run of nothing is %v, want no art at all", got)
	}
	if got := (Art{}).Bars(); got != 0 {
		t.Errorf("art that is not a graph says %d bars", got)
	}
	// The count does not disturb the bars themselves.
	art := Graph([]int{1, 2, 3})
	for i, want := range map[int]int{ArtGraphBars - 3: 1, ArtGraphBars - 2: 2, ArtGraphBars - 1: 3} {
		if got := art.Bar(i); got != want {
			t.Errorf("bar %d = %d, want %d", i, got, want)
		}
	}
}

// Filling a region never fills it with one cell's art: a little picture
// belongs to one cell, not to everything behind it.
func TestFillDoesNotSpreadArt(t *testing.T) {
	g := New(4, 2, fg, bg)
	g.View().Fill(Cell{Rune: ' ', Width: 1, Art: Graph([]int{1, 2})})
	for y := range 2 {
		for x := range 4 {
			if got := g.At(x, y).Art.Kind; got != ArtNone {
				t.Fatalf("cell %d,%d carries %v", x, y, got)
			}
		}
	}
}

// An icon is packed into the cell and read back the same, and one that
// does not exist is not an icon at all.
func TestIconRoundTrips(t *testing.T) {
	for _, want := range []IconKind{IconTerminal, IconCommand, IconFiles, IconTunnel} {
		art := Icon(want)
		if art.Kind != ArtIcon {
			t.Fatalf("Icon(%d) is %v", want, art.Kind)
		}
		got, ok := art.Icon()
		if !ok || got != want {
			t.Fatalf("Icon(%d) reads back as %d, %v", want, got, ok)
		}
	}
	for _, art := range []Art{{}, Graph([]int{1}), {Kind: ArtIcon, Data: uint64(numIcons)}} {
		if _, ok := art.Icon(); ok {
			t.Errorf("%v says it is an icon", art)
		}
	}
}
