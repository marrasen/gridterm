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
	waitFor(t, a, "the read to come back", func() bool { return len(done) > 0 })
	if got := <-done; got.err != nil {
		t.Fatalf("reading after the drop: %v", got.err)
	}

	// The machine is connected again and the filesystem holds it.
	if a.machines.named(host) == nil {
		t.Error("nothing is connected after the read")
	}
	if f.under == nil {
		t.Error("the filesystem did not keep what it opened")
	}
	// And the bottom row said what was happening, because a click that
	// waits with nothing on screen reads as a window that has stopped.
	if said := a.saying(); !strings.Contains(said, "Reconnecting") {
		t.Errorf("the bottom row says %q", said)
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

	// It comes back once that connection has settled, whichever way it
	// went: given up on, the read asks for the machine itself.
	a.machines.giveUp(stuck)
	waitFor(t, a, "the read to come back", func() bool { return len(done) > 0 })
	if err := <-done; err != nil {
		t.Fatalf("reading once the machine on its way had settled: %v", err)
	}
}
