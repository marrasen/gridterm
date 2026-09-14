package main

import (
	"errors"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/internal/sshtest"
	"github.com/marrasen/gridterm/meter"
	"github.com/marrasen/gridterm/remote"
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
	if a.machines[host] == nil {
		t.Fatalf("the connection was not kept: %v", names(a))
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
	if a.machines["edge"] == nil || a.machines["db"] == nil {
		t.Fatalf("the window holds %v, want both machines", names(a))
	}
	if got := a.machines["db"].conn.Via(); got != a.machines["edge"].conn {
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
	if len(a.machines) != 0 {
		t.Fatalf("the window still holds %v", names(a))
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
	if len(a.machines) != 0 {
		t.Fatalf("the window still holds %v", names(a))
	}
	if len(a.panes) != 1 {
		t.Fatalf("%d panes after closing the connection, want the local one", len(a.panes))
	}
}

// Two connections to one machine at once would leave the window holding
// the second and closing neither.
func TestOneMachineIsOnlyConnectedToOnce(t *testing.T) {
	deafHost, deafPort := sshtest.Deaf(t)
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)

	cfg := serverConfig(t, sshtest.New(t))
	cfg.Host, cfg.Port = deafHost, deafPort
	a.connectAs("slow", cfg)
	if a.connecting != 1 {
		t.Fatalf("%d connections are being made, want 1", a.connecting)
	}

	a.connectAs("slow", cfg)
	if a.connecting != 1 {
		t.Fatalf("%d connections are being made, want the first one only", a.connecting)
	}
	f := waitForDialog(t, a, "Could not connect to slow")
	if !strings.Contains(strings.Join(f.Lines, " "), "already connecting") {
		t.Fatalf("the refusal says %q", strings.Join(f.Lines, " "))
	}
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
	waitForDialog(t, a, "Could not connect to box")
	if a.opening["box"] {
		t.Fatal("the machine is still marked as being connected to")
	}
	if len(a.machines) != 0 {
		t.Fatalf("a failed connection was kept: %v", names(a))
	}
}

// Half a route is a set of connections nothing knows about and nothing
// would ever close, so a failure part way closes what it had opened.
func TestAFailedRouteLeavesNothingOpen(t *testing.T) {
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
	f := waitForDialog(t, a, "Could not connect to db")
	if !strings.Contains(strings.Join(f.Lines, " "), "db") {
		t.Fatalf("the failure does not say which machine refused: %q", f.Lines)
	}
	if len(a.machines) != 0 {
		t.Fatalf("the first hop was left open: %v", names(a))
	}
	// And it was really closed, not merely forgotten about: a connection
	// nothing holds is one nothing will ever close.
	for deadline := time.Now().Add(waitBudget); near.Live() > 0; {
		if time.Now().After(deadline) {
			t.Fatalf("the machine in the way still has %d connections open", near.Live())
		}
		time.Sleep(time.Millisecond)
	}
	if len(a.panes) != 1 {
		t.Fatalf("%d panes after a failed route, want the one that was there", len(a.panes))
	}
	if a.opening["edge"] || a.opening["db"] {
		t.Fatal("a machine was left marked as being connected to")
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
	if a.connecting != 0 {
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
	f := waitForDialog(t, a, "Run a command on "+host)
	for _, r := range "uname -a" {
		a.root.HandleKey(input1(r))
	}
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
	f := waitForDialog(t, a, "Run a command on "+host)
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

// There is no connection to run a command on when the machine is the one
// gridterm is running on, and saying so beats a dialog that cannot work.
func TestRunACommandRefusesThisMachine(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)

	if err := a.openCommandHere(); err == nil {
		t.Fatal("a command was offered on the machine gridterm is running on")
	}
	if m := a.root.Modal(); m != nil {
		t.Fatalf("a dialog opened anyway: %T", m)
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

// names lists the machines the window is holding, for a failure message.
func names(a *testApp) []string {
	var out []string
	for name := range a.machines {
		out = append(out, name)
	}
	return out
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

// A shell that ends takes its pane with it: there is nothing left to
// read, and the user asked for a shell rather than for what it last
// said.
func TestAShellThatEndsTakesItsPane(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	if err := a.openTab(); err != nil {
		t.Fatalf("openTab: %v", err)
	}
	if len(a.panes) != 2 {
		t.Fatalf("%d panes", len(a.panes))
	}

	a.shells[0].Close()
	waitUntil(t, func() bool {
		a.reapExited()
		return len(a.panes) == 1
	})
	checkTree(t, a)
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
	if err := a.openTab(); err != nil {
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
	shell.Close()

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
	f, ok := a.root.Modal().(*ui.Form)
	if !ok {
		t.Fatalf("the failure showed %T, want a dialog", a.root.Modal())
	}
	if !strings.Contains(strings.Join(f.Lines, " "), boom.Error()) {
		t.Fatalf("the dialog says %q", strings.Join(f.Lines, " "))
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
