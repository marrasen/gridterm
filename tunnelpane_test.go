package main

import (
	"io"
	"net"
	"slices"
	"strings"
	"testing"

	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/input"
	"github.com/marrasen/gridterm/internal/sshtest"
	"github.com/marrasen/gridterm/remote"
	"github.com/marrasen/gridterm/ui/term"
)

// aTunnelTo opens a tunnel to a target and answers its row.
func aTunnelTo(t *testing.T, a *testApp, host, target string) *conns.Entry {
	t.Helper()
	if _, err := a.startTunnel(host, remote.Tunnel{
		Kind: remote.LocalForward, Listen: "127.0.0.1:0", Target: target,
	}); err != nil {
		t.Fatalf("open the tunnel: %v", err)
	}
	for e := range a.tunnels {
		return e
	}
	t.Fatal("the tunnel has no row")
	return nil
}

// tunnelPaneText is what a tunnel's pane says.
func tunnelPaneText(t *testing.T, a *testApp, e *conns.Entry) string {
	t.Helper()
	pane := a.tunnelPanes[e]
	if pane == nil {
		t.Fatal("the tunnel has no pane")
	}
	return paneText(pane)
}

// pickOnPane presses one of the choices along the bottom of a pane,
// the way the user does: along to it and enter.
func pickOnPane(t *testing.T, a *testApp, pane *term.Terminal, label string) {
	t.Helper()
	at := slices.Index(pane.Choices(), label)
	if at < 0 {
		t.Fatalf("the pane offers no %q, only %v", label, pane.Choices())
	}
	a.focus(pane)
	// To the first one, so the test does not depend on which choice
	// the selection starts on.
	for range len(pane.Choices()) {
		sendKey(t, a, press(input.KeyLeft, 0))
	}
	for range at {
		sendKey(t, a, press(input.KeyRight, 0))
	}
	sendKey(t, a, press(input.KeyEnter, 0))
}

// sayThrough opens a connection to an address, says something and
// reads the answer back.
func sayThrough(t *testing.T, at, said string) {
	t.Helper()
	c, err := net.Dial("tcp", at)
	if err != nil {
		t.Fatalf("reach %s: %v", at, err)
	}
	defer c.Close()
	if _, err := io.WriteString(c, said); err != nil {
		t.Fatalf("say it: %v", err)
	}
	back := make([]byte, len(said))
	if _, err := io.ReadFull(c, back); err != nil {
		t.Fatalf("read it back: %v", err)
	}
}

// withATunnelPane opens a tunnel to the echo server and its pane.
func withATunnelPane(t *testing.T) (*testApp, *conns.Entry, *term.Terminal) {
	t.Helper()
	s := sshtest.New(t)
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	withDialogs(t, a)
	a.commands()
	host := connectedTo(t, a, s)
	e := aTunnelTo(t, a, host, echoAt(t))
	e.Reveal()
	pane := a.tunnelPanes[e]
	if pane == nil {
		t.Fatal("clicking the tunnel opened no pane")
	}
	return a, e, pane
}

// Clicking a tunnel opens a pane that says what it has been doing.
func TestATunnelsPaneSaysWhatItHasBeenDoing(t *testing.T) {
	a, e, _ := withATunnelPane(t)

	waitFor(t, a, "the pane to say the tunnel opened", func() bool {
		return strings.Contains(tunnelPaneText(t, a, e), "opened")
	})
	if got := len(a.tunnelPanes); got != 1 {
		t.Errorf("%d tunnel panes", got)
	}
	// And the window's own log is somewhere else: a pane reading a log
	// is not the log pane.
	if a.logPane() != nil {
		t.Error("the tunnel's pane was taken for the window's log")
	}
}

// Clicking it again goes to the pane that is open rather than opening
// a second one on the same tunnel.
func TestClickingATunnelTwiceOpensOnePane(t *testing.T) {
	a, e, _ := withATunnelPane(t)

	e.Reveal()

	if got := len(a.tunnelPanes); got != 1 {
		t.Errorf("%d panes for one tunnel", got)
	}
}

// The pane is where the tunnel is closed from.
func TestTheTunnelsPaneClosesIt(t *testing.T) {
	a, _, pane := withATunnelPane(t)

	pickOnPane(t, a, pane, "Close the tunnel")

	if len(a.tunnels) != 0 {
		t.Errorf("%d tunnels after closing it from its pane", len(a.tunnels))
	}
	// The pane stays, with nothing left to press: what the tunnel did
	// is still worth reading.
	if _, live := a.panes[pane]; !live {
		t.Error("the pane went with the tunnel")
	}
	if got := pane.Asking(); got != "" {
		t.Errorf("the pane still offers %q", got)
	}
}

