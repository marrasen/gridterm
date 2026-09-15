package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/input"
	"github.com/marrasen/gridterm/internal/sshtest"
	"github.com/marrasen/gridterm/remote"
	"github.com/marrasen/gridterm/ui"
)

// serverConfig points at the in-process SSH server, with host key
// checking pinned and nothing that would reach the developer's own keys.
func serverConfig(t *testing.T, s *sshtest.Server) remote.Config {
	t.Helper()
	host, port := s.Host()
	return remote.Config{
		Host: host, Port: port, User: "tester",
		HostKeyCallback: ssh.FixedHostKey(s.HostKey()),
		NoAgent:         true,
		Identities:      []string{sshtest.WriteKey(t)},
	}
}

// waitForPanes runs the pump until the app has n panes.
func waitForPanes(t *testing.T, a *testApp, n int) {
	t.Helper()
	deadline := time.Now().Add(waitBudget)
	for time.Now().Before(deadline) {
		a.pump.run()
		// And nothing still connecting. A connection opens its pane as
		// soon as it starts, so the pane is there before the machine
		// is, and a test that counted panes alone would go on before
		// there was anything to connect to.
		if len(a.panes) == n && a.connecting == 0 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("the app has %d panes and %d connections still opening, want %d panes",
		len(a.panes), a.connecting, n)
}

// waitForDialog runs the pump until a dialog with the given title is on
// the stack.
func waitForDialog(t *testing.T, a *testApp, title string) *ui.Form {
	t.Helper()
	deadline := time.Now().Add(waitBudget)
	for time.Now().Before(deadline) {
		a.pump.run()
		if f, ok := a.root.Modal().(*ui.Form); ok && f.Title == title {
			return f
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("no %q dialog opened", title)
	return nil
}

// A target that is not a target has to be said so where it was typed,
// with what was typed still there to correct.
func TestOpenServerRejectsABadTarget(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)

	if err := a.openServer(); err != nil {
		t.Fatalf("openServer: %v", err)
	}
	f := waitForDialog(t, a, "Connect to a server")
	for _, r := range "host:nope" {
		a.root.HandleKey(input1(r))
	}
	pressButton(t, a, f, "Connect")

	if a.root.Modal() != f {
		t.Fatal("the dialog closed on a target it could not parse")
	}
	if f.Error() == nil {
		t.Fatal("nothing said why the target was refused")
	}
	if got := f.Fields()[0].Text(); got != "host:nope" {
		t.Errorf("the dialog lost what was typed: %q", got)
	}
}

// The whole path: ask for a machine, connect to it in the background,
// and put a terminal on it when it arrives.
func TestConnectOpensATab(t *testing.T) {
	s := sshtest.New(t)
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)

	a.connect(serverConfig(t, s))
	// A row holds the place while the connection is made.
	waitForConnecting(t, a)
	waitForPanes(t, a, 2)

	if a.root.Modal() != nil {
		t.Errorf("a dialog was left open after the connection arrived: %T", a.root.Modal())
	}
	checkTree(t, a)
	if n := s.Conns(); n != 1 {
		t.Fatalf("the server saw %d connections, want 1", n)
	}
}

// A connection that fails says why, rather than leaving the user looking
// at a dialog that went away for no stated reason.
func TestConnectShowsWhyItFailed(t *testing.T) {
	s := sshtest.New(t)
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)

	cfg := serverConfig(t, s)
	// A key the server would accept, pointed at a port nothing answers.
	cfg.Port = 1
	a.connect(cfg)

	said := waitForFailure(t, a, cfg.Target())
	if strings.TrimSpace(said) == "" {
		t.Fatal("the failure was reported with no reason in it")
	}
	// The pane that was watching stays, holding the reason: a failure
	// the user can still read is the whole point of watching in a pane.
	// Closing it is what takes it away.
	if len(a.panes) != 2 {
		t.Errorf("%d panes after a failed connection, want the one that was there"+
			" and the one that says why", len(a.panes))
	}
	if a.opening[cfg.Target()] != nil {
		t.Error("the machine is still marked as being connected to")
	}
	for pane, e := range a.panes {
		if e.Host != cfg.Target() {
			continue
		}
		if err := a.closePane(pane); err != nil {
			t.Fatalf("close the pane: %v", err)
		}
	}
	for _, group := range a.registry.Groups(time.Now()) {
		for _, row := range group.Rows {
			if row.Host == cfg.Target() {
				t.Fatalf("closing it left %q on the panel", row.Label)
			}
		}
	}
}

