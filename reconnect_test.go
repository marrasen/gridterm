package main

import (
	"errors"
	"io"
	"net"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/input"
	"github.com/marrasen/gridterm/internal/sshtest"
	"github.com/marrasen/gridterm/meter"
	"github.com/marrasen/gridterm/remote"
	"github.com/marrasen/gridterm/session"
	"github.com/marrasen/gridterm/ui"
	"github.com/marrasen/gridterm/ui/term"
)

// endedAndSaid reports whether a pane has ended and its program's status
// has landed, so the question on it is the one the user reads rather than
// the one asked before the status arrived.
func endedAndSaid(a *testApp, pane *term.Terminal) bool {
	_, said := pane.Ending()
	return a.Ended(pane) && said
}

// firstPane is the window's one pane.
func firstPane(t *testing.T, a *testApp) *term.Terminal {
	t.Helper()
	pane, ok := onlyPaneWidget(t, a).(*term.Terminal)
	if !ok {
		t.Fatal("the window opened on something that is not a terminal")
	}
	return pane
}

// theQuestion is what a pane is asking, and fails the test when it is
// asking nothing.
func theQuestion(t *testing.T, pane *term.Terminal) string {
	t.Helper()
	asked := pane.Asking()
	if asked == "" {
		t.Fatal("the pane is asking nothing")
	}
	return strings.ToLower(asked)
}

// clickTheAnswer picks one of the question's choices by its label, the
// way the user clicks it.
func clickTheAnswer(t *testing.T, a *testApp, pane *term.Terminal, label string) {
	t.Helper()
	theQuestion(t, pane)
	choices := pane.Choices()
	which := slices.Index(choices, label)
	if which < 0 {
		t.Fatalf("the window offers no %q", label)
	}
	box := pane.Box()
	// The blank column the question keeps at each end of its row.
	const pad = 1
	at := ui.ButtonColsIn(choices, box.Cols, pad)
	if at[which] < 0 {
		t.Fatalf("there is no room for %q on a pane %d columns wide", label, box.Cols)
	}
	if !pressOn(t, a, pane, at[which], box.Rows-1) {
		t.Fatalf("nothing took the press on %q", label)
	}
}

// pressTheAnswer picks the choice Enter picks, which is the default one.
func pressTheAnswer(t *testing.T, a *testApp, pane *term.Terminal) {
	t.Helper()
	theQuestion(t, pane)
	a.focus(pane)
	sendKey(t, a, press(input.KeyEnter, 0))
}

// sayOnPane gives a shell a line and waits for it to reach the pane.
func sayOnPane(t *testing.T, a *testApp, which int, pane *term.Terminal, text string) {
	t.Helper()
	a.shells[which].out <- []byte(text + "\r\n")
	waitFor(t, a, "the shell's line to reach the pane", func() bool {
		return strings.Contains(pane.ReadLines(60).Text, text)
	})
}

// aDroppedConnection is a window whose pane was running on a machine
// that has just gone, the way a server being rebooted goes.
func aDroppedConnection(t *testing.T) (*testApp, *sshtest.Server, string, *term.Terminal) {
	t.Helper()
	a, s, host := aConnectedWindow(t, 80, 24)
	pane := paneFor(a, host)
	if pane == nil {
		t.Fatal("nothing opened on the machine")
	}
	s.CloseClients()
	waitFor(t, a, "the window to see the machine go", func() bool {
		a.reapExited()
		return a.machines.named(host) == nil && endedAndSaid(a, pane)
	})
	return a, s, host, pane
}

// cameBack waits for a pane the user asked to reconnect to be running on
// the machine again.
func cameBack(t *testing.T, a *testApp, host string, pane *term.Terminal) {
	t.Helper()
	waitFor(t, a, "the machine to answer again", func() bool {
		a.reapExited()
		return a.machines.named(host) != nil && a.machines.beingMade() == 0 && !a.Ended(pane)
	})
}

// aPortNothingAnswersOn is a loopback port with nothing listening on it,
// which is what a machine that has not come back up yet looks like.
func aPortNothingAnswersOn(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("take a port: %v", err)
	}
	_, p, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatalf("read the port: %v", err)
	}
	if err := ln.Close(); err != nil {
		t.Fatalf("give the port back: %v", err)
	}
	port, err := strconv.Atoi(p)
	if err != nil {
		t.Fatalf("read the port: %v", err)
	}
	return port
}

// A shell the user typed exit into asks what to do next on its own last
// row, in the words ssh itself prints after an exit.
func TestAShellThatExitsAsksWhatToDoNext(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	pane := firstPane(t, a)

	endTheShell(t, a, 0, pane)

	asked := theQuestion(t, pane)
	if !strings.Contains(asked, "connection closed") {
		t.Errorf("the pane asks %q, want it to say the connection closed", asked)
	}
	if !strings.Contains(asked, "reconnect") {
		t.Errorf("the pane asks %q, want it to offer to reconnect", asked)
	}
}

