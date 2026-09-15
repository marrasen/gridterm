package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/marrasen/gridterm/input"
	"github.com/marrasen/gridterm/internal/sshtest"
	"github.com/marrasen/gridterm/remote"
	"github.com/marrasen/gridterm/ui"
)

// saveServer fills in the add-a-server dialog and presses Save.
func saveServer(t *testing.T, a *testApp, name, target string) *ui.Form {
	t.Helper()
	if err := a.openAddServer(); err != nil {
		t.Fatalf("openAddServer: %v", err)
	}
	f := awaitModal(t, a, "the Add a server dialog", byTitle[*ui.Form]("Add a server"))
	typeIntoField(t, a, f, "Name", name)
	typeIntoField(t, a, f, "Server", target)
	pressButton(t, a, f, "Save")
	return f
}

// focusField tabs the focus onto one field of a dialog.
//
// By label rather than by position, so adding a row to a form does not
// move every test that types into the ones after it.
func focusField(t *testing.T, a *testApp, f *ui.Form, label string) {
	t.Helper()
	at := -1
	for i, have := range f.Fields() {
		if have == f.Field(label) {
			at = i
			break
		}
	}
	if at < 0 {
		t.Fatalf("the dialog has no %q field", label)
	}
	for i := 0; i < len(f.Fields())+len(f.Buttons())+1; i++ {
		if got, isButton := f.Focused(); !isButton && got == at {
			return
		}
		if _, err := a.root.HandleKey(press(input.KeyTab, 0)); err != nil {
			t.Fatalf("tabbing towards the %q field: %v", label, err)
		}
	}
	t.Fatalf("focus never reached the %q field", label)
}

// typeIntoField moves the focus onto one field and types into it.
func typeIntoField(t *testing.T, a *testApp, f *ui.Form, label, text string) {
	t.Helper()
	focusField(t, a, f, label)
	for _, r := range text {
		if _, err := a.root.HandleKey(input1(r)); err != nil {
			t.Fatalf("typing into the %q field: %v", label, err)
		}
	}
}

// retypeField clears a field with the keys the dialog itself takes -- End,
// then Ctrl+U -- and types the new value in.
func retypeField(t *testing.T, a *testApp, f *ui.Form, label, text string) {
	t.Helper()
	focusField(t, a, f, label)
	for _, ev := range []input.Event{press(input.KeyEnd, 0), press(input.KeyU, input.ModCtrl)} {
		if _, err := a.root.HandleKey(ev); err != nil {
			t.Fatalf("clearing the %q field: %v", label, err)
		}
	}
	if got := f.Field(label).Text(); got != "" {
		t.Fatalf("the %q field still holds %q after being cleared", label, got)
	}
	typeIntoField(t, a, f, label, text)
}

