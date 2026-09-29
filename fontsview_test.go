package main

import (
	"testing"
	"time"

	"github.com/marrasen/kakel/app"

	"github.com/marrasen/kakel/internal/sessiontest"
	"github.com/marrasen/kakel/internal/testhome"
	"github.com/marrasen/kakel/screen"

	"github.com/marrasen/gunim/geom"
	gi "github.com/marrasen/gunim/input"
	"github.com/marrasen/gunim/text"
	"github.com/marrasen/kakel/fonts"
	"github.com/marrasen/kakel/vt"
)

func TestANewWindowOpensOnATerminalOf80By30(t *testing.T) {
	testhome.New(t)
	opts, err := app.ParseOptions(nil)
	if err != nil {
		t.Fatal(err)
	}
	win, sh, publish := windowStageOf(t, opts.WindowSize())
	quiet := screen.Hooks{Output: func() {}, Title: func(string) {}, Exit: func() {}, Clipboard: func(string) {}}
	sh.Set("p1", screen.Open(sessiontest.New(), vt.DefaultPalette(), quiet))
	t.Cleanup(func() { _ = sh.Get("p1").T.Close() })
	st := app.State{Panes: []app.Pane{{ID: "p1", Title: "Terminal 1"}}, Stage: &app.Box{Pane: "p1"}, Focus: "p1", Sidebar: true, SidebarWidth: 220, FontSize: defaultSize, PaneTitles: true}
	for range 10 {
		publish(st)
	}
	if cols, rows := win.terms["p1"].cells.Fit(); cols != 80 || rows != 30 {
		t.Fatalf("the window opens on a terminal of %d by %d", cols, rows)
	}
}

func TestTheWindowDrawsInTheFontAndZoomsWithCtrlAndTheWheel(t *testing.T) {
	win, sh, publish := windowStage(t)
	quiet := screen.Hooks{Output: func() {}, Title: func(string) {}, Exit: func() {}, Clipboard: func(string) {}}
	sh.Set("p1", screen.Open(sessiontest.New(), vt.DefaultPalette(), quiet))
	t.Cleanup(func() { _ = sh.Get("p1").T.Close() })
	face, err := text.Parse(fonts.DOS)
	if err != nil {
		t.Fatal(err)
	}
	font := app.Font{Name: dosFamily, Faces: [4]*text.Face{face}}
	st := app.State{Panes: []app.Pane{{ID: "p1", Title: "Terminal 1"}}, Stage: &app.Box{Pane: "p1"}, Focus: "p1", Fonts: []string{bundledFamily, dosFamily}, Font: font, FontSize: defaultSize}
	publish(st)
	if win.terms["p1"].cells.Faces != font.Faces {
		t.Fatal("the terminal is not drawn in the font picked")
	}
	for i, m := range win.bar.Menus {
		if m.Title != "Font" {
			continue
		}
		if len(m.Checked) < 2 || !m.Checked[1] || m.Checked[0] {
			t.Fatalf("the Font menu ticks %v", win.bar.Menus[i].Checked)
		}
	}
	for len(lastWindow.Client().Intents()) > 0 {
		<-lastWindow.Client().Intents()
	}
	box, _ := lastUI.Bounds(win.terms["p1"])
	lastWindow.Input(gi.Scroll{Pos: box.Center(), Delta: geom.Pt(0, zoomNotch), Mods: gi.ModControl})
	lastWindow.Frame(time.Second / 60)
	if in := nextIntent(t); in != (app.FontSize{Step: 1}) {
		t.Fatalf("Ctrl and a notch of the wheel sent %#v", in)
	}
	if !win.run("font.use.bundled", lastUI) {
		t.Fatal("font.use.bundled was not taken")
	}
	if in := nextIntent(t); in != (app.PickFont{Name: bundledFamily}) {
		t.Fatalf("font.use.bundled sent %#v", in)
	}
}

// What the program publishes, as a window first hears it: the default
// font size, and the faces compiled in, in the Font menu's order.
const (
	defaultSize   float32 = 15
	bundledFamily         = "Go Mono (bundled)"
	dosFamily             = "PxPlus IBM VGA8"
)
