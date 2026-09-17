package main

import (
	"context"
	"errors"
	"net"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/internal/sshtest"
	"github.com/marrasen/gridterm/meter"
	"github.com/marrasen/gridterm/remote"
	"github.com/marrasen/gridterm/session"
	"github.com/marrasen/gridterm/ui"
	"github.com/marrasen/gridterm/ui/term"
)

// pinServers makes the app connect to the given test servers and to
// nothing else: their host keys, one key file, and no agent.
//
// It is the saved-server path's equivalent of serverConfig, which the
// typed-target tests hand in directly. A machine the test did not name
// is refused rather than reaching the developer's own ~/.ssh.
func pinServers(t *testing.T, a *testApp, servers ...*sshtest.Server) {
	t.Helper()
	key := sshtest.WriteKey(t)
	keys := map[string]ssh.PublicKey{}
	for _, s := range servers {
		keys[s.Addr()] = s.HostKey()
	}
	a.prepare = func(cfg remote.Config) remote.Config {
		cfg.NoAgent = true
		cfg.Identities = []string{key}
		if key, ok := keys[net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))]; ok {
			cfg.HostKeyCallback = ssh.FixedHostKey(key)
		}
		return cfg
	}
}

// holdTheNames holds a route's names the way a connection being made
// does, for a test that drives the dialling without a machine to dial.
func holdTheNames(t *testing.T, a *testApp, d *dialling) {
	t.Helper()
	if err := a.machines.holdNames(d); err != nil {
		t.Fatalf("holding the names of %v: %v", d.names, err)
	}
}

// saveHost puts a machine in the book, pointed at a test server.
func saveHost(t *testing.T, a *testApp, name string, s *sshtest.Server, via string) {
	t.Helper()
	host, port := s.Host()
	h := remote.Host{Name: name, Address: host, Port: port, User: "tester", Via: via}
	if err := a.book.Put(h, ""); err != nil {
		t.Fatalf("save %s: %v", name, err)
	}
}

// A second terminal on a machine is another channel on the connection
// already open, not another login.
func TestASecondTerminalRidesOnTheSameConnection(t *testing.T) {
	s := sshtest.New(t)
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)

	a.connect(serverConfig(t, s))
	waitForPanes(t, a, 2)

	host := serverConfig(t, s).Target()
	if a.machines.named(host) == nil {
		t.Fatalf("the connection was not kept: %v", a.machines.names())
	}
	if err := a.openOn(host, nil, nil); err != nil {
		t.Fatalf("a second terminal: %v", err)
	}
	if len(a.panes) != 3 {
		t.Fatalf("%d panes, want the local one and two on the server", len(a.panes))
	}
	if n := s.Conns(); n != 1 {
		t.Fatalf("the server saw %d logins, want the one both terminals ride on", n)
	}
	checkTree(t, a)
}

// A remote command is a connection of its own kind, named by what it
// runs, on the machine that is already connected to.
func TestRunACommandOnAConnectedMachine(t *testing.T) {
	s := sshtest.New(t)
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)

	a.connect(serverConfig(t, s))
	waitForPanes(t, a, 2)
	host := serverConfig(t, s).Target()

	if err := a.openOn(host, []string{"apt-get", "upgrade"}, nil); err != nil {
		t.Fatalf("run a command: %v", err)
	}
	if len(a.panes) != 3 {
		t.Fatalf("%d panes, want the command as well", len(a.panes))
	}
	if n := s.Conns(); n != 1 {
		t.Fatalf("the server saw %d logins, want one", n)
	}

	var found bool
	for _, e := range a.panes {
		if e.Kind == conns.Command && e.Label == "apt-get upgrade" {
			found = true
		}
	}
	if !found {
		t.Fatalf("no row says what is running: %v", panelText(a, time.Now()))
	}
}

// A saved machine behind another is reached through it, with no local
// port opened for it.
func TestConnectSavedWalksTheRoute(t *testing.T) {
	near := sshtest.New(t)
	far := sshtest.New(t)
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	pinServers(t, a, near, far)

	saveHost(t, a, "edge", near, "")
	saveHost(t, a, "db", far, "edge")

	if err := a.connectSaved("db"); err != nil {
		t.Fatalf("connectSaved: %v", err)
	}
	waitForPanes(t, a, 2)

	// Both machines are held, and the far one says what carries it.
	if a.machines.named("edge") == nil || a.machines.named("db") == nil {
		t.Fatalf("the window holds %v, want both machines", a.machines.names())
	}
	if got := a.machines.named("db").conn.Via(); got != a.machines.named("edge").conn {
		t.Fatalf("db came through %v, want edge", got)
	}
	// And it really went through rather than being dialled from here.
	if n := near.Forwards(); n != 1 {
		t.Fatalf("the machine in the middle carried %d streams, want the one hop", n)
	}
	if n := far.Conns(); n != 1 {
		t.Fatalf("the far machine saw %d connections, want 1", n)
	}
	// The terminal is on the far machine, not on the one in the way.
	var hosts []string
	for _, e := range a.panes {
		hosts = append(hosts, e.Host)
	}
	if !has(hosts, "db") {
		t.Fatalf("the terminal is on %v, want db", hosts)
	}
	checkTree(t, a)
}

// Closing the machine in the middle closes what was reached through it,
// and takes the panes on both away.
func TestClosingABastionClosesWhatRidesOnIt(t *testing.T) {
	near := sshtest.New(t)
	far := sshtest.New(t)
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	pinServers(t, a, near, far)

	saveHost(t, a, "edge", near, "")
	saveHost(t, a, "db", far, "edge")
	if err := a.connectSaved("db"); err != nil {
		t.Fatalf("connectSaved: %v", err)
	}
	waitForPanes(t, a, 2)
	// A terminal on the machine in the middle as well, so there is
	// something of its own to lose.
	if err := a.openOn("edge", nil, nil); err != nil {
		t.Fatalf("a terminal on edge: %v", err)
	}
	if len(a.panes) != 3 {
		t.Fatalf("%d panes, want one local and one on each machine", len(a.panes))
	}

	if err := a.dropMachine("edge"); err != nil {
		t.Fatalf("dropMachine: %v", err)
	}
	if a.machines.count() != 0 {
		t.Fatalf("the window still holds %v", a.machines.names())
	}
	if len(a.panes) != 1 {
		t.Fatalf("%d panes after closing the machine in the middle, want the local one", len(a.panes))
	}
	// Nothing about either machine is left on the panel.
	for _, group := range a.registry.Groups(time.Now()) {
		if group.Host == "edge" || group.Host == "db" {
			t.Fatalf("%s is still on the panel with %d rows", group.Host, len(group.Rows))
		}
	}
	checkTree(t, a)
}

