package main

import (
	"os"
	"testing"
	"time"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/geom"
	gi "github.com/marrasen/gunim/input"
	"github.com/marrasen/gunim/text"

	"github.com/marrasen/gridterm/settings"
	"github.com/marrasen/gridterm/themes"
	"github.com/marrasen/gridterm/vt"
)

func fontApp(t *testing.T) *app {
	t.Helper()
	w := gunim.NewOffscreen(geom.Sz(400, 300), nil)
	a := newApp(w.Client(), &shells{m: map[string]*shell{}})
	a.ctx = t.Context()
	set, err := settings.Load(t.TempDir() + "/settings.json")
	if err != nil {
		t.Fatal(err)
	}
	a.settings = set
	a.themes = loadThemes()
	return a
}

func TestTheFontMenuPicksAFace(t *testing.T) {
	a := fontApp(t)
	a.handle(PickFont{Name: dosFamily})
	if f := a.st.Font; f.Name != dosFamily || f.Faces[0] == nil {
		t.Fatalf("picked the DOS face, the font is %q with %v", f.Name, f.Faces)
	}
	a.handle(PickFont{Name: bundledFamily})
	if f := a.st.Font; f.Name != "" || f.Faces != [4]*text.Face{} {
		t.Fatalf("picked Go Mono, the font is %q", f.Name)
	}
	a.handle(PickFont{Name: "No Such Mono"})
	if len(a.st.Notices) != 1 {
		t.Fatalf("picked a face that is not here, the notices are %+v", a.st.Notices)
	}
}

func TestAThemeNamingAFaceDrawsInItUnlessOneWasPicked(t *testing.T) {
	a := fontApp(t)
	dos := ""
	for _, th := range a.themes {
		if th.source.Font == dosFamily {
			dos = th.name
		}
	}
	if dos == "" {
		t.Fatal("no theme names the DOS face")
	}
	a.pickTheme(dos)
	if a.st.Font.Name != dosFamily {
		t.Fatalf("the theme %s names the DOS face, the font is %q", dos, a.st.Font.Name)
	}
	a.handle(PickFont{Name: bundledFamily})
	a.pickTheme(dos)
	if a.st.Font.Name != "" {
		t.Fatalf("with Go Mono picked, taking the theme again drew in %q", a.st.Font.Name)
	}
}

func TestTheFontSizeIsKept(t *testing.T) {
	a := fontApp(t)
	a.handle(FontSize{Step: 2})
	if size, ok := a.settings.FontSize(); !ok || float32(size) != defaultFontSize+2 {
		t.Fatalf("kept %v, %v", size, ok)
	}
}

func TestTheWindowDrawsInTheFontAndZoomsWithCtrlAndTheWheel(t *testing.T) {
	win, sh, publish := windowStage(t)
	quiet := shellHooks{output: func() {}, title: func(string) {}, exit: func() {}, clipboard: func(string) {}}
	sh.set("p1", openShell(&typed{done: make(chan struct{})}, vt.DefaultPalette(), quiet))
	t.Cleanup(func() { _ = sh.get("p1").t.Close() })
	a := fontApp(t)
	if err := a.pickFont(dosFamily); err != nil {
		t.Fatal(err)
	}
	st := State{Panes: []Pane{{ID: "p1", Title: "Terminal 1"}}, Stage: &Box{Pane: "p1"}, Focus: "p1", Fonts: a.st.Fonts, Font: a.st.Font, FontSize: defaultFontSize}
	publish(st)
	if win.terms["p1"].cells.Faces != a.st.Font.Faces {
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

func TestANewWindowOpensOnATerminalOf80By30(t *testing.T) {
	win, sh, publish := windowStageOf(t, firstSize(defaultFontSize, 220))
	quiet := shellHooks{output: func() {}, title: func(string) {}, exit: func() {}, clipboard: func(string) {}}
	sh.set("p1", openShell(&typed{done: make(chan struct{})}, vt.DefaultPalette(), quiet))
	t.Cleanup(func() { _ = sh.get("p1").t.Close() })
	st := State{Panes: []Pane{{ID: "p1", Title: "Terminal 1"}}, Stage: &Box{Pane: "p1"}, Focus: "p1", Sidebar: true, SidebarWidth: 220, FontSize: defaultFontSize, PaneTitles: true}
	for range 10 {
		publish(st)
	}
	if cols, rows := win.terms["p1"].cells.Fit(); cols != openCols || rows != openRows {
		t.Fatalf("the window opens on a terminal of %d by %d", cols, rows)
	}
}

// Picking another theme lets its face win again over one picked by
// hand; taking the same theme again leaves the hand's pick.
func TestAnotherThemesFaceWinsOverOnePickedBefore(t *testing.T) {
	a := fontApp(t)
	dos, plain := "", ""
	for _, th := range a.themes {
		if th.source.Font == dosFamily {
			dos = th.name
		} else if plain == "" {
			plain = th.name
		}
	}
	a.pickTheme(plain)
	a.handle(PickFont{Name: bundledFamily})
	a.pickTheme(plain)
	if a.st.Font.Name != "" {
		t.Fatalf("the same theme again moved the face to %q", a.st.Font.Name)
	}
	a.pickTheme(dos)
	if a.st.Font.Name != dosFamily {
		t.Fatalf("another theme naming a face left it at %q", a.st.Font.Name)
	}
	a.handle(PickTheme{Name: "No Such Theme"})
	if got, _ := a.settings.Theme(); got == "No Such Theme" {
		t.Fatal("a theme not in the list was kept")
	}
}

func TestAThemesFileThatCannotBeReadIsSaid(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir, err := settings.Dir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(themes.Path(dir), []byte("{ not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	all, err := loadThemesSaying()
	if err == nil {
		t.Fatal("a broken themes file said nothing")
	}
	if len(all) != len(themes.Built()) {
		t.Fatalf("with the file broken, %d themes are left, want the %d built in", len(all), len(themes.Built()))
	}
}
