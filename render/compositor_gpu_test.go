package render

import (
	"image/color"
	"testing"

	"github.com/hajimehoshi/ebiten/v2"
	"golang.org/x/image/font/gofont/gomono"

	"github.com/marrasen/gridterm/glyph"
	"github.com/marrasen/gridterm/grid"
)

// These tests run outside ebiten's game loop, so every draw only queues
// a command that is never flushed, and ReadPixels panics. They check the
// code path and the frame accounting. Nothing here proves what a pixel
// ends up being; that still needs a real window.

// newTestRenderer builds a real atlas and renderer.
func newTestRenderer(t *testing.T) *Renderer {
	t.Helper()
	a, err := glyph.NewAtlas(glyph.Fonts{Regular: gomono.TTF}, 12, 96)
	if err != nil {
		t.Fatalf("no atlas from the embedded font, so this is broken code and not this machine: %v", err)
	}
	return New(a)
}

func TestCompositorDrawsAndSkips(t *testing.T) {
	r := newTestRenderer(t)
	c := NewCompositor(r)
	screen := ebiten.NewImage(320, 240)

	base := &Layer{Grid: grid.New(20, 8, fg, bg)}
	base.Grid.SetString(0, 0, "hello", fg, bg, 0)
	c.Add(base)

	c.Draw(screen)
	got := c.Stats()
	if got.Skipped {
		t.Fatal("the first frame was skipped")
	}
	if got.Repainted != 1 || got.Blits != 1 || got.Layers != 1 {
		t.Errorf("first frame = %+v, want one layer repainted and blitted", got)
	}

	// Nothing changed, so the frame costs nothing.
	c.Draw(screen)
	if got := c.Stats(); !got.Skipped || got.Repainted != 0 || got.Blits != 0 {
		t.Errorf("idle frame = %+v, want skipped with no work", got)
	}

	// A content change repaints and blits again.
	base.Grid.SetString(0, 1, "more", fg, bg, 0)
	c.Draw(screen)
	if got := c.Stats(); got.Skipped || got.Repainted != 1 {
		t.Errorf("changed frame = %+v, want a repaint", got)
	}
}

// TestCompositorBlitsEveryLayerWhenOneChanges checks that a change low
// in the stack is not left hidden behind the layers above it.
func TestCompositorBlitsEveryLayerWhenOneChanges(t *testing.T) {
	r := newTestRenderer(t)
	c := NewCompositor(r)
	screen := ebiten.NewImage(320, 240)

	base := &Layer{Grid: grid.New(20, 8, fg, bg)}
	over := &Layer{Grid: grid.New(6, 2, fg, bg), X: 16, Y: 16, Transparent: true}
	c.Add(base)
	c.Add(over)
	c.Draw(screen)

	base.Grid.SetString(0, 0, "changed", fg, bg, 0)
	c.Draw(screen)

	got := c.Stats()
	if got.Repainted != 1 {
		t.Errorf("repainted %d layers, want 1: only the bottom one changed", got.Repainted)
	}
	if got.Blits != 2 {
		t.Errorf("blitted %d layers, want 2: the overlay must be drawn back on top", got.Blits)
	}
}

// TestCompositorMovingALayerRedrawsTheScreen checks the case no grid
// reports: the content is identical but it belongs somewhere else.
func TestCompositorMovingALayerRedrawsTheScreen(t *testing.T) {
	r := newTestRenderer(t)
	c := NewCompositor(r)
	screen := ebiten.NewImage(320, 240)
	over := &Layer{Grid: grid.New(6, 2, fg, bg), Transparent: true}
	c.Add(&Layer{Grid: grid.New(20, 8, fg, bg)})
	c.Add(over)
	c.Draw(screen)
	c.Draw(screen)

	over.X, over.Y = 40, 40
	c.Draw(screen)

	if got := c.Stats(); got.Skipped || got.Blits != 2 {
		t.Errorf("after moving a layer = %+v, want a redraw of both layers", got)
	}
}

