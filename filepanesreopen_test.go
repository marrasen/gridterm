package main

import (
	"strings"
	"testing"
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
