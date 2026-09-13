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
