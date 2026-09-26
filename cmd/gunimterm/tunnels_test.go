package main

import (
	"context"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/geom"

	"github.com/marrasen/gridterm/internal/sshtest"
	"github.com/marrasen/gridterm/remote"
)

// passwordOnly answers the test server's password, and trusts it.
type passwordOnly struct{}

func (passwordOnly) Passphrase(context.Context, remote.LockedKey) (string, error) {
	return "", errDeclined
}
func (passwordOnly) Password(context.Context, string, string) (string, error) {
	return sshtest.Password, nil
}
func (passwordOnly) Question(context.Context, remote.Question) ([]string, error) {
	return nil, errDeclined
}
func (passwordOnly) TrustHostKey(context.Context, remote.HostKey) (bool, error) { return true, nil }
func (passwordOnly) Notice(context.Context, remote.Notice)                      {}

// tunnelApp is the program side, connected to the test server as
// "srv", with an echo server for tunnels to reach.
func tunnelApp(t *testing.T) (a *app, conn *remote.Conn, echo string) {
	t.Helper()
	s := sshtest.New(t)
	host, port := s.Host()
	conn, err := remote.Connect(t.Context(), remote.Config{
		Host: host, Port: port, User: "tester", Ask: passwordOnly{},
		HostKeyCallback: ssh.FixedHostKey(s.HostKey()), NoAgent: true, NoIdentities: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	w := gunim.NewOffscreen(geom.Sz(400, 300), nil)
	a = newApp(w.Client(), &shells{m: map[string]*shell{}})
	a.ctx = t.Context()
	a.conns["srv"] = conn
	t.Cleanup(func() {
		for id := range a.tunnels {
			_ = a.closeTunnel(id)
		}
	})
	return a, conn, echoServer(t)
}

// echoServer sends back what it is sent, in upper case.
func echoServer(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer func() { _ = c.Close() }()
				buf := make([]byte, 256)
				for {
					n, err := c.Read(buf)
					if n > 0 {
						_, _ = c.Write([]byte(strings.ToUpper(string(buf[:n]))))
					}
					if err != nil {
						return
					}
				}
			}()
		}
	}()
	return ln.Addr().String()
}

// waitFor runs what the app's goroutines send it until ok, as its loop
// would, and fails after five seconds.
func waitFor(t *testing.T, a *app, what string, ok func() bool) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for !ok() {
		select {
		case f := <-a.events:
			f()
		case <-time.After(10 * time.Millisecond):
		case <-deadline:
			t.Fatalf("waited five seconds for %s", what)
		}
	}
}

// say writes what down c and reads the answer.
func say(t *testing.T, c net.Conn, what string) string {
	t.Helper()
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := c.Write([]byte(what)); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, len(what))
	if _, err := io.ReadFull(c, buf); err != nil {
		t.Fatal(err)
	}
	return string(buf)
}

func TestATunnelCarriesBytesAndItsRowSaysSo(t *testing.T) {
	a, _, echo := tunnelApp(t)
	a.handle(OpenTunnel{Machine: "srv", Tunnel: remote.Tunnel{Kind: remote.LocalForward, Listen: "127.0.0.1:0", Target: echo}})
	if len(a.st.Tunnels) != 1 {
		t.Fatalf("after opening one, the rows are %+v", a.st.Tunnels)
	}
	row := a.st.Tunnels[0]
	if !row.Live || row.Machine != "srv" || !strings.HasPrefix(row.Label, ":") || !strings.HasSuffix(row.Label, echo) {
		t.Fatalf("the row is %+v", row)
	}
	c, err := net.Dial("tcp", a.tunnels[row.ID].f.Addr())
	if err != nil {
		t.Fatal(err)
	}
	if got := say(t, c, "hello"); got != "HELLO" {
		t.Fatalf("through the tunnel came %q", got)
	}
	waitFor(t, a, "the row to count the stream", func() bool {
		return strings.HasPrefix(a.st.Tunnels[0].Note, "1 stream · ")
	})
	_ = c.Close()
	deadline := time.Now().Add(5 * time.Second)
	for !strings.HasPrefix(a.st.Tunnels[0].Note, "idle · ") {
		if time.Now().After(deadline) {
			t.Fatalf("five seconds after the stream closed, the row says %q and the tunnel counts %d streams; notices %+v", a.st.Tunnels[0].Note, a.tunnels[row.ID].f.Streams(), a.st.Notices)
		}
		select {
		case f := <-a.events:
			f()
		case <-time.After(10 * time.Millisecond):
		}
	}

	addr := a.tunnels[row.ID].f.Addr()
	a.handle(CloseTunnel{ID: row.ID})
	if len(a.st.Tunnels) != 0 || len(a.tunnels) != 0 {
		t.Fatalf("closed, the tunnel is still listed: %+v", a.st.Tunnels)
	}
	if c, err := net.Dial("tcp", addr); err == nil {
		_ = c.Close()
		t.Fatalf("closed, the tunnel still accepts on %s", addr)
	}
}

