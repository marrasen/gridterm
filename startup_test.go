package main

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/internal/sshtest"
	"github.com/marrasen/gridterm/remote"
	"github.com/marrasen/gridterm/ui"
)

// startedWithSsh builds the window the way main does for "gridterm -ssh
// <target>", with the dialogs, the sidebar and the menu bar it needs to
// ask anything and to show what it holds.
func startedWithSsh(t *testing.T, target string, command ...string) *testApp {
	t.Helper()
	a := newTestApp(t, 90, 30, startedWith(startup{target: target, command: command}))
	withDialogs(t, a)
	withPanel(t, a)
	withMenubar(t, a)
	return a
}

// unknownHostKey makes the window check the host key against a
// known_hosts file of the test's own, which is empty, so the key is
// unknown and the window has to ask about it.
//
// Nothing here reaches the keys of whoever is running the tests: the
// file, the identity and the agent are all the test's.
func unknownHostKey(t *testing.T, a *testApp) {
	t.Helper()
	key := sshtest.WriteKey(t)
	known := filepath.Join(t.TempDir(), "known_hosts")
	a.prepare = func(cfg remote.Config) remote.Config {
		cfg.NoAgent = true
		cfg.Identities = []string{key}
		cfg.KnownHosts = []string{known}
		return cfg
	}
}

// -ssh opens the window first and connects in a pane, so the unknown
// host key is asked about in a dialog and the account of how the machine
// was reached is kept.
//
// It used to connect before there was a window, on the console, where an
// unknown host key was a refusal and there was no account at all.
func TestSshConnectsInAPaneAndAsksInADialog(t *testing.T) {
	s := sshtest.New(t)
	target := serverConfig(t, s).Target()
	a := startedWithSsh(t, target)
	unknownHostKey(t, a)
	dials := dialCounter(a)

	if len(a.panes) != 0 {
		t.Fatalf("%d panes before the first frame, want none: -ssh opens no shell here",
			len(a.panes))
	}

	// It draws with nothing in it at all, which is every frame before
	// the connection opens its pane.
	paint(a)

	// The first frame connects, in a pane that says what it is doing.
	waitFor(t, a, "the pane watching the connection", func() bool { return len(a.panes) == 1 })
	pane := newestPane(t, a)
	watching := watchPane(t, pane)
	if e := a.panes[pane]; e == nil || e.Host != target {
		t.Fatalf("the pane's row is %+v, want one on %s", e, target)
	}
	waitFor(t, a, "the pane to say what it is doing", func() bool {
		return strings.Contains(watching.text(), "connecting to "+target)
	})

	// In a dialog, which is the whole reason the window opens first.
	f := awaitModal(t, a, "the host key question", byTitle[*ui.Form]("Unknown host key"))
	pressButton(t, a, f, "Connect")

	waitForPanes(t, a, 1)
	if got := a.about(target).kind; got != hostMachine {
		t.Errorf("the -ssh target is a %v, want a machine like any other", got)
	}
	if a.machines.runningOn(pane) == nil {
		t.Error("the pane that watched the connection is not carrying the shell")
	}
	if *dials != 1 {
		t.Errorf("%d machines were dialled, want the one -ssh named", *dials)
	}
	checkTree(t, a)
}

// The machine -ssh named is on the sidebar under its own heading, with
// the account on its menu and everything else a connected machine
// offers.
func TestSshTargetIsAMachineOnTheSidebar(t *testing.T) {
	s := sshtest.New(t)
	target := serverConfig(t, s).Target()
	a := startedWithSsh(t, target)
	pinServers(t, a, s)
	waitForPanes(t, a, 1)

	if !strings.Contains(strings.Join(panelText(a, time.Now()), "\n"), target) {
		t.Fatalf("the sidebar has no heading for %s: %v", target, panelText(a, time.Now()))
	}
	menu := clickPlus(t, a, target)
	for _, want := range []string{"conn.terminal", "conn.files", "conn.log"} {
		if !slices.Contains(menuCommands(menu), want) {
			t.Errorf("the plus on %s offers %v, with no %s", target, menuCommands(menu), want)
		}
	}

	chooseMenuItem(t, menu, "conn.log")
	n := awaitModal(t, a, "the account of how it was reached",
		byTitlePrefix[*ui.Notice]("How "))
	if !strings.Contains(n.Message(), "connected to "+target) {
		t.Errorf("the account says %q", n.Message())
	}
}

// -ssh with a command runs the command on the far end rather than
// opening a shell there.
func TestSshRunsTheCommandItWasGiven(t *testing.T) {
	s := sshtest.New(t)
	target := serverConfig(t, s).Target()
	a := startedWithSsh(t, target, "echo", "hi")
	pinServers(t, a, s)

	// Watched from the moment the pane opens: the fold empties the
	// screen when the shell arrives, so what the command printed is only
	// there in what the pane was written.
	waitFor(t, a, "the pane the command runs in", func() bool { return len(a.panes) == 1 })
	pane := newestPane(t, a)
	watching := watchPane(t, pane)
	waitForPanes(t, a, 1)

	if e := a.panes[pane]; e == nil || e.Kind != conns.Command || e.Label != "echo hi" {
		t.Fatalf("the pane's row is %+v, want the command on it", e)
	}
	waitFor(t, a, "the command to run on the far end", func() bool {
		return strings.Contains(watching.text(), `RAN 'echo' 'hi'`)
	})
}

// A machine that does not answer leaves the window open, with the pane
// that watched the attempt still holding the account of it.
//
// The window would otherwise have nothing in it at all: -ssh opens no
// pane of its own, so this is the only one there is.
func TestSshThatCannotConnectKeepsItsPaneAndTheWindow(t *testing.T) {
	s := sshtest.New(t)
	cfg := serverConfig(t, s)
	// A machine the test can name, at a port nothing answers on.
	cfg.Port = 1
	target := cfg.Target()
	a := startedWithSsh(t, target)
	unknownHostKey(t, a)

	said := waitForFailure(t, a, target)
	if !strings.Contains(said, "connecting to "+target) {
		t.Errorf("the pane kept %q, want the account of the attempt", said)
	}
	if len(a.panes) != 1 {
		t.Errorf("%d panes after a failed -ssh, want the one that says why", len(a.panes))
	}
	if a.quit.Load() {
		t.Error("the window quit when the connection failed")
	}
	pane := newestPane(t, a)
	if e := a.panes[pane]; e == nil || e.Label != "not connected" {
		t.Errorf("the row says %+v, want it to say the connection was not made", e)
	}
	checkTree(t, a)
}
