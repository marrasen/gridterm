package app

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/marrasen/kakel/screen"

	"github.com/marrasen/kakel/machines"

	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/gunimtest"

	"github.com/marrasen/kakel/internal/sshtest"
	"github.com/marrasen/kakel/internal/testhome"
	"github.com/marrasen/kakel/remote"
	"github.com/marrasen/kakel/vfs"
)

// realIDApp is dialApp with the IDs the server list gives, not names:
// what tests of an ID and a name told apart need. It returns srv's ID.
func realIDApp(t *testing.T) (a *app, srv machines.ID, answering func()) {
	t.Helper()
	testhome.New(t)
	t.Setenv("SSH_AUTH_SOCK", "")
	s := sshtest.New(t)
	host, port := s.Host()
	a = newApp(gunimtest.New(t, geom.Sz(400, 300), nil).Client(), screen.NewShells())
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
	return a, machines.ID(h.ID), func() {
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
	if a.machines.Get(srv).Conn == nil || a.st.Panes[0].Machine != srv || a.machines.Name(srv) != "srv" {
		t.Fatalf("connected, it is kept as %v, the pane on %q, called %q", a.machines.Connected(), a.st.Panes[0].Machine, a.machines.Name(srv))
	}
	h, _ := a.book.LookupID(string(srv))
	h.Name = "prod"
	if err := a.saveServer(SaveServer{Host: h, Under: "srv"}); err != nil {
		t.Fatal(err)
	}
	if a.machines.Get(srv).Conn == nil || a.machines.Name(srv) != "prod" {
		t.Fatalf("renamed, it is kept as %v, called %q", a.machines.Connected(), a.machines.Name(srv))
	}
	h.Address, h.Port = "127.0.0.1", 1
	if err := a.saveServer(SaveServer{Host: h, Under: "prod"}); err != nil {
		t.Fatal(err)
	}
	if err := a.open(srv, Placement{}); err == nil || !strings.Contains(err.Error(), "Disconnect it first") {
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
	waitFor(t, a, "the connection to go", func() bool { return a.machines.Get(srv).Conn == nil })
	if a.machines.Name(srv) != "srv" {
		t.Fatalf("removed, with its pane open, it is called %q", a.machines.Name(srv))
	}
	a.remove(a.st.Panes[0].ID)
	a.publish()
	if slices.Contains(a.st.Accounts, srv) || a.machines.Get(srv).Log != nil {
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
	if err := a.runCommand(RunCommand{Machine: machines.ID(box.ID), Line: "uptime"}); err == nil || !strings.Contains(err.Error(), "box is a kakel window") {
		t.Fatalf("a command on a saved window said %v", err)
	}
}

// Work kept on a machine beyond a window finds it again: the window as
// kept, and the machine by the window's name for it, which lasts where
// the window's key for a quick connection there does not.
func TestWorkKeptBeyondAWindowFindsItAgain(t *testing.T) {
	a, _, _ := realIDApp(t)
	win := a.machines.NewQuick("desk:7777", true)
	a.machines.At(win).Window = &machines.Window{Bound: map[string]string{}}
	t.Cleanup(func() { a.machines.At(win).Window = nil })
	far := machines.FarID(win, "3fa2c1d4e5f6a7b8")
	a.machines.NameFar(win, "3fa2c1d4e5f6a7b8", "db")
	if name := a.machines.Name(far); name != "db through desk:7777" {
		t.Fatalf("it is called %q", name)
	}
	kept := a.keptAs(far)
	if strings.Contains(kept, "quick-") || strings.Contains(kept, "3fa2c1d4e5f6a7b8") {
		t.Fatalf("kept as %q, with a key in it", kept)
	}
	got, err := a.machineNow(kept, a.serverID(far))
	if want := win + farSep + "db"; got != want || err != nil {
		t.Fatalf("kept as %q, it is found as %q, %v; want %q", kept, got, err, want)
	}
}

// What goes wrong with a server's files names the server, not its ID,
// and its new name once it is renamed.
func TestAServersFilesSayWhatWentWrongByItsName(t *testing.T) {
	a, srv, answering := realIDApp(t)
	a.handle(ConnectTo{Server: srv})
	waitFor(t, a, "a shell there", func() bool { answering(); return oneShell(a) })
	var files vfs.FS
	if err := a.withFiles(srv, func(f vfs.FS) { files = f }); err != nil {
		t.Fatal(err)
	}
	waitFor(t, a, "the files", func() bool { return files != nil })
	_, err := files.ReadDir("/no-such-folder-here")
	if err == nil || !strings.Contains(err.Error(), "srv") || strings.Contains(err.Error(), string(srv)) {
		t.Fatalf("its files said %v", err)
	}
	h, _ := a.book.LookupID(string(srv))
	h.Name = "prod"
	if err := a.saveServer(SaveServer{Host: h, Under: "srv"}); err != nil {
		t.Fatal(err)
	}
	if _, err := files.ReadDir("/no-such-folder-here"); err == nil || !strings.Contains(err.Error(), "prod") {
		t.Fatalf("renamed, its files said %v", err)
	}
}
