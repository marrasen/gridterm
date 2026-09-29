package app

import (
	"os"
	"testing"

	"github.com/marrasen/kakel/look"

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
	a.themes = look.Load()
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

// A theme leaves the face alone: the one picked stays through every
// theme, previews included.
func TestThemesLeaveTheFontAlone(t *testing.T) {
	a := fontApp(t)
	a.handle(PickFont{Name: dosFamily})
	for _, th := range a.themes {
		a.handle(PreviewTheme{Name: th.Name})
		a.handle(PickTheme{Name: th.Name})
		if a.st.Font.Name != dosFamily {
			t.Fatalf("the theme %s moved the face to %q", th.Name, a.st.Font.Name)
		}
	}
	a.handle(PickTheme{Name: "No Such Theme"})
	if got, _ := a.settings.Theme(); got == "No Such Theme" {
		t.Fatal("a theme not in the list was kept")
	}
}

// The face picked from the Font menu is kept, and the next start draws
// in it: a compiled-in face at once, one on disk once the fonts are
// found, and one no longer here is let go.
func TestTheFontPickedIsKept(t *testing.T) {
	a := fontApp(t)
	a.handle(PickFont{Name: dosFamily})
	if got, ok := a.settings.FontFamily(); !ok || got != dosFamily {
		t.Fatalf("kept %q, %v", got, ok)
	}
	next := fontApp(t)
	next.keptFont, _ = a.settings.FontFamily()
	next.useKeptFont(false)
	if next.st.Font.Name != dosFamily || next.keptFont != "" {
		t.Fatalf("the next start is in %q, still to take %q", next.st.Font.Name, next.keptFont)
	}
	gone := fontApp(t)
	gone.keptFont = "No Such Mono"
	gone.useKeptFont(false)
	if gone.keptFont == "" {
		t.Fatal("a face on disk was given up on before the fonts were found")
	}
	gone.useKeptFont(true)
	if gone.st.Font.Name != "" || gone.keptFont != "" || len(gone.st.Notices) != 0 {
		t.Fatalf("a face no longer here left the font %q, %q to take, notices %+v", gone.st.Font.Name, gone.keptFont, gone.st.Notices)
	}
}

func TestTheFontSizeIsKept(t *testing.T) {
	a := fontApp(t)
	a.handle(FontSize{Step: 2})
	if size, ok := a.settings.FontSize(); !ok || float32(size) != defaultFontSize+2 {
		t.Fatalf("kept %v, %v", size, ok)
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
	all, err := look.LoadSaying()
	if err == nil {
		t.Fatal("a broken themes file said nothing")
	}
	if len(all) != len(themes.Built()) {
		t.Fatalf("with the file broken, %d themes are left, want the %d built in", len(all), len(themes.Built()))
	}
}

// The Font menu lists the faces compiled in first, Go Mono and then the
// DOS face, at the size a new window opens with; the window ticks them
// by that order.
func TestTheFontsStartWithTheFacesCompiledIn(t *testing.T) {
	a := fontApp(t)
	if len(a.st.Fonts) < 2 || a.st.Fonts[0] != "Go Mono (bundled)" || a.st.Fonts[1] != "PxPlus IBM VGA8" || a.st.FontSize != 15 {
		t.Fatalf("the fonts are %q at %v", a.st.Fonts, a.st.FontSize)
	}
}