// Nothing is written down until somebody asks. A tunnel carries
// whatever it carries.
func TestATunnelIsNotWatchedUntilItIsAskedFor(t *testing.T) {
	a, e, _ := withATunnelPane(t)
	open := a.tunnels[e]

	if open.watching.on() {
		t.Fatal("a tunnel nobody asked about is being written down")
	}
	sayThrough(t, open.f.Addr(), "hello there")

	if got := tunnelPaneText(t, a, e); strings.Contains(got, "hello there") {
		t.Errorf("the traffic was written down with nobody watching:\n%s", got)
	}
}

// Asked for, what goes through is written down.
func TestWatchingATunnelWritesTheTrafficDown(t *testing.T) {
	a, e, pane := withATunnelPane(t)

	pickOnPane(t, a, pane, "Watch the traffic")
	sayThrough(t, a.tunnels[e].f.Addr(), "hello there")

	waitFor(t, a, "the pane to show the traffic", func() bool {
		return strings.Contains(tunnelPaneText(t, a, e), "hello there")
	})
	if !slices.Contains(pane.Choices(), "Stop watching") {
		t.Errorf("the pane offers %v, want a way to stop", pane.Choices())
	}
}

// And stopping stops it.
func TestStoppingWatchingStopsIt(t *testing.T) {
	a, e, pane := withATunnelPane(t)
	pickOnPane(t, a, pane, "Watch the traffic")

	pickOnPane(t, a, pane, "Stop watching")

	if a.tunnels[e].watching.on() {
		t.Error("it is still being written down")
	}
	sayThrough(t, a.tunnels[e].f.Addr(), "after it stopped")
	if got := tunnelPaneText(t, a, e); strings.Contains(got, "after it stopped") {
		t.Errorf("the traffic is still being written down:\n%s", got)
	}
}

// Closing the pane stops the writing, so a tunnel nobody is looking at
// costs nothing.
func TestClosingThePaneStopsTheWriting(t *testing.T) {
	a, e, pane := withATunnelPane(t)
	pickOnPane(t, a, pane, "Watch the traffic")

	if err := a.closePane(pane); err != nil {
		t.Fatalf("close the pane: %v", err)
	}
	a.forgetTunnelPanes()

	if a.tunnels[e].watching.on() {
		t.Error("the traffic is still being written down with no pane to read it")
	}
	if len(a.tunnelPanes) != 0 {
		t.Errorf("%d panes are still held for tunnels", len(a.tunnelPanes))
	}
}

// A chunk of traffic is written the way a pane can show it: which way
// it went, and the bytes with nothing in them that would draw over the
// window.
func TestAChunkOfTrafficIsWrittenReadably(t *testing.T) {
	got := string(chunkLines(true, []byte("GET / HTTP/1.1\r\nHost: x\r\n\r\n")))

	if !strings.HasPrefix(got, "→ 27 bytes") {
		t.Errorf("it starts %q", strings.SplitN(got, "\n", 2)[0])
	}
	if !strings.Contains(got, "GET / HTTP/1.1\nHost: x") {
		t.Errorf("the text did not come through:\n%q", got)
	}
	if strings.Contains(got, "\r") {
		t.Error("a carriage return survived")
	}

	got = string(chunkLines(false, []byte{0x00, 0x1b, 'o', 'k', 0x7f}))
	if !strings.HasPrefix(got, "← 5 bytes") {
		t.Errorf("it starts %q", strings.SplitN(got, "\n", 2)[0])
	}
	if !strings.Contains(got, "..ok.") {
		t.Errorf("the bytes came out as %q", got)
	}
}

// A very large chunk is cut down, because a pane holds what a person
// reads and not what a file transfer sends.
func TestAVeryLargeChunkIsCutDown(t *testing.T) {
	got := string(chunkLines(true, make([]byte, mostTapBytes*3)))

	if !strings.Contains(got, "first 4096 shown") {
		t.Errorf("it does not say it was cut: %q", strings.SplitN(got, "\n", 2)[0])
	}
	if len(got) > mostTapBytes*2 {
		t.Errorf("it wrote %d bytes down for one chunk", len(got))
	}
}
