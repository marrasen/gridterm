package main

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/input"
	"github.com/marrasen/gridterm/internal/sshtest"
	"github.com/marrasen/gridterm/remote"
	"github.com/marrasen/gridterm/ui"
)

// startedWithSSH builds the window the way main does for "gridterm -ssh
// <target>", with the dialogs, the sidebar and the menu bar it needs to
// ask anything and to show what it holds.
func startedWithSSH(t *testing.T, target string, command ...string) *testApp {
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
	a := startedWithSSH(t, target)
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
	f := awaitModal(t, a, "the host key question", byTitle[*ui.Form](dlgUnknownHostKey))
	pressButton(t, a, f, btnConnect)

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
	a := startedWithSSH(t, target)
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
	accountPane(t, a, "connected to "+target)
}

// -ssh with a command runs the command on the far end rather than
// opening a shell there.
func TestSshRunsTheCommandItWasGiven(t *testing.T) {
	s := sshtest.New(t)
	target := serverConfig(t, s).Target()
	a := startedWithSSH(t, target, "echo", "hi")
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
	a := startedWithSSH(t, target)
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

// A tab opened under -ssh opens on the machine -ssh named, on the
// connection that is already there rather than a second login.
func TestANewTabUnderSshOpensOnTheTarget(t *testing.T) {
	s := sshtest.New(t)
	target := serverConfig(t, s).Target()
	a := startedWithSSH(t, target)
	pinServers(t, a, s)
	waitForPanes(t, a, 1)

	sendKey(t, a, press(input.KeyT, input.ModCtrl|input.ModShift))

	waitForPanes(t, a, 2)
	pane := newestPane(t, a)
	if e := a.panes[pane]; e == nil || e.Host != target {
		t.Fatalf("the new tab's row is %+v, want one on %s", e, target)
	}
	if m := a.machines.runningOn(pane); m == nil {
		t.Error("the new tab is not riding on the connection to the target")
	}
	if n := s.Conns(); n != 1 {
		t.Errorf("the server saw %d logins, want the one both panes ride on", n)
	}
	checkTree(t, a)
}

// So does a split, from the line the chooser opens on.
func TestASplitUnderSshOpensOnTheTarget(t *testing.T) {
	s := sshtest.New(t)
	target := serverConfig(t, s).Target()
	a := startedWithSSH(t, target)
	pinServers(t, a, s)
	waitForPanes(t, a, 1)

	sendKey(t, a, press(input.KeyD, input.ModCtrl|input.ModShift))
	c, ok := a.root.Modal().(*ui.Chooser)
	if !ok {
		t.Fatalf("the split key showed %T, want the chooser", a.root.Modal())
	}
	takeChoice(t, c, "New terminal")

	waitForPanes(t, a, 2)
	pane := newestPane(t, a)
	if e := a.panes[pane]; e == nil || e.Host != target {
		t.Fatalf("the split's row is %+v, want one on %s", e, target)
	}
	if n := s.Conns(); n != 1 {
		t.Errorf("the server saw %d logins, want the one both panes ride on", n)
	}
	checkTree(t, a)
}

// Once the machine -ssh named has gone, a new pane opens here: there is
// nothing left to open one on.
func TestANewTabOpensHereOnceTheTargetHasGone(t *testing.T) {
	s := sshtest.New(t)
	target := serverConfig(t, s).Target()
	a := startedWithSSH(t, target)
	pinServers(t, a, s)
	waitForPanes(t, a, 1)

	if err := a.dropMachine(target); err != nil {
		t.Fatalf("close the connection: %v", err)
	}
	waitFor(t, a, "the connection to go", func() bool { return a.about(target).machine == nil })

	if err := a.openPane(); err != nil {
		t.Fatalf("a new tab: %v", err)
	}
	pane := newestPane(t, a)
	if e := a.panes[pane]; e == nil || e.Host != conns.Local {
		t.Fatalf("the new tab's row is %+v, want one on this machine", e)
	}
	checkTree(t, a)
}

// A -ssh target the window will not connect to leaves a window with a
// shell in it, not an empty one: the connection opens no pane, so
// nothing else would.
func TestSshThatOpensNoPaneLeavesAShellHere(t *testing.T) {
	a := newTestApp(t, 90, 30, startedWith(startup{target: "statio"}))
	withDialogs(t, a)
	withPanel(t, a)
	// Saved as a gridterm window, so -ssh takes it over rather than
	// logging in, and already held, so the take-over is refused before
	// it opens a pane of its own. Nothing stands behind the machine, so
	// nothing may open a shell on it.
	if err := a.book.Put(remote.Host{
		Name: "statio", Address: "10.0.0.5", Port: 2222, Window: true,
	}, ""); err != nil {
		t.Fatalf("save the window: %v", err)
	}
	pretendMachine(t, a, "statio")

	n := awaitModal(t, a, "the reason it was refused", byTitlePrefix[*ui.Notice]("Could not "))
	if !strings.Contains(n.Message(), "already connected") {
		t.Errorf("it said %q", n.Message())
	}
	if len(a.panes) != 1 {
		t.Fatalf("%d panes, want the one shell the window fell back to", len(a.panes))
	}
	for _, e := range a.panes {
		if e.Host != conns.Local {
			t.Errorf("the pane is on %s, want this machine", e.Host)
		}
	}
	if a.quit.Load() {
		t.Error("the window quit")
	}
}

// The keys that act on a pane do nothing at all at the window -ssh opens
// with, which has none yet: no panic, and nothing quits.
func TestTheKeysDoNothingAtAWindowWithNoPaneYet(t *testing.T) {
	s := sshtest.New(t)
	a := startedWithSSH(t, serverConfig(t, s).Target())
	if len(a.panes) != 0 {
		t.Fatalf("%d panes before the first frame, want none", len(a.panes))
	}

	for _, key := range []input.Key{input.KeyW, input.KeyD, input.KeyH} {
		if _, err := a.root.HandleKey(press(key, input.ModCtrl|input.ModShift)); err != nil {
			t.Fatalf("the window refused %v: %v", key, err)
		}
		if a.quit.Load() {
			t.Fatalf("the window quit on %v", key)
		}
		// Each says why in a dialog, which the next key would go to.
		if m, ok := a.root.Modal().(ui.KeyHandler); ok {
			dismiss(t, m)
		}
	}
	// And the one that opens a pane opens it here, because the machine
	// -ssh named is not connected yet.
	sendKey(t, a, press(input.KeyT, input.ModCtrl|input.ModShift))
	if len(a.panes) != 1 {
		t.Fatalf("%d panes after a new tab, want the one", len(a.panes))
	}
	if a.quit.Load() {
		t.Error("the window quit")
	}
	checkTree(t, a)
}

// "Terminal" on the row for this machine opens a shell here, even while
// new panes are opening on the machine -ssh named.
//
// The row names the machine, so the line on it means that machine and
// not the one a bare new tab would use.
func TestTerminalOnTheLocalRowOpensAShellHere(t *testing.T) {
	s := sshtest.New(t)
	target := serverConfig(t, s).Target()
	a := startedWithSSH(t, target)
	pinServers(t, a, s)

	// A pane here, opened before the target answered, which is what puts
	// a row for this machine on the sidebar.
	sendKey(t, a, press(input.KeyT, input.ModCtrl|input.ModShift))
	waitForPanes(t, a, 2)
	if a.about(target).machine == nil {
		t.Fatal("the target is not connected, so this proves nothing")
	}

	chooseMenuItem(t, clickPlus(t, a, conns.Local), "conn.terminal")

	if len(a.panes) != 3 {
		t.Fatalf("%d panes, want the one the line opened", len(a.panes))
	}
	pane := newestPane(t, a)
	if e := a.panes[pane]; e == nil || e.Host != conns.Local {
		t.Fatalf("the new pane's row is %+v, want one on this machine", e)
	}
	if n := s.Conns(); n != 1 {
		t.Errorf("the server saw %d logins, want the one it had already", n)
	}
	checkTree(t, a)

	// And so does the chooser's line for this machine, which is offered
	// because the line it opens on is the target rather than here.
	sendKey(t, a, press(input.KeyD, input.ModCtrl|input.ModShift))
	c, ok := a.root.Modal().(*ui.Chooser)
	if !ok {
		t.Fatalf("the split key showed %T, want the chooser", a.root.Modal())
	}
	takeChoice(t, c, "Terminal on Local")

	if e := a.panes[newestPane(t, a)]; e == nil || e.Host != conns.Local {
		t.Fatalf("the split's row is %+v, want one on this machine", e)
	}
	if n := s.Conns(); n != 1 {
		t.Errorf("the server saw %d logins, want the one it had already", n)
	}
	checkTree(t, a)
}
