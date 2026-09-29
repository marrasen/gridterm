package app

import (
	"crypto/ed25519"
	"crypto/rand"
	"io"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/marrasen/kakel/machines"
	"github.com/marrasen/kakel/screen"

	"github.com/pkg/sftp"

	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/gunimtest"
	"golang.org/x/crypto/ssh"

	"github.com/marrasen/kakel/remote"
	"github.com/marrasen/kakel/serve"
	"github.com/marrasen/kakel/vfs"
)

// servedApp is agentApp's window served on a free port of this
// machine, to one key, called laptop, which the other window is
// given.
func servedApp(t *testing.T) (a *app, win *serve.Window) {
	t.Helper()
	a, _ = agentApp(t)
	dir := t.TempDir()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	line := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer.PublicKey()))) + " laptop\n"
	a.serving.hostKey, a.serving.allowed = filepath.Join(dir, "host_key"), filepath.Join(dir, "authorized_keys")
	if err := os.WriteFile(a.serving.allowed, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	a.handle(StartServing{Port: "0"})
	if !a.st.Serving.On {
		t.Fatalf("asked to serve, the window says %+v, notices %+v", a.st.Serving, a.st.Notices)
	}
	t.Cleanup(func() { _ = a.stopServing() })
	asAgent(t, a, func() {
		win, err = serve.Dial(t.Context(), serve.DialConfig{
			Addr: a.st.Serving.Addr, Keys: []ssh.Signer{signer}, HostKey: ssh.InsecureIgnoreHostKey(),
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = win.Close() })
	waitFor(t, a, "the window to join", func() bool { return len(a.st.Serving.Clients) == 1 })
	return a, win
}

// heard collects what a session says, on a goroutine of its own.
type heard struct {
	mu   sync.Mutex
	said strings.Builder
}

func readAll(s io.Reader) *heard {
	r := &heard{}
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := s.Read(buf)
			r.mu.Lock()
			r.said.Write(buf[:n])
			r.mu.Unlock()
			if err != nil {
				return
			}
		}
	}()
	return r
}

func (r *heard) has(s string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return strings.Contains(r.said.String(), s)
}

func TestAnotherWindowWorksInAPaneHere(t *testing.T) {
	a, win := servedApp(t)
	if c := a.st.Serving.Clients[0]; c.Name != "laptop" {
		t.Fatalf("joined, the window is called %q", c.Name)
	}
	waitFor(t, a, "the list of what is open", func() bool { return len(win.Opens()) == 1 })
	open := win.Opens()[0]
	if open.ID != a.st.Panes[0].ID || open.Kind != "Terminal" || !open.HasScreen() {
		t.Fatalf("the other window is told %+v", open)
	}
	var sess io.ReadWriteCloser
	var err error
	asAgent(t, a, func() { sess, err = win.Attach(open, 70, 20) })
	if err != nil {
		t.Fatal(err)
	}
	said := readAll(sess)
	line, want := saysAnswer("attached")
	if _, err := sess.Write([]byte(line + "\r")); err != nil {
		t.Fatal(err)
	}
	waitFor(t, a, "the other window to see what it typed run", func() bool { return said.has(want) })
	if !strings.Contains(a.terminal(a.st.Panes[0].ID).Text(), want) {
		t.Fatal("what the other window typed did not reach the pane here")
	}
	if size := a.terminal(a.st.Panes[0].ID).Size(); size.Cols != 70 || size.Rows != 20 {
		t.Fatalf("watched, the pane is %dx%d, want the watcher's 70x20", size.Cols, size.Rows)
	}
	a.publish()
	if got := a.st.Panes[0].Note; got != "at 70x20, watched by 1" {
		t.Fatalf("watched, the pane's row says %q", got)
	}
	_ = sess.Close()
}

