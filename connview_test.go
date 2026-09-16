package main

import (
	"strings"
	"testing"

	"github.com/marrasen/gridterm/internal/sshtest"
	"github.com/marrasen/gridterm/ui"
	"github.com/marrasen/gridterm/ui/term"
)

// paneWithScrollback is everything a pane holds: what is on screen and
// what has scrolled off the top of it.
func paneWithScrollback(pane *term.Terminal) string {
	pane.ScrollView(pane.Size().Rows * 4)
	back := paneText(pane)
	pane.ScrollView(-pane.Size().Rows * 4)
	return back + paneText(pane)
}

// aConnectedMachine connects to a test server from the plus on its row
// and hands back the window and the pane the shell is in.
func aConnectedMachine(t *testing.T) (*testApp, *term.Terminal) {
	t.Helper()
	s := sshtest.New(t)
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	withMenubar(t, a)
	pinServers(t, a, s)
	saveHost(t, a, "margit", s, "")
	a.refreshServers()

	clickTerminalLine(t, a, "margit")
	waitFor(t, a, "the machine to answer", func() bool {
		return a.machines.named("margit") != nil
	})
	pane := newestPane(t, a)
	waitFor(t, a, "the pane to fold its account away", func() bool {
		return strings.Contains(paneText(pane), "connected to margit in")
	})
	return a, pane
}

// Once the connection is made the pane holds one line about it, and the
// account that was there while it was being made is gone: screen and
// scrollback both.
func TestTheAccountIsFoldedAwayOnceTheShellIsThere(t *testing.T) {
	_, pane := aConnectedMachine(t)

	got := paneWithScrollback(pane)
	if strings.Contains(got, "connecting to") {
		t.Errorf("the account is still in the pane: %q", got)
	}
	// The summary wraps in a pane this wide, so only the words that stay
	// on one line are looked for.
	if !strings.Contains(got, `"How it was reached"`) {
		t.Errorf("the summary does not say where the account went: %q", got)
	}
}

// A connection that was not made folds nothing away: the pane is the
// only account of what happened and it stays open, whole.
func TestAFailedConnectionKeepsItsAccountInThePane(t *testing.T) {
	s := sshtest.New(t)
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)

	cfg := serverConfig(t, s)
	// A key the server would accept, pointed at a port nothing answers.
	cfg.Port = 1
	a.connect(cfg)
	pane := newestPane(t, a)

	waitFor(t, a, "the pane to say the connection was not made", func() bool {
		return strings.Contains(paneText(pane), "The connection was not made")
	})
	if got := paneText(pane); !strings.Contains(got, "connecting to") {
		t.Errorf("the account was folded away from a connection that failed: %q", got)
	}
}

// The plus on a connected machine's row opens the whole account, in a
// dialog that scrolls and copies.
func TestThePlusOnAConnectedRowShowsHowItWasReached(t *testing.T) {
	a, _ := aConnectedMachine(t)

	menu := clickPlus(t, a, "margit")
	if !offers(menu, "conn.log") {
		t.Fatalf("the plus offers %v", menuCommands(menu))
	}
	chooseMenuItem(t, menu, "conn.log")

	n := awaitModal(t, a, "the account", byTitle[*ui.Notice]("How margit was reached"))
	for _, want := range []string{"connecting to", "connected to margit"} {
		if !strings.Contains(n.Message(), want) {
			t.Errorf("the account does not say %q: %q", want, n.Message())
		}
	}
}

// And the palette opens the same account, for a user who does not want
// the mouse.
func TestThePaletteShowsHowAMachineWasReached(t *testing.T) {
	a, _ := aConnectedMachine(t)

	runFromPalette(t, a, "conn.log")

	n := awaitModal(t, a, "the account", byTitle[*ui.Notice]("How margit was reached"))
	if !strings.Contains(n.Message(), "connected to margit") {
		t.Errorf("the account is %q", n.Message())
	}
}

// A machine nothing was connected to has no account, and the line that
// would show one is not offered.
func TestThereIsNoAccountForAMachineNothingReached(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	saveHostNamed(t, a, "margit", "margit.example")
	a.refreshServers()

	menu := clickPlus(t, a, "margit")
	if offers(menu, "conn.log") {
		t.Errorf("the plus on a machine nothing reached offers %v", menuCommands(menu))
	}
}

// A window taken over keeps its account the same way, and the plus on
// its row opens it.
func TestThePlusOnATakenOverWindowShowsHowItWasReached(t *testing.T) {
	client := aWindowTakenOverAs(t, "statio")
	waitFor(t, client, "the window's account to be kept", func() bool {
		return client.about("statio").log() != nil
	})

	menu := clickPlus(t, client, "statio")
	if !offers(menu, "conn.log") {
		t.Fatalf("the plus offers %v", menuCommands(menu))
	}
	chooseMenuItem(t, menu, "conn.log")

	n := awaitModal(t, client, "the account", byTitle[*ui.Notice]("How statio was reached"))
	if !strings.Contains(n.Message(), "taking over") {
		t.Errorf("the account does not say what was done: %q", n.Message())
	}
}