// Closing a connection from the panel ends it and everything on it.
func TestTheServerRowClosesTheWholeConnection(t *testing.T) {
	s := sshtest.New(t)
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)

	a.connect(serverConfig(t, s))
	waitForPanes(t, a, 2)
	host := serverConfig(t, s).Target()

	var server *conns.Entry
	for _, group := range a.registry.Groups(time.Now()) {
		for _, row := range group.Rows {
			if row.Kind == conns.Server {
				server = row.Entry
			}
		}
	}
	if server == nil {
		t.Fatalf("the connection itself has no row: %v", panelText(a, time.Now()))
	}
	if server.Host != host {
		t.Errorf("the row is under %q, want %q", server.Host, host)
	}
	if server.Close == nil {
		t.Fatal("the connection cannot be closed from the panel")
	}
	if err := server.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if a.machines.count() != 0 {
		t.Fatalf("the window still holds %v", a.machines.names())
	}
	if len(a.panes) != 1 {
		t.Fatalf("%d panes after closing the connection, want the local one", len(a.panes))
	}
}

// Waiting for a connection already on its way runs the request again
// once it is done.
func TestWaitingForTheOneOnItsWayRunsTheRequestAgain(t *testing.T) {
	s := sshtest.New(t)
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)

	cfg := serverConfig(t, s)
	a.connectAs("box", cfg)
	a.connectAs("box", cfg)
	f := awaitModal(t, a, "the Already connecting to box dialog", byTitle[*ui.Form]("Already connecting to box"))
	pressButton(t, a, f, "Wait for it")

	// The first connection lands, and then the second request runs on
	// the machine it made rather than logging in again.
	waitFor(t, a, "both panes to be open", func() bool { return len(a.panes) == 3 })
	if n := s.Conns(); n != 1 {
		t.Fatalf("the machine saw %d logins, want the one", n)
	}
	// And the machine that answered while the second request waited is
	// connected rather than still being connected to.
	heldAs(t, a, "box", isConnected)
}

// A machine that failed to connect is not left marked as busy, or the
// next attempt is refused for ever.
func TestAFailedConnectionCanBeRetried(t *testing.T) {
	s := sshtest.New(t)
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)

	cfg := serverConfig(t, s)
	cfg.Port = 1
	a.connectAs("box", cfg)
	waitForFailure(t, a, "box")
	if a.machines.connecting("box") != nil {
		t.Fatal("the machine is still marked as being connected to")
	}
	if a.machines.count() != 0 {
		t.Fatalf("a failed connection was kept: %v", a.machines.names())
	}
}

// A machine of a route that answered is a machine like any other, so it
// stays connected when the one beyond it does not. Closing it because
// the next hop refused would throw away a connection the user can work
// on and would have to make again.
func TestAFailedRouteKeepsTheHopItReached(t *testing.T) {
	near := sshtest.New(t)
	far := sshtest.New(t)
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	pinServers(t, a, near, far)

	saveHost(t, a, "edge", near, "")
	// The far machine's key is pinned to the near one's, so the first
	// hop works and the second is refused.
	host, port := far.Host()
	if err := a.book.Put(remote.Host{
		Name: "db", Address: host, Port: port, User: "tester", Via: "edge",
	}, ""); err != nil {
		t.Fatalf("Put: %v", err)
	}
	a.prepare = func(cfg remote.Config) remote.Config {
		cfg.NoAgent = true
		cfg.Identities = []string{sshtest.WriteKey(t)}
		cfg.HostKeyCallback = ssh.FixedHostKey(near.HostKey())
		return cfg
	}

	if err := a.connectSaved("db"); err != nil {
		t.Fatalf("connectSaved: %v", err)
	}
	said := waitForFailure(t, a, "db")
	if !strings.Contains(said, "db") {
		t.Fatalf("the failure does not say which machine refused: %q", said)
	}
	if a.machines.named("edge") == nil {
		t.Fatalf("the machine that answered was closed: %v", a.machines.names())
	}
	if a.machines.named("db") != nil {
		t.Fatalf("the machine that refused was kept: %v", a.machines.names())
	}
	if !strings.Contains(said, "edge answered and stays connected") {
		t.Errorf("the pane does not say what is still connected: %q", said)
	}
	// And no name is held any more, so another attempt at either can be
	// made straight away.
	if a.machines.connecting("edge") != nil || a.machines.connecting("db") != nil {
		t.Fatal("a machine was left marked as being connected to")
	}
	// The pane that was watching stays, holding the reason. Closing it
	// is what takes it away, and leaves what was there before.
	if len(a.panes) != 2 {
		t.Fatalf("%d panes after a failed route, want the one that was there"+
			" and the one that says why", len(a.panes))
	}
	for pane, e := range a.panes {
		if e.Host != "db" {
			continue
		}
		if err := a.closePane(pane); err != nil {
			t.Fatalf("close the pane: %v", err)
		}
	}
	if len(a.panes) != 1 {
		t.Fatalf("%d panes after closing it, want the one that was there", len(a.panes))
	}
	// Closing the machine that answered really closes it: a connection
	// nothing holds is one nothing will ever close.
	if err := a.dropMachine("edge"); err != nil {
		t.Fatalf("drop the machine that answered: %v", err)
	}
	for deadline := time.Now().Add(waitBudget); near.Live() > 0; {
		if time.Now().After(deadline) {
			t.Fatalf("the machine in the way still has %d connections open", near.Live())
		}
		time.Sleep(time.Millisecond)
	}
}

