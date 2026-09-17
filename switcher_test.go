package main

import (
	"strings"
	"testing"

	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/input"
	"github.com/marrasen/gridterm/render"
	"github.com/marrasen/gridterm/ui"
	"github.com/marrasen/gridterm/ui/files"
	"github.com/marrasen/gridterm/ui/term"
)

// aWindowOfPanes is a window with n panes open in it, wide enough to
// draw them all at once.
func aWindowOfPanes(t *testing.T, n int) *testApp {
	t.Helper()
	a := newTestApp(t, 140, 44)
	withDialogs(t, a)
	withPanel(t, a)
	a.comp = render.NewCompositor(a.renderer)
	a.layer = &render.Layer{Grid: a.g}
	a.comp.Add(a.layer)
	for len(a.panes) < n {
		if err := a.openPane(); err != nil {
			t.Fatalf("open a pane: %v", err)
		}
	}
	if len(a.panes) != n {
		t.Fatalf("the window holds %d panes, want %d", len(a.panes), n)
	}
	return a
}

// openTiles opens the switcher and returns it.
func openTiles(t *testing.T, a *testApp) *ui.Tiles {
	t.Helper()
	if err := a.openSwitcher(); err != nil {
		t.Fatalf("open the switcher: %v", err)
	}
	tiles, ok := a.root.Modal().(*ui.Tiles)
	if !ok {
		t.Fatalf("it showed %T, want the tiles", a.root.Modal())
	}
	return tiles
}

// The switcher shows one tile per pane, named the way the sidebar names
// them.
func TestTheSwitcherShowsOneTilePerPane(t *testing.T) {
	a := aWindowOfPanes(t, 4)

	tiles := openTiles(t, a)

	if got := tiles.Len(); got != 4 {
		t.Errorf("it shows %d tiles, want one per pane", got)
	}
	if got := len(tiles.Areas()); got != 4 {
		t.Errorf("it laid out %d tiles", got)
	}
}

// It opens with the pane the user is on marked, so Escape and Enter both
// leave them where they were.
func TestTheSwitcherOpensOnThePaneInFront(t *testing.T) {
	a := aWindowOfPanes(t, 4)
	panes := a.panesInSidebarOrder()
	want := 2
	a.focus(panes[want])

	tiles := openTiles(t, a)

	if got := tiles.At(); got != want {
		t.Errorf("it marked tile %d, want the %d the user is on", got, want)
	}
}

// Picking a tile goes to that pane and closes the switcher.
func TestPickingATileGoesToThatPane(t *testing.T) {
	a := aWindowOfPanes(t, 4)
	panes := a.panesInSidebarOrder()
	a.focus(panes[0])
	openTiles(t, a)

	if _, err := a.root.HandleKey(press(input.KeyRight, 0)); err != nil {
		t.Fatalf("right: %v", err)
	}
	if _, err := a.root.HandleKey(press(input.KeyEnter, 0)); err != nil {
		t.Fatalf("enter: %v", err)
	}

	if a.root.Modal() != nil {
		t.Errorf("the switcher is still up: %T", a.root.Modal())
	}
	if got := ui.FocusedLeaf(a.root.Widget()); got != panes[1] {
		t.Errorf("the window is on %v, want the pane that was picked", a.paneName(got))
	}
}

// Escape leaves the window on the pane it was on.
func TestEscapeLeavesTheWindowWhereItWas(t *testing.T) {
	a := aWindowOfPanes(t, 4)
	panes := a.panesInSidebarOrder()
	a.focus(panes[1])
	openTiles(t, a)

	if _, err := a.root.HandleKey(press(input.KeyRight, 0)); err != nil {
		t.Fatalf("right: %v", err)
	}
	if _, err := a.root.HandleKey(press(input.KeyEscape, 0)); err != nil {
		t.Fatalf("escape: %v", err)
	}

	if a.root.Modal() != nil {
		t.Errorf("the switcher is still up: %T", a.root.Modal())
	}
	if got := ui.FocusedLeaf(a.root.Widget()); got != panes[1] {
		t.Errorf("the window moved to %v", a.paneName(got))
	}
}

