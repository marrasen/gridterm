package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/marrasen/gridterm/internal/sshtest"
	"github.com/marrasen/gridterm/remote"
	"github.com/marrasen/gridterm/ui"
	"github.com/marrasen/gridterm/ui/term"
)

// A saved machine can be edited and forgotten from its own plus menu.
//
// It is where they belong: the row is the machine, so the plus on it is
// where everything about that machine is. There was no way to remove a
// server at all.
func TestThePlusOnASavedServerOffersToForgetIt(t *testing.T) {
	a := newTestApp(t, 100, 30)
	withPanel(t, a)
	withDialogs(t, a)
	saveServer(t, a, "edge", "user@edge.example:22")
	a.refreshServers()

	menu := clickPlus(t, a, "edge")
	for _, want := range []string{"server.editThis", "server.forget"} {
		if !offers(menu, want) {
			t.Fatalf("the menu offers %v, with no %s", menuCommands(menu), want)
		}
	}
	dismiss(t, menu)

	// Offered is not enough: both lines are run, because the bug each
	// time was in what the line did, not in whether it was drawn.
	chooseMenuItem(t, clickPlus(t, a, "edge"), "server.editThis")
	edit := awaitModal(t, a, "the Edit edge dialog", byTitle[*ui.Form]("Edit edge"))
	// The default port is left off, so the field says the target as the
	// list holds it rather than as it was typed.
	if got := edit.Field("Server").Text(); got != "user@edge.example" {
		t.Errorf("the edit dialog holds %q, want the machine that was saved", got)
	}
	pressButton(t, a, edit, "Cancel")

	chooseMenuItem(t, clickPlus(t, a, "edge"), "server.forget")
	gone := awaitModal(t, a, "the Remove edge? dialog", byTitle[*ui.Form]("Remove edge?"))
	pressButton(t, a, gone, "Remove")

	if _, ok := a.book.Lookup("edge"); ok {
		t.Fatal("the machine is still in the server list")
	}
}

// A machine that is not in the list has nothing to forget, and says so
// rather than doing nothing.
func TestForgettingAMachineThatIsNotSavedSaysSo(t *testing.T) {
	a := newTestApp(t, 100, 30)
	withPanel(t, a)
	a.hostMenus.nowAbout("somewhere")

	err := a.forgetThisServer()

	if err == nil || !strings.Contains(err.Error(), "not in the server list") {
		t.Fatalf("forgetting an unsaved machine returned %v", err)
	}
}

// Closing a connection that is still being made gives up on it.
//
// It did nothing at all: a machine only reaches the list of what is
// open once it is open, so closing one that had not arrived found
// nothing, reported success and left it hanging.
func TestClosingAConnectionStillOnItsWayGivesUp(t *testing.T) {
	a := newTestApp(t, 100, 30)
	withPanel(t, a)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	holdTheNames(t, a, &dialling{cancel: cancel, names: []string{"edge"}})
	a.hostMenus.nowAbout("edge")

	if err := a.disconnectHere(); err != nil {
		t.Fatalf("disconnect: %v", err)
	}
	if a.machines.connecting("edge") != nil {
		t.Error("the window is still holding the name, so nothing can try again")
	}

	select {
	case <-ctx.Done():
	case <-time.After(waitBudget):
		t.Fatal("the connection was not given up on")
	}
}

// And a machine that is neither open nor on its way says so, rather
// than reporting success and changing nothing.
func TestClosingNothingSaysSo(t *testing.T) {
	a := newTestApp(t, 100, 30)
	withPanel(t, a)
	a.hostMenus.nowAbout("edge")

	err := a.disconnectHere()

	if err == nil || !strings.Contains(err.Error(), "nothing is connected") {
		t.Fatalf("closing nothing returned %v", err)
	}
}

