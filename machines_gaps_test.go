package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/internal/sshtest"
	"github.com/marrasen/gridterm/meter"
	"github.com/marrasen/gridterm/remote"
	"github.com/marrasen/gridterm/ui"
)

// waitFor runs the pump until something is true, or gives up loudly.
func waitFor(t *testing.T, a *testApp, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(waitBudget)
	for time.Now().Before(deadline) {
		a.pump.run()
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// serverRow returns the panel row that stands for a machine's connection.
func serverRow(t *testing.T, a *testApp, host string) *conns.Entry {
	t.Helper()
	for _, group := range a.registry.Groups(time.Now()) {
		for _, row := range group.Rows {
			if row.Kind == conns.Server && row.Host == host {
				return row.Entry
			}
		}
	}
	t.Fatalf("no row for the connection to %s: %v", host, panelText(a, time.Now()))
	return nil
}

// A command that fails has to say so in the window. A window opened from
// an icon has no console, so an error nobody shows is a command that did
// nothing at all as far as the user can tell.
func TestACommandThatFailsSaysSo(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)

	// Running a command needs a machine to run it on, and the local pane
	// has the keys, so this one refuses.
	if err := a.root.Commands.Run("conn.command"); err != nil {
		t.Fatalf("the command reported a failure to nobody: %v", err)
	}
	f, ok := a.root.Modal().(*ui.Form)
	if !ok {
		t.Fatalf("nothing was shown: %T", a.root.Modal())
	}
	if f.Title != "Run a command…" {
		t.Errorf("the dialog is titled %q, want the command's own name", f.Title)
	}
	if strings.TrimSpace(strings.Join(f.Lines, "")) == "" {
		t.Fatal("the dialog says nothing about what went wrong")
	}
}

// A connection the far end drops stops saying it is connected.
//
// Nothing else would ever notice: the window does not poll, and the
// connection was never closed from here.
func TestAConnectionThatDropsStopsSayingItIsConnected(t *testing.T) {
	s := sshtest.New(t)
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)

	a.connect(serverConfig(t, s))
	waitForPanes(t, a, 2)
	host := serverConfig(t, s).Target()
	row := serverRow(t, a, host)
	if got := row.State(time.Now()); got != meter.Opened {
		t.Fatalf("the row is %v before anything happens, want opened", got)
	}

	// The network goes away.
	s.CloseClients()

	waitFor(t, a, "the connection to be given up on", func() bool {
		return a.machines[host] == nil
	})
	if got := row.State(time.Now()); got != meter.Closed {
		t.Fatalf("the row is %v after the connection dropped, want closed", got)
	}
	if row.Note != "" {
		t.Errorf("the row still says %q", row.Note)
	}
	// And it can be taken off the panel, which only works for a row that
	// says it has finished.
	if n := a.registry.DropFinished(time.Now()); n == 0 {
		t.Fatal("the dead connection could not be cleared")
	}
}

// Two logins on one machine are two connections. A terminal opened on
// the second must not land on the first: it is a different account, and
// nothing on the panel would say so.
func TestTwoLoginsOnOneMachineAreTwoConnections(t *testing.T) {
	one := sshtest.New(t)
	two := sshtest.New(t)
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)

	first := serverConfig(t, one)
	second := serverConfig(t, two)
	// The same address, two accounts, so only the name tells them apart.
	second.Host = first.Host
	a.connect(first)
	waitForPanes(t, a, 2)
	a.connect(second)
	waitForPanes(t, a, 3)

	if len(a.machines) != 2 {
		t.Fatalf("the window holds %v, want both logins", names(a))
	}
	if n := two.Conns(); n != 1 {
		t.Fatalf("the second machine saw %d logins, want 1", n)
	}
}

// A name that is already a connection to somewhere else is refused
// rather than quietly meaning the machine that got there first.
func TestANameCannotMeanTwoMachines(t *testing.T) {
	one := sshtest.New(t)
	two := sshtest.New(t)
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)

	a.connectAs("box", serverConfig(t, one))
	waitForPanes(t, a, 2)

	a.connectAs("box", serverConfig(t, two))
	f := waitForDialog(t, a, "Could not connect to box")
	if !strings.Contains(strings.Join(f.Lines, " "), "close it first") {
		t.Fatalf("the refusal says %q", strings.Join(f.Lines, " "))
	}
	if n := two.Conns(); n != 0 {
		t.Fatalf("the other machine saw %d logins, want none", n)
	}
	if len(a.panes) != 2 {
		t.Fatalf("%d panes, want the local one and the first connection", len(a.panes))
	}
}

