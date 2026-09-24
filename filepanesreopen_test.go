package main

import (
	"strings"
	"testing"
	"time"

	"github.com/marrasen/gridterm/internal/sshtest"
	"github.com/marrasen/gridterm/remote"
)

// A connection that drops does not take the file panes on it.
//
// It used to close them, and closing one asked an SFTP channel that had
// gone with the transport to close itself -- so the window took the
// user's panes away and then put up a dialog about how the taking away
// had gone. That dialog is the report in issue #23.
func TestADroppedConnectionKeepsItsFilePanes(t *testing.T) {
	a, s, host := aConnectedWindow(t, 100, 30)
	pane := openFilesFromThePlus(t, a, host)
	if pane == nil {
		t.Fatal("no file pane opened")
	}

	s.CloseClients()
	waitFor(t, a, "the window to see the machine go", func() bool {
		a.reapExited()
		return a.machines.named(host) == nil
	})

	// The pane is still there.
	var found bool
	for _, p := range a.files.view.Panes() {
		if p == pane {
			found = true
		}
	}
	if !found {
		t.Error("the file pane went with the connection")
	}
	// And nothing was said about failing to close it.
	if up, is := a.root.Modal().(interface{ Message() string }); is {
		if strings.Contains(up.Message(), "SFTP") || strings.Contains(up.Message(), "close") {
			t.Errorf("a dialog complained about the closing: %q", up.Message())
		}
	}
}

// And its filesystem knows the connection has gone, so the next thing
// asked of it opens the machine again rather than failing on a dead
// session.
func TestAFilePanesFilesystemIsToldTheConnectionWent(t *testing.T) {
	a, s, host := aConnectedWindow(t, 100, 30)
	if openFilesFromThePlus(t, a, host) == nil {
		t.Fatal("no file pane opened")
	}
	held := reopenersOn(t, a, host)
	if len(held) != 1 {
		t.Fatalf("%d filesystems hold %s, want the pane's one", len(held), host)
	}
	if held[0].under == nil {
		t.Fatal("the filesystem has nothing open before the drop")
	}

	s.CloseClients()
	waitFor(t, a, "the window to see the machine go", func() bool {
		a.reapExited()
		return a.machines.named(host) == nil
	})

	if held[0].under != nil {
		t.Error("the filesystem still holds the session that went")
	}
	// What it says about the machine survives, because the goroutine
	// that draws asks for it every frame and must not wait.
	if held[0].Name() == "" || held[0].Sep() == 0 {
		t.Error("it forgot what the machine is called")
	}
}

// Closing a file pane takes its filesystem off the window's list, so a
// machine going does not talk to one nobody is using.
func TestAClosedFilePaneIsForgotten(t *testing.T) {
	a, _, host := aConnectedWindow(t, 100, 30)
	pane := openFilesFromThePlus(t, a, host)
	if len(reopenersOn(t, a, host)) != 1 {
		t.Fatalf("%d filesystems hold %s to begin with", len(reopenersOn(t, a, host)), host)
	}

	if err := a.closePane(pane); err != nil {
		t.Fatalf("close the pane: %v", err)
	}
	if got := len(reopenersOn(t, a, host)); got != 0 {
		t.Errorf("%d filesystems still hold %s", got, host)
	}
}

// A machine deliberately disconnected still takes its panes: the user
// said to close it, which is not the same as it dropping.
func TestDisconnectingStillTakesTheFilePanes(t *testing.T) {
	a, _, host := aConnectedWindow(t, 100, 30)
	if openFilesFromThePlus(t, a, host) == nil {
		t.Fatal("no file pane opened")
	}

	if err := a.dropMachine(host); err != nil {
		t.Fatalf("disconnect: %v", err)
	}
	// The browser goes with its last pane, so there may be nothing left
	// to look through.
	if a.files != nil {
		for _, p := range a.files.view.Panes() {
			if a.filedUnder(p, host) {
				t.Error("a file pane is still open on a machine the user disconnected")
			}
		}
	}
	if got := len(reopenersOn(t, a, host)); got != 0 {
		t.Errorf("%d filesystems still hold a machine the user disconnected", got)
	}
}

// reopenersOn is the filesystems the window holds for a machine.
func reopenersOn(t *testing.T, a *testApp, host string) []*reopening {
	t.Helper()
	var out []*reopening
	for _, r := range a.reopening {
		if r.host == host {
			out = append(out, r)
		}
	}
	return out
}

