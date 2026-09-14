package main

import (
	"testing"

	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/input"
	"github.com/marrasen/gridterm/render"
	"github.com/marrasen/gridterm/ui"
)

// withMenubar puts a bar over the app's tree, the way main does.
func withMenubar(t *testing.T, a *testApp) *ui.Menubar {
	t.Helper()
	a.comp = render.NewCompositor(nil)
	a.commands()
	a.bar = a.newMenubar(a.root.Widget())
	a.root.SetWidget(a.bar)
	a.relayout()
	return a.bar
}

// TestEveryMenuLineNamesACommand is the tripwire for a typo in the menu
// definitions. A line naming nothing is drawn greyed out and cannot be
// chosen, so nothing else in the program would report it.
func TestEveryMenuLineNamesACommand(t *testing.T) {
	a := newTestApp(t, 40, 20)
	bar := withMenubar(t, a)

	for _, def := range bar.Menus {
		if def.Title == "" {
			t.Error("a menu has no title")
		}
		for _, item := range def.Items {
			if item.Command == "" {
				continue // a separator
			}
			if _, ok := a.root.Commands.Lookup(item.Command); !ok {
				t.Errorf("menu %q names command %q, which is not registered",
					def.Title, item.Command)
			}
		}
	}
}

// TestMenuOpensOnItsOwnLayer checks the dialog's whole life through the
// app: it goes on the modal stack, gets a layer of its own, and takes
// both away again.
func TestMenuOpensOnItsOwnLayer(t *testing.T) {
	a := newTestApp(t, 40, 20)
	bar := withMenubar(t, a)
	layersBefore := len(a.comp.Layers())

	if err := a.openMenu(); err != nil {
		t.Fatalf("open menu: %v", err)
	}

	if bar.OpenIndex() != 0 {
		t.Errorf("open = %d, want the first menu", bar.OpenIndex())
	}
	if a.root.Modal() == nil {
		t.Error("the menu is not on the modal stack")
	}
	if got := len(a.comp.Layers()); got != layersBefore+1 {
		t.Errorf("%d layers, want one more than %d", got, layersBefore)
	}

	if err := a.openMenu(); err != nil {
		t.Fatalf("close menu: %v", err)
	}

	if bar.OpenIndex() != -1 {
		t.Errorf("open = %d, want nothing open", bar.OpenIndex())
	}
	if a.root.Modal() != nil {
		t.Error("the menu is still on the modal stack")
	}
	if got := len(a.comp.Layers()); got != layersBefore {
		t.Errorf("%d layers, want it back to %d", got, layersBefore)
	}
}

// TestMenuAndPaletteGetALayerEach checks two dialogs open at once. One
// shared layer would have each clear the other, because both paint their
// whole view so that a box which shrank leaves nothing behind.
func TestMenuAndPaletteGetALayerEach(t *testing.T) {
	a := newTestApp(t, 40, 20)
	withMenubar(t, a)
	layersBefore := len(a.comp.Layers())

	if err := a.openMenu(); err != nil {
		t.Fatalf("open menu: %v", err)
	}
	if err := a.openPalette(); err != nil {
		t.Fatalf("open palette: %v", err)
	}

	if got := len(a.modals); got != 2 {
		t.Fatalf("%d dialogs on the stack, want two", got)
	}
	if a.modals[0].layer == a.modals[1].layer || a.modals[0].g == a.modals[1].g {
		t.Error("the two dialogs share a layer, so each would wipe the other")
	}
	if got := len(a.comp.Layers()); got != layersBefore+2 {
		t.Errorf("%d layers, want two more than %d", got, layersBefore)
	}

	// Each is drawn onto its own grid and nothing else.
	a.drawModals()
	for i, m := range a.modals {
		if !m.g.AnyDirty() {
			t.Errorf("dialog %d drew nothing onto its layer", i)
		}
	}
}