// A route that cannot be worked out at all is said so where it was
// asked for, with nothing started.
func TestOpenOnRefusesAMachineItDoesNotKnow(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)

	err := a.openOn("nowhere", nil, nil)
	if err == nil {
		t.Fatal("a machine nothing knows about was connected to")
	}
	if !strings.Contains(err.Error(), "nowhere") {
		t.Fatalf("error = %v, want it to name the machine", err)
	}
	if a.machines.beingMade() != 0 {
		t.Error("a connection was started anyway")
	}
}

// The path a user actually takes to run something on a machine: look at
// it, ask for a command, type it, press Run.
func TestRunACommandThroughTheDialog(t *testing.T) {
	s := sshtest.New(t)
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)

	a.connect(serverConfig(t, s))
	waitForPanes(t, a, 2)
	host := serverConfig(t, s).Target()

	if err := a.openCommandHere(); err != nil {
		t.Fatalf("openCommandHere: %v", err)
	}
	f := awaitModal(t, a, "the Run a command on "+host+" dialog", byTitle[*ui.Form]("Run a command on "+host))
	typeIntoField(t, a, f, "Command", "uname -a")
	pressButton(t, a, f, "Run")
	waitForPanes(t, a, 3)

	if m := a.root.Modal(); m != nil {
		t.Errorf("a dialog was left open: %T", m)
	}
	var found bool
	for _, e := range a.panes {
		if e.Kind == conns.Command && e.Label == "uname -a" {
			found = true
		}
	}
	if !found {
		t.Fatalf("no row says what is running: %v", panelText(a, time.Now()))
	}
	checkTree(t, a)
}

// A command with nothing in it is said so where it was typed, with the
// dialog still open rather than gone with nothing run.
func TestRunACommandRefusesAnEmptyOne(t *testing.T) {
	s := sshtest.New(t)
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)

	a.connect(serverConfig(t, s))
	waitForPanes(t, a, 2)
	host := serverConfig(t, s).Target()

	if err := a.openCommandHere(); err != nil {
		t.Fatalf("openCommandHere: %v", err)
	}
	f := awaitModal(t, a, "the Run a command on "+host+" dialog", byTitle[*ui.Form]("Run a command on "+host))
	pressButton(t, a, f, "Run")

	if a.root.Modal() != f {
		t.Fatal("the dialog closed on a command with nothing in it")
	}
	if f.Error() == nil {
		t.Fatal("nothing said why an empty command was refused")
	}
	if len(a.panes) != 2 {
		t.Fatalf("%d panes, want nothing started", len(a.panes))
	}
}

// A command runs on this machine too, and gets a command row: named by
// what it runs, and asking whether to run it again when it ends.
func TestRunACommandOnThisMachine(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)

	if err := a.openCommandHere(); err != nil {
		t.Fatalf("a command on this machine: %v", err)
	}
	f := awaitModal[*ui.Form](t, a, "the command dialog", nil)
	typeIntoField(t, a, f, "Command", "make deploy")
	pressButton(t, a, f, "Run")
	waitForPanes(t, a, 2)

	pane := a.focusedTerminal()
	if pane == nil {
		t.Fatal("the command opened no pane")
	}
	if got := a.lastArgv(t); !slices.Equal(got, []string{"make", "deploy"}) {
		t.Fatalf("the pane runs %v, want the command that was typed", got)
	}
	e := a.panes[pane]
	if e == nil {
		t.Fatal("the pane has no row")
	}
	if e.Kind != conns.Command {
		t.Errorf("the row is a %v, want a command", e.Kind)
	}
	a.refreshPanel(time.Now())
	row, ok := panelRow(a, e)
	if !ok {
		t.Fatalf("the pane has no row: %v", panelText(a, time.Now()))
	}
	if row.Text != "make deploy" {
		t.Errorf("the row says %q, want what it runs", row.Text)
	}

	// And when it ends the pane asks whether to run it again, the way a
	// command on a machine does.
	endTheShell(t, a, len(a.shells)-1, pane)
	if q := pane.Asking(); !strings.Contains(q, "make deploy") {
		t.Errorf("the pane asks %q, want the command named in it", q)
	}
	if !strings.Contains(pane.Asking(), "again") {
		t.Errorf("the pane asks %q, want the offer to run it again", pane.Asking())
	}
}

// A command taken from the split chooser lands in the split.
func TestACommandHereLandsInTheSplit(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)

	c := splitChoices(t, a, ui.Columns)
	takeChoice(t, c, "Command on Local…")
	f := awaitModal(t, a, "the command dialog", byTitle[*ui.Form]("Run a command on Local"))
	typeIntoField(t, a, f, "Command", "make deploy")
	pressButton(t, a, f, "Run")
	waitForPanes(t, a, 2)

	if got := len(a.stage.Children()); got != 1 {
		t.Fatalf("the stage holds %d things, want the one split", got)
	}
	if _, isSplit := a.stage.Children()[0].(*ui.Split); !isSplit {
		t.Fatalf("the stage holds %T, want the split", a.stage.Children()[0])
	}
	checkTree(t, a)
}

// A command here that cannot be started says so, and opens no pane.
func TestACommandHereThatWillNotStartSaysSo(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	boom := errors.New("there is no such program")
	a.newShell = func([]string, int, int) (session.Session, error) { return nil, boom }

	if err := a.openCommandHere(); err != nil {
		t.Fatalf("a command on this machine: %v", err)
	}
	f := awaitModal[*ui.Form](t, a, "the command dialog", nil)
	typeIntoField(t, a, f, "Command", "nowhere")
	pressButton(t, a, f, "Run")

	n := awaitModal[*ui.Notice](t, a, "a notice", nil)
	if n.Title != "Could not run it on Local" {
		t.Errorf("the dialog is titled %q, want the machine named", n.Title)
	}
	if !strings.Contains(n.Message(), boom.Error()) {
		t.Errorf("the dialog says\n%s\nwant it to hold %q", n.Message(), boom)
	}
	if len(a.panes) != 1 {
		t.Errorf("%d panes, want nothing opened", len(a.panes))
	}
}

