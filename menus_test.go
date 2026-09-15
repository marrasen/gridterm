package main

import (
	"errors"
	"testing"

	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/input"
	"github.com/marrasen/gridterm/remote"
	"github.com/marrasen/gridterm/render"
	"github.com/marrasen/gridterm/ui"
)

// withMenubar puts a bar over the app's tree, the way main does, with
// the tree on a layer of its own so a dialog's layer has something to
// sit above.
func withMenubar(t *testing.T, a *testApp) *ui.Menubar {
	t.Helper()
	a.comp = render.NewCompositor(nil)
	a.layer = &render.Layer{Grid: a.g}
	a.comp.Add(a.layer)
	a.commands()
	a.bar = a.newMenubar(a.root.Widget())
	a.root.SetWidget(a.bar)
	a.relayout()
	// The Servers menu reaches the bar from here, the way main does it.
	a.refreshServers()
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

// TestEveryPlusMenuLineNamesACommand is the same tripwire for the menu
// the plus on a machine's row drops down.
//
// hostItems builds a different list for each shape a name can have, and
// a line naming nothing is drawn greyed out wherever it appears.
func TestEveryPlusMenuLineNamesACommand(t *testing.T) {
	a := newTestApp(t, 40, 20)
	withMenubar(t, a)

	for _, shape := range hostShapes() {
		items := hostItems(shape.facts)
		if len(items) == 0 {
			t.Errorf("the plus on a %s offers nothing", shape.name)
		}
		for _, item := range items {
			if item.Command == "" {
				continue // a separator
			}
			if item.Title == "" {
				t.Errorf("a line of the %s menu names %q and has no title", shape.name, item.Command)
			}
			if _, ok := a.root.Commands.Lookup(item.Command); !ok {
				t.Errorf("the %s menu names command %q, which is not registered",
					shape.name, item.Command)
			}
		}
	}
}

// hostShapes is one hostFacts of every shape the plus menu is built for,
// including the two flags that add lines of their own.
func hostShapes() []struct {
	name  string
	facts hostFacts
} {
	var out []struct {
		name  string
		facts hostFacts
	}
	add := func(name string, f hostFacts) {
		out = append(out, struct {
			name  string
			facts hostFacts
		}{name, f})
	}
	for _, kind := range []hostKind{
		hostUnknown, hostHere, hostWindow, hostSavedWindow,
		hostMachine, hostConnecting, hostSavedMachine,
	} {
		add(kind.String(), hostFacts{name: "margit", kind: kind})
		add(kind.String()+", saved", hostFacts{name: "margit", kind: kind, saved: true})
		add(kind.String()+", saved as a window", hostFacts{
			name: "margit", kind: kind, saved: true, serves: true,
		})
	}
	return out
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

	// Each is drawn onto its own grid and nothing else. A new grid starts
	// dirty, so the damage has to be cleared or this proves nothing.
	for _, m := range a.modals {
		m.g.ClearDirty()
	}
	a.drawModals()
	for i, m := range a.modals {
		if !m.g.AnyDirty() {
			t.Errorf("dialog %d drew nothing onto its layer", i)
		}
	}
}

// TestDialogLayersSitAboveTheTree checks the compositor order. A dialog
// under the tree's layer is drawn and then covered by the window.
func TestDialogLayersSitAboveTheTree(t *testing.T) {
	a := newTestApp(t, 40, 20)
	withMenubar(t, a)
	if err := a.openMenu(); err != nil {
		t.Fatalf("open menu: %v", err)
	}

	layers := a.comp.Layers()
	if len(layers) != 2 {
		t.Fatalf("%d layers, want the tree and the dialog", len(layers))
	}
	if layers[0] != a.layer {
		t.Error("the tree is not at the bottom of the stack")
	}
	if layers[1] != a.modals[0].layer {
		t.Error("the dialog is not above the tree")
	}
}

// TestOpeningADialogDoesNotRepaintTheTree is the point of giving a
// dialog its own layer: the window underneath has not changed, so
// nothing under it should be redrawn. Left and Right along a menu bar
// close one menu and open the next, so a repaint here is a repaint of
// every pane on every keystroke.
func TestOpeningADialogDoesNotRepaintTheTree(t *testing.T) {
	a := newTestApp(t, 40, 20)
	bar := withMenubar(t, a)
	a.root.Draw(a.g.View())
	a.g.ClearDirty()

	if err := a.openMenu(); err != nil {
		t.Fatalf("open menu: %v", err)
	}
	a.root.Draw(a.g.View())

	// Two rows may change: the bar's, because a title is now marked open,
	// and whichever row the focused pane's cursor was on, because the
	// cursor goes while a dialog holds focus. Marking the window dirty
	// would redraw all twenty.
	if got := dirtyRows(a); got > 2 {
		t.Errorf("opening a menu dirtied %d of 20 rows, want at most 2", got)
	}

	a.g.ClearDirty()
	bar.Close()
	a.root.Draw(a.g.View())
	if got := dirtyRows(a); got > 2 {
		t.Errorf("closing a menu dirtied %d of 20 rows, want at most 2", got)
	}
}

// dirtyRows counts the rows of the window that changed.
func dirtyRows(a *testApp) int {
	_, rows := a.g.Size()
	n := 0
	for y := 0; y < rows; y++ {
		if a.g.RowDirty(y) {
			n++
		}
	}
	return n
}

// TestClosingADialogTwiceIsHarmless checks the guard that stops a
// re-entrant close from tearing down the rest of the stack.
func TestClosingADialogTwiceIsHarmless(t *testing.T) {
	a := newTestApp(t, 40, 20)
	withMenubar(t, a)
	if err := a.openMenu(); err != nil {
		t.Fatalf("open menu: %v", err)
	}
	gone := a.modals[0]
	if err := a.openPalette(); err != nil {
		t.Fatalf("open palette: %v", err)
	}
	a.hideModal(gone)

	// The menu is long gone, along with the palette that was over it.
	a.hideModal(gone)

	if got := len(a.modals); got != 0 {
		t.Errorf("%d dialogs left, want none", got)
	}
	if got := len(a.comp.Layers()); got != 1 {
		t.Errorf("%d layers, want just the tree", got)
	}
}

// TestClosingADialogThatIsNotOpenLeavesTheStackAlone checks the guard
// from the other side: a dialog that was never on the stack must not
// take the ones that are with it.
func TestClosingADialogThatIsNotOpenLeavesTheStackAlone(t *testing.T) {
	a := newTestApp(t, 40, 20)
	withMenubar(t, a)
	if err := a.openMenu(); err != nil {
		t.Fatalf("open menu: %v", err)
	}
	if err := a.openPalette(); err != nil {
		t.Fatalf("open palette: %v", err)
	}

	a.hideModal(&modal{})

	if got := len(a.modals); got != 2 {
		t.Errorf("%d dialogs left, want the two that are open", got)
	}
	if a.palette == nil {
		t.Error("the palette was taken away by a dialog that was never open")
	}
	if a.root.Modal() != ui.Widget(a.palette) {
		t.Errorf("top modal = %v, want the palette", a.root.Modal())
	}
}

// TestADialogTakenFromUnderneathIsToldSo checks the wiring that is not
// reached by closing a dialog through its own close function. Without
// it, the menu bar goes on marking a title as open with no menu under
// it, and the title can never be opened again.
func TestADialogTakenFromUnderneathIsToldSo(t *testing.T) {
	a := newTestApp(t, 40, 20)
	bar := withMenubar(t, a)
	if err := a.openMenu(); err != nil {
		t.Fatalf("open menu: %v", err)
	}
	menu := a.modals[0]
	if err := a.openPalette(); err != nil {
		t.Fatalf("open palette: %v", err)
	}

	// Take the menu away from under the palette, without going through
	// the menu bar.
	a.hideModal(menu)

	if bar.OpenIndex() != -1 {
		t.Errorf("the bar still marks menu %d as open", bar.OpenIndex())
	}
	if a.palette != nil {
		t.Error("the palette went with it but was not told")
	}
	// And the bar can open a menu again afterwards.
	if err := a.openMenu(); err != nil {
		t.Fatalf("open menu again: %v", err)
	}
	if bar.OpenIndex() != 0 {
		t.Errorf("open = %d, want the bar working again", bar.OpenIndex())
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
	if got := len(a.comp.Layers()); got != 1 {
		t.Errorf("%d layers left, want just the tree", got)
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

// A notice posted while a menu is down outlives the next rebuild of that
// menu. The server list is rebuilt whenever a connection is made or
// lost, and rebuilding closes the drop-down along with everything
// stacked over it.
func TestANoticeSurvivesTheServerMenuBeingRebuilt(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	bar := withMenubar(t, a)
	a.refreshServers()
	at := -1
	for i, def := range bar.Menus {
		if def.Title == "Servers" {
			at = i
		}
	}
	if at < 0 {
		t.Fatal("there is no Servers menu to drop down")
	}
	if !bar.Open(at) {
		t.Fatal("the Servers menu would not open")
	}

	a.reportError("Could not open it", errors.New("the machine went away"))
	n := awaitModal[*ui.Notice](t, a, "a notice", nil)

	// A saved machine appears, so the menu really is rebuilt rather than
	// left as it was.
	if err := a.book.Put(remote.Host{Name: "margit", Address: "margit.skalarit.net"}, ""); err != nil {
		t.Fatalf("saving a server: %v", err)
	}
	a.refreshServers()

	if got := a.root.Modal(); got != ui.Widget(n) {
		t.Fatalf("the top dialog is %T, want the notice still", got)
	}
	if !menuNames(a).has("Connect to margit") {
		t.Error("the menu was not rebuilt, so nothing was tested")
	}
}
