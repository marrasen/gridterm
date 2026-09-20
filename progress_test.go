package main

import (
	"strings"
	"testing"
	"time"

	"github.com/marrasen/gridterm/conns"
)

// noteOnPaneRow is what the sidebar row of the only pane says.
func noteOnPaneRow(t *testing.T, a *testApp) string {
	t.Helper()
	pane := onlyPaneOn(t, a)
	e := a.panes[pane]
	if e == nil {
		t.Fatal("the pane has no row")
	}
	return e.Note
}

// A message a program asks to have shown lands on the pane's row.
//
// On the row rather than in a dialog: a dialog takes the keyboard, and
// anything that can write to a pane can send one of these.
func TestAMessageFromAProgramLandsOnTheRow(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	withDialogs(t, a)
	a.commands()

	a.shells[0].out <- []byte("\x1b]9;the build finished\x07")
	waitFor(t, a, "the row to say it", func() bool {
		a.refreshPanel(time.Now())
		return strings.Contains(noteOnPaneRow(t, a), "the build finished")
	})

	if a.root.Modal() != nil {
		t.Error("a program put a dialog in front of the user")
	}
}

// How far along a program says it is lands there too.
func TestHowFarAlongAProgramIsLandsOnTheRow(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	withDialogs(t, a)
	a.commands()

	a.shells[0].out <- []byte("\x1b]9;4;1;42\x07")
	waitFor(t, a, "the row to say how far along it is", func() bool {
		a.refreshPanel(time.Now())
		return strings.Contains(noteOnPaneRow(t, a), "42%")
	})
}

// The row keeps up: a program that says it has finished takes the
// number off again.
func TestFinishingTakesTheNumberOffTheRow(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	withDialogs(t, a)
	a.commands()
	a.shells[0].out <- []byte("\x1b]9;4;1;42\x07")
	waitFor(t, a, "the row to say how far along it is", func() bool {
		a.refreshPanel(time.Now())
		return strings.Contains(noteOnPaneRow(t, a), "42%")
	})

	a.shells[0].out <- []byte("\x1b]9;4;0\x07")

	waitFor(t, a, "the row to stop saying it", func() bool {
		a.refreshPanel(time.Now())
		return !strings.Contains(noteOnPaneRow(t, a), "42%")
	})
}

// A note something else put on the row is not written over. The one a
// pane can carry says its channel could not be let go of, and the row
// is the only place the user can read it.
func TestANoteFromElsewhereIsNotWrittenOver(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	withDialogs(t, a)
	a.commands()
	pane := onlyPaneOn(t, a)
	a.panes[pane].Note = "the channel would not close"

	a.shells[0].out <- []byte("\x1b]9;4;1;42\x07")
	waitFor(t, a, "the pane to take it in", func() bool {
		return pane.Progress().Percent == 42
	})
	a.refreshPanel(time.Now())

	if got := a.panes[pane].Note; got != "the channel would not close" {
		t.Errorf("the row now says %q", got)
	}
}

// How far along a program says it is, in a few words.
func TestHowFarAlongIsWordedPlainly(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	withDialogs(t, a)
	a.commands()

	for _, tc := range []struct{ sent, want string }{
		{"\x1b]9;4;1;7\x07", "7%"},
		{"\x1b]9;4;3\x07", "working"},
		{"\x1b]9;4;2;70\x07", "failed at 70%"},
		{"\x1b]9;4;2;0\x07", "failed"},
		{"\x1b]9;4;4;10\x07", "10%, warning"},
	} {
		a.shells[0].out <- []byte(tc.sent)
		waitFor(t, a, "the row to say "+tc.want, func() bool {
			a.refreshPanel(time.Now())
			return noteOnPaneRow(t, a) == tc.want
		})
	}
}

// A message is written to the window's log as well, so one that goes
// by while the user is looking elsewhere is still there to read.
func TestAMessageGoesIntoTheLog(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	withDialogs(t, a)
	a.commands()
	pane := onlyPaneOn(t, a)

	a.logNewNotice(pane, "the build finished", 1)
	a.logNewNotice(pane, "the build finished", 1)

	if got := a.noticed[pane]; got != 1 {
		t.Errorf("the pane is at message %d, want the first", got)
	}
	// Twice with the same number is one message, so it is written down
	// once.
	a.logNewNotice(pane, "and again", 2)
	if got := a.noticed[pane]; got != 2 {
		t.Errorf("a second message left the pane at %d", got)
	}
}

// A pane that has closed is forgotten, so a long-lived window does not
// keep a row for every pane it ever had.
func TestAClosedPaneIsForgotten(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	withDialogs(t, a)
	a.commands()
	pane := onlyPaneOn(t, a)
	a.logNewNotice(pane, "something", 1)
	if len(a.noticed) != 1 {
		t.Fatal("nothing was remembered about the pane")
	}

	if err := a.closePane(pane); err != nil {
		t.Fatalf("close it: %v", err)
	}
	a.forgetNotices()

	if len(a.noticed) != 0 {
		t.Errorf("%d panes that have closed are still remembered", len(a.noticed))
	}
}

// groupName is what the log calls this machine, so the line says which
// pane spoke.
func TestTheLogLineNamesTheMachine(t *testing.T) {
	if groupName(conns.Local) == "" {
		t.Error("this machine has no name for a log line to use")
	}
}
