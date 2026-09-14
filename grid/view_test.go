package grid

import (
	"image/color"
	"math"
	"testing"
)

// rowText reads a row of the grid back as a string, for asserting on
// what a view did and did not touch.
func rowText(g *Grid, y int) string {
	out := make([]rune, 0, g.cols)
	for x := 0; x < g.cols; x++ {
		c := g.At(x, y)
		if c.Width == 0 {
			continue
		}
		out = append(out, c.Rune)
	}
	return string(out)
}

func TestViewOffsetsWrites(t *testing.T) {
	g := New(10, 4, fg, bg)
	v := g.View().Sub(3, 1, 4, 2)

	v.SetString(0, 0, "ab", fg, bg, 0)

	if got := g.At(3, 1).Rune; got != 'a' {
		t.Errorf("grid 3,1 = %q, want 'a'", got)
	}
	if got := g.At(4, 1).Rune; got != 'b' {
		t.Errorf("grid 4,1 = %q, want 'b'", got)
	}
	if got := g.At(0, 0).Rune; got != ' ' {
		t.Errorf("grid 0,0 = %q, want a blank: the write escaped the view", got)
	}
}

func TestViewClipsWritesToItsOwnArea(t *testing.T) {
	g := New(10, 4, fg, bg)
	v := g.View().Sub(2, 1, 3, 1)

	// Past the right edge, past the bottom, and behind the origin.
	v.Set(3, 0, Cell{Rune: 'x', FG: fg, BG: bg, Width: 1})
	v.Set(0, 1, Cell{Rune: 'y', FG: fg, BG: bg, Width: 1})
	v.Set(-1, 0, Cell{Rune: 'z', FG: fg, BG: bg, Width: 1})

	if got := rowText(g, 1); got != "          " {
		t.Errorf("row 1 = %q, want all blanks", got)
	}
	if got := rowText(g, 2); got != "          " {
		t.Errorf("row 2 = %q, want all blanks", got)
	}
}

func TestViewSubComposes(t *testing.T) {
	g := New(10, 4, fg, bg)
	v := g.View().Sub(2, 1, 6, 3).Sub(1, 1, 2, 1)

	if cols, rows := v.Size(); cols != 2 || rows != 1 {
		t.Fatalf("size = %dx%d, want 2x1", cols, rows)
	}
	v.Set(0, 0, Cell{Rune: 'q', FG: fg, BG: bg, Width: 1})

	if got := g.At(3, 2).Rune; got != 'q' {
		t.Errorf("grid 3,2 = %q, want 'q': nested offsets did not add up", got)
	}
}

func TestViewSubClipsToItsParent(t *testing.T) {
	g := New(10, 4, fg, bg)

	// Wider and taller than the parent, and starting behind its origin.
	v := g.View().Sub(8, 3, 5, 5)
	if cols, rows := v.Size(); cols != 2 || rows != 1 {
		t.Errorf("oversized sub = %dx%d, want 2x1", cols, rows)
	}

	v = g.View().Sub(-2, -1, 5, 3)
	if cols, rows := v.Size(); cols != 3 || rows != 2 {
		t.Errorf("negative sub = %dx%d, want 3x2", cols, rows)
	}
	v.Set(0, 0, Cell{Rune: 'a', FG: fg, BG: bg, Width: 1})
	if got := g.At(0, 0).Rune; got != 'a' {
		t.Errorf("grid 0,0 = %q, want 'a': a clipped origin should land at 0,0", got)
	}
}

func TestViewSubOutsideTheParentIsEmpty(t *testing.T) {
	g := New(10, 4, fg, bg)
	g.ClearDirty()
	v := g.View().Sub(20, 20, 4, 4)

	if cols, rows := v.Size(); cols != 0 || rows != 0 {
		t.Fatalf("size = %dx%d, want 0x0", cols, rows)
	}
	v.Set(0, 0, Cell{Rune: 'x', FG: fg, BG: bg, Width: 1})
	v.Fill(Cell{Rune: 'y', FG: fg, BG: bg, Width: 1})
	if g.AnyDirty() {
		t.Error("an empty view wrote to the grid")
	}
}