// A layer drawn at a new scale covers different pixels with no cell of
// its grid touched, so the screen is put back first.
//
// The texture is not repainted: it keeps the grid's own size whatever
// the scale, so only the blit changes.
func TestCompositorScalingALayerRedrawsTheScreen(t *testing.T) {
	r := newTestRenderer(t)
	c := NewCompositor(r)
	screen := ebiten.NewImage(320, 240)
	l := &Layer{Grid: grid.New(20, 8, fg, bg)}
	c.Add(l)
	c.Draw(screen)
	c.Draw(screen)
	if !c.Stats().Skipped {
		t.Fatal("the second frame was drawn, so the layer was not idle to begin with")
	}

	l.Scale = 0.5
	c.Draw(screen)

	got := c.Stats()
	if got.Skipped || got.Blits != 1 {
		t.Errorf("after the scale changed = %+v, want the layer blitted again", got)
	}
	if !got.Cleared {
		t.Error("the screen was not wiped, so the pixels the layer vacated are still there")
	}
	if got.Repainted != 0 {
		t.Errorf("repainted %d layers, want none: the texture is the same size and the same cells", got.Repainted)
	}
}

// Padding that moved is drawn, and the screen is put back first.
//
// Padding shifts pixels without changing a single cell, so no damage
// flag reports it. A compositor that went on its flags alone would skip
// the frame and leave the grid drawn where it used to be, with the
// pixels it vacated still on screen.
func TestCompositorDrawsPaddingThatMoved(t *testing.T) {
	r := newTestRenderer(t)
	c := NewCompositor(r)
	screen := ebiten.NewImage(320, 240)
	l := &Layer{Grid: grid.New(20, 8, fg, bg)}
	c.Add(l)
	c.Draw(screen)
	c.Draw(screen)
	if !c.Stats().Skipped {
		t.Fatal("the second frame was drawn, so the layer was not idle to begin with")
	}

	l.Grid.SetColPad(0, grid.Pad{Before: 2})
	c.Draw(screen)

	got := c.Stats()
	if got.Skipped || got.Repainted != 1 {
		t.Errorf("after the padding moved = %+v, want the layer repainted", got)
	}
	if !got.Cleared {
		t.Error("the screen was not wiped, so the pixels the padding vacated are still there")
	}
}

// TestCompositorHiddenLayerIsNotDrawn checks that hiding a layer skips
// its blit but keeps it in the stack.
func TestCompositorHiddenLayerIsNotDrawn(t *testing.T) {
	r := newTestRenderer(t)
	c := NewCompositor(r)
	screen := ebiten.NewImage(320, 240)
	over := &Layer{Grid: grid.New(6, 2, fg, bg), Transparent: true}
	c.Add(&Layer{Grid: grid.New(20, 8, fg, bg)})
	c.Add(over)
	c.Draw(screen)

	over.Hidden = true
	c.Draw(screen)

	if got := c.Stats(); got.Blits != 1 || got.Layers != 2 {
		t.Errorf("with one layer hidden = %+v, want 1 blit of 2 layers", got)
	}
}

// TestCompositorResizingAGridRepaintsItWhole checks that a grid growing
// past its texture gets a new one and is drawn again in full, rather
// than keeping a stale texture of the old size.
func TestCompositorResizingAGridRepaintsItWhole(t *testing.T) {
	r := newTestRenderer(t)
	c := NewCompositor(r)
	screen := ebiten.NewImage(320, 240)
	l := &Layer{Grid: grid.New(10, 4, fg, bg)}
	c.Add(l)
	c.Draw(screen)
	c.Draw(screen)

	l.Grid.Resize(20, 8)
	c.Draw(screen)

	geo := &Geometry{}
	r.Measure(l.Grid, geo)
	wantW, wantH := l.Size(geo)
	if b := l.tex.Bounds(); b.Dx() != wantW || b.Dy() != wantH {
		t.Errorf("texture = %dx%d, want %dx%d", b.Dx(), b.Dy(), wantW, wantH)
	}
	got := c.Stats()
	if got.Skipped || got.Repainted != 1 {
		t.Fatalf("after a resize = %+v, want a full repaint", got)
	}
	if got.RowsDrawn != 8 {
		t.Errorf("drew %d rows, want all 8: the new texture starts blank", got.RowsDrawn)
	}
	if !got.Cleared {
		t.Error("the screen was not wiped, so the old texture's pixels are still on it")
	}
}