// Cancelling gives up on a connection that is genuinely still being
// made, and says nothing afterwards: the user knows, they cancelled.
//
// The server here accepts and then never speaks, so the dial really is
// still running when Cancel is pressed. A port nothing listens on is
// refused in well under a millisecond, which would let this pass without
// the cancellation doing anything at all.
func TestConnectCancelStopsADialThatIsStillRunning(t *testing.T) {
	host, port := sshtest.Deaf(t)
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)

	cfg := serverConfig(t, sshtest.New(t))
	cfg.Host, cfg.Port = host, port
	a.connect(cfg)

	// A row on the panel says it is on its way, and it can be cancelled
	// from there.
	waiting := waitForConnecting(t, a)
	if a.connecting != 1 {
		t.Fatalf("%d connections are being made, want 1", a.connecting)
	}
	if waiting.Close == nil {
		t.Fatal("the row cannot be cancelled")
	}
	if err := waiting.Close(); err != nil {
		t.Fatalf("cancel: %v", err)
	}

	// The dial has to end, and end quietly.
	deadline := time.Now().Add(waitBudget)
	for time.Now().Before(deadline) {
		a.pump.run()
		if a.connecting == 0 {
			if m := a.root.Modal(); m != nil {
				t.Fatalf("a dialog was left open after cancelling: %T", m)
			}
			if len(a.panes) != 1 {
				t.Fatalf("%d panes after cancelling, want the one that was there", len(a.panes))
			}
			// And the row it was waiting in has gone.
			for _, e := range a.registry.Groups(time.Now()) {
				for _, row := range e.Rows {
					if row.Entry == waiting {
						t.Fatal("the row was left on the panel")
					}
				}
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("the dial was still running long after it was cancelled")
}

// Cancelling is the user's own decision, so it is not reported back to
// them as a failure.
func TestReportErrorSaysNothingAboutACancellation(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)

	a.reportError("Could not connect", context.Canceled)
	if m := a.root.Modal(); m != nil {
		t.Fatalf("cancelling opened a dialog: %T", m)
	}
	a.reportError("Could not connect", errors.New("no route to host"))
	if a.root.Modal() == nil {
		t.Fatal("a real failure opened no dialog")
	}
}

// Several connections can be on their way at once, each with its own row
// on the panel. A dialog each would stack, and closing one takes
// everything above it.
func TestConnectRunsSeveralAtOnce(t *testing.T) {
	host, port := sshtest.Deaf(t)
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)

	for _, name := range []string{"one", "two", "three"} {
		cfg := serverConfig(t, sshtest.New(t))
		cfg.Host, cfg.Port = host, port
		a.connectAs(name, cfg)
	}
	if a.connecting != 3 {
		t.Fatalf("%d connections are being made, want 3", a.connecting)
	}

	// One row each, under a heading each, and no dialog anywhere.
	rows := panelText(a, time.Now())
	for _, name := range []string{"one", "two", "three"} {
		var found bool
		for _, row := range rows {
			if row == name {
				found = true
			}
		}
		if !found {
			t.Errorf("there is no row for %q: %v", name, rows)
		}
	}
	if m := a.root.Modal(); m != nil {
		t.Fatalf("a dialog opened for a connection: %T", m)
	}

	// Each has a pane of its own, saying what it is doing.
	var opening []*conns.Entry
	for _, group := range a.registry.Groups(time.Now()) {
		for _, row := range group.Rows {
			if row.Label == "connecting" {
				opening = append(opening, row.Entry)
			}
		}
	}
	if len(opening) != 3 {
		t.Fatalf("%d panes say a connection is opening, want 3", len(opening))
	}

	// Cancelling one leaves the others alone. Settled first, so a
	// cancel-all is not missed while the count is passing through 2.
	if err := opening[0].Close(); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	deadline := time.Now().Add(waitBudget)
	for time.Now().Before(deadline) && a.connecting > 2 {
		a.pump.run()
		time.Sleep(time.Millisecond)
	}
	// Long enough that a cancel-all would have landed too.
	for i := 0; i < 20; i++ {
		a.pump.run()
		time.Sleep(5 * time.Millisecond)
	}
	if a.connecting != 2 {
		t.Fatalf("%d connections are being made after cancelling one, want 2", a.connecting)
	}
	for _, e := range opening[1:] {
		var found bool
		for _, group := range a.registry.Groups(time.Now()) {
			for _, row := range group.Rows {
				if row.Entry == e {
					found = true
				}
			}
		}
		if !found {
			t.Fatal("cancelling one connection took another off the panel")
		}
	}
}

