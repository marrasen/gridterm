package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/gunimtest"

	"github.com/marrasen/kakel/internal/sshtest"
	"github.com/marrasen/kakel/internal/testhome"
	"github.com/marrasen/kakel/remote"
)

// jumpApp is a program side with a saved jump host, "bastion", and two
// servers behind it, "inner1" and "inner2", all of them the test SSH
// server, which forwards to itself. "far" is behind "mid", behind the
// bastion, and mid answers nowhere.
func jumpApp(t *testing.T) (a *app, s *sshtest.Server, answering func()) {
	t.Helper()
	testhome.New(t)
	t.Setenv("SSH_AUTH_SOCK", "")
	s = sshtest.New(t)
	host, port := s.Host()
	w := gunimtest.New(t, geom.Sz(400, 300), nil)
	a = newApp(w.Client(), &shells{m: map[string]*shell{}})
	a.ctx = t.Context()
	book, err := remote.LoadBook(filepath.Join(t.TempDir(), "servers.json"))
	if err != nil {
		t.Fatal(err)
	}
	put := func(h remote.Host) string {
		t.Helper()
		if err := book.Put(h, ""); err != nil {
			t.Fatal(err)
		}
		got, ok := book.Lookup(h.Name)
		if !ok {
			t.Fatalf("%s was not saved", h.Name)
		}
		return got.ID
	}
	bastion := put(remote.Host{Name: "bastion", Address: host, Port: port, User: "tester"})
	put(remote.Host{Name: "inner1", Address: host, Port: port, User: "tester", Via: bastion})
	put(remote.Host{Name: "inner2", Address: host, Port: port, User: "tester", Via: bastion})
	mid := put(remote.Host{Name: "mid", Address: "127.0.0.1", Port: 1, User: "tester", Via: bastion})
	put(remote.Host{Name: "far", Address: host, Port: port, User: "tester", Via: mid})
	a.book = book
	a.st.Saved = book.Hosts()
	t.Cleanup(func() {
		for len(a.st.Panes) > 0 {
			a.remove(a.st.Panes[0].ID)
		}
		a.hangUp()
	})
	return a, s, func() {
		for _, q := range a.st.Asks {
			ans := AskAnswered{ID: q.ID, Yes: true}
			if len(q.Prompts) > 0 {
				ans.Answers = []string{sshtest.Password}
			}
			a.handle(ans)
		}
	}
}

// A second server behind a jump host goes through the connection to it
// the first made, rather than signing in to it again, and the jump host
// closes once neither needs it.
func TestServersBehindAJumpHostShareIt(t *testing.T) {
	a, s, answering := jumpApp(t)
	a.handle(ConnectTo{Saved: "inner1"})
	waitFor(t, a, "inner1", func() bool { answering(); return a.conns["inner1"] != nil })
	if n := s.Conns(); n != 2 {
		t.Fatalf("reaching inner1 signed in %d times, want 2: the jump host and inner1", n)
	}
	a.handle(ConnectTo{Saved: "inner2"})
	waitFor(t, a, "inner2", func() bool { answering(); return a.conns["inner2"] != nil })
	if n := s.Conns(); n != 3 {
		t.Fatalf("reaching inner2 as well signed in %d times, want 3: the jump host once", n)
	}
	a.handle(Disconnect{Machine: "inner1"})
	waitFor(t, a, "inner1 to go", func() bool { return a.conns["inner1"] == nil })
	waitFor(t, a, "inner1 to close", func() bool { return s.Live() == 2 })
	a.handle(Disconnect{Machine: "inner2"})
	waitFor(t, a, "inner2 to go", func() bool { return a.conns["inner2"] == nil })
	waitFor(t, a, "the jump host to close", func() bool { return s.Live() == 0 })
	if len(a.hops) != 0 || len(a.hopUsers) != 0 {
		t.Fatalf("with nothing behind it, the jump host is still kept: %v, %v", a.hops, a.hopUsers)
	}
}

