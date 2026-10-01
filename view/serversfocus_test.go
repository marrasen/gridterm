package view

import (
	"testing"

	"github.com/marrasen/kakel/app"

	"github.com/marrasen/gunim/geom"
	gi "github.com/marrasen/gunim/input"
)

// A click on the empty room under the Servers list leaves the window's
// shortcuts working.
func TestShortcutsWorkAfterAClickUnderTheServers(t *testing.T) {
	win, _, publish := windowStage(t)
	publish(withServers(app.State{Panes: []app.Pane{}, Focus: "ps"}))
	frames(30)
	box, ok := lastUI.Bounds(win.serversView)
	if !ok {
		t.Fatal("the Servers pane is not drawn")
	}
	at := geom.Pt(box.Center().X, box.Max.Y-20)
	lastWindow.Input(gi.PointerDown{Pos: at, Button: gi.ButtonPrimary, Clicks: 1})
	lastWindow.Input(gi.PointerUp{Pos: at, Button: gi.ButtonPrimary})
	frames(2)
	lastWindow.Input(gi.KeyPress{Key: gi.KeyK, Mods: gi.ModControl | gi.ModShift})
	frames(10)
	if !win.palette.IsOpen() {
		t.Fatalf("after a click under the list, Ctrl+Shift+K opened nothing; the keyboard is with %T", lastUI.Focused())
	}
}

// With nothing focused at all, the window's shortcuts still work.
func TestShortcutsWorkWithNothingFocused(t *testing.T) {
	win, _ := termStage(t, geom.Sz(900, 600))
	lastUI.Focus(nil)
	frames(2)
	lastWindow.Input(gi.KeyPress{Key: gi.KeyK, Mods: gi.ModControl | gi.ModShift})
	frames(10)
	if !win.palette.IsOpen() {
		t.Fatal("with nothing focused, Ctrl+Shift+K opened nothing")
	}
}