// waitForConnecting runs the pump until a connection is on its way and
// returns the row standing for it.
func waitForConnecting(t *testing.T, a *testApp) *conns.Entry {
	t.Helper()
	deadline := time.Now().Add(waitBudget)
	for time.Now().Before(deadline) {
		a.pump.run()
		for _, group := range a.registry.Groups(time.Now()) {
			for _, row := range group.Rows {
				if row.Label == "connecting" {
					return row.Entry
				}
			}
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("nothing is being connected to")
	return nil
}

// Locking forgets every key, so the next connection asks again.
func TestLockKeysEmptiesTheRing(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)

	path := sshtest.WriteEncryptedKey(t, "let me in")
	if _, err := a.keys.Unlock(a.ctx, path, &fixedAsk{passphrase: "let me in"}); err != nil {
		t.Fatalf("Unlock: %v", err)
	}
	if !a.keys.Has(path) {
		t.Fatal("the key was not kept")
	}
	if err := a.lockKeys(); err != nil {
		t.Fatalf("lockKeys: %v", err)
	}
	if a.keys.Has(path) {
		t.Fatal("a key survived locking the ring")
	}
}

func TestWrapLines(t *testing.T) {
	cases := []struct {
		in    string
		width int
		want  []string
	}{
		{"short", 20, []string{"short"}},
		{"one two three four", 8, []string{"one two", "three", "four"}},
		{"", 10, nil},
		// A word longer than the box is cut rather than pushing the
		// dialog wider than the window.
		{"aaaaaaaaaa", 4, []string{"aaaa", "aaaa", "aa"}},
	}
	for _, tc := range cases {
		got := wrapLines(tc.in, tc.width)
		if len(got) != len(tc.want) {
			t.Fatalf("wrapLines(%q, %d) = %q, want %q", tc.in, tc.width, got, tc.want)
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Fatalf("wrapLines(%q, %d) = %q, want %q", tc.in, tc.width, got, tc.want)
			}
		}
		for _, line := range got {
			if len(line) > tc.width {
				t.Fatalf("wrapLines(%q, %d) produced a line %d wide", tc.in, tc.width, len(line))
			}
		}
	}
}

// input1 is one typed character.
func input1(r rune) input.Event {
	return input.Event{Kind: input.Text, Rune: r, NormalText: true}
}

// fixedAsk answers with what it was built with, for a test that only
// needs the ring to open a key.
type fixedAsk struct{ passphrase string }

func (f *fixedAsk) Passphrase(context.Context, string) (string, error) {
	return f.passphrase, nil
}
func (f *fixedAsk) Password(context.Context, string, string) (string, error) { return "", nil }
func (f *fixedAsk) Question(context.Context, remote.Question) ([]string, error) {
	return nil, nil
}
func (f *fixedAsk) TrustHostKey(context.Context, remote.HostKey) (bool, error) {
	return false, nil
}
func (f *fixedAsk) Notice(context.Context, remote.Notice) {}

// The path a user actually takes: the command, the form, the keys.
//
// This is what the direct call to connect misses. The Connect button
// used to open the waiting dialog from inside the form's own Do, and the
// form then closed -- taking the waiting dialog with it and cancelling
// the connection it had just started. Nothing was reported.
func TestOpenServerConnectsThroughTheForm(t *testing.T) {
	s := sshtest.New(t)
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	host, port := s.Host()

	// The real connection has to go through serverConfig's pinned host
	// key, so the form only supplies the target and the rest is set on
	// the way past.
	a.prepare = func(cfg remote.Config) remote.Config {
		base := serverConfig(t, s)
		base.User = cfg.User
		return base
	}

	if err := a.openServer(); err != nil {
		t.Fatalf("openServer: %v", err)
	}
	f := waitForDialog(t, a, "Connect to a server")
	for _, r := range fmt.Sprintf("tester@%s:%d", host, port) {
		a.root.HandleKey(input1(r))
	}
	pressButton(t, a, f, "Connect")

	waitForPanes(t, a, 2)
	if a.root.Modal() != nil {
		t.Errorf("a dialog was left open: %T", a.root.Modal())
	}
	checkTree(t, a)
}

