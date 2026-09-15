package main

import (
	"context"
	"errors"
	"fmt"
	"image/color"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"

	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/internal/sshtest"
	"github.com/marrasen/gridterm/remote"
	"github.com/marrasen/gridterm/serve"
	"github.com/marrasen/gridterm/ui"
	"github.com/marrasen/gridterm/ui/term"
	"github.com/marrasen/gridterm/vfs"
)

// A real window serving, and a real window working in its shell.
//
// Everything below this is exercised in pieces elsewhere. This is the
// one test that says the pieces are wired to each other: the dialog
// opens a real port, a key in the real file gets in, and what comes
// back is the output of a shell on the machine being served.
func TestOneWindowWorksInAnotherMachinesShell(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	mine, line := aKeyPair(t)
	withServing(t, a, line)
	if err := a.startServing("0", whereHere); err != nil {
		t.Fatalf("serve: %v", err)
	}

	w, err := serve.Dial(context.Background(), serve.DialConfig{
		Addr: a.server.Addr(), Keys: []ssh.Signer{mine},
		HostKey: ssh.FixedHostKey(a.server.HostKey()),
	})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer w.Close()

	sess, err := w.Open(80, 24)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer sess.Close()

	if _, err := sess.Write([]byte("echo taken-over-ok\r")); err != nil {
		t.Fatalf("write: %v", err)
	}
	var got strings.Builder
	buf := make([]byte, 4096)
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		n, err := sess.Read(buf)
		got.Write(buf[:n])
		if strings.Contains(got.String(), "taken-over-ok") {
			return
		}
		if err != nil {
			t.Fatalf("read: %v, having got %q", err, got.String())
		}
	}
	t.Fatalf("no answer from the shell: %q", got.String())
}

// The whole path a user takes: one window serves, another takes it
// over from its own dialog, and a pane here draws a shell there.
func TestTakingOverAWindowOpensAPaneOnIt(t *testing.T) {
	host := newTestApp(t, 90, 30)
	withDialogs(t, host)
	keyFile, line := aKeyFile(t)
	withServing(t, host, line)
	if err := host.startServing("0", whereHere); err != nil {
		t.Fatalf("serve: %v", err)
	}

	client := newTestApp(t, 90, 30)
	withDialogs(t, client)
	withPanel(t, client)
	panes := len(client.panes)

	if err := client.takeOver(host.server.Addr(), keyFile); err != nil {
		t.Fatalf("take over: %v", err)
	}
	// The window has never been reached before, so its key is offered
	// and has to be accepted, the same as any other machine's.
	answer(t, client, "Connect")

	waitFor(t, client, "the window to be taken over", func() bool {
		return client.windows[host.server.Addr()] != nil && len(client.panes) > panes
	})
	if got := len(host.server.Clients()); got != 1 {
		t.Errorf("%d clients on the serving window, want one", got)
	}

	// And the panel says so, under the address it was reached at.
	var named bool
	for _, line := range panelText(client, panelNow) {
		if strings.Contains(line, host.server.Addr()) {
			named = true
		}
	}
	if !named {
		t.Errorf("the panel does not name the window taken over: %v", panelText(client, panelNow))
	}
}

// Letting go of a window takes the panes drawn from it, and hangs up.
func TestLettingGoOfATakenWindowTakesItsPanes(t *testing.T) {
	host := newTestApp(t, 90, 30)
	withDialogs(t, host)
	keyFile, line := aKeyFile(t)
	withServing(t, host, line)
	if err := host.startServing("0", whereHere); err != nil {
		t.Fatalf("serve: %v", err)
	}
	addr := host.server.Addr()

	client := newTestApp(t, 90, 30)
	withDialogs(t, client)
	withPanel(t, client)
	panes := len(client.panes)
	if err := client.takeOver(addr, keyFile); err != nil {
		t.Fatalf("take over: %v", err)
	}
	answer(t, client, "Connect")
	waitFor(t, client, "the window to be taken over", func() bool {
		return client.windows[addr] != nil && len(client.panes) > panes
	})

	if err := client.dropWindow(addr); err != nil {
		t.Fatalf("drop: %v", err)
	}

	if client.windows[addr] != nil {
		t.Error("the window is still held")
	}
	if got := len(client.panes); got != panes {
		t.Errorf("%d panes are left, want the %d there were before", got, panes)
	}
	waitFor(t, client, "the serving window to see it go", func() bool {
		return len(host.server.Clients()) == 0
	})
}

// A window that is not serving is not taken over, and the pane that was
// watching says why.
func TestTakingOverAWindowThatIsNotThereFails(t *testing.T) {
	client := newTestApp(t, 90, 30)
	withDialogs(t, client)
	withPanel(t, client)
	keyFile, _ := aKeyFile(t)

	// A port nothing is listening on.
	if err := client.takeOver("127.0.0.1:1", keyFile); err != nil {
		t.Fatalf("take over: %v", err)
	}
	pane := newestPane(t, client)

	waitFor(t, client, "the pane to say why it could not", func() bool {
		return strings.Contains(paneText(pane), "The connection was not made")
	})
	if got := paneText(pane); !strings.Contains(got, "127.0.0.1:1") {
		t.Errorf("it does not say which machine: %q", got)
	}
	if client.windows["127.0.0.1:1"] != nil {
		t.Error("a window that answered nothing was held anyway")
	}
}

// The window being served says so, and whoever is sitting at it can
// end the connection from there.
//
// Whoever is at this screen owns it, however far away the person using
// it is.
func TestTheServedWindowSaysItIsBeingServed(t *testing.T) {
	host := newTestApp(t, 90, 30)
	withDialogs(t, host)
	withPanel(t, host)
	keyFile, line := aKeyFile(t)
	withServing(t, host, line)
	if err := host.startServing("0", whereHere); err != nil {
		t.Fatalf("serve: %v", err)
	}
	addr := host.server.Addr()

	client := newTestApp(t, 90, 30)
	withDialogs(t, client)
	withPanel(t, client)
	if err := client.takeOver(addr, keyFile); err != nil {
		t.Fatalf("take over: %v", err)
	}
	answer(t, client, "Connect")
	waitFor(t, client, "the window to be taken over", func() bool {
		return client.windows[addr] != nil
	})

	waitFor(t, host, "the served window to say so", func() bool {
		for _, line := range panelText(host, panelNow) {
			if strings.Contains(line, "serving") {
				return true
			}
		}
		return false
	})

	// And the row can end it from here.
	var ended func() error
	for _, g := range host.registry.Groups(panelNow) {
		for _, row := range g.Rows {
			if strings.HasPrefix(row.Entry.Label, "serving") {
				ended = row.Entry.Close
			}
		}
	}
	if ended == nil {
		t.Fatal("the row has no way to end the connection")
	}
	if err := ended(); err != nil {
		t.Fatalf("end it: %v", err)
	}
	waitFor(t, host, "the row to go when the window does", func() bool {
		for _, line := range panelText(host, panelNow) {
			if strings.Contains(line, "serving") {
				return false
			}
		}
		return true
	})
}

