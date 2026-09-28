package main

import (
	"slices"
	"strings"
	"testing"
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