// TestCompositorTwoLayersOnOneGrid checks that both layers are painted.
// Row damage lives on the grid and the first repaint clears it, so a
// layer cannot infer from the grid whether its own texture is current.
func TestCompositorTwoLayersOnOneGrid(t *testing.T) {
	r := newTestRenderer(t)
	c := NewCompositor(r)
	screen := ebiten.NewImage(320, 240)
	g := grid.New(10, 4, fg, bg)
	a := &Layer{Grid: g}
	b := &Layer{Grid: g, X: 100}
	c.Add(a)
	c.Add(b)

	c.Draw(screen)
	if got := c.Stats(); got.Repainted != 2 {
		t.Fatalf("first frame repainted %d layers, want 2", got.Repainted)
	}

	g.SetString(0, 0, "hello", fg, bg, 0)
	c.Draw(screen)

	got := c.Stats()
	if got.Repainted != 2 {
		t.Fatalf("repainted %d layers, want 2: one of them still shows the old text",
			got.Repainted)
	}
	if !a.painted || !b.painted {
		t.Errorf("painted flags = %v, %v, want both true", a.painted, b.painted)
	}
	// One changed row each. Damage is cleared after every layer has had
	// it, so the second layer does not have to repaint the whole grid.
	if got.RowsDrawn != 2 {
		t.Errorf("drew %d rows, want 2: damage tracking is off for the second layer",
			got.RowsDrawn)
	}
}

// TestCompositorUnhidingRepaints checks the sequence that a shared
// damage flag gets wrong: hide a layer, change its grid behind it, then
// bring it back.
func TestCompositorUnhidingRepaints(t *testing.T) {
	r := newTestRenderer(t)
	c := NewCompositor(r)
	screen := ebiten.NewImage(320, 240)
	g := grid.New(10, 4, fg, bg)
	visible := &Layer{Grid: g}
	hidden := &Layer{Grid: g, X: 100, Hidden: true}
	c.Add(visible)
	c.Add(hidden)
	c.Draw(screen)

	g.SetString(0, 0, "hello", fg, bg, 0)
	c.Draw(screen) // the visible layer takes the grid's row damage
	hidden.Hidden = false
	c.Draw(screen)

	got := c.Stats()
	if got.Repainted != 1 {
		t.Fatalf("repainted %d layers, want 1: the revealed layer never saw the change",
			got.Repainted)
	}
	if !hidden.painted {
		t.Error("the revealed layer is still stale")
	}
	// The damage was cleared while it was hidden, so nothing says which
	// rows moved and the whole grid has to be drawn.
	if got.RowsDrawn != 4 {
		t.Errorf("drew %d rows, want all 4: a revealed layer knows only that it is behind",
			got.RowsDrawn)
	}
}

// TestCompositorRepaintsWhenTheAtlasIsRebuiltInPlace checks a font size
// change that lands on the same cell box. The glyphs all move, but no
// size anywhere changes to say so.
func TestCompositorRepaintsWhenTheAtlasIsRebuiltInPlace(t *testing.T) {
	a, err := glyph.NewAtlas(glyph.Fonts{Regular: gomono.TTF}, 12, 96)
	if err != nil {
		t.Fatalf("no atlas from the embedded font, so this is broken code and not this machine: %v", err)
	}
	r := New(a)
	before := a.Metrics()
	if err := a.SetSize(12.5); err != nil {
		t.Fatalf("SetSize: %v", err)
	}
	if a.Metrics() != before {
		t.Fatalf("12pt and 12.5pt no longer share a cell box (%+v vs %+v), so the "+
			"rebuild this test is about cannot happen; pick two sizes that do", before, a.Metrics())
	}

	c := NewCompositor(r)
	screen := ebiten.NewImage(320, 240)
	c.Add(&Layer{Grid: grid.New(10, 4, fg, bg)})
	c.Draw(screen)
	c.Draw(screen)
	if got := c.Stats(); !got.Skipped {
		t.Fatalf("frame before the rebuild = %+v, want skipped", got)
	}

	if err := a.SetSize(13); err != nil {
		t.Fatalf("SetSize: %v", err)
	}
	// Back to the colliding size, so the cell box is unchanged again.
	if err := a.SetSize(12); err != nil {
		t.Fatalf("SetSize: %v", err)
	}
	c.Draw(screen)

	if got := c.Stats(); got.Skipped || got.Repainted != 1 {
		t.Errorf("after the atlas was rebuilt = %+v, want a repaint", got)
	}
}