func TestATunnelOpenToTheNetworkIsAskedAboutFirst(t *testing.T) {
	a, _, echo := tunnelApp(t)
	a.handle(OpenTunnel{Machine: "srv", Tunnel: remote.Tunnel{Kind: remote.LocalForward, Listen: "0.0.0.0:0", Target: echo}})
	waitFor(t, a, "the question", func() bool { return len(a.st.Asks) == 1 })
	if len(a.st.Tunnels) != 0 {
		t.Fatalf("before the answer, a tunnel opened: %+v", a.st.Tunnels)
	}
	q := a.st.Asks[0]
	if !q.Danger || q.Yes != "Open" {
		t.Fatalf("the question is %+v, want a danger button saying Open", q)
	}
	a.handle(AskAnswered{ID: q.ID, Yes: true})
	waitFor(t, a, "the tunnel", func() bool { return len(a.st.Tunnels) == 1 })
}

func TestATunnelStopsWithItsConnectionAndStaysUntilCleared(t *testing.T) {
	a, conn, echo := tunnelApp(t)
	a.handle(OpenTunnel{Machine: "srv", Tunnel: remote.Tunnel{Kind: remote.LocalForward, Listen: "127.0.0.1:0", Target: echo}})
	id := a.st.Tunnels[0].ID
	_ = conn.Close()
	a.tunnelsDiedOn("srv")
	if row := a.st.Tunnels[0]; row.Live || row.Note != "stopped" {
		t.Fatalf("with its connection gone, the row is %+v", row)
	}
	a.handle(ShowTunnel{ID: id})
	pane := a.st.Tunnels[0].Pane
	if pane == "" || a.st.Focus != pane {
		t.Fatalf("shown, the stopped tunnel has pane %q and the focus is on %q", pane, a.st.Focus)
	}
	a.handle(CloseTunnel{ID: id})
	if len(a.st.Tunnels) != 0 {
		t.Fatalf("cleared, the row stays: %+v", a.st.Tunnels)
	}
	a.remove(pane)
}

func TestWatchingATunnelWritesItsTrafficDown(t *testing.T) {
	a, _, echo := tunnelApp(t)
	a.handle(OpenTunnel{Machine: "srv", Tunnel: remote.Tunnel{Kind: remote.LocalForward, Listen: "127.0.0.1:0", Target: echo}})
	id := a.st.Tunnels[0].ID
	open := a.tunnels[id]
	c, err := net.Dial("tcp", open.f.Addr())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	say(t, c, "quiet")
	before := open.seen.Held()
	a.handle(WatchTunnel{ID: id, On: true})
	if !a.st.Tunnels[0].Watching {
		t.Fatal("watching, the row says it is not")
	}
	say(t, c, "loud")
	// The line saying watching began, then a chunk each way.
	if got := open.seen.Held() - before; got != 3 {
		t.Fatalf("watching, %d entries were written, want 3", got)
	}
	held := open.seen.Held()
	a.handle(WatchTunnel{ID: id})
	say(t, c, "quiet again")
	if got := open.seen.Held() - held; got != 1 {
		t.Fatalf("after stopping, %d entries were written, want only the one saying so", got)
	}
}

func TestATunnelsPaneIsLitOnTheTunnelsRow(t *testing.T) {
	panes := []Pane{{ID: "p1", Title: "Terminal 1", Machine: "srv"}, {ID: "p2", Title: "Tunnel :80 → x:80", Machine: "srv", Kind: kindTunnel, Tunnel: "t1"}}
	tunnels := []Tunnel{{ID: "t1", Machine: "srv", Label: ":80 → x:80", Note: "idle", Live: true, Pane: "p2"}}
	var keys []string
	for _, r := range sidebarRows(panes, tunnels, Share{}, nil) {
		keys = append(keys, r.key+"="+r.pane)
	}
	want := "machine:=,machine:srv=,p1=p1,tunnel:t1=p2"
	if got := strings.Join(keys, ","); got != want {
		t.Fatalf("the rows are %s, want %s", got, want)
	}
}