// A command that finished names itself in the question and its choice
// says it will run, because picking it runs that command a second time
// with whatever it does to the machine.
func TestACommandThatFinishedNamesWhatYesWouldRun(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	if err := a.openPane(); err != nil {
		t.Fatalf("openPane: %v", err)
	}
	pane := a.focusedTerminal()
	if pane == nil {
		t.Fatal("the new tab has no terminal")
	}
	// A second pane standing in for a command, the way the row of one
	// run on a machine is filed, and the argv the question has to name.
	a.panes[pane].Kind = conns.Command
	a.started[pane].argv = []string{"make", "deploy"}

	endTheShell(t, a, 1, pane)

	asked := theQuestion(t, pane)
	if !strings.Contains(asked, "make deploy") {
		t.Errorf("the pane asks %q without naming what it would run", asked)
	}
	if !strings.Contains(asked, "run it again") {
		t.Errorf("the pane asks %q, want it to say the command would run again", asked)
	}
	if strings.Contains(asked, "reconnect") {
		t.Errorf("the pane asks %q, which reads as reconnecting rather than running", asked)
	}
	// And the choice itself says so, because that is what the user reads
	// before they press it.
	if got := pane.Choices()[0]; !strings.Contains(strings.ToLower(got), "run") {
		t.Errorf("the choice is labelled %q, want it to say it runs the command", got)
	}
	clickTheAnswer(t, a, pane, "Run again")
	if got := pane.Asking(); got != "" {
		t.Errorf("the question is still up: %q", got)
	}
}

// shellIsReady is the first thing the test server prints in a shell it
// has opened, which is how a pane says a new shell has reached it.
const shellIsReady = "READY "

// ranDeploy is what the test server prints when it is asked to run the
// command these tests run: the argv, quoted, as it came on the wire.
const ranDeploy = "RAN 'make' 'deploy'"

// commandPane is the one pane the window has open for a command.
func commandPane(t *testing.T, a *testApp) *term.Terminal {
	t.Helper()
	var found *term.Terminal
	for pane, e := range a.panes {
		if e.Kind != conns.Command {
			continue
		}
		if found != nil {
			t.Fatal("the window has more than one command open")
		}
		found = pane
	}
	if found == nil {
		t.Fatal("the window has no command open")
	}
	return found
}

// A command that finished on a machine runs again on the connection that
// is already up, with the last run's output still above it. That is the
// point of the whole question: a deploy that errored and one that did
// not, read side by side in one pane.
func TestACommandRunsAgainOnTheConnectionItHas(t *testing.T) {
	a, s, host := aConnectedWindow(t, 80, 24)
	m := a.machines.named(host)
	if err := a.openOn(host, []string{"make", "deploy"}, nil); err != nil {
		t.Fatalf("running the command: %v", err)
	}
	pane := commandPane(t, a)
	waitFor(t, a, "the command to finish", func() bool {
		a.reapExited()
		return endedAndSaid(a, pane)
	})

	asked := theQuestion(t, pane)
	if !strings.Contains(asked, "make deploy") {
		t.Errorf("the pane asks %q without naming what it would run", asked)
	}
	if strings.Contains(asked, "reconnect") || strings.Contains(asked, "connection") {
		t.Errorf("the pane asks %q, but nothing is being connected: the machine is up", asked)
	}
	if got := pane.Choices()[0]; got != "Run again" {
		t.Errorf("the choice is labelled %q, want it to say it runs the command", got)
	}

	// A second login would offer the keys again, which is how a dial
	// nobody asked for shows up here.
	offered := len(s.Offered())
	clickTheAnswer(t, a, pane, "Run again")

	waitFor(t, a, "the command to run a second time", func() bool {
		a.reapExited()
		return strings.Count(pane.ReadLines(60).Text, ranDeploy) == 2
	})
	if a.machines.named(host) != m {
		t.Error("the window logged in again instead of running on the connection it had")
	}
	if got := len(s.Offered()); got != offered {
		t.Errorf("the client offered %d keys, want the %d it had: it dialled again", got, offered)
	}
	if a.machines.beingMade() != 0 {
		t.Error("the window is dialling, with the machine already connected")
	}
	// Both runs in the one pane, the earlier one above the later.
	read := pane.ReadLines(60).Text
	if strings.Index(read, ranDeploy) == strings.LastIndex(read, ranDeploy) {
		t.Fatalf("the pane holds one run, not two:\n%s", read)
	}
	if !strings.Contains(read, "the program has finished") {
		t.Errorf("the record of the first run going has gone:\n%s", read)
	}
	checkTree(t, a)
}

// A command that ended well says so, because "exit 0" is what tells the
// user this run was not the one that went wrong.
func TestACommandThatWorkedSaysItsStatus(t *testing.T) {
	a, _, host := aConnectedWindow(t, 80, 24)
	if err := a.openOn(host, []string{"make", "deploy"}, nil); err != nil {
		t.Fatalf("running the command: %v", err)
	}
	pane := commandPane(t, a)
	waitFor(t, a, "the command to finish", func() bool {
		a.reapExited()
		return endedAndSaid(a, pane)
	})

	if asked := theQuestion(t, pane); !strings.Contains(asked, "exit 0") {
		t.Errorf("the pane asks %q without saying how the run went", asked)
	}
}

