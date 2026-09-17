package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/marrasen/gridterm/session"
	"github.com/marrasen/gridterm/ui"
)

// A window whose first pane will not start opens anyway and says why, in
// a notice and in the log.
func TestAWindowWhoseFirstPaneWillNotStartOpensAndSaysWhy(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	refused := errors.New("Windows would not put this shell in a job object")
	a.newShell = func([]string, string, int, int) (session.Session, error) {
		return nil, refused
	}

	first, err := a.openFirst(startup{})

	if err != nil {
		t.Fatalf("it refused to open the window: %v", err)
	}
	if first != nil {
		t.Fatal("it gave back a pane it could not start")
	}
	// Logged as well as shown: a notice is gone once it is dismissed.
	if !a.logged.holds(refused.Error()) {
		t.Error("the reason is not in the log")
	}
	// The notice lands on a frame, because the tree it goes in is built
	// after openFirst returns.
	a.pump.run()
	n := awaitModal(t, a, "the notice", byTitle[*ui.Notice](noFirstPane))
	if !strings.Contains(n.Message(), refused.Error()) {
		t.Errorf("it says %q, want the reason it could not start", n.Message())
	}
	if !n.Failure {
		t.Error("the notice is not marked as a failure")
	}
}

// A target -ssh cannot read goes the same way. A gridterm on a desktop
// shortcut has no console for that message either.
func TestATargetSshCannotReadOpensTheWindowToo(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)

	first, err := a.openFirst(startup{target: "user@@@host:notaport"})

	if err != nil {
		t.Fatalf("it refused to open the window: %v", err)
	}
	if first != nil {
		t.Fatal("it gave back a pane")
	}
	a.pump.run()
	awaitModal(t, a, "the notice", byTitlePrefix[*ui.Notice]("Could not read what -ssh names"))
}

// The window a failure leaves behind still works: it does not mark
// itself for closing, and a pane can be opened in it afterwards.
func TestTheWindowAFailureLeavesBehindStillWorks(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	refuse := true
	was := a.newShell
	a.newShell = func(argv []string, dir string, cols, rows int) (session.Session, error) {
		if refuse {
			return nil, errors.New("no")
		}
		return was(argv, dir, cols, rows)
	}

	if _, err := a.openFirst(startup{}); err != nil {
		t.Fatalf("open: %v", err)
	}

	// Closing the last pane is what closes a window, and nothing closed
	// here: there was never a pane to close.
	if a.quit.Load() {
		t.Fatal("the window marked itself for closing")
	}
	// And the user can try again from the File menu.
	refuse = false
	had := len(a.panes)
	if err := a.openPaneHere(); err != nil {
		t.Fatalf("open a pane in it: %v", err)
	}
	if got := len(a.panes); got != had+1 {
		t.Errorf("the window holds %d panes, want one more than the %d it had", got, had)
	}
}

// A pane that does start reaches the sidebar, which is how the user gets
// back to it.
func TestAFirstPaneThatStartsReachesTheSidebar(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	was := a.registry.Len()

	first, err := a.openFirst(startup{})

	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if first == nil {
		t.Fatal("it opened no pane")
	}
	if got := a.registry.Len(); got != was+1 {
		t.Errorf("the sidebar has %d rows, want one more than the %d it had", got, was)
	}
}
