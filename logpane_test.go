package main

import (
	"log"
	"os"
	"strings"
	"testing"

	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/grid"
)

// withLog points the window's log at the buffer for one test, and
// keeps the lines out of the test's own output while it runs.
func withLog(t *testing.T) {
	t.Helper()
	keepLog()
	windowLog.Also(nil)
	t.Cleanup(func() {
		log.SetOutput(os.Stderr)
		windowLog.Also(os.Stderr)
	})
}

// logRow is the sidebar row of the log pane, and nil when none is open.
func logRow(a *testApp) *conns.Entry {
	for _, e := range a.panes {
		if e != nil && e.Kind == conns.Log {
			return e
		}
	}
	return nil
}

// The menu opens a pane with the log in it, and what was logged before
// the pane opened is in it. Debugging starts after the thing went
// wrong, so a log that began when the pane did would be empty.
func TestTheLogPaneShowsWhatWasLoggedBeforeItOpened(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	a.commands()
	withLog(t)
	log.Print("could not reach margit")

	if err := a.root.Commands.Run(logCommand); err != nil {
		t.Fatalf("running %s: %v", logCommand, err)
	}

	pane := a.logPane()
	if pane == nil {
		t.Fatal("no pane opened on the log")
	}
	waitFor(t, a, "the log line to reach the pane", func() bool {
		return strings.Contains(paneText(pane), "could not reach margit")
	})
}

// A line logged while the pane is open arrives in it, which is the
// point: watching what the window is doing as it does it.
func TestALineLoggedWhileThePaneIsOpenArrivesInIt(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	a.commands()
	withLog(t)
	if err := a.root.Commands.Run(logCommand); err != nil {
		t.Fatalf("running %s: %v", logCommand, err)
	}
	pane := a.logPane()

	log.Print("the connection went")

	waitFor(t, a, "the new line to reach the pane", func() bool {
		return strings.Contains(paneText(pane), "the connection went")
	})
}

// Asking again goes to the pane already open rather than opening a
// second. Two panes would show the same lines twice.
func TestAskingForTheLogTwiceGoesToTheOneAlreadyOpen(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	a.commands()
	if err := a.root.Commands.Run(logCommand); err != nil {
		t.Fatalf("the first time: %v", err)
	}
	first := a.logPane()

	if err := a.root.Commands.Run(logCommand); err != nil {
		t.Fatalf("the second time: %v", err)
	}

	if got := a.logPane(); got != first {
		t.Error("asking twice opened a second log pane")
	}
	if n := countLogPanes(a); n != 1 {
		t.Errorf("%d log panes are open, want one", n)
	}
	if !first.Focused() {
		t.Error("asking again did not go to the pane already open")
	}
}

// countLogPanes is how many panes are showing the log.
func countLogPanes(a *testApp) int {
	n := 0
	for _, e := range a.panes {
		if e != nil && e.Kind == conns.Log {
			n++
		}
	}
	return n
}

// The pane gets a row on the sidebar with a picture of its own, so it
// reads as the log rather than as another shell.
func TestTheLogPaneHasARowOfItsOwn(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	a.commands()
	if err := a.root.Commands.Run(logCommand); err != nil {
		t.Fatalf("running %s: %v", logCommand, err)
	}

	row := logRow(a)
	if row == nil {
		t.Fatal("the log pane has no row on the sidebar")
	}
	if got, want := row.Label, logLabel; got != want {
		t.Errorf("the row says %q, want %q", got, want)
	}
	if got := icon(row.Kind); got == grid.Icon(grid.IconTerminal) {
		t.Error("the row carries the terminal's picture, want the log's own")
	}
}

// Closing the pane leaves the window logging, so opening it again
// shows what happened in between.
func TestClosingTheLogPaneLeavesTheWindowLogging(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	a.commands()
	withLog(t)
	if err := a.root.Commands.Run(logCommand); err != nil {
		t.Fatalf("opening it: %v", err)
	}
	if err := a.closePane(a.logPane()); err != nil {
		t.Fatalf("closing it: %v", err)
	}
	if a.logPane() != nil {
		t.Fatal("the pane is still there after being closed")
	}

	log.Print("after the pane went")

	if err := a.root.Commands.Run(logCommand); err != nil {
		t.Fatalf("opening it again: %v", err)
	}
	again := a.logPane()
	waitFor(t, a, "the line logged in between to be in the new pane", func() bool {
		return strings.Contains(paneText(again), "after the pane went")
	})
}

// Nothing typed into the log reaches anything. There is no program at
// the far end, and a key that looked like it did something would be
// worse than one that does nothing.
func TestTypingIntoTheLogPaneChangesNothing(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	a.commands()
	withLog(t)
	if err := a.root.Commands.Run(logCommand); err != nil {
		t.Fatalf("running %s: %v", logCommand, err)
	}
	pane := a.logPane()
	held := windowLog.Held()

	if _, err := pane.HandleKey(input1('x')); err != nil {
		t.Fatalf("typing: %v", err)
	}

	if got := windowLog.Held(); got != held {
		t.Errorf("the log holds %d lines after typing, want the %d it had", got, held)
	}
}