// stepOptions moves the focus onto a field that offers a list and steps
// it on, with the keys the dialog's own hint names.
func stepOptions(t *testing.T, a *testApp, f *ui.Form, label string) {
	t.Helper()
	focusField(t, a, f, label)
	if _, err := a.root.HandleKey(press(input.KeyDown, input.ModCtrl)); err != nil {
		t.Fatalf("stepping the %q field: %v", label, err)
	}
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
	f := awaitModal(t, a, "the Edit margit dialog", byTitle[*ui.Form]("Edit margit"))
	// The dialog opens filled in with what was saved.
	if got := f.Field("Server").Text(); got != "marcus@margit.skalarit.net" {
		t.Fatalf("the Server field holds %q, want what was saved", got)
	}
	retypeField(t, a, f, "Name", "bastion")
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
	f := awaitModal(t, a, "the Edit margit dialog", byTitle[*ui.Form]("Edit margit"))
	pressButton(t, a, f, "Remove")

	ask := awaitModal(t, a, "the Remove margit? dialog", byTitle[*ui.Form]("Remove margit?"))
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
	n := awaitModal(t, a, "the The server list could not be read notice", byTitle[*ui.Notice]("The server list could not be read"))
	if !strings.Contains(n.Message(), "repaired") {
		t.Errorf("the dialog does not say what happens next: %q", n.Message())
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
	if got := f.Field("Server").Text(); got != "host:nope" {
		t.Errorf("the dialog lost what was typed: %q", got)
	}
	if len(a.book.Hosts()) != 0 {
		t.Fatal("a server that would not parse was saved")
	}
}

// A machine saved as being behind another is connected to through it,
// with the one in the way connected to first.
//
// This is only the order; the whole route against two real servers is
// TestConnectSavedWalksTheRoute.
func TestConnectSavedStartsWithTheMachineInTheWay(t *testing.T) {
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

	if err := a.connectSaved("db"); err != nil {
		t.Fatalf("connectSaved: %v", err)
	}
	if a.machines.beingMade() != 1 {
		t.Fatalf("%d connections are being made, want the one route", a.machines.beingMade())
	}
	// Both machines on the route are being connected to, under the one
	// row that stands for the far end.
	if a.machines.connecting("edge") == nil || a.machines.connecting("db") == nil {
		t.Fatalf("the route being made is %v, want both machines", a.machines.reaching())
	}
	waiting := waitForConnecting(t, a)
	if waiting.Host != "db" {
		t.Errorf("the row is under %q, want db", waiting.Host)
	}
	// Given up on rather than left dialling a machine that is not there.
	if err := waiting.Close(); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	for deadline := time.Now().Add(waitBudget); time.Now().Before(deadline); {
		a.pump.run()
		if a.machines.beingMade() == 0 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("the route was still being made long after it was cancelled")
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
	f := awaitModal(t, a, "the Add a server dialog", byTitle[*ui.Form]("Add a server"))
	typeIntoField(t, a, f, "Name", "db")
	typeIntoField(t, a, f, "Server", "postgres@db.internal:5433")
	typeIntoField(t, a, f, "Key file", "/keys/db")
	typeIntoField(t, a, f, "Through", "bastion")
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
	f := awaitModal(t, a, "the Edit web1 dialog", byTitle[*ui.Form]("Edit web1"))
	if got := f.Field("Key file").Text(); got != "/keys/one" {
		t.Fatalf("the Key file field shows %q, want the first one", got)
	}
	// Change nothing but the port.
	retypeField(t, a, f, "Server", "web1.internal:2222")
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

// The Through field offers the machines already saved, so one can be
// chosen rather than remembered.
func TestTheThroughFieldOffersTheSavedServers(t *testing.T) {
	s := sshtest.New(t)
	a := newTestApp(t, 100, 30)
	withDialogs(t, a)
	saveHost(t, a, "edge", s, "")
	saveHost(t, a, "db", s, "")

	if err := a.openAddServer(); err != nil {
		t.Fatalf("openAddServer: %v", err)
	}
	f, ok := a.root.Modal().(*ui.Form)
	if !ok {
		t.Fatalf("it showed %T", a.root.Modal())
	}
	via := f.Field("Through")
	if len(via.Options) != 3 {
		t.Fatalf("the field offers %v, want the blank and both machines", via.Options)
	}
	// The blank first: leaving it empty is the usual answer, and it is
	// what stepping back round lands on.
	if via.Options[0] != "" {
		t.Fatalf("the field offers %v, want the blank first", via.Options)
	}
	for _, want := range []string{"edge", "db"} {
		var found bool
		for _, option := range via.Options {
			if option == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("the field offers %v, missing %q", via.Options, want)
		}
	}
	// And the dialog says how to reach them.
	hint := strings.Join(f.Lines, " ")
	if !strings.Contains(hint, "ctrl+down") || !strings.Contains(hint, "edge") {
		t.Fatalf("the dialog says %q", hint)
	}
}

// A machine cannot be reached through itself, so editing one leaves it
// off its own list.
func TestAServerIsNotOfferedAsItsOwnRoute(t *testing.T) {
	s := sshtest.New(t)
	a := newTestApp(t, 100, 30)
	withDialogs(t, a)
	saveHost(t, a, "edge", s, "")
	saveHost(t, a, "db", s, "")

	if err := a.openEditServer("db"); err != nil {
		t.Fatalf("openEditServer: %v", err)
	}
	f := a.root.Modal().(*ui.Form)
	via := f.Field("Through")
	for _, option := range via.Options {
		if option == "db" {
			t.Fatalf("the dialog offers db as its own route: %v", via.Options)
		}
	}
	if len(via.Options) != 2 {
		t.Fatalf("the dialog offers %v, want the blank and edge", via.Options)
	}
}

// The palette lists a terminal and a file pane for every machine by
// name, including this one, so a machine can be reached by typing what
// it is called rather than by first going to look at it.
func TestEveryMachineHasItsOwnCommands(t *testing.T) {
	s := sshtest.New(t)
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	pinServers(t, a, s)
	saveHost(t, a, "margit", s, "")
	// saveHost writes straight to the book; the dialog that saves one in
	// the window rebuilds the commands after it.
	a.refreshServers()

	for _, want := range []string{
		"Open a terminal on Local", "Browse files on Local",
		"Open a terminal on margit", "Browse files on margit",
	} {
		if !titled(a, want) {
			t.Errorf("the palette has no %q: %v", want, commandTitles(a))
		}
	}
	// And the ones that act on whatever is in front are still there.
	for _, want := range []string{"Open a terminal here", "Browse files here"} {
		if !titled(a, want) {
			t.Errorf("the palette has no %q", want)
		}
	}
	// "another" is gone: a terminal is a terminal.
	for _, title := range commandTitles(a) {
		if strings.Contains(title, "another terminal") {
			t.Errorf("the palette still says %q", title)
		}
	}
}

// A machine the window has connected to gets its commands even though
// the book never named it.
func TestAConnectedMachineGetsItsOwnCommands(t *testing.T) {
	s := sshtest.New(t)
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	a.connectAs("live", serverConfig(t, s))
	waitForPanes(t, a, 2)

	if !titled(a, "Browse files on live") {
		t.Fatalf("the palette has no browse command for it: %v", commandTitles(a))
	}
	// And they go when it does.
	if err := a.dropMachine("live"); err != nil {
		t.Fatalf("dropMachine: %v", err)
	}
	if titled(a, "Browse files on live") {
		t.Fatalf("the commands outlived the connection: %v", commandTitles(a))
	}
}

// commandTitles is what the registry is offering.
func commandTitles(a *testApp) []string {
	var out []string
	for _, cmd := range a.root.Commands.All() {
		out = append(out, cmd.Title)
	}
	return out
}

// titled reports whether a command with this title is registered.
func titled(a *testApp, title string) bool {
	for _, got := range commandTitles(a) {
		if got == title {
			return true
		}
	}
	return false
}

// A rebuild that would change nothing is skipped, so a connection the
// user was not watching cannot take the menu they are reading away.
func TestTheServerCommandsAreNotRebuiltForNothing(t *testing.T) {
	s := sshtest.New(t)
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	saveHost(t, a, "margit", s, "")
	a.refreshServers()
	a.bar = a.newMenubar(a.root.Widget())
	a.root.SetWidget(a.bar)
	a.relayout()

	if !a.bar.Open(0) {
		t.Fatal("no menu opened")
	}
	if a.bar.OpenIndex() < 0 {
		t.Fatal("the bar says nothing is open")
	}
	// The machines have not changed, so this changes nothing.
	a.refreshServers()
	if a.bar.OpenIndex() < 0 {
		t.Fatal("a rebuild that changed nothing closed the open menu")
	}

	// A machine really arriving does rebuild, and the menu goes with it:
	// the list it hangs under has changed shape.
	a.connectAs("live", serverConfig(t, s))
	waitForPanes(t, a, 2)
	if !titled(a, "Browse files on live") {
		t.Fatalf("the new machine has no commands: %v", commandTitles(a))
	}
}

// Renaming a saved machine takes what is open under the old name with
// it.
//
// The machine has not changed, only what it is called. The old name
// stayed in the sidebar with a pane under it, so one machine showed as
// two.
func TestRenamingAMachineTakesWhatIsOpenWithIt(t *testing.T) {
	s := sshtest.New(t)
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	pinServers(t, a, s)

	saveHost(t, a, "picard", s, "")
	if err := a.connectSaved("picard"); err != nil {
		t.Fatalf("connectSaved: %v", err)
	}
	waitFor(t, a, "the machine to connect", func() bool { return a.machines.named("picard") != nil })

	if err := a.openEditServer("picard"); err != nil {
		t.Fatalf("edit: %v", err)
	}
	f := awaitModal[*ui.Form](t, a, "a dialog", nil)
	retypeField(t, a, f, "Name", "picard via skylake")
	pressButton(t, a, f, "Save")
	a.pump.run()

	if a.machines.named("picard") != nil {
		t.Fatal("the connection is still held under the old name")
	}
	m := a.machines.named("picard via skylake")
	if m == nil {
		t.Fatalf("the connection did not follow the rename: %v", a.machines.names())
	}
	shown := strings.Join(panelText(a, time.Now()), "\n")
	if strings.Contains(shown, "picard\n") || strings.HasSuffix(shown, "picard") {
		t.Fatalf("the old name is still in the sidebar:\n%s", shown)
	}
	if !strings.Contains(shown, "picard via skylake") {
		t.Fatalf("the new name is not in the sidebar:\n%s", shown)
	}
	// And the row still closes the connection, which is what the old
	// name's closure would have looked for.
	if err := m.entry.Close(); err != nil {
		t.Fatalf("close the renamed connection: %v", err)
	}
	if a.machines.named("picard via skylake") != nil {
		t.Fatal("closing the renamed connection did nothing")
	}
}

// Renaming only the capitals of a name still moves what is open.
//
// The book keeps names case-insensitively and the window keys its own
// record by name exactly, so "picard" to "Picard" left one machine
// showing as two and let a second connection be made to it.
func TestRenamingOnlyTheCapitalsStillMovesWhatIsOpen(t *testing.T) {
	s := sshtest.New(t)
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	pinServers(t, a, s)

	saveHost(t, a, "picard", s, "")
	if err := a.connectSaved("picard"); err != nil {
		t.Fatalf("connectSaved: %v", err)
	}
	waitFor(t, a, "the machine to connect", func() bool { return a.machines.named("picard") != nil })

	if err := a.openEditServer("picard"); err != nil {
		t.Fatalf("edit: %v", err)
	}
	f := awaitModal[*ui.Form](t, a, "a dialog", nil)
	retypeField(t, a, f, "Name", "Picard")
	pressButton(t, a, f, "Save")
	a.pump.run()

	if a.machines.named("picard") != nil {
		t.Fatal("the connection is still held under the old capitals")
	}
	if a.machines.named("Picard") == nil {
		t.Fatalf("the connection did not follow the rename: %v", a.machines.names())
	}
}

// Renaming a machine onto a name something is already connected as is
// refused, with the dialog left open to correct.
func TestRenamingOntoAConnectedNameIsRefused(t *testing.T) {
	s := sshtest.New(t)
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	pinServers(t, a, s)

	// One saved, and one reached by typing a target, which the book
	// never hears about.
	saveHost(t, a, "picard", s, "")
	a.connectAs("enterprise", a.prepare(serverConfig(t, s)))
	waitFor(t, a, "the typed machine to connect", func() bool {
		return a.machines.named("enterprise") != nil
	})
	was := a.machines.named("enterprise")

	if err := a.openEditServer("picard"); err != nil {
		t.Fatalf("edit: %v", err)
	}
	f := awaitModal[*ui.Form](t, a, "a dialog", nil)
	retypeField(t, a, f, "Name", "enterprise")
	pressButton(t, a, f, "Save")
	a.pump.run()

	if a.machines.named("enterprise") != was {
		t.Fatal("the connection that was already there was dropped without being closed")
	}
	if _, ok := a.book.Lookup("enterprise"); ok {
		t.Fatal("the rename was saved anyway")
	}
	if _, ok := a.book.Lookup("picard"); !ok {
		t.Fatal("the machine lost its name")
	}
}

// Renaming onto a name still being connected to is refused the same
// way: the connection on its way will be held under that name.
func TestRenamingOntoAConnectingNameIsRefused(t *testing.T) {
	deafHost, deafPort := sshtest.Deaf(t)
	s := sshtest.New(t)
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	pinServers(t, a, s)

	saveHost(t, a, "picard", s, "")
	cfg := serverConfig(t, s)
	cfg.Host, cfg.Port = deafHost, deafPort
	a.connectAs("slow", cfg)
	if a.machines.connecting("slow") == nil {
		t.Fatal("nothing is on its way to the deaf machine, so this proves nothing")
	}

	if err := a.openEditServer("picard"); err != nil {
		t.Fatalf("edit: %v", err)
	}
	f := awaitModal[*ui.Form](t, a, "a dialog", nil)
	retypeField(t, a, f, "Name", "slow")
	pressButton(t, a, f, "Save")
	a.pump.run()

	if _, ok := a.book.Lookup("slow"); ok {
		t.Fatal("the rename was saved onto a name still being connected to")
	}
	if _, ok := a.book.Lookup("picard"); !ok {
		t.Fatal("the machine lost its name")
	}
}

// A rename that also changes the address leaves the old connection
// where it was: it is a connection to the old machine, and the new name
// is not it.
func TestRenamingAndRetargetingLeavesTheOldConnection(t *testing.T) {
	near := sshtest.New(t)
	far := sshtest.New(t)
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	pinServers(t, a, near, far)

	saveHost(t, a, "picard", near, "")
	if err := a.connectSaved("picard"); err != nil {
		t.Fatalf("connectSaved: %v", err)
	}
	waitFor(t, a, "the machine to connect", func() bool { return a.machines.named("picard") != nil })

	host, port := far.Host()
	if err := a.openEditServer("picard"); err != nil {
		t.Fatalf("edit: %v", err)
	}
	f := awaitModal[*ui.Form](t, a, "a dialog", nil)
	retypeField(t, a, f, "Name", "enterprise")
	retypeField(t, a, f, "Server", fmt.Sprintf("tester@%s:%d", host, port))
	pressButton(t, a, f, "Save")
	a.pump.run()

	if a.machines.named("enterprise") != nil {
		t.Fatal("the connection to the old machine was moved under the new name")
	}
	m := a.machines.named("picard")
	if m == nil {
		t.Fatalf("the connection to the old machine was lost: %v", a.machines.names())
	}

	// And its rows stayed with it. They used to follow the name, which
	// left the panel drawing them under a heading nothing is connected
	// to, and the live connection's heading with no dot on it.
	panelText(a, time.Now())
	heading, ok := panelRow(a, hostKey("picard"))
	if !ok {
		t.Fatalf("the panel has no heading for the live connection: %v",
			panelText(a, time.Now()))
	}
	if heading.Mark == ' ' {
		t.Error("the heading of the live connection has no dot on it")
	}
	if moved, ok := panelRow(a, hostKey("enterprise")); ok && moved.Mark != ' ' {
		t.Error("the new name has a dot for a connection that did not move")
	}
	for _, pane := range a.machines.panesOn(m) {
		if got := a.panes[pane].Host; got != "picard" {
			t.Errorf("a pane on the connection is filed under %q, want picard", got)
		}
	}

	// And the old name still closes it.
	a.hostMenus.nowAbout("picard")
	if err := a.disconnectHere(); err != nil {
		t.Fatalf("closing it from the name it is still under: %v", err)
	}
	heldAs(t, a, "picard", isFree)
}

// A gridterm window can be saved under a name, and taken over from the
// sidebar without retyping its address and its key every time.
func TestASavedWindowIsTakenOverByName(t *testing.T) {
	host := newTestApp(t, 90, 30)
	withDialogs(t, host)
	keyFile, line := aKeyFile(t)
	withServing(t, host, line)
	if err := host.startServing("0", whereHere); err != nil {
		t.Fatalf("serve: %v", err)
	}
	addr := host.serving.addr()

	client := newTestApp(t, 90, 30)
	withDialogs(t, client)
	withPanel(t, client)
	if err := client.book.Put(remote.Host{
		Name: "statio", Address: hostOf(t, addr), Port: portOf(t, addr),
		Window: true, Identities: []string{keyFile},
	}, ""); err != nil {
		t.Fatalf("Put: %v", err)
	}
	client.refreshServers()

	if err := client.connectSaved("statio"); err != nil {
		t.Fatalf("connectSaved: %v", err)
	}
	answer(t, client, "Connect")
	waitFor(t, client, "the window to be taken over", func() bool {
		return client.windows.named("statio") != nil
	})

	// Under the name it was given, not under its address: one heading
	// for it, not two.
	if client.windows.named(addr) != nil {
		t.Fatal("it is also held under its address")
	}
	shown := strings.Join(panelText(client, time.Now()), "\n")
	if !strings.Contains(shown, "statio") {
		t.Fatalf("the sidebar does not name it:\n%s", shown)
	}
}

// A saved window is a different colour in the sidebar, because it is a
// different thing to a machine.
func TestASavedWindowHasItsOwnColourInTheSidebar(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	if err := a.book.Put(remote.Host{
		Name: "statio", Address: "10.0.0.5", Window: true,
	}, ""); err != nil {
		t.Fatalf("Put: %v", err)
	}
	saveHostNamed(t, a, "margit", "10.0.0.6")
	a.refreshServers()
	a.refreshPanel(time.Now())

	window := a.hostRow(a.about("statio"), time.Now())
	machine := a.hostRow(a.about("margit"), time.Now())
	if window.FG == machine.FG {
		t.Fatalf("a window and a machine are the same colour: %v", window.FG)
	}
	if (window.FG == ui.ListRow{}.FG) {
		t.Fatal("the window heading has no colour of its own")
	}
}

// saveHostNamed puts a plain machine in the book.
func saveHostNamed(t *testing.T, a *testApp, name, address string) {
	t.Helper()
	if err := a.book.Put(remote.Host{Name: name, Address: address, User: "tester"}, ""); err != nil {
		t.Fatalf("save %s: %v", name, err)
	}
}

// hostOf and portOf split a host:port the test server gave back.
func hostOf(t *testing.T, addr string) string {
	t.Helper()
	h, _, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("split %s: %v", addr, err)
	}
	return h
}

func portOf(t *testing.T, addr string) int {
	t.Helper()
	_, p, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("split %s: %v", addr, err)
	}
	n, err := strconv.Atoi(p)
	if err != nil {
		t.Fatalf("port %s: %v", p, err)
	}
	return n
}

// A window already taken over is not taken over again because its name
// changed.
//
// Saving, renaming or forgetting one changes what it is called and
// changes nothing about the connection, so the guard goes by address.
func TestAWindowAlreadyTakenOverIsNotTakenOverTwice(t *testing.T) {
	host := newTestApp(t, 90, 30)
	withDialogs(t, host)
	keyFile, line := aKeyFile(t)
	withServing(t, host, line)
	if err := host.startServing("0", whereHere); err != nil {
		t.Fatalf("serve: %v", err)
	}
	addr := host.serving.addr()

	client := newTestApp(t, 90, 30)
	withDialogs(t, client)
	withPanel(t, client)
	if err := client.takeOver(addr, keyFile, nil); err != nil {
		t.Fatalf("take over: %v", err)
	}
	answer(t, client, "Connect")
	waitFor(t, client, "the window to be taken over", func() bool {
		return client.windows.named(addr) != nil
	})

	// Saved after the fact, so the name it goes by and the name the
	// book gives it are now different.
	if err := client.book.Put(remote.Host{
		Name: "statio", Address: hostOf(t, addr), Port: portOf(t, addr),
		Window: true, Identities: []string{keyFile},
	}, ""); err != nil {
		t.Fatalf("Put: %v", err)
	}
	client.refreshServers()

	if err := client.takeOver(addr, keyFile, nil); err == nil {
		t.Fatal("it took over the same window twice")
	}
	if n := client.windows.count(); n != 1 {
		t.Fatalf("%d windows are held, want the one", n)
	}
}

// Renaming a window that is taken over takes the connection with it.
func TestRenamingATakenOverWindowMovesItsConnection(t *testing.T) {
	host := newTestApp(t, 90, 30)
	withDialogs(t, host)
	keyFile, line := aKeyFile(t)
	withServing(t, host, line)
	if err := host.startServing("0", whereHere); err != nil {
		t.Fatalf("serve: %v", err)
	}
	addr := host.serving.addr()

	client := newTestApp(t, 90, 30)
	withDialogs(t, client)
	withPanel(t, client)
	if err := client.book.Put(remote.Host{
		Name: "statio", Address: hostOf(t, addr), Port: portOf(t, addr),
		Window: true, Identities: []string{keyFile},
	}, ""); err != nil {
		t.Fatalf("Put: %v", err)
	}
	client.refreshServers()
	if err := client.connectSaved("statio"); err != nil {
		t.Fatalf("connectSaved: %v", err)
	}
	answer(t, client, "Connect")
	waitFor(t, client, "the window to be taken over", func() bool {
		return client.windows.named("statio") != nil
	})

	if err := client.openEditServer("statio"); err != nil {
		t.Fatalf("edit: %v", err)
	}
	f := awaitModal[*ui.Form](t, client, "a dialog", nil)
	retypeField(t, client, f, "Name", "m-statio")
	pressButton(t, client, f, "Save")
	client.pump.run()

	if client.windows.named("statio") != nil {
		t.Fatal("the connection is still held under the old name")
	}
	t2 := client.windows.named("m-statio")
	if t2 == nil {
		t.Fatalf("the connection did not follow the rename: %v", client.windows.names())
	}
	if got := client.about("m-statio").kind; got != hostWindow {
		t.Errorf("under its new name it is a %v, want a window", got)
	}
	// And closing it under the new name really closes it.
	if err := client.dropWindow("m-statio"); err != nil {
		t.Fatalf("let go of it: %v", err)
	}
	if client.windows.named("m-statio") != nil {
		t.Fatal("it is still held")
	}
}

// A kind that is neither a machine nor a window is a mistake, not an
// instruction to change what the machine is.
func TestAKindThatIsNeitherIsRefused(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	if err := a.book.Put(remote.Host{
		Name: "statio", Address: "10.0.0.5", Window: true,
	}, ""); err != nil {
		t.Fatalf("Put: %v", err)
	}

	if err := a.openEditServer("statio"); err != nil {
		t.Fatalf("edit: %v", err)
	}
	f := awaitModal[*ui.Form](t, a, "a dialog", nil)
	retypeField(t, a, f, "Kind", "windwo")
	pressButton(t, a, f, "Save")
	a.pump.run()

	h, ok := a.book.Lookup("statio")
	if !ok {
		t.Fatal("the machine was lost")
	}
	if !h.Window {
		t.Fatal("a typo turned the window into a machine")
	}
}

// The kind chosen in the dialog is the kind that is saved.
//
// Tested through the dialog rather than by building the record: the
// record was always right, and what went wrong was that the field
// telling the user how to change it named keys that do nothing.
func TestTheKindChosenInTheDialogIsSaved(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)

	if err := a.openAddServer(); err != nil {
		t.Fatalf("add: %v", err)
	}
	f := awaitModal[*ui.Form](t, a, "a dialog", nil)
	typeIntoField(t, a, f, "Name", "statio")
	typeIntoField(t, a, f, "Server", "10.0.0.5:2222")

	// The way the dialog says to: the keys the hint names.
	kind := f.Field("Kind")
	if kind == nil {
		t.Fatal("the dialog has no Kind")
	}
	was := kind.Text()
	if _, err := kind.HandleKey(press(input.KeyDown, input.ModCtrl)); err != nil {
		t.Fatalf("stepping the Kind field: %v", err)
	}
	if kind.Text() == was {
		t.Fatalf("the keys the dialog names do nothing: it still says %q", was)
	}
	if kind.Text() != kindWindow {
		t.Fatalf("it stepped to %q, want %q", kind.Text(), kindWindow)
	}
	pressButton(t, a, f, "Save")
	a.pump.run()

	h, ok := a.book.Lookup("statio")
	if !ok {
		t.Fatal("it was not saved")
	}
	if !h.Window {
		t.Fatal("it was saved as a machine, not as a window")
	}
}

