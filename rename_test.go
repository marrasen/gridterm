package main

import (
	"slices"
	"strings"
	"testing"

	"github.com/marrasen/kakel/remote"
	"github.com/marrasen/kakel/vfs"
)

// A server renamed while connected takes what is open with it: its
// connection and its panes are under the new name, and letting go of it
// there lets go of it.
func TestRenamingAConnectedServerTakesWhatIsOpenWithIt(t *testing.T) {
	a, answering := dialApp(t)
	a.handle(OpenOn{Machine: "srv"})
	waitFor(t, a, "a shell on the server", func() bool { answering(); return oneShell(a) })
	h, _ := a.book.Lookup("srv")
	h.Name = "prod"
	if err := a.saveServer(SaveServer{Host: h, Under: "srv"}); err != nil {
		t.Fatal(err)
	}
	if a.conns["prod"] == nil || a.conns["srv"] != nil {
		t.Fatalf("renamed, the connections are %v", a.conns)
	}
	if !slices.ContainsFunc(a.st.Panes, func(p Pane) bool { return p.Machine == "prod" }) {
		t.Fatalf("renamed, the panes are %+v", a.st.Panes)
	}
	a.handle(Disconnect{Machine: "prod"})
	waitFor(t, a, "the connection to go", func() bool { return a.conns["prod"] == nil })
	if a.dropped["prod"] || a.dropped["srv"] {
		t.Fatalf("let go of on purpose, it is kept as dropped: %v", a.dropped)
	}
}

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
	err := a.connect(ConnectTo{Saved: "srv"})
	if err == nil || !strings.Contains(err.Error(), "Disconnect it first") {
		t.Fatalf("opening on it said %v", err)
	}
	if len(a.st.Panes) != panes {
		t.Fatal("a shell opened through the connection as it was")
	}
}

// A server being connected to is not renamed until that is over.
func TestAServerBeingConnectedToIsNotRenamed(t *testing.T) {
	a, _ := dialApp(t)
	a.dialing["srv"] = true
	h, _ := a.book.Lookup("srv")
	h.Name = "prod"
	if err := a.saveServer(SaveServer{Host: h, Under: "srv"}); err == nil {
		t.Fatal("a server being connected to was renamed")
	}
	if _, ok := a.book.Lookup("srv"); !ok {
		t.Fatal("the refused rename was saved")
	}
}

// A terminal open before its server was renamed follows its links
// under the new name.
func TestATerminalFollowsItsLinksAfterItsServerIsRenamed(t *testing.T) {
	a, answering := dialApp(t)
	a.handle(OpenOn{Machine: "srv"})
	waitFor(t, a, "a shell on the server", func() bool { answering(); return oneShell(a) })
	id := a.st.Panes[0].ID
	h, _ := a.book.Lookup("srv")
	h.Name = "prod"
	if err := a.saveServer(SaveServer{Host: h, Under: "srv"}); err != nil {
		t.Fatal(err)
	}
	if got := a.linkNames[id].get(); got != "prod" {
		t.Fatalf("renamed, the pane's links go to %q", got)
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

// A server renamed onto a name something is still open on is refused;
// onto one with only a log of before, it takes the name, and the log
// of the other machine goes.
func TestRenamingOntoANameInUse(t *testing.T) {
	a, answering := dialApp(t)
	a.handle(OpenOn{Machine: "srv"})
	waitFor(t, a, "a shell on the server", func() bool { answering(); return oneShell(a) })
	a.addPane(Pane{ID: "px", Title: "ended", Machine: "other", Ended: true}, nil, placement{})
	h, _ := a.book.Lookup("srv")
	h.Name = "other"
	if err := a.saveServer(SaveServer{Host: h, Under: "srv"}); err == nil {
		t.Fatal("renamed onto a name a pane is open on")
	}
	a.remove("px")
	a.account("other")
	a.dropped["other"] = true
	if err := a.saveServer(SaveServer{Host: h, Under: "srv"}); err != nil {
		t.Fatal(err)
	}
	if a.dropped["other"] || a.conns["other"] == nil {
		t.Fatalf("renamed, other is dropped %v, connected %v", a.dropped["other"], a.conns["other"] != nil)
	}
	if n := slices.Index(a.st.Accounts, "other"); n < 0 || slices.Index(a.st.Accounts[n+1:], "other") >= 0 {
		t.Fatalf("the logs are %v", a.st.Accounts)
	}
}

// A connection typed by the name a saved server has is not moved when
// the saved server is renamed.
func TestATypedConnectionStaysWhenTheSavedServerIsRenamed(t *testing.T) {
	a, _ := dialApp(t)
	typed := &remote.Conn{}
	a.conns["srv"] = typed
	a.connIDs["srv"] = ""
	t.Cleanup(func() {
		// Not a connection to close.
		for n, c := range a.conns {
			if c == typed {
				delete(a.conns, n)
			}
		}
	})
	h, _ := a.book.Lookup("srv")
	h.Name = "prod"
	if err := a.saveServer(SaveServer{Host: h, Under: "srv"}); err != nil {
		t.Fatal(err)
	}
	if a.conns["srv"] != typed || a.conns["prod"] != nil {
		t.Fatal("the typed connection moved with the saved server's name")
	}
}
