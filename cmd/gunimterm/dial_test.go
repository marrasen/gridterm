package main

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/widget"

	"github.com/marrasen/gridterm/internal/sshtest"
	"github.com/marrasen/gridterm/remote"
)

// dialApp is a program side and the test SSH server, saved as "srv",
// with a way to answer what connecting asks.
func dialApp(t *testing.T) (a *app, answering func()) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("SSH_AUTH_SOCK", "")
	s := sshtest.New(t)
	host, port := s.Host()
	w := gunim.NewOffscreen(geom.Sz(400, 300), nil)
	a = newApp(w.Client(), &shells{m: map[string]*shell{}})
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
	t.Cleanup(func() {
		for len(a.st.Panes) > 0 {
			a.remove(a.st.Panes[0].ID)
		}
		for _, c := range a.conns {
			_ = c.Close()
		}
	})
	return a, func() {
		for _, q := range a.st.Asks {
			ans := AskAnswered{ID: q.ID, Yes: true}
			if len(q.Prompts) > 0 {
				ans.Answers = []string{sshtest.Password}
			}
			a.handle(ans)
		}
	}
}

func TestADialIsGivenUp(t *testing.T) {
	a, _ := dialApp(t)
	a.handle(ConnectTo{Saved: "srv"})
	waitFor(t, a, "the host key question", func() bool { return len(a.st.Asks) > 0 })
	a.handle(Disconnect{Machine: "srv"})
	waitFor(t, a, "the dial to end", func() bool { return len(a.dialing) == 0 && len(a.st.Asks) == 0 })
	if len(a.st.Panes) != 0 || len(a.conns) != 0 {
		t.Fatalf("given up, there are panes %+v and connections %v", a.st.Panes, a.conns)
	}
	if len(a.st.Notices) != 0 {
		t.Fatalf("given up on purpose, it said %+v", a.st.Notices)
	}
}

func TestRemovingAServerClosesItsConnection(t *testing.T) {
	a, answering := dialApp(t)
	a.handle(ConnectTo{Saved: "srv"})
	waitFor(t, a, "a shell on the server", func() bool { answering(); return len(a.st.Panes) == 1 })
	a.handle(RemoveServer{Name: "srv"})
	waitFor(t, a, "the connection to close", func() bool { return a.conns["srv"] == nil && a.st.Panes[0].Ended })
	if len(a.st.Saved) != 0 {
		t.Fatalf("removed, the list is %+v", a.st.Saved)
	}
}

func TestTheRemoveQuestionSaysWhatItCloses(t *testing.T) {
	win, _, publish := windowStage(t)
	publish(State{Panes: []Pane{{ID: "p1", Title: "Terminal 1", Machine: "srv"}}, Connected: []string{"srv"}, Dialing: []string{"far"}})
	if got := win.removeSays("srv"); got != "srv is connected. Removing it closes the connection and everything through it: 1 pane." {
		t.Fatalf("for a connected server it says %q", got)
	}
	if got := win.removeSays("far"); got != "Removing it cancels the connection in progress." {
		t.Fatalf("for a server being connected to it says %q", got)
	}
	if got := win.removeSays("idle"); got != "" {
		t.Fatalf("for a server holding nothing it says %q", got)
	}
}

func TestConnectingAgainWhileConnectingAsks(t *testing.T) {
	a, answering := dialApp(t)
	a.handle(ConnectTo{Saved: "srv"})
	waitFor(t, a, "the host key question", func() bool { return len(a.st.Asks) > 0 })
	a.handle(ConnectTo{Saved: "srv"})
	waitFor(t, a, "the second question", func() bool { return len(a.st.Asks) == 2 })
	q := a.st.Asks[1]
	if q.Title != "Already connecting to srv" || !slices.Equal(q.Choose, []string{"Wait", "Retry"}) {
		t.Fatalf("asked %+v", q)
	}
	a.handle(AskAnswered{ID: q.ID, Yes: true, Answers: []string{"Wait"}})
	// Waited for, the first lands and the second opens a shell too.
	waitFor(t, a, "two shells", func() bool { answering(); return len(a.st.Panes) == 2 })
}

func TestOpeningOnASavedServerConnectsFirst(t *testing.T) {
	a, answering := dialApp(t)
	a.handle(OpenOn{Machine: "srv"})
	waitFor(t, a, "a shell on the server", func() bool { answering(); return len(a.st.Panes) == 1 })
	if a.st.Panes[0].Machine != "srv" || a.conns["srv"] == nil {
		t.Fatalf("opened %+v", a.st.Panes)
	}
}

func TestSavedServersAreListedWithAWayToConnect(t *testing.T) {
	rows := sidebarRows(nil, nil, Share{}, nil, []string{"desk"})
	if !slices.ContainsFunc(rows, func(r sideItem) bool { return r.key == "machine:desk" && r.heading }) {
		t.Fatalf("a saved server has no heading: %+v", rows)
	}
	win, _, publish := windowStage(t)
	publish(State{Sidebar: true, SidebarWidth: 220})
	if _, ok := widget.RowOf[*sideRow](win.list, "connect:new"); !ok {
		t.Fatal("the sidebar has no way to connect to a server")
	}
}