// A command that failed says which status it failed with, which is the
// difference between the run to repeat and the run to read.
func TestACommandThatFailedSaysItsStatus(t *testing.T) {
	a, _, host := aConnectedWindow(t, 80, 24)
	pane := paneFor(a, host)
	if pane == nil {
		t.Fatal("nothing opened on the machine")
	}
	// The test server ends on "bye" with a status of its own, whatever
	// it was asked to run, so the pane is filed as the command it stands
	// in for.
	a.panes[pane].Kind = conns.Command
	a.started[pane].argv = []string{"make", "deploy"}

	endTheRemoteShell(t, a, pane)

	asked := theQuestion(t, pane)
	if !strings.Contains(asked, "exit 7") {
		t.Errorf("the pane asks %q, want it to say the status the run failed with", asked)
	}
	if !strings.Contains(asked, "make deploy") {
		t.Errorf("the pane asks %q without naming what failed", asked)
	}
}

// A shell that exited cleanly says nothing about its status: that is the
// ordinary way out of a shell, and saying so is noise.
func TestAShellThatExitedCleanlySaysNoStatus(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	pane := firstPane(t, a)

	endTheShell(t, a, 0, pane)

	if asked := theQuestion(t, pane); strings.Contains(asked, "exit") {
		t.Errorf("the pane asks %q, want nothing about a status a shell always has", asked)
	}
}

// A shell that died says which status it died with, which is not the
// ordinary way out and is worth knowing about.
func TestAShellThatDiedSaysItsStatus(t *testing.T) {
	a, _, host := aConnectedWindow(t, 80, 24)
	pane := paneFor(a, host)
	if pane == nil {
		t.Fatal("nothing opened on the machine")
	}

	endTheRemoteShell(t, a, pane)

	asked := theQuestion(t, pane)
	if !strings.Contains(asked, "exit 7") {
		t.Errorf("the pane asks %q, want it to say the status the shell died with", asked)
	}
	if !strings.Contains(asked, "reconnect") {
		t.Errorf("the pane asks %q, want it still to offer to reconnect", asked)
	}
}

// A pane whose connection dropped says nothing about a status: there was
// none. Saying it exited cleanly would be the one thing that did not
// happen.
func TestAPaneWhoseConnectionDroppedSaysNoStatus(t *testing.T) {
	_, _, _, pane := aDroppedConnection(t)

	if asked := theQuestion(t, pane); strings.Contains(asked, "exit") {
		t.Errorf("the pane asks %q, but the connection went before any status could", asked)
	}
}

// A pane whose program is still running asks nothing. The question is
// about a program that has gone, and one on a live shell would eat every
// keystroke meant for it.
func TestAPaneWhoseProgramIsRunningAsksNothing(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	first := firstPane(t, a)
	if err := a.openPane(); err != nil {
		t.Fatalf("openPane: %v", err)
	}
	second := a.focusedTerminal()

	if got := first.Asking(); got != "" {
		t.Errorf("a running pane asks %q", got)
	}

	// And it still asks nothing once the pane beside it has ended.
	endTheShell(t, a, 1, second)
	if got := first.Asking(); got != "" {
		t.Errorf("a running pane asks %q once its neighbour ended", got)
	}
}

// A pane whose connection dropped asks about reconnecting, because that
// is what really happened to it: the shell did not exit.
func TestAConnectionThatDropsAsksAboutReconnecting(t *testing.T) {
	_, _, _, pane := aDroppedConnection(t)

	asked := theQuestion(t, pane)
	if !strings.Contains(asked, "connection") {
		t.Errorf("the pane asks %q, want it worded for a connection that closed", asked)
	}
	if !strings.Contains(asked, "reconnect") {
		t.Errorf("the pane asks %q, want it to offer to reconnect", asked)
	}
}

// Picking Close closes the pane, which is the other half of the
// question: the user has read it and is done with it.
func TestPickingCloseClosesThePane(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	first := firstPane(t, a)
	if err := a.openPane(); err != nil {
		t.Fatalf("openPane: %v", err)
	}
	endTheShell(t, a, 0, first)
	// The pane put in front, which is how the user reaches a question on
	// a pane the tab beside it is covering.
	if err := a.revealRow(paneRow(t, a, a.panes[first])); err != nil {
		t.Fatalf("choosing the row: %v", err)
	}

	clickTheAnswer(t, a, first, "Close")

	if a.panes[first] != nil {
		t.Fatal("the pane is still one the window holds")
	}
	if inTheTree(a, first) {
		t.Fatal("the pane is still in the tree")
	}
	checkTree(t, a)
}