// TestHidingADialogTakesWhatIsStackedOnIt checks the ordering rule: a
// dialog cannot be pulled out from under the one covering it, so closing
// it closes that one too and says so.
func TestHidingADialogTakesWhatIsStackedOnIt(t *testing.T) {
	a := newTestApp(t, 40, 20)
	bar := withMenubar(t, a)
	if err := a.openMenu(); err != nil {
		t.Fatalf("open menu: %v", err)
	}
	if err := a.openPalette(); err != nil {
		t.Fatalf("open palette: %v", err)
	}

	// Close the menu, which is underneath the palette.
	bar.Close()

	if got := len(a.modals); got != 0 {
		t.Errorf("%d dialogs left, want none", got)
	}
	if a.root.Modal() != nil {
		t.Error("something is still on the modal stack")
	}
	if got := len(a.comp.Layers()); got != 0 {
		t.Errorf("%d layers left, want none", got)
	}
	// The palette went with it, and was told so rather than being left
	// thinking it is still on screen.
	if a.palette != nil || a.dismissPalette != nil {
		t.Error("the palette was taken away without being told")
	}
	if bar.OpenIndex() != -1 {
		t.Errorf("the bar still marks menu %d as open", bar.OpenIndex())
	}
}

// TestMenuFollowsAResize checks that widening the window moves the menu
// with it rather than leaving it measured for the old one.
func TestMenuFollowsAResize(t *testing.T) {
	a := newTestApp(t, 40, 20)
	a.g = grid.New(40, 20, a.colours.FG, a.colours.BG)
	withMenubar(t, a)
	if err := a.openMenu(); err != nil {
		t.Fatalf("open menu: %v", err)
	}

	a.setGridSize(80, 30)

	cols, rows := a.modals[0].g.Size()
	if cols != 80 || rows != 30 {
		t.Errorf("the menu's layer is %dx%d, want the window's 80x30", cols, rows)
	}
}

// TestMenuBarLeavesThePaneTheRestOfTheWindow checks that the bar takes
// exactly one row and the pane is told about the rest.
func TestMenuBarLeavesThePaneTheRestOfTheWindow(t *testing.T) {
	a := newTestApp(t, 40, 20)
	withMenubar(t, a)
	checkTree(t, a)

	pane := ui.FocusedLeaf(a.root.Widget())
	area, shown := a.root.AreaOf(pane)
	if !shown {
		t.Fatal("the pane is not on screen")
	}
	if want := (ui.Rect{Y: 1, Cols: 40, Rows: 19}); area != want {
		t.Errorf("pane area = %+v, want %+v", area, want)
	}
}

// TestMenuRunsACommand checks the third way into the registry end to
// end: a line chosen in a menu does what its key binding would.
func TestMenuRunsACommand(t *testing.T) {
	a := newTestApp(t, 40, 20)
	withMenubar(t, a)
	if err := a.openMenu(); err != nil {
		t.Fatalf("open menu: %v", err)
	}
	menu, ok := a.root.Modal().(*ui.Menu)
	if !ok {
		t.Fatalf("top modal = %T, want a menu", a.root.Modal())
	}

	// The File menu starts on "New tab".
	cmd, ok := menu.Selected()
	if !ok {
		t.Fatal("nothing is selected")
	}
	if cmd.ID != "tab.open" {
		t.Fatalf("selected %q, want the first line of the File menu", cmd.ID)
	}
	if _, err := a.root.HandleKey(press(input.KeyEnter, 0)); err != nil {
		t.Fatalf("enter: %v", err)
	}

	checkTree(t, a)
	if len(a.panes) != 2 {
		t.Errorf("%d panes, want the menu to have opened a tab", len(a.panes))
	}
	if len(a.modals) != 0 {
		t.Error("the menu stayed open after running something")
	}
}

// TestMenuTakesKeysFromTheTerminal checks the routing a modal needs:
// while a menu is open the shell must not receive the arrow keys.
func TestMenuTakesKeysFromTheTerminal(t *testing.T) {
	a := newTestApp(t, 40, 20)
	withMenubar(t, a)
	if err := a.openMenu(); err != nil {
		t.Fatalf("open menu: %v", err)
	}
	menu := a.root.Modal().(*ui.Menu)
	before := menu.SelectedIndex()

	if _, err := a.root.HandleKey(press(input.KeyDown, 0)); err != nil {
		t.Fatalf("down: %v", err)
	}

	if menu.SelectedIndex() == before {
		t.Error("the arrow key went past the menu to the pane underneath")
	}
}