// Each tile gets a layer of its own, scaled to fit, so the pictures are
// drawn by the GPU rather than copied cell by cell.
func TestEachTileGetsAScaledLayer(t *testing.T) {
	a := aWindowOfPanes(t, 4)
	tiles := openTiles(t, a)

	a.placeSwitcher()

	if got := len(a.switcher.shown); got != 4 {
		t.Fatalf("%d panes have a picture, want 4", got)
	}
	for what, tile := range a.switcher.shown {
		if tile.layer.Hidden {
			t.Errorf("%v has a picture that is not shown", a.paneName(what))
		}
		if tile.layer.Scale <= 0 || tile.layer.Scale > 1 {
			t.Errorf("%v is drawn at %v, want it shrunk to fit",
				a.paneName(what), tile.layer.Scale)
		}
	}
	// And every picture lands inside the tile it belongs to.
	a.renderer.Measure(a.g, &a.geo)
	for i, area := range tiles.Areas() {
		what := a.switcher.panes[i]
		tile := a.switcher.shown[what]
		left, width := a.geo.CellsX(area.X, area.X+area.Cols)
		top, height := a.geo.CellsY(area.Y, area.Y+area.Rows)
		if tile.layer.X < left || tile.layer.Y < top {
			t.Errorf("tile %d starts at %d,%d, before its box at %d,%d",
				i, tile.layer.X, tile.layer.Y, left, top)
		}
		// The far edge as well as the near one: a picture drawn too big
		// spills over the tiles beside it.
		wide := int(float64(tile.geo.Width()) * tile.layer.Scale)
		tall := int(float64(tile.geo.Height()) * tile.layer.Scale)
		if tile.layer.X+wide > left+width || tile.layer.Y+tall > top+height {
			t.Errorf("tile %d runs to %d,%d, past its box ending at %d,%d",
				i, tile.layer.X+wide, tile.layer.Y+tall, left+width, top+height)
		}
	}
}

// A window with no room left for a tile hides its picture. A scale of
// nothing means none at all, so the picture would otherwise be drawn
// whole over the window.
func TestATileWithNoRoomHidesItsPicture(t *testing.T) {
	a := aWindowOfPanes(t, 4)
	openTiles(t, a)
	a.placeSwitcher()

	// One row, which leaves the tiles in the first row no height at all.
	a.root.Layout(ui.Rect{Cols: 140, Rows: 1})
	a.placeSwitcher()

	for what, tile := range a.switcher.shown {
		if tile.layer.Hidden {
			continue
		}
		if tile.layer.Scale <= 0 {
			t.Errorf("%v is shown at a scale of %v", a.paneName(what), tile.layer.Scale)
		}
	}
}

// The picture is what the pane is showing now, not what it showed when
// the switcher opened.
func TestThePictureIsWhatThePaneIsShowingNow(t *testing.T) {
	a := aWindowOfPanes(t, 4)
	openTiles(t, a)
	a.placeSwitcher()

	const said = "printed while the switcher was open"
	a.shells[0].out <- []byte(said)
	var pane *term.Terminal
	waitFor(t, a, "a pane to print", func() bool {
		for _, w := range a.panesInSidebarOrder() {
			if p, is := w.(*term.Terminal); is && strings.Contains(paneText(p), said) {
				pane = p
				return true
			}
		}
		return false
	})
	a.placeSwitcher()

	tile := a.switcher.shown[ui.Widget(pane)]
	if tile == nil {
		t.Fatal("the pane has no picture")
	}
	if got := tileText(tile.g); !strings.Contains(got, said) {
		t.Errorf("the picture does not show what the pane says now:\n%s", got)
	}
}