// A command that has ended keeps its name on the row, so the sidebar
// still says what the pane holds.
func TestACommandHereKeepsItsNameOnTheRowAfterItEnds(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)

	if err := a.openCommandHere(); err != nil {
		t.Fatalf("a command on this machine: %v", err)
	}
	f := awaitModal[*ui.Form](t, a, "the command dialog", nil)
	typeIntoField(t, a, f, "Command", "make deploy")
	pressButton(t, a, f, "Run")
	waitForPanes(t, a, 2)

	pane := a.focusedTerminal()
	endTheShell(t, a, len(a.shells)-1, pane)

	a.refreshPanel(time.Now())
	row, ok := panelRow(a, a.panes[pane])
	if !ok {
		t.Fatalf("the pane has no row: %v", panelText(a, time.Now()))
	}
	if row.Text != "make deploy" {
		t.Errorf("the row says %q once it has ended, want what it ran", row.Text)
	}
}

// A shell run as a command keeps the command's name on its row. A
// command row says the command, whatever the command happens to be.
func TestAShellRunAsACommandHereKeepsTheCommandName(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	scanShells(t, a)

	if err := a.openCommandHere(); err != nil {
		t.Fatalf("a command on this machine: %v", err)
	}
	f := awaitModal[*ui.Form](t, a, "the command dialog", nil)
	// The path of a shell this machine has, so the row could be renamed
	// after it if a command row were named the way a terminal row is.
	typeIntoField(t, a, f, "Command", cmdPath)
	pressButton(t, a, f, "Run")
	waitForPanes(t, a, 2)

	pane := a.focusedTerminal()
	a.refreshPanel(time.Now())
	row, ok := panelRow(a, a.panes[pane])
	if !ok {
		t.Fatalf("the pane has no row: %v", panelText(a, time.Now()))
	}
	if row.Text != cmdPath {
		t.Errorf("the row says %q, want the command that was typed", row.Text)
	}
}

// Another terminal on this machine is a new tab: there is no connection
// to ride on and nothing to log into.
func TestOpenTerminalHereOnThisMachineOpensATab(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)

	if err := a.openTerminalHere(); err != nil {
		t.Fatalf("openTerminalHere: %v", err)
	}
	if len(a.panes) != 2 {
		t.Fatalf("%d panes, want the new tab", len(a.panes))
	}
	checkTree(t, a)
}

// The machine the user is looking at is the one the panel has selected,
// and the focused pane's when it has selected nothing.
func TestCurrentHostFollowsThePanelThenTheFocus(t *testing.T) {
	s := sshtest.New(t)
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)

	if got := a.currentHost(); got != conns.Local {
		t.Fatalf("currentHost = %q, want the local machine", got)
	}

	a.connect(serverConfig(t, s))
	waitForPanes(t, a, 2)
	host := serverConfig(t, s).Target()
	// The new pane has the keys, so that is the machine being looked at.
	if got := a.currentHost(); got != host {
		t.Fatalf("currentHost = %q, want %q", got, host)
	}

	// The panel's selection only counts while the panel has the keys: it
	// outlives being looked at, and a row selected minutes ago is not
	// where the user is.
	a.refreshPanel(time.Now())
	for _, row := range a.panel.Rows() {
		if e, ok := row.Key.(*conns.Entry); ok && e.Host == conns.Local {
			if !a.panel.Select(e) {
				t.Fatal("the local shell could not be selected")
			}
			break
		}
	}
	if got := a.currentHost(); got != host {
		t.Fatalf("currentHost = %q, want the pane with the keys at %q", got, host)
	}

	// Going to the panel makes its selection the machine being looked at.
	if err := a.focusPanel(); err != nil {
		t.Fatalf("focusPanel: %v", err)
	}
	if got := a.currentHost(); got != conns.Local {
		t.Fatalf("currentHost = %q with the local row selected, want the local machine", got)
	}
}

// has reports whether a list holds a string.
func has(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

// A command that has finished keeps its pane. What it printed is what it
// was run for, and a window that cleared the screen the moment the
// program stopped would take the answer away with it.
func TestAFinishedCommandKeepsItsOutput(t *testing.T) {
	s := sshtest.New(t)
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)

	a.connect(serverConfig(t, s))
	waitForPanes(t, a, 2)
	host := serverConfig(t, s).Target()

	if err := a.openOn(host, []string{"uname", "-a"}, nil); err != nil {
		t.Fatalf("run a command: %v", err)
	}
	if len(a.panes) != 3 {
		t.Fatalf("%d panes, want the command as well", len(a.panes))
	}
	// The test server answers a command and ends the session at once.
	waitFor(t, a, "the command to finish", func() bool {
		a.reapExited()
		for pane, e := range a.panes {
			if e.Kind == conns.Command {
				return a.Ended(pane)
			}
		}
		return false
	})

	// The pane is still there, and still in the tree.
	if len(a.panes) != 3 {
		t.Fatalf("%d panes after the command finished, want its output kept", len(a.panes))
	}
	var e *conns.Entry
	var pane *term.Terminal
	for p, have := range a.panes {
		if have.Kind == conns.Command {
			e, pane = have, p
		}
	}
	if e == nil {
		t.Fatal("the command's row has gone")
	}
	if got := e.State(time.Now()); got != meter.Closed {
		t.Fatalf("the row is %v, want it to say the command finished", got)
	}
	var inTree bool
	for _, leaf := range ui.Leaves(a.root.Widget()) {
		if leaf == ui.Widget(pane) {
			inTree = true
		}
	}
	if !inTree {
		t.Fatal("the pane was taken out of the tree with its output")
	}
	checkTree(t, a)

	// And the user can close it when they have read it.
	if e.Close == nil {
		t.Fatal("the finished command cannot be closed")
	}
	if err := e.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if len(a.panes) != 2 {
		t.Fatalf("%d panes after closing it", len(a.panes))
	}
}

