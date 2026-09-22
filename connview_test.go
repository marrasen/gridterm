package main

import (
	"strings"
	"sync"
	"testing"

	"github.com/marrasen/gridterm/internal/sshtest"
	"github.com/marrasen/gridterm/remote"
	"github.com/marrasen/gridterm/ui"
	"github.com/marrasen/gridterm/ui/term"
)

// paneWatcher records everything a pane is ever written, in order.
//
// It is how a test sees what the pane showed at a moment it has already
// gone past: reading the screen catches only what is on it now, and the
// fold empties the screen as soon as the shell is there.
type paneWatcher struct {
	mu  sync.Mutex
	saw strings.Builder
}

func (w *paneWatcher) Screen(p []byte) error { _, err := w.Write(p); return err }
func (w *paneWatcher) Ended()                {}
func (w *paneWatcher) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.saw.Write(p)
	return len(p), nil
}

// text is what the pane has been written so far.
func (w *paneWatcher) text() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.saw.String()
}

// watchPane records what a pane is written until the test ends.
func watchPane(t *testing.T, pane *term.Terminal) *paneWatcher {
	t.Helper()
	w := &paneWatcher{}
	stop, err := pane.Watch(w)
	if err != nil {
		t.Fatalf("watch the pane: %v", err)
	}
	t.Cleanup(stop)
	return w
}

// paneWithScrollback is everything a pane holds: what is on screen and
// what has scrolled off the top of it.
func paneWithScrollback(pane *term.Terminal) string {
	pane.ScrollView(pane.Size().Rows * 4)
	back := paneText(pane)
	pane.ScrollView(-pane.Size().Rows * 4)
	return back + paneText(pane)
}

// accountPane waits for a pane showing an account to hold what it should
// say, and hands the pane back.
//
// A pane rather than a dialog: the account is read through a terminal
// like the window's own log, so it arrives a read at a time.
func accountPane(t *testing.T, a *testApp, want string, also ...*testApp) *term.Terminal {
	t.Helper()
	var found *term.Terminal
	waitFor(t, a, "the account pane to say "+want, func() bool {
		for pane := range a.accounts {
			if strings.Contains(paneWithScrollback(pane), want) {
				found = pane
				return true
			}
		}
		return false
	}, also...)
	return found
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
		return strings.Contains(paneText(pane), "Connected to margit")
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
	if !strings.Contains(got, `"Connection Log"`) {
		t.Errorf("the summary does not say where the account went: %q", got)
	}
}

// A connection that was not made folds nothing away: the pane is the
// only account of what happened and it stays open, whole.
func TestAFailedConnectionKeepsItsAccountInThePane(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	withMenubar(t, a)
	// Nothing to dial with but the test's own key, and a port nothing
	// answers on.
	pinServers(t, a)
	if err := a.book.Put(remote.Host{
		Name: "margit", Address: "127.0.0.1", Port: 1, User: "tester",
	}, ""); err != nil {
		t.Fatalf("save the machine: %v", err)
	}
	a.refreshServers()

	clickTerminalLine(t, a, "margit")
	pane := newestPane(t, a)

	waitFor(t, a, "the pane to say the connection was not made", func() bool {
		return strings.Contains(paneText(pane), "The connection was not made")
	})
	if got := paneText(pane); !strings.Contains(got, "connecting to") {
		t.Errorf("the account was folded away from a connection that failed: %q", got)
	}
}

// The plus on a connected machine's row opens the whole account, in a
// pane that scrolls and copies.
func TestThePlusOnAConnectedRowShowsHowItWasReached(t *testing.T) {
	a, _ := aConnectedMachine(t)

	menu := clickPlus(t, a, "margit")
	if !offers(menu, "conn.log") {
		t.Fatalf("the plus offers %v", menuCommands(menu))
	}
	chooseMenuItem(t, menu, "conn.log")

	pane := accountPane(t, a, "connected to margit")
	if got := paneWithScrollback(pane); !strings.Contains(got, "connecting to") {
		t.Errorf("the account does not say how it started: %q", got)
	}
	// Nothing to type into: there is no program at the far end.
	if a.root.Modal() != nil {
		t.Errorf("it opened %T as well", a.root.Modal())
	}
}