// A server that says something without asking is shown, and the window
// does not wait for an answer.
//
// It is how a server that signs people in through a browser sends the
// link. The message used to be dropped, so the connection sat there
// with nothing on screen and no way to know why.
func TestAServerMessageIsShownWithoutWaiting(t *testing.T) {
	a := newTestApp(t, 100, 30)
	withPanel(t, a)
	withDialogs(t, a)
	ask := &askUser{app: a.app}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		ask.Notice(ctx, remote.Notice{
			User: "rdp", Host: "marras-skylake:22",
			Instruction: "To authenticate, visit: https://login.example/a/1234",
		})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(waitBudget):
		t.Fatal("showing a message waited for an answer")
	}

	f := awaitModal[*ui.Form](t, a, "a dialog", nil)
	text := strings.Join(f.Lines, "\n")
	for _, want := range []string{"rdp@marras-skylake:22", "https://login.example/a/1234"} {
		if !strings.Contains(text, want) {
			t.Errorf("the message does not say %q:\n%s", want, text)
		}
	}
}

// The message goes when the connection is settled, whichever way it
// went: nobody has to dismiss it.
func TestAServerMessageGoesWhenTheConnectionIsSettled(t *testing.T) {
	a := newTestApp(t, 100, 30)
	withPanel(t, a)
	withDialogs(t, a)
	ask := &askUser{app: a.app}

	ctx, cancel := context.WithCancel(context.Background())
	ask.Notice(ctx, remote.Notice{User: "rdp", Host: "here", Text: "hello"})
	awaitModal[*ui.Form](t, a, "a dialog", nil)

	cancel()

	waitFor(t, a, "the message to go once the connection was settled", func() bool {
		return a.root.Modal() == nil
	})
}

// The "Give up" button on a server's message gives up.
//
// It used to look the machine up by what the server calls itself, which
// is an address, while the window holds the name the user gave it. The
// two never matched, so the one button in front of a user waiting on a
// browser sign-in did nothing at all.
func TestGivingUpOnAServerMessageGivesUp(t *testing.T) {
	a := newTestApp(t, 100, 30)
	withPanel(t, a)
	withDialogs(t, a)

	gave := false
	ask := &askUser{app: a.app, stop: func() { gave = true }}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ask.Notice(ctx, remote.Notice{
		User: "rdp", Host: "10.0.0.5:22",
		Instruction: "To authenticate, visit: https://login.example/a/1234",
	})

	f := awaitModal[*ui.Form](t, a, "a dialog", nil)
	pressButton(t, a, f, "Give up")
	a.pump.run()
	if !gave {
		t.Error("the button gave up on nothing")
	}
}

// A message that arrives for a connection already given up on is not
// shown.
//
// A handshake that was walked away from goes on running, and its dialog
// would otherwise take the screen minutes later for a connection nobody
// is waiting for.
func TestAServerMessageForAConnectionAlreadyGivenUpOnIsNotShown(t *testing.T) {
	a := newTestApp(t, 100, 30)
	withPanel(t, a)
	withDialogs(t, a)
	ask := &askUser{app: a.app}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ask.Notice(ctx, remote.Notice{User: "rdp", Host: "here", Text: "hello"})
	if n := a.pump.pending(); n != 0 {
		t.Errorf("%d pieces of work were posted, want none: the dialog was built anyway", n)
	}

	a.pump.run()
	if f, ok := a.root.Modal().(*ui.Form); ok {
		t.Errorf("a dialog opened anyway: %v", f.Lines)
	}
}

// A question asked for a connection already given up on is not shown.
//
// An abandoned handshake goes on running and can reach the point where
// it wants a password. A dialog for it would take the screen, and the
// keys, for a connection nobody is waiting for.
func TestAQuestionForAConnectionAlreadyGivenUpOnIsNotAsked(t *testing.T) {
	a := newTestApp(t, 100, 30)
	withPanel(t, a)
	withDialogs(t, a)
	ask := &askUser{app: a.app}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ask.Password(ctx, "rdp", "marras-skylake"); err == nil {
		t.Fatal("a password was asked for a connection given up on")
	}
	if n := a.pump.pending(); n != 0 {
		t.Errorf("%d pieces of work were posted, want none: the dialog was built anyway", n)
	}

	a.pump.run()
	if f, ok := a.root.Modal().(*ui.Form); ok {
		t.Errorf("a dialog opened anyway: %v", f.Title)
	}
}