// And the hint names keys that really step the field.
func TestTheKindHintNamesKeysThatWork(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	if err := a.openAddServer(); err != nil {
		t.Fatalf("add: %v", err)
	}
	f := awaitModal[*ui.Form](t, a, "a dialog", nil)

	said := strings.Join(f.Lines, " ")
	if !strings.Contains(said, "ctrl+down") {
		t.Fatalf("the dialog says %q, which does not name the keys that step Kind", said)
	}
}

// Changing a saved machine into a window takes effect at once.
//
// Which machines there are and what each one is are two different
// questions, and changing the kind changes no name. The window read the
// kinds only when the list of names changed, so a machine edited into a
// window stayed known as a machine for the rest of the session: asking
// for a terminal on it logged in to it and was refused.
func TestChangingAMachineIntoAWindowTakesEffectAtOnce(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	withMenubar(t, a)

	if err := a.book.Put(remote.Host{
		Name: "statio", Address: "10.0.0.5", Port: 2222,
	}, ""); err != nil {
		t.Fatalf("Put: %v", err)
	}
	a.refreshServers()
	if a.about("statio").serves {
		t.Fatal("a machine is known as a window before it is one")
	}

	if err := a.openEditServer("statio"); err != nil {
		t.Fatalf("edit: %v", err)
	}
	f := awaitModal[*ui.Form](t, a, "a dialog", nil)
	retypeField(t, a, f, "Kind", kindWindow)
	pressButton(t, a, f, "Save")
	a.pump.run()

	if !a.about("statio").serves {
		t.Fatal("it is still known as a machine")
	}
	// And the command says what it now does, rather than offering a
	// terminal on something with no shell.
	cmd, ok := a.root.Commands.Lookup(termPrefix + remote.CommandName("statio"))
	if !ok {
		t.Fatal("there is no command for it")
	}
	if !strings.Contains(cmd.Title, "Take over") {
		t.Fatalf("the command is %q, want it to offer taking it over", cmd.Title)
	}
}