// Clearing the finished connections takes away the pane as well as the
// row.
//
// A pane with no row cannot be reached: the sidebar is the only way to
// choose what is showing, and there is no strip of tabs any more.
func TestClearingAFinishedCommandTakesItsPaneToo(t *testing.T) {
	s := sshtest.New(t)
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)

	a.connect(serverConfig(t, s))
	waitForPanes(t, a, 2)
	host := serverConfig(t, s).Target()
	if err := a.openOn(host, []string{"uname", "-a"}, nil); err != nil {
		t.Fatalf("run a command: %v", err)
	}
	waitFor(t, a, "the command to finish", func() bool {
		a.reapExited()
		for pane, e := range a.panes {
			if e.Kind == conns.Command {
				return a.Ended(pane)
			}
		}
		return false
	})

	if err := a.clearFinished(); err != nil {
		t.Fatalf("clear: %v", err)
	}
	for _, e := range a.panes {
		if e.Kind == conns.Command {
			t.Fatal("the command's pane is still open with no row on the sidebar")
		}
	}
	if len(a.ended) != 0 {
		t.Fatalf("%d panes are still marked as finished", len(a.ended))
	}
	// The login terminal and the local one are untouched.
	if len(a.panes) != 2 {
		t.Fatalf("%d panes left, want the local one and the login", len(a.panes))
	}
}

// A command whose channel will not close does not get a row saying it
// finished cleanly.
//
// The dot going grey is the window saying the work is over and let go
// of. When the close failed none of that is true, and the failure is
// shown rather than written to a console the window does not have.
func TestACommandThatWillNotCloseSaysSo(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)

	// A second pane standing in for a command: what it printed is what
	// it was run for, so it keeps its pane when it stops.
	if err := a.openPane(); err != nil {
		t.Fatalf("open tab: %v", err)
	}
	pane := a.focusedTerminal()
	if pane == nil {
		t.Fatal("the new tab has no terminal")
	}
	e := a.panes[pane]
	if e == nil {
		t.Fatal("the new pane has no row")
	}
	e.Kind = conns.Command

	// The shell behind it ends, and refuses to be let go of.
	shell := a.shells[len(a.shells)-1]
	boom := errors.New("the channel would not close")
	shell.failOnClose(boom)
	_ = shell.Close()

	waitFor(t, a, "the command to stop", func() bool {
		a.reapExited()
		return a.Ended(pane)
	})

	if e.Note != "could not be closed" {
		t.Fatalf("the row says %q, want it to say the close failed", e.Note)
	}
	// The pane stays, with what the command printed still in it.
	if a.panes[pane] == nil {
		t.Fatal("the pane went with the failed close")
	}
	n, ok := a.root.Modal().(*ui.Notice)
	if !ok {
		t.Fatalf("the failure showed %T, want a dialog", a.root.Modal())
	}
	if !strings.Contains(n.Message(), boom.Error()) {
		t.Fatalf("the dialog says %q", n.Message())
	}

	// And it is not tried again on every frame: one failure, one dialog.
	a.paneExited()
	a.reapExited()
	a.paneExited()
	a.reapExited()
	if n := len(a.root.Modals()); n != 1 {
		t.Fatalf("%d dialogs, want one", n)
	}
}

// A machine on the way that answered is connected and named straight
// away, so a terminal can be opened on it while the machine beyond it is
// still being reached.
//
// It used to hold every name of the route until the whole route was
// done. A machine further along that never answered then left the
// window saying it was already connecting to one it was connected to,
// and there was no way to give up on either.
func TestAHopThatAnsweredIsUsableWhileTheNextIsStillBeingReached(t *testing.T) {
	near := sshtest.New(t)
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)

	// A machine that answers the socket and then says nothing at all,
	// which is the second hop of the route.
	deafHost, deafPort := sshtest.Deaf(t)
	key := sshtest.WriteKey(t)
	a.prepare = func(cfg remote.Config) remote.Config {
		cfg.NoAgent = true
		cfg.Identities = []string{key}
		cfg.HostKeyCallback = ssh.FixedHostKey(near.HostKey())
		return cfg
	}
	saveHost(t, a, "edge", near, "")
	if err := a.book.Put(remote.Host{
		Name: "db", Address: deafHost, Port: deafPort, User: "tester", Via: "edge",
	}, ""); err != nil {
		t.Fatalf("Put: %v", err)
	}

	if err := a.connectSaved("db"); err != nil {
		t.Fatalf("connectSaved: %v", err)
	}
	waitFor(t, a, "the machine on the way to answer", func() bool { return a.machines.named("edge") != nil })
	if a.machines.connecting("edge") != nil {
		t.Fatal("the window is still holding the name of a machine it is connected to")
	}
	// Which is the whole point of letting go of it: a terminal opens on
	// the machine that answered.
	if err := a.openOn("edge", nil, nil); err != nil {
		t.Fatalf("a terminal on the machine that answered: %v", err)
	}

	// The machine beyond it never answers. Giving up on it comes back at
	// once and lets go of the name, so another attempt can be made.
	if err := a.dropMachine("db"); err != nil {
		t.Fatalf("give up on the machine that never answered: %v", err)
	}
	if a.machines.connecting("db") != nil {
		t.Fatal("the window is still holding the name of a machine nobody is connecting to")
	}
	waitFor(t, a, "the pane to say it was given up on", func() bool {
		for pane, e := range a.panes {
			if e.Host == "db" && strings.Contains(paneText(pane), "given up on") {
				return true
			}
		}
		return false
	})
	// And the machine that answered is still connected: closing it
	// because the one beyond it did not answer would throw away a
	// connection the user can work on.
	if a.machines.named("edge") == nil {
		t.Fatalf("giving up on the machine beyond it closed the one that answered: %v", a.machines.names())
	}
}