// A key the agent holds can still sign when the dial happens.
//
// It could not. The connection to the agent was closed as keysFor
// returned, and an agent signer does its signing over that socket: the
// key was offered, accepted, and then failed to sign. One such key
// fails the whole handshake without the others being tried, so the
// ordinary path -- no key file named, the key in the agent -- could not
// connect at all.
func TestAKeyFromTheAgentCanStillSign(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	signer, line := aKeyPair(t)
	withServing(t, a, line)
	if err := a.startServing("0", whereHere); err != nil {
		t.Fatalf("serve: %v", err)
	}

	// An agent whose keys stop working once it is closed, which is what
	// a real one does. Reached the way the window reaches one, so that
	// the whole path is what is being tried and not a piece of it.
	shut := &shutting{signer: signer}
	known := filepath.Join(t.TempDir(), "known_windows")
	if err := os.WriteFile(known, []byte(knownLine(t, a)), 0o600); err != nil {
		t.Fatalf("write the known window: %v", err)
	}

	w, err := reachWindow(context.Background(), reach{
		addr: a.server.Addr(), ring: a.keys,
		known: func() (string, error) { return known, nil },
		agent: func() ([]ssh.Signer, io.Closer, error) {
			return []ssh.Signer{shut}, shut, nil
		},
	})

	if err != nil {
		t.Fatalf("take over with a key from the agent: %v", err)
	}
	_ = w.Close()
}

// knownLine is the known_windows line for a window that is serving.
func knownLine(t *testing.T, a *testApp) string {
	t.Helper()
	return knownhosts.Line([]string{a.server.Addr()}, a.server.HostKey()) + "\n"
}

// shutting is a key that stops working once it is closed, the way one
// the SSH agent holds stops working when the socket to it goes.
type shutting struct {
	signer ssh.Signer
	closed bool
}

func (s *shutting) PublicKey() ssh.PublicKey { return s.signer.PublicKey() }

func (s *shutting) Sign(rand io.Reader, data []byte) (*ssh.Signature, error) {
	if s.closed {
		return nil, errors.New("agent: the connection is closed")
	}
	return s.signer.Sign(rand, data)
}

func (s *shutting) Close() error { s.closed = true; return nil }

// With no keys anywhere, the reason says which: an agent that answered
// and failed is a different thing to go and fix from no agent at all.
func TestNoKeysSaysWhyThereAreNone(t *testing.T) {
	a := newTestApp(t, 90, 30)

	_, _, err := keysFor(context.Background(), "", a.keys, nil,
		func() ([]ssh.Signer, io.Closer, error) {
			return nil, nil, errors.New("the agent said no")
		}, nil)

	if err == nil {
		t.Fatal("it found keys where there are none")
	}
	if !strings.Contains(err.Error(), "the agent said no") {
		t.Errorf("it said %v, without saying why there were none", err)
	}
}

// Taking over the same window twice at once asks which one to keep.
//
// Two at once would leave the window holding the second and closing
// neither: a connection nothing could reach, and a serving window
// showing a client that is not there.
func TestTakingOverTheSameWindowTwiceAsks(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	keyFile, _ := aKeyFile(t)

	// A port nothing answers on, so the first is still on its way.
	if err := a.takeOver("127.0.0.1:1", keyFile); err != nil {
		t.Fatalf("first: %v", err)
	}
	if err := a.takeOver("127.0.0.1:1", keyFile); err != nil {
		t.Fatalf("second: %v", err)
	}

	// Asked about rather than refused: the user says whether to wait for
	// the one on its way or to throw it away and start again.
	f := waitForDialog(t, a, "Already connecting to 127.0.0.1:1")
	pressButton(t, a, f, "Leave it")
	if a.connecting != 1 {
		t.Fatalf("%d windows are being taken over, want the first one only", a.connecting)
	}
	if a.opening["127.0.0.1:1"] == nil {
		t.Fatal("the first attempt was let go of")
	}
}

// And one already taken over is refused as well.
func TestTakingOverAWindowAlreadyHeldIsRefused(t *testing.T) {
	host, client, addr := twoWindows(t)
	_ = host

	err := client.takeOver(addr, "")

	if err == nil {
		t.Fatal("it took over the same window twice")
	}
	if !strings.Contains(err.Error(), "already taken over") {
		t.Errorf("it refused with %v", err)
	}
}

// The machine field carries the port a serving window uses, so an
// address with no port in it reaches one.
func TestAMachineWithNoPortGetsTheServingOne(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	keyFile, _ := aKeyFile(t)

	if err := a.takeOver("127.0.0.1", keyFile); err != nil {
		t.Fatalf("take over: %v", err)
	}

	want := fmt.Sprintf("127.0.0.1:%d", servePort)
	if a.opening[want] == nil {
		t.Errorf("it is reaching %v, want %s", mapKeys(a.opening), want)
	}
}

// Giving up on a window still being reached takes its row away and
// leaves nothing behind.
func TestGivingUpOnAWindowLeavesNothing(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	keyFile, _ := aKeyFile(t)
	if err := a.takeOver("127.0.0.1:1", keyFile); err != nil {
		t.Fatalf("take over: %v", err)
	}
	if a.connecting != 1 {
		t.Fatalf("%d connections are being made, want one", a.connecting)
	}

	if err := a.dropWindow("127.0.0.1:1"); err != nil {
		t.Fatalf("give up: %v", err)
	}

	waitFor(t, a, "the row to go", func() bool { return a.connecting == 0 })
	if a.opening["127.0.0.1:1"] != nil {
		t.Error("the window is still being reached")
	}
	for _, line := range panelText(a, panelNow) {
		if strings.Contains(line, "taking over") {
			t.Errorf("a row was left behind: %v", panelText(a, panelNow))
		}
	}
}

// A pane closed on its own is forgotten, so letting go of the window
// afterwards does not report a failure to close it again.
func TestAPaneClosedOnItsOwnIsForgotten(t *testing.T) {
	_, client, addr := twoWindows(t)
	var pane *term.Terminal
	for p, on := range client.paneOnWindow {
		if on.name == addr {
			pane = p
		}
	}
	if pane == nil {
		t.Fatal("no pane was opened on the window")
	}

	if err := client.closePane(pane); err != nil {
		t.Fatalf("close the pane: %v", err)
	}

	if len(client.paneOnWindow) != 0 {
		t.Errorf("%d panes are still recorded", len(client.paneOnWindow))
	}
	if err := client.dropWindow(addr); err != nil {
		t.Errorf("letting go of the window said %v", err)
	}
}

