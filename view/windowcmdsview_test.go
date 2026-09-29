package view

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/marrasen/kakel/app"

	"github.com/marrasen/kakel/machines"

	"github.com/marrasen/gunim"
	gi "github.com/marrasen/gunim/input"
	"github.com/marrasen/gunim/widget"

	"github.com/marrasen/kakel/input"
	"github.com/marrasen/kakel/keys"
	"github.com/marrasen/kakel/remote"
	"github.com/marrasen/kakel/ui"
)

func TestTheHelpListsEveryCommandWithItsShortcut(t *testing.T) {
	win, _, publish := windowStage(t)
	publish(app.State{Panes: []app.Pane{{ID: "p1", Kind: app.KindHelp, Title: "Shortcuts and Commands"}}, Stage: &app.Box{Pane: "p1"}, Focus: "p1"})
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
	// kakel's own list.
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

// A shortcuts file written for gridterm, naming its commands, its
// commands on one thing of many and its other names, is taken whole.
func TestAShortcutsFileForGridtermIsTaken(t *testing.T) {
	win, _, publish := windowStage(t)
	change := func(k input.Key, id string) keys.Change {
		return keys.Change{Chord: ui.Chord{Key: k, Mods: input.ModCtrl | input.ModAlt}, Command: id, Written: "ctrl+alt+" + id}
	}
	publish(app.State{Shortcuts: []keys.Change{
		change(input.KeyA, "view.switcher"),
		change(input.KeyB, "pane.open"),
		change(input.KeyC, "server.open.my-desk"),
		change(input.KeyD, "conn.files.my-desk.1"),
		change(input.KeyE, "secrets.forget"),
	}, ShortcutsRead: 1})
	if id, _ := win.keys.Lookup(ui.Chord{Key: input.KeyB, Mods: input.ModCtrl | input.ModAlt}); id != "conn.terminal" {
		t.Fatalf("kakel's pane.open is bound to %q, want New Terminal", id)
	}
	if id, _ := win.keys.Lookup(ui.Chord{Key: input.KeyC, Mods: input.ModCtrl | input.ModAlt}); id != "server.open.my-desk" {
		t.Fatalf("a saved server's command is bound to %q", id)
	}
}

func TestCommandsOnOneThingFindItByGridtermsName(t *testing.T) {
	win, _, publish := windowStage(t)
	publish(app.State{Saved: []remote.Host{{ID: "d1", Name: "My Desk", Address: "desk", Folders: []string{"/srv/www"}}}, Connected: []machines.ID{"d1"},
		Machines: []machines.Info{{ID: "d1", Name: "My Desk"}}})
	for len(lastWindow.Client().Intents()) > 0 {
		<-lastWindow.Client().Intents()
	}
	for _, c := range []struct {
		id   string
		want gunim.Intent
	}{
		{"server.open.my-desk", app.ConnectTo{Server: "d1"}},
		{"conn.terminal.my-desk", app.OpenOn{Machine: "d1"}},
		{"conn.terminal.", app.OpenOn{Machine: ""}},
		{"conn.files.my-desk", app.FilesOn{Machine: "d1"}},
		{"conn.files.my-desk.1", app.FilesOn{Machine: "d1", Path: "/srv/www"}},
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
	publish(app.State{Secrets: app.Secrets{Exists: true}})
	for len(lastWindow.Client().Intents()) > 0 {
		<-lastWindow.Client().Intents()
	}
	win.run("secrets.change", lastUI)
	if in := nextIntent(t); in != (app.UnlockSecrets{}) {
		t.Fatalf("with the secrets locked, it sent %#v", in)
	}
	publish(app.State{Secrets: app.Secrets{Exists: true, Open: true, Items: []app.SecretItem{{ID: "s1", Name: "db"}}}})
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

// Editing a server keeps the key files after the first, which the form
// does not show, and refuses a name something is connected as.
func TestEditingAServerKeepsItsKeysAndRefusesATakenName(t *testing.T) {
	win, _, publish := windowStage(t)
	desk := remote.Host{ID: "d1", Name: "desk", Address: "desk.example", Identities: []string{"/k/one", "/k/two"}}
	publish(app.State{Saved: []remote.Host{desk}, Connected: []machines.ID{"laptop"}})
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
		if in, ok := nextIntent(t).(app.SaveServer); ok {
			if !slices.Equal(in.Host.Identities, []string{"/k/one", "/k/two"}) {
				t.Fatalf("saved the keys %v", in.Host.Identities)
			}
			break
		}
	}
	// Everything goes by the server's ID: a new name takes nothing
	// from anything connected.
	renamed := desk
	renamed.Name = "laptop"
	if why := win.savingClashes(renamed, &desk); why != "" {
		t.Fatalf("renamed, it was refused: %s", why)
	}
}

// Enter on About closes it, rather than asking the network anything.
func TestEnterClosesAbout(t *testing.T) {
	win, _, publish := windowStage(t)
	publish(app.State{})
	for len(lastWindow.Client().Intents()) > 0 {
		<-lastWindow.Client().Intents()
	}
	win.aboutDialog(lastUI)
	for range 3 {
		lastWindow.Frame(time.Second / 60)
	}
	lastWindow.Input(gi.KeyPress{Key: gi.KeyEnter})
	lastWindow.Frame(time.Second / 60)
	if got := nextIntent(t); got != (app.DialogClosed{}) {
		t.Fatalf("Enter on About sent %#v", got)
	}
}

// A window's form greys out what a window has none of, calls a window
// what the message about one does, and keeps a server's key to offer
// next time.
func TestTheServerFormFitsItsType(t *testing.T) {
	win, _, publish := windowStage(t)
	desk := remote.Host{ID: "d1", Name: "desk", Address: "desk.example", Window: true}
	publish(app.State{Saved: []remote.Host{desk, {ID: "j1", Name: "jump", Address: "jump.example"}}})
	win.serverForm(&desk, lastUI)
	for range 3 {
		lastWindow.Frame(time.Second / 60)
	}
	form, ok := win.dialog.Body.(*widget.Form)
	if !ok {
		t.Fatalf("the form is a %T", win.dialog.Body)
	}
	var via, kind *widget.Dropdown
	var forward *widget.Checkbox
	// Every field, the ones greyed out too, which take no focus.
	for _, f := range form.Children() {
		switch f := f.(type) {
		case *widget.Dropdown:
			switch f.Label {
			case "Through":
				via = f
			case "Type":
				kind = f
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
	if kind == nil {
		t.Fatal("the form has no Type drop-down")
	}
	if kind.Selected != 1 || kind.Items[1] != remote.WindowKind {
		t.Fatalf("the Type drop-down offers %q with %d chosen, want %q chosen", kind.Items, kind.Selected, remote.WindowKind)
	}

}

// The window takes the shortcuts file's changes: a key moved, one let
// go of, and says so.
func TestTheWindowTakesTheShortcutsFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), keys.File)
	if err := os.WriteFile(path, []byte(`{"version":1,"keys":{"ctrl+shift+J":"pane.close","ctrl+shift+W":"nothing"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	changes, err := keys.Load(path)
	if err != nil || len(changes) != 2 {
		t.Fatalf("read %+v, %v", changes, err)
	}
	win, _, publish := windowStage(t)
	publish(app.State{Shortcuts: changes, ShortcutsRead: 1, ShortcutsAgain: true})
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
	publish(app.State{Shortcuts: []keys.Change{{Chord: ui.Chord{Key: input.KeyK, Mods: input.ModCtrl}, Command: "no.such", Written: "ctrl+K"}}, ShortcutsRead: 2, ShortcutsAgain: true})
	if id, _ := win.keys.Lookup(ui.Chord{Key: input.KeyJ, Mods: input.ModCtrl | input.ModShift}); id != "pane.close" {
		t.Fatal("a file naming an unknown command changed the keys")
	}
	if n := win.toasts.Len(); n != 2 {
		t.Fatalf("refused, the window has shown %d toasts, want one more, saying why", n)
	}
	// A command renamed since the file was written is followed.
	keys.Renamed["pane.shut"] = "pane.close"
	t.Cleanup(func() { delete(keys.Renamed, "pane.shut") })
	publish(app.State{Shortcuts: []keys.Change{{Chord: ui.Chord{Key: input.KeyK, Mods: input.ModCtrl | input.ModShift}, Command: "pane.shut", Written: "ctrl+shift+K"}}, ShortcutsRead: 3})
	if id, _ := win.keys.Lookup(ui.Chord{Key: input.KeyK, Mods: input.ModCtrl | input.ModShift}); id != "pane.close" {
		t.Fatalf("a renamed command's chord runs %q", id)
	}
	_ = gi.KeyA
}