// TestCompositorClearingTheGridDropsTheLayer checks that a layer with
// nothing to show stops being drawn, rather than leaving its last
// picture on screen.
func TestCompositorClearingTheGridDropsTheLayer(t *testing.T) {
	r := newTestRenderer(t)
	c := NewCompositor(r)
	screen := ebiten.NewImage(320, 240)
	base := &Layer{Grid: grid.New(20, 8, fg, bg)}
	dialog := &Layer{Grid: grid.New(8, 3, fg, bg), X: 20, Y: 20}
	c.Add(base)
	c.Add(dialog)
	c.Draw(screen)
	c.Draw(screen)

	dialog.Grid = nil
	c.Draw(screen)

	got := c.Stats()
	if got.Skipped {
		t.Fatal("dropping the grid was not noticed, so the layer is still on screen")
	}
	if got.Blits != 1 {
		t.Errorf("blitted %d layers, want 1", got.Blits)
	}
	if !got.Cleared {
		t.Error("the screen was not wiped, so the dropped layer's pixels remain")
	}
	if dialog.tex != nil {
		t.Error("the dropped layer kept its texture")
	}
}

// TestCompositorTransparentToggleRepaints checks the exported field
// that changes what the texture looks like without touching any grid.
func TestCompositorTransparentToggleRepaints(t *testing.T) {
	r := newTestRenderer(t)
	c := NewCompositor(r)
	screen := ebiten.NewImage(320, 240)
	l := &Layer{Grid: grid.New(8, 3, fg, bg)}
	c.Add(l)
	c.Draw(screen)
	c.Draw(screen)

	l.Transparent = true
	c.Draw(screen)

	if got := c.Stats(); got.Skipped || got.Repainted != 1 {
		t.Errorf("after turning on transparency = %+v, want a repaint", got)
	}
}

// TestLayerClearsASeeThroughRowOnAnOpaqueLayer checks that a row whose
// cells have no background is wiped anyway. Without it, erased text
// stays on screen: a blank cell draws no glyph and a see-through cell
// draws no background, so nothing covers what was there.
func TestLayerClearsASeeThroughRowOnAnOpaqueLayer(t *testing.T) {
	clear := color.RGBA{}
	l := &Layer{Grid: grid.New(6, 2, fg, clear)}

	if !l.clears(0) {
		t.Error("a see-through row on an opaque layer was not cleared")
	}

	opaque := &Layer{Grid: grid.New(6, 2, fg, bg)}
	if opaque.clears(0) {
		t.Error("an opaque row was cleared, which costs a fill for nothing")
	}

	opaque.Transparent = true
	if !opaque.clears(0) {
		t.Error("Transparent did not force the clear")
	}
}

// TestCompositorReplacingAGridRepaints checks the change no damage flag
// reports: the same layer pointed at a different, already clean grid.
func TestCompositorReplacingAGridRepaints(t *testing.T) {
	r := newTestRenderer(t)
	c := NewCompositor(r)
	screen := ebiten.NewImage(320, 240)
	first := grid.New(10, 4, fg, bg)
	l := &Layer{Grid: first}
	c.Add(l)
	c.Draw(screen)

	second := grid.New(10, 4, fg, bg)
	second.SetString(0, 0, "BBBB", fg, bg, 0)
	second.ClearDirty()
	l.Grid = second
	c.Draw(screen)

	if got := c.Stats(); got.Skipped || got.Repainted != 1 {
		t.Errorf("after swapping the grid = %+v, want a repaint", got)
	}
}

// TestCompositorNewScreenForcesABlit checks the frame after a window
// resize that does not change the cell count. ebiten hands back a fresh,
// blank offscreen image, so skipping the frame leaves a black window.
func TestCompositorNewScreenForcesABlit(t *testing.T) {
	r := newTestRenderer(t)
	c := NewCompositor(r)
	l := &Layer{Grid: grid.New(10, 4, fg, bg)}
	c.Add(l)
	c.Draw(ebiten.NewImage(320, 240))
	c.Draw(ebiten.NewImage(320, 240))
	if got := c.Stats(); got.Skipped {
		t.Fatal("a new screen image of the same size was skipped")
	}

	// Same image again, nothing else changed: now it may skip.
	same := ebiten.NewImage(320, 240)
	c.Draw(same)
	c.Draw(same)
	if got := c.Stats(); !got.Skipped {
		t.Errorf("idle frame = %+v, want skipped", got)
	}

	c.Draw(ebiten.NewImage(324, 240))

	got := c.Stats()
	if got.Skipped || got.Blits != 1 {
		t.Errorf("after a window resize = %+v, want the stack blitted back", got)
	}
	// The textures are still good; only the screen was thrown away.
	if got.Repainted != 0 {
		t.Errorf("repainted %d layers, want 0: a resize costs a blit, not a repaint",
			got.Repainted)
	}
}

