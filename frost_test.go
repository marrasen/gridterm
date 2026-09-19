package main

import (
	"testing"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/input"
	"github.com/marrasen/gridterm/render"
	"github.com/marrasen/gridterm/ui"
)

// TestDialogGetsFrostedGlass drives the window's own draw path with a
// dialog open and checks the panel is drawn. It is the only test that
// runs the shaders against a real screen; everything else about frost is
// arithmetic.
func TestDialogGetsFrostedGlass(t *testing.T) {
	a := newTestApp(t, 40, 20)
	a.comp = render.NewCompositor(a.renderer)
	a.comp.OnError = func(err error) { t.Errorf("compositor: %v", err) }
	a.layer = &render.Layer{Grid: a.g}
	a.comp.Add(a.layer)
	a.commands()
	a.bar = a.newMenubar(a.root.Widget())
	a.root.SetWidget(a.bar)
	a.relayout()
	if err := a.openMenu(); err != nil {
		t.Fatalf("open menu: %v", err)
	}

	cw, ch := a.renderer.CellSize()
	a.Draw(ebiten.NewImage(40*cw, 20*ch))

	if got := a.comp.Stats().Frosted; got != 1 {
		t.Errorf("%d panels drawn, want one behind the menu", got)
	}
	// The panel covers the menu's box and nothing else.
	menu, ok := a.root.Modal().(*ui.Menu)
	if !ok {
		t.Fatalf("top modal = %T, want a menu", a.root.Modal())
	}
	box := menu.Box()
	want := a.modals[0].layer.Frost.Rect
	if want.Dx() != box.Cols*cw || want.Dy() != box.Rows*ch {
		t.Errorf("panel is %v, want the menu's %d by %d cells", want, box.Cols, box.Rows)
	}
	if want.Min.X != box.X*cw || want.Min.Y != box.Y*ch {
		t.Errorf("panel starts at %v, want the menu's corner at %d,%d", want.Min, box.X, box.Y)
	}
}

// TestNoDialogDrawsNoGlass checks that an ordinary window pays nothing
// for the frost path.
func TestNoDialogDrawsNoGlass(t *testing.T) {
	a := newTestApp(t, 40, 20)
	a.comp = render.NewCompositor(a.renderer)
	a.comp.OnError = func(err error) { t.Errorf("compositor: %v", err) }
	a.layer = &render.Layer{Grid: a.g}
	a.comp.Add(a.layer)
	a.commands()

	cw, ch := a.renderer.CellSize()
	a.Draw(ebiten.NewImage(40*cw, 20*ch))

	if got := a.comp.Stats().Frosted; got != 0 {
		t.Errorf("%d panels drawn with no dialog open", got)
	}
}

// TestFrostFollowsTheDialog checks the panel moves with the box. A
// palette box shrinks as the query narrows it, and a panel left where
// the old one was would sit beside the dialog.
func TestFrostFollowsTheDialog(t *testing.T) {
	a := newTestApp(t, 60, 20)
	a.comp = render.NewCompositor(a.renderer)
	a.commands()
	if err := a.openPalette(); err != nil {
		t.Fatalf("open palette: %v", err)
	}
	a.drawModals()
	wide := a.modals[0].layer.Frost.Rect

	// Typing narrows the list, which makes the box shorter.
	for _, r := range "split" {
		ev := input.Event{Kind: input.Text, Rune: r, NormalText: true}
		if _, err := a.root.HandleKey(ev); err != nil {
			t.Fatalf("typing: %v", err)
		}
	}
	a.drawModals()

	narrow := a.modals[0].layer.Frost.Rect
	if narrow == wide {
		t.Fatal("the box did not change, so this proves nothing")
	}
	cw, ch := a.renderer.CellSize()
	box := a.palette.Box()
	if narrow.Dy() != box.Rows*ch || narrow.Dx() != box.Cols*cw {
		t.Errorf("panel is %v, want the dialog's %d by %d cells", narrow, box.Cols, box.Rows)
	}
}

// TestFrostGoesWithTheDialog checks that closing a dialog takes its
// panel with it, rather than leaving glass over the window.
func TestFrostGoesWithTheDialog(t *testing.T) {
	a := newTestApp(t, 40, 20)
	a.comp = render.NewCompositor(a.renderer)
	a.commands()
	if err := a.openPalette(); err != nil {
		t.Fatalf("open palette: %v", err)
	}
	a.drawModals()
	if a.modals[0].layer.Frost.Rect.Empty() {
		t.Fatal("the dialog has no panel to lose, so this proves nothing")
	}

	a.closePalette()

	for _, l := range a.comp.Layers() {
		if l.Frost != nil && !l.Frost.Rect.Empty() {
			t.Error("a panel is still on screen with no dialog behind it")
		}
	}
}