// A route that runs past a machine already connected to starts from that
// machine. Connecting to it a second time would leave the window holding
// the second connection and closing neither.
func TestARouteStartsFromTheMachineAlreadyConnectedTo(t *testing.T) {
	edge := sshtest.New(t)
	db := sshtest.New(t)
	app := sshtest.New(t)
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	pinServers(t, a, edge, db, app)

	saveHost(t, a, "edge", edge, "")
	saveHost(t, a, "db", db, "")
	saveHost(t, a, "app", app, "db")

	if err := a.connectSaved("db"); err != nil {
		t.Fatalf("connectSaved: %v", err)
	}
	waitForPanes(t, a, 2)

	// db moves behind edge while it is connected to, which is what
	// editing the Through field does.
	host, port := db.Host()
	if err := a.book.Put(remote.Host{
		Name: "db", Address: host, Port: port, User: "tester", Via: "edge",
	}, "db"); err != nil {
		t.Fatalf("Put: %v", err)
	}

	if err := a.connectSaved("app"); err != nil {
		t.Fatalf("connectSaved: %v", err)
	}
	waitForPanes(t, a, 3)

	if n := db.Conns(); n != 1 {
		t.Fatalf("db saw %d logins, want the one that was already open", n)
	}
	if n := edge.Conns(); n != 0 {
		t.Fatalf("edge saw %d logins, want none: db was already reachable", n)
	}
	if got := a.machines["app"].conn.Via(); got != a.machines["db"].conn {
		t.Fatalf("app came through %v, want db", got)
	}
	if len(a.machines) != 2 {
		t.Fatalf("the window holds %v, want db and app", names(a))
	}
}

// Closing a connection really closes it, rather than only forgetting
// about it: one nobody holds is one nobody will ever close.
func TestClosingAConnectionReallyClosesIt(t *testing.T) {
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

	if err := a.dropMachine("edge"); err != nil {
		t.Fatalf("dropMachine: %v", err)
	}
	waitFor(t, a, "both machines to go", func() bool {
		return near.Live() == 0 && far.Live() == 0
	})
}

// The panes closed with a machine are the ones running on it, not every
// pane filed under the same name. With -ssh every pane carries the name
// of the machine it runs on, and a connection made from the window can
// carry that name too.
func TestClosingAConnectionLeavesOtherPanesUnderThatNameAlone(t *testing.T) {
	s := sshtest.New(t)
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	host := serverConfig(t, s).Target()

	// A pane of its own on the same machine, the way -ssh makes them.
	a.localHost = host
	if err := a.openTab(); err != nil {
		t.Fatalf("openTab: %v", err)
	}
	a.connect(serverConfig(t, s))
	waitForPanes(t, a, 3)

	if err := a.dropMachine(host); err != nil {
		t.Fatalf("dropMachine: %v", err)
	}
	if len(a.panes) != 2 {
		t.Fatalf("%d panes after closing the connection, want the two that were not on it",
			len(a.panes))
	}
	checkTree(t, a)
}

// A terminal opened while the panel has the keys goes beside the panes,
// not into the panel. A tab strip built around the connections list puts
// the list in a tab, where hiding the panel hides a terminal with it.
func TestOpeningATerminalFromThePanelDoesNotSwallowIt(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	if err := a.focusPanel(); err != nil {
		t.Fatalf("focusPanel: %v", err)
	}

	if err := a.root.Commands.Run("conn.terminal"); err != nil {
		t.Fatalf("conn.terminal: %v", err)
	}
	if len(a.panes) != 2 {
		t.Fatalf("%d panes, want the new one", len(a.panes))
	}
	if got := a.dock.Panel(); got != ui.Widget(a.side) {
		t.Fatalf("the panel is now %T", got)
	}
	for _, leaf := range ui.Leaves(a.dock.Rest()) {
		if leaf == ui.Widget(a.side) {
			t.Fatal("the connections list ended up among the panes")
		}
	}
	checkTree(t, a)
}