// Closing a connection that is still being made gives up on it and lets
// go of every name it was holding, there and then.
//
// This is what "close the connection" does, and it did nothing that the
// user could see. The names went only when the dial goroutine came
// back, and a handshake carried inside another connection need never
// come back at all: the window went on saying it was already connecting
// to a machine nobody was connecting to.
func TestClosingAConnectionStillBeingMadeLetsGoOfItsNames(t *testing.T) {
	near := sshtest.New(t)
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)

	deafHost, deafPort := sshtest.Deaf(t)
	key := sshtest.WriteKey(t)
	a.prepare = func(cfg remote.Config) remote.Config {
		cfg.NoAgent = true
		cfg.Identities = []string{key}
		cfg.HostKeyCallback = ssh.FixedHostKey(near.HostKey())
		return cfg
	}
	saveHost(t, a, "edge", near, "")
	deaf := net.JoinHostPort(deafHost, strconv.Itoa(deafPort))
	if err := a.book.Put(remote.Host{
		Name: "db", Address: deafHost, Port: deafPort, User: "tester", Via: "edge",
	}, ""); err != nil {
		t.Fatalf("Put: %v", err)
	}

	if err := a.connectSaved("db"); err != nil {
		t.Fatalf("connectSaved: %v", err)
	}
	// Wait until it is inside the handshake with the machine that never
	// answers, which is where it used to be impossible to stop.
	waitFor(t, a, "the machine that never answers to be reached", func() bool {
		for pane, e := range a.panes {
			if e.Host == "db" && strings.Contains(paneText(pane), "asking "+deaf) {
				return true
			}
		}
		return false
	})

	// The way the user does it: "Close the connection" on the machine's
	// own row in the sidebar.
	a.hostMenus.nowAbout("db")
	if err := a.disconnectHere(); err != nil {
		t.Fatalf("close the connection: %v", err)
	}
	a.hostMenus.forget()
	if a.machines.connecting("db") != nil {
		t.Fatal("the window is still holding the name of a connection nobody is making")
	}
	// And another attempt can be made at once, which is the whole point.
	if err := a.connectSaved("db"); err != nil {
		t.Fatalf("a second attempt: %v", err)
	}
}

// A name another attempt has taken is left alone when the first one
// lets go.
//
// A dial that is still unwinding comes back after the user has tried
// again. Letting go of the name then would leave the second attempt
// unnamed, and a third one would be allowed to start beside it.
func TestLettingGoLeavesANameAnotherAttemptHasTaken(t *testing.T) {
	a := newTestApp(t, 80, 24)

	first := &dialling{cancel: func() {}, names: []string{"edge"}}
	second := &dialling{cancel: func() {}, names: []string{"edge"}}
	holdTheNames(t, a, first)
	holdTheNames(t, a, second)

	a.machines.release(first)
	if a.machines.connecting("edge") != second {
		t.Fatal("the second attempt lost the name the first one let go of")
	}
	a.machines.release(second)
	if a.machines.connecting("edge") != nil {
		t.Fatal("the name is still held by nobody")
	}
}

// A machine that answers after another attempt has taken its name is
// closed, not kept.
//
// Two connections to one machine would leave the window holding the
// second and closing neither.
func TestAMachineNobodyHasANameForIsClosed(t *testing.T) {
	s := sshtest.New(t)
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	pinServers(t, a, s)

	conn, err := remote.Connect(t.Context(), a.prepare(serverConfig(t, s)))
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	waitFor(t, a, "the connection to be made", func() bool { return s.Live() == 1 })

	mine := &dialling{cancel: func() {}, names: []string{"edge"}}
	theirs := &dialling{cancel: func() {}, names: []string{"edge"}}
	holdTheNames(t, a, theirs)

	// The pane the first attempt is being watched in, which is where
	// anything it has to say has to land.
	log := newConnLog(nil)
	pane, err := a.openSessionTab(log, "edge", conns.Terminal, "connecting", nil)
	if err != nil {
		t.Fatalf("the pane watching it: %v", err)
	}

	route := []step{{name: "edge", cfg: serverConfig(t, s)}}
	a.reached(mine, log, pane, route, 0, conn, "")

	if a.machines.named("edge") != nil {
		t.Fatal("a machine was held under a name another attempt owns")
	}
	waitFor(t, a, "the connection nobody has a name for to close", func() bool {
		return s.Live() == 0
	})
	// And the user is told, rather than watching a pane that says it is
	// still connecting.
	if got := a.panes[pane].Label; got != "not connected" {
		t.Errorf("the row says %q, want it to say the connection was not made", got)
	}
	waitFor(t, a, "the pane to say why", func() bool {
		return strings.Contains(paneText(pane), "something else")
	})
}

// Giving up stops the route where it is, rather than signing in to the
// next machine along.
func TestGivingUpStopsTheRouteWhereItIs(t *testing.T) {
	near := sshtest.New(t)
	far := sshtest.New(t)
	a := newTestApp(t, 80, 24)
	pinServers(t, a, near, far)

	route := []step{
		{name: "edge", cfg: a.prepare(serverConfig(t, near))},
		{name: "db", cfg: a.prepare(serverConfig(t, far))},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var made []*remote.Conn
	err := dialRoute(ctx, nil, route, func(_ int, conn *remote.Conn) {
		made = append(made, conn)
		// What closing the pane does, while the route is between
		// machines.
		cancel()
	})
	for _, conn := range made {
		if err := conn.Close(); err != nil {
			t.Errorf("close what was opened: %v", err)
		}
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("dialRoute = %v, want it to say it was given up on", err)
	}
	if n := far.Conns(); n != 0 {
		t.Fatalf("the machine beyond the one given up on saw %d connections, want none", n)
	}
	// And it does not blame a machine it never tried to sign in to.
	if strings.Contains(err.Error(), "db") {
		t.Fatalf("dialRoute = %v, want it not to name the machine it stopped before", err)
	}
}

// A connection that ended says so on the panel, rather than going on
// saying it is connecting.
//
// The row said "connecting" and nothing took that back, so a connection
// that failed an hour ago still read as one on its way, greyed out.
func TestAConnectionThatEndedNoLongerSaysItIsConnecting(t *testing.T) {
	s := sshtest.New(t)
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)

	cfg := serverConfig(t, s)
	cfg.Port = 1
	a.connectAs("box", cfg)
	waitForFailure(t, a, "box")

	shown := strings.Join(panelText(a, time.Now()), "\n")
	if strings.Contains(shown, "connecting") {
		t.Fatalf("the panel still says it is connecting:\n%s", shown)
	}
	if !strings.Contains(shown, "not connected") {
		t.Fatalf("the panel does not say what became of it:\n%s", shown)
	}
}

