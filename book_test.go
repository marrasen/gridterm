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
	for _, id := range []string{openPrefix + "margit", editPrefix + "margit"} {
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
	f.Fields()[0].SetText("bastion")
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

func TestCommandSlug(t *testing.T) {
	cases := map[string]string{
		"margit":        "margit",
		"My Box":        "my-box",
		"  spaced  ":    "spaced",
		"UPPER":         "upper",
		"web1.internal": "web1.internal",
	}
	for in, want := range cases {
		if got := commandSlug(in); got != want {
			t.Errorf("commandSlug(%q) = %q, want %q", in, got, want)
		}
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