// A window that quits at the far end is let go of here, rather than
// sitting on the panel saying it is taken over.
func TestAWindowThatQuitsIsLetGoOf(t *testing.T) {
	host, client, addr := twoWindows(t)

	// The serving window stops serving, which is what quitting looks
	// like from here.
	if err := host.stopServing(); err != nil {
		t.Fatalf("stop serving: %v", err)
	}

	waitFor(t, client, "the window to be let go of", func() bool {
		return client.windows[addr] == nil
	})
	if len(client.paneOnWindow) != 0 {
		t.Errorf("%d panes are still drawn from it", len(client.paneOnWindow))
	}
}

// The row for a taken-over window is what takes it away, and the panel
// stops naming it.
func TestLettingGoTakesTheRowAway(t *testing.T) {
	_, client, addr := twoWindows(t)

	if err := client.dropWindow(addr); err != nil {
		t.Fatalf("drop: %v", err)
	}

	for _, line := range panelText(client, panelNow) {
		if strings.Contains(line, addr) {
			t.Errorf("the panel still names it: %v", panelText(client, panelNow))
		}
	}
}

// The menu on a taken-over window's row reaches the window, not the
// machines, where it is not.
//
// Every one of those commands went looking among the machines and
// failed -- and "Close the connection" said nothing was connected to a
// window sitting one row above it.
func TestTheCommandsOnAWindowReachTheWindow(t *testing.T) {
	_, client, addr := twoWindows(t)
	client.actOn, client.acting = addr, true
	panes := len(client.panes)

	if err := client.openTerminalHere(); err != nil {
		t.Fatalf("another terminal: %v", err)
	}
	waitFor(t, client, "a second pane on the window", func() bool {
		return len(client.panes) == panes+1
	})

	if err := client.disconnectHere(); err != nil {
		t.Fatalf("close the connection: %v", err)
	}
	if client.windows[addr] != nil {
		t.Error("the window is still held")
	}
}

// A window taken over is offered both of the things that connection
// carries: a terminal on it and its files.
func TestAWindowIsOfferedATerminalAndABrowser(t *testing.T) {
	_, client, addr := twoWindows(t)

	var terminal, files bool
	for _, title := range commandTitles(client) {
		if strings.Contains(title, "Open a terminal on "+addr) {
			terminal = true
		}
		if strings.Contains(title, "Browse files on "+addr) {
			files = true
		}
	}
	if !terminal {
		t.Errorf("it offers no way to open a terminal on %s", addr)
	}
	if !files {
		t.Errorf("it offers no way to browse the files of %s", addr)
	}
}

// Shutting down hangs up on every window taken over.
func TestShuttingDownHangsUpOnEveryWindow(t *testing.T) {
	host, client, addr := twoWindows(t)

	if err := client.closeWindows(); err != nil {
		t.Fatalf("close: %v", err)
	}

	if client.windows[addr] != nil {
		t.Error("a window is still held")
	}
	waitFor(t, host, "the serving window to see it go", func() bool {
		return len(host.server.Clients()) == 0
	})
}

// twoWindows is one window serving and another that has taken it over,
// with a pane open on it.
func twoWindows(t *testing.T) (host, client *testApp, addr string) {
	t.Helper()
	host = newTestApp(t, 90, 30)
	withDialogs(t, host)
	withPanel(t, host)
	keyFile, line := aKeyFile(t)
	withServing(t, host, line)
	if err := host.startServing("0", whereHere); err != nil {
		t.Fatalf("serve: %v", err)
	}
	addr = host.server.Addr()

	client = newTestApp(t, 90, 30)
	withDialogs(t, client)
	withPanel(t, client)
	panes := len(client.panes)
	if err := client.takeOver(addr, keyFile); err != nil {
		t.Fatalf("take over: %v", err)
	}
	answer(t, client, "Connect")
	waitFor(t, client, "a pane on the other window", func() bool {
		return client.windows[addr] != nil && len(client.panes) > panes
	})
	return host, client, addr
}

// mapKeys is what a map is keyed by, for saying what was found instead.
func mapKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// The sidebar shows what the window taken over has open, under it.
func TestTheSidebarShowsWhatTheOtherWindowHasOpen(t *testing.T) {
	host, client, addr := twoWindows(t)

	// Something on the serving window that this one did not open.
	if err := host.openTab(); err != nil {
		t.Fatalf("a shell on the serving window: %v", err)
	}
	host.refreshPanel(panelNow)

	// The serving window now has two things open, and says so.
	waitFor(t, client, "the other window to say it has two open", func() bool {
		return len(client.windows[addr].win.Opens()) == 2
	})

	// And this window shows a row for each, under the window itself.
	client.refreshPanel(panelNow)
	want := len(client.windows[addr].win.Opens())
	var shown int
	for _, row := range client.panel.Rows() {
		if key, ok := row.Key.(remoteKey); ok && key.window == addr {
			shown++
		}
	}
	if shown != want {
		t.Errorf("%d rows are shown for the %d it said it had open", shown, want)
	}
}

// A window this one has not taken over contributes no such rows.
func TestOnlyATakenWindowContributesRemoteRows(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withPanel(t, a)
	a.refreshPanel(panelNow)

	for _, row := range a.panel.Rows() {
		if _, ok := row.Key.(remoteKey); ok {
			t.Errorf("a row for another window appeared: %q", row.Text)
		}
	}
	if got := a.remoteRows("nowhere"); got != nil {
		t.Errorf("a machine that is not a window gave %v", got)
	}
}