// A request waiting for a connection that is not made is thrown away,
// and the pane says so.
//
// It was waiting for that machine. Starting a fresh connection instead
// is not what "wait for it" says, and on a machine that never answers
// the window would ask about it again and again.
func TestWhatWaitedForAConnectionThatFailedIsDropped(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)

	// A machine that answers and then says nothing, so the first
	// attempt is still on its way while the second one asks about it.
	deafHost, deafPort := sshtest.Deaf(t)
	cfg := serverConfig(t, sshtest.New(t))
	cfg.Host, cfg.Port = deafHost, deafPort
	a.connectAs("box", cfg)
	a.connectAs("box", cfg)
	f := awaitModal(t, a, "the Already connecting to box dialog", byTitle[*ui.Form]("Already connecting to box"))
	pressButton(t, a, f, "Wait for it")
	a.pump.run()

	// Given up on, so what was waiting for it has nothing to wait for.
	a.hostMenus.nowAbout("box")
	if err := a.disconnectHere(); err != nil {
		t.Fatalf("give up: %v", err)
	}
	a.hostMenus.forget()

	waitFor(t, a, "the pane to say what was thrown away", func() bool {
		for pane, e := range a.panes {
			if e.Host == "box" && strings.Contains(paneText(pane), "was not started") {
				return true
			}
		}
		return false
	})
	// Nothing was started in its place, and nothing is being asked about
	// a second time.
	waitFor(t, a, "the dial to unwind", func() bool { return a.machines.beingMade() == 0 })
	if _, ok := a.root.Modal().(*ui.Form); ok {
		t.Fatal("it asked about the connection again")
	}
}