// Forgetting a machine the window is connected to closes the connection.
//
// It used to drop the name and leave the connection open, so the row
// stayed behind under a name nothing saved.
func TestForgettingAConnectedMachineClosesIt(t *testing.T) {
	s := sshtest.New(t)
	a := aSavedMachineConnectedTo(t, s, "edge")

	f := openTheForgetDialog(t, a, "edge")
	dialogSays(t, f, "edge is connected.", "Forgetting it closes that connection.")
	pressButton(t, a, f, "Remove")

	if a.machines.named("edge") != nil {
		t.Error("the connection is still held")
	}
	if _, ok := a.book.Lookup("edge"); ok {
		t.Error("edge is still in the server list")
	}
	waitFor(t, a, "the far end to see the connection close", func() bool { return s.Live() == 0 })
	a.refreshPanel(panelNow)
	if _, ok := panelRow(a, hostKey("edge")); ok {
		t.Errorf("the row stayed behind: %v", panelText(a, panelNow))
	}
}

// The dialog names the machines and the panes that go with a connection
// others are reached through, and Remove takes them all.
//
// The list refuses to forget a machine another saved one is reached
// through, so this is the rider whose Through field was edited away: the
// connection still rides on the machine, and the list no longer says so.
func TestForgettingAJumpHostSaysWhatElseGoesWithIt(t *testing.T) {
	near, far, beyond := sshtest.New(t), sshtest.New(t), sshtest.New(t)
	a := newTestApp(t, 100, 30)
	withPanel(t, a)
	withDialogs(t, a)
	pinServers(t, a, near, far, beyond)
	saveHost(t, a, "edge", near, "")
	saveHost(t, a, "db", far, "edge")
	// Two hops out, because closing the first closes the whole chain
	// and the dialog has to count all of it.
	saveHost(t, a, "deep", beyond, "db")
	a.refreshServers()

	clickTerminalLine(t, a, "deep")
	waitForPanes(t, a, 2)
	for _, name := range []string{"edge", "db", "deep"} {
		if a.machines.named(name) == nil {
			t.Fatalf("it is holding %v, want all three machines", a.machines.names())
		}
	}
	// The Through field cleared, which is what lets the list forget the
	// machine db is still reached through.
	chooseMenuItem(t, clickPlus(t, a, "db"), "server.editThis")
	edit := awaitModal(t, a, "the Edit db dialog", byTitle[*ui.Form]("Edit db"))
	retypeField(t, a, edit, "Through", "")
	pressButton(t, a, edit, "Save")

	f := openTheForgetDialog(t, a, "edge")
	dialogSays(t, f,
		"Forgetting it closes that connection, and everything reached through it: 2 machines, 1 pane.")
	pressButton(t, a, f, "Remove")

	if n := a.machines.count(); n != 0 {
		t.Errorf("it is still holding %v, want none of them", a.machines.names())
	}
	waitFor(t, a, "every far end to see its connection close", func() bool {
		return near.Live() == 0 && far.Live() == 0 && beyond.Live() == 0
	})
}