// Working in a shell that was already running on the other machine.
//
// This is what taking over a window means, as against opening a new
// shell there: the pane shows what is already on that screen, typing in
// it reaches the program that was running, and the pane keeps running
// over there when this window lets go.
func TestAttachingShowsWhatIsAlreadyOnTheScreen(t *testing.T) {
	host, client, addr := twoWindows(t)

	// Something already on the serving window's screen, put there by
	// the shell running in it.
	hostPane := onlyPaneOn(t, host)
	host.shells[0].out <- []byte("already-here\r\n")
	waitForBoth(t, host, client, "the shell there to say it", func() bool {
		return strings.Contains(paneText(hostPane), "already-here")
	})
	host.refreshPanel(panelNow)

	// The row for it, as this window was told about it.
	var row remoteKey
	waitForBoth(t, host, client, "a row for the shell over there", func() bool {
		client.refreshPanel(panelNow)
		for _, r := range client.panel.Rows() {
			if key, ok := r.Key.(remoteKey); ok && key.window == addr {
				row = key
				return true
			}
		}
		return false
	})

	panes := len(client.panes)
	if err := client.attachHere(row, nil); err != nil {
		t.Fatalf("attach: %v", err)
	}
	waitForBoth(t, host, client, "a pane watching it", func() bool {
		return len(client.panes) == panes+1
	})

	// The screen as it stands, without waiting for the program to say
	// anything more.
	here := newestPane(t, client)
	waitForBoth(t, host, client, "the screen already there", func() bool {
		return strings.Contains(paneText(here), "already-here")
	})

	// What the shell says from now on, which is the whole point: a
	// screen sent once is a photograph, not a window into it.
	host.shells[0].out <- []byte("live-after-attach\r\n")
	waitForBoth(t, host, client, "what it said after the attach", func() bool {
		return strings.Contains(paneText(here), "live-after-attach")
	})

	// One thing open is one row. The row for it over there was there a
	// moment ago, and goes when a pane of this window is showing it.
	client.refreshPanel(panelNow)
	for _, r := range client.panel.Rows() {
		if key, ok := r.Key.(remoteKey); ok && key == row {
			t.Error("it is listed twice: once as a pane and once as a row over there")
		}
	}

	// And the window being watched says so, on the row of the pane
	// being read.
	waitForBoth(t, host, client, "the host to say it is being watched", func() bool {
		host.refreshPanel(panelNow)
		for _, r := range host.panel.Rows() {
			if strings.HasPrefix(r.Note, watchedBy) {
				return true
			}
		}
		return false
	})

	// Choosing the same row again brings that pane forward rather than
	// opening a second one typing into one shell.
	was := len(client.panes)
	if err := client.attachHere(row, nil); err != nil {
		t.Fatalf("attach again: %v", err)
	}
	if len(client.panes) != was {
		t.Errorf("choosing it twice opened another pane on one shell")
	}

	// And typing here reaches the shell there.
	here.Send([]byte("typed-from-here\r"))
	waitForBoth(t, host, client, "what was typed here to reach there", func() bool {
		return strings.Contains(host.shells[0].sentText(), "typed-from-here")
	})

	// Letting go leaves it running over there.
	if err := client.dropWindow(addr); err != nil {
		t.Fatalf("drop: %v", err)
	}
	waitForBoth(t, host, client, "the shell there to be let go of", func() bool {
		return hostPane.Watched() == 0
	})
	host.shells[0].out <- []byte("still-running\r\n")
	waitForBoth(t, host, client, "the shell there to carry on", func() bool {
		return strings.Contains(paneText(hostPane), "still-running")
	})
}

// Attaching to something that is not there says so, and opens nothing.
//
// The window over there says what it has open, and this one asks about
// what it was told. A row that is not in that list is one that has
// closed, and there is nothing to open a pane onto.
func TestAttachingToNothingSaysSo(t *testing.T) {
	_, client, addr := twoWindows(t)
	panes := len(client.panes)

	err := client.attachHere(remoteKey{window: addr, id: "no-such-thing"}, nil)

	if err == nil {
		t.Fatal("it attached to something that is not open")
	}
	if !strings.Contains(err.Error(), "no longer has that open") {
		t.Errorf("it said %v", err)
	}
	if len(client.panes) != panes {
		t.Errorf("it opened a pane anyway")
	}
}

// onlyPaneOn is the one pane a window has.
func onlyPaneOn(t *testing.T, a *testApp) *term.Terminal {
	t.Helper()
	if len(a.panes) != 1 {
		t.Fatalf("%d panes, want one", len(a.panes))
	}
	for pane := range a.panes {
		return pane
	}
	return nil
}

// newestPane is the pane most recently opened, which is the one in
// front.
func newestPane(t *testing.T, a *testApp) *term.Terminal {
	t.Helper()
	pane := a.focusedTerminal()
	if pane == nil {
		t.Fatal("no pane is in front")
	}
	return pane
}