// Picking Yes on a pane here starts the same shell again in the same
// pane, with everything the last one printed still above it.
func TestPickingYesHereStartsTheShellAgainInThePane(t *testing.T) {
	a := newTestApp(t, 80, 24, startedWith(startup{command: []string{"a-shell"}}))
	withDialogs(t, a)
	withPanel(t, a)
	pane := firstPane(t, a)
	sayOnPane(t, a, 0, pane, "before it went")
	endTheShell(t, a, 0, pane)

	pressTheAnswer(t, a, pane)

	if a.Ended(pane) {
		t.Fatal("the pane is still one the window has finished with")
	}
	if got := pane.Asking(); got != "" {
		t.Errorf("the question is still up: %q", got)
	}
	if len(a.shells) != 2 {
		t.Fatalf("%d shells were started, want a second one", len(a.shells))
	}
	if got := a.argvs[1]; !slices.Equal(got, a.argvs[0]) {
		t.Errorf("the second shell runs %v, want the %v the first ran", got, a.argvs[0])
	}
	sayOnPane(t, a, 1, pane, "after it came back")

	read := pane.ReadLines(60).Text
	if !strings.Contains(read, "before it went") {
		t.Errorf("the transcript of the shell that went has gone:\n%s", read)
	}
	if strings.Index(read, "before it went") > strings.Index(read, "after it came back") {
		t.Errorf("the new shell is not below the old transcript:\n%s", read)
	}
	checkTree(t, a)
}

// Picking Yes on a pane whose machine is still connected opens another
// shell on that connection rather than logging in again.
func TestPickingYesOnAConnectedMachineOpensAShellOnIt(t *testing.T) {
	a, s, host := aConnectedWindow(t, 80, 24)
	pane := paneFor(a, host)
	if pane == nil {
		t.Fatal("nothing opened on the machine")
	}
	m := a.machines.named(host)
	opened := s.SessionsOpened()

	endTheRemoteShell(t, a, pane)
	if asked := theQuestion(t, pane); !strings.Contains(asked, "reconnect") {
		t.Errorf("the pane asks %q, want it to offer to reconnect", asked)
	}

	pressTheAnswer(t, a, pane)

	waitFor(t, a, "the pane to be running again", func() bool { return !a.Ended(pane) })
	if got := s.SessionsOpened(); got <= opened {
		t.Fatalf("the server has opened %d sessions, want another past the %d it had",
			got, opened)
	}
	if a.machines.named(host) != m {
		t.Error("the window logged in again instead of riding on the connection it had")
	}
	if a.machines.runningOn(pane) != m {
		t.Error("the pane is not recorded as running on that connection")
	}
	checkTree(t, a)
}

// Picking Yes on a pane whose machine has gone dials it again and brings
// it back in the same pane. This is what the question is for: a server
// that was rebooted.
func TestPickingYesOnAMachineThatHasGoneDialsItAgain(t *testing.T) {
	a, _, host, pane := aDroppedConnection(t)

	pressTheAnswer(t, a, pane)
	cameBack(t, a, host, pane)

	if a.machines.runningOn(pane) == nil {
		t.Error("the pane is not recorded as running on the connection it came back on")
	}
	if !inTheTree(a, pane) {
		t.Error("the pane that came back is not the pane the user was looking at")
	}
	if got := len(a.panes); got != 2 {
		t.Errorf("the window has %d panes, want the two it had: a second one opened", got)
	}
	checkTree(t, a)
}

// A pane that came back reads as live on the sidebar: its row is not
// closed, it no longer says the connection was lost, and the speed it
// shows is a speed rather than the wrap of a meter that was swapped
// under a rate remembering the old one's totals.
func TestAPaneThatCameBackReadsAsLive(t *testing.T) {
	a, _, host, pane := aDroppedConnection(t)
	e := a.panes[pane]
	if e == nil {
		t.Fatal("the pane has no row")
	}
	if e.Label != transportLost {
		t.Fatalf("the row says %q, want it to say the connection was lost", e.Label)
	}

	// The row sampled the way the sidebar samples it, twice, while the
	// meter that went still holds everything the connection carried.
	speed := func(when time.Time) uint64 {
		a.refreshPanel(when)
		rate := a.rates[e]
		if rate == nil {
			t.Fatal("the sidebar kept no rate for the row")
		}
		in, out := rate.Sample(e.Meter, when)
		return max(in, out)
	}
	at := panelNow
	speed(at)
	speed(at.Add(meter.RateWindow + meter.RateWindow/2))

	pressTheAnswer(t, a, pane)
	cameBack(t, a, host, pane)

	if got := e.State(time.Now()); got == meter.Closed {
		t.Error("the row still says the program has finished")
	}
	if e.Label == transportLost {
		t.Error("the row still says the connection was lost")
	}
	if e.Note != "" {
		t.Errorf("the row still says %q", e.Note)
	}
	at = at.Add(4 * meter.RateWindow)
	speed(at)
	if got := speed(at.Add(meter.RateWindow + meter.RateWindow/2)); got > 1<<20 {
		t.Errorf("the row shows %s, which is not a speed this pane ever moved at",
			meter.Speed(got))
	}
}

