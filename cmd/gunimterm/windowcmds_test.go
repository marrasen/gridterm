package main

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/marrasen/gunim"
	gi "github.com/marrasen/gunim/input"

	"github.com/marrasen/gridterm/input"
	"github.com/marrasen/gridterm/internal/update"
	"github.com/marrasen/gridterm/keys"
	"github.com/marrasen/gridterm/remote"
	"github.com/marrasen/gridterm/settings"
	shellfind "github.com/marrasen/gridterm/shells"
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

// Every command gridterm has is one this window knows, by the same
// name, so a shortcuts file written for gridterm works here: a file
// naming one command this window lacks is not used at all.
func TestEveryGridtermCommandIsKnownHere(t *testing.T) {
	root, err := filepath.Glob("../../*.go")
	if err != nil || len(root) == 0 {
		t.Fatalf("gridterm's source: %v, %v", root, err)
	}
	var src strings.Builder
	for _, f := range root {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		src.Write(b)
	}
	consts := map[string]string{}
	for _, m := range regexp.MustCompile(`\b(\w+)\s*=\s*"([a-z]+\.[A-Za-z.]+)"`).FindAllStringSubmatch(src.String(), -1) {
		consts[m[1]] = m[2]
	}
	known := map[string]bool{}
	for _, c := range everyCommand() {
		known[c[0]] = true
	}
	for _, b := range shortcuts().Bindings() {
		known[b.ID] = true
	}
	for alias := range aliases {
		known[alias] = true
	}
	ids := regexp.MustCompile(`ui\.Command\{\s*ID:\s*("[^"]+"|\w+)`).FindAllStringSubmatch(src.String(), -1)
	if len(ids) < 50 {
		t.Fatalf("found %d of gridterm's commands; the pattern has stopped matching", len(ids))
	}
	// Ids gridterm builds with a function, by the prefix they start
	// with.
	built := map[string]string{
		"folderCommandID": "conn.files.", "savedCommandID": "conn.saved.",
		"savedTunnelID": "conn.savedtunnel.", "fontCommandID": "font.use.",
		"ids": shellfind.CommandPrefix,
	}
	// Listed under "Not ported yet" in the README.
	notYet := map[string]bool{"font.use.": true}
	for _, m := range ids {
		id := strings.Trim(m[1], `"`)
		if c, ok := consts[m[1]]; ok {
			id = c
		}
		if p, ok := built[m[1]]; ok {
			id = p
		}
		switch {
		case notYet[id]:
		case strings.HasSuffix(id, "."):
			if !slices.Contains(itemPrefixes, id) {
				t.Errorf("gridterm's commands starting %s are unknown here", id)
			}
		case !known[id]:
			t.Errorf("gridterm's %s is unknown here", id)
		}
	}
}

// A shortcuts file written for gridterm, naming its commands, its
// commands on one thing of many and its other names, is taken whole.
func TestAShortcutsFileForGridtermIsTaken(t *testing.T) {
	win, _, publish := windowStage(t)
	change := func(k input.Key, id string) keys.Change {
		return keys.Change{Chord: ui.Chord{Key: k, Mods: input.ModCtrl | input.ModAlt}, Command: id, Written: "ctrl+alt+" + id}
	}
	publish(State{Shortcuts: []keys.Change{
		change(input.KeyA, "view.switcher"),
		change(input.KeyB, "pane.open"),
		change(input.KeyC, "server.open.my-desk"),
		change(input.KeyD, "conn.files.my-desk.1"),
		change(input.KeyE, "secrets.forget"),
	}, ShortcutsRead: 1})
	if id, _ := win.keys.Lookup(ui.Chord{Key: input.KeyB, Mods: input.ModCtrl | input.ModAlt}); id != "conn.terminal" {
		t.Fatalf("gridterm's pane.open is bound to %q, want New Terminal", id)
	}
	if id, _ := win.keys.Lookup(ui.Chord{Key: input.KeyC, Mods: input.ModCtrl | input.ModAlt}); id != "server.open.my-desk" {
		t.Fatalf("a saved server's command is bound to %q", id)
	}
}

func TestCommandsOnOneThingFindItByGridtermsName(t *testing.T) {
	win, _, publish := windowStage(t)
	publish(State{Saved: []remote.Host{{Name: "My Desk", Address: "desk", Folders: []string{"/srv/www"}}}, Connected: []string{"My Desk"}})
	for len(lastWindow.Client().Intents()) > 0 {
		<-lastWindow.Client().Intents()
	}
	for _, c := range []struct {
		id   string
		want gunim.Intent
	}{
		{"server.open.my-desk", ConnectTo{Saved: "My Desk"}},
		{"conn.terminal.my-desk", OpenOn{Machine: "My Desk"}},
		{"conn.terminal.", OpenOn{Machine: ""}},
		{"conn.files.my-desk", FilesOn{Machine: "My Desk"}},
		{"conn.files.my-desk.1", FilesOn{Machine: "My Desk", Path: "/srv/www"}},
	} {
		if !win.run(c.id, lastUI) {
			t.Fatalf("%s was not taken", c.id)
		}
		if got := nextIntent(t); got != c.want {
			t.Errorf("%s sent %#v, want %#v", c.id, got, c.want)
		}
	}
}

// A command on the secrets that picks one first unlocks them, and
// carries on once they are open.
func TestChangeSecretUnlocksFirst(t *testing.T) {
	win, _, publish := windowStage(t)
	publish(State{Secrets: Secrets{Exists: true}})
	for len(lastWindow.Client().Intents()) > 0 {
		<-lastWindow.Client().Intents()
	}
	win.run("secrets.change", lastUI)
	if in := nextIntent(t); in != (UnlockSecrets{}) {
		t.Fatalf("with the secrets locked, it sent %#v", in)
	}
	publish(State{Secrets: Secrets{Exists: true, Open: true, Items: []SecretItem{{ID: "s1", Name: "db"}}}})
	if win.afterUnlock != "" {
		t.Fatal("the command was left waiting")
	}
	// The palette asks which; Enter takes the first, and the form opens.
	lastWindow.Input(gi.KeyPress{Key: gi.KeyEnter})
	for range 5 {
		lastWindow.Frame(time.Second / 60)
	}
	if win.dialog == nil {
		t.Fatal("picking the secret opened no form")
	}
}