// And the palette opens the same account, for a user who does not want
// the mouse.
func TestThePaletteShowsHowAMachineWasReached(t *testing.T) {
	a, _ := aConnectedMachine(t)

	runFromPalette(t, a, "conn.log")

	accountPane(t, a, "connected to margit")
}

// A machine still being reached has an account too, and the dialog says
// so in the tense the connection is in.
func TestAMachineStillBeingReachedShowsItsAccountSoFar(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	withMenubar(t, a)
	// A server that accepts the connection and then never speaks, so
	// the dial is genuinely still running.
	host, port := sshtest.Deaf(t)
	pinServers(t, a)
	if err := a.book.Put(remote.Host{
		Name: "margit", Address: host, Port: port, User: "tester",
	}, ""); err != nil {
		t.Fatalf("save the machine: %v", err)
	}
	a.refreshServers()

	clickTerminalLine(t, a, "margit")
	waitFor(t, a, "the dial to be under way", func() bool {
		return a.about("margit").log() != nil
	})

	menu := clickPlus(t, a, "margit")
	chooseMenuItem(t, menu, "conn.log")

	// Whatever state the connection is in: the account itself says
	// whether it is still connecting.
	accountPane(t, a, "connecting to")
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

	accountPane(t, client, "taking over")
}

// The row of a connection that dropped opens the account of it.
//
// This is the whole of what the window has to say afterwards: the
// machine is no longer held under its name, so the menu line that opens
// an account is not offered and the row is the only way to it. It used
// to answer a click with nothing at all.
func TestTheRowOfADroppedConnectionOpensItsAccount(t *testing.T) {
	s := sshtest.New(t)
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)

	a.connect(serverConfig(t, s))
	waitForPanes(t, a, 2)
	host := serverConfig(t, s).Target()
	row := serverRow(t, a, host)

	// The network goes away.
	s.CloseClients()
	waitFor(t, a, "the connection to be given up on", func() bool {
		return a.machines.named(host) == nil
	})

	if row.Reveal == nil {
		t.Fatal("the row of a dropped connection does nothing when it is clicked")
	}
	row.Reveal()

	pane := accountPane(t, a, "connection lost")
	if got := paneWithScrollback(pane); !strings.Contains(got, "connecting to") {
		t.Errorf("the account does not say how the connection was made: %q", got)
	}
}

// The account is the same one however it is opened, and a second ask
// goes to the pane that is already showing it.
func TestOneAccountOpensOnePane(t *testing.T) {
	a, _ := aConnectedMachine(t)

	runFromPalette(t, a, "conn.log")
	first := accountPane(t, a, "connected to margit")
	panes := len(a.panes)

	runFromPalette(t, a, "conn.log")

	if len(a.accounts) != 1 {
		t.Errorf("%d panes are showing accounts, want the one", len(a.accounts))
	}
	if len(a.panes) != panes {
		t.Errorf("the window holds %d panes, want the %d it had", len(a.panes), panes)
	}
	if got := ui.FocusedLeaf(a.root.Widget()); got != ui.Widget(first) {
		t.Errorf("the second ask focused %T, want the pane already showing it", got)
	}
}

// The window's own log is a different pane from a connection's account,
// although both are read the same way.
func TestTheWindowLogIsNotAConnectionsAccount(t *testing.T) {
	a, _ := aConnectedMachine(t)

	runFromPalette(t, a, "conn.log")
	account := accountPane(t, a, "connected to margit")
	if err := a.showLog(); err != nil {
		t.Fatalf("open the window log: %v", err)
	}

	log := a.logPane()
	if log == nil {
		t.Fatal("the window log did not open")
	}
	if log == account {
		t.Error("the window log opened the account pane instead of one of its own")
	}
}
