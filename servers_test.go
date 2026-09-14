package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

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
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		a.pump.run()
		if len(a.panes) == n {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("the app has %d panes, want %d", len(a.panes), n)
}

// waitForDialog runs the pump until a dialog with the given title is on
// the stack.
func waitForDialog(t *testing.T, a *testApp, title string) *ui.Form {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
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
	// The waiting dialog holds the place while the connection is made.
	waitForDialog(t, a, "Connecting")
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

	f := waitForDialog(t, a, "Could not connect to "+cfg.Host)
	if len(f.Lines) == 0 || strings.TrimSpace(strings.Join(f.Lines, "")) == "" {
		t.Fatal("the failure was reported with no reason in it")
	}
	if len(a.panes) != 1 {
		t.Errorf("%d panes after a failed connection, want the one that was there", len(a.panes))
	}
}

// Cancelling gives up on the connection rather than leaving a goroutine
// dialling a machine nobody is waiting for.
func TestConnectCancelClosesTheWaitingDialog(t *testing.T) {
	s := sshtest.New(t)
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)

	cfg := serverConfig(t, s)
	cfg.Port = 1 // nothing answers, so the dial is still running
	a.connect(cfg)

	f := waitForDialog(t, a, "Connecting")
	pressButton(t, a, f, "Cancel")

	// The dial ends and reports nothing: the user knows, they cancelled.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		a.pump.run()
		if a.root.Modal() == nil {
			if len(a.panes) != 1 {
				t.Fatalf("%d panes after cancelling, want the one that was there", len(a.panes))
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("a dialog was left open after cancelling: %T", a.root.Modal())
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