// A jump host the user connected to is theirs: going through it signs
// in to nothing twice, and it stays once what went through it has gone.
func TestAJumpHostConnectedToStays(t *testing.T) {
	a, s, answering := jumpApp(t)
	a.handle(ConnectTo{Saved: "bastion"})
	waitFor(t, a, "the bastion", func() bool { answering(); return a.conns["bastion"] != nil })
	a.handle(ConnectTo{Saved: "inner1"})
	waitFor(t, a, "inner1", func() bool { answering(); return a.conns["inner1"] != nil })
	if n := s.Conns(); n != 2 {
		t.Fatalf("signed in %d times, want 2", n)
	}
	a.handle(Disconnect{Machine: "inner1"})
	waitFor(t, a, "inner1 to go", func() bool { return a.conns["inner1"] == nil })
	waitFor(t, a, "inner1 to close", func() bool { return s.Live() == 1 })
	if c := a.conns["bastion"]; c == nil || c.Closed() {
		t.Fatal("the bastion the user connected to closed with what went through it")
	}
}

// A route that fails part way closes the jump hosts it reached, and
// says which hop it failed at.
func TestAFailedRouteClosesItsHopsAndNamesTheOneThatFailed(t *testing.T) {
	a, s, answering := jumpApp(t)
	a.handle(ConnectTo{Saved: "far"})
	waitFor(t, a, "the dial to fail", func() bool { answering(); return len(a.dialing) == 0 && len(a.st.Notices) > 0 })
	if n := a.st.Notices[len(a.st.Notices)-1]; !strings.Contains(n.Body, "through mid") {
		t.Fatalf("the failure says %q, want it to name mid", n.Body)
	}
	waitFor(t, a, "the jump host to close", func() bool { return s.Live() == 0 })
	if len(a.hops) != 0 || len(a.hopUsers) != 0 {
		t.Fatalf("a failed route kept its hops: %v, %v", a.hops, a.hopUsers)
	}
}

// A dial going through a jump host holds it: the server that was going
// through it may leave meanwhile, and the jump host stays for the dial.
func TestADialHoldsTheJumpHostItGoesThrough(t *testing.T) {
	a, s, answering := jumpApp(t)
	a.handle(ConnectTo{Saved: "inner1"})
	waitFor(t, a, "inner1", func() bool { answering(); return a.conns["inner1"] != nil })
	// inner2's dial waits on its password, unanswered for now.
	a.handle(ConnectTo{Saved: "inner2"})
	waitFor(t, a, "inner2's password question", func() bool { return len(a.st.Asks) > 0 })
	a.handle(Disconnect{Machine: "inner1"})
	waitFor(t, a, "inner1 to go", func() bool { return a.conns["inner1"] == nil })
	waitFor(t, a, "inner2", func() bool { answering(); return a.conns["inner2"] != nil || len(a.dialing) == 0 })
	if a.conns["inner2"] == nil {
		t.Fatalf("inner2 did not connect: %+v", a.st.Notices)
	}
	if n := s.Conns(); n != 3 {
		t.Fatalf("signed in %d times, want 3: the jump host once", n)
	}
}

// A jump host whose saved server has changed is not gone through again:
// the connection to it is to where it was.
func TestAChangedJumpHostIsNotGoneThroughAgain(t *testing.T) {
	a, _, answering := jumpApp(t)
	a.handle(ConnectTo{Saved: "inner1"})
	waitFor(t, a, "inner1", func() bool { answering(); return a.conns["inner1"] != nil })
	b, _ := a.book.Lookup("bastion")
	b.Address, b.Port = "127.0.0.1", 1
	if err := a.book.Put(b, "bastion"); err != nil {
		t.Fatal(err)
	}
	a.handle(ConnectTo{Saved: "inner2"})
	waitFor(t, a, "the dial to end", func() bool { answering(); return len(a.dialing) == 0 })
	if a.conns["inner2"] != nil {
		t.Fatal("inner2 connected through the bastion as it was before the change")
	}
}

// Disconnecting a jump host the user connected to takes the servers
// reached through it with it, as let go of rather than dropped.
func TestDisconnectingAJumpHostLetsGoOfWhatWentThroughIt(t *testing.T) {
	a, _, answering := jumpApp(t)
	a.handle(ConnectTo{Saved: "bastion"})
	waitFor(t, a, "the bastion", func() bool { answering(); return a.conns["bastion"] != nil })
	a.handle(ConnectTo{Saved: "inner1"})
	waitFor(t, a, "inner1", func() bool { answering(); return a.conns["inner1"] != nil })
	a.handle(Disconnect{Machine: "bastion"})
	waitFor(t, a, "both to go", func() bool { return a.conns["bastion"] == nil && a.conns["inner1"] == nil })
	if a.dropped["inner1"] || a.dropped["bastion"] {
		t.Fatalf("let go of on purpose, they are kept as dropped: %v", a.dropped)
	}
}