// A window cannot be made into a route to log in to, whoever asks.
//
// The guard that takes one over lives where connections are opened. This
// one lives where a route is built, so a caller that forgot to ask gets
// an answer it cannot ignore rather than an SSH login to a port with no
// shell behind it.
func TestAWindowCannotBeMadeIntoARoute(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	if err := a.book.Put(remote.Host{
		Name: "statio", Address: "10.0.0.5", Port: 2222, Window: true,
	}, ""); err != nil {
		t.Fatalf("Put: %v", err)
	}
	a.refreshServers()

	_, err := a.route("statio")
	if err == nil {
		t.Fatal("it built a route that would log in to a window")
	}
	if !strings.Contains(err.Error(), "taken over") {
		t.Errorf("it refused with %v, want it to say what to do instead", err)
	}
}

// Nothing dials except through openRoute, so the guard there cannot be
// walked around.
//
// Checked in the source rather than at runtime: the mistake each time
// was believing some other function was the one place everything went
// through, and only the callers can settle that.
func TestOnlyOpenRouteDials(t *testing.T) {
	dialers := callersOf(t, "remote.Connect", "Through")
	if len(dialers) == 0 {
		t.Fatal("nothing dials at all, so this proves nothing")
	}
	for _, at := range dialers {
		if !strings.HasPrefix(at, "route.go") {
			t.Errorf("something outside route.go dials: %s", at)
		}
	}
	routes := callersOf(t, "openRoute")
	if len(routes) == 0 {
		t.Fatal("nothing calls openRoute at all, so this proves nothing")
	}
	for _, at := range routes {
		switch {
		case strings.HasPrefix(at, "connect.go"), strings.HasPrefix(at, "servers.go"):
		default:
			t.Errorf("openRoute is called from %s, which the guard was not checked against", at)
		}
	}
}