// The window goes with the last pane, panel or no panel. The panel is a
// leaf of the tree too, so the tree is not empty when the last shell has
// gone.
func TestClosingTheLastPaneQuitsWithAPanel(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)

	if err := a.closePane(onlyPaneWidget(t, a)); err != nil {
		t.Fatalf("closePane: %v", err)
	}
	if !a.quit.Load() {
		t.Fatal("the window is still open with no panes in it")
	}
}

// Enter on a connection's own row goes to something running on it.
func TestTheServerRowRevealsAPaneOnThatMachine(t *testing.T) {
	s := sshtest.New(t)
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)

	a.connect(serverConfig(t, s))
	waitForPanes(t, a, 2)
	host := serverConfig(t, s).Target()
	row := serverRow(t, a, host)

	// Somewhere else first, so revealing has something to undo.
	a.focus(onlyLocalPane(t, a))
	if row.Reveal == nil {
		t.Fatal("the connection's row cannot be revealed")
	}
	row.Reveal()

	t2 := a.focusedTerminal()
	if t2 == nil || a.paneOn[t2] == nil || a.paneOn[t2] != a.machines[host] {
		t.Fatal("the keys did not land on a pane running on that machine")
	}
}

// A window that closes ends every connection it was holding, and says
// what went wrong once rather than once per machine: a machine reached
// through another is closed by that one.
func TestClosingTheWindowEndsEveryConnection(t *testing.T) {
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

	if err := a.closeMachines(); err != nil {
		t.Fatalf("closeMachines: %v", err)
	}
	if len(a.machines) != 0 {
		t.Fatalf("the window still holds %v", names(a))
	}
	waitFor(t, a, "both machines to go", func() bool {
		return near.Live() == 0 && far.Live() == 0
	})
}

// A connection that lands after it stopped being wanted is no use: the
// user gave up on it, or the machine it was reached through has closed.
func TestAConnectionThatIsNoLongerWanted(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)

	if err := a.stillWanted(nil, nil); err != nil {
		t.Fatalf("a plain connection was refused: %v", err)
	}

	if err := a.stillWanted(context.Canceled, nil); err == nil {
		t.Fatal("a connection the user gave up on was kept")
	}

	// A carrier the window is no longer holding, which is what closing
	// the machine in the middle mid-dial leaves behind.
	gone := &machine{at: step{name: "edge"}}
	err := a.stillWanted(nil, gone)
	if err == nil {
		t.Fatal("a hop was kept through a machine that had closed")
	}
	if !strings.Contains(err.Error(), "edge") {
		t.Fatalf("error = %v, want it to name the machine that closed", err)
	}
	// Held again, with no connection under it: nothing here looks at one,
	// and the window closing would try to close it.
	a.machines["edge"] = gone
	defer delete(a.machines, "edge")
	if err := a.stillWanted(nil, gone); err != nil {
		t.Fatalf("a hop through a machine still held was refused: %v", err)
	}
}

// onlyPaneWidget returns the app's single terminal as a widget.
func onlyPaneWidget(t *testing.T, a *testApp) ui.Widget {
	t.Helper()
	if len(a.panes) != 1 {
		t.Fatalf("%d panes, want 1", len(a.panes))
	}
	for pane := range a.panes {
		return pane
	}
	return nil
}

// onlyLocalPane returns the pane that is not running on any machine.
func onlyLocalPane(t *testing.T, a *testApp) ui.Widget {
	t.Helper()
	for pane := range a.panes {
		if a.paneOn[pane] == nil {
			return pane
		}
	}
	t.Fatal("every pane is running on a machine")
	return nil
}

// waitForBoth waits for something while keeping two windows running.
//
// Both, because a window taken over answers its client from the
// goroutine that draws: a test that pumped only one of them would wait
// for an answer the other was never going to give.
func waitForBoth(t *testing.T, a, b *testApp, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(waitBudget)
	for time.Now().Before(deadline) {
		a.pump.run()
		b.pump.run()
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}
