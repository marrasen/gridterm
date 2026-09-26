package main

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"sync"
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

// A password asked again on one connection says the last was refused,
// and a new key's question opens on Cancel.
func TestSigningInSaysAPasswordWasRefused(t *testing.T) {
	a, _ := dialApp(t)
	a.handle(ConnectTo{Saved: "srv"})
	waitFor(t, a, "the host key question", func() bool { return len(a.st.Asks) > 0 })
	if q := a.st.Asks[0]; !q.Careful || q.Danger {
		t.Fatalf("the host key question is %+v", q)
	}
	a.handle(Disconnect{Machine: "srv"})

	q := newAsker(a, "")
	said := make(chan string, 2)
	go func() {
		for range 2 {
			_, _ = q.Password(t.Context(), "tester", "srv")
		}
	}()
	for range 2 {
		waitFor(t, a, "the password question", func() bool { return len(a.st.Asks) > 0 && len(a.st.Asks[0].Prompts) > 0 })
		said <- a.st.Asks[0].Text
		a.handle(AskAnswered{ID: a.st.Asks[0].ID, Yes: true, Answers: []string{"x"}})
	}
	if first, second := <-said, <-said; first != "" || second != "Invalid password." {
		t.Fatalf("the questions said %q, then %q", first, second)
	}
}

func TestASignInLinkWaitsInAQuestionThatGoesWithTheDial(t *testing.T) {
	a, _ := dialApp(t)
	ctx, cancel := context.WithCancel(t.Context())
	var logged syncBuffer
	a.account("srv").Also(&logged)
	q := newAsker(a, "srv")
	q.Notice(ctx, remote.Notice{User: "tester", Host: "srv", Text: "Sign in at https://sso.example/device and enter ABCD"})
	waitFor(t, a, "the question", func() bool { return len(a.st.Asks) == 1 })
	ask := a.st.Asks[0]
	if ask.Title != "Waiting for server" || ask.Link != "https://sso.example/device" || !slices.Equal(ask.Actions, []string{"Open Link", "Copy"}) {
		t.Fatalf("asked %+v", ask)
	}
	if !strings.Contains(logged.String(), "https://sso.example/device") {
		t.Fatal("the connection log lacks the link")
	}
	cancel()
	waitFor(t, a, "the question to go", func() bool { return len(a.st.Asks) == 0 })
}

// syncBuffer is a buffer written from one goroutine and read from
// another.
type syncBuffer struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
