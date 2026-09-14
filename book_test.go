package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marrasen/gridterm/input"
	"github.com/marrasen/gridterm/remote"
	"github.com/marrasen/gridterm/ui"
)

// saveServer fills in the add-a-server dialog and presses Save.
func saveServer(t *testing.T, a *testApp, name, target string) *ui.Form {
	t.Helper()
	if err := a.openAddServer(); err != nil {
		t.Fatalf("openAddServer: %v", err)
	}
	f := waitForDialog(t, a, "Add a server")
	typeIntoField(t, a, f, 0, name)
	typeIntoField(t, a, f, 1, target)
	pressButton(t, a, f, "Save")
	return f
}

// typeIntoField moves the focus onto one field and types into it.
func typeIntoField(t *testing.T, a *testApp, f *ui.Form, at int, text string) {
	t.Helper()
	for i := 0; i < len(f.Fields())+len(f.Buttons())+1; i++ {
		if got, isButton := f.Focused(); !isButton && got == at {
			for _, r := range text {
				a.root.HandleKey(input1(r))
			}
			return
		}
		a.root.HandleKey(press(input.KeyTab, 0))
	}
	t.Fatalf("focus never reached field %d", at)
}

// The whole path: add a server, and find it on the menu and in the
// palette afterwards.
func TestAddServerRegistersItsCommands(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withMenubar(t, a)
	a.refreshServers()

	saveServer(t, a, "margit", "marcus@margit.skalarit.net")

	if got := a.book.Hosts(); len(got) != 1 || got[0].Name != "margit" {
		t.Fatalf("the book holds %v, want one server called margit", got)
	}
	for _, id := range []string{"server.open.margit", "server.edit.margit"} {
		if _, ok := a.root.Commands.Lookup(id); !ok {
			t.Errorf("no command %q was registered", id)
		}
	}
	if !menuNames(a).has("Connect to margit") {
		t.Error("the server is not on the menu")
	}
}

// A server that has been renamed must not still answer to its old name.
func TestEditServerReplacesItsCommands(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withMenubar(t, a)
	a.refreshServers()
	saveServer(t, a, "margit", "marcus@margit.skalarit.net")

	if err := a.openEditServer("margit"); err != nil {
		t.Fatalf("openEditServer: %v", err)
	}
	f := waitForDialog(t, a, "Edit margit")
	// The dialog opens filled in with what was saved.
	if got := f.Fields()[1].Text(); got != "marcus@margit.skalarit.net" {
		t.Fatalf("the Server field holds %q, want what was saved", got)
	}
	typeIntoField(t, a, f, 0, "")
	f.Fields()[0].SetText("")
	typeIntoField(t, a, f, 0, "bastion")
	pressButton(t, a, f, "Save")

	if _, ok := a.root.Commands.Lookup(openPrefix + "margit"); ok {
		t.Error("the old name is still a command")
	}
	if _, ok := a.root.Commands.Lookup(openPrefix + "bastion"); !ok {
		t.Error("the new name is not a command")
	}
	if menuNames(a).has("Connect to margit") {
		t.Error("the old name is still on the menu")
	}
}

// Removing asks first, and the question opens on the answer that changes
// nothing.
func TestRemoveServerAsksFirst(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withMenubar(t, a)
	a.refreshServers()
	saveServer(t, a, "margit", "margit.skalarit.net")

	if err := a.openEditServer("margit"); err != nil {
		t.Fatalf("openEditServer: %v", err)
	}
	f := waitForDialog(t, a, "Edit margit")
	pressButton(t, a, f, "Remove")

	ask := waitForDialog(t, a, "Remove margit?")
	if at, isButton := ask.Focused(); !isButton || ask.Buttons()[at].Title != "Keep it" {
		t.Error("the question does not open on the answer that changes nothing")
	}
	if len(a.book.Hosts()) != 1 {
		t.Fatal("the server went before the question was answered")
	}

	pressButton(t, a, ask, "Remove")
	if got := a.book.Hosts(); len(got) != 0 {
		t.Fatalf("the book still holds %v", got)
	}
	if _, ok := a.root.Commands.Lookup(openPrefix + "margit"); ok {
		t.Error("the command outlived the server")
	}
}

