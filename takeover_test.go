package main

import (
	"context"
	"errors"
	"fmt"
	"image/color"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"

	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/serve"
	"github.com/marrasen/gridterm/ui/term"
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
	waitFor(t, client, "a pane on the other window", func() bool {
		return len(client.panes) > panes
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

// A window that is not serving is not taken over, and the failure is
// shown rather than leaving a row that never fills.
func TestTakingOverAWindowThatIsNotThereFails(t *testing.T) {
	client := newTestApp(t, 90, 30)
	withDialogs(t, client)
	withPanel(t, client)
	keyFile, _ := aKeyFile(t)

	// A port nothing is listening on.
	if err := client.takeOver("127.0.0.1:1", keyFile); err != nil {
		t.Fatalf("take over: %v", err)
	}

	f := openDialog(t, client)
	if text := strings.Join(f.Lines, "\n"); !strings.Contains(text, "Could not take over") &&
		!strings.Contains(f.Title, "Could not take over") {
		t.Errorf("the failure was not shown: %q %v", f.Title, f.Lines)
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
		})

	if err == nil {
		t.Fatal("it found keys where there are none")
	}
	if !strings.Contains(err.Error(), "the agent said no") {
		t.Errorf("it said %v, without saying why there were none", err)
	}
}

// Taking over the same window twice at once is refused.
//
// The second would write over the first's way of being closed, leaving
// a connection nothing could reach and a serving window showing a
// client that is not there.
func TestTakingOverTheSameWindowTwiceIsRefused(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	keyFile, _ := aKeyFile(t)

	// A port nothing answers on, so the first is still on its way.
	if err := a.takeOver("127.0.0.1:1", keyFile); err != nil {
		t.Fatalf("first: %v", err)
	}
	err := a.takeOver("127.0.0.1:1", keyFile)

	if err == nil {
		t.Fatal("it started taking over the same window twice")
	}
	if !strings.Contains(err.Error(), "already taking over") {
		t.Errorf("it refused with %v", err)
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

// A window taken over is not offered a file browser, because nothing
// can browse one yet. A command that cannot work is worse than none.
func TestAWindowIsNotOfferedAFileBrowser(t *testing.T) {
	_, client, addr := twoWindows(t)

	for _, title := range commandTitles(client) {
		if strings.Contains(title, "Browse files on "+addr) {
			t.Errorf("it offers %q, which cannot work", title)
		}
	}
	var found bool
	for _, title := range commandTitles(client) {
		if strings.Contains(title, "Open a terminal on "+addr) {
			found = true
		}
	}
	if !found {
		t.Errorf("it offers no way to open a terminal on %s", addr)
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
	host.shells[0].out <- []byte("already-here\\r\\n")
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
	if err := client.attachHere(row.window, row.id, row.label, nil); err != nil {
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

	// And typing here reaches the shell there.
	here.Send([]byte("typed-from-here\\r"))
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
	host.shells[0].out <- []byte("still-running\\r\\n")
	waitForBoth(t, host, client, "the shell there to carry on", func() bool {
		return strings.Contains(paneText(hostPane), "still-running")
	})
}

// Attaching to something that is not there says so.
func TestAttachingToNothingSaysSo(t *testing.T) {
	host, client, addr := twoWindows(t)

	// The channel opens before the other end has decided, so the reason
	// arrives in the pane rather than as an error here. That is where
	// the user would see it.
	if err := client.attachHere(addr, "Local#99", "gone", nil); err != nil {
		t.Fatalf("attach: %v", err)
	}
	pane := newestPane(t, client)

	waitForBoth(t, host, client, "the reason in the pane", func() bool {
		return strings.Contains(paneText(pane), "nothing called")
	})
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
