package main

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/marrasen/kakel/vfs"
)

// A server saved at another address while connected is not opened on
// through the old connection.
func TestAServerChangedWhileConnectedIsNotOpenedOnAsItWas(t *testing.T) {
	a, answering := dialApp(t)
	a.handle(OpenOn{Machine: "srv"})
	waitFor(t, a, "a shell on the server", func() bool { answering(); return oneShell(a) })
	h, _ := a.book.Lookup("srv")
	h.Address, h.Port = "127.0.0.1", 1
	if err := a.saveServer(SaveServer{Host: h, Under: "srv"}); err != nil {
		t.Fatal(err)
	}
	panes := len(a.st.Panes)
	err := a.connect(ConnectTo{Server: "srv"})
	if err == nil || !strings.Contains(err.Error(), "Disconnect it first") {
		t.Fatalf("opening on it said %v", err)
	}
	if len(a.st.Panes) != panes {
		t.Fatal("a shell opened through the connection as it was")
	}
}

// Every way of opening something on a server changed since it was
// connected is refused, not only Connect.
func TestNothingOpensOnAServerChangedSinceItConnected(t *testing.T) {
	a, answering := dialApp(t)
	a.handle(OpenOn{Machine: "srv"})
	waitFor(t, a, "a shell on the server", func() bool { answering(); return oneShell(a) })
	h, _ := a.book.Lookup("srv")
	h.Address, h.Port = "127.0.0.1", 1
	if err := a.saveServer(SaveServer{Host: h, Under: "srv"}); err != nil {
		t.Fatal(err)
	}
	panes := len(a.st.Panes)
	if err := a.open("srv", placement{}); err == nil || !strings.Contains(err.Error(), "Disconnect it first") {
		t.Fatalf("a terminal on it said %v", err)
	}
	if err := a.runCommand(RunCommand{Machine: "srv", Line: "true"}); err == nil || !strings.Contains(err.Error(), "Disconnect it first") {
		t.Fatalf("a command on it said %v", err)
	}
	if err := a.withFiles("srv", func(vfs.FS) {}); err == nil || !strings.Contains(err.Error(), "Disconnect it first") {
		t.Fatalf("its files said %v", err)
	}
	if len(a.st.Panes) != panes {
		t.Fatal("something opened through the connection as it was")
	}
}

// A server renamed while connected is only renamed: its connection and
// its panes go by its ID, and stay as they are, under the new name.
func TestRenamingAConnectedServerIsOnlyANewName(t *testing.T) {
	a, answering := dialApp(t)
	a.handle(OpenOn{Machine: "srv"})
	waitFor(t, a, "a shell on the server", func() bool { answering(); return oneShell(a) })
	h, _ := a.book.LookupID("srv")
	h.Name = "prod"
	if err := a.saveServer(SaveServer{Host: h, Under: "srv"}); err != nil {
		t.Fatal(err)
	}
	if a.conns["srv"] == nil || a.st.Panes[0].Machine != "srv" {
		t.Fatalf("renamed, the connection moved: %v, the pane is on %q", a.conns, a.st.Panes[0].Machine)
	}
	if got := a.nameOf("srv"); got != "prod" {
		t.Fatalf("renamed, it is called %q", got)
	}
	a.publish()
	if i := slices.IndexFunc(a.st.Machines, func(m Machine) bool { return m.ID == "srv" }); i < 0 || a.st.Machines[i].Name != "prod" {
		t.Fatalf("the window is told %+v", a.st.Machines)
	}
	a.handle(Disconnect{Machine: "srv"})
	waitFor(t, a, "the connection to go", func() bool { return a.conns["srv"] == nil })
	if a.dropped["srv"] {
		t.Fatal("let go of on purpose, it is kept as dropped")
	}
}

// A quick connection, typed rather than saved, gets an ID of its own,
// is called by its address, and is forgotten once it is not connected
// and nothing is open on it.
func TestAQuickConnectionIsForgottenOnceNothingIsOpenOnIt(t *testing.T) {
	a, answering := dialApp(t)
	h, _ := a.book.LookupID("srv")
	target := fmt.Sprintf("tester@%s:%d", h.Address, h.Port)
	a.handle(ConnectTo{Target: target})
	waitFor(t, a, "a shell there", func() bool { answering(); return oneShell(a) })
	id := a.st.Panes[0].Machine
	if !strings.HasPrefix(string(id), "quick-") || a.conns[id] == nil {
		t.Fatalf("typed, it is kept as %q", id)
	}
	if got := a.nameOf(id); got != target {
		t.Fatalf("it is called %q, want %q", got, target)
	}
	a.publish()
	if i := slices.IndexFunc(a.st.Machines, func(m Machine) bool { return m.ID == id }); i < 0 || !a.st.Machines[i].Quick {
		t.Fatalf("the window is told %+v", a.st.Machines)
	}
	// Its connection gone, with its pane open, it is kept.
	a.handle(Disconnect{Machine: id})
	waitFor(t, a, "the connection to go", func() bool { return a.conns[id] == nil })
	a.publish()
	if !a.isQuick(id) {
		t.Fatal("with its pane open, the quick connection was forgotten")
	}
	a.remove(a.st.Panes[0].ID)
	a.publish()
	if a.isQuick(id) || slices.ContainsFunc(a.st.Machines, func(m Machine) bool { return m.ID == id }) {
		t.Fatal("with nothing open on it, the quick connection is kept")
	}
}

// A quick connection whose connection went is connected to again as
// itself: the same ID, its panes on it still.
func TestAQuickConnectionReconnectsAsItself(t *testing.T) {
	a, answering := dialApp(t)
	h, _ := a.book.LookupID("srv")
	a.handle(ConnectTo{Target: fmt.Sprintf("tester@%s:%d", h.Address, h.Port)})
	waitFor(t, a, "a shell there", func() bool { answering(); return oneShell(a) })
	id := a.st.Panes[0].Machine
	_ = a.conns[id].Close() // as if the network went
	waitFor(t, a, "the drop", func() bool { return a.conns[id] == nil })
	if err := a.dialAgain(id, nil); err != nil {
		t.Fatal(err)
	}
	waitFor(t, a, "the connection back", func() bool { answering(); return a.conns[id] != nil })
	if len(a.quick) != 1 {
		t.Fatalf("reconnected, there are %d quick connections", len(a.quick))
	}
}

// A saved server removed while its pane is open is named as it was
// until that pane goes.
func TestARemovedServerIsNamedWhileSomethingIsOpenOnIt(t *testing.T) {
	a, answering := dialApp(t)
	a.handle(OpenOn{Machine: "srv"})
	waitFor(t, a, "a shell on the server", func() bool { answering(); return oneShell(a) })
	if err := a.removeServer("srv"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, a, "the connection to go", func() bool { return a.conns["srv"] == nil })
	if got := a.nameOf("srv"); got != "srv" {
		t.Fatalf("removed, it is called %q", got)
	}
	a.remove(a.st.Panes[0].ID)
	a.publish()
	if _, kept := a.goneNames["srv"]; kept {
		t.Fatal("with nothing open on it, its name is still kept")
	}
}
