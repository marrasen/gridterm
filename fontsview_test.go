package main

import (
	"testing"
	"time"

	"github.com/marrasen/kakel/screen"

	"github.com/marrasen/gunim/geom"
	gi "github.com/marrasen/gunim/input"
	"github.com/marrasen/gunim/text"
	"github.com/marrasen/kakel/fonts"
	"github.com/marrasen/kakel/vt"
)

func TestANewWindowOpensOnATerminalOf80By30(t *testing.T) {
	win, sh, publish := windowStageOf(t, firstSize(defaultFontSize, 220))
	quiet := screen.Hooks{Output: func() {}, Title: func(string) {}, Exit: func() {}, Clipboard: func(string) {}}
	sh.Set("p1", screen.Open(&typed{done: make(chan struct{})}, vt.DefaultPalette(), quiet))
	t.Cleanup(func() { _ = sh.Get("p1").T.Close() })
	st := State{Panes: []Pane{{ID: "p1", Title: "Terminal 1"}}, Stage: &Box{Pane: "p1"}, Focus: "p1", Sidebar: true, SidebarWidth: 220, FontSize: defaultFontSize, PaneTitles: true}
	for range 10 {
		publish(st)
	}
	if cols, rows := win.terms["p1"].cells.Fit(); cols != openCols || rows != openRows {
		t.Fatalf("the window opens on a terminal of %d by %d", cols, rows)
	}
}

func TestTheWindowDrawsInTheFontAndZoomsWithCtrlAndTheWheel(t *testing.T) {
	win, sh, publish := windowStage(t)
	quiet := screen.Hooks{Output: func() {}, Title: func(string) {}, Exit: func() {}, Clipboard: func(string) {}}
	sh.Set("p1", screen.Open(&typed{done: make(chan struct{})}, vt.DefaultPalette(), quiet))
	t.Cleanup(func() { _ = sh.Get("p1").T.Close() })
	face, err := text.Parse(fonts.DOS)
	if err != nil {
		t.Fatal(err)
	}
	font := Font{Name: dosFamily, Faces: [4]*text.Face{face}}
	st := State{Panes: []Pane{{ID: "p1", Title: "Terminal 1"}}, Stage: &Box{Pane: "p1"}, Focus: "p1", Fonts: []string{bundledFamily, dosFamily}, Font: font, FontSize: defaultFontSize}
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
	if in := nextIntent(t); in != (FontSize{Step: 1}) {
		t.Fatalf("Ctrl and a notch of the wheel sent %#v", in)
	}
	if !win.run("font.use.bundled", lastUI) {
		t.Fatal("font.use.bundled was not taken")
	}
	if in := nextIntent(t); in != (PickFont{Name: bundledFamily}) {
		t.Fatalf("font.use.bundled sent %#v", in)
	}
}
