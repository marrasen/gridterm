package main

import (
	"image/color"
	"os"
	"path/filepath"
	"testing"

	"github.com/marrasen/gunim/theme"
	"github.com/marrasen/gunim/widget"

	"github.com/marrasen/gridterm/themes"
)

// A theme with no Echo block takes the echo's colours from its palette,
// and one with a block takes what it names, and its strength.
func TestAThemeSetsTheEcho(t *testing.T) {
	base := themes.Built()[0]
	th, err := themeOf(base)
	if err != nil {
		t.Fatal(err)
	}
	pal, err := base.Palette()
	if err != nil {
		t.Fatal(err)
	}
	live := theme.NewLive(th.theme)
	if got, want := widget.EchoProblem.Get(live), nrgba(pal.ANSI[9]); got != want {
		t.Fatalf("with no block, a problem echoes in %v, want bright red %v", got, want)
	}

	half := 0.5
	base.Echo = &themes.Echo{Done: "#123456", Strength: &half}
	th, err = themeOf(base)
	if err != nil {
		t.Fatal(err)
	}
	live = theme.NewLive(th.theme)
	if got, want := widget.EchoDone.Get(live), (color.NRGBA{R: 0x12, G: 0x34, B: 0x56, A: 0xff}); got != want {
		t.Fatalf("the block's Done reads %v, want %v", got, want)
	}
	if got := widget.EchoStrength.Get(live); got != 0.5 {
		t.Fatalf("the block's Strength reads %v, want 0.5", got)
	}
	if got, want := widget.EchoCall.Get(live), nrgba(pal.ANSI[11]); got != want {
		t.Fatalf("with Call left out, a bell echoes in %v, want bright yellow %v", got, want)
	}

	base.Echo = &themes.Echo{Wait: "grey-ish"}
	if _, err := themeOf(base); err == nil {
		t.Fatal("a colour that reads as none was taken")
	}
	less := -1.0
	base.Echo = &themes.Echo{Strength: &less}
	if _, err := themeOf(base); err == nil {
		t.Fatal("a strength below 0 was taken")
	}
}

// The themes file carries the Echo block.
func TestTheThemesFileReadsTheEchoBlock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "themes.json")
	b := themes.Built()[0]
	raw := `{"version": 1, "themes": [{"name": "Echoing", "fg": "` + b.FG + `", "bg": "` + b.BG + `", "ansi": [`
	for i, c := range b.ANSI {
		if i > 0 {
			raw += ","
		}
		raw += `"` + c + `"`
	}
	raw += `], "Echo": {"Problem": "#ff0000", "Strength": 0}}]}`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	all, err := themes.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	th, ok := themes.Named(all, "Echoing")
	if !ok || th.Echo == nil || th.Echo.Problem != "#ff0000" || th.Echo.Strength == nil || *th.Echo.Strength != 0 {
		t.Fatalf("the file's Echo block reads %+v", th.Echo)
	}
}