// callersOf lists the file and line of everywhere the window's own source
// names one of these functions, leaving tests out.
//
// A name with a dot is a package's, so "remote.Connect" is that function
// and nothing else; a bare name matches a method or a function of that
// name however it is reached, and its own declaration does not count.
// Read from the syntax tree, so a method value counts and a name inside a
// comment or a string does not.
func callersOf(t *testing.T, names ...string) []string {
	t.Helper()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	wanted := make(map[string]bool, len(names))
	for _, name := range names {
		wanted[name] = true
	}
	fset := token.NewFileSet()
	var out []string
	for _, at := range files {
		if strings.HasSuffix(at, "_test.go") {
			continue
		}
		raw, err := os.ReadFile(at)
		if err != nil {
			t.Fatalf("read %s: %v", at, err)
		}
		file, err := parser.ParseFile(fset, at, raw, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", at, err)
		}
		declared := make(map[*ast.Ident]bool)
		for _, decl := range file.Decls {
			if fn, is := decl.(*ast.FuncDecl); is {
				declared[fn.Name] = true
			}
		}
		seen := make(map[string]bool)
		note := func(pos token.Pos) {
			where := fset.Position(pos)
			at := fmt.Sprintf("%s:%d", filepath.Base(where.Filename), where.Line)
			if !seen[at] {
				seen[at] = true
				out = append(out, at)
			}
		}
		ast.Inspect(file, func(n ast.Node) bool {
			switch node := n.(type) {
			case *ast.SelectorExpr:
				if pkg, is := node.X.(*ast.Ident); is && wanted[pkg.Name+"."+node.Sel.Name] {
					note(node.Sel.Pos())
				}
				if wanted[node.Sel.Name] {
					note(node.Sel.Pos())
				}
				// The Sel is this selector's own name, not a mention of
				// something else, so only the left-hand side is walked.
				ast.Inspect(node.X, func(ast.Node) bool { return true })
				return false
			case *ast.Ident:
				if !declared[node] && wanted[node.Name] {
					note(node.Pos())
				}
			}
			return true
		})
	}
	return out
}

