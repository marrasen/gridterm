package main

import (
	"context"
	"os"
	"strings"
	"testing"

	gi "github.com/marrasen/gunim/input"

	"github.com/marrasen/gridterm/input"
	"github.com/marrasen/gridterm/internal/update"
	"github.com/marrasen/gridterm/keys"
	"github.com/marrasen/gridterm/settings"
	"github.com/marrasen/gridterm/themes"
	"github.com/marrasen/gridterm/ui"
)

func TestTheShortcutsFileMovesAKey(t *testing.T) {
	a, _ := agentApp(t)
	dir, err := settings.Dir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keys.Path(dir), []byte(`{"version":1,"keys":{"ctrl+shift+J":"pane.close","ctrl+shift+W":"nothing"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	a.handle(ReloadShortcuts{})
	if len(a.st.Shortcuts) != 2 || a.st.ShortcutsRead == 0 {
		t.Fatalf("read, the changes are %+v; notices %+v", a.st.Shortcuts, a.st.Notices)
	}

	win, _, publish := windowStage(t)
	publish(State{Shortcuts: a.st.Shortcuts, ShortcutsRead: 1})
	if id, _ := win.keys.Lookup(ui.Chord{Key: input.KeyJ, Mods: input.ModCtrl | input.ModShift}); id != "pane.close" {
		t.Fatalf("Ctrl+Shift+J runs %q", id)
	}
	if id, ok := win.keys.Lookup(ui.Chord{Key: input.KeyW, Mods: input.ModCtrl | input.ModShift}); ok {
		t.Fatalf("Ctrl+Shift+W still runs %q", id)
	}
	// One naming a command there is none of changes nothing.
	publish(State{Shortcuts: []keys.Change{{Chord: ui.Chord{Key: input.KeyK, Mods: input.ModCtrl}, Command: "no.such", Written: "ctrl+K"}}, ShortcutsRead: 2})
	if id, _ := win.keys.Lookup(ui.Chord{Key: input.KeyJ, Mods: input.ModCtrl | input.ModShift}); id != "pane.close" {
		t.Fatal("a file naming an unknown command changed the keys")
	}
	_ = gi.KeyA
}

func TestTheThemeFileIsWrittenAndReadAgain(t *testing.T) {
	a, _ := agentApp(t)
	a.themes = loadThemes()
	a.pickTheme(a.themes[0].name)
	registered := 0
	a.registerThemes = func(all []themed) { registered = len(all) }
	a.handle(WriteThemeFile{})
	dir, _ := settings.Dir()
	read, err := themes.Load(themes.Path(dir))
	if err != nil || len(read) == 0 {
		t.Fatalf("written, the file reads %d themes, %v", len(read), err)
	}
	a.handle(ReloadThemes{})
	if registered == 0 || len(a.st.Themes) == 0 || a.st.Contents == nil {
		t.Fatalf("read again, %d themes were registered, the state has %v", registered, a.st.Themes)
	}
}

func TestANewerReleaseIsOffered(t *testing.T) {
	a, _ := agentApp(t)
	was := latestRelease
	latestRelease = func(context.Context) (update.Release, error) {
		return update.Release{Version: "v99.0.0", Page: "https://example.com/release"}, nil
	}
	t.Cleanup(func() { latestRelease = was })
	a.handle(CheckUpdates{})
	waitFor(t, a, "the offer", func() bool { return len(a.st.Asks) > 0 })
	if q := a.st.Asks[0]; !strings.Contains(q.Text, "v99.0.0") || q.Yes != "Open the Page" {
		t.Fatalf("the offer is %+v", q)
	}
}

func TestTheHelpListsEveryCommandWithItsShortcut(t *testing.T) {
	win, _, publish := windowStage(t)
	publish(State{Panes: []Pane{{ID: "p1", Kind: kindHelp, Title: "Shortcuts and Commands"}}, Stage: &Box{Pane: "p1"}, Focus: "p1"})
	found := false
	for _, r := range win.help.rows {
		if r[2] == "pane.close" && r[1] == "Ctrl+Shift+W" {
			found = true
		}
	}
	if !found {
		t.Fatal("the help lacks Close Pane on Ctrl+Shift+W")
	}
}