// A server list that could not be read is not written over, and the
// dialog says so rather than taking what is typed and losing it.
func TestAddServerRefusesWhenTheListCouldNotBeRead(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)

	path := filepath.Join(t.TempDir(), "servers.json")
	if err := os.WriteFile(path, []byte("{ not json"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	book, err := remote.LoadBook(path)
	if err == nil {
		t.Fatal("LoadBook read a file that is not a server list")
	}
	a.book = book

	if err := a.openAddServer(); err == nil {
		t.Fatal("the dialog opened for a list that cannot be saved")
	}
	if m := a.root.Modal(); m != nil {
		t.Fatalf("a dialog opened anyway: %T", m)
	}
}

// The window says why on its first frame: one opened from an icon has no
// console to read.
func TestBookErrorIsReportedInTheWindow(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)

	path := filepath.Join(t.TempDir(), "servers.json")
	if err := os.WriteFile(path, []byte("{ not json"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	a.book, _ = remote.LoadBook(path)

	a.reportBookError()
	f := waitForDialog(t, a, "The server list could not be read")
	joined := strings.Join(f.Lines, " ")
	if !strings.Contains(joined, "repaired") {
		t.Errorf("the dialog does not say what happens next: %q", joined)
	}
}

// A saved name that is not a valid target keeps the dialog open with
// what was typed still in it.
func TestAddServerKeepsTheDialogOpenOnABadTarget(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	a.refreshServers()

	f := saveServer(t, a, "broken", "host:nope")
	if a.root.Modal() != f {
		t.Fatal("the dialog closed on a target it could not parse")
	}
	if f.Error() == nil {
		t.Fatal("nothing said why")
	}
	if got := f.Fields()[1].Text(); got != "host:nope" {
		t.Errorf("the dialog lost what was typed: %q", got)
	}
	if len(a.book.Hosts()) != 0 {
		t.Fatal("a server that would not parse was saved")
	}
}

// Reaching one machine through another needs a connection that outlives
// the shell on it, which is not built yet. Saying so beats connecting to
// the wrong machine.
func TestConnectSavedRefusesARouteItCannotMakeYet(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)

	if err := a.book.Put(remote.Host{Name: "edge", Address: "edge.example"}, ""); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := a.book.Put(remote.Host{
		Name: "db", Address: "db.internal", Via: "edge",
	}, ""); err != nil {
		t.Fatalf("Put: %v", err)
	}

	err := a.connectSaved("db")
	if err == nil {
		t.Fatal("a route gridterm cannot make yet was attempted")
	}
	if !strings.Contains(err.Error(), "edge") {
		t.Fatalf("error = %v, want it to name the machine in the way", err)
	}
	if a.connecting {
		t.Error("a connection was started anyway")
	}
}

// Two names that reduce to the same command id would leave one of the
// two unreachable: not on the menu, not in the palette, and only
// removable by hand-editing the file.
func TestAddServerRefusesANameTooLikeAnother(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withMenubar(t, a)
	a.refreshServers()

	saveServer(t, a, "My Box", "one.example")
	f := saveServer(t, a, "my-box", "two.example")

	if f.Error() == nil {
		t.Fatal("a second server took a name that reduces to the same command")
	}
	if got := a.book.Hosts(); len(got) != 1 {
		t.Fatalf("the book holds %d servers, want the one that was saved", len(got))
	}
	// And the menu offers exactly one line for a saved server.
	if lines := len(serverMenuLines(a)); lines != 1 {
		t.Fatalf("%d lines connect to a saved server, want 1", lines)
	}
}

// menuTitles is what the menu bar is currently offering.
type menuTitles []string

func (m menuTitles) has(title string) bool {
	for _, have := range m {
		if have == title {
			return true
		}
	}
	return false
}

// serverMenuLines returns the ids of the saved-server lines on the
// Servers menu, which is not the same as every line on it.
func serverMenuLines(a *testApp) []string {
	var out []string
	for _, def := range a.bar.Menus {
		if def.Title != "Servers" {
			continue
		}
		for _, item := range def.Items {
			if strings.HasPrefix(item.Command, openPrefix) {
				out = append(out, item.Command)
			}
		}
	}
	return out
}

// menuNames returns the title of every command on the Servers menu.
func menuNames(a *testApp) menuTitles {
	var out menuTitles
	for _, def := range a.bar.Menus {
		if def.Title != "Servers" {
			continue
		}
		for _, item := range def.Items {
			if item.Command == "" {
				continue
			}
			if cmd, ok := a.root.Commands.Lookup(item.Command); ok {
				out = append(out, cmd.Title)
			}
		}
	}
	return out
}

// Every field in the dialog has to reach the saved server. Three of the
// four did nothing that any test could see.
func TestAddServerSavesEveryFieldInTheDialog(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	a.refreshServers()

	// Something to be reached through, first.
	saveServer(t, a, "bastion", "root@bastion.example")

	if err := a.openAddServer(); err != nil {
		t.Fatalf("openAddServer: %v", err)
	}
	f := waitForDialog(t, a, "Add a server")
	typeIntoField(t, a, f, 0, "db")
	typeIntoField(t, a, f, 1, "postgres@db.internal:5433")
	typeIntoField(t, a, f, 2, "/keys/db")
	typeIntoField(t, a, f, 3, "bastion")
	pressButton(t, a, f, "Save")

	if f.Error() != nil {
		t.Fatalf("Save: %v", f.Error())
	}
	got, ok := a.book.Lookup("db")
	if !ok {
		t.Fatal("the server was not saved")
	}
	switch {
	case got.Address != "db.internal":
		t.Errorf("address = %q", got.Address)
	case got.Port != 5433:
		t.Errorf("port = %d, want 5433", got.Port)
	case got.User != "postgres":
		t.Errorf("user = %q, want postgres", got.User)
	case got.Via != "bastion":
		t.Errorf("through = %q, want bastion", got.Via)
	case len(got.Identities) != 1 || got.Identities[0] != "/keys/db":
		t.Errorf("key files = %v, want the one that was typed", got.Identities)
	}
}

// A field that cannot show everything a machine has must not delete the
// rest of it.
func TestEditServerKeepsWhatTheDialogCannotShow(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	a.refreshServers()

	// More key files than the dialog has room for, and a terminal type
	// it does not ask about at all.
	saved := remote.Host{
		Name: "web1", Address: "web1.internal",
		Identities: []string{"/keys/one", "/keys/two", "/keys/three"},
		Term:       "xterm-mono",
	}
	if err := a.book.Put(saved, ""); err != nil {
		t.Fatalf("Put: %v", err)
	}
	a.refreshServers()

	if err := a.openEditServer("web1"); err != nil {
		t.Fatalf("openEditServer: %v", err)
	}
	f := waitForDialog(t, a, "Edit web1")
	if got := f.Fields()[2].Text(); got != "/keys/one" {
		t.Fatalf("the Key file field shows %q, want the first one", got)
	}
	// Change nothing but the port.
	f.Fields()[1].SetText("web1.internal:2222")
	pressButton(t, a, f, "Save")

	got, ok := a.book.Lookup("web1")
	if !ok {
		t.Fatal("the server went")
	}
	if got.Port != 2222 {
		t.Errorf("port = %d, want the edit to have stuck", got.Port)
	}
	if len(got.Identities) != 3 {
		t.Errorf("key files = %v, want all three kept", got.Identities)
	}
	if got.Term != "xterm-mono" {
		t.Errorf("terminal type = %q, want it kept", got.Term)
	}
}

// A user who repairs the file says so, rather than restarting.
func TestReloadBookPicksUpTheRepairedFile(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withMenubar(t, a)

	path := filepath.Join(t.TempDir(), "servers.json")
	if err := os.WriteFile(path, []byte("{ broken"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	a.book, _ = remote.LoadBook(path)
	a.refreshServers()
	if len(serverMenuLines(a)) != 0 {
		t.Fatal("a broken list put servers on the menu")
	}

	const repaired = `{"version":1,"servers":[{"name":"margit","address":"margit.example"}]}`
	if err := os.WriteFile(path, []byte(repaired), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := a.reloadBook(); err != nil {
		t.Fatalf("reloadBook: %v", err)
	}
	if got := serverMenuLines(a); len(got) != 1 || got[0] != "server.open.margit" {
		t.Fatalf("the menu offers %v after a repair", got)
	}
}

// With nothing saved, the menu is still the two things a user can do.
func TestServerMenuWithNothingSaved(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withMenubar(t, a)
	a.refreshServers()

	var found bool
	for _, def := range a.bar.Menus {
		if def.Title != "Servers" {
			continue
		}
		found = true
		if len(serverMenuLines(a)) != 0 {
			t.Error("a server is on the menu with none saved")
		}
		// No stray separator above the first line.
		if len(def.Items) == 0 || def.Items[0].Command == "" {
			t.Errorf("the menu starts with %+v, want something to run", def.Items)
		}
		for _, item := range def.Items {
			if item.Command == "" {
				continue
			}
			if _, ok := a.root.Commands.Lookup(item.Command); !ok {
				t.Errorf("the menu names %q, which is not a command", item.Command)
			}
		}
	}
	if !found {
		t.Fatal("there is no Servers menu")
	}
}