// The glass lands on the cells the dialog draws in, not on the padding
// around them. The window puts a half-cell margin before column 0, so a
// panel measured from the outer boxes reached out over it and the menu
// at the left edge looked as though its border had slipped.
func TestTheGlassLandsOnTheCellsTheDialogDrawsIn(t *testing.T) {
	a := newTestApp(t, 40, 20)
	a.comp = render.NewCompositor(a.renderer)
	a.layer = &render.Layer{Grid: a.g}
	a.comp.Add(a.layer)
	a.commands()
	a.bar = a.newMenubar(a.root.Widget())
	a.root.SetWidget(a.bar)
	a.relayout()
	a.applyPads()
	if err := a.openMenu(); err != nil {
		t.Fatalf("open menu: %v", err)
	}
	menu, ok := a.root.Modal().(*ui.Menu)
	if !ok {
		t.Fatalf("top modal = %T, want a menu", a.root.Modal())
	}
	box := menu.Box()
	if box.X != 0 {
		t.Fatalf("the menu is at column %d, and this tests the one at the left edge", box.X)
	}
	// The window pads row 0 for the menu bar, and the menu starts below
	// it, so the row the glass is measured from is padded here.
	a.g.SetRowPad(box.Y, grid.Pad{Before: 2, After: 2})
	a.modals[0].g.SetRowPad(box.Y, grid.Pad{Before: 2, After: 2})

	a.drawModals()

	// The padding is there, or the two ways of measuring agree and this
	// tests nothing.
	if a.geo.CellX(0) == 0 {
		t.Fatal("column 0 has no padding before it, so this proves nothing")
	}
	got := a.modals[0].layer.Frost.Rect
	if want := a.geo.CellX(box.X); got.Min.X != want {
		t.Errorf("the glass starts at %d, want the %d the border is drawn at", got.Min.X, want)
	}
	cw, ch := a.renderer.CellSize()
	if want := a.geo.CellX(box.X+box.Cols-1) + cw; got.Max.X != want {
		t.Errorf("the glass ends at %d, want the %d the border ends at", got.Max.X, want)
	}
	if at, _ := a.geo.RowBox(box.Y, box.Y+1); a.geo.CellY(box.Y) == at {
		t.Fatal("the menu's first row has no padding before it, so this proves nothing")
	}
	if want := a.geo.CellY(box.Y); got.Min.Y != want {
		t.Errorf("the glass starts at row pixel %d, want the %d the border is drawn at",
			got.Min.Y, want)
	}
	if want := a.geo.CellY(box.Y+box.Rows-1) + ch; got.Max.Y != want {
		t.Errorf("the glass ends at row pixel %d, want the %d the border ends at",
			got.Max.Y, want)
	}
}

// The glass casts the dialog's shadow, so the widgets draw none in
// cells. A dialog's corner is rounded in pixels by the shader, and a
// shadow in whole cells can never follow it.
func TestTheGlassCastsTheShadowAndTheCellsDoNot(t *testing.T) {
	a := newTestApp(t, 80, 24)

	glass := a.frost()

	if glass == nil {
		t.Fatal("a window with no theme frame has no glass")
	}
	if glass.Shadow.A == 0 {
		t.Error("the glass casts no shadow")
	}
	if glass.Drop == [2]float32{0, 0} {
		t.Error("the shadow falls nowhere, so it sits behind the panel")
	}
	if got := a.panelShadow(); got.A != 0 {
		t.Errorf("the widgets draw a shadow of %v in cells as well", got)
	}
}

// A theme that draws its own frame gets no glass, so the cells draw the
// shadow the way they always did.
func TestAThemeWithItsOwnFrameKeepsTheCellShadow(t *testing.T) {
	a := newTestApp(t, 80, 24)
	a.look.Set = true

	if a.frost() != nil {
		t.Error("a theme with its own frame was given glass")
	}
	if got := a.panelShadow(); got.A == 0 {
		t.Error("a theme with its own frame draws no shadow at all")
	}
}

// The shadow falls further when the text is drawn bigger, the way the
// corner is already rounded further.
//
// Both are measured in cells, so a dialog open while the font size
// changes keeps its shape rather than tucking the shadow under itself.
func TestTheShadowFollowsTheFontSize(t *testing.T) {
	a := newTestApp(t, 60, 20)
	a.comp = render.NewCompositor(a.renderer)
	a.commands()
	// Room in pixels, so the dialog still has cells to sit in once the
	// text is twice the size.
	cw, ch := a.renderer.CellSize()
	a.resizeTo(120*cw, 60*ch)
	if err := a.openPalette(); err != nil {
		t.Fatalf("open the palette: %v", err)
	}
	a.drawModals()

	small := a.modals[0].layer.Frost.Drop
	if small == [2]float32{0, 0} {
		t.Fatalf("the shadow falls nowhere at %v", small)
	}

	if err := a.setFontSize(a.fontSize * 2); err != nil {
		t.Fatalf("make the text bigger: %v", err)
	}
	a.drawModals()

	big := a.modals[0].layer.Frost.Drop
	if big[0] <= small[0] || big[1] <= small[1] {
		t.Errorf("the shadow falls %v at twice the font size, want further than %v", big, small)
	}
	// And it is the drop for the cell the text is drawn in now, not a
	// bigger one that happens to have grown.
	grew, _ := a.renderer.CellSize()
	if want := frostDrop(a.renderer.CellSize()); big != want {
		t.Errorf("the shadow falls %v for a cell %d wide, want %v", big, grew, want)
	}
}