// Forgetting a gridterm window taken over lets go of it.
func TestForgettingAWindowTakenOverLetsGoOfIt(t *testing.T) {
	host, client, addr, keyFile := aServingWindow(t)
	saveWindowFromTheDialog(t, client, "office", addr, keyFile)
	clickTerminalLine(t, client, "office")
	waitFor(t, client, "the window to be taken over", func() bool {
		return client.windows.named("office") != nil
	}, host)

	f := openTheForgetDialog(t, client, "office")
	dialogSays(t, f,
		"This window has taken over office.",
		"Forgetting it lets go of office and closes its panes.")
	pressButton(t, client, f, "Remove")

	if n := client.windows.count(); n != 0 {
		t.Errorf("it is still holding %v", client.windows.names())
	}
	if _, ok := client.book.Lookup("office"); ok {
		t.Error("office is still in the server list")
	}
	waitFor(t, host, "the serving window to see its client go", func() bool {
		return len(host.serving.clients()) == 0
	}, client)
}

// Forgetting a machine still being connected to gives up on the dial.
//
// The machine here accepts and then says nothing at all, so the dial is
// genuinely still running when Remove is pressed.
func TestForgettingAMachineStillOnItsWayGivesUp(t *testing.T) {
	host, port := sshtest.Deaf(t)
	a := newTestApp(t, 100, 30)
	withPanel(t, a)
	withDialogs(t, a)
	if err := a.book.Put(remote.Host{Name: "edge", Address: host, Port: port, User: "tester"}, ""); err != nil {
		t.Fatalf("save edge: %v", err)
	}
	a.refreshServers()

	clickTerminalLine(t, a, "edge")
	waitFor(t, a, "the dial to start", func() bool { return a.machines.connecting("edge") != nil })
	pane := theConnectingPane(t, a, "edge")

	f := openTheForgetDialog(t, a, "edge")
	dialogSays(t, f,
		"The window is still connecting to edge.",
		"Forgetting it gives up on that connection.")
	pressButton(t, a, f, "Remove")

	if a.machines.connecting("edge") != nil {
		t.Error("the window is still holding the name, so nothing can try again")
	}
	if _, ok := a.book.Lookup("edge"); ok {
		t.Error("edge is still in the server list")
	}
	// The pane stays, holding the account of how far it got, and says
	// what became of the dial.
	waitFor(t, a, "the pane to say the dial was given up on", func() bool {
		return a.machines.beingMade() == 0 && a.panes[pane] != nil &&
			a.panes[pane].Label == "given up on"
	})
}

// Trouble closing is reported on its own, and the dialog goes.
//
// Leaving it open would leave a Remove button that can only say there is
// no saved server by that name: the list let go of it before the close
// was tried.
func TestForgettingReportsTroubleClosingOnItsOwn(t *testing.T) {
	s := sshtest.New(t)
	a := aSavedMachineConnectedTo(t, s, "edge")
	// A pane taken out of the tree behind the window's back, which is
	// the one thing closing a connection reports and carries on from.
	takeOutOfTheTree(t, a, a.machines.panesOn(a.machines.named("edge"))[0])

	pressButton(t, a, openTheForgetDialog(t, a, "edge"), "Remove")

	awaitModal(t, a, "the notice about the close", byTitle[*ui.Notice]("Trouble closing edge"))
	if _, ok := a.book.Lookup("edge"); ok {
		t.Error("edge is still in the server list")
	}
	if a.machines.named("edge") != nil {
		t.Error("the connection is still held")
	}
}

// Keeping the machine keeps the connection with it.
func TestKeepingAServerClosesNothing(t *testing.T) {
	s := sshtest.New(t)
	a := aSavedMachineConnectedTo(t, s, "edge")

	pressButton(t, a, openTheForgetDialog(t, a, "edge"), "Keep it")

	if a.machines.named("edge") == nil {
		t.Error("the connection was closed by the button that keeps the machine")
	}
	if _, ok := a.book.Lookup("edge"); !ok {
		t.Error("edge went from the server list anyway")
	}
	if n := s.Live(); n != 1 {
		t.Errorf("the far end has %d connections, want the one that was kept", n)
	}
}