func TestZeroViewIsSafe(t *testing.T) {
	var v View

	if cols, rows := v.Size(); cols != 0 || rows != 0 {
		t.Errorf("size = %dx%d, want 0x0", cols, rows)
	}
	if got := v.At(0, 0).Rune; got != ' ' {
		t.Errorf("At = %q, want a blank", got)
	}
	// None of these may panic.
	v.Set(0, 0, Cell{Rune: 'x', Width: 1})
	v.SetWide(0, 0, Cell{Rune: 'x', Width: 2})
	v.SetString(0, 0, "hello", fg, bg, 0)
	v.Fill(Cell{Rune: 'y', Width: 1})
	v.Clear()
	sub := v.Sub(0, 0, 2, 2)
	if sub.g != nil {
		t.Error("Sub of the zero view produced a grid")
	}
	if cols, rows := sub.Size(); cols != 0 || rows != 0 {
		t.Errorf("Sub of the zero view = %dx%d, want 0x0", cols, rows)
	}
}

func TestViewWideCellCannotSpillPastTheViewEdge(t *testing.T) {
	g := New(10, 2, fg, bg)
	v := g.View().Sub(2, 0, 3, 1)
	wide := Cell{Rune: '世', FG: fg, BG: bg, Width: 2}

	if n := v.SetWide(2, 0, wide); n != 0 {
		t.Errorf("SetWide at the last view column = %d, want 0", n)
	}
	if got := g.At(5, 0).Rune; got != ' ' {
		t.Errorf("grid 5,0 = %q, want a blank: the wide cell spilled out of the view", got)
	}
	if n := v.SetWide(1, 0, wide); n != 2 {
		t.Errorf("SetWide with room = %d, want 2", n)
	}
	if got := g.At(3, 0).Rune; got != '世' {
		t.Errorf("grid 3,0 = %q, want the wide rune", got)
	}
	if got := g.At(4, 0).Width; got != 0 {
		t.Errorf("grid 4,0 width = %d, want 0 for a continuation", got)
	}
}

func TestViewSetStringStopsAtTheViewEdge(t *testing.T) {
	g := New(10, 2, fg, bg)
	v := g.View().Sub(2, 0, 3, 1)

	if got := v.SetString(0, 0, "abcdef", fg, bg, 0); got != 3 {
		t.Errorf("SetString returned %d, want 3", got)
	}
	if got := rowText(g, 0); got != "  abc     " {
		t.Errorf("row 0 = %q, want %q", got, "  abc     ")
	}
}

func TestViewSetStringBlanksAWideClusterThatWouldStraddleTheEdge(t *testing.T) {
	g := New(10, 2, fg, bg)
	v := g.View().Sub(2, 0, 3, 1)

	if got := v.SetString(0, 0, "ab世", fg, bg, 0); got != 3 {
		t.Errorf("SetString returned %d, want 3", got)
	}
	if got := g.At(4, 0).Rune; got != ' ' {
		t.Errorf("grid 4,0 = %q, want a blank where the wide cluster did not fit", got)
	}
	if got := g.At(5, 0).Rune; got != ' ' {
		t.Errorf("grid 5,0 = %q, want an untouched blank outside the view", got)
	}
}

func TestViewFillCoversOnlyItsOwnArea(t *testing.T) {
	g := New(6, 3, fg, bg)
	v := g.View().Sub(1, 1, 3, 1)

	v.Fill(Cell{Rune: '#', FG: fg, BG: bg, Width: 1})

	if got := rowText(g, 1); got != " ###  " {
		t.Errorf("row 1 = %q, want %q", got, " ###  ")
	}
	if got := rowText(g, 0); got != "      " {
		t.Errorf("row 0 = %q, want all blanks", got)
	}
}