// Reading a directory after the connection went opens the machine
// again and answers, which is the whole point of not closing the pane.
func TestAReadAfterTheDropOpensTheMachineAgain(t *testing.T) {
	a, s, host := aConnectedWindow(t, 100, 30)
	if openFilesFromThePlus(t, a, host) == nil {
		t.Fatal("no file pane opened")
	}
	held := reopenersOn(t, a, host)
	if len(held) != 1 {
		t.Fatalf("%d filesystems hold %s", len(held), host)
	}
	f := held[0]

	s.CloseClients()
	waitFor(t, a, "the window to see the machine go", func() bool {
		a.reapExited()
		return a.machines.named(host) == nil
	})
	if f.under != nil {
		t.Fatal("the filesystem was not told its connection went")
	}

	// A read, the way the pane's own does it: off the goroutine that
	// draws, because this one waits for the connection.
	type answer struct {
		err error
	}
	done := make(chan answer, 1)
	go func() {
		_, err := f.ReadDir("/")
		done <- answer{err}
	}()
	// What the bottom row said while the read was out, because a click
	// that waits with nothing on screen reads as a window that has
	// stopped. Read here rather than afterwards: the line goes when the
	// connection has been made, which is what the read waits for.
	var said bool
	waitFor(t, a, "the read to come back", func() bool {
		said = said || strings.Contains(a.saying(), "Reconnecting")
		return len(done) > 0
	})
	if got := <-done; got.err != nil {
		t.Fatalf("reading after the drop: %v", got.err)
	}
	if !said {
		t.Error("the bottom row never said it was reconnecting")
	}

	// The machine is connected again and the filesystem holds it.
	if a.machines.named(host) == nil {
		t.Error("nothing is connected after the read")
	}
	if f.under == nil {
		t.Error("the filesystem did not keep what it opened")
	}
	// And the line is gone once the machine has answered, because it
	// said what the window was doing and the window has done it.
	if got := a.saying(); strings.Contains(got, "Reconnecting") {
		t.Errorf("the bottom row still says %q", got)
	}
}

// A filesystem whose pane has been closed opens nothing more.
//
// A finished copy holds the filesystems it ran on, and one of those
// outlives the pane it came from. Without this, a job pane drawn after
// its machine went reached through that copy and opened a connection
// nobody had asked for, which put a row on the sidebar out of nothing.
func TestAForgottenFilesystemOpensNothing(t *testing.T) {
	a, _, host := aConnectedWindow(t, 100, 30)
	pane := openFilesFromThePlus(t, a, host)
	held := reopenersOn(t, a, host)
	if len(held) != 1 {
		t.Fatalf("%d filesystems hold %s", len(held), host)
	}
	f := held[0]

	if err := a.closePane(pane); err != nil {
		t.Fatalf("close the pane: %v", err)
	}
	f.Lost()

	was := len(a.registry.Groups(panelNow))
	if _, err := f.ReadDir("/"); err == nil {
		t.Error("a filesystem nobody is browsing answered a read")
	}
	if got := len(a.registry.Groups(panelNow)); got != was {
		t.Errorf("the sidebar grew from %d groups to %d", was, got)
	}
}