// paneText is what a pane is showing.
func paneText(pane *term.Terminal) string {
	g := grid.New(pane.Size().Cols, pane.Size().Rows, color.RGBA{}, color.RGBA{})
	pane.Draw(g.View())
	var b strings.Builder
	cols, rows := g.Size()
	for y := 0; y < rows; y++ {
		for x := 0; x < cols; x++ {
			if r := g.At(x, y).Rune; r != 0 {
				b.WriteRune(r)
			}
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// The row chosen is the one attached to, when the window over there has
// more than one thing open.
//
// A row is named by a machine and a place in its list, so a client that
// ignored the place would hand back the first shell whichever row was
// picked, and the user would type into the wrong one.
func TestTheRowChosenIsTheOneAttachedTo(t *testing.T) {
	host, client, addr := twoWindows(t)
	first := onlyPaneOn(t, host)

	// A second pane over there, and a name for each so the rows can be
	// told apart. The shells this window opened over there are in the
	// same list, so the new one is counted from where the list stood.
	second := len(host.shells)
	if err := host.openTab(); err != nil {
		t.Fatalf("a second shell there: %v", err)
	}
	newest := newestPane(t, host)
	host.setTitle(t, 0, first, "first-shell")
	host.setTitle(t, second, newest, "second-shell")
	host.shells[0].out <- []byte("this-is-the-first\r\n")
	host.shells[second].out <- []byte("this-is-the-second\r\n")

	// The row for the second one, as this window was told about it.
	var row remoteKey
	waitForBoth(t, host, client, "a row named for the second shell", func() bool {
		host.refreshPanel(panelNow)
		client.refreshPanel(panelNow)
		for _, r := range client.panel.Rows() {
			key, ok := r.Key.(remoteKey)
			if !ok || key.window != addr {
				continue
			}
			if open, there := client.openOver(key); there && open.Label == "second-shell" {
				row = key
				return true
			}
		}
		return false
	})

	if err := client.attachHere(row, nil); err != nil {
		t.Fatalf("attach: %v", err)
	}
	here := newestPane(t, client)
	waitForBoth(t, host, client, "whichever shell it attached to", func() bool {
		return strings.Contains(paneText(here), "this-is-the-")
	})
	if text := paneText(here); !strings.Contains(text, "this-is-the-second") {
		t.Errorf("it attached to the wrong shell: %q", text)
	}
}

// A pane showing a screen that is not its own size says what size that
// screen is, because nothing else explains what it is drawing.
func TestAPaneSaysWhenTheFarScreenIsAnotherSize(t *testing.T) {
	pane := ui.Size{Cols: 80, Rows: 24}
	if got := farNote(pane, 120, 40); got != "at 120x40" {
		t.Errorf("it said %q", got)
	}
	if got := farNote(pane, 80, 24); got != "" {
		t.Errorf("a screen of the same size said %q", got)
	}
	if got := farNote(pane, 0, 0); got != "" {
		t.Errorf("something with no screen said %q", got)
	}
	if !isFarNote("at 120x40") {
		t.Error("it does not recognise its own note")
	}
	if isFarNote("via bastion") || isOurNote("via bastion") {
		t.Error("it claimed somebody else's note")
	}
}

// And a pane being read from elsewhere says how many are reading it.
func TestAWatchedPaneSaysSo(t *testing.T) {
	if got := watchedNote(1); got != "watched by 1" {
		t.Errorf("one watcher gave %q", got)
	}
	if got := watchedNote(3); got != "watched by 3" {
		t.Errorf("three watchers gave %q", got)
	}
	if !isOurNote(watchedNote(2)) {
		t.Error("it does not recognise its own note")
	}
	if isOurNote("could not be closed") {
		t.Error("it claimed somebody else's note")
	}
}

// A row keeps its name when something before it on the list closes.
//
// This is what makes a row safe to click. Named by where it sat in a
// list built afresh every frame, a row would quietly become a different
// program the moment anything above it went, and the user would be
// typing into a shell they never chose.
func TestARowKeepsItsNameWhenSomethingElseCloses(t *testing.T) {
	host, client, addr := twoWindows(t)
	first := onlyPaneOn(t, host)

	second := len(host.shells)
	if err := host.openTab(); err != nil {
		t.Fatalf("a second shell there: %v", err)
	}
	newest := newestPane(t, host)
	host.setTitle(t, second, newest, "the-one-i-want")
	host.shells[second].out <- []byte("this-is-the-second\r\n")

	var row remoteKey
	waitForBoth(t, host, client, "a row for the second shell", func() bool {
		host.refreshPanel(panelNow)
		client.refreshPanel(panelNow)
		for _, r := range client.panel.Rows() {
			key, ok := r.Key.(remoteKey)
			if !ok || key.window != addr {
				continue
			}
			if open, there := client.openOver(key); there && open.Label == "the-one-i-want" {
				row = key
				return true
			}
		}
		return false
	})

	// The one before it goes, which moves everything after it up.
	if err := host.closePane(first); err != nil {
		t.Fatalf("close the first: %v", err)
	}
	waitForBoth(t, host, client, "the other window to say it has one less", func() bool {
		host.refreshPanel(panelNow)
		for _, open := range client.windows[addr].win.Opens() {
			if open.ID == row.id && open.Label == "the-one-i-want" {
				return true
			}
		}
		return false
	})

	// The row the user was shown is still the shell they were shown it
	// for.
	if err := client.attachHere(row, nil); err != nil {
		t.Fatalf("attach: %v", err)
	}
	here := newestPane(t, client)
	waitForBoth(t, host, client, "whichever shell it attached to", func() bool {
		return strings.Contains(paneText(here), "this-is-the-")
	})
	if text := paneText(here); !strings.Contains(text, "this-is-the-second") {
		t.Errorf("the row now holds a different shell: %q", text)
	}
}

// How big a screen is over there travels with the row, because watching
// does not resize it and the pane showing it has no other way to know.
func TestTheSizeOfAScreenOverThereTravels(t *testing.T) {
	host, client, addr := twoWindows(t)
	hostPane := onlyPaneOn(t, host)

	var open serve.Open
	waitForBoth(t, host, client, "a row with a size on it", func() bool {
		host.refreshPanel(panelNow)
		for _, o := range client.windows[addr].win.Opens() {
			if o.Cols > 0 && o.Rows > 0 {
				open = o
				return true
			}
		}
		return false
	})

	if want := hostPane.Size(); open.Cols != want.Cols || open.Rows != want.Rows {
		t.Errorf("it said %dx%d for a pane that is %dx%d",
			open.Cols, open.Rows, want.Cols, want.Rows)
	}
}

// What this window has open is told to whoever is working in it from
// elsewhere, whether or not the sidebar here happens to be open.
func TestWhatIsOpenIsToldWithTheSidebarShut(t *testing.T) {
	host, client, addr := twoWindows(t)
	hostPane := onlyPaneOn(t, host)
	host.setTitle(t, 0, hostPane, "named-while-shut")

	if host.dock == nil {
		t.Fatal("there is no sidebar to shut")
	}
	host.dock.Collapsed = true

	waitForBoth(t, host, client, "the name to reach the other window", func() bool {
		host.refreshPanel(panelNow)
		for _, o := range client.windows[addr].win.Opens() {
			if o.Label == "named-while-shut" {
				return true
			}
		}
		return false
	})
}

// A row keeps its key while what it is doing changes, so the user's
// place in the list is not taken from them by a connection going quiet.
func TestARemoteRowKeepsItsKeyWhileItWorks(t *testing.T) {
	busy := serve.Open{
		ID: "margit#1", Kind: "Terminal", Label: "vim",
		Note: "via bastion", State: "active", Cols: 80, Rows: 24,
	}
	quiet := busy
	quiet.Note, quiet.State, quiet.Cols, quiet.Rows = "", "settled", 120, 40

	if remoteKeyFor("margit", busy) != remoteKeyFor("margit", quiet) {
		t.Error("the row changed key when the program went quiet")
	}

	other := busy
	other.ID = "margit#2"
	if remoteKeyFor("margit", busy) == remoteKeyFor("margit", other) {
		t.Error("two different rows share a key")
	}
	if remoteKeyFor("margit", busy) == remoteKeyFor("web1", busy) {
		t.Error("the same row on two windows shares a key")
	}
}

// The size a watching pane reports is the size the far screen is now,
// not the size it was when the pane opened.
//
// The far end is redrawn at its own size whenever that changes, and a
// size remembered from the moment of attaching would go on being shown
// long after it stopped being true.
func TestTheFarScreenSizeIsAskedForAgain(t *testing.T) {
	host, client, addr := twoWindows(t)
	hostPane := onlyPaneOn(t, host)

	var row remoteKey
	waitForBoth(t, host, client, "a row for the shell over there", func() bool {
		host.refreshPanel(panelNow)
		client.refreshPanel(panelNow)
		for _, r := range client.panel.Rows() {
			if key, ok := r.Key.(remoteKey); ok && key.window == addr {
				row = key
				return true
			}
		}
		return false
	})
	if err := client.attachHere(row, nil); err != nil {
		t.Fatalf("attach: %v", err)
	}
	here := newestPane(t, client)

	// The screen over there is made a size this pane is not.
	was := hostPane.Size()
	hostPane.Layout(ui.Size{Cols: was.Cols - 7, Rows: was.Rows - 3})

	waitForBoth(t, host, client, "the row to say what size it is now", func() bool {
		host.refreshPanel(panelNow)
		client.refreshPanel(panelNow)
		e := client.panes[here]
		return e != nil && e.Note == farNote(here.Size(), was.Cols-7, was.Rows-3)
	})

	// And back again, so the note goes when there is nothing to say.
	hostPane.Layout(here.Size())
	waitForBoth(t, host, client, "the note to go when the sizes agree", func() bool {
		host.refreshPanel(panelNow)
		client.refreshPanel(panelNow)
		e := client.panes[here]
		return e != nil && e.Note == ""
	})
}

// A note somebody else put on a row is not written over.
//
// The one other note a pane can carry says its channel could not be let
// go of, and its row is the only place the user can read that.
func TestANoteAboutAFailureIsNotWrittenOver(t *testing.T) {
	host, client, addr := twoWindows(t)
	hostPane := onlyPaneOn(t, host)

	var row remoteKey
	waitForBoth(t, host, client, "a row for the shell over there", func() bool {
		host.refreshPanel(panelNow)
		client.refreshPanel(panelNow)
		for _, r := range client.panel.Rows() {
			if key, ok := r.Key.(remoteKey); ok && key.window == addr {
				row = key
				return true
			}
		}
		return false
	})
	if err := client.attachHere(row, nil); err != nil {
		t.Fatalf("attach: %v", err)
	}
	waitForBoth(t, host, client, "the host to know it is watched", func() bool {
		return hostPane.Watched() == 1
	})

	// Something else has written on the row of the pane being watched.
	host.refreshPanel(panelNow)
	e := host.panes[hostPane]
	if e == nil {
		t.Fatal("the pane over there has no row")
	}
	e.Note = "could not be closed"

	host.refreshPanel(panelNow)
	if e.Note != "could not be closed" {
		t.Errorf("the failure was written over with %q", e.Note)
	}
}

// The files of the window taken over are browsable from here.
//
// One connection carries the shells and the files both: taking over a
// window is meant to be the whole of that window, not its terminals
// with a second login for everything else.
func TestTheFilesOfTheWindowTakenOverAreBrowsable(t *testing.T) {
	host, client, addr := twoWindows(t)

	// Something on the serving machine's disk to find. The pane is
	// asked for a directory by name, so the test does not depend on
	// where either window happens to be running.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "over-there.txt"), []byte("hello"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	if err := client.openFilesOn(addr); err != nil {
		t.Fatalf("a pane on %s: %v", addr, err)
	}
	b := client.files
	if b == nil {
		t.Fatal("the window has no file manager")
	}
	panes := b.view.Panes()
	if len(panes) != 1 {
		t.Fatalf("the manager holds %d panes, want one", len(panes))
	}
	pane := panes[0]
	if got := pane.FS().Name(); got != addr {
		t.Errorf("the pane is on %q, not the window taken over", got)
	}
	// Reached over a connection, not by opening the same disk twice.
	// A filesystem this window could read on its own would spell paths
	// the way this machine does.
	if got := pane.FS().Sep(); got != '/' {
		t.Errorf("it separates paths with %q, which is not what crosses a wire", got)
	}

	// What is on that machine's disk, read over the same connection the
	// shells ride on.
	var entries []vfs.Entry
	within(t, "read the directory over there", func() error {
		var err error
		entries, err = pane.FS().ReadDir(overThere(dir))
		return err
	})
	found := false
	for _, e := range entries {
		if e.Name == "over-there.txt" {
			found = true
		}
	}
	if !found {
		t.Errorf("the file was not there: %v", entries)
	}

	// And it really is that connection. The window over there stops
	// serving, and the pane can no longer read a directory it has just
	// read -- which a filesystem of this machine's own could.
	if err := host.stopServing(); err != nil {
		t.Fatalf("stop serving: %v", err)
	}
	waitForBoth(t, host, client, "the pane to lose the machine it was reading", func() bool {
		_, err := pane.FS().ReadDir(overThere(dir))
		return err != nil
	})
}

// overThere spells a path of this machine the way a file session over
// the wire spells it: one root, forward slashes, and a drive letter
// under the root on Windows.
func overThere(dir string) string {
	at := filepath.ToSlash(dir)
	if !strings.HasPrefix(at, "/") {
		at = "/" + at
	}
	return at
}

// within runs something that talks to another window, failing the test
// rather than hanging it when the other end goes quiet.
func within(t *testing.T, what string, do func() error) {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- do() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("%s: %v", what, err)
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("%s: the other window never answered", what)
	}
}

// A file pane on a window taken over belongs to that window.
//
// Everything the window does with a pane starts by asking which machine
// it is on: which group its row goes under, what the commands on that
// row act on, and which way a copy is counted. A pane that said it was
// on this machine would put all of that on the wrong one.
func TestAFilePaneOnAWindowBelongsToThatWindow(t *testing.T) {
	_, client, addr := twoWindows(t)

	if err := client.openFilesOn(addr); err != nil {
		t.Fatalf("a pane on %s: %v", addr, err)
	}
	pane := client.files.view.Panes()[0]

	if got := client.hostOf(pane.FS()); got != addr {
		t.Errorf("the pane says it is on %q, not on the window", got)
	}

	// Its row goes under the window, not under this machine.
	client.refreshPanel(panelNow)
	found := ""
	for _, group := range client.registry.Groups(time.Now()) {
		for _, row := range group.Rows {
			if row.Kind == conns.Files {
				found = group.Host
			}
		}
	}
	if found != addr {
		t.Errorf("its row is under %q", found)
	}

	// And with that pane in front, the window knows which machine the
	// user is looking at.
	client.focus(pane)
	if got := client.currentHost(); got != addr {
		t.Errorf("the window thinks the user is looking at %q", got)
	}
}

// heldOpen is a file client that finishes closing only once the channel
// under it has gone, which is what a machine that has stopped answering
// looks like: the close sends an end of file and waits for a reply that
// is never coming.
type heldOpen struct {
	started chan struct{}
	freed   chan struct{}
	once    sync.Once
}

func (h *heldOpen) Close() error {
	h.once.Do(func() { close(h.started) })
	<-h.freed
	return nil
}

// theChannel frees the client under it when it is closed.
type theChannel struct {
	client *heldOpen

	mu    sync.Mutex
	times int
}

func (c *theChannel) Close() error {
	c.mu.Lock()
	c.times++
	first := c.times == 1
	c.mu.Unlock()
	if first {
		close(c.client.freed)
	}
	return nil
}

func (c *theChannel) closed() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.times
}