func TestViewFillRepairsAWideCellCutByTheViewEdge(t *testing.T) {
	g := New(8, 1, fg, bg)
	wide := Cell{Rune: '世', FG: fg, BG: bg, Width: 2}
	// A wide cell straddling each edge of the view below.
	g.SetWide(1, 0, wide)
	g.SetWide(4, 0, wide)

	g.View().Sub(2, 0, 3, 1).Fill(Cell{Rune: '#', FG: fg, BG: bg, Width: 1})

	if got := g.At(1, 0).Rune; got != ' ' {
		t.Errorf("grid 1,0 = %q, want a blank: the orphaned lead was left behind", got)
	}
	if got := g.At(5, 0).Width; got != 1 {
		t.Errorf("grid 5,0 width = %d, want 1: the orphaned continuation was left behind", got)
	}
	if got := g.At(5, 0).Rune; got != ' ' {
		t.Errorf("grid 5,0 = %q, want a blank", got)
	}
	for _, x := range []int{0, 6, 7} {
		if got := g.At(x, 0); got.Rune != ' ' || got.Width != 1 {
			t.Errorf("grid %d,0 = %+v, want an untouched blank outside the view", x, got)
		}
	}
}

func TestViewClearUsesTheGridDefaults(t *testing.T) {
	red := color.RGBA{0xff, 0x00, 0x00, 0xff}
	g := New(6, 2, fg, red)
	g.View().Sub(1, 0, 2, 1).Fill(Cell{Rune: '#', FG: red, BG: fg, Width: 1})

	g.View().Sub(1, 0, 2, 1).Clear()

	c := g.At(1, 0)
	if c.Rune != ' ' || c.BG != red || c.FG != fg {
		t.Errorf("cleared cell = %+v, want a blank in the grid defaults", c)
	}
}

func TestViewAtReadsThroughTheOffset(t *testing.T) {
	g := New(10, 4, fg, bg)
	g.Set(4, 2, Cell{Rune: 'k', FG: fg, BG: bg, Width: 1})
	v := g.View().Sub(3, 1, 4, 2)

	if got := v.At(1, 1).Rune; got != 'k' {
		t.Errorf("At(1,1) = %q, want 'k'", got)
	}
	if got := v.At(9, 9).Rune; got != ' ' {
		t.Errorf("At outside the view = %q, want a blank", got)
	}
}

// TestSetStringGoldenCases pins View.SetString against expectations
// written out by hand. Grid.SetString delegates to it, so a differential
// test between the two would compare a function with itself.
func TestSetStringGoldenCases(t *testing.T) {
	for _, tc := range []struct {
		name    string
		cols    int
		x, y    int
		s       string
		wantN   int
		wantRow string
	}{
		{name: "ascii", cols: 6, s: "abc", wantN: 3, wantRow: "abc   "},
		{name: "fills the row exactly", cols: 3, s: "abc", wantN: 3, wantRow: "abc"},
		{name: "clipped at the row end", cols: 3, s: "abcdef", wantN: 3, wantRow: "abc"},
		{name: "offset start", cols: 6, x: 2, s: "ab", wantN: 4, wantRow: "  ab  "},
		{name: "wide cluster", cols: 6, s: "a世b", wantN: 4, wantRow: "a世b  "},
		{
			name: "wide cluster with one column left",
			cols: 4, s: "abc世", wantN: 4, wantRow: "abc ",
		},
		{
			name:  "combining mark shares its base cell",
			cols:  4,
			s:     "éx",
			wantN: 2, wantRow: "ex  ", // rowText shows base runes only
		},
		{name: "empty string", cols: 4, s: "", wantN: 0, wantRow: "    "},
		{name: "x already past the end", cols: 4, x: 4, s: "ab", wantN: 4, wantRow: "    "},
		{name: "x far past the end returns x", cols: 4, x: 9, s: "ab", wantN: 9, wantRow: "    "},
		{name: "row out of range", cols: 4, y: 9, s: "ab", wantN: 4, wantRow: "    "},
		{name: "negative x reports the row full", cols: 4, x: -1, s: "ab", wantN: 4, wantRow: "    "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := New(tc.cols, 1, fg, bg)
			v := g.View()

			if got := v.SetString(tc.x, tc.y, tc.s, fg, bg, 0); got != tc.wantN {
				t.Errorf("SetString returned %d, want %d", got, tc.wantN)
			}
			if got := rowText(g, 0); got != tc.wantRow {
				t.Errorf("row = %q, want %q", got, tc.wantRow)
			}
		})
	}
}