// A reconnect the user gives up on answers the read, rather than
// leaving the pane reading for ever.
//
// What queues behind a connection being made is thrown away when that
// connection is not made, which is right for work but wrong for a
// caller blocked on an answer: the pane's read sat in its select until
// the window closed, holding a goroutine and leaving the pane unable to
// read again.
func TestAReconnectGivenUpOnAnswersTheRead(t *testing.T) {
	a, s, host := aConnectedWindow(t, 100, 30)
	if openFilesFromThePlus(t, a, host) == nil {
		t.Fatal("no file pane opened")
	}
	held := reopenersOn(t, a, host)
	if len(held) != 1 {
		t.Fatalf("%d filesystems hold %s", len(held), host)
	}
	f := held[0]

	s.CloseClients()
	waitFor(t, a, "the window to see the machine go", func() bool {
		a.reapExited()
		return a.machines.named(host) == nil
	})

	// A connection to the machine that is on its way and never comes
	// back, which is what the read has to queue behind.
	stuck := &dialling{names: []string{host}, cancel: func() {}}
	if err := a.machines.holdNames(stuck); err != nil {
		t.Fatalf("hold the name: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := f.ReadDir("/")
		done <- err
	}()
	waitFor(t, a, "the read to queue behind the connection", func() bool {
		return len(stuck.answering) > 0
	})

	// The user gives up on it, from the row watching it.
	a.machines.giveUp(stuck)
	waitFor(t, a, "the read to come back", func() bool { return len(done) > 0 })
	if err := <-done; err == nil {
		t.Error("the read answered although the connection was never made")
	}
}

// A machine renamed while its connection is gone is still the machine
// its panes are on.
//
// The filesystem holds the machine by name. Left under the old one it
// would never be told the connection had gone, so the pane would call a
// session that was not there -- which is issue #23 again -- and opening
// it would dial under a name the window no longer uses.
func TestARenamedMachineStillReachesItsFilePanes(t *testing.T) {
	a, s, host := aConnectedWindow(t, 100, 30)
	if openFilesFromThePlus(t, a, host) == nil {
		t.Fatal("no file pane opened")
	}
	held := reopenersOn(t, a, host)
	if len(held) != 1 {
		t.Fatalf("%d filesystems hold %s", len(held), host)
	}
	f := held[0]

	addr, port := s.Host()
	a.renamedMachine(host, remote.Host{
		Name: "renamed", Address: addr, Port: port, User: "tester",
	})
	if got := f.Host(); got != "renamed" {
		t.Fatalf("the filesystem is filed under %q, want the name the machine has now", got)
	}

	s.CloseClients()
	waitFor(t, a, "the window to see the machine go", func() bool {
		a.reapExited()
		return a.machines.named("renamed") == nil
	})
	if f.under != nil {
		t.Error("the filesystem was never told its connection went")
	}
}

// A pane on a machine that dropped is still filed under that machine.
//
// Which machine a filesystem is on used to be read off what is
// connected, so one whose connection had gone answered Local: a copy
// out of the pane would be counted against this machine, and the
// commands on the row would work on the wrong one.
func TestAPaneOnADroppedMachineIsStillFiledUnderIt(t *testing.T) {
	a, s, host := aConnectedWindow(t, 100, 30)
	pane := openFilesFromThePlus(t, a, host)
	if pane == nil {
		t.Fatal("no file pane opened")
	}

	s.CloseClients()
	waitFor(t, a, "the window to see the machine go", func() bool {
		a.reapExited()
		return a.machines.named(host) == nil
	})

	if got := a.hostOf(pane.FS()); got != host {
		t.Errorf("the pane is filed under %q, want %q", got, host)
	}
}

// A filesystem a job opened for itself comes off the window's list when
// the job closes it.
//
// A job closes what it opened on a goroutine of its own, without going
// through the browser, so leaving the forgetting to the browser left
// every one of those on the list for good.
func TestAFilesystemLetGoOfIsForgotten(t *testing.T) {
	a, _, host := aConnectedWindow(t, 100, 30)
	f, err := a.filesystem(host)
	if err != nil {
		t.Fatalf("open a filesystem on %s: %v", host, err)
	}
	if got := len(reopenersOn(t, a, host)); got != 1 {
		t.Fatalf("%d filesystems hold %s after opening one", got, host)
	}

	if err := f.Close(); err != nil {
		t.Fatalf("close it: %v", err)
	}
	settleAndClearNotices(t, a)
	if got := len(reopenersOn(t, a, host)); got != 0 {
		t.Errorf("%d filesystems still hold %s after the close", got, host)
	}
	// And it opens nothing more.
	if _, err := f.ReadDir("/"); err == nil {
		t.Error("a filesystem that was closed answered a read")
	}
}

// A pane closed while its machine is being opened again does not leave
// the session that comes back open.
//
// The far end holds an SFTP session until it is closed. One handed to a
// filesystem nobody is using any more would be held for the life of the
// window.
func TestAReconnectThatLandsTooLateIsClosed(t *testing.T) {
	a, s, host := aConnectedWindow(t, 100, 30)
	pane := openFilesFromThePlus(t, a, host)
	if pane == nil {
		t.Fatal("no file pane opened")
	}
	held := reopenersOn(t, a, host)
	if len(held) != 1 {
		t.Fatalf("%d filesystems hold %s", len(held), host)
	}
	f := held[0]

	s.CloseClients()
	waitFor(t, a, "the window to see the machine go", func() bool {
		a.reapExited()
		return a.machines.named(host) == nil
	})

	// The read starts, and its request to open the machine sits on the
	// queue: the pane is closed before the window gets to it.
	done := make(chan error, 1)
	go func() {
		_, err := f.ReadDir("/")
		done <- err
	}()
	for start := time.Now(); a.pump.pending() == 0; {
		if time.Since(start) > waitBudget {
			t.Fatal("the read never asked for the machine")
		}
		time.Sleep(time.Millisecond)
	}
	if err := a.closePane(pane); err != nil {
		t.Fatalf("close the pane: %v", err)
	}

	waitFor(t, a, "the read to come back", func() bool { return len(done) > 0 })
	if err := <-done; err == nil {
		t.Error("a filesystem nobody is browsing answered a read")
	}
	if f.under != nil {
		t.Error("it kept a session opened for a pane that had gone")
	}
}

// A reconnect through a machine that is already being connected to
// waits for it, rather than asking the user about a connection they did
// not ask for.
//
// Nobody typed this: a pane read a folder. The dialog about a machine
// on its way would land over whatever the user is doing, and the read
// would be answered with "nothing is connected" while they read it.
func TestAReconnectWaitsForAJumpHostOnItsWay(t *testing.T) {
	near, far := sshtest.New(t), sshtest.New(t)
	a := newTestApp(t, 100, 30)
	withDialogs(t, a)
	withPanel(t, a)
	pinServers(t, a, near, far)
	saveHost(t, a, "edge", near, "")
	saveHost(t, a, "db", far, "edge")
	if err := a.connectSaved("db"); err != nil {
		t.Fatalf("connectSaved: %v", err)
	}
	waitFor(t, a, "both machines to answer", func() bool {
		return a.machines.named("db") != nil && a.machines.named("edge") != nil
	})
	if openFilesFromThePlus(t, a, "db") == nil {
		t.Fatal("no file pane opened")
	}
	held := reopenersOn(t, a, "db")
	if len(held) != 1 {
		t.Fatalf("%d filesystems hold db", len(held))
	}
	f := held[0]

	far.CloseClients()
	near.CloseClients()
	waitFor(t, a, "the window to see the machines go", func() bool {
		a.reapExited()
		return a.machines.named("db") == nil && a.machines.named("edge") == nil
	})
	settleAndClearNotices(t, a)

	// The machine db is reached through is on its way, under a name of
	// its own. It is that one the window would otherwise ask about.
	stuck := &dialling{names: []string{"edge"}, cancel: func() {}}
	holdTheNames(t, a, stuck)

	done := make(chan error, 1)
	go func() {
		_, err := f.ReadDir("/")
		done <- err
	}()
	waitFor(t, a, "the read to queue behind the connection", func() bool {
		return len(stuck.answering) > 0
	})
	if up := a.root.Modal(); up != nil {
		t.Errorf("a dialog opened over the read: %T", up)
	}

	// It comes back once that connection has settled. Given up on: that
	// machine is on the way to this one, so this one is not reachable
	// either, and the read says so rather than the window dialling the
	// route a second time a moment after the user said no.
	a.machines.giveUp(stuck)
	waitFor(t, a, "the read to come back", func() bool { return len(done) > 0 })
	if err := <-done; err == nil {
		t.Error("the read answered although the machine on the way was given up on")
	}
	if a.machines.named("db") != nil || a.machines.named("edge") != nil {
		t.Errorf("the window dialled %v after the user gave up", a.machines.names())
	}
}

// The bottom row says it is reconnecting for as long as that takes, not
// for four seconds.
//
// A line that says something worked is gone by the time the user has
// done the next thing. This one says what the window is doing, and a
// login can take longer than that: a row that went blank half way
// through the wait would read as a window that has stopped, which is
// the thing the line is up to prevent.
func TestTheReconnectingLineHoldsUntilTheMachineAnswers(t *testing.T) {
	a, s, host := aConnectedWindow(t, 100, 30)
	if openFilesFromThePlus(t, a, host) == nil {
		t.Fatal("no file pane opened")
	}
	held := reopenersOn(t, a, host)
	if len(held) != 1 {
		t.Fatalf("%d filesystems hold %s", len(held), host)
	}
	f := held[0]

	s.CloseClients()
	waitFor(t, a, "the window to see the machine go", func() bool {
		a.reapExited()
		return a.machines.named(host) == nil
	})
	settleAndClearNotices(t, a)

	// A connection to the machine that is on its way and takes its time.
	stuck := &dialling{names: []string{host}, cancel: func() {}}
	holdTheNames(t, a, stuck)
	done := make(chan error, 1)
	go func() {
		_, err := f.ReadDir("/")
		done <- err
	}()
	waitFor(t, a, "the read to ask for the machine", func() bool {
		return len(stuck.answering) > 0
	})
	// Read off the drawn grid, not off the field: the sidebar is on a
	// layer over this row, and a line shorter than the sidebar is wide
	// is one nobody sees.
	a.g.Clear()
	a.drawHint()
	if got := bottomRow(t, a); !strings.Contains(got, "Reconnecting to") {
		t.Fatalf("the bottom row reads %q", got)
	}

	// Long past the few seconds a line that says something worked gets.
	was := a.frameTime()
	a.now = func() time.Time { return was.Add(10 * statusFor) }
	a.stepStatus()
	// Cleared first, because a row nothing writes to keeps what the
	// last frame left on it -- which is the line, and would read as
	// still being there however broken the holding was.
	a.g.Clear()
	a.drawHint()
	if got := bottomRow(t, a); !strings.Contains(got, "Reconnecting to") {
		t.Errorf("the bottom row reads %q after the wait, want it still saying so", got)
	}

	// And it goes when the connection has settled.
	a.machines.giveUp(stuck)
	waitFor(t, a, "the read to come back", func() bool { return len(done) > 0 })
	<-done
	a.stepStatus()
	a.g.Clear()
	a.drawHint()
	if got := bottomRow(t, a); strings.Contains(got, "Reconnecting") {
		t.Errorf("the bottom row still reads %q", got)
	}
}

// A dial that fails beyond the machine on the way leaves that machine
// connected, so a read queued behind it goes on through it.
//
// What a dial comes back with says whether its own far end answered.
// That is not this machine and need not even be the one on the way: a
// machine of a route that answered is held and stays held when the dial
// beyond it fails. Asking that dial would fail the read although the
// way to the machine is open.
func TestAReadWaitsThroughADialThatFailedBeyondTheJumpHost(t *testing.T) {
	near, far := sshtest.New(t), sshtest.New(t)
	a := newTestApp(t, 100, 30)
	withDialogs(t, a)
	withPanel(t, a)
	pinServers(t, a, near, far)
	saveHost(t, a, "edge", near, "")
	saveHost(t, a, "db", far, "edge")
	if err := a.connectSaved("db"); err != nil {
		t.Fatalf("connectSaved: %v", err)
	}
	waitFor(t, a, "both machines to answer", func() bool {
		return a.machines.named("db") != nil && a.machines.named("edge") != nil
	})
	if openFilesFromThePlus(t, a, "db") == nil {
		t.Fatal("no file pane opened")
	}
	held := reopenersOn(t, a, "db")
	if len(held) != 1 {
		t.Fatalf("%d filesystems hold db", len(held))
	}
	f := held[0]

	// Only the machine at the far end goes. The one on the way stays
	// connected, which is what makes this different from both dropping.
	far.CloseClients()
	waitFor(t, a, "the window to see the far machine go", func() bool {
		a.reapExited()
		return a.machines.named("db") == nil
	})
	settleAndClearNotices(t, a)
	edge := a.machines.named("edge")
	if edge == nil {
		t.Fatal("the machine on the way went too")
	}

	// A dial on its way somewhere else through edge, holding both
	// names, caught in the moment before edge has answered.
	delete(a.machines.held, "edge")
	stuck := &dialling{names: []string{"edge", "web"}, cancel: func() {}}
	holdTheNames(t, a, stuck)

	done := make(chan error, 1)
	go func() {
		_, err := f.ReadDir("/")
		done <- err
	}()
	waitFor(t, a, "the read to queue behind it", func() bool {
		return len(stuck.answering) > 0
	})

	// edge answers and is held; the machine beyond it does not.
	a.machines.releaseName(stuck, "edge")
	a.machines.take(edge)
	a.machines.settle(stuck, false)

	waitFor(t, a, "the read to come back", func() bool { return len(done) > 0 })
	if err := <-done; err != nil {
		t.Fatalf("reading through a machine on the way that answered: %v", err)
	}
	if a.machines.named("db") == nil {
		t.Error("db was never reached, although edge was connected")
	}
}