// Yes on a pane whose program cannot be started leaves the question up,
// so the user can answer it again.
func TestYesThatCannotStartLeavesTheQuestionUp(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	pane := firstPane(t, a)
	endTheShell(t, a, 0, pane)
	asked := pane.Asking()

	boom := errors.New("no shell to start")
	a.newShell = func([]string, int, int) (session.Session, error) { return nil, boom }
	a.focus(pane)
	took, err := a.root.HandleKey(press(input.KeyEnter, 0))
	if !took {
		t.Fatal("nothing took Enter on the question")
	}
	if err == nil || !strings.Contains(err.Error(), boom.Error()) {
		t.Fatalf("answering reported %v, want the reason the shell would not start", err)
	}

	if got := pane.Asking(); got != asked {
		t.Errorf("the pane asks %q, want %q still up to answer again", got, asked)
	}
	if !a.Ended(pane) {
		t.Error("the pane reads as running with nothing in it")
	}
}

// Yes on a machine that still cannot be reached leaves the user with the
// question again, so they can wait and try once more.
func TestYesOnAMachineStillOutOfReachAsksAgain(t *testing.T) {
	a, _, _, pane := aDroppedConnection(t)

	// The machine answers on nothing, which is what one still coming up
	// looks like from here.
	a.prepare = func(cfg remote.Config) remote.Config {
		cfg.Port = aPortNothingAnswersOn(t)
		return cfg
	}
	pressTheAnswer(t, a, pane)

	waitFor(t, a, "the window to give up on the machine again", func() bool {
		a.reapExited()
		return a.machines.beingMade() == 0 && a.Ended(pane) && pane.Asking() != ""
	})
	asked := theQuestion(t, pane)
	if !strings.Contains(asked, "reconnect") {
		t.Errorf("the pane asks %q, want it to offer to reconnect again", asked)
	}
	// And no status: there was no program to have one.
	if strings.Contains(asked, "exit") {
		t.Errorf("the pane asks %q about a status the dial never got to", asked)
	}
	if !inTheTree(a, pane) {
		t.Error("the pane went with the connection that was not made")
	}
	checkTree(t, a)
}

// A pane that came back can end again and ask again, which is what makes
// the question worth having on a machine that reboots twice.
func TestAPaneThatCameBackCanEndAndAskAgain(t *testing.T) {
	a, s, host, pane := aDroppedConnection(t)

	pressTheAnswer(t, a, pane)
	cameBack(t, a, host, pane)

	// And it goes a second time.
	s.CloseClients()
	waitFor(t, a, "the window to see the machine go again", func() bool {
		a.reapExited()
		return a.machines.named(host) == nil && endedAndSaid(a, pane)
	})

	if asked := theQuestion(t, pane); !strings.Contains(asked, "reconnect") {
		t.Errorf("the pane asks %q the second time, want it to offer to reconnect", asked)
	}

	// And the second answer works, which is the first one to run against
	// what the first restart wrote down about the pane.
	pressTheAnswer(t, a, pane)
	cameBack(t, a, host, pane)

	if a.machines.runningOn(pane) == nil {
		t.Error("the pane is not recorded as running on the connection it came back on a second time")
	}
	if !inTheTree(a, pane) {
		t.Error("the pane that came back twice is not the pane the user was looking at")
	}
	checkTree(t, a)
}

// A pane drawn from a window taken over asks nothing: its program is the
// other window's, and this one has no way to start it again.
func TestAPaneOnAWindowTakenOverAsksNothing(t *testing.T) {
	host, client, addr, keyFile := aServingWindow(t)
	pane := takeOverFromTheDialog(t, client, addr, keyFile)

	// The window over there opened a shell of its own for this pane, and
	// that shell goes.
	waitFor(t, client, "the window over there to start the shell", func() bool {
		host.shellsMu.Lock()
		defer host.shellsMu.Unlock()
		return len(host.shells) == 2
	}, host)
	if err := host.shells[1].Close(); err != nil {
		t.Fatalf("ending the shell over there: %v", err)
	}
	waitFor(t, client, "the pane to see the program over there go", func() bool {
		client.reapExited()
		return client.Ended(pane)
	}, host)

	if got := pane.Asking(); got != "" {
		t.Errorf("the pane asks %q about a program this window does not own", got)
	}
	if client.panes[pane] == nil {
		t.Error("the pane went with the program over there")
	}
	if got := paneText(pane); !strings.Contains(got, "the program has finished") {
		t.Errorf("the pane says nothing about the program finishing: %q", got)
	}
	// And the line names a way out, because this pane has no question to
	// name one and the sidebar that would can be hidden.
	//
	// The line breaks wherever the pane is wide enough for, so they come
	// out before the line is read: a row that filled has no padding, so
	// taking them out joins the words back up.
	read := strings.ReplaceAll(paneText(pane), "\n", "")
	if !strings.Contains(read, "closes this pane") {
		t.Errorf("the pane names no way out of it: %q", paneText(pane))
	}
}

