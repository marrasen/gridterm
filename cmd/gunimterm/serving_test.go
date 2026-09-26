package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pkg/sftp"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/geom"
	"golang.org/x/crypto/ssh"

	"github.com/marrasen/gridterm/remote"
	"github.com/marrasen/gridterm/serve"
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
	if _, err := sess.Write([]byte("echo attached-$((2*3))\r")); err != nil {
		t.Fatal(err)
	}
	waitFor(t, a, "the other window to see what it typed run", func() bool { return said.has("attached-6") })
	if !strings.Contains(a.terminal(a.st.Panes[0].ID).Text(), "attached-6") {
		t.Fatal("what the other window typed did not reach the pane here")
	}
	if size := a.terminal(a.st.Panes[0].ID).Size(); size.Cols != 70 || size.Rows != 20 {
		t.Fatalf("watched, the pane is %dx%d, want the watcher's 70x20", size.Cols, size.Rows)
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
	if len(a.st.Panes) != 2 || !strings.HasSuffix(a.st.Panes[1].Title, "(opened from another window)") {
		t.Fatalf("opened from elsewhere, the panes are %+v", a.st.Panes)
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
		defer func() { _ = c.Close() }()
		f, e := c.Open(filepath.Join(home, "here.txt"))
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
	w := gunim.NewOffscreen(geom.Sz(400, 300), nil)
	b = newApp(w.Client(), &shells{m: map[string]*shell{}})
	b.ctx = t.Context()
	t.Cleanup(func() {
		for name := range b.windows {
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
	pumpBoth(t, a, b, "a terminal on the window", func() bool { return len(b.st.Panes) == 1 })
	if p := b.st.Panes[0]; p.Machine != addr {
		t.Fatalf("the terminal is on %q, want the window at %s", p.Machine, addr)
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
	b.handle(AttachWindow{Window: addr, ID: first.ID})
	pumpBoth(t, a, b, "the pane attached", func() bool { return len(b.st.Panes) == 2 })
	there := b.terminal(b.st.Panes[1].ID)
	there.Paste("echo from-b-$((3*3))\r")
	pumpBoth(t, a, b, "the command to run there", func() bool {
		return strings.Contains(a.terminal(a.st.Panes[0].ID).Text(), "from-b-9") && strings.Contains(there.Text(), "from-b-9")
	})
	if len(b.st.Windows[0].Open) != 0 {
		t.Fatalf("attached, it is still listed: %+v", b.st.Windows[0].Open)
	}

	// Its files.
	b.st.Focus = b.st.Panes[0].ID
	b.handle(OpenFiles{})
	pumpBoth(t, a, b, "the files", func() bool { return len(b.st.Panes) == 3 && b.st.Panes[2].Kind == kindFiles })

	// Disconnected by the first, the second is told, and its panes end.
	a.handle(DisconnectClients{})
	pumpBoth(t, a, b, "the second window to let go", func() bool { return len(b.windows) == 0 })
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
	b.book = book
	b.st.Saved = book.Hosts()
	b.handle(ConnectTo{Saved: "desk"})
	pumpBoth(t, a, b, "the question about the host key", func() bool { return len(b.st.Asks) > 0 })
	b.handle(AskAnswered{ID: b.st.Asks[0].ID, Yes: true})
	pumpBoth(t, a, b, "a terminal on the window", func() bool { return len(b.st.Panes) == 1 })
	if b.st.Panes[0].Machine != "desk" || b.windows["desk"] == nil {
		t.Fatalf("connected, the pane is on %q and the windows are %v", b.st.Panes[0].Machine, b.windows)
	}
}
