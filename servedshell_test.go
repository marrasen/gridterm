package main

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/session"
)

// A shell another window starts here gets a row on this window's
// sidebar. Whoever is sitting at this machine can then see that
// somebody else is running something on it.
func TestAShellStartedFromAnotherWindowGetsARow(t *testing.T) {
	a, _ := aWindowThatCanServe(t)
	was := a.registry.Len()

	sess, err := a.newSession(80, 24)
	if err != nil {
		t.Fatalf("start it: %v", err)
	}
	a.pump.run()

	if got := a.registry.Len(); got != was+1 {
		t.Fatalf("the sidebar has %d rows, want one more than the %d it had", got, was)
	}
	said := strings.Join(panelText(a, time.Now()), "\n")
	if !strings.Contains(said, servedLabel) {
		t.Errorf("the sidebar does not say a shell was started from elsewhere:\n%s", said)
	}
	_ = sess.Close()
}

// The row goes when the shell does.
func TestTheRowGoesWhenTheServedShellDoes(t *testing.T) {
	a, _ := aWindowThatCanServe(t)
	was := a.registry.Len()
	sess, err := a.newSession(80, 24)
	if err != nil {
		t.Fatalf("start it: %v", err)
	}
	a.pump.run()

	if err := sess.Close(); err != nil {
		t.Fatalf("close it: %v", err)
	}
	a.pump.run()

	if got := a.registry.Len(); got != was {
		t.Errorf("the sidebar has %d rows, want the %d it had", got, was)
	}
}

// Closing the row ends the shell, because whoever is sitting here owns
// the machine however far away the person using it is.
func TestClosingTheRowEndsTheServedShell(t *testing.T) {
	a, _ := aWindowThatCanServe(t)
	sess, err := a.newSession(80, 24)
	if err != nil {
		t.Fatalf("start it: %v", err)
	}
	a.pump.run()
	row := servedRow(t, a)

	if err := row.Close(); err != nil {
		t.Fatalf("close the row: %v", err)
	}
	a.pump.run()

	if _, err := sess.Read(make([]byte, 1)); err == nil {
		t.Error("the shell is still running")
	}
	if got := servedRows(a); got != 0 {
		t.Errorf("%d rows are left", got)
	}
}

// Closing twice is harmless. The server closes a session when the
// connection ends, and the row closes it when the user does.
func TestClosingAServedShellTwiceIsHarmless(t *testing.T) {
	a, _ := aWindowThatCanServe(t)
	was := a.registry.Len()
	sess, err := a.newSession(80, 24)
	if err != nil {
		t.Fatalf("start it: %v", err)
	}
	a.pump.run()

	_ = sess.Close()
	_ = sess.Close()
	a.pump.run()

	if got := a.registry.Len(); got != was {
		t.Errorf("the sidebar has %d rows, want the %d it had", got, was)
	}
	if got := len(a.servedRows); got != 0 {
		t.Errorf("the window still holds %d served rows", got)
	}
}

// A shell that will not start gets no row.
func TestAShellThatWillNotStartGetsNoRow(t *testing.T) {
	a, _ := aWindowThatCanServe(t)
	was := a.registry.Len()
	boom := errors.New("no shell here")
	a.newShell = func([]string, string, int, int) (session.Session, error) {
		return nil, boom
	}

	if _, err := a.newSession(80, 24); err == nil {
		t.Fatal("it started a shell that could not start")
	}
	a.pump.run()

	if got := a.registry.Len(); got != was {
		t.Errorf("the sidebar has %d rows, want the %d it had", got, was)
	}
}

// servedRow is the one row standing for a shell started from elsewhere.
func servedRow(t *testing.T, a *testApp) *conns.Entry {
	t.Helper()
	for _, group := range a.registry.Groups(time.Now()) {
		for _, row := range group.Rows {
			if row.Label == servedLabel {
				return row.Entry
			}
		}
	}
	t.Fatal("no row stands for a shell started from elsewhere")
	return nil
}

// servedRows is how many rows stand for a shell started from elsewhere.
func servedRows(a *testApp) int {
	n := 0
	for _, group := range a.registry.Groups(time.Now()) {
		for _, row := range group.Rows {
			if row.Label == servedLabel {
				n++
			}
		}
	}
	return n
}

// A shell a client started here is left out of what this window
// publishes. The window that opened it is already drawing it, and there
// is no pane here to attach to.
func TestAServedShellIsNotPublished(t *testing.T) {
	a, _ := aWindowThatCanServe(t)
	now := time.Now()
	was := len(a.snapshot(now).Open)

	sess, err := a.newSession(80, 24)
	if err != nil {
		t.Fatalf("start it: %v", err)
	}
	a.pump.run()

	// It is on the sidebar.
	if got := servedRows(a); got != 1 {
		t.Fatalf("%d rows stand for a shell started from elsewhere, want 1", got)
	}
	// And not in what the clients are told.
	if got := len(a.snapshot(now).Open); got != was {
		t.Errorf("the snapshot holds %d things, want the %d it held", got, was)
	}
	_ = sess.Close()
}

// And it goes out of that set when it closes, so nothing is left
// holding a row that has gone.
func TestAServedShellLeavesTheSetWhenItCloses(t *testing.T) {
	a, _ := aWindowThatCanServe(t)
	sess, err := a.newSession(80, 24)
	if err != nil {
		t.Fatalf("start it: %v", err)
	}
	a.pump.run()
	if len(a.servedRows) != 1 {
		t.Fatalf("the window holds %d served rows", len(a.servedRows))
	}

	_ = sess.Close()
	a.pump.run()

	if got := len(a.servedRows); got != 0 {
		t.Errorf("the window still holds %d served rows", got)
	}
}
