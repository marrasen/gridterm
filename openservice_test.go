package main

import (
	"net"
	"strings"
	"testing"

	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/internal/sshtest"
)

// opensInstead makes the browser record what it was given rather than
// start one, and puts the real one back when the test ends.
func opensInstead(t *testing.T) *[]string {
	t.Helper()
	var opened []string
	was := openInBrowser
	openInBrowser = func(at string) error {
		opened = append(opened, at)
		return nil
	}
	t.Cleanup(func() { openInBrowser = was })
	return &opened
}

// An address a pane on this machine printed goes straight to the
// browser: this machine's localhost is this machine's.
func TestAnAddressHereGoesStraightToTheBrowser(t *testing.T) {
	a := newTestApp(t, 80, 24)
	opened := opensInstead(t)

	if err := a.openLinkFrom(conns.Local, "http://localhost:5173/app"); err != nil {
		t.Fatalf("open it: %v", err)
	}

	if len(*opened) != 1 || (*opened)[0] != "http://localhost:5173/app" {
		t.Errorf("the browser was given %v", *opened)
	}
	if len(a.tunnels) != 0 {
		t.Errorf("%d tunnels were opened for an address on this machine", len(a.tunnels))
	}
}

// An address on a machine at the far end is only that machine's
// localhost, so it needs a tunnel before a browser here can reach it.
func TestAnAddressOverThereNeedsATunnel(t *testing.T) {
	for _, tc := range []struct {
		at         string
		wantTunnel bool
	}{
		{"http://localhost:5173/", true},
		{"http://127.0.0.1:8080", true},
		{"http://0.0.0.0:3000/app", true},
		// Named machines are reachable from here already, or are not
		// ours to guess about.
		{"http://build.example.com:8080/", false},
		{"https://example.com/", false},
		// Nothing to tunnel to without a port.
		{"http://localhost/", false},
	} {
		target, ok := serviceOnTheFarEnd("margit", tc.at)
		if ok != tc.wantTunnel {
			t.Errorf("%q: a tunnel is %v, want %v", tc.at, ok, tc.wantTunnel)
			continue
		}
		if !ok {
			continue
		}
		if _, port, err := net.SplitHostPort(target); err != nil || !strings.Contains(tc.at, port) {
			t.Errorf("%q would tunnel to %q", tc.at, target)
		}
	}
}

// The browser is sent to this end of the tunnel, keeping the path the
// program printed.
func TestTheBrowserIsSentToThisEndOfTheTunnel(t *testing.T) {
	for _, tc := range []struct{ at, local, want string }{
		{"http://localhost:5173/app", "127.0.0.1:49761", "http://127.0.0.1:49761/app"},
		{"http://0.0.0.0:3000", "127.0.0.1:1234", "http://127.0.0.1:1234"},
		{"http://localhost:5173/x?y=1", "127.0.0.1:9", "http://127.0.0.1:9/x?y=1"},
	} {
		got, err := throughTunnel(tc.at, tc.local)
		if err != nil {
			t.Errorf("%q through %q: %v", tc.at, tc.local, err)
			continue
		}
		if got != tc.want {
			t.Errorf("%q through %q gave %q, want %q", tc.at, tc.local, got, tc.want)
		}
	}
}

// Two names for one machine's own loopback are one target, so the
// same service clicked twice does not open a second tunnel.
func TestTwoNamesForOneLoopbackAreOneTarget(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		same bool
	}{
		{"127.0.0.1:5173", "localhost:5173", true},
		{"127.0.0.1:5173", "0.0.0.0:5173", true},
		{"127.0.0.1:5173", "127.0.0.1:5174", false},
		{"build.example:80", "127.0.0.1:80", false},
		{"build.example:80", "BUILD.EXAMPLE:80", true},
	} {
		if got := sameTarget(tc.a, tc.b); got != tc.same {
			t.Errorf("%q and %q came out as same=%v", tc.a, tc.b, got)
		}
	}
}

// A machine nothing is connected to cannot have a tunnel opened over
// it, and the click says so rather than opening a browser at a port
// with nothing behind it.
func TestAnAddressOnAMachineThatHasGoneSaysSo(t *testing.T) {
	a := newTestApp(t, 80, 24)
	opened := opensInstead(t)

	err := a.openLinkFrom("margit", "http://localhost:5173/")

	if err == nil {
		t.Fatal("it said the address opened")
	}
	if !strings.Contains(err.Error(), "margit") {
		t.Errorf("it said %q, which does not name the machine", err)
	}
	if len(*opened) != 0 {
		t.Errorf("the browser was given %v", *opened)
	}
}

// Clicking an address a server printed opens a tunnel to it and sends
// the browser to this end of that tunnel.
func TestClickingAServersAddressTunnelsToIt(t *testing.T) {
	s := sshtest.New(t)
	echo := echoAt(t)
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	host := connectedTo(t, a, s)
	opened := opensInstead(t)
	_, port, err := net.SplitHostPort(echo)
	if err != nil {
		t.Fatalf("read the echo port: %v", err)
	}

	if err := a.openLinkFrom(host, "http://localhost:"+port+"/app"); err != nil {
		t.Fatalf("open it: %v", err)
	}

	if len(a.tunnels) != 1 {
		t.Fatalf("%d tunnels, want the one that reaches it", len(a.tunnels))
	}
	if len(*opened) != 1 {
		t.Fatalf("the browser was given %v", *opened)
	}
	at := (*opened)[0]
	if !strings.HasPrefix(at, "http://127.0.0.1:") || !strings.HasSuffix(at, "/app") {
		t.Errorf("the browser was sent to %q", at)
	}
	if strings.Contains(at, ":"+port+"/") {
		t.Errorf("the browser was sent to the far machine's own port: %q", at)
	}
}

// Clicking the same address again uses the tunnel that is already
// there, so a page reloaded twice does not fill the sidebar.
func TestTheSameAddressTwiceOpensOneTunnel(t *testing.T) {
	s := sshtest.New(t)
	echo := echoAt(t)
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	host := connectedTo(t, a, s)
	opened := opensInstead(t)
	_, port, _ := net.SplitHostPort(echo)

	for range 3 {
		if err := a.openLinkFrom(host, "http://localhost:"+port+"/"); err != nil {
			t.Fatalf("open it: %v", err)
		}
	}

	if len(a.tunnels) != 1 {
		t.Errorf("%d tunnels for the same address three times", len(a.tunnels))
	}
	if len(*opened) != 3 {
		t.Fatalf("the browser was opened %d times", len(*opened))
	}
	if (*opened)[0] != (*opened)[2] {
		t.Errorf("the same address gave %q and then %q", (*opened)[0], (*opened)[2])
	}
}

// The tunnel it opens is on this machine only. It was opened by a
// click rather than asked for, so it is not one to put on the network.
func TestTheTunnelAClickOpensIsNotOnTheNetwork(t *testing.T) {
	s := sshtest.New(t)
	echo := echoAt(t)
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	host := connectedTo(t, a, s)
	opensInstead(t)
	_, port, _ := net.SplitHostPort(echo)

	if err := a.openLinkFrom(host, "http://localhost:"+port+"/"); err != nil {
		t.Fatalf("open it: %v", err)
	}

	if a.root.Modal() != nil {
		t.Errorf("it asked about a tunnel on this machine only: %T", a.root.Modal())
	}
	for _, open := range a.tunnels {
		at := open.f.Addr()
		host, _, err := net.SplitHostPort(at)
		if err != nil {
			t.Fatalf("read %q: %v", at, err)
		}
		if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
			t.Errorf("the tunnel listens on %q, which is not this machine only", at)
		}
	}
}
