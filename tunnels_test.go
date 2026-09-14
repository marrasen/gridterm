package main

import (
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/internal/sshtest"
	"github.com/marrasen/gridterm/meter"
	"github.com/marrasen/gridterm/remote"
	"github.com/marrasen/gridterm/ui"
)

// echoAt answers every connection with what it was sent, in upper case.
func echoAt(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
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

// connectedTo opens a connection to a test server and returns what the
// window calls it.
func connectedTo(t *testing.T, a *testApp, s *sshtest.Server) string {
	t.Helper()
	a.connect(serverConfig(t, s))
	waitForPanes(t, a, len(a.panes)+1)
	return serverConfig(t, s).Target()
}

// tunnelRow returns the panel row of the window's one tunnel.
func tunnelRow(t *testing.T, a *testApp) *conns.Entry {
	t.Helper()
	for _, group := range a.registry.Groups(time.Now()) {
		for _, row := range group.Rows {
			if row.Kind == conns.Tunnel {
				return row.Entry
			}
		}
	}
	t.Fatalf("no tunnel on the panel: %v", panelText(a, time.Now()))
	return nil
}

// The whole path: ask for a tunnel, open it, and carry bytes through it.
func TestOpenATunnelThroughTheDialog(t *testing.T) {
	s := sshtest.New(t)
	echo := echoAt(t)
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	host := connectedTo(t, a, s)

	if err := a.openTunnelHere(); err != nil {
		t.Fatalf("openTunnelHere: %v", err)
	}
	f := waitForDialog(t, a, "Tunnel over "+host)
	typeInto(t, a, f, "127.0.0.1:0", echo)
	pressButton(t, a, f, "Listen here")
	a.pump.run()

	if m := a.root.Modal(); m != nil {
		t.Fatalf("a dialog was left open: %T", m)
	}
	row := tunnelRow(t, a)
	if len(a.tunnels) != 1 {
		t.Fatalf("the window holds %d tunnels, want 1", len(a.tunnels))
	}

	// The row says where it is listening, with the port it was given.
	addr := a.tunnels[row].f.Addr()
	if !strings.Contains(row.Label, addr) {
		t.Fatalf("the row says %q, want the address %q it is listening on", row.Label, addr)
	}

	// And it carries bytes.
	c, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial the tunnel: %v", err)
	}
	defer c.Close()
	if err := c.SetDeadline(time.Now().Add(waitBudget)); err != nil {
		t.Fatalf("deadline: %v", err)
	}
	if _, err := c.Write([]byte("hello")); err != nil {
		t.Fatalf("write: %v", err)
	}
	buf := make([]byte, 5)
	if _, err := io.ReadFull(c, buf); err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(buf) != "HELLO" {
		t.Fatalf("the far end said %q, want HELLO", buf)
	}

	// The panel says it is busy, and how busy.
	waitFor(t, a, "the bytes to be counted", func() bool {
		in, out := row.Meter.Totals()
		return in == 5 && out == 5
	})
	if got := row.State(time.Now()); got != meter.Active {
		t.Fatalf("the tunnel row is %v while bytes are moving, want active", got)
	}
}

// A tunnel that would let the rest of the network through asks first.
func TestATunnelOpenToTheNetworkAsksFirst(t *testing.T) {
	s := sshtest.New(t)
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	host := connectedTo(t, a, s)

	a.confirmTunnel(host, remote.Tunnel{
		Kind: remote.LocalForward, Listen: "0.0.0.0:0", Target: "127.0.0.1:9",
	})
	f, ok := a.root.Modal().(*ui.Form)
	if !ok {
		t.Fatalf("nothing asked: %T", a.root.Modal())
	}
	if !strings.Contains(f.Title, "network") {
		t.Fatalf("the question is %q", f.Title)
	}
	if len(a.tunnels) != 0 {
		t.Fatal("the tunnel was opened before the question was answered")
	}

	// Saying no opens nothing.
	pressButton(t, a, f, "Cancel")
	a.pump.run()
	if len(a.tunnels) != 0 {
		t.Fatalf("%d tunnels after cancelling", len(a.tunnels))
	}
}

// One on this machine only opens without a word, because it lets nothing
// new through.
func TestATunnelOnThisMachineDoesNotAsk(t *testing.T) {
	s := sshtest.New(t)
	echo := echoAt(t)
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	host := connectedTo(t, a, s)

	a.confirmTunnel(host, remote.Tunnel{
		Kind: remote.LocalForward, Listen: "127.0.0.1:0", Target: echo,
	})
	if m := a.root.Modal(); m != nil {
		t.Fatalf("a tunnel on this machine asked something: %T", m)
	}
	if len(a.tunnels) != 1 {
		t.Fatalf("%d tunnels, want the one that was asked for", len(a.tunnels))
	}
}