// Letting go of a window whose far end has stopped answering does not
// wait for ever.
//
// Closing an SFTP client sends an end of file and waits for the far end
// to close the channel. A machine that has stopped answering never
// will, and this runs on the goroutine that draws: without a bound, the
// one action left to a user with a wedged window would be the one that
// wedges it.
func TestLettingGoOfFilesOnAWindowThatStoppedAnsweringComesBack(t *testing.T) {
	client := &heldOpen{started: make(chan struct{}), freed: make(chan struct{})}
	ch := &theChannel{client: client}

	done := make(chan error, 1)
	go func() { done <- closeFilesOver(client, ch) }()
	<-client.started

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("letting go gave %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("it waited for a window that had stopped answering")
	}
	if got := ch.closed(); got == 0 {
		t.Error("the channel was left open, so nothing freed the client")
	}
}

// The plus on a window taken over offers the window's own list.
//
// Not only the list itself: the row has to ask for it. Asked for the
// machine list instead, it would offer a command, a tunnel and a proxy,
// none of which can work on a window.
func TestTheRowOfAWindowOpensTheWindowsMenu(t *testing.T) {
	_, client, addr := twoWindows(t)
	client.refreshPanel(panelNow)

	menu := clickPlus(t, client, addr)

	for _, want := range []string{"conn.terminal", "conn.files", "conn.disconnect"} {
		if !offers(menu, want) {
			t.Errorf("the menu does not offer %s: %v", want, menuCommands(menu))
		}
	}
	for _, not := range []string{"conn.command", "conn.tunnel", "conn.socks"} {
		if offers(menu, not) {
			t.Errorf("the menu offers %s, which cannot work on a window", not)
		}
	}
}