// tileText is what a tile's own grid says, one line a row.
func tileText(g *grid.Grid) string {
	cols, rows := g.Size()
	var b strings.Builder
	for y := 0; y < rows; y++ {
		for x := 0; x < cols; x++ {
			r := g.At(x, y).Rune
			if r == 0 {
				r = ' '
			}
			b.WriteRune(r)
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// Closing the switcher takes every picture off the window, so nothing is
// drawn for a pane nobody is looking at.
func TestClosingTheSwitcherTakesThePicturesOff(t *testing.T) {
	a := aWindowOfPanes(t, 4)
	// Counted before the switcher, so every picture has to go rather
	// than just one of them.
	was := len(a.comp.Layers())
	openTiles(t, a)
	a.placeSwitcher()
	// Its own layer and one picture per pane.
	if got := len(a.comp.Layers()) - was; got != 5 {
		t.Fatalf("the switcher put %d layers on, want 5", got)
	}

	if _, err := a.root.HandleKey(press(input.KeyEscape, 0)); err != nil {
		t.Fatalf("escape: %v", err)
	}

	if a.switcher != nil {
		t.Error("the window still holds a switcher")
	}
	if got := len(a.comp.Layers()); got != was {
		t.Errorf("%d layers are left of the %d there were", got, was)
	}
}

// A pane that closes while the switcher is open loses its picture rather
// than leaving one of a pane that has gone.
func TestAPaneThatClosesLosesItsPicture(t *testing.T) {
	a := aWindowOfPanes(t, 4)
	openTiles(t, a)
	a.placeSwitcher()
	going := a.panesInSidebarOrder()[3].(*term.Terminal)

	if err := a.closePane(going); err != nil {
		t.Fatalf("close it: %v", err)
	}
	a.placeSwitcher()

	if _, still := a.switcher.shown[ui.Widget(going)]; still {
		t.Error("the pane that closed still has a picture")
	}
	if got := len(a.switcher.shown); got != 3 {
		t.Errorf("%d pictures are left, want 3", got)
	}
}

// A window with one pane says there is nothing to switch between.
func TestAWindowWithOnePaneSaysThereIsNothingToSwitch(t *testing.T) {
	a := aWindowOfPanes(t, 1)

	err := a.openSwitcher()

	if err == nil {
		t.Fatal("it opened a switcher for one pane")
	}
	if !strings.Contains(err.Error(), "only one pane") {
		t.Errorf("it says %q", err)
	}
}

// A window too small to draw the panes says so rather than showing a
// grid nobody can read.
func TestAWindowTooSmallSaysSo(t *testing.T) {
	a := newTestApp(t, 20, 8)
	withDialogs(t, a)
	withPanel(t, a)
	for len(a.panes) < 4 {
		if err := a.openPane(); err != nil {
			t.Fatalf("open a pane: %v", err)
		}
	}

	err := a.openSwitcher()

	if err == nil {
		t.Fatal("it drew four panes in a window that small")
	}
	if !strings.Contains(err.Error(), "too small") {
		t.Errorf("it says %q", err)
	}
}

// A window with no room for tiles at all hides every picture, rather
// than leaving them where they were last frame.
func TestAWindowWithNoRoomAtAllHidesEveryPicture(t *testing.T) {
	a := aWindowOfPanes(t, 4)
	tiles := openTiles(t, a)
	a.placeSwitcher()

	// No room, so the tiles have no boxes to place the pictures in.
	tiles.Layout(ui.Size{})
	a.placeSwitcher()

	if got := len(tiles.Areas()); got != 0 {
		t.Fatalf("the tiles laid out %d boxes in no room", got)
	}
	for what, tile := range a.switcher.shown {
		if !tile.layer.Hidden {
			t.Errorf("%v is still drawn, with no box to draw it in", a.paneName(what))
		}
	}
}

// A pane the tree has let go of has no size to draw, and is left out
// rather than drawn at a size made up for it.
func TestAPaneTheTreeHasLetGoOfHasNoSize(t *testing.T) {
	a := aWindowOfPanes(t, 2)
	loose := files.New(nil)

	if _, ok := a.paneScreen(loose); ok {
		t.Error("a pane that is not in the tree was given a size")
	}
	// And one that is in the tree has one.
	if _, ok := a.paneScreen(a.panesInSidebarOrder()[0]); !ok {
		t.Error("a pane in the tree has no size")
	}
}

// A screen too big for a texture is left out. A held screen is sized by
// somebody on another machine, so it is bounded rather than trusted.
func TestAScreenTooBigForATextureIsLeftOut(t *testing.T) {
	a := aWindowOfPanes(t, 4)
	huge := a.panesInSidebarOrder()[0].(*term.Terminal)
	cellW, cellH := a.renderer.CellSize()
	huge.Hold(mostScaledPixels/cellW+10, mostScaledPixels/cellH+10)
	if fitsATexture(huge.Size(), cellW, cellH) {
		t.Fatalf("a %v screen still fits a texture, so this proves nothing", huge.Size())
	}
	openTiles(t, a)

	a.placeSwitcher()

	if _, drawn := a.switcher.shown[ui.Widget(huge)]; drawn {
		t.Error("a screen too big for a texture was drawn anyway")
	}
	// The rest are still drawn.
	if got := len(a.switcher.shown); got != 3 {
		t.Errorf("%d panes have a picture, want the 3 that fit", got)
	}
}
