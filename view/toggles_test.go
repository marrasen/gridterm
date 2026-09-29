package view

import (
	"testing"
	"time"

	gi "github.com/marrasen/gunim/input"
	"github.com/marrasen/gunim/widget"

	"github.com/marrasen/kakel/app"
)

// The switcher's shortcut closes it again.
func TestTheSwitcherShortcutClosesItAgain(t *testing.T) {
	win, _, publish := windowStage(t)
	publish(app.State{Panes: []app.Pane{{ID: "p1", Title: "a", Kind: app.KindFiles}}, Stage: &app.Box{Pane: "p1"}, Focus: "p1",
		Browsers: map[string]app.Browser{"p1": {Path: "/", Seq: 1}}})
	press := func(k gi.Key) {
		lastWindow.Input(gi.KeyPress{Key: k, Mods: gi.ModControl | gi.ModShift, Time: time.Now()})
		for range 3 {
			lastWindow.Frame(time.Second / 60)
		}
	}
	press(gi.KeyA)
	if win.sw == nil {
		t.Fatal("the switcher's shortcut did not open it")
	}
	// Held down, the key repeats, and the switcher stays.
	lastWindow.Input(gi.KeyPress{Key: gi.KeyA, Mods: gi.ModControl | gi.ModShift, Repeat: true, Time: time.Now()})
	lastWindow.Frame(time.Second / 60)
	if win.sw == nil {
		t.Fatal("the key repeating closed the switcher")
	}
	press(gi.KeyA)
	if win.sw != nil {
		t.Fatal("the switcher's shortcut again left it open")
	}
}

// In full screen, the sidebar's shortcut brings the sidebar over the
// stage and takes it away again, and the window stays full screen.
func TestTheSidebarComesBackInFullScreen(t *testing.T) {
	win, _, publish := windowStage(t)
	publish(app.State{Sidebar: false, SidebarWidth: 220, Panes: []app.Pane{{ID: "p1", Title: "a", Kind: app.KindFiles}}, Stage: &app.Box{Pane: "p1"}, Focus: "p1",
		Browsers: map[string]app.Browser{"p1": {Path: "/", Seq: 1}}})
	win.present(true, lastUI)
	win.run("sidebar.toggle", lastUI)
	if !win.presenting || win.outer.Share() != 220 {
		t.Fatalf("in full screen, the sidebar is %v wide, presenting %v", win.outer.Share(), win.presenting)
	}
	win.run("sidebar.toggle", lastUI)
	if !win.presenting || win.outer.Share() != 0 {
		t.Fatalf("toggled again, the sidebar is %v wide", win.outer.Share())
	}
	// Out of full screen, it is the program's to show or hide.
	win.present(false, lastUI)
	for len(lastWindow.Client().Intents()) > 0 {
		<-lastWindow.Client().Intents()
	}
	win.run("sidebar.toggle", lastUI)
	if in := nextIntent(t); in != (app.ToggleSidebar{}) {
		t.Fatalf("out of full screen, the shortcut sent %#v", in)
	}
}

// Close Selected Row with no row, or one that cannot be closed, says
// so rather than doing nothing.
func TestCloseSelectedRowSaysWhyItDidNothing(t *testing.T) {
	win, _, publish := windowStage(t)
	publish(app.State{Sidebar: true, SidebarWidth: 220, Panes: []app.Pane{{ID: "p1", Title: "a", Kind: app.KindFiles}}, Stage: &app.Box{Pane: "p1"}, Focus: "p1",
		Browsers: map[string]app.Browser{"p1": {Path: "/", Seq: 1}}})
	was := win.toasts.Len()
	win.run("sidebar.closeRow", lastUI)
	if win.toasts.Len() != was+1 {
		t.Fatal("with no row selected, nothing was said")
	}
	row, ok := widget.RowOf[*sideRow](win.list, widget.Key("machine:"))
	if !ok {
		t.Fatalf("no heading for this computer in %v", win.list.Keys())
	}
	lastUI.Focus(row)
	if lastUI.Focused() != row {
		t.Fatal("the heading took no focus")
	}
	win.run("sidebar.closeRow", lastUI)
	if win.toasts.Len() != was+2 {
		t.Fatal("on a heading, nothing was said")
	}
}