// A pane that came back keeps everything the last program printed. That
// transcript is the whole point of reconnecting in the same pane, and
// the connection log folding away must not clear it.
func TestAPaneThatCameBackKeepsItsTranscript(t *testing.T) {
	a, _, host, pane := aDroppedConnection(t)
	before := pane.ReadLines(0).Text
	if !strings.Contains(before, "the program has finished") {
		t.Fatalf("the pane never said the program went:\n%s", before)
	}

	pressTheAnswer(t, a, pane)
	cameBack(t, a, host, pane)
	// The account of the new connection folds away as the shell on it
	// opens, and that fold is what would clear the pane. So the wait is
	// for the second shell's own first line, or for the transcript to
	// have gone, rather than timing out when it has.
	waitFor(t, a, "the shell on the new connection to reach the pane", func() bool {
		read := pane.ReadLines(200).Text
		return strings.Count(read, shellIsReady) == 2 ||
			!strings.Contains(read, "the program has finished")
	})

	read := pane.ReadLines(200).Text
	if !strings.Contains(read, "the program has finished") {
		t.Errorf("reconnecting wiped the transcript the pane was kept for:\n%s", read)
	}
}

// A command whose connection went did not finish, and the question says
// so. "Finished" would be the same lie as reporting an exit of zero, in
// words rather than a number.
func TestACommandCutOffDoesNotSayItFinished(t *testing.T) {
	a, s, host := aConnectedWindow(t, 80, 24)
	pane := paneFor(a, host)
	if pane == nil {
		t.Fatal("nothing opened on the machine")
	}
	// The pane filed as the command it stands in for, so the question is
	// worded the way a command's is.
	a.panes[pane].Kind = conns.Command
	a.started[pane].argv = []string{"make", "deploy"}

	s.CloseClients()
	waitFor(t, a, "the window to see the machine go", func() bool {
		a.reapExited()
		return a.machines.named(host) == nil && endedAndSaid(a, pane)
	})

	asked := theQuestion(t, pane)
	if strings.Contains(asked, "finished") {
		t.Errorf("the pane asks %q, but the connection went before the command could finish", asked)
	}
	if !strings.Contains(asked, "connection") {
		t.Errorf("the pane asks %q without saying the connection went", asked)
	}
	if !strings.Contains(asked, "make deploy") {
		t.Errorf("the pane asks %q without naming what was cut off", asked)
	}
	if strings.Contains(asked, "exit") {
		t.Errorf("the pane asks %q about a status a cut-off command never reported", asked)
	}
}

// A command on a machine that has gone dials the machine again and runs
// the command in the same pane, which is the re-dial the question offers
// on every pane that is not a shell.
func TestACommandRunsAgainAfterTheMachineIsDialledBack(t *testing.T) {
	a, s, host := aConnectedWindow(t, 80, 24)
	if err := a.openOn(host, []string{"make", "deploy"}, nil); err != nil {
		t.Fatalf("running the command: %v", err)
	}
	pane := commandPane(t, a)
	waitFor(t, a, "the command to finish", func() bool {
		a.reapExited()
		return endedAndSaid(a, pane)
	})
	// And then the machine goes, so running it again has to dial first.
	s.CloseClients()
	waitFor(t, a, "the window to see the machine go", func() bool {
		a.reapExited()
		return a.machines.named(host) == nil
	})

	clickTheAnswer(t, a, pane, "Run again")

	waitFor(t, a, "the machine to answer and run the command again", func() bool {
		a.reapExited()
		return a.machines.named(host) != nil && a.machines.beingMade() == 0 &&
			strings.Count(pane.ReadLines(200).Text, ranDeploy) == 2
	})
	if !inTheTree(a, pane) {
		t.Error("the command opened somewhere other than the pane that asked")
	}
	if got := a.panes[pane]; got == nil || got.Kind != conns.Command {
		t.Error("the pane is no longer filed as the command it ran")
	}
	checkTree(t, a)
}

// A pane that ends while the user is in another tab is found asking, with
// everything it printed still there. That is the whole user story: run a
// command, go and do something else, come back to it.
func TestAPaneThatEndedInAnotherTabIsFoundAsking(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	first := firstPane(t, a)
	if err := a.openPane(); err != nil {
		t.Fatalf("openPane: %v", err)
	}
	second := a.focusedTerminal()
	sayOnPane(t, a, 0, first, "before it went")

	endTheShell(t, a, 0, first)

	if a.focusedTerminal() != second {
		t.Fatal("a pane ending took the user out of the tab they were in")
	}
	// And the user comes back to it.
	if err := a.revealRow(paneRow(t, a, a.panes[first])); err != nil {
		t.Fatalf("choosing the row: %v", err)
	}
	if asked := theQuestion(t, first); !strings.Contains(asked, "reconnect") {
		t.Errorf("the pane asks %q, want the question still up on the user's return", asked)
	}
	if read := first.ReadLines(60).Text; !strings.Contains(read, "before it went") {
		t.Errorf("the transcript of the shell that went has gone:\n%s", read)
	}
	checkTree(t, a)
}