// TestViewSetRefusesAPartnerOutsideTheView checks the containment rule
// that stops a widget claiming half of a double-width pair it does not
// own, which a later repair would then blank.
func TestViewSetRefusesAPartnerOutsideTheView(t *testing.T) {
	t.Run("lead in the last column", func(t *testing.T) {
		g := New(10, 1, fg, bg)
		g.View().Sub(5, 0, 5, 1).SetString(0, 0, "N", fg, bg, 0)
		w := g.View().Sub(0, 0, 5, 1)

		w.Set(4, 0, Cell{Rune: 'X', FG: fg, BG: bg, Width: 2})
		w.Fill(Cell{Rune: '.', FG: fg, BG: bg, Width: 1})

		if got := g.At(5, 0).Rune; got != 'N' {
			t.Errorf("neighbour cell = %q, want 'N': the widget blanked it", got)
		}
	})

	t.Run("continuation in the first column", func(t *testing.T) {
		g := New(10, 1, fg, bg)
		g.View().Sub(0, 0, 5, 1).SetString(4, 0, "N", fg, bg, 0)
		w := g.View().Sub(5, 0, 5, 1)

		w.Set(0, 0, Cell{Rune: ' ', FG: fg, BG: bg, Width: 0})
		w.SetWide(0, 0, Cell{Rune: 'a', FG: fg, BG: bg, Width: 1})

		if got := g.At(4, 0).Rune; got != 'N' {
			t.Errorf("neighbour cell = %q, want 'N': the widget blanked it", got)
		}
	})
}

// TestViewWriteLeavesALoneHalfOutsideAlone checks every write path, not
// just Fill. A lone half on the far side of the view boundary is not
// half of anything, so nothing may blank it.
func TestViewWriteLeavesALoneHalfOutsideAlone(t *testing.T) {
	// The view owns columns 4..7. Column 3 belongs to a neighbour.
	for _, orient := range []struct {
		name  string
		setUp func(g *Grid)
	}{
		{
			name: "lone lead outside",
			setUp: func(g *Grid) {
				g.Set(3, 0, Cell{Rune: 'N', FG: fg, BG: bg, Width: 2}) // no continuation
				g.Set(4, 0, Cell{Rune: 'x', FG: fg, BG: bg, Width: 1})
			},
		},
		{
			name: "lone continuation inside",
			setUp: func(g *Grid) {
				g.Set(3, 0, Cell{Rune: 'N', FG: fg, BG: bg, Width: 1})
				g.Set(4, 0, Cell{Rune: ' ', FG: fg, BG: bg, Width: 0}) // no lead
			},
		},
	} {
		for _, write := range writePaths() {
			t.Run(orient.name+"/"+write.name, func(t *testing.T) {
				g := New(8, 1, fg, bg)
				orient.setUp(g)
				before := g.At(3, 0)

				write.fn(g.View().Sub(4, 0, 4, 1))

				if got := g.At(3, 0); !got.Equal(before) {
					t.Errorf("grid 3,0 = %+v, want %+v: the neighbour was blanked", got, before)
				}
			})
		}
	}
}

