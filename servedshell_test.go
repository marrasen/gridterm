package main

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/session"
	"github.com/marrasen/gridterm/ui/term"
)

// openedFromAnotherWindow asks for something to work in the way a client
// does, which is from a goroutine of the server's, and runs the window
// until the answer comes back.
func openedFromAnotherWindow(t *testing.T, a *testApp, cols, rows int) (session.Session, error) {
	t.Helper()
	type made struct {
		sess session.Session
		err  error
	}
	back := make(chan made, 1)
	go func() {
		sess, err := a.newSession(cols, rows)
		back <- made{sess: sess, err: err}
	}()
	deadline := time.Now().Add(waitBudget)
	for time.Now().Before(deadline) {
		a.pump.run()
		select {
		case got := <-back:
			return got.sess, got.err
		default:
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("the window never answered a client asking for something to work in")
	return nil, nil
}

// A window a client opens something in opens a pane of its own, with a
// row saying where it came from. Somebody sitting at this machine can
// see what is being run on it, and work in it.
func TestSomethingOpenedFromAnotherWindowIsAPaneHere(t *testing.T) {
	a, _ := aWindowThatCanServe(t)
	was := len(a.panes)

	sess, err := openedFromAnotherWindow(t, a, 80, 24)
	if err != nil {
		t.Fatalf("open it: %v", err)
	}
	defer func() { _ = sess.Close() }()

	if got := len(a.panes); got != was+1 {
		t.Fatalf("the window holds %d panes, want one more than the %d it had", got, was)
	}
	said := strings.Join(panelText(a, time.Now()), "\n")
	if !strings.Contains(said, servedLabel) {
		t.Errorf("the sidebar does not say where the pane came from:\n%s", said)
	}
}

// The window that opened it going leaves the pane running here.
//
// Marcus opened a pane on the host from a client, left the client, and
// the pane went with it. The shell runs on this machine, so leaving is
// the end of a watch and not the end of the shell.
func TestTheWindowThatOpenedAPaneGoingLeavesItRunningHere(t *testing.T) {
	a, _ := aWindowThatCanServe(t)
	sess, err := openedFromAnotherWindow(t, a, 80, 24)
	if err != nil {
		t.Fatalf("open it: %v", err)
	}
	pane := paneFromAnotherWindow(t, a)
	panes := len(a.panes)

	// The client goes, which is the server closing what it was given.
	if err := sess.Close(); err != nil {
		t.Fatalf("the client leaving: %v", err)
	}
	a.pump.run()

	if got := len(a.panes); got != panes {
		t.Errorf("the window holds %d panes, want the %d it had", got, panes)
	}
	if a.panes[pane] == nil {
		t.Error("the pane the other window opened has gone with it")
	}
	// Still running: a pane whose program has gone refuses a watch.
	again, err := newWatched(pane)
	if err != nil {
		t.Errorf("the shell ended when the window that opened it left: %v", err)
	} else {
		_ = again.Close()
	}
}

// Closing the pane here ends the shell, because whoever is sitting here
// owns the machine however far away the person using it is.
func TestClosingAPaneOpenedFromAnotherWindowEndsTheShell(t *testing.T) {
	a, _ := aWindowThatCanServe(t)
	sess, err := openedFromAnotherWindow(t, a, 80, 24)
	if err != nil {
		t.Fatalf("open it: %v", err)
	}
	pane := paneFromAnotherWindow(t, a)

	if err := a.closePane(pane); err != nil {
		t.Fatalf("close the pane: %v", err)
	}
	a.pump.run()

	if a.panes[pane] != nil {
		t.Error("the pane is still held")
	}
	// And the watch on it ends, so the window that opened it is told.
	waitFor(t, a, "the watch to end", func() bool {
		_, err := sess.Read(make([]byte, 1))
		return err != nil
	})
}

// A shell that will not start opens no pane.
func TestAShellThatWillNotStartOpensNoPane(t *testing.T) {
	a, _ := aWindowThatCanServe(t)
	was := len(a.panes)
	a.newShell = func([]string, string, int, int) (session.Session, error) {
		return nil, errors.New("no shell here")
	}

	if _, err := openedFromAnotherWindow(t, a, 80, 24); err == nil {
		t.Fatal("it opened a pane on a shell that could not start")
	}

	if got := len(a.panes); got != was {
		t.Errorf("the window holds %d panes, want the %d it had", got, was)
	}
}

// A pane opened from another window is published like any other, so a
// client can attach to it again after letting go.
//
// It used to be left out, because it had no pane here to attach to.
func TestAPaneOpenedFromAnotherWindowIsPublished(t *testing.T) {
	a, _ := aWindowThatCanServe(t)
	now := time.Now()
	was := len(a.snapshot(now).Open)

	sess, err := openedFromAnotherWindow(t, a, 80, 24)
	if err != nil {
		t.Fatalf("open it: %v", err)
	}
	defer func() { _ = sess.Close() }()

	if got := len(a.snapshot(time.Now()).Open); got != was+1 {
		t.Errorf("the snapshot holds %d things, want one more than the %d it held", got, was)
	}
}

// paneFromAnotherWindow is the one pane another window opened here.
func paneFromAnotherWindow(t *testing.T, a *testApp) *term.Terminal {
	t.Helper()
	var found *term.Terminal
	for pane, e := range a.panes {
		if e != nil && e.Note == servedLabel {
			if found != nil {
				t.Fatal("more than one pane says it came from another window")
			}
			found = pane
		}
	}
	if found == nil {
		t.Fatalf("no pane says it came from another window: %v", panelText(a, time.Now()))
	}
	return found
}

// servedRow is the row for the one pane another window opened here.
func servedRow(t *testing.T, a *testApp) *conns.Entry {
	t.Helper()
	return a.panes[paneFromAnotherWindow(t, a)]
}
