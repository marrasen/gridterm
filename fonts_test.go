package main

import (
	"os"
	"testing"

	"github.com/marrasen/kakel/screen"

	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/gunimtest"
	"github.com/marrasen/gunim/text"

	"github.com/marrasen/kakel/internal/testhome"
	"github.com/marrasen/kakel/settings"
	"github.com/marrasen/kakel/themes"
)

func fontApp(t *testing.T) *app {
	t.Helper()
	w := gunimtest.New(t, geom.Sz(400, 300), nil)
	a := newApp(w.Client(), screen.NewShells())
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
	testhome.New(t)
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
