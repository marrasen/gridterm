package main

import (
	"strings"
	"testing"
	"time"

	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/input"
	"github.com/marrasen/gridterm/render"
	"github.com/marrasen/gridterm/ui"
	"github.com/marrasen/gridterm/ui/files"
	"github.com/marrasen/gridterm/ui/term"
	"github.com/marrasen/gridterm/vfs"
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

// openTiles opens the switcher, lets the zoom finish and returns the
// tiles. A test about where a picture settles wants it settled.
func openTiles(t *testing.T, a *testApp) *ui.Tiles {
	t.Helper()
	tiles := openTilesZooming(t, a)
	zoomDone(a)
	return tiles
}

// openTilesZooming opens the switcher with the zoom still to run.
func openTilesZooming(t *testing.T, a *testApp) *ui.Tiles {
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

// zoomDone moves the window's clock past the zoom.
func zoomDone(a *testApp) { moveClock(a, zoomTime) }

// moveClock moves the window's clock on by d.
func moveClock(a *testApp, d time.Duration) {
	was := a.clock()
	a.now = func() time.Time { return was.Add(d) }
	a.frameAt = a.now()
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

	frame(t, a)

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
	// And every picture lands inside the rule round its tile, not over
	// it.
	a.renderer.Measure(a.g, &a.geo)
	for i := range tiles.Areas() {
		what := a.switcher.panes[i]
		tile := a.switcher.shown[what]
		inside := tiles.Inside(i)
		left, width := a.geo.CellsX(inside.X, inside.X+inside.Cols)
		top, height := a.geo.CellsY(inside.Y, inside.Y+inside.Rows)
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
	frame(t, a)

	// One row, which leaves the tiles in the first row no height at all.
	a.root.Layout(ui.Rect{Cols: 140, Rows: 1})
	frame(t, a)

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
	frame(t, a)

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
	frame(t, a)

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
	for y := range rows {
		for x := range cols {
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
	frame(t, a)
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
	frame(t, a)
	going := a.panesInSidebarOrder()[3].(*term.Terminal)

	if err := a.closePane(going); err != nil {
		t.Fatalf("close it: %v", err)
	}
	frame(t, a)

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
	frame(t, a)

	// No room, so the tiles have no boxes to place the pictures in.
	tiles.Layout(ui.Size{})
	frame(t, a)

	if got := len(tiles.Areas()); got != 0 {
		t.Fatalf("the tiles laid out %d boxes in no room", got)
	}
	for what, tile := range a.switcher.shown {
		if !tile.layer.Hidden {
			t.Errorf("%v is still drawn, with no box to draw it in", a.paneName(what))
		}
	}
}

// A pane that was never laid out has no size to draw, and is left out
// rather than drawn at a size made up for it.
func TestAPaneThatWasNeverLaidOutHasNoSize(t *testing.T) {
	a := aWindowOfPanes(t, 2)
	loose := files.New(nil)

	if _, ok := a.paneScreen(loose); ok {
		t.Error("a pane that was never laid out was given a size")
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

	frame(t, a)

	if _, drawn := a.switcher.shown[ui.Widget(huge)]; drawn {
		t.Error("a screen too big for a texture was drawn anyway")
	}
	// The rest are still drawn.
	if got := len(a.switcher.shown); got != 3 {
		t.Errorf("%d panes have a picture, want the 3 that fit", got)
	}
}

// A tile says what its pane is called now, not what it was called when
// the switcher opened. A program renames a pane as it goes.
func TestATileSaysWhatThePaneIsCalledNow(t *testing.T) {
	a := aWindowOfPanes(t, 4)
	tiles := openTiles(t, a)
	frame(t, a)
	pane := a.switcher.panes[0]
	was := tiles.Name(0)

	renamePane(t, a, pane, "a new title")
	frame(t, a)

	got := tiles.Name(0)
	if got == was {
		t.Fatalf("the tile still says %q", got)
	}
	if !strings.Contains(got, "a new title") {
		t.Errorf("the tile says %q, want the pane's new name", got)
	}
}

// renamePane gives a pane a new label the way the window does.
func renamePane(t *testing.T, a *testApp, w ui.Widget, name string) {
	t.Helper()
	pane, is := w.(*term.Terminal)
	if !is {
		t.Fatalf("%T is not a terminal", w)
	}
	e := a.panes[pane]
	if e == nil {
		t.Fatal("the pane has no row")
	}
	e.Label = name
}

// The pictures zoom out of the room their panes were in, shrinking on
// to their tiles rather than jumping there.
func TestThePicturesZoomOutOfTheRoomTheirPanesWereIn(t *testing.T) {
	a := aWindowOfPanes(t, 4)
	tiles := openTilesZooming(t, a)
	s := a.switcher
	inside := tiles.Inside(0)
	from := a.paneRoom(s.panes[0])
	if from.Empty() {
		t.Fatal("the pane has no room to zoom out of")
	}

	// It starts on the pane's own room and ends on the tile.
	if got := s.boxAt(from, inside, a.frameTime()); got != from {
		t.Errorf("it opens at %v, want the %v the pane was in", got, from)
	}
	zoomDone(a)
	if got := s.boxAt(from, inside, a.frameTime()); got != inside {
		t.Errorf("it settles at %v, want the tile's %v", got, inside)
	}
}

// And it shrinks every step of the way, rather than going out before it
// comes in.
func TestTheZoomOnlyEverShrinks(t *testing.T) {
	a := aWindowOfPanes(t, 4)
	tiles := openTilesZooming(t, a)
	s := a.switcher
	inside := tiles.Inside(0)
	from := a.paneRoom(s.panes[0])
	if from.Cols <= inside.Cols {
		t.Fatalf("the pane's room %v is no wider than its tile %v", from, inside)
	}

	was := from.Cols + 1
	for step := range 8 {
		at := s.opened.Add(zoomTime * time.Duration(step) / 7)
		got := s.boxAt(from, inside, at).Cols
		if got > was {
			t.Errorf("step %d is %d columns wide, after %d", step, got, was)
		}
		was = got
	}
	if was != inside.Cols {
		t.Errorf("it ends %d columns wide, want the tile's %d", was, inside.Cols)
	}
	// And it stays there. The ease runs backwards past its end, so a
	// zoom with nothing to stop it would swing back out again.
	late := s.opened.Add(zoomTime * 4)
	if got := s.boxAt(from, inside, late); got != inside {
		t.Errorf("long after it settled it is at %v, want the tile's %v", got, inside)
	}
	if s.zooming(late) {
		t.Error("it is still zooming long after its time was up")
	}
}

// The zoom lasts long enough to be seen and not long enough to be a
// wait.
func TestTheZoomLastsLongEnoughToSee(t *testing.T) {
	if zoomTime < 60*time.Millisecond {
		t.Errorf("the zoom takes %v, too quick to read as a movement", zoomTime)
	}
	if zoomTime > 300*time.Millisecond {
		t.Errorf("the zoom takes %v, long enough to be a wait", zoomTime)
	}
	// And it really is still running part way through.
	s := &switcher{opened: time.Now()}
	if !s.zooming(s.opened.Add(zoomTime / 2)) {
		t.Error("it is over half way through its own time")
	}
}

// Two panes side by side zoom out of their own halves of the window,
// not out of one box they share. A pane that started somewhere it never
// was reads as a shuffle rather than a zoom.
func TestTwoPanesZoomOutOfTheirOwnHalves(t *testing.T) {
	a := aWindowOfPanes(t, 2)
	takeChoice(t, splitChoices(t, a, ui.Columns), "Move")
	panes := a.panesInSidebarOrder()
	if len(panes) != 2 {
		t.Fatalf("the window holds %d panes", len(panes))
	}

	first, second := a.paneRoom(panes[0]), a.paneRoom(panes[1])

	if first.Empty() || second.Empty() {
		t.Fatalf("a pane in the split has no room: %v and %v", first, second)
	}
	if first == second {
		t.Errorf("both panes zoom out of %v, want each out of its own half", first)
	}
	// And each is its own pane's room, not the stage they sit on.
	for i, pane := range panes {
		area, ok := a.paneArea(pane)
		if !ok {
			t.Fatalf("pane %d is not in the tree", i)
		}
		if got := a.paneRoom(pane); got != area {
			t.Errorf("pane %d zooms out of %v, want the %v it is in", i, got, area)
		}
	}
}

// The zoom is timed from the frame it opened on, not from the moment
// the key was pressed. A key is handled after the frame began, so a
// zoom timed from the key is already a frame old and snaps into place
// before jumping back out.
func TestTheZoomIsTimedFromTheFrameItOpenedOn(t *testing.T) {
	a := aWindowOfPanes(t, 4)
	// The frame began, and the key arrives a moment later.
	a.frameAt = a.clock()
	was := a.clock()
	a.now = func() time.Time { return was.Add(3 * time.Millisecond) }

	openTilesZooming(t, a)

	if got := a.switcher.opened; !got.Equal(a.frameAt) {
		t.Errorf("the zoom is timed from %v, want the frame at %v", got, a.frameAt)
	}
	if !a.switcher.zooming(a.frameTime()) {
		t.Error("the zoom was over before the frame it opened on had drawn")
	}
}

// The zoom does not mark the window dirty. The compositor already
// re-blits a layer that has moved, and the window's own grid has not
// changed.
func TestTheZoomDoesNotRepaintTheWindow(t *testing.T) {
	a := aWindowOfPanes(t, 4)
	openTilesZooming(t, a)
	frame(t, a)

	a.g.ClearDirty()
	a.placeSwitcher()

	if a.g.AnyDirty() {
		t.Error("a frame of the zoom repainted every cell of the window")
	}
}

// A switcher with nowhere to zoom out of goes straight to its tiles
// rather than starting from a box of nothing.
func TestASwitcherWithNowhereToZoomFromSettlesAtOnce(t *testing.T) {
	tile := ui.Rect{X: 4, Y: 2, Cols: 10, Rows: 6}

	// One that has never opened has no time to measure against.
	never := &switcher{}
	if got := never.zoomedTo(time.Now()); got != 1 {
		t.Errorf("one that never opened is %v of the way through, want all of it", got)
	}
	if never.zooming(time.Now()) {
		t.Error("one that never opened says it is zooming")
	}

	// And one whose pane has no room on screen goes straight to its
	// tile rather than growing out of a box of nothing.
	open := &switcher{opened: time.Now()}
	if got := open.boxAt(ui.Rect{}, tile, time.Now()); got != tile {
		t.Errorf("a pane with no room opens at %v, want its tile %v", got, tile)
	}
}

// The switcher goes when the window becomes too small to draw every
// pane at once.
//
// It refuses to open at that size. Staying open at it left a row of
// tiles with nothing inside them: the pictures have no room, so every
// one is hidden and what is left is empty boxes and names.
func TestTheSwitcherGoesWhenTheWindowIsTooSmallForIt(t *testing.T) {
	a := aWindowOfPanes(t, 4)
	openTiles(t, a)
	frame(t, a)
	if a.switcher == nil {
		t.Fatal("the switcher did not open")
	}

	a.setGridSize(24, 8)
	frame(t, a)

	if a.switcher != nil {
		t.Error("the switcher is still up in a window with no room for it")
	}
	if _, is := a.root.Modal().(*ui.Tiles); is {
		t.Error("the tiles are still the dialog on top")
	}
}

// And it stays while there is still room, so a window nudged smaller
// does not lose it.
func TestTheSwitcherStaysWhileThereIsRoom(t *testing.T) {
	a := aWindowOfPanes(t, 4)
	openTiles(t, a)
	frame(t, a)

	a.setGridSize(70, 22)
	frame(t, a)

	if a.switcher == nil {
		t.Fatal("the switcher went from a window that still had room")
	}
	shown := 0
	for _, tile := range a.switcher.shown {
		if !tile.layer.Hidden {
			shown++
		}
	}
	if shown != 4 {
		t.Errorf("%d pictures are shown after the resize, want 4", shown)
	}
	// And the tiles are laid out for the window it is now, not the one
	// it opened in.
	for i, area := range a.switcher.tiles.Areas() {
		if area.X+area.Cols > 70 || area.Y+area.Rows > 22 {
			t.Errorf("tile %d is at %v, which is off a 70x22 window", i, area)
		}
	}
}

// Asking for the switcher again while it is up takes it away.
//
// The shortcut that shows something is the one that hides it. Escape
// was the only way out, and pressing the shortcut again did nothing.
func TestTheSwitcherShortcutClosesItAgain(t *testing.T) {
	a := aWindowOfPanes(t, 4)
	openTiles(t, a)
	frame(t, a)
	if a.switcher == nil {
		t.Fatal("the switcher did not open")
	}

	// The chord itself, through the window. The tiles take every key
	// while they are up, so a test that called openSwitcher would pass
	// with the shortcut going nowhere.
	chord, on := a.root.Accelerators.ChordFor(switcherCommand)
	if !on {
		t.Fatal("the switcher has no shortcut")
	}
	sendKey(t, a, press(chord.Key, chord.Mods))
	frame(t, a)

	if a.switcher != nil {
		t.Error("the switcher is still up after being asked for again")
	}
	if _, is := a.root.Modal().(*ui.Tiles); is {
		t.Error("the tiles are still the dialog on top")
	}
}

// A window wide enough to pass the bound a watcher's screen is held to
// still shows its panes.
//
// Marcus maximised a window and every tile came up empty. The bound is
// there because a watcher on another machine chooses the size it asks
// for, and it was being applied to this window's own panes as well. A
// pane is about as wide as the window, so a maximised window on a
// display with any scaling went past 4096 and every picture was skipped.
func TestTheSwitcherShowsItsPanesInAMaximisedWindow(t *testing.T) {
	a := aWindowOfPanes(t, 4)
	cellW, cellH := a.renderer.CellSize()
	// Wider than a watcher's screen may be, the way a maximised window
	// on a scaled display is.
	cols := mostScaledPixels/cellW + 40
	rows := 60
	a.setGridSize(cols, rows)
	frame(t, a)

	openTiles(t, a)
	frame(t, a)

	if a.switcher == nil {
		t.Fatal("the switcher did not open in a big window")
	}
	for i, pane := range a.switcher.panes {
		size, ok := a.paneScreen(pane)
		if !ok {
			t.Fatalf("pane %d has no screen", i)
		}
		if fitsATexture(size, cellW, cellH) {
			t.Fatalf("pane %d is %v, which is inside the watcher's bound: "+
				"the window is not big enough to be the case this is about", i, size)
		}
	}
	shown := 0
	for _, tile := range a.switcher.shown {
		if !tile.layer.Hidden {
			shown++
		}
	}
	if shown != 4 {
		t.Errorf("%d pictures are shown in a maximised window, want 4", shown)
	}
}

// A screen a watcher sized keeps the tighter bound, even in a window
// big enough for its own panes to pass it.
//
// The two bounds mean different things. One is what the GPU will make,
// for a pane this window sized. The other is how far a size chosen on
// another machine is trusted, and a big window is no reason to trust it
// further.
func TestAWatchersScreenKeepsTheTighterBoundInABigWindow(t *testing.T) {
	a := aWindowOfPanes(t, 4)
	cellW, cellH := a.renderer.CellSize()
	a.setGridSize(mostScaledPixels/cellW+40, 60)
	frame(t, a)

	// Held at a size past what a watcher is trusted with, and inside
	// what this window's own panes are allowed.
	huge := a.panesInSidebarOrder()[0].(*term.Terminal)
	huge.Hold(mostScaledPixels/cellW+10, mostScaledPixels/cellH+10)
	if fitsATexture(huge.Size(), cellW, cellH) {
		t.Fatalf("a %v screen is inside the watcher's bound, so this proves nothing", huge.Size())
	}
	if !fitsAPaneTexture(huge.Size(), cellW, cellH) {
		t.Fatalf("a %v screen is outside the pane bound too, so this proves nothing", huge.Size())
	}

	openTiles(t, a)
	frame(t, a)

	if _, drawn := a.switcher.shown[ui.Widget(huge)]; drawn {
		t.Error("a screen a watcher sized was drawn past the bound it is trusted to")
	}
	if got := len(a.switcher.shown); got != 3 {
		t.Errorf("%d panes have a picture, want the 3 this window sized", got)
	}
}

// A picture exactly as wide as the bound is left out.
//
// ebiten pads an image by a pixel before it goes on an atlas, so one
// exactly at the limit is a pixel past what the GPU will make. That is
// a panic in the draw path and no recover anywhere, so the window dies
// rather than leaving a tile empty.
func TestAPictureExactlyAtTheBoundIsLeftOut(t *testing.T) {
	const cell = 8

	if fitsAPaneTexture(ui.Size{Cols: mostPanePixels / cell, Rows: 1}, cell, cell) {
		t.Error("a picture exactly as wide as the bound fits, want it left out")
	}
	if fitsAPaneTexture(ui.Size{Cols: 1, Rows: mostPanePixels / cell}, cell, cell) {
		t.Error("a picture exactly as tall as the bound fits, want it left out")
	}
	if !fitsAPaneTexture(ui.Size{Cols: mostPanePixels/cell - 1, Rows: 1}, cell, cell) {
		t.Error("a picture a cell narrower is left out, want it drawn")
	}
}

// A settled switcher lets the window idle.
//
// The tiles paint a ground and then draw frames over it, which writes
// the same cells twice. Straight onto the layer that dirtied every row
// it touched for good, and the window redrew every frame for a picture
// that was not moving.
func TestASettledSwitcherSkipsTheFrame(t *testing.T) {
	a := aWindowOfPanes(t, 8)
	openTiles(t, a)
	// The first frame after the zoom draws, and so does the one that
	// puts the pictures where they settle.
	frame(t, a)
	frame(t, a)

	frame(t, a)

	if got := a.comp.Stats(); !got.Skipped {
		t.Errorf("a settled switcher drew %+v, want the frame skipped entirely", got)
	}
}

// And a switcher whose picture moved does not skip, or the zoom would
// not be drawn at all.
func TestAMovingSwitcherDrawsTheFrame(t *testing.T) {
	a := aWindowOfPanes(t, 8)
	openTilesZooming(t, a)

	frame(t, a)

	if got := a.comp.Stats(); got.Skipped {
		t.Error("the switcher skipped a frame of its zoom")
	}
}

// A pane behind another still gets a picture when every pane is shown.
//
// A terminal knows its own screen, so it always had one. Everything else
// is measured by the room it has in the window, and a pane in a deck
// behind another has none: its tile came out empty.
func TestAPaneBehindAnotherStillGetsAPicture(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	a.comp = render.NewCompositor(a.renderer)
	a.layer = &render.Layer{Grid: a.g}
	a.comp.Add(a.layer)

	browser := openFilesFromThePlus(t, a, conns.Local)
	// A pane on top of it, which puts the browser behind in the deck.
	if err := a.openPane(); err != nil {
		t.Fatalf("a pane over it: %v", err)
	}
	a.relayout()
	if _, on := a.paneArea(browser); on {
		t.Fatal("the browser is still on screen, so this proves nothing")
	}

	if err := a.openSwitcher(); err != nil {
		t.Fatalf("show every pane: %v", err)
	}
	a.relayout()
	a.placeSwitcher()

	if a.switcher.shown[browser] == nil {
		t.Errorf("the browser's tile has no picture; %d of %d panes have one",
			len(a.switcher.shown), len(a.switcher.panes))
	}
}

// And the picture is of the browser, not of an empty grid: the size it
// was last laid out at is what it is drawn from.
func TestTheHiddenPanesPictureHoldsItsText(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	a.comp = render.NewCompositor(a.renderer)
	a.layer = &render.Layer{Grid: a.g}
	a.comp.Add(a.layer)

	browser := openFilesFromThePlus(t, a, conns.Local)
	if err := a.openPane(); err != nil {
		t.Fatalf("a pane over it: %v", err)
	}
	a.relayout()
	if err := a.openSwitcher(); err != nil {
		t.Fatalf("show every pane: %v", err)
	}
	a.relayout()
	a.placeSwitcher()

	tile := a.switcher.shown[browser]
	if tile == nil {
		t.Fatal("the browser's tile has no picture")
	}
	cols, rows := tile.g.Size()
	if cols <= 1 || rows <= 1 {
		t.Fatalf("the picture is %dx%d, want the room the browser last had", cols, rows)
	}
	var text strings.Builder
	for y := range rows {
		for x := range cols {
			if r := tile.g.At(x, y).Rune; r != 0 {
				text.WriteRune(r)
			}
		}
	}
	if !strings.Contains(text.String(), "Local") {
		t.Errorf("the picture holds %q, want the browser's own heading in it",
			strings.TrimSpace(text.String()))
	}
}

// A file being read is measured the same way, so one behind another pane
// gets a picture too.
func TestAReaderBehindAnotherStillGetsAPicture(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	a.comp = render.NewCompositor(a.renderer)
	a.layer = &render.Layer{Grid: a.g}
	a.comp.Add(a.layer)

	name, path := aReadableFile(t, "one", "two", "three")
	if err := a.openReader(vfs.NewLocal(), conns.Local, path, name, false, 0); err != nil {
		t.Fatalf("open a reader: %v", err)
	}
	reader := onlyReader(t, a)
	waitUntil(t, "the file to be read", func() bool {
		a.pump.run()
		return reader.Lines() > 0
	})
	if err := a.openPane(); err != nil {
		t.Fatalf("a pane over it: %v", err)
	}
	a.relayout()
	if _, on := a.paneArea(reader); on {
		t.Fatal("the reader is still on screen, so this proves nothing")
	}

	if err := a.openSwitcher(); err != nil {
		t.Fatalf("show every pane: %v", err)
	}
	a.relayout()
	a.placeSwitcher()

	if a.switcher.shown[reader] == nil {
		t.Errorf("the reader's tile has no picture; %d of %d panes have one",
			len(a.switcher.shown), len(a.switcher.panes))
	}
}

// A file pane that closes while the switcher is open loses its picture,
// the way a terminal does.
//
// Which panes the window has is what says so, not what kind of thing a
// widget is: every file pane is a file pane, closed or not. A machine
// that drops closes its file panes without the user touching anything.
func TestAFilePaneThatClosesLosesItsPicture(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	a.comp = render.NewCompositor(a.renderer)
	a.layer = &render.Layer{Grid: a.g}
	a.comp.Add(a.layer)

	browser := openFilesFromThePlus(t, a, conns.Local)
	if err := a.openPane(); err != nil {
		t.Fatalf("a second pane: %v", err)
	}
	a.relayout()
	if err := a.openSwitcher(); err != nil {
		t.Fatalf("show every pane: %v", err)
	}
	a.relayout()
	a.placeSwitcher()
	if a.switcher.shown[browser] == nil {
		t.Fatal("the browser has no picture to lose")
	}

	if err := a.closePane(browser); err != nil {
		t.Fatalf("close the browser: %v", err)
	}
	a.relayout()
	a.placeSwitcher()

	if _, still := a.switcher.shown[ui.Widget(browser)]; still {
		t.Error("the browser that closed still has a picture")
	}
}
