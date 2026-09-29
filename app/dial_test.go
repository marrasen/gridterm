package app

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/marrasen/kakel/screen"

	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/gunimtest"

	"github.com/marrasen/kakel/internal/sshtest"
	"github.com/marrasen/kakel/internal/testhome"
	"github.com/marrasen/kakel/internal/update"
	"github.com/marrasen/kakel/remote"
)

// dialApp is a program side and the test SSH server, saved as "srv",
// with a way to answer what connecting asks.
func dialApp(t *testing.T) (a *app, answering func()) {
	t.Helper()
	testhome.New(t)
	t.Setenv("SSH_AUTH_SOCK", "")
	s := sshtest.New(t)
	host, port := s.Host()
	w := gunimtest.New(t, geom.Sz(400, 300), nil)
	a = newApp(w.Client(), screen.NewShells())
	a.ctx = t.Context()
	book, err := remote.LoadBook(filepath.Join(t.TempDir(), "servers.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := book.Put(remote.Host{Name: "srv", Address: host, Port: port, User: "tester"}, ""); err != nil {
		t.Fatal(err)
	}
	idsAsNames(t, book)
	a.book = book
	a.st.Saved = book.Hosts()
	t.Cleanup(func() {
		for len(a.st.Panes) > 0 {
			a.remove(a.st.Panes[0].ID)
		}
		for _, id := range a.machines.Connected() {
			c := a.machines.Get(id).Conn
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

// oneShell reports whether the one pane is a shell: the connection log
// shown while connecting has folded away into it.
func oneShell(a *app) bool {
	return len(a.st.Panes) == 1 && a.st.Panes[0].Kind != KindLog
}

func TestADialIsGivenUp(t *testing.T) {
	a, _ := dialApp(t)
	a.handle(ConnectTo{Server: "srv"})
	waitFor(t, a, "the host key question", func() bool { return len(a.st.Asks) > 0 })
	a.handle(Disconnect{Machine: "srv"})
	waitFor(t, a, "the dial to end", func() bool { return len(a.machines.Dialing()) == 0 && len(a.st.Asks) == 0 })
	if len(a.st.Panes) != 0 || len(a.machines.Connected()) != 0 {
		t.Fatalf("given up, there are panes %+v and connections %v", a.st.Panes, a.machines.Connected())
	}
	if len(a.st.Notices) != 0 {
		t.Fatalf("given up on purpose, it said %+v", a.st.Notices)
	}
}

func TestRemovingAServerClosesItsConnection(t *testing.T) {
	a, answering := dialApp(t)
	a.handle(ConnectTo{Server: "srv"})
	waitFor(t, a, "a shell on the server", func() bool { answering(); return oneShell(a) })
	a.handle(RemoveServer{ID: "srv"})
	waitFor(t, a, "the connection to close", func() bool { return a.machines.Get("srv").Conn == nil && a.st.Panes[0].Ended })
	if len(a.st.Saved) != 0 {
		t.Fatalf("removed, the list is %+v", a.st.Saved)
	}
}

// Removed after its connection went, a server takes what it left along.
func TestRemovingADroppedServerClearsIt(t *testing.T) {
	a, answering := dialApp(t)
	a.handle(ConnectTo{Server: "srv"})
	waitFor(t, a, "a shell", func() bool { answering(); return oneShell(a) })
	_ = a.machines.Get("srv").Conn.Close() // as if the network went
	waitFor(t, a, "the drop", func() bool { return a.machines.Get("srv").Dropped && a.st.Panes[0].Ended })
	a.handle(RemoveServer{ID: "srv"})
	if len(a.machines.Dropped()) != 0 || len(a.st.Panes) != 0 {
		t.Fatalf("removed, it keeps %v and panes %+v", a.machines.Dropped(), a.st.Panes)
	}
}

// Removed while it reconnects, a dropped server takes the reconnect
// and what it left along.
func TestRemovingADroppedServerWhileItReconnects(t *testing.T) {
	a, answering := dialApp(t)
	a.handle(ConnectTo{Server: "srv"})
	waitFor(t, a, "a shell", func() bool { answering(); return oneShell(a) })
	_ = a.machines.Get("srv").Conn.Close() // as if the network went
	waitFor(t, a, "the drop", func() bool { return a.machines.Get("srv").Dropped })
	a.handle(ConnectTo{Server: "srv"})
	waitFor(t, a, "the host key question", func() bool { return len(a.st.Asks) > 0 })
	a.handle(RemoveServer{ID: "srv"})
	waitFor(t, a, "the reconnect to end", func() bool { return len(a.machines.Dialing()) == 0 && len(a.st.Asks) == 0 })
	if len(a.machines.Dropped()) != 0 || len(a.st.Panes) != 0 {
		t.Fatalf("removed, it keeps %v and panes %+v", a.machines.Dropped(), a.st.Panes)
	}
}

func TestConnectingAgainWhileConnectingAsks(t *testing.T) {
	a, answering := dialApp(t)
	a.handle(ConnectTo{Server: "srv"})
	waitFor(t, a, "the host key question", func() bool { return len(a.st.Asks) > 0 })
	a.handle(ConnectTo{Server: "srv"})
	waitFor(t, a, "the second question", func() bool { return len(a.st.Asks) == 2 })
	q := a.st.Asks[1]
	if q.Title != "Already connecting to srv" || !slices.Equal(q.Choose, []string{"Wait", "Retry"}) {
		t.Fatalf("asked %+v", q)
	}
	a.handle(AskAnswered{ID: q.ID, Yes: true, Answers: []string{"Wait"}})
	// Waited for, the first lands and the second opens a shell too.
	waitFor(t, a, "two shells", func() bool {
		answering()
		return len(a.st.Panes) == 2 && !slices.ContainsFunc(a.st.Panes, func(p Pane) bool { return p.Kind == KindLog })
	})
}

func TestOpeningOnASavedServerConnectsFirst(t *testing.T) {
	a, answering := dialApp(t)
	a.handle(OpenOn{Machine: "srv"})
	waitFor(t, a, "a shell on the server", func() bool { answering(); return oneShell(a) })
	if a.st.Panes[0].Machine != "srv" || a.machines.Get("srv").Conn == nil {
		t.Fatalf("opened %+v", a.st.Panes)
	}
}

// A password asked again on one connection says the last was refused,
// and a new key's question opens on Cancel.
func TestSigningInSaysAPasswordWasRefused(t *testing.T) {
	a, _ := dialApp(t)
	a.handle(ConnectTo{Server: "srv"})
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

// A connection that goes by itself leaves its machine on the sidebar
// until cleared; one let go of on purpose takes it along.
func TestADroppedConnectionStaysUntilCleared(t *testing.T) {
	a, answering := dialApp(t)
	a.handle(ConnectTo{Server: "srv"})
	waitFor(t, a, "a shell", func() bool { answering(); return oneShell(a) })
	_ = a.machines.Get("srv").Conn.Close() // as if the network went
	waitFor(t, a, "the drop", func() bool { return a.machines.Get("srv").Conn == nil })
	if !a.machines.Get("srv").Dropped {
		t.Fatalf("dropped, the machines kept are %v", a.machines.Dropped())
	}
	a.handle(ClearMachine{ID: "srv"})
	if len(a.machines.Dropped()) != 0 || len(a.st.Panes) != 0 {
		t.Fatalf("cleared, it keeps %v and panes %+v", a.machines.Dropped(), a.st.Panes)
	}
	a.handle(ConnectTo{Server: "srv"})
	waitFor(t, a, "a shell again", func() bool { answering(); return oneShell(a) })
	a.handle(Disconnect{Machine: "srv"})
	waitFor(t, a, "the disconnect", func() bool { return a.machines.Get("srv").Conn == nil })
	if len(a.machines.Dropped()) != 0 {
		t.Fatalf("let go of on purpose, it keeps %v", a.machines.Dropped())
	}
}

// The connection log shows as the connection is made, and gives way to
// the shell once it is; a connection that fails leaves it, saying why.
func TestTheConnectionLogShowsWhileConnecting(t *testing.T) {
	a, answering := dialApp(t)
	a.handle(ConnectTo{Server: "srv"})
	if len(a.st.Panes) != 1 || a.st.Panes[0].Kind != KindLog || a.st.Focus != a.st.Panes[0].ID {
		t.Fatalf("connecting, the panes are %+v", a.st.Panes)
	}
	log := a.st.Panes[0].ID
	waitFor(t, a, "the log's first line", func() bool {
		return strings.Contains(a.shells.Get(log).T.AllText(), "connecting to srv")
	})
	waitFor(t, a, "the shell in its place", func() bool { answering(); return oneShell(a) })

	a.handle(ConnectTo{Target: "tester@127.0.0.1:1"})
	waitFor(t, a, "the dial to fail", func() bool { return len(a.machines.Dialing()) == 0 })
	var failed string
	for _, p := range a.st.Panes {
		if p.Kind == KindLog {
			failed = p.ID
		}
	}
	if failed == "" {
		t.Fatalf("failed, the panes are %+v", a.st.Panes)
	}
	waitFor(t, a, "why it failed", func() bool {
		return strings.Contains(a.shells.Get(failed).T.AllText(), "could not connect")
	})
}

// A saved server's shell asks for the TERM the server list names for
// it.
func TestAServersShellAsksForItsTerm(t *testing.T) {
	testhome.New(t)
	t.Setenv("SSH_AUTH_SOCK", "")
	s := sshtest.New(t)
	host, port := s.Host()
	a := newApp(gunimtest.New(t, geom.Sz(400, 300), nil).Client(), screen.NewShells())
	a.ctx = t.Context()
	book, err := remote.LoadBook(filepath.Join(t.TempDir(), "servers.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := book.Put(remote.Host{Name: "srv", Address: host, Port: port, User: "tester", Term: "screen-256color"}, ""); err != nil {
		t.Fatal(err)
	}
	idsAsNames(t, book)
	a.book = book
	t.Cleanup(func() {
		for len(a.st.Panes) > 0 {
			a.remove(a.st.Panes[0].ID)
		}
		for _, id := range a.machines.Connected() {
			_ = a.machines.Get(id).Conn.Close()
		}
	})
	a.handle(OpenOn{Machine: "srv"})
	waitFor(t, a, "a shell", func() bool {
		for _, q := range a.st.Asks {
			ans := AskAnswered{ID: q.ID, Yes: true}
			if len(q.Prompts) > 0 {
				ans.Answers = []string{sshtest.Password}
			}
			a.handle(ans)
		}
		return oneShell(a)
	})
	if got := s.LastTerm(); got != "screen-256color" {
		t.Fatalf("the shell asked for TERM %q", got)
	}
}

// The status line names every connection on its way, so one that lands
// leaves the other said.
func TestTheStatusNamesEveryDial(t *testing.T) {
	a := newApp(gunimtest.New(t, geom.Sz(400, 300), nil).Client(), screen.NewShells())
	one, two := a.machines.NewQuick("one.example", false), a.machines.NewQuick("two.example", false)
	a.machines.At(one).Dialing = func() {}
	a.machines.At(two).Dialing = func() {}
	a.showDialling()
	if !strings.Contains(a.st.Status, "one.example") || !strings.Contains(a.st.Status, "two.example") {
		t.Fatalf("dialling two, the status says %q", a.st.Status)
	}
	a.machines.At(one).Dialing = nil
	a.showDialling()
	if a.st.Status != "Connecting to two.example…" {
		t.Fatalf("one landed, the status says %q", a.st.Status)
	}
	a.machines.At(two).Dialing = nil
	a.showDialling()
	if a.st.Status != "" {
		t.Fatalf("both landed, the status says %q", a.st.Status)
	}
}

// Other work that ends leaves a connection on its way said.
func TestAnUpdateCheckLeavesADialSaid(t *testing.T) {
	a, _ := agentApp(t)
	was := latestRelease
	latestRelease = func(context.Context) (update.Release, error) { return update.Release{Version: "v0.0.1"}, nil }
	t.Cleanup(func() { latestRelease = was })
	one := a.machines.NewQuick("one.example", false)
	a.machines.At(one).Dialing = func() {}
	a.handle(CheckUpdates{})
	waitFor(t, a, "the answer", func() bool { return !a.checking })
	if a.st.Status != "Connecting to one.example…" {
		t.Fatalf("the check done, the status says %q", a.st.Status)
	}
}

// A server's shell started again is taught again to say what it is
// doing, as the new shell knows nothing of the old one's setup.
func TestAServersShellStartedAgainIsTaughtAgain(t *testing.T) {
	a, answering := dialApp(t)
	h, _ := a.book.Lookup("srv")
	h.Setup = true
	if err := a.book.Put(h, "srv"); err != nil {
		t.Fatal(err)
	}
	a.st.Saved = a.book.Hosts()
	a.handle(OpenOn{Machine: "srv"})
	waitFor(t, a, "a shell", func() bool { answering(); return oneShell(a) })
	id := a.st.Panes[0].ID
	taught := func() int { return strings.Count(a.terminal(id).Text(), "__gt_d(){") }
	waitFor(t, a, "the setup typed", func() bool { return taught() == 1 })
	a.terminal(id).Send([]byte("bye\n"))
	waitFor(t, a, "the shell to end", func() bool { return a.terminal(id).Exited() })
	if err := a.startAgain(id); err != nil {
		t.Fatal(err)
	}
	waitFor(t, a, "the setup typed again", func() bool { return taught() == 2 })
}