// Closing a tunnel from the panel stops it listening and takes its row.
func TestClosingATunnelFromThePanel(t *testing.T) {
	s := sshtest.New(t)
	echo := echoAt(t)
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	host := connectedTo(t, a, s)

	a.openTunnel(host, remote.Tunnel{
		Kind: remote.LocalForward, Listen: "127.0.0.1:0", Target: echo,
	})
	row := tunnelRow(t, a)
	addr := a.tunnels[row].f.Addr()

	if row.Close == nil {
		t.Fatal("the tunnel cannot be closed from the panel")
	}
	if err := row.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if len(a.tunnels) != 0 {
		t.Fatalf("the window still holds %d tunnels", len(a.tunnels))
	}
	if c, err := net.Dial("tcp", addr); err == nil {
		_ = c.Close()
		t.Fatal("the tunnel was still listening after it was closed")
	}
	for _, group := range a.registry.Groups(time.Now()) {
		for _, r := range group.Rows {
			if r.Entry == row {
				t.Fatal("the row was left on the panel")
			}
		}
	}
}

// Closing the connection closes the tunnels running over it, and takes
// their rows with them: a port listening into nothing is worse than no
// port at all.
func TestClosingTheConnectionTakesItsTunnels(t *testing.T) {
	s := sshtest.New(t)
	echo := echoAt(t)
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	host := connectedTo(t, a, s)

	a.openTunnel(host, remote.Tunnel{
		Kind: remote.LocalForward, Listen: "127.0.0.1:0", Target: echo,
	})
	row := tunnelRow(t, a)
	addr := a.tunnels[row].f.Addr()

	if err := a.dropMachine(host); err != nil {
		t.Fatalf("dropMachine: %v", err)
	}
	if len(a.tunnels) != 0 {
		t.Fatalf("the window still holds %d tunnels", len(a.tunnels))
	}
	if c, err := net.Dial("tcp", addr); err == nil {
		_ = c.Close()
		t.Fatal("the tunnel was still listening after its connection closed")
	}
	if len(panelText(a, time.Now())) != 2 {
		t.Fatalf("the panel still shows %v", panelText(a, time.Now()))
	}
}

// A connection that drops takes its tunnels with it, and their rows stay
// to say what happened.
func TestAConnectionThatDropsClosesItsTunnels(t *testing.T) {
	s := sshtest.New(t)
	echo := echoAt(t)
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	host := connectedTo(t, a, s)

	a.openTunnel(host, remote.Tunnel{
		Kind: remote.LocalForward, Listen: "127.0.0.1:0", Target: echo,
	})
	row := tunnelRow(t, a)

	s.CloseClients()
	waitFor(t, a, "the connection to be given up on", func() bool {
		return a.machines[host] == nil
	})

	if len(a.tunnels) != 0 {
		t.Fatalf("the window still holds %d tunnels over a connection that has gone", len(a.tunnels))
	}
	if got := row.State(time.Now()); got != meter.Closed {
		t.Fatalf("the tunnel row is %v after its connection dropped, want closed", got)
	}
	// And it can still be taken off the panel.
	if err := row.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

// A tunnel needs a connection to run over, and saying so beats a dialog
// that cannot work.
func TestATunnelNeedsAConnection(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)

	if err := a.openTunnelHere(); err == nil {
		t.Fatal("a tunnel was offered on the machine gridterm is running on")
	}
	if m := a.root.Modal(); m != nil {
		t.Fatalf("a dialog opened anyway: %T", m)
	}
}

// A tunnel that makes no sense is said so where it was typed, with the
// dialog still open.
func TestATunnelThatMakesNoSenseIsRefusedInTheDialog(t *testing.T) {
	s := sshtest.New(t)
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	host := connectedTo(t, a, s)

	if err := a.openTunnelHere(); err != nil {
		t.Fatalf("openTunnelHere: %v", err)
	}
	f := waitForDialog(t, a, "Tunnel over "+host)
	typeInto(t, a, f, "127.0.0.1:0", "nowhere")
	pressButton(t, a, f, "Listen here")

	if a.root.Modal() != f {
		t.Fatal("the dialog closed on a tunnel it could not make")
	}
	if f.Error() == nil {
		t.Fatal("nothing said why it was refused")
	}
	if len(a.tunnels) != 0 {
		t.Fatalf("%d tunnels were opened anyway", len(a.tunnels))
	}
}

// A stream that cannot reach the other end is reported, and the tunnel
// stays open: the next one may work.
func TestAStreamThatFailsIsReportedAndTheTunnelStays(t *testing.T) {
	s := sshtest.New(t)
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	host := connectedTo(t, a, s)

	// Port 9 on the far machine, which the test server refuses.
	a.openTunnel(host, remote.Tunnel{
		Kind: remote.LocalForward, Listen: "127.0.0.1:0", Target: "127.0.0.1:9",
	})
	row := tunnelRow(t, a)
	addr := a.tunnels[row].f.Addr()

	c, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()

	f := waitForDialogPrefix(t, a, "Trouble on the tunnel")
	if strings.TrimSpace(strings.Join(f.Lines, "")) == "" {
		t.Fatal("the failure was reported with no reason in it")
	}
	if len(a.tunnels) != 1 {
		t.Fatal("one stream that failed closed the whole tunnel")
	}
}