// A machine renamed while it was being reached is held under the name it
// has now.
//
// The dial goroutine holds the name it started with, so the connection
// landed under the old name and undid the rename: one machine showed as
// two all over again.
func TestAMachineRenamedWhileItWasBeingReachedLandsUnderTheNewName(t *testing.T) {
	s := sshtest.New(t)
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	pinServers(t, a, s)

	saveHost(t, a, "picard", s, "")
	route := []step{{name: "picard", cfg: a.prepare(serverConfig(t, s))}}
	held := &dialling{cancel: func() {}, names: []string{"picard"}}
	holdTheNames(t, a, held)

	// Renamed while the dial is still running.
	h, _ := a.book.Lookup("picard")
	h.Name = "picard via skylake"
	if err := a.book.Put(h, "picard"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	a.renamedMachine("picard", h)

	// The dial is on its way to the new name from here on: the old one
	// is nobody's, and the new one is held by the dial and by nothing
	// else.
	if a.machines.connecting("picard") != nil {
		t.Fatal("the old name is still being connected to")
	}
	if a.machines.connecting("picard via skylake") != held {
		t.Fatal("the dial on its way did not follow the rename")
	}
	heldAs(t, a, "picard via skylake", isConnecting)

	conn, err := remote.Connect(t.Context(), route[0].cfg)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	a.reached(held, newConnLog(nil), nil, route, 0, conn, "")

	if a.machines.named("picard") != nil {
		t.Fatalf("it landed under the name that is gone: %v", a.machines.names())
	}
	if a.machines.named("picard via skylake") == nil {
		t.Fatalf("it did not land under the name it has now: %v", a.machines.names())
	}
}

// A machine renamed twice while it was being reached lands under the
// name it has now, not the one in between.
func TestAMachineRenamedTwiceWhileBeingReachedLandsUnderTheLastName(t *testing.T) {
	s := sshtest.New(t)
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	pinServers(t, a, s)

	saveHost(t, a, "picard", s, "")
	route := []step{{name: "picard", cfg: a.prepare(serverConfig(t, s))}}
	held := &dialling{cancel: func() {}, names: []string{"picard"}}
	holdTheNames(t, a, held)

	// Twice, while the dial is still running.
	renameSaved(t, a, "picard", "number one")
	renameSaved(t, a, "number one", "captain")

	conn, err := remote.Connect(t.Context(), route[0].cfg)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	a.reached(held, newConnLog(nil), nil, route, 0, conn, "")

	if a.machines.named("captain") == nil {
		t.Fatalf("it did not land under the name it has now: %v", a.machines.names())
	}
	for _, gone := range []string{"picard", "number one"} {
		heldAs(t, a, gone, isFree)
	}
}

// holding is what a name stands for, for a test checking the invariant.
type holding string

const (
	isConnected  holding = "connected"
	isConnecting holding = "being connected to"
	isFree       holding = "held by nothing"
)

// heldAs checks the invariant of the machines type from the outside: a
// name is held by at most one of connected and connecting, and by the
// one the test names.
func heldAs(t *testing.T, a *testApp, name string, want holding) {
	t.Helper()
	got := isFree
	switch {
	case a.machines.named(name) != nil && a.machines.connecting(name) != nil:
		t.Fatalf("%s is connected and being connected to at once", name)
	case a.machines.named(name) != nil:
		got = isConnected
	case a.machines.connecting(name) != nil:
		got = isConnecting
	}
	if got != want {
		t.Fatalf("%s is %s, want %s", name, got, want)
	}
}

// renameSaved renames a saved machine, the way the edit dialog does.
func renameSaved(t *testing.T, a *testApp, was, now string) {
	t.Helper()
	h, ok := a.book.Lookup(was)
	if !ok {
		t.Fatalf("%s is not saved", was)
	}
	h.Name = now
	if err := a.book.Put(h, was); err != nil {
		t.Fatalf("renaming %s to %s: %v", was, now, err)
	}
	a.renamedMachine(was, h)
}

// A dial cannot take the name of a machine already connected, and a
// route refused that way holds none of its other names either.
func TestADialIsRefusedTheNameOfAConnectedMachine(t *testing.T) {
	ms := newMachines()
	ms.take(&machine{at: step{name: "edge"}})

	d := &dialling{cancel: func() {}, names: []string{"edge", "db"}}
	if err := ms.holdNames(d); err == nil {
		t.Fatal("a dial took the name of a machine already connected")
	}
	if ms.connecting("edge") != nil {
		t.Error("the name is connected and being connected to at once")
	}
	if ms.connecting("db") != nil {
		t.Error("the rest of a route that was refused is held anyway")
	}
	// And a machine that answers under a name already connected is
	// refused the same way, rather than replacing the connection there.
	if err := ms.answered(d, &machine{at: step{name: "edge"}}); err == nil {
		t.Fatal("a second connection was held under a name already connected")
	}
}

// A rename onto a name a dial is holding is refused by the type as well
// as by the dialog, so the two sides of the invariant cannot meet.
func TestARenameOntoAConnectingNameIsRefusedByTheType(t *testing.T) {
	ms := newMachines()
	ms.take(&machine{at: step{name: "picard"}})
	d := &dialling{cancel: func() {}, names: []string{"slow"}}
	if err := ms.holdNames(d); err != nil {
		t.Fatalf("hold the name: %v", err)
	}

	if _, err := ms.rename("picard", remote.Host{Name: "slow"}); err == nil {
		t.Fatal("a connection was renamed onto a name being connected to")
	}
	if ms.named("picard") == nil || ms.named("slow") != nil {
		t.Errorf("the refusal moved something: %v", ms.names())
	}
}

// A machine that answers stops counting as one being connected to, in
// the same step: that is where a name crosses from one side of the
// invariant to the other.
func TestAMachineThatAnswersLetsGoOfTheNameItWasReachedUnder(t *testing.T) {
	ms := newMachines()
	mine := &dialling{cancel: func() {}, names: []string{"edge"}}
	if err := ms.holdNames(mine); err != nil {
		t.Fatalf("holding the name: %v", err)
	}

	m := &machine{at: step{name: "edge"}}
	if err := ms.answered(mine, m); err != nil {
		t.Fatalf("the machine that answered was not held: %v", err)
	}
	if ms.named("edge") != m {
		t.Fatal("the machine that answered is not held under its name")
	}
	if ms.connecting("edge") != nil {
		t.Fatal("the name is connected and being connected to at once")
	}

	// A machine that answers under a name another attempt has taken is
	// refused, so that attempt still owns it alone.
	theirs := &dialling{cancel: func() {}, names: []string{"db"}}
	if err := ms.holdNames(theirs); err != nil {
		t.Fatalf("holding the name: %v", err)
	}
	if err := ms.answered(mine, &machine{at: step{name: "db"}}); err == nil {
		t.Fatal("a machine was held under a name another attempt owns")
	}
	if ms.named("db") != nil {
		t.Fatal("it was held anyway")
	}
	if ms.connecting("db") != theirs {
		t.Fatal("the attempt that owns the name lost it")
	}
}

// A second attempt on a saved machine still connecting asks the user,
// and whichever of the three they choose the name is left held by one
// thing.
func TestASecondAttemptOnASavedMachineAsks(t *testing.T) {
	for _, tc := range []struct {
		button string
		then   func(t *testing.T, a *testApp, first *dialling)
	}{
		{button: "Leave it", then: func(t *testing.T, a *testApp, first *dialling) {
			if a.machines.connecting("slow") != first {
				t.Error("leaving it alone did not leave the first attempt holding the name")
			}
			if len(first.waiting) != 0 {
				t.Errorf("%d requests are queued behind it, and leaving it queues none",
					len(first.waiting))
			}
			if n := a.machines.beingMade(); n != 1 {
				t.Errorf("%d connections are being made, want the first one only", n)
			}
		}},
		{button: "Wait for it", then: func(t *testing.T, a *testApp, first *dialling) {
			if a.machines.connecting("slow") != first {
				t.Error("waiting for it did not leave the first attempt holding the name")
			}
			// That the request really runs when the first connection
			// lands is TestWaitingForTheOneOnItsWayRunsTheRequestAgain,
			// against a machine that answers. This one never does.
			if len(first.waiting) != 1 {
				t.Errorf("%d requests are waiting for it, want the second one", len(first.waiting))
			}
		}},
		{button: "Give up on that one", then: func(t *testing.T, a *testApp, first *dialling) {
			waitFor(t, a, "the second attempt to take the name", func() bool {
				d := a.machines.connecting("slow")
				return d != nil && d != first
			})
		}},
	} {
		t.Run(tc.button, func(t *testing.T) {
			deafHost, deafPort := sshtest.Deaf(t)
			a := newTestApp(t, 80, 24)
			withDialogs(t, a)
			// The keys and the host key of a machine that will never be
			// reached, so nothing here reads the developer's ~/.ssh.
			pinServers(t, a, sshtest.New(t))
			if err := a.book.Put(remote.Host{
				Name: "slow", Address: deafHost, Port: deafPort, User: "tester",
			}, ""); err != nil {
				t.Fatalf("save the machine: %v", err)
			}
			a.refreshServers()
			connect := openPrefix + remote.CommandName("slow")

			runFromPalette(t, a, connect)
			first := a.machines.connecting("slow")
			if first == nil {
				t.Fatal("nothing is on its way to the deaf machine, so this proves nothing")
			}

			runFromPalette(t, a, connect)
			f := awaitModal(t, a, "the Already connecting to slow dialog", byTitle[*ui.Form]("Already connecting to slow"))
			pressButton(t, a, f, tc.button)

			tc.then(t, a, first)
			heldAs(t, a, "slow", isConnecting)
		})
	}
}