// waitForFailure waits for the pane watching a connection to say it
// could not be made, and gives back what it says.
//
// A failure is written into the pane that was watching for it rather
// than shown in a dialog: the pane stays, so the reason can still be
// read afterwards.
func waitForFailure(t *testing.T, a *testApp, host string) string {
	t.Helper()
	deadline := time.Now().Add(waitBudget)
	for time.Now().Before(deadline) {
		a.pump.run()
		for pane, e := range a.panes {
			if e.Host != host {
				continue
			}
			if got := paneText(pane); strings.Contains(got, "The connection was not made") {
				return got
			}
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("no pane says the connection to %s failed: %v", host, panelText(a, time.Now()))
	return ""
}

// Connecting to a server opens a pane and says what it is doing in it,
// and the same pane then carries the shell.
//
// This is what makes a connection that goes wrong reportable: without
// it there is a row that says "opening" and nothing else, whatever
// happens.
func TestConnectingOpensAPaneAndSaysWhatItIsDoing(t *testing.T) {
	s := sshtest.New(t)
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	before := len(a.panes)

	cfg := serverConfig(t, s)
	a.connect(cfg)

	pane := newestPane(t, a)
	if len(a.panes) != before+1 {
		t.Fatalf("%d panes, want one more than the %d there were", len(a.panes), before)
	}
	// It says what it is doing before there is anything to connect to.
	waitFor(t, a, "the pane to say what it is doing", func() bool {
		return strings.Contains(paneText(pane), "connecting to "+cfg.Target())
	})

	// And the same pane carries the shell, with the account of how it
	// was reached still above it.
	waitFor(t, a, "the connection to be made", func() bool {
		return a.machines[cfg.Target()] != nil
	})
	waitFor(t, a, "the pane to say it connected", func() bool {
		return strings.Contains(paneText(pane), "connected to "+cfg.Target())
	})
	if a.paneOn[pane] == nil {
		t.Error("the pane is not on the machine it connected to")
	}
	if len(a.panes) != before+1 {
		t.Errorf("%d panes, so a second one opened for the shell", len(a.panes))
	}
}

// What a server says on the way in is written into the pane, whole.
//
// A server that signs people in through a browser sends the link this
// way. In a dialog it is cut off at the edge, cannot be selected, and
// is gone the moment the dialog is dismissed; in the pane it is there
// to read and to copy.
func TestWhatAServerSaysGoesIntoThePane(t *testing.T) {
	const link = "https://login.tailscale.com/a/0123456789abcdef0123456789abcdef"
	s := sshtest.New(t)
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)

	s.SayOnTheWayIn("To authenticate, visit:\r\n\r\n" + link + "\r\n")

	cfg := serverConfig(t, s)
	a.connect(cfg)
	pane := newestPane(t, a)

	// Joined back up: a link longer than the pane is wide is wrapped
	// across two rows, the way any long line is. What matters is that
	// all of it is there.
	waitFor(t, a, "the link to reach the pane", func() bool {
		return strings.Contains(unwrapped(paneText(pane)), link)
	})
	if got := paneText(pane); !strings.Contains(got, "says:") {
		t.Errorf("it does not say who said it: %q", got)
	}
}

// unwrapped is what a pane shows with the row breaks taken out, for
// reading something longer than the pane is wide.
func unwrapped(text string) string { return strings.ReplaceAll(text, "\n", "") }

// A pane that says why a connection failed is not reaped away.
//
// The window takes away a terminal whose shell has gone, because there
// is nothing left to read. A connection that failed is the opposite: the
// pane is the only account of what happened, and it has to still be
// there when the user goes looking.
func TestThePaneThatSaysWhyIsNotReapedAway(t *testing.T) {
	s := sshtest.New(t)
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	before := len(a.panes)

	cfg := serverConfig(t, s)
	cfg.Port = 1
	a.connect(cfg)

	said := waitForFailure(t, a, cfg.Target())
	if !strings.Contains(said, "The connection was not made") {
		t.Fatalf("the pane says %q", said)
	}

	// The window tidies up after shells that have gone, every frame.
	for i := 0; i < 10; i++ {
		a.pump.run()
		a.reapExited()
	}

	if len(a.panes) != before+1 {
		t.Fatalf("%d panes after tidying up, want the pane that says why to still be there",
			len(a.panes))
	}
	for pane, e := range a.panes {
		if e.Host != cfg.Target() {
			continue
		}
		if got := paneText(pane); !strings.Contains(got, "The connection was not made") {
			t.Errorf("the pane no longer says why: %q", got)
		}
		return
	}
	t.Error("there is no pane for the machine that could not be reached")
}