// TestCompositorStatsSumAcrossLayers checks that two repainted layers
// are added together rather than the last one overwriting the first.
func TestCompositorStatsSumAcrossLayers(t *testing.T) {
	r := newTestRenderer(t)
	c := NewCompositor(r)
	screen := ebiten.NewImage(320, 240)
	c.Add(&Layer{Grid: grid.New(10, 4, fg, bg)})
	c.Add(&Layer{Grid: grid.New(10, 6, fg, bg), X: 100})

	c.Draw(screen)

	got := c.Stats()
	if got.Repainted != 2 {
		t.Fatalf("repainted %d layers, want 2", got.Repainted)
	}
	if got.RowsDrawn != 10 {
		t.Errorf("RowsDrawn = %d, want 10: 4 rows plus 6", got.RowsDrawn)
	}
	if got.CellsTotal != 100 {
		t.Errorf("CellsTotal = %d, want 100: 40 cells plus 60", got.CellsTotal)
	}
}

// TestCompositorLayerAddedLater checks a dialog appearing mid-session,
// and being dismissed again.
func TestCompositorLayerAddedLater(t *testing.T) {
	r := newTestRenderer(t)
	c := NewCompositor(r)
	screen := ebiten.NewImage(320, 240)
	base := &Layer{Grid: grid.New(20, 8, fg, bg)}
	c.Add(base)
	c.Draw(screen)
	c.Draw(screen)

	dialog := &Layer{Grid: grid.New(8, 3, fg, bg), X: 20, Y: 20, Transparent: true}
	c.Add(dialog)
	c.Draw(screen)
	if got := c.Stats(); got.Skipped || got.Repainted != 1 || got.Blits != 2 {
		t.Errorf("after adding a dialog = %+v, want it painted and both blitted", got)
	}

	c.Remove(dialog)
	c.Draw(screen)

	got := c.Stats()
	if got.Skipped || got.Blits != 1 {
		t.Errorf("after dismissing it = %+v, want one blit", got)
	}
	if !got.Cleared {
		t.Error("the screen was not wiped, so the dialog's pixels are still on it")
	}
	if dialog.tex != nil {
		t.Error("the dismissed dialog kept its texture")
	}
}

// TestCompositorRemoveThenAddRepaints checks a layer coming back after
// its texture was freed.
func TestCompositorRemoveThenAddRepaints(t *testing.T) {
	r := newTestRenderer(t)
	c := NewCompositor(r)
	screen := ebiten.NewImage(320, 240)
	l := &Layer{Grid: grid.New(8, 3, fg, bg)}
	c.Add(l)
	c.Draw(screen)
	c.Remove(l)
	c.Draw(screen)

	c.Add(l)
	c.Draw(screen)

	if got := c.Stats(); got.Repainted != 1 || got.Blits != 1 {
		t.Errorf("after re-adding = %+v, want it repainted and blitted", got)
	}
	if l.tex == nil {
		t.Error("no texture was allocated for the returning layer")
	}
}

// TestCompositorEmptyGridDoesNotFault checks the degenerate sizes a
// layout can produce while it settles.
func TestCompositorEmptyGridDoesNotFault(t *testing.T) {
	r := newTestRenderer(t)
	c := NewCompositor(r)
	screen := ebiten.NewImage(320, 240)
	c.Add(&Layer{Grid: grid.New(0, 0, fg, bg)})
	c.Add(&Layer{Grid: grid.New(0, 5, fg, bg), Transparent: true})
	c.Add(&Layer{Grid: grid.New(5, 0, fg, bg)})

	c.Draw(screen)
	c.Draw(screen)
}

// TestCompositorRemoveFreesTheTexture checks that a dismissed dialog
// does not leave its texture behind.
func TestCompositorRemoveFreesTheTexture(t *testing.T) {
	r := newTestRenderer(t)
	c := NewCompositor(r)
	screen := ebiten.NewImage(320, 240)
	l := &Layer{Grid: grid.New(6, 2, fg, bg)}
	c.Add(l)
	c.Draw(screen)
	if l.tex == nil {
		t.Fatal("no texture was allocated")
	}

	c.Remove(l)

	if l.tex != nil {
		t.Error("the texture outlived the layer")
	}
}

