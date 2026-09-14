package render

import (
	"image/color"
	"testing"

	"github.com/marrasen/gridterm/grid"
)

var (
	fg = color.RGBA{0xff, 0xff, 0xff, 0xff}
	bg = color.RGBA{0x00, 0x00, 0x00, 0xff}
)

func TestLayerSizeMeasuresItsGrid(t *testing.T) {
	l := &Layer{Grid: grid.New(10, 4, fg, bg)}

	if w, h := l.Size(7, 15); w != 70 || h != 60 {
		t.Errorf("size = %dx%d, want 70x60", w, h)
	}
}

// TestLayerSizeIsNeverZero checks that an empty grid still yields a
// usable texture size: ebiten refuses a zero-sized image.
func TestLayerSizeIsNeverZero(t *testing.T) {
	for _, dims := range [][2]int{{0, 0}, {0, 4}, {10, 0}} {
		l := &Layer{Grid: grid.New(dims[0], dims[1], fg, bg)}

		w, h := l.Size(7, 15)
		if w < 1 || h < 1 {
			t.Errorf("grid %v: size = %dx%d, want at least 1x1", dims, w, h)
		}
	}
}

// samePlacementSlices compares two snapshots, which the tests do and the
// compositor does not: it compares a snapshot against the live layers.
func samePlacementSlices(a, b []placement) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestSamePlacementsSpotsEveryVisibleChange(t *testing.T) {
	a := &Layer{Grid: grid.New(4, 2, fg, bg)}
	b := &Layer{Grid: grid.New(4, 2, fg, bg)}
	base := []*Layer{a, b}
	was := appendPlacements(nil, base)

	if !samePlacementSlices(was, appendPlacements(nil, base)) {
		t.Fatal("an unchanged stack compared as changed")
	}

	for _, tc := range []struct {
		name   string
		change func()
		undo   func()
	}{
		{"moved", func() { a.X = 5 }, func() { a.X = 0 }},
		{"moved down", func() { a.Y = 5 }, func() { a.Y = 0 }},
		{"hidden", func() { b.Hidden = true }, func() { b.Hidden = false }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.change()
			defer tc.undo()

			if samePlacementSlices(was, appendPlacements(nil, base)) {
				t.Error("the change was not noticed, so the screen would keep the old frame")
			}
		})
	}

	t.Run("reordered", func(t *testing.T) {
		if samePlacementSlices(was, appendPlacements(nil, []*Layer{b, a})) {
			t.Error("swapping two layers was not noticed")
		}
	})

	t.Run("added", func(t *testing.T) {
		c := &Layer{Grid: grid.New(4, 2, fg, bg)}
		if samePlacementSlices(was, appendPlacements(nil, []*Layer{a, b, c})) {
			t.Error("a new layer was not noticed")
		}
	})

	t.Run("removed", func(t *testing.T) {
		if samePlacementSlices(was, appendPlacements(nil, []*Layer{a})) {
			t.Error("a removed layer was not noticed")
		}
	})
}

// TestSamePlacementsIgnoresGridContent checks that placement tracking
// answers only "would this draw somewhere else", leaving "did the
// content change" to the grid's own damage tracking.
func TestSamePlacementsIgnoresGridContent(t *testing.T) {
	a := &Layer{Grid: grid.New(4, 2, fg, bg)}
	was := appendPlacements(nil, []*Layer{a})

	a.Grid.SetString(0, 0, "hello", fg, bg, 0)

	if !samePlacementSlices(was, appendPlacements(nil, []*Layer{a})) {
		t.Error("a content change counted as a placement change")
	}
}

func TestCompositorAddAndRemove(t *testing.T) {
	c := NewCompositor(nil)
	a := &Layer{Grid: grid.New(4, 2, fg, bg)}
	b := &Layer{Grid: grid.New(4, 2, fg, bg)}

	c.Add(a)
	c.Add(b)
	if got := c.Layers(); len(got) != 2 || got[0] != a || got[1] != b {
		t.Fatalf("stack = %v, want [a b] with a at the bottom", got)
	}

	c.Remove(a)
	if got := c.Layers(); len(got) != 1 || got[0] != b {
		t.Errorf("stack after removing a = %v, want [b]", got)
	}

	// Removing something that is not in the stack must not disturb it.
	c.Remove(&Layer{})
	if got := c.Layers(); len(got) != 1 || got[0] != b {
		t.Errorf("stack = %v, want [b] left alone", got)
	}
}

func TestCompositorAnyVisibleStale(t *testing.T) {
	c := NewCompositor(nil)
	a := &Layer{Grid: grid.New(4, 2, fg, bg)}
	c.Add(a)

	if !c.anyVisibleStale() {
		t.Fatal("a layer that has never been painted reported up to date")
	}
	a.painted = true
	if c.anyVisibleStale() {
		t.Fatal("a painted layer reported stale")
	}

	a.painted = false
	if !c.anyVisibleStale() {
		t.Error("a stale layer did not report stale")
	}

	// A hidden layer cannot show its change, so it must not cost a
	// frame. It stays stale for when it comes back.
	a.Hidden = true
	if c.anyVisibleStale() {
		t.Error("a hidden layer forced a repaint")
	}
	if a.painted {
		t.Error("hiding a layer marked it painted, so it will show stale content")
	}
}

// TestCompositorAnyVisibleStaleIgnoresALayerWithNoGrid checks the nil
// grid a layer has before anything is attached to it.
func TestCompositorAnyVisibleStaleIgnoresALayerWithNoGrid(t *testing.T) {
	c := NewCompositor(nil)
	c.Add(&Layer{})

	if c.anyVisibleStale() {
		t.Error("a layer with no grid reported stale")
	}
}

// TestCompositorAddIgnoresADuplicate checks that adding the same layer
// twice does not blit it twice or let one Remove free a live texture.
func TestCompositorAddIgnoresADuplicate(t *testing.T) {
	c := NewCompositor(nil)
	l := &Layer{Grid: grid.New(4, 2, fg, bg)}

	c.Add(l)
	c.Add(l)

	if got := c.Layers(); len(got) != 1 {
		t.Errorf("stack = %v, want one entry", got)
	}
}

// TestCompositorRemoveDoesNotCorruptAHeldSlice checks that the shift in
// Remove does not leave a duplicate in a slice a caller already took.
func TestCompositorRemoveDoesNotCorruptAHeldSlice(t *testing.T) {
	c := NewCompositor(nil)
	a := &Layer{Grid: grid.New(4, 2, fg, bg)}
	b := &Layer{Grid: grid.New(4, 2, fg, bg)}
	c.Add(a)
	c.Add(b)
	held := c.Layers()

	c.Remove(a)

	if held[0] == held[1] {
		t.Errorf("held slice = %v, want no duplicate left by the shift", held)
	}
}

func TestStatsAdd(t *testing.T) {
	got := Stats{RowsDrawn: 1, Quads: 2, DrawCalls: 3, CellsTotal: 4}
	got.add(Stats{RowsDrawn: 10, Quads: 20, DrawCalls: 30, CellsTotal: 40})

	want := Stats{RowsDrawn: 11, Quads: 22, DrawCalls: 33, CellsTotal: 44}
	if got != want {
		t.Errorf("summed stats = %+v, want %+v", got, want)
	}
}
