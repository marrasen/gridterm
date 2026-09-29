package view

import (
	"errors"
	"strings"
	"testing"
	"time"

	gi "github.com/marrasen/gunim/input"
	"github.com/marrasen/gunim/widget"

	"github.com/marrasen/kakel/app"
	"github.com/marrasen/kakel/internal/sessiontest"
	"github.com/marrasen/kakel/screen"
	"github.com/marrasen/kakel/vt"
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

// On "Replace notes.txt?", Tab then Enter leaves the file, as Enter
// alone does: Tab reaches Leave It before Replace.
func TestTabThenEnterLeavesTheFile(t *testing.T) {
	_, _, publish := windowStage(t)
	publish(app.State{Asks: []app.Ask{{ID: 1, Title: "Replace notes.txt?", Text: "x", Choose: []string{"Leave It", "Replace"}, Also: "Do the same for the rest", No: "Stop"}}})
	for range 5 {
		lastWindow.Frame(time.Second / 60)
	}
	for len(lastWindow.Client().Intents()) > 0 {
		<-lastWindow.Client().Intents()
	}
	lastWindow.Input(gi.KeyPress{Key: gi.KeyTab, Time: time.Now()})
	lastWindow.Frame(time.Second / 60)
	lastWindow.Input(gi.KeyPress{Key: gi.KeyEnter, Time: time.Now()})
	lastWindow.Frame(time.Second / 60)
	in, ok := nextIntent(t).(app.AskAnswered)
	if !ok || len(in.Answers) == 0 || in.Answers[0] != "Leave It" {
		t.Fatalf("Tab then Enter sent %#v", in)
	}
}

// The palette's shortcut closes it again, from inside it.
func TestThePaletteShortcutClosesItAgain(t *testing.T) {
	win, _, publish := windowStage(t)
	publish(app.State{Panes: []app.Pane{{ID: "p1", Title: "a", Kind: app.KindFiles}}, Stage: &app.Box{Pane: "p1"}, Focus: "p1",
		Browsers: map[string]app.Browser{"p1": {Path: "/", Seq: 1}}})
	press := func() {
		lastWindow.Input(gi.KeyPress{Key: gi.KeyK, Mods: gi.ModControl | gi.ModShift, Time: time.Now()})
		for range 3 {
			lastWindow.Frame(time.Second / 60)
		}
	}
	press()
	if !win.palette.IsOpen() {
		t.Fatal("the palette's shortcut did not open it")
	}
	press()
	if win.palette.IsOpen() {
		t.Fatal("the palette's shortcut again left it open")
	}
}

// The palette ticks a switch while it is on, as the View menu does.
func TestThePaletteTicksASwitchThatIsOn(t *testing.T) {
	win, _, publish := windowStage(t)
	ticked := func() bool {
		for i, id := range win.paletteIDs {
			if id == "sidebar.toggle" {
				return win.palette.Items[i].Checked
			}
		}
		t.Fatal("no Show Sidebar in the palette")
		return false
	}
	publish(app.State{Sidebar: true, SidebarWidth: 220})
	if !ticked() {
		t.Fatal("with the sidebar showing, the palette leaves it unticked")
	}
	publish(app.State{Sidebar: false, SidebarWidth: 220})
	if ticked() {
		t.Fatal("with the sidebar hidden, the palette ticks it")
	}
}

// A paste that finds the clipboard cannot be read says so, rather than
// taking it for a clipboard with nothing on it.
func TestAnUnreadableClipboardIsSaid(t *testing.T) {
	win, sh, publish := windowStage(t)
	quiet := screen.Hooks{Output: func() {}, Title: func(string) {}, Exit: func() {}, Clipboard: func(string) {}}
	sh.Set("p1", screen.Open(sessiontest.New(), vt.DefaultPalette(), quiet))
	t.Cleanup(func() { _ = sh.Get("p1").T.Close() })
	publish(app.State{Panes: []app.Pane{{ID: "p1", Title: "Terminal 1"}}, Stage: &app.Box{Pane: "p1"}, Focus: "p1"})
	lastWindow.Offscreen().SetClipboardError(errors.New("no display"))
	for len(lastWindow.Client().Intents()) > 0 {
		<-lastWindow.Client().Intents()
	}
	win.terms["p1"].pasteClipboard(lastUI)
	if in, ok := nextIntent(t).(app.ClipboardUnreadable); !ok || in.Why != "no display" {
		t.Fatalf("the paste sent %#v", in)
	}
}

// A question with preformatted text keeps its lines whole, in the
// fixed-width face, and lets them be selected.
func TestAPreformattedQuestionKeepsItsLinesWhole(t *testing.T) {
	win, _, publish := windowStage(t)
	publish(app.State{Asks: []app.Ask{{ID: 1, Title: "api-key", Text: "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGx0 me@desk", Preformatted: true, Yes: "Done"}}})
	var note *widget.Label
	for _, f := range win.dialog.Body.(*widget.Form).Children() {
		if l, ok := f.(*widget.Label); ok && strings.HasPrefix(l.Text, "ssh-ed25519") {
			note = l
		}
	}
	if note == nil || !note.NoWrap || !note.Selectable || note.Face.Key() != widget.MonoFont.Key() {
		t.Fatalf("the text is shown as %+v", note)
	}
}