// Taking a window over changes what the palette offers on it, in the
// frame it is taken over.
//
// The rebuild is skipped when nothing the commands are built from has
// changed, and taking a window over changes no name. So the palette
// went on offering to take over a window this one was already holding,
// for the rest of the session.
func TestTakingAWindowOverChangesWhatThePaletteOffers(t *testing.T) {
	_, client, addr, keyFile := aServingWindow(t)
	saveWindowFromTheDialog(t, client, "statio", addr, keyFile)

	id := termPrefix + remote.CommandName("statio")
	if got := commandTitle(t, client, id); got != "Take over statio" {
		t.Fatalf("before it is taken over the palette offers %q", got)
	}

	// The plus on its row, and the line that takes it over.
	clickTerminalLine(t, client, "statio")
	waitFor(t, client, "the window to be taken over", func() bool {
		return client.windows.count() == 1
	})

	if got := commandTitle(t, client, id); got != "Open a terminal on statio" {
		t.Errorf("the palette still offers %q on a window this one is holding", got)
	}
}

// commandTitle is what the palette lists a command as. The Servers menu
// shows a different one for the same machine: "Connect to it".
func commandTitle(t *testing.T, a *testApp, id string) string {
	t.Helper()
	cmd, ok := a.root.Commands.Lookup(id)
	if !ok {
		t.Fatalf("there is no command %q", id)
	}
	return cmd.Title
}