// TestViewRepairsAGenuineStraddlingPair checks the one documented
// exception still works at both edges: a real pair cut by the view edge
// loses its outside half, which would otherwise draw as a stray glyph.
func TestViewRepairsAGenuineStraddlingPair(t *testing.T) {
	// The pair always sits at columns 3 and 4.
	for _, edge := range []struct {
		name     string
		writeAt  func(g *Grid) View
		orphanX  int
		skipWide bool
	}{
		{
			// The view starts at 4 and holds the continuation, so the
			// lead at 3 is orphaned.
			name:    "lead outside the left edge",
			writeAt: func(g *Grid) View { return g.View().Sub(4, 0, 4, 1) },
			orphanX: 3,
		},
		{
			// The view ends at 3 and holds the lead, so the continuation
			// at 4 is orphaned. A wide cell is refused in a view's last
			// column, so that path writes nothing and repairs nothing.
			name:     "continuation outside the right edge",
			writeAt:  func(g *Grid) View { return g.View().Sub(0, 0, 4, 1).Sub(3, 0, 1, 1) },
			orphanX:  4,
			skipWide: true,
		},
	} {
		for _, write := range writePaths() {
			if edge.skipWide && write.name == "SetWide wide" {
				continue
			}
			t.Run(edge.name+"/"+write.name, func(t *testing.T) {
				g := New(8, 1, fg, bg)
				g.SetWide(3, 0, Cell{Rune: '世', FG: fg, BG: bg, Width: 2})

				write.fn(edge.writeAt(g))

				if got := g.At(edge.orphanX, 0); got.Rune != ' ' || got.Width != 1 {
					t.Errorf("grid %d,0 = %+v, want a blank: the orphan was left behind",
						edge.orphanX, got)
				}
			})
		}
	}
}

// writePaths returns every way a widget can write through a view, each
// writing at the view's own origin.
func writePaths() []struct {
	name string
	fn   func(v View)
} {
	return []struct {
		name string
		fn   func(v View)
	}{
		{"SetWide narrow", func(v View) {
			v.SetWide(0, 0, Cell{Rune: 'a', FG: fg, BG: bg, Width: 1})
		}},
		{"SetWide wide", func(v View) {
			v.SetWide(0, 0, Cell{Rune: '界', FG: fg, BG: bg, Width: 2})
		}},
		{"SetString", func(v View) { v.SetString(0, 0, "a", fg, bg, 0) }},
		{"Fill", func(v View) { v.Fill(Cell{Rune: '.', FG: fg, BG: bg, Width: 1}) }},
		{"Clear", func(v View) { v.Clear() }},
	}
}

// TestViewFillLeavesALoneLeadAlone checks that the edge repair proves a
// pair exists before blanking anything. A cell claiming to be half of a
// pair is not proof that the other half is there.
func TestViewFillLeavesALoneLeadAlone(t *testing.T) {
	g := New(8, 1, fg, bg)
	g.Set(3, 0, Cell{Rune: 'L', FG: fg, BG: bg, Width: 2}) // no continuation
	g.Set(4, 0, Cell{Rune: 'N', FG: fg, BG: bg, Width: 1}) // an ordinary neighbour

	g.View().Sub(0, 0, 4, 1).Fill(Cell{Rune: '.', FG: fg, BG: bg, Width: 1})

	if got := g.At(4, 0).Rune; got != 'N' {
		t.Errorf("grid 4,0 = %q, want 'N': it was never half of a pair", got)
	}
}

// TestViewSetStringNoFitRepairsAnOrphanedLead checks that blanking the
// column a wide cluster could not use also clears the lead of whatever
// was there, rather than leaving it to draw over the blank.
func TestViewSetStringNoFitRepairsAnOrphanedLead(t *testing.T) {
	g := New(3, 1, fg, bg)
	v := g.View()
	v.SetWide(1, 0, Cell{Rune: '世', FG: fg, BG: bg, Width: 2})

	v.SetString(2, 0, "界", fg, bg, 0)

	if got := g.At(1, 0); got.Rune != ' ' || got.Width != 1 {
		t.Errorf("grid 1,0 = %+v, want a blank: the lead was orphaned", got)
	}
	if got := g.At(2, 0); got.Rune != ' ' || got.Width != 1 {
		t.Errorf("grid 2,0 = %+v, want a blank", got)
	}
}