// silentMachine answers and then says nothing, which is what a firewall
// that accepts, a port forwarded to nothing, or another service on the
// port looks like from here.
func silentMachine(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	// Kept under a lock rather than sent down a channel: the goroutine
	// can be holding a connection at the moment the test ends, and a
	// channel closed under it panics.
	var (
		mu   sync.Mutex
		held []net.Conn
	)
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			held = append(held, c)
			mu.Unlock()
		}
	}()
	t.Cleanup(func() {
		_ = ln.Close()
		mu.Lock()
		defer mu.Unlock()
		for _, c := range held {
			_ = c.Close()
		}
	})
	return ln.Addr().String()
}

// Giving up on a window that is not answering lets it go, and lets the
// window try again.
//
// The pane stays with its account of what happened; what must not stay
// is the window thinking it is still taking that machine over, which is
// what answers a second attempt with "already taking over".
func TestGivingUpOnAWindowThatIsNotAnsweringLetsItGo(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	keyFile, _ := aKeyFile(t)
	addr := silentMachine(t)

	if err := a.takeOver(addr, keyFile); err != nil {
		t.Fatalf("take over: %v", err)
	}
	pane := newestPane(t, a)
	waitFor(t, a, "the pane to say it is connecting", func() bool {
		return strings.Contains(paneText(pane), stepConnect)
	})

	// What the user does: close the pane, which is what gives up.
	if err := a.closePane(pane); err != nil {
		t.Fatalf("close the pane: %v", err)
	}
	waitFor(t, a, "it to let go of the machine", func() bool {
		return a.opening[addr] == nil
	})

	// And it will try again rather than saying it already is.
	if err := a.takeOver(addr, keyFile); err != nil {
		t.Fatalf("it would not try again: %v", err)
	}
	if err := a.dropWindow(addr); err != nil {
		t.Fatalf("drop: %v", err)
	}
	waitFor(t, a, "the second attempt to go", func() bool { return a.opening[addr] == nil })
}

// A window that answers and then says nothing is given up on by itself,
// and the pane says so.
func TestAWindowThatSaysNothingIsGivenUpOnByItself(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	keyFile, _ := aKeyFile(t)
	addr := silentMachine(t)

	if err := a.takeOver(addr, keyFile); err != nil {
		t.Fatalf("take over: %v", err)
	}
	pane := newestPane(t, a)

	waitFor(t, a, "the pane to say it gave up", func() bool {
		return strings.Contains(paneText(pane), "The connection was not made")
	})
	if a.opening[addr] != nil {
		t.Error("it still thinks it is taking that machine over")
	}
}

// The pane says what is being done, so a connection that stalls says
// where it stalled.
func TestThePaneSaysWhatItIsDoing(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	keyFile, _ := aKeyFile(t)
	addr := silentMachine(t)

	if err := a.takeOver(addr, keyFile); err != nil {
		t.Fatalf("take over: %v", err)
	}
	pane := newestPane(t, a)

	waitFor(t, a, "the pane to say what it is doing", func() bool {
		got := paneText(pane)
		return strings.Contains(got, "taking over "+addr) &&
			strings.Contains(got, stepKeys) &&
			strings.Contains(got, stepConnect)
	})
}

// rowFor is the row for a window being taken over, or nil when there is
// none.
func rowFor(a *testApp, addr string) *conns.Entry {
	for _, group := range a.registry.Groups(time.Now()) {
		if group.Host != addr {
			continue
		}
		for _, row := range group.Rows {
			if row.Label == "taking over" {
				return row.Entry
			}
		}
	}
	return nil
}

// The same for a window taken over: the pane that says why stays.
func TestThePaneThatSaysWhyAWindowFailedStays(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	keyFile, _ := aKeyFile(t)
	before := len(a.panes)

	// A port nothing is listening on.
	if err := a.takeOver("127.0.0.1:1", keyFile); err != nil {
		t.Fatalf("take over: %v", err)
	}
	pane := newestPane(t, a)
	waitFor(t, a, "the pane to say why it could not", func() bool {
		return strings.Contains(paneText(pane), "The connection was not made")
	})

	for i := 0; i < 10; i++ {
		a.pump.run()
		a.reapExited()
	}

	if len(a.panes) != before+1 {
		t.Fatalf("%d panes after tidying up, want the pane that says why to still be there",
			len(a.panes))
	}
	if got := paneText(pane); !strings.Contains(got, "The connection was not made") {
		t.Errorf("the pane no longer says why: %q", got)
	}
}

// The dialog about a window already being taken over really opens.
//
// It is reached from the Take over button, and a dialog opened from
// inside a button is torn down with the dialog the button belongs to.
// The user pressed the button and nothing happened at all.
func TestAskingAboutTheWindowOnItsWayOpensFromTheButton(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	keyFile, _ := aKeyFile(t)

	// A port nothing answers on, so the first is still on its way.
	if err := a.takeOver("127.0.0.1:1", keyFile); err != nil {
		t.Fatalf("first: %v", err)
	}

	// The second one the way the user does it: through the form.
	if err := a.openTakeOver(); err != nil {
		t.Fatalf("open the form: %v", err)
	}
	f := openDialog(t, a)
	f.Fields()[0].SetText("127.0.0.1:1")
	f.Fields()[1].SetText(keyFile)
	pressButton(t, a, f, "Take over")

	ask := waitForDialog(t, a, "Already connecting to 127.0.0.1:1")
	pressButton(t, a, ask, "Leave it")
	if a.opening["127.0.0.1:1"] == nil {
		t.Fatal("the first attempt was let go of")
	}
}

// Waiting for a window being taken over opens a terminal on it once it
// lands, rather than taking it over a second time.
//
// Taking it over again only reports that it has been taken over
// already, so the successful outcome of waiting was an error dialog.
func TestWaitingForAWindowOpensATerminalOnIt(t *testing.T) {
	host, client, addr := twoWindows(t)
	_ = host

	was := len(client.panes)
	client.workOnWindow(addr, "")
	waitFor(t, client, "a terminal on the window", func() bool {
		return len(client.panes) > was
	})
	if f, ok := client.root.Modal().(*ui.Form); ok {
		t.Fatalf("it reported a failure instead: %v", f.Title)
	}
}

// Taking over a window leaves an agent alone once it is known not to
// answer, the same as connecting to a machine does.
//
// It has its own way of reading the agent, and it went on paying the
// wait every time while every other connection had stopped.
func TestTakingOverAWindowLeavesASilentAgentAlone(t *testing.T) {
	ring := remote.NewRing()
	ring.AgentGaveUp(errors.New("it had 10s to say what keys it holds and did not"))

	asked := false
	_, _, err := keysFor(t.Context(), "", ring, nil,
		func() ([]ssh.Signer, io.Closer, error) {
			asked = true
			return nil, nil, nil
		}, nil)
	if asked {
		t.Fatal("it asked an agent that is known not to answer")
	}
	if err == nil || !strings.Contains(err.Error(), "did not") {
		t.Fatalf("keysFor = %v, want it to say why there are no keys", err)
	}
}

