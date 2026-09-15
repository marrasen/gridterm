package main

import (
	"errors"
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
	"github.com/marrasen/gridterm/ui/term"
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
	host := serverConfig(t, s).Target()
	a.connect(serverConfig(t, s))
	// The pane opens as soon as the connecting starts, so what says the
	// machine was reached is the window holding it.
	waitFor(t, a, "the connection to "+host, func() bool {
		return a.machines[host] != nil && a.paneOn[paneFor(a, host)] != nil
	})
	return host
}

// paneFor is the pane a machine's connection is drawn in, or nil.
func paneFor(a *testApp, host string) *term.Terminal {
	for pane, m := range a.paneOn {
		if m != nil && m.at.name == host {
			return pane
		}
	}
	return nil
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

	// The row says the port it was given, which is the only way to learn
	// it when the tunnel asked for any free one.
	addr := a.tunnels[row].f.Addr()
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("the tunnel is listening on %q: %v", addr, err)
	}
	if port == "0" {
		t.Fatal("the tunnel was never given a port")
	}
	if !strings.Contains(row.Label, ":"+port) {
		t.Fatalf("the row says %q, want the port %q it is listening on", row.Label, port)
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
	addr := a.tunnels[row].f.Addr()

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
	if row.Note != "" {
		t.Errorf("the row still says %q, which was true while it was running", row.Note)
	}
	// The port is really let go of. A local forward listens on a socket
	// of this machine, which the far end dropping does nothing to: left
	// alone it would keep accepting for the life of the window, with
	// nothing able to take it back.
	if c, err := net.Dial("tcp", addr); err == nil {
		_ = c.Close()
		t.Fatal("the tunnel is still listening after its connection dropped")
	}
	// And it can still be taken off the panel.
	if err := row.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

// A stream that fails is shown once. A dialog for every one of them
// would be a dialog for every dead name a browser looks up through a
// proxy, and every one costs a layer the size of the window.
func TestOneFailedStreamIsShown_TheRestAreCounted(t *testing.T) {
	s := sshtest.New(t)
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	host := connectedTo(t, a, s)

	// Port 9 on the far machine, which the test server refuses.
	a.openTunnel(host, remote.Tunnel{
		Kind: remote.LocalForward, Listen: "127.0.0.1:0", Target: "127.0.0.1:9",
	})
	row := tunnelRow(t, a)
	addr := a.tunnels[row].f.Addr()

	const tries = 6
	for i := 0; i < tries; i++ {
		c, err := net.Dial("tcp", addr)
		if err != nil {
			t.Fatalf("dial: %v", err)
		}
		_ = c.Close()
	}
	// Both: the row says how many failed only once none are still
	// live, and a stream that has failed is not finished being let go
	// of at the moment its failure is counted.
	waitFor(t, a, "every failure to be counted and let go of", func() bool {
		open := a.tunnels[row]
		return open != nil && open.trouble >= tries && open.f.Streams() == 0
	})

	if got := len(a.modals); got != 1 {
		t.Fatalf("%d dialogs are stacked, want the one that says what is wrong", got)
	}
	// And the row says how many there have been.
	a.refreshPanel(time.Now())
	if got := a.note(conns.Row{Entry: row, State: meter.Settled}, time.Now()); !strings.Contains(got, "failed") {
		t.Fatalf("the row says %q, want it to say how many streams failed", got)
	}
}

// A tunnel whose listener has failed is taken away rather than left as a
// row that reads as open.
func TestATunnelThatStopsOnItsOwnIsTakenAway(t *testing.T) {
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

	a.tunnelStopped(row, errors.New("stopped accepting: out of handles"))

	if len(a.tunnels) != 0 {
		t.Fatalf("the window still holds %d tunnels", len(a.tunnels))
	}
	if got := row.State(time.Now()); got != meter.Closed {
		t.Fatalf("the row is %v after the tunnel stopped, want closed", got)
	}
	f := waitForDialogPrefix(t, a, "The tunnel")
	if !strings.Contains(strings.Join(f.Lines, " "), "out of handles") {
		t.Fatalf("the dialog says %q, want the reason in it", f.Lines)
	}
	// The row can still be cleared, and clearing it is all that is left.
	if err := row.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

// Every kind of tunnel that would be open to the network asks first, and
// says what it would let through.
func TestEveryExposedTunnelAsksFirst(t *testing.T) {
	s := sshtest.New(t)
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	host := connectedTo(t, a, s)

	for _, tc := range []struct {
		name string
		t    remote.Tunnel
		says string
	}{
		{"a local one", remote.Tunnel{
			Kind: remote.LocalForward, Listen: "0.0.0.0:0", Target: "db:5432"}, "db:5432"},
		{"a remote one", remote.Tunnel{
			Kind: remote.RemoteForward, Listen: "0.0.0.0:0", Target: "db:5432"}, host},
		{"a proxy", remote.Tunnel{
			Kind: remote.DynamicForward, Listen: "0.0.0.0:0"}, "wherever"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a.confirmTunnel(host, tc.t)
			f, ok := a.root.Modal().(*ui.Form)
			if !ok {
				t.Fatalf("nothing asked: %T", a.root.Modal())
			}
			said := strings.Join(f.Lines, " ")
			if !strings.Contains(said, tc.says) {
				t.Errorf("the question says %q, want it to name %q", said, tc.says)
			}
			// The clause the question turns on is the last one, so none of
			// it may be lost to the edge of the box.
			if !strings.Contains(said, "with nothing asked") {
				t.Errorf("the question says %q, and leaves out what it costs", said)
			}
			for _, line := range f.Lines {
				if len(line) > errorLineWidth {
					t.Errorf("a line is %d wide, which a dialog trims: %q", len(line), line)
				}
			}
			pressButton(t, a, f, "Cancel")
			a.pump.run()
			if len(a.tunnels) != 0 {
				t.Fatalf("%d tunnels after cancelling", len(a.tunnels))
			}
		})
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
	if got := a.note(conns.Row{Entry: row, State: meter.Opened}, time.Now()); got != "" {
		t.Fatalf("a tunnel with nothing on it says %q, and the dot says the rest", got)
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
	// Busy, and it still says what it is carrying: how fast is the
	// graph's business now, and a count is what the graph cannot say.
	row.Meter.Moved(64*1024, 0, time.Now())
	a.note(conns.Row{Entry: row, State: meter.Active}, time.Now())
	row.Meter.Moved(64*1024, 0, time.Now().Add(time.Second))
	got := a.note(conns.Row{Entry: row, State: meter.Active}, time.Now().Add(time.Second))
	if got != "2 streams" {
		t.Fatalf("a busy tunnel says %q, want what it is carrying", got)
	}
	if strings.Contains(got, "/s") {
		t.Fatalf("a busy tunnel says %q, want no speed in words", got)
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

// A remote forward is asked about whatever address it names. Where the
// far machine really binds it is the far machine's decision, and
// gridterm is never told: a server set to share forwarded ports opens
// them to its whole network however the client asked.
func TestARemoteForwardAlwaysAsks(t *testing.T) {
	s := sshtest.New(t)
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	host := connectedTo(t, a, s)

	a.confirmTunnel(host, remote.Tunnel{
		Kind: remote.RemoteForward, Listen: "127.0.0.1:9000", Target: "127.0.0.1:80",
	})
	f, ok := a.root.Modal().(*ui.Form)
	if !ok {
		t.Fatalf("a remote forward opened with nothing asked: %T", a.root.Modal())
	}
	said := strings.Join(f.Lines, " ")
	if !strings.Contains(said, "its own choice") {
		t.Fatalf("the question says %q, and does not say the server decides", said)
	}
	if len(a.tunnels) != 0 {
		t.Fatal("the tunnel was opened before the question was answered")
	}
}

// A tunnel that asked for any free port is named by the port it was
// given, everywhere the user reads about it.
func TestATunnelIsNamedByThePortItWasGiven(t *testing.T) {
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
	_, port, err := net.SplitHostPort(a.tunnels[row].f.Addr())
	if err != nil {
		t.Fatalf("the tunnel is listening on %q: %v", a.tunnels[row].f.Addr(), err)
	}
	if port == "0" {
		t.Fatal("the tunnel was never given a port")
	}
	if !strings.Contains(row.Label, ":"+port) {
		t.Fatalf("the row says %q, want the port %q it was given", row.Label, port)
	}
	// And what the tunnel calls itself agrees with the row, so a failure
	// on it does not name a port that never existed.
	if got := a.tunnels[row].f.Tunnel().String(); !strings.Contains(got, port) {
		t.Fatalf("the tunnel calls itself %q, want the port %q it was given", got, port)
	}
}
