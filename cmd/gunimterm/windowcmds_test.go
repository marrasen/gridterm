package main

import (
	"context"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/marrasen/gunim"
	gi "github.com/marrasen/gunim/input"
	"github.com/marrasen/gunim/widget"

	"github.com/marrasen/gridterm/conf"
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

	if !a.st.ShortcutsAgain {
		t.Fatal("read again, the state says it was read as the window opened")
	}

	win, _, publish := windowStage(t)
	publish(State{Shortcuts: a.st.Shortcuts, ShortcutsRead: 1, ShortcutsAgain: true})
	if id, _ := win.keys.Lookup(ui.Chord{Key: input.KeyJ, Mods: input.ModCtrl | input.ModShift}); id != "pane.close" {
		t.Fatalf("Ctrl+Shift+J runs %q", id)
	}
	if id, ok := win.keys.Lookup(ui.Chord{Key: input.KeyW, Mods: input.ModCtrl | input.ModShift}); ok {
		t.Fatalf("Ctrl+Shift+W still runs %q", id)
	}
	if n := win.toasts.Len(); n != 1 {
		t.Fatalf("taken, the window showed %d toasts, want the one saying so", n)
	}
	// One naming a command there is none of changes nothing, and says
	// that alone.
	publish(State{Shortcuts: []keys.Change{{Chord: ui.Chord{Key: input.KeyK, Mods: input.ModCtrl}, Command: "no.such", Written: "ctrl+K"}}, ShortcutsRead: 2, ShortcutsAgain: true})
	if id, _ := win.keys.Lookup(ui.Chord{Key: input.KeyJ, Mods: input.ModCtrl | input.ModShift}); id != "pane.close" {
		t.Fatal("a file naming an unknown command changed the keys")
	}
	if n := win.toasts.Len(); n != 2 {
		t.Fatalf("refused, the window has shown %d toasts, want one more, saying why", n)
	}
	// A command renamed since the file was written is followed.
	keys.Renamed["pane.shut"] = "pane.close"
	t.Cleanup(func() { delete(keys.Renamed, "pane.shut") })
	publish(State{Shortcuts: []keys.Change{{Chord: ui.Chord{Key: input.KeyK, Mods: input.ModCtrl | input.ModShift}, Command: "pane.shut", Written: "ctrl+shift+K"}}, ShortcutsRead: 3})
	if id, _ := win.keys.Lookup(ui.Chord{Key: input.KeyK, Mods: input.ModCtrl | input.ModShift}); id != "pane.close" {
		t.Fatalf("a renamed command's chord runs %q", id)
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
	was, wasVersion := latestRelease, thisVersion
	latestRelease = func(context.Context) (update.Release, error) {
		return update.Release{Version: "v99.0.0", Page: "https://example.com/release"}, nil
	}
	t.Cleanup(func() { latestRelease, thisVersion = was, wasVersion })
	for _, c := range []struct{ have, title string }{
		{"v1.0.0", "Update available"},
		{"v1.0.0-3-gabcdef-dirty", "Newest release"},
	} {
		thisVersion = func() string { return c.have }
		a.handle(CheckUpdates{})
		waitFor(t, a, "the offer", func() bool { return len(a.st.Asks) > 0 })
		q := a.st.Asks[0]
		if q.Title != c.title || !strings.Contains(q.Text, "v99.0.0") || !strings.Contains(q.Text, c.have) || q.Yes != "Open the Page" || !q.Careful {
			t.Fatalf("to %s, the offer is %+v", c.have, q)
		}
		a.handle(AskAnswered{ID: q.ID})
		waitFor(t, a, "the offer to close", func() bool { return len(a.st.Asks) == 0 })
	}
}

func TestASecondUpdateCheckWaitsForTheFirst(t *testing.T) {
	a, _ := agentApp(t)
	was := latestRelease
	var asked atomic.Int32
	answer := make(chan struct{})
	latestRelease = func(context.Context) (update.Release, error) {
		asked.Add(1)
		<-answer
		return update.Release{Version: "v0.0.1"}, nil
	}
	t.Cleanup(func() { latestRelease = was })
	a.handle(CheckUpdates{})
	a.handle(CheckUpdates{})
	close(answer)
	waitFor(t, a, "the answer", func() bool { return !a.checking })
	if n := asked.Load(); n != 1 {
		t.Fatalf("pressed twice, it asked %d times", n)
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
	// Under the menu it is on, and the file pane's keys from
	// gridterm's own list.
	var heads []string
	under := map[string]string{}
	keys := slices.Sorted(maps.Keys(win.help.rows))
	for _, k := range keys {
		r := win.help.rows[k]
		if win.help.heads[k] {
			heads = append(heads, r[0])
			continue
		}
		under[r[0]] = heads[len(heads)-1]
	}
	if heads[0] != "File" || under["Close Pane"] != "File" || !strings.HasPrefix(under["Tail"], "The file pane's keys") || !strings.HasPrefix(under["Hex"], "The reader's keys") {
		t.Fatalf("the help's groups are %v, with Close Pane under %q, Tail under %q, Hex under %q", heads, under["Close Pane"], under["Tail"], under["Hex"])
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
	for _, m := range ids {
		id := strings.Trim(m[1], `"`)
		if c, ok := consts[m[1]]; ok {
			id = c
		}
		if p, ok := built[m[1]]; ok {
			id = p
		}
		switch {
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

func TestClosingTheWindowAsksWhileAnythingIsOpen(t *testing.T) {
	a, _ := agentApp(t)
	a.handle(Exit{})
	waitFor(t, a, "the question", func() bool { return len(a.st.Asks) == 1 })
	q := a.st.Asks[0]
	if q.Text != "Still open: 1 pane and an agent share." || !q.Danger {
		t.Fatalf("asked %+v", q)
	}
	// A second close while it asks asks nothing more.
	a.handle(Exit{})
	for f := range len(a.events) {
		_ = f
		(<-a.events)()
	}
	if len(a.st.Asks) != 1 {
		t.Fatalf("closed twice, it asks %d questions", len(a.st.Asks))
	}
	a.handle(AskAnswered{ID: q.ID})
	waitFor(t, a, "the no", func() bool { return !a.leaving })
	if len(a.st.Panes) != 1 {
		t.Fatalf("told no, the window closed %d panes", 1-len(a.st.Panes))
	}
	a.handle(Exit{})
	waitFor(t, a, "the question again", func() bool { return len(a.st.Asks) == 1 })
	a.handle(AskAnswered{ID: a.st.Asks[0].ID, Yes: true})
	waitFor(t, a, "the window to close", func() bool { return len(a.st.Panes) == 0 })
}

// Editing a server keeps the key files after the first, which the form
// does not show, and refuses a name something is connected as.
func TestEditingAServerKeepsItsKeysAndRefusesATakenName(t *testing.T) {
	win, _, publish := windowStage(t)
	desk := remote.Host{ID: "d1", Name: "desk", Address: "desk.example", Identities: []string{"/k/one", "/k/two"}}
	publish(State{Saved: []remote.Host{desk}, Connected: []string{"laptop"}})
	for len(lastWindow.Client().Intents()) > 0 {
		<-lastWindow.Client().Intents()
	}
	win.serverForm(&desk, lastUI)
	for range 5 {
		lastWindow.Frame(time.Second / 60)
	}
	lastWindow.Input(gi.KeyPress{Key: gi.KeyEnter})
	lastWindow.Frame(time.Second / 60)
	for {
		if in, ok := nextIntent(t).(SaveServer); ok {
			if !slices.Equal(in.Host.Identities, []string{"/k/one", "/k/two"}) {
				t.Fatalf("saved the keys %v", in.Host.Identities)
			}
			break
		}
	}
	renamed := desk
	renamed.Name = "laptop"
	if why := win.savingClashes(renamed, &desk); why == "" {
		t.Fatal("renamed to a name something is connected as, it was taken")
	}
}

// A window's form greys out what a window has none of, and a server's
// key is kept to be offered next time.
func TestTheServerFormFitsItsType(t *testing.T) {
	win, _, publish := windowStage(t)
	desk := remote.Host{ID: "d1", Name: "desk", Address: "desk.example", Window: true}
	publish(State{Saved: []remote.Host{desk, {ID: "j1", Name: "jump", Address: "jump.example"}}})
	win.serverForm(&desk, lastUI)
	for range 3 {
		lastWindow.Frame(time.Second / 60)
	}
	form, ok := win.dialog.Body.(*widget.Form)
	if !ok {
		t.Fatalf("the form is a %T", win.dialog.Body)
	}
	var via *widget.Dropdown
	var forward *widget.Checkbox
	// Every field, the ones greyed out too, which take no focus.
	for _, f := range form.Children() {
		switch f := f.(type) {
		case *widget.Dropdown:
			if f.Label == "Through" {
				via = f
			}
		case *widget.Checkbox:
			if strings.Contains(f.Label, "agent") {
				forward = f
			}
		}
	}
	if via == nil || forward == nil || !via.Disabled || !forward.Disabled {
		t.Fatalf("for a window, Through is %+v and the agent box %+v", via, forward)
	}

	a := fontApp(t)
	book, err := remote.LoadBook(filepath.Join(t.TempDir(), "servers.json"))
	if err != nil {
		t.Fatal(err)
	}
	a.book = book
	a.handle(SaveServer{Host: remote.Host{Name: "srv", Address: "srv.example", Identities: []string{"/k/id_ed25519"}}})
	if !slices.Equal(a.st.KeyFiles, []string{"/k/id_ed25519"}) {
		t.Fatalf("saved, the kept keys are %v", a.st.KeyFiles)
	}
}

// Enter on About closes it, rather than asking the network anything.
func TestEnterClosesAbout(t *testing.T) {
	win, _, publish := windowStage(t)
	publish(State{})
	for len(lastWindow.Client().Intents()) > 0 {
		<-lastWindow.Client().Intents()
	}
	win.aboutDialog(lastUI)
	for range 3 {
		lastWindow.Frame(time.Second / 60)
	}
	lastWindow.Input(gi.KeyPress{Key: gi.KeyEnter})
	lastWindow.Frame(time.Second / 60)
	if got := nextIntent(t); got != (DialogClosed{}) {
		t.Fatalf("Enter on About sent %#v", got)
	}
}

func TestMakePortableCopiesTheFilesBesideTheProgram(t *testing.T) {
	a, _ := agentApp(t)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	beside := conf.Beside(exe)
	t.Cleanup(func() { _ = os.RemoveAll(beside) })
	a.handle(MakePortable{})
	waitFor(t, a, "the list of what was done", func() bool { return len(a.st.Asks) == 1 })
	if q := a.st.Asks[0]; q.Title != "Made Portable" || !strings.Contains(q.Text, "Created "+beside) || !strings.Contains(q.Text, "Restart gunimterm") {
		t.Fatalf("made portable, it says %+v", q)
	}
	if made, err := conf.IsDir(beside); !made || err != nil {
		t.Fatalf("the folder beside is made %v, %v", made, err)
	}
}