// TestCompositorAddedLayerRepaintsInFullWhileTheGridIsBusy checks a
// layer joining on a frame that also has new content. The grid's row
// damage describes the other layers' frame; this one is behind on
// everything and must not mistake one for the other.
func TestCompositorAddedLayerRepaintsInFullWhileTheGridIsBusy(t *testing.T) {
	r := newTestRenderer(t)
	c := NewCompositor(r)
	screen := ebiten.NewImage(320, 240)
	g := grid.New(10, 4, fg, bg)
	for y := 0; y < 4; y++ {
		g.SetString(0, y, "filled", fg, bg, 0)
	}
	c.Add(&Layer{Grid: g})
	c.Draw(screen)
	c.Draw(screen)

	// One row changes, and a second layer joins, in the same frame.
	g.SetString(0, 0, "new", fg, bg, 0)
	joined := &Layer{Grid: g, X: 150}
	c.Add(joined)
	c.Draw(screen)

	got := c.Stats()
	if got.Repainted != 2 {
		t.Fatalf("repainted %d layers, want 2", got.Repainted)
	}
	// One changed row for the layer that was already up to date, and
	// all four for the one that has never been painted.
	if got.RowsDrawn != 5 {
		t.Errorf("drew %d rows, want 5: the new layer's other rows are blank for good",
			got.RowsDrawn)
	}
}

// TestCompositorUnhidingRepaintsInFullWhileTheGridIsBusy checks the
// same trap on the way back from hidden. The row damage on this frame
// says nothing about what changed while the layer was away.
func TestCompositorUnhidingRepaintsInFullWhileTheGridIsBusy(t *testing.T) {
	r := newTestRenderer(t)
	c := NewCompositor(r)
	screen := ebiten.NewImage(320, 240)
	g := grid.New(10, 4, fg, bg)
	visible := &Layer{Grid: g}
	hidden := &Layer{Grid: g, X: 150}
	c.Add(visible)
	c.Add(hidden)
	c.Draw(screen)

	hidden.Hidden = true
	// A row changes while it is away, and the damage is cleared.
	g.SetString(0, 1, "while away", fg, bg, 0)
	c.Draw(screen)

	// Another row changes on the very frame it comes back.
	g.SetString(0, 3, "on return", fg, bg, 0)
	hidden.Hidden = false
	c.Draw(screen)

	got := c.Stats()
	if got.Repainted != 2 {
		t.Fatalf("repainted %d layers, want 2", got.Repainted)
	}
	// One changed row for the layer that stayed, and all four for the
	// one that missed a change it can no longer identify.
	if got.RowsDrawn != 5 {
		t.Errorf("drew %d rows, want 5: the revealed layer still misses the earlier row",
			got.RowsDrawn)
	}
}

// TestLayerClearsAPartlyTransparentRow checks a background that is not
// fully opaque. It draws a quad, but the quad blends rather than covers,
// so the old pixels show through what should have replaced them.
func TestLayerClearsAPartlyTransparentRow(t *testing.T) {
	for _, alpha := range []uint8{0x00, 0x01, 0x80, 0xfe} {
		l := &Layer{Grid: grid.New(6, 2, fg, color.RGBA{0x20, 0x20, 0x20, alpha})}
		if !l.clears(0) {
			t.Errorf("background alpha %#02x was not cleared, so erased text survives", alpha)
		}
	}
	opaque := &Layer{Grid: grid.New(6, 2, fg, color.RGBA{0x20, 0x20, 0x20, 0xff})}
	if opaque.clears(0) {
		t.Error("a fully opaque row was cleared, which costs a fill for nothing")
	}
}

func BenchmarkCompositorIdleFrame(b *testing.B) {
	a, err := glyph.NewAtlas(glyph.Fonts{Regular: gomono.TTF}, 12, 96)
	if err != nil {
		b.Skipf("no atlas: %v", err)
	}
	c := NewCompositor(New(a))
	screen := ebiten.NewImage(320, 240)
	for i := 0; i < 3; i++ {
		c.Add(&Layer{Grid: grid.New(10, 4, fg, bg), X: i * 50})
	}
	c.Draw(screen)
	c.Draw(screen)
	if !c.Stats().Skipped {
		b.Fatal("the frame before the benchmark was not idle")
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.Draw(screen)
	}
}