// Enter on a dead command pane closes it rather than running the command.
// Pressing Enter at a terminal that has stopped answering is a reflex,
// and running a deploy against a live machine is not something to do by
// reflex.
func TestEnterOnACommandPaneDoesNotRunItAgain(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	if err := a.openPane(); err != nil {
		t.Fatalf("openPane: %v", err)
	}
	pane := a.focusedTerminal()
	a.panes[pane].Kind = conns.Command
	a.started[pane].argv = []string{"make", "deploy"}
	endTheShell(t, a, 1, pane)
	shells := len(a.shells)

	pressTheAnswer(t, a, pane)

	if a.panes[pane] != nil {
		t.Error("Enter left the pane open, so it answered something other than Close")
	}
	if got := len(a.shells); got != shells {
		t.Errorf("%d shells were started, want the %d there were: Enter ran the command", got, shells)
	}
	checkTree(t, a)
}

// A pane too narrow to show every answer answers with one it showed. On a
// pane with room for Close alone, Enter must not run the choice beside it
// that was never drawn.
func TestANarrowPaneAnswersWithAChoiceItShowed(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	pane := firstPane(t, a)
	endTheShell(t, a, 0, pane)

	// Narrow enough for Close and no more, which is the case the
	// selection starting on the first choice gets wrong.
	pane.Layout(ui.Size{Cols: 12, Rows: 24})
	at := ui.ButtonColsIn(pane.Choices(), pane.Box().Cols, 1)
	if at[0] >= 0 || at[1] < 0 {
		t.Fatalf("the choices land at %v, want room for the second one alone", at)
	}
	shells := len(a.shells)

	pressTheAnswer(t, a, pane)

	if got := len(a.shells); got != shells {
		t.Errorf("%d shells were started, want the %d there were: Enter ran a choice"+
			" the pane had no room to show", got, shells)
	}
	if a.panes[pane] != nil {
		t.Error("Enter answered neither choice, leaving the pane with no way out")
	}
}

// A command too long for the row is cut with an ellipsis, so the question
// still reads as a question: the cut is marked and the question mark is
// still on the end.
func TestALongCommandIsCutWithTheQuestionLeftOnTheEnd(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	pane := firstPane(t, a)
	a.panes[pane].Kind = conns.Command
	a.started[pane].argv = []string{"make", strings.Repeat("deploy-", 30)}

	endTheShell(t, a, 0, pane)

	asked := pane.Asking()
	if !strings.HasSuffix(asked, "Run it again?") {
		t.Errorf("the pane asks %q, want it still to end in the question", asked)
	}
	if !strings.Contains(asked, grid.Ellipsis) {
		t.Errorf("the pane asks %q, want the cut marked", asked)
	}
}

// killedBySignal is a process state saying a program was killed rather
// than exiting, which is what exec reports through Sys.
type killedBySignal struct{ sig syscall.Signal }

func (k killedBySignal) Signaled() bool         { return true }
func (k killedBySignal) Signal() syscall.Signal { return k.sig }

// A shell on this machine killed by a signal reads the way every shell
// reports one: 128 plus the signal. exec calls that an exit code of -1,
// which is not a status anything reports.
func TestAShellKilledBySignalReadsAsTheShellsReportIt(t *testing.T) {
	if got, known := localStatus(-1, killedBySignal{sig: 9}); !known || got != 137 {
		t.Errorf("a shell killed by signal 9 reads as exit %d (known %v), want exit 137",
			got, known)
	}
	if got, known := localStatus(-1, nil); known {
		t.Errorf("a code of -1 with no signal to name reads as exit %d;"+
			" -1 is not a status any shell reports", got)
	}
	if got, known := localStatus(7, nil); !known || got != 7 {
		t.Errorf("a shell that exited 7 reads as exit %d (known %v)", got, known)
	}
}

// heldSession is a program that stops printing before it says how it
// ended, which is what a pane whose input broke under a program still
// running looks like.
type heldSession struct {
	out  chan []byte
	told chan struct{}

	// status is what the program ended with, handed over by saysItEnded
	// and read only after told is closed.
	status error

	quiet sync.Once
}

func newHeldSession() *heldSession {
	return &heldSession{out: make(chan []byte, 4), told: make(chan struct{})}
}

func (h *heldSession) Read(p []byte) (int, error) {
	b, ok := <-h.out
	if !ok {
		return 0, io.EOF
	}
	return copy(p, b), nil
}

func (h *heldSession) Write(p []byte) (int, error) { return len(p), nil }
func (h *heldSession) Resize(int, int) error       { return nil }
func (h *heldSession) Close() error                { h.stopsPrinting(); return nil }

func (h *heldSession) Wait() error {
	<-h.told
	return h.status
}

// stopsPrinting ends the program's output with its status still to come.
func (h *heldSession) stopsPrinting() { h.quiet.Do(func() { close(h.out) }) }

// saysItEnded hands over the status the program ended with.
func (h *heldSession) saysItEnded(status error) {
	h.status = status
	close(h.told)
}