// A machine that cannot be forgotten keeps its connection.
//
// The list refuses to drop a machine another saved one is reached
// through. Closing before that answer is known would take away a
// connection for a forget that then does not happen.
func TestAServerThatCannotBeForgottenKeepsItsConnection(t *testing.T) {
	s := sshtest.New(t)
	a := aSavedMachineConnectedTo(t, s, "edge")
	saveHost(t, a, "db", s, "edge")
	a.refreshServers()

	f := openTheForgetDialog(t, a, "edge")
	pressButton(t, a, f, "Remove")

	if f.Error() == nil {
		t.Fatal("nothing said why edge could not be forgotten")
	}
	if got := f.Error().Error(); !strings.Contains(got, "is reached through") {
		t.Errorf("the dialog says %q, want the machine that is reached through edge", got)
	}
	if _, ok := a.book.Lookup("edge"); !ok {
		t.Error("edge went from the server list anyway")
	}
	if a.machines.named("edge") == nil {
		t.Error("the connection was closed for a forget that did not happen")
	}
	if n := s.Live(); n != 1 {
		t.Errorf("the far end has %d connections, want the one that was kept", n)
	}
}

// A machine nothing is connected to is forgotten with no word about
// closing anything.
func TestForgettingAnUnconnectedServerSaysOnlyThatItIsForgotten(t *testing.T) {
	a := newTestApp(t, 100, 30)
	withPanel(t, a)
	withDialogs(t, a)
	saveServer(t, a, "edge", "user@edge.example:22")
	a.refreshServers()

	f := openTheForgetDialog(t, a, "edge")

	body := strings.Join(f.Lines, "\n")
	if !strings.Contains(body, "It is only forgotten here.") {
		t.Errorf("the dialog says %q", body)
	}
	if strings.Contains(body, "connect") || strings.Contains(body, "closes") {
		t.Errorf("the dialog talks about a connection there is none of: %q", body)
	}
}

// aSavedMachineConnectedTo saves a test server in the list and connects
// to it from the plus on its row.
func aSavedMachineConnectedTo(t *testing.T, s *sshtest.Server, name string) *testApp {
	t.Helper()
	a := newTestApp(t, 100, 30)
	withPanel(t, a)
	withDialogs(t, a)
	pinServers(t, a, s)
	saveHost(t, a, name, s, "")
	a.refreshServers()

	clickTerminalLine(t, a, name)
	waitForPanes(t, a, 2)
	if a.machines.named(name) == nil {
		t.Fatalf("nothing is connected to %s", name)
	}
	return a
}

// theConnectingPane is the pane watching a machine being connected to.
func theConnectingPane(t *testing.T, a *testApp, host string) *term.Terminal {
	t.Helper()
	for pane, e := range a.panes {
		if e.Host == host {
			return pane
		}
	}
	t.Fatalf("no pane is watching %s being connected to", host)
	return nil
}

// takeOutOfTheTree detaches a pane without telling the window, so that
// closing it afterwards fails.
func takeOutOfTheTree(t *testing.T, a *testApp, pane *term.Terminal) {
	t.Helper()
	root, detached := ui.Detach(a.root.Widget(), pane)
	if !detached {
		t.Fatal("the pane was not in the tree to start with")
	}
	a.root.SetWidget(root)
}

// openTheForgetDialog runs the forget line on a machine's plus menu and
// hands back the dialog it opens, without pressing anything.
func openTheForgetDialog(t *testing.T, a *testApp, name string) *ui.Form {
	t.Helper()
	chooseMenuItem(t, clickPlus(t, a, name), "server.forget")
	return awaitModal(t, a, "the Remove "+name+"? dialog", byTitle[*ui.Form]("Remove "+name+"?"))
}

// dialogSays checks a dialog's body says each of these, word for word.
func dialogSays(t *testing.T, f *ui.Form, want ...string) {
	t.Helper()
	body := strings.Join(f.Lines, "\n")
	for _, line := range want {
		if !strings.Contains(body, line) {
			t.Errorf("the dialog does not say %q:\n%s", line, body)
		}
	}
}