func TestAnotherWindowOpensAShellHere(t *testing.T) {
	a, win := servedApp(t)
	var err error
	var sess io.ReadWriteCloser
	asAgent(t, a, func() {
		s, e := win.Open(80, 24, func(serve.Attached) {})
		sess, err = s, e
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sess.Close() }()
	// Said on its row for as long as it is open, whatever its program
	// calls it.
	if len(a.st.Panes) != 2 {
		t.Fatalf("opened from elsewhere, the panes are %+v", a.st.Panes)
	}
	a.retitle(a.st.Panes[1].ID, "vim notes.txt")
	a.notePanes()
	if p := a.st.Panes[1]; !strings.HasPrefix(p.Note, openedForNote) || p.Title != "vim notes.txt" {
		t.Fatalf("retitled, the pane is %+v", p)
	}
	// The program's loop publishes after every change, which is when
	// windows connected hear of it.
	waitFor(t, a, "the new pane in the list", func() bool {
		a.publish()
		return len(win.Opens()) == 2
	})
}

func TestAnotherWindowReadsTheFilesHere(t *testing.T) {
	a, win := servedApp(t)
	home := os.Getenv("HOME")
	if err := os.WriteFile(filepath.Join(home, "here.txt"), []byte("from this machine"), 0o600); err != nil {
		t.Fatal(err)
	}
	var got []byte
	var err error
	asAgent(t, a, func() {
		fs, e := win.Files()
		if e != nil {
			err = e
			return
		}
		c, e := sftp.NewClientPipe(fs, fs)
		if e != nil {
			err = e
			return
		}
		files := vfs.NewSFTP("laptop", nil, c, c.Close)
		defer func() { _ = files.Close() }()
		// Named the way this machine names it, as a pane's shell here
		// says it: on Windows, "C:\...", which SFTP spells "/C:/...".
		f, e := files.Open(filepath.Join(home, "here.txt"))
		if e != nil {
			err = e
			return
		}
		got, err = io.ReadAll(f)
	})
	if err != nil || string(got) != "from this machine" {
		t.Fatalf("read %q, %v", got, err)
	}
}

func TestDisconnectingHangsUpOnTheOtherWindow(t *testing.T) {
	a, win := servedApp(t)
	// The reason goes down the other window's control channel, which is
	// open once the first list has come down it.
	waitFor(t, a, "the list of what is open", func() bool { return len(win.Opens()) == 1 })
	a.handle(DisconnectClients{})
	gone := make(chan struct{})
	go func() { _ = win.Wait(); close(gone) }()
	waitFor(t, a, "the other window to go", func() bool {
		select {
		case <-gone:
			return len(a.st.Serving.Clients) == 0
		default:
			return false
		}
	})
	if win.Going() != serve.GoingKicked {
		t.Fatalf("hung up on, the other window was told %q", win.Going())
	}
}

// pumpBoth runs what both programs' goroutines send them until ok.
func pumpBoth(t *testing.T, a, b *app, what string, ok func() bool) {
	t.Helper()
	defer stuckAfter(what, 20*time.Second)()
	deadline := time.After(10 * time.Second)
	for !ok() {
		select {
		case f := <-a.events:
			f()
		case f := <-b.events:
			f()
		case <-time.After(10 * time.Millisecond):
		case <-deadline:
			t.Fatalf("waited ten seconds for %s", what)
		}
	}
}

// clientOf is a second window, with the key the first allows, ready
// to connect to it.
func clientOf(t *testing.T, a *app) (b *app, keyFile string) {
	t.Helper()
	keyFile = filepath.Join(t.TempDir(), "id_ed25519")
	writeKey(t, keyFile)
	pub, err := os.ReadFile(keyFile + ".pub")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(a.serving.allowed, append(pub[:len(pub)-1], []byte(" laptop\n")...), 0o600); err != nil {
		t.Fatal(err)
	}
	// Serving reads the allowed keys as it starts, so it starts again.
	if err := a.stopServing(); err != nil {
		t.Fatal(err)
	}
	a.handle(StartServing{Port: "0"})
	w := gunimtest.New(t, geom.Sz(400, 300), nil)
	b = newApp(w.Client(), screen.NewShells())
	b.ctx = t.Context()
	t.Cleanup(func() {
		for _, name := range b.machines.Windows() {
			_ = b.disconnectWindow(name)
		}
		for len(b.st.Panes) > 0 {
			b.remove(b.st.Panes[0].ID)
		}
	})
	return b, keyFile
}

func TestAWindowConnectsToAServedOne(t *testing.T) {
	a, _ := agentApp(t)
	dir := t.TempDir()
	a.serving.hostKey, a.serving.allowed = filepath.Join(dir, "host_key"), filepath.Join(dir, "authorized_keys")
	b, keyFile := clientOf(t, a)
	addr := a.st.Serving.Addr

	b.handle(ConnectWindow{Addr: addr, KeyFile: keyFile})
	pumpBoth(t, a, b, "the question about the host key", func() bool { return len(b.st.Asks) > 0 })
	b.handle(AskAnswered{ID: b.st.Asks[0].ID, Yes: true})
	pumpBoth(t, a, b, "a terminal on the window", func() bool { return oneShell(b) })
	if p := b.st.Panes[0]; b.machines.Name(p.Machine) != addr {
		t.Fatalf("the terminal is on %q, want the window at %s", b.machines.Name(p.Machine), addr)
	}
	// It opened a pane on the first window too, which that one shows.
	pumpBoth(t, a, b, "the pane on the first window", func() bool { return len(a.st.Panes) == 2 })

	// The first window's own pane is listed under it, to work in.
	pumpBoth(t, a, b, "the list", func() bool {
		a.publish()
		return len(b.st.Windows) == 1 && len(b.st.Windows[0].Open) == 1
	})
	first := b.st.Windows[0].Open[0]
	if first.ID != a.st.Panes[0].ID {
		t.Fatalf("listed %+v, want the first window's own pane %s", first, a.st.Panes[0].ID)
	}
	b.handle(AttachWindow{Window: b.st.Panes[0].Machine, ID: first.ID})
	pumpBoth(t, a, b, "the pane attached", func() bool { return len(b.st.Panes) == 2 })
	there := b.terminal(b.st.Panes[1].ID)
	line, want := saysAnswer("from-b")
	there.Paste(line + "\r")
	pumpBoth(t, a, b, "the command to run there", func() bool {
		return strings.Contains(a.terminal(a.st.Panes[0].ID).Text(), want) && strings.Contains(there.Text(), want)
	})
	if len(b.st.Windows[0].Open) != 0 {
		t.Fatalf("attached, it is still listed: %+v", b.st.Windows[0].Open)
	}

	// Its files.
	b.st.Focus = b.st.Panes[0].ID
	b.handle(OpenFiles{})
	pumpBoth(t, a, b, "the files", func() bool { return len(b.st.Panes) == 3 && b.st.Panes[2].Kind == KindFiles })

	// Disconnected by the first, the second is told, and its panes end.
	a.handle(DisconnectClients{})
	pumpBoth(t, a, b, "the second window to let go", func() bool { return len(b.machines.Windows()) == 0 })
	pumpBoth(t, a, b, "its terminals to end", func() bool { return b.st.Panes[0].Ended && b.st.Panes[1].Ended })
}

func TestASavedWindowIsConnectedToAsOne(t *testing.T) {
	a, _ := agentApp(t)
	dir := t.TempDir()
	a.serving.hostKey, a.serving.allowed = filepath.Join(dir, "host_key"), filepath.Join(dir, "authorized_keys")
	b, keyFile := clientOf(t, a)
	book, err := remote.LoadBook(filepath.Join(t.TempDir(), "servers.json"))
	if err != nil {
		t.Fatal(err)
	}
	host, port, _ := net.SplitHostPort(a.st.Serving.Addr)
	p, _ := strconv.Atoi(port)
	if err := book.Put(remote.Host{Name: "desk", Address: host, Port: p, Window: true, Identities: []string{keyFile}}, ""); err != nil {
		t.Fatal(err)
	}
	idsAsNames(t, book)
	b.book = book
	b.st.Saved = book.Hosts()
	b.handle(ConnectTo{Server: "desk"})
	pumpBoth(t, a, b, "the question about the host key", func() bool { return len(b.st.Asks) > 0 })
	b.handle(AskAnswered{ID: b.st.Asks[0].ID, Yes: true})
	pumpBoth(t, a, b, "a terminal on the window", func() bool { return oneShell(b) })
	if b.st.Panes[0].Machine != "desk" || b.machines.Get("desk").Window == nil {
		t.Fatalf("connected, the pane is on %q and the windows are %v", b.st.Panes[0].Machine, b.machines.Windows())
	}
}

func TestOneServedWindowIsDisconnectedFromItsRow(t *testing.T) {
	a, win := servedApp(t)
	waitFor(t, a, "the list of what is open", func() bool { return len(win.Opens()) == 1 })
	c := a.st.Serving.Clients[0]
	a.handle(DisconnectClient(c))
	gone := make(chan struct{})
	go func() { _ = win.Wait(); close(gone) }()
	waitFor(t, a, "the other window to go", func() bool {
		select {
		case <-gone:
			return len(a.st.Serving.Clients) == 0
		default:
			return false
		}
	})
	if win.Going() != serve.GoingKicked {
		t.Fatalf("hung up on, the other window was told %q", win.Going())
	}
}

func TestServeAgainCanBeToldNotToAsk(t *testing.T) {
	a := fontApp(t)
	if err := a.settings.PutServeOn(true); err != nil {
		t.Fatal(err)
	}
	go a.offerToServeAgain()
	waitFor(t, a, "the question", func() bool { return len(a.st.Asks) == 1 })
	q := a.st.Asks[0]
	if !slices.Contains(q.Choose, "Don't Ask Again") {
		t.Fatalf("asked %+v", q)
	}
	a.handle(AskAnswered{ID: q.ID, Yes: true, Answers: []string{"Don't Ask Again"}})
	waitFor(t, a, "the setting", func() bool { return !a.settings.ServeOn() })
}

// Starting a terminal on a served window again starts it again there,
// in the pane it had, rather than opening another.
func TestAPaneOnAServedWindowStartsAgainThere(t *testing.T) {
	a, _ := agentApp(t)
	dir := t.TempDir()
	a.serving.hostKey, a.serving.allowed = filepath.Join(dir, "host_key"), filepath.Join(dir, "authorized_keys")
	b, keyFile := clientOf(t, a)
	addr := a.st.Serving.Addr
	b.handle(ConnectWindow{Addr: addr, KeyFile: keyFile})
	pumpBoth(t, a, b, "the question about the host key", func() bool { return len(b.st.Asks) > 0 })
	b.handle(AskAnswered{ID: b.st.Asks[0].ID, Yes: true})
	pumpBoth(t, a, b, "a terminal on the window", func() bool { return oneShell(b) })
	pumpBoth(t, a, b, "its pane on the first window", func() bool { return len(a.st.Panes) == 2 })
	here, there := b.st.Panes[0].ID, a.st.Panes[1].ID
	b.terminal(here).Paste("exit\r")
	pumpBoth(t, a, b, "the shell to end", func() bool { return b.terminal(here).Exited() })
	if err := b.startAgain(here); err != nil {
		t.Fatal(err)
	}
	pumpBoth(t, a, b, "it to start again", func() bool { return !b.terminal(here).Exited() })
	if len(a.st.Panes) != 2 {
		t.Fatalf("started again, the first window has %d panes, want its 2", len(a.st.Panes))
	}
	pumpBoth(t, a, b, "it to run there again", func() bool {
		tt := a.terminal(there)
		return tt != nil && !tt.Exited()
	})
}

// A pane closed on the served window, started again from here, opens a
// shell of its own there, rather than failing.
func TestAPaneClosedOnTheServedWindowStartsAgainAsAShell(t *testing.T) {
	a, _ := agentApp(t)
	dir := t.TempDir()
	a.serving.hostKey, a.serving.allowed = filepath.Join(dir, "host_key"), filepath.Join(dir, "authorized_keys")
	b, keyFile := clientOf(t, a)
	addr := a.st.Serving.Addr
	b.handle(ConnectWindow{Addr: addr, KeyFile: keyFile})
	pumpBoth(t, a, b, "the question about the host key", func() bool { return len(b.st.Asks) > 0 })
	b.handle(AskAnswered{ID: b.st.Asks[0].ID, Yes: true})
	pumpBoth(t, a, b, "a terminal on the window", func() bool { return oneShell(b) })
	pumpBoth(t, a, b, "its pane on the first window", func() bool { return len(a.st.Panes) == 2 })
	here, there := b.st.Panes[0].ID, a.st.Panes[1].ID
	a.remove(there)
	pumpBoth(t, a, b, "the shell to end here", func() bool { return b.terminal(here).Exited() })
	if err := b.startAgain(here); err != nil {
		t.Fatal(err)
	}
	pumpBoth(t, a, b, "a shell there in its place", func() bool {
		return !b.terminal(here).Exited() && len(a.st.Panes) == 2
	})
}

// A saved window's address typed to connect to is that window, reached
// as one, not SSH to the port it serves on.
func TestASavedWindowsAddressTypedIsThatWindow(t *testing.T) {
	a, _ := agentApp(t)
	dir := t.TempDir()
	a.serving.hostKey, a.serving.allowed = filepath.Join(dir, "host_key"), filepath.Join(dir, "authorized_keys")
	b, keyFile := clientOf(t, a)
	host, port, err := net.SplitHostPort(a.st.Serving.Addr)
	if err != nil {
		t.Fatal(err)
	}
	book, err := remote.LoadBook(filepath.Join(t.TempDir(), "servers.json"))
	if err != nil {
		t.Fatal(err)
	}
	n, _ := strconv.Atoi(port)
	if err := book.Put(remote.Host{Name: "desk", Address: host, Port: n, Window: true, Identities: []string{keyFile}}, ""); err != nil {
		t.Fatal(err)
	}
	b.book = book
	desk, _ := book.Lookup("desk")
	b.handle(ConnectTo{Target: host + ":" + port})
	pumpBoth(t, a, b, "the question about the host key", func() bool { return len(b.st.Asks) > 0 })
	b.handle(AskAnswered{ID: b.st.Asks[0].ID, Yes: true})
	pumpBoth(t, a, b, "the window", func() bool { return b.machines.Get(machines.ID(desk.ID)).Window != nil })
	if len(b.machines.Connected()) != 0 {
		t.Fatalf("typed, it also connected over SSH to %v", b.machines.Connected())
	}
}

// Only a target with no user names a saved window, and an IP address
// matches however it is spelled.
func TestASavedWindowIsKnownByItsAddressOnly(t *testing.T) {
	a := fontApp(t)
	book, err := remote.LoadBook(filepath.Join(t.TempDir(), "servers.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := book.Put(remote.Host{Name: "desk", Address: "[::1]", Port: 7022, Window: true}, ""); err != nil {
		t.Fatal(err)
	}
	a.book = book
	for typed, want := range map[string]bool{
		"::1": true, "[0:0::1]:7022": true, "[::1]:22": false, "me@[::1]": false, "::2": false,
	} {
		cfg, err := remote.ParseTarget(typed)
		if err != nil {
			t.Fatal(err)
		}
		if _, got := a.savedWindowAt(cfg); got != want {
			t.Errorf("%q names the saved window: %v, want %v", typed, got, want)
		}
	}
}

// A window tells a client the machines it is connected to, so each
// has a heading there with nothing open on it.
func TestAWindowSaysWhichMachinesItIsConnectedTo(t *testing.T) {
	_, conn, _ := tunnelApp(t)
	a, b := connectedWindows(t)
	a.machines.At("srv").Conn = conn
	pumpBoth(t, a, b, "srv listed", func() bool {
		a.publish()
		return len(b.st.Windows) == 1 && slices.Contains(b.st.Windows[0].Machines, "srv")
	})
}

// A machine that holds as many file sessions left waiting to end as it
// may is given no more.
func TestAFileRelayIsRefusedWhileTooManyAreLeftWaiting(t *testing.T) {
	a, conn, _ := tunnelApp(t)
	a.machines.At("srv").Conn = conn
	a.parked[conn] = mostParked
	here, there := net.Pipe()
	defer func() { _ = here.Close(); _ = there.Close() }()
	errs := make(chan error, 1)
	go func() { errs <- a.serveFiles(t.Context(), "srv", there) }()
	var err error
	waitFor(t, a, "the refusal", func() bool {
		select {
		case err = <-errs:
			return true
		default:
			return false
		}
	})
	if err == nil || !strings.Contains(err.Error(), "still waiting to end") {
		t.Fatalf("it said %v", err)
	}
}

// A window's address typed again while it is connected is that window:
// no second connection under a second heading.
func TestAWindowConnectedToIsKnownByItsAddress(t *testing.T) {
	a, b := connectedWindows(t)
	err := b.connectWindow(ConnectWindow{Addr: a.st.Serving.Addr})
	if err == nil || !strings.Contains(err.Error(), "already connected") {
		t.Fatalf("typed again, it said %v", err)
	}
	if n := len(b.machines.Windows()); n != 1 {
		t.Fatalf("typed again, the window holds %d windows", n)
	}
}

// Disconnecting a window that has gone already says so.
func TestDisconnectingAWindowAlreadyGoneSaysSo(t *testing.T) {
	a, _ := agentApp(t)
	err := a.disconnectClient(DisconnectClient{Name: "desk", From: "10.0.0.2:5000"})
	if err == nil || !strings.Contains(err.Error(), "had already gone") {
		t.Fatalf("it said %v", err)
	}
}