// A status that lands after the program went still reaches the question,
// because saying how the run went is the whole reason a command asks.
func TestAStatusThatLandsLateStillReachesTheQuestion(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	held := newHeldSession()
	a.newShell = func([]string, int, int) (session.Session, error) { return held, nil }
	if err := a.openPane(); err != nil {
		t.Fatalf("openPane: %v", err)
	}
	pane := a.focusedTerminal()
	a.panes[pane].Kind = conns.Command
	a.started[pane].argv = []string{"make", "deploy"}

	held.stopsPrinting()
	waitFor(t, a, "the window to see the program go", func() bool {
		a.reapExited()
		return a.Ended(pane)
	})
	asked := theQuestion(t, pane)
	if strings.Contains(asked, "exit") {
		t.Errorf("the pane asks %q about a status that has not arrived", asked)
	}
	if strings.Contains(asked, "finished") {
		t.Errorf("the pane asks %q, with nothing yet to say the run finished", asked)
	}

	held.saysItEnded(nil)

	waitFor(t, a, "the status to reach the question", func() bool {
		a.reapExited()
		return strings.Contains(strings.ToLower(pane.Asking()), "exit 0")
	})
	if asked := theQuestion(t, pane); !strings.Contains(asked, "make deploy") {
		t.Errorf("the pane asks %q, want it still to name what it would run", asked)
	}
}

// A command whose machine will not answer never ran, and the question
// says so. "Finished" would be a lie about a run that never started.
func TestACommandWhoseRedialFailedSaysNothingRan(t *testing.T) {
	a, s, host := aConnectedWindow(t, 80, 24)
	if err := a.openOn(host, []string{"make", "deploy"}, nil); err != nil {
		t.Fatalf("running the command: %v", err)
	}
	pane := commandPane(t, a)
	waitFor(t, a, "the command to finish", func() bool {
		a.reapExited()
		return endedAndSaid(a, pane)
	})
	s.CloseClients()
	waitFor(t, a, "the window to see the machine go", func() bool {
		a.reapExited()
		return a.machines.named(host) == nil
	})

	// The machine answers on nothing, which is what one still coming up
	// looks like from here.
	a.prepare = func(cfg remote.Config) remote.Config {
		cfg.Port = aPortNothingAnswersOn(t)
		return cfg
	}
	clickTheAnswer(t, a, pane, "Run again")

	waitFor(t, a, "the pane to ask about the dial that failed", func() bool {
		a.reapExited()
		return a.machines.beingMade() == 0 && endedAndSaid(a, pane) &&
			strings.Contains(strings.ToLower(pane.Asking()), "did not run")
	})
	asked := theQuestion(t, pane)
	if strings.Contains(asked, "finished") {
		t.Errorf("the pane asks %q about a run that never started", asked)
	}
	if strings.Contains(asked, "exit") {
		t.Errorf("the pane asks %q about a status nothing ever reported", asked)
	}
	if !strings.Contains(asked, "make deploy") {
		t.Errorf("the pane asks %q without naming what did not run", asked)
	}
	if !inTheTree(a, pane) {
		t.Error("the pane went with the connection that was not made")
	}
	checkTree(t, a)
}

// A shell started again here is started at the size of the pane it goes
// in. A shell told it is as wide as the whole window draws its first
// prompt for a width it has not got.
func TestAShellStartedAgainHereGetsThePanesSize(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	pane := firstPane(t, a)
	endTheShell(t, a, 0, pane)
	want := pane.Size()
	if want.Cols >= a.lastSize[0] {
		t.Fatalf("the pane is %d columns of a window %d wide, so this test"+
			" cannot tell the two apart", want.Cols, a.lastSize[0])
	}

	var started [2]int
	shell := a.newShell
	a.newShell = func(argv []string, cols, rows int) (session.Session, error) {
		started = [2]int{cols, rows}
		return shell(argv, cols, rows)
	}
	pressTheAnswer(t, a, pane)

	if started != [2]int{want.Cols, want.Rows} {
		t.Errorf("the new shell was started at %dx%d, want the pane's %dx%d",
			started[0], started[1], want.Cols, want.Rows)
	}
}

// A saved machine moved since the pane opened is dialled where it is
// now, so the pane says the target changed rather than putting another
// machine under the old transcript with nothing to tell them apart.
func TestReconnectingToATargetThatMovedSaysSo(t *testing.T) {
	a, _, host, pane := aDroppedConnection(t)
	// Saved under the name the pane was reached by, somewhere else.
	moved := remote.Host{
		Name:    host,
		Address: "127.0.0.1",
		Port:    aPortNothingAnswersOn(t),
		User:    "tester",
	}
	if err := a.book.Put(moved, ""); err != nil {
		t.Fatalf("saving the machine at its new address: %v", err)
	}

	pressTheAnswer(t, a, pane)

	waitFor(t, a, "the pane to say where it is dialling instead", func() bool {
		a.reapExited()
		return strings.Contains(pane.ReadLines(200).Text, "This pane was on")
	})
}