// TestViewSubClampsAHugeSize checks that asking for the rest of the row
// with a very large width is not wrapped into an empty view.
func TestViewSubClampsAHugeSize(t *testing.T) {
	g := New(10, 4, fg, bg)

	v := g.View().Sub(1, 1, math.MaxInt, math.MaxInt)

	if cols, rows := v.Size(); cols != 9 || rows != 3 {
		t.Errorf("size = %dx%d, want 9x3", cols, rows)
	}
}

// TestViewWritesDoNotDirtyAnIdleGrid checks that damage tracking still
// holds through a view: an unchanged repaint must cost nothing.
func TestViewWritesDoNotDirtyAnIdleGrid(t *testing.T) {
	t.Run("fill", func(t *testing.T) {
		g := New(10, 3, fg, bg)
		v := g.View().Sub(2, 1, 6, 2)
		v.Fill(Cell{Rune: '.', FG: fg, BG: bg, Width: 1})
		g.ClearDirty()

		v.Fill(Cell{Rune: '.', FG: fg, BG: bg, Width: 1})

		if g.AnyDirty() {
			t.Error("an identical fill dirtied the grid")
		}
	})

	t.Run("string with a wide cluster", func(t *testing.T) {
		g := New(10, 3, fg, bg)
		v := g.View().Sub(2, 1, 6, 2)
		v.SetString(0, 0, "ab世", fg, bg, 0)
		g.ClearDirty()

		v.SetString(0, 0, "ab世", fg, bg, 0)

		if g.AnyDirty() {
			t.Error("an identical string dirtied the grid")
		}
	})
}

// TestViewSetStringKeepsCombiningMarks checks that a base rune and its
// mark share one cell when written through a view.
func TestViewSetStringKeepsCombiningMarks(t *testing.T) {
	g := New(4, 1, fg, bg)

	g.View().SetString(0, 0, "éx", fg, bg, 0)

	c := g.At(0, 0)
	if c.Rune != 'e' || len(c.Comb) != 1 || c.Comb[0] != '́' {
		t.Errorf("cell 0,0 = %+v, want 'e' with one combining mark", c)
	}
	if got := g.At(1, 0).Rune; got != 'x' {
		t.Errorf("cell 1,0 = %q, want 'x'", got)
	}
}

// TestViewSurvivesAResize checks the documented contract: a view built
// before a resize keeps working, dropping writes that fall outside the
// grid rather than panicking.
func TestViewSurvivesAResize(t *testing.T) {
	for _, size := range [][2]int{{4, 2}, {20, 8}, {1, 1}, {0, 0}, {0, 4}, {4, 0}} {
		g := New(10, 4, fg, bg)
		v := g.View().Sub(6, 2, 4, 2)

		g.Resize(size[0], size[1])

		// None of these may panic, whatever the grid became.
		v.Set(0, 0, Cell{Rune: 'x', FG: fg, BG: bg, Width: 1})
		v.SetWide(0, 0, Cell{Rune: '世', FG: fg, BG: bg, Width: 2})
		v.SetString(0, 0, "abc", fg, bg, 0)
		v.Fill(Cell{Rune: '.', FG: fg, BG: bg, Width: 1})
		v.Clear()
		v.At(0, 0)
		v.Sub(0, 0, 2, 2).Set(0, 0, Cell{Rune: 'y', FG: fg, BG: bg, Width: 1})

		if cols, rows := v.Size(); cols != 4 || rows != 2 {
			t.Errorf("resize to %v: stale size = %dx%d, want the original 4x2",
				size, cols, rows)
		}
	}
}