// An agent that is not running is not held against it there either.
func TestTakingOverAWindowDoesNotHoldAMissingAgentAgainstIt(t *testing.T) {
	ring := remote.NewRing()
	_, _, err := keysFor(t.Context(), "", ring, nil,
		func() ([]ssh.Signer, io.Closer, error) {
			return nil, nil, errors.New("remote: no SSH agent: open the pipe: not found")
		}, nil)
	if err == nil {
		t.Fatal("it found keys where there are none")
	}
	if why := ring.AgentTrouble(); why != nil {
		t.Fatalf("it will not ask the agent again because %v", why)
	}
}

// homeWith puts a private key in the usual place and points the home
// directory at it, so a test sees what a user's ~/.ssh looks like.
func homeWith(t *testing.T, name, from string) {
	t.Helper()
	home := t.TempDir()
	dir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("make .ssh: %v", err)
	}
	if from != "" {
		b, err := os.ReadFile(from)
		if err != nil {
			t.Fatalf("read the key: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), b, 0o600); err != nil {
			t.Fatalf("write the key: %v", err)
		}
	}
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", home)
		return
	}
	t.Setenv("HOME", home)
}

// Taking over a window offers the key files in the usual places.
//
// It offered only what was already unlocked and what the agent held.
// A user with one key in ~/.ssh and an agent that says nothing was told
// there were no keys to offer, when the key that works was sitting
// there.
func TestTakingOverAWindowOffersTheUsualKeys(t *testing.T) {
	homeWith(t, "id_ed25519", sshtest.WriteKey(t))

	keys, closer, err := keysFor(t.Context(), "", remote.NewRing(), nil,
		func() ([]ssh.Signer, io.Closer, error) {
			return nil, nil, errors.New("remote: no SSH agent here")
		}, nil)
	if closer != nil {
		_ = closer.Close()
	}
	if err != nil {
		t.Fatalf("keysFor: %v", err)
	}
	if len(keys) != 1 {
		t.Fatalf("it found %d keys, want the one in ~/.ssh", len(keys))
	}
}

// A key in the usual place that needs a passphrase is asked about, once
// there is nothing else left to offer.
func TestTakingOverAWindowAsksForALockedUsualKey(t *testing.T) {
	const passphrase = "open sesame"
	homeWith(t, "id_ed25519", sshtest.WriteEncryptedKey(t, passphrase))

	asked := 0
	ask := &countingAsk{pass: passphrase, asked: &asked}
	keys, closer, err := keysFor(t.Context(), "", remote.NewRing(), ask,
		func() ([]ssh.Signer, io.Closer, error) {
			return nil, nil, errors.New("remote: no SSH agent here")
		}, nil)
	if closer != nil {
		_ = closer.Close()
	}
	if err != nil {
		t.Fatalf("keysFor: %v", err)
	}
	if len(keys) != 1 {
		t.Fatalf("it found %d keys, want the one in ~/.ssh", len(keys))
	}
	if asked != 1 {
		t.Fatalf("it asked for a passphrase %d times, want once", asked)
	}
}

// With nothing anywhere, it says where it looked.
func TestTakingOverAWindowWithNoKeysSaysWhereToPutOne(t *testing.T) {
	homeWith(t, "", "")

	_, _, err := keysFor(t.Context(), "", remote.NewRing(), nil,
		func() ([]ssh.Signer, io.Closer, error) { return nil, nil, nil }, nil)
	if err == nil {
		t.Fatal("it found keys where there are none")
	}
	for _, want := range []string{"name a key file", "~/.ssh", "SSH agent"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the failure does not mention %q: %v", want, err)
		}
	}
}

// countingAsk answers a passphrase and counts how often it was asked.
type countingAsk struct {
	remote.Ask
	pass  string
	asked *int
}

func (c *countingAsk) Passphrase(context.Context, string) (string, error) {
	*c.asked++
	return c.pass, nil
}

// Finding a key says what it looked at, so a window that could not be
// reached says why rather than only that it could not.
func TestFindingAKeySaysWhatItLookedAt(t *testing.T) {
	homeWith(t, "id_ed25519", sshtest.WriteKey(t))
	ring := remote.NewRing()
	ring.AgentGaveUp(errors.New("it had 5s to say what keys it holds and did not"))

	var said []string
	_, closer, err := keysFor(t.Context(), "", ring, nil,
		func() ([]ssh.Signer, io.Closer, error) { return nil, nil, nil },
		func(what string) { said = append(said, what) })
	if closer != nil {
		_ = closer.Close()
	}
	if err != nil {
		t.Fatalf("keysFor: %v", err)
	}

	account := strings.Join(said, "\n")
	for _, want := range []string{
		"leaving the SSH agent alone",
		"Forget unlocked keys",
		"1 private keys in the usual places need no passphrase",
	} {
		if !strings.Contains(account, want) {
			t.Errorf("the account does not say %q:\n%s", want, account)
		}
	}
}

// A row on the other window with no screen cannot be watched, and
// asking leaves nothing behind.
//
// Asking anyway opened a pane that showed the refusal, and the pane was
// a row of its own. Every attempt left another one, so a user clicking
// the row that says somebody is working in that window could make rows
// for ever.
func TestARowWithNoScreenCannotBeWatched(t *testing.T) {
	host, client, addr := twoWindows(t)

	// The row the serving window has for the client working in it. It
	// is on its panel and has no screen behind it, so this window does
	// not list it -- but a key naming it can still arrive, from a row
	// listed a moment before it closed.
	var serving remoteKey
	waitForBoth(t, host, client, "the serving row to reach the client", func() bool {
		host.refreshPanel(time.Now())
		for _, open := range client.windows[addr].win.Opens() {
			if strings.Contains(open.Label, "serving") {
				serving = remoteKeyFor(addr, open)
				return !open.HasScreen()
			}
		}
		return false
	})

	// It is not offered.
	for _, row := range client.remoteRows(addr) {
		if row.Key == serving {
			t.Fatal("a row with no screen was offered to watch")
		}
	}

	// And asking for it anyway opens nothing.
	panes := len(client.panes)
	err := client.attachHere(serving, nil)
	if err == nil {
		t.Fatal("it opened a pane on something with no screen")
	}
	if !strings.Contains(err.Error(), "no screen") {
		t.Errorf("it refused with %v, want it to say there is no screen", err)
	}
	if len(client.panes) != panes {
		t.Fatalf("%d panes, want the %d there were", len(client.panes), panes)
	}
	// And again, because the bug was that each attempt left a row.
	if err := client.attachHere(serving, nil); err == nil {
		t.Fatal("the second attempt opened a pane")
	}
	if len(client.panes) != panes {
		t.Fatalf("%d panes after two attempts, want the %d there were", len(client.panes), panes)
	}
}