// A quiet tunnel says how many streams it is carrying, which is the one
// thing about it the meter cannot say. A busy one says how fast instead:
// a tunnel with two streams moving nothing is stuck, and the row has to
// be able to say so.
func TestATunnelRowSaysHowManyStreamsItHas(t *testing.T) {
	s := sshtest.New(t)
	echo := echoAt(t)
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	host := connectedTo(t, a, s)

	a.openTunnel(host, remote.Tunnel{
		Kind: remote.LocalForward, Listen: "127.0.0.1:0", Target: echo,
	})
	row := tunnelRow(t, a)
	if got := a.note(conns.Row{Entry: row, State: meter.Opened}, time.Now()); got != "opened" {
		t.Fatalf("a tunnel with nothing on it says %q, want opened", got)
	}

	addr := a.tunnels[row].f.Addr()
	var open []net.Conn
	for i := 0; i < 2; i++ {
		c, err := net.Dial("tcp", addr)
		if err != nil {
			t.Fatalf("dial: %v", err)
		}
		open = append(open, c)
		t.Cleanup(func() { _ = c.Close() })
	}
	waitFor(t, a, "both streams to be counted", func() bool {
		return a.tunnels[row].f.Streams() == 2
	})

	// Quiet, so it says what it is carrying.
	a.refreshPanel(time.Now())
	if got := a.note(conns.Row{Entry: row, State: meter.Settled}, time.Now()); got != "2 streams" {
		t.Fatalf("the row says %q, want 2 streams", got)
	}
	// Busy, so it says how fast instead.
	row.Meter.Moved(64*1024, 0, time.Now())
	a.note(conns.Row{Entry: row, State: meter.Active}, time.Now())
	row.Meter.Moved(64*1024, 0, time.Now().Add(time.Second))
	got := a.note(conns.Row{Entry: row, State: meter.Active}, time.Now().Add(time.Second))
	if !strings.Contains(got, "/s") {
		t.Fatalf("a busy tunnel says %q, want a speed", got)
	}
}

// A SOCKS proxy opens over the connection and reaches what each stream
// asks for.
func TestOpenASocksProxyThroughTheDialog(t *testing.T) {
	s := sshtest.New(t)
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	host := connectedTo(t, a, s)

	if err := a.openSocksHere(); err != nil {
		t.Fatalf("openSocksHere: %v", err)
	}
	f := waitForDialog(t, a, "SOCKS proxy over "+host)
	typeInto(t, a, f, "127.0.0.1:0")
	pressButton(t, a, f, "Open")
	a.pump.run()

	if len(a.tunnels) != 1 {
		t.Fatalf("the window holds %d tunnels, want the proxy", len(a.tunnels))
	}
	row := tunnelRow(t, a)
	if !strings.Contains(row.Label, "socks5") {
		t.Fatalf("the row says %q, want it to say what it is", row.Label)
	}
	if got := a.tunnels[row].f.Tunnel().Kind; got != remote.DynamicForward {
		t.Fatalf("the tunnel is %v, want a dynamic one", got)
	}
}

// A SOCKS proxy takes no address to reach, so the dialog does not ask
// for one.
func TestTheSocksDialogAsksForOneThingOnly(t *testing.T) {
	s := sshtest.New(t)
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	host := connectedTo(t, a, s)

	if err := a.openSocksHere(); err != nil {
		t.Fatalf("openSocksHere: %v", err)
	}
	f := waitForDialog(t, a, "SOCKS proxy over "+host)
	if len(f.Fields()) != 1 {
		t.Fatalf("the dialog has %d fields, want the one it needs", len(f.Fields()))
	}
}

// typeInto fills a dialog's fields in order, from the first one.
func typeInto(t *testing.T, a *testApp, f *ui.Form, values ...string) {
	t.Helper()
	for i, value := range values {
		if i >= len(f.Fields()) {
			t.Fatalf("the dialog has %d fields, want at least %d", len(f.Fields()), len(values))
		}
		f.Fields()[i].SetText(value)
	}
}

// waitForDialogPrefix runs the pump until a dialog whose title starts
// with what is asked for is on the stack.
func waitForDialogPrefix(t *testing.T, a *testApp, prefix string) *ui.Form {
	t.Helper()
	deadline := time.Now().Add(waitBudget)
	for time.Now().Before(deadline) {
		a.pump.run()
		if f, ok := a.root.Modal().(*ui.Form); ok && strings.HasPrefix(f.Title, prefix) {
			return f
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("no dialog opened whose title starts with %q", prefix)
	return nil
}
