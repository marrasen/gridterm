package main

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/gunimtest"

	"github.com/marrasen/kakel/internal/sshtest"
	"github.com/marrasen/kakel/internal/testhome"
	"github.com/marrasen/kakel/remote"
)

// realIDApp is dialApp with the IDs the server list gives, not names:
// what tests of an ID and a name told apart need. It returns srv's ID.
func realIDApp(t *testing.T) (a *app, srv string, answering func()) {
	t.Helper()
	testhome.New(t)
	t.Setenv("SSH_AUTH_SOCK", "")
	s := sshtest.New(t)
	host, port := s.Host()
	a = newApp(gunimtest.New(t, geom.Sz(400, 300), nil).Client(), &shells{m: map[string]*shell{}})
	a.ctx = t.Context()
	book, err := remote.LoadBook(filepath.Join(t.TempDir(), "servers.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := book.Put(remote.Host{Name: "srv", Address: host, Port: port, User: "tester"}, ""); err != nil {
		t.Fatal(err)
	}
	a.book = book
	a.st.Saved = book.Hosts()
	h, _ := book.Lookup("srv")
	if h.ID == "srv" {
		t.Fatal("the list gave srv its name for an ID")
	}
	t.Cleanup(func() {
		for len(a.st.Panes) > 0 {
			a.remove(a.st.Panes[0].ID)
		}
		a.hangUp()
	})
	return a, h.ID, func() {
		for _, q := range a.st.Asks {
			ans := AskAnswered{ID: q.ID, Yes: true}
			if len(q.Prompts) > 0 {
				ans.Answers = []string{sshtest.Password}
			}
			a.handle(ans)
		}
	}
}

// With the IDs the list gives: a connection goes by the ID, is named by
// the name, keeps going through a rename, and is refused once the
// server is saved at another address.
func TestAServerGoesByItsIDAndIsNamedByItsName(t *testing.T) {
	a, srv, answering := realIDApp(t)
	a.handle(ConnectTo{Server: srv})
	waitFor(t, a, "a shell there", func() bool { answering(); return oneShell(a) })
	if a.conns[srv] == nil || a.st.Panes[0].Machine != srv || a.nameOf(srv) != "srv" {
		t.Fatalf("connected, it is kept as %v, the pane on %q, called %q", a.conns, a.st.Panes[0].Machine, a.nameOf(srv))
	}
	h, _ := a.book.LookupID(srv)
	h.Name = "prod"
	if err := a.saveServer(SaveServer{Host: h, Under: "srv"}); err != nil {
		t.Fatal(err)
	}
	if a.conns[srv] == nil || a.nameOf(srv) != "prod" {
		t.Fatalf("renamed, it is kept as %v, called %q", a.conns, a.nameOf(srv))
	}
	h.Address, h.Port = "127.0.0.1", 1
	if err := a.saveServer(SaveServer{Host: h, Under: "prod"}); err != nil {
		t.Fatal(err)
	}
	if err := a.open(srv, placement{}); err == nil || !strings.Contains(err.Error(), "Disconnect it first") {
		t.Fatalf("saved at another address, a terminal on it said %v", err)
	}
	if err := a.connect(ConnectTo{Server: srv}); err == nil || !strings.Contains(err.Error(), "prod") {
		t.Fatalf("saved at another address, connecting said %v", err)
	}
}

// A removed server's log goes once nothing is open on it, rather than
// staying named by its ID.
func TestARemovedServersLogGoesWithIt(t *testing.T) {
	a, srv, answering := realIDApp(t)
	a.handle(ConnectTo{Server: srv})
	waitFor(t, a, "a shell there", func() bool { answering(); return oneShell(a) })
	if err := a.removeServer(srv); err != nil {
		t.Fatal(err)
	}
	waitFor(t, a, "the connection to go", func() bool { return a.conns[srv] == nil })
	if a.nameOf(srv) != "srv" {
		t.Fatalf("removed, with its pane open, it is called %q", a.nameOf(srv))
	}
	a.remove(a.st.Panes[0].ID)
	a.publish()
	if slices.Contains(a.st.Accounts, srv) || a.accounts[srv] != nil {
		t.Fatalf("with nothing open on it, its log is kept: %v", a.st.Accounts)
	}
}

// A command on a saved kakel window, by its ID, is refused as one.
func TestACommandOnASavedWindowByItsIDIsRefused(t *testing.T) {
	a, _, _ := realIDApp(t)
	if err := a.book.Put(remote.Host{Name: "box", Address: "127.0.0.1", Port: 1, Window: true}, ""); err != nil {
		t.Fatal(err)
	}
	box, _ := a.book.Lookup("box")
	if err := a.runCommand(RunCommand{Machine: box.ID, Line: "uptime"}); err == nil || !strings.Contains(err.Error(), "box is a kakel window") {
		t.Fatalf("a command on a saved window said %v", err)
	}
}

// Work kept on a machine beyond a window finds it again: the window as
// kept, and the window's own key for the machine.
func TestWorkKeptBeyondAWindowFindsItAgain(t *testing.T) {
	a, _, _ := realIDApp(t)
	win := a.newQuick("desk:7777", true)
	a.windows[win] = &remoteWin{bound: map[string]string{}, farNames: map[string]string{"k1": "db"}}
	t.Cleanup(func() { delete(a.windows, win) })
	far := win + farSep + "k1"
	kept := a.keptAs(far)
	if strings.Contains(kept, "quick-") {
		t.Fatalf("kept as %q, with a quick connection's ID in it", kept)
	}
	got, err := a.machineNow(kept, a.serverID(far))
	if got != far || err != nil {
		t.Fatalf("kept as %q, it is found as %q, %v; want %q", kept, got, err, far)
	}
	if name := a.nameOf(far); name != "db through desk:7777" {
		t.Fatalf("it is called %q", name)
	}
}

// Something kept on a saved server is found in a dialog for it by its
// ID, and something kept on a quick connection by its address.
func TestKeptWorkIsFoundForItsMachine(t *testing.T) {
	win, _, publish := windowStage(t)
	publish(State{Machines: []Machine{{ID: "a1", Name: "srv"}, {ID: "quick-1", Name: "me@typed", Quick: true}}})
	for _, c := range []struct {
		host, id, machine string
		want              bool
	}{
		{"old name", "a1", "a1", true},
		// Kept before servers had IDs, by its name.
		{"srv", "", "a1", true},
		{"me@typed", "", "quick-1", true},
		{"", "", "", true},
		{"", "", "a1", false},
	} {
		if got := win.keptFor(c.host, c.id, c.machine); got != c.want {
			t.Errorf("kept on %q (%q), for %q: %v, want %v", c.host, c.id, c.machine, got, c.want)
		}
	}
}