// TestCompositorFullRepaintCostDependsOnStackOrder pins the cost of a
// layer repainting in full over a grid a sibling shares. Marking the
// grid all-dirty makes a sibling drawn afterwards redo rows it had
// already done, so the order decides the cost. Both orders draw the
// same pixels.
func TestCompositorFullRepaintCostDependsOnStackOrder(t *testing.T) {
	const rows = 8
	for _, tc := range []struct {
		name      string
		joinFirst bool
		wantRows  int
	}{
		// The joining layer repaints in full; the settled one needs the
		// single row that changed.
		{name: "joining layer on top", joinFirst: false, wantRows: 1 + rows},
		// Reversed, the joining layer's MarkAllDirty lands before the
		// settled layer is drawn, so that one repaints in full too.
		{name: "joining layer underneath", joinFirst: true, wantRows: rows + rows},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newTestRenderer(t)
			c := NewCompositor(r)
			screen := ebiten.NewImage(320, 240)
			g := grid.New(10, rows, fg, bg)
			settled := &Layer{Grid: g}
			c.Add(settled)
			c.Draw(screen)
			c.Draw(screen)

			g.SetString(0, 0, "new", fg, bg, 0)
			joining := &Layer{Grid: g, X: 150}
			c.Add(joining)
			if tc.joinFirst {
				stack := c.Layers()
				stack[0], stack[1] = stack[1], stack[0]
			}
			c.Draw(screen)

			got := c.Stats()
			if got.Repainted != 2 {
				t.Fatalf("repainted %d layers, want 2", got.Repainted)
			}
			if got.RowsDrawn != tc.wantRows {
				t.Errorf("drew %d rows, want %d", got.RowsDrawn, tc.wantRows)
			}
		})
	}
}

// A layer that brings its own geometry is drawn by it, not by whatever
// measuring its grid would give.
//
// A region is a hole in the window and its columns are the window's
// own, down to the odd pixels the window had over. Measured afresh it
// would put those somewhere else.
func TestCompositorUsesTheGeometryALayerBrings(t *testing.T) {
	r := newTestRenderer(t)
	c := NewCompositor(r)
	screen := ebiten.NewImage(320, 240)
	g := grid.New(10, 4, fg, bg)

	var geo Geometry
	r.Measure(g, &geo)
	// Wider than the grid measures on its own, the way a hole in a
	// padded window is.
	geo.FitRows(geo.Height() + 7)
	l := &Layer{Grid: g, Geom: &geo}
	c.Add(l)
	c.Draw(screen)

	if b := l.tex.Bounds(); b.Dy() != geo.Height() {
		t.Errorf("the texture is %d tall, want the %d its geometry asks for",
			b.Dy(), geo.Height())
	}
}

// A carried geometry moving is drawn, and the screen is put back first.
//
// Nothing else would notice. The compositor does not measure a layer
// that brought its own, and a geometry can change with not one cell of
// the grid touched.
func TestCompositorDrawsACarriedGeometryThatMoved(t *testing.T) {
	r := newTestRenderer(t)
	c := NewCompositor(r)
	screen := ebiten.NewImage(320, 240)
	g := grid.New(10, 4, fg, bg)

	// Two sources the same width overall, with the padding at opposite
	// ends: taking columns from one and then the other moves every cell
	// without changing the size of anything.
	head := grid.New(10, 4, fg, bg)
	head.SetColPad(0, grid.Pad{Before: 2})
	tail := grid.New(10, 4, fg, bg)
	tail.SetColPad(9, grid.Pad{After: 2})
	var from, to Geometry
	r.Measure(head, &from)
	r.Measure(tail, &to)

	var geo Geometry
	r.Measure(g, &geo)
	geo.TakeCols(&from, 0, 10)
	l := &Layer{Grid: g, Geom: &geo}
	c.Add(l)
	c.Draw(screen)
	c.Draw(screen)
	if !c.Stats().Skipped {
		t.Fatal("the second frame was drawn, so the layer was not idle to begin with")
	}
	was := geo.Width()

	geo.TakeCols(&to, 0, 10)
	if geo.Width() != was {
		t.Fatalf("the grid is %d wide and was %d: the size gives the move away",
			geo.Width(), was)
	}
	c.Draw(screen)

	got := c.Stats()
	if got.Skipped || got.Repainted != 1 {
		t.Errorf("after the geometry moved = %+v, want the layer repainted", got)
	}
	if !got.Cleared {
		t.Error("the screen was not wiped, so the pixels the grid moved off are still there")
	}
}
