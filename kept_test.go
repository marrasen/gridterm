package main

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/marrasen/gridterm/agent"
	"github.com/marrasen/gridterm/internal/sshtest"
	"github.com/marrasen/gridterm/meter"
	"github.com/marrasen/gridterm/ui"
	"github.com/marrasen/gridterm/ui/term"
)

// inTheTree reports whether a pane is still one of the window's leaves.
func inTheTree(a *testApp, pane ui.Widget) bool {
	for _, leaf := range ui.Leaves(a.root.Widget()) {
		if leaf == pane {
			return true
		}
	}
	return false
}

// endTheShell ends one of the fake shells and waits for the window to
// notice, which is what a user typing exit looks like from here.
func endTheShell(t *testing.T, a *testApp, which int, pane *term.Terminal) {
	t.Helper()
	if err := a.shells[which].Close(); err != nil {
		t.Fatalf("ending the shell: %v", err)
	}
	waitFor(t, a, "the window to see the shell end", func() bool {
		a.reapExited()
		return a.Ended(pane)
	})
}

// A shell that ends keeps its pane. The user asked for the pane, and
// what it printed is still worth reading once the shell has gone.
func TestAShellThatEndsKeepsItsPane(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	first, ok := onlyPaneWidget(t, a).(*term.Terminal)
	if !ok {
		t.Fatal("the window opened on something that is not a terminal")
	}
	if err := a.openTab(); err != nil {
		t.Fatalf("openTab: %v", err)
	}

	endTheShell(t, a, 0, first)

	if a.panes[first] == nil {
		t.Fatal("the pane went with its shell")
	}
	if !inTheTree(a, first) {
		t.Fatal("the pane was taken out of the tree with its shell")
	}
	if len(a.panes) != 2 {
		t.Fatalf("%d panes, want both", len(a.panes))
	}
	checkTree(t, a)
}

// The pane of a shell that ended can still be chosen from the sidebar
// and put in front, which is the only way to reach it again.
func TestAPaneWhoseShellEndedCanStillBeChosen(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	first, ok := onlyPaneWidget(t, a).(*term.Terminal)
	if !ok {
		t.Fatal("the window opened on something that is not a terminal")
	}
	if err := a.openTab(); err != nil {
		t.Fatalf("openTab: %v", err)
	}
	endTheShell(t, a, 0, first)

	e := a.panes[first]
	if e == nil {
		t.Fatal("the pane went with its shell")
	}
	// The other pane is in front, so choosing this one has to move it.
	if a.focusedTerminal() == first {
		t.Fatal("the pane whose shell ended is already in front")
	}
	if err := a.revealRow(paneRow(t, a, e)); err != nil {
		t.Fatalf("choosing the row: %v", err)
	}

	if a.focusedTerminal() != first {
		t.Fatal("choosing the row did not put the pane in front")
	}
	if a.showing() != e {
		t.Fatal("the stage is not showing the pane the sidebar chose")
	}
}

// What a shell printed is still readable once it has gone, including
// the lines that had scrolled off the top.
func TestAPaneWhoseShellEndedKeepsItsScrollback(t *testing.T) {
	a := newTestApp(t, 40, 6)
	withDialogs(t, a)
	withPanel(t, a)
	pane, ok := onlyPaneWidget(t, a).(*term.Terminal)
	if !ok {
		t.Fatal("the window opened on something that is not a terminal")
	}

	// More lines than the pane has rows, so the first ones scroll off.
	for i := 1; i <= 20; i++ {
		a.shells[0].out <- []byte(fmt.Sprintf("line %d\r\n", i))
	}
	waitFor(t, a, "the pane to show what the shell printed", func() bool {
		return strings.Contains(paneText(pane), "line 20")
	})

	endTheShell(t, a, 0, pane)

	read := pane.ReadLines(30)
	if !strings.Contains(read.Text, "line 20") {
		t.Errorf("the pane no longer holds the last line: %q", read.Text)
	}
	// A line the screen had already scrolled past: the scrollback half
	// of what was asked for.
	if !strings.Contains(read.Text, "line 3") {
		t.Errorf("the scrollback went with the shell: %q", read.Text)
	}
}

// The row of a pane whose shell ended says the shell has gone, the way
// every finished row on the sidebar says it: the mark and the name go
// grey.
func TestTheRowOfAPaneWhoseShellEndedSaysSo(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	first, ok := onlyPaneWidget(t, a).(*term.Terminal)
	if !ok {
		t.Fatal("the window opened on something that is not a terminal")
	}
	if err := a.openTab(); err != nil {
		t.Fatalf("openTab: %v", err)
	}
	running := paneRow(t, a, a.panes[first])

	endTheShell(t, a, 0, first)

	e := a.panes[first]
	if e == nil {
		t.Fatal("the pane went with its shell")
	}
	if got := e.State(time.Now()); got != meter.Closed {
		t.Fatalf("the row is %v, want it to say the shell finished", got)
	}
	drawn := paneRow(t, a, e)
	if drawn.FG != a.colours.ANSI[8] {
		t.Errorf("the row is drawn in %v, want the grey a finished row is drawn in", drawn.FG)
	}
	if drawn.MarkFG == running.MarkFG {
		t.Error("the mark in front of the row is drawn the same as a running one")
	}
	// And it can still be reached, or the pane could not be chosen.
	if e.Reveal == nil {
		t.Error("the row has nothing left to reveal")
	}
}

// Clearing the finished connections takes away a pane whose shell
// ended, the same as it takes away a command that finished.
func TestClearingFinishedTakesAwayAPaneWhoseShellEnded(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	first, ok := onlyPaneWidget(t, a).(*term.Terminal)
	if !ok {
		t.Fatal("the window opened on something that is not a terminal")
	}
	if err := a.openTab(); err != nil {
		t.Fatalf("openTab: %v", err)
	}
	endTheShell(t, a, 0, first)
	e := a.panes[first]

	if err := a.clearFinished(); err != nil {
		t.Fatalf("clearFinished: %v", err)
	}

	if a.panes[first] != nil {
		t.Fatal("clearing left the pane open with no row to reach it by")
	}
	if len(a.ended) != 0 {
		t.Fatalf("%d panes are still marked as finished", len(a.ended))
	}
	shown := panelText(a, time.Now())
	for _, row := range a.panel.Rows() {
		if row.Key == any(e) {
			t.Errorf("the row is still on the panel: %v", shown)
		}
	}
	checkTree(t, a)
}

// The close-pane command takes away a pane whose shell has ended, which
// is the ordinary way the user closes one.
func TestClosePaneTakesAwayAPaneWhoseShellEnded(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	a.commands()
	first, ok := onlyPaneWidget(t, a).(*term.Terminal)
	if !ok {
		t.Fatal("the window opened on something that is not a terminal")
	}
	if err := a.openTab(); err != nil {
		t.Fatalf("openTab: %v", err)
	}
	endTheShell(t, a, 0, first)
	a.focus(first)

	if err := a.root.Commands.Run("pane.close"); err != nil {
		t.Fatalf("pane.close: %v", err)
	}

	if a.panes[first] != nil {
		t.Fatal("the close-pane command left the pane open")
	}
	if inTheTree(a, first) {
		t.Fatal("the close-pane command left the pane in the tree")
	}
	checkTree(t, a)
}

// The window stays open when the last shell exits, and closes when the
// user then closes that pane.
//
// Typing exit in the only shell used to close the window. The pane now
// outlives the shell, so the window waits for the user to say they have
// read it.
func TestTheWindowWaitsForTheUserAfterTheLastShellExits(t *testing.T) {
	a := newTestApp(t, 40, 10)
	withDialogs(t, a)
	withPanel(t, a)
	only, ok := onlyPaneWidget(t, a).(*term.Terminal)
	if !ok {
		t.Fatal("the window opened on something that is not a terminal")
	}

	endTheShell(t, a, 0, only)

	if a.quit.Load() {
		t.Fatal("the window closed when the only shell exited")
	}
	if len(a.panes) != 1 {
		t.Fatalf("%d panes, want the one whose shell ended", len(a.panes))
	}

	if err := a.closePane(only); err != nil {
		t.Fatalf("close the pane: %v", err)
	}
	if !a.quit.Load() {
		t.Error("closing the last pane did not close the window")
	}
}

// A pane handed to an agent stays handed over when its shell ends.
//
// The listener stays up, the agent can still list the pane and read
// what it printed, and the session code still opens it. A shell that
// ended used to take the pane away, which took the handover, the
// listener and the code with it.
func TestAHandoverOutlivesTheShell(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	pane, code, c := handedOver(t, a)

	var got agent.Pane
	offWindow(t, a, "the window to answer the agent", func() error {
		var err error
		got, err = c.Use(code)
		return err
	})
	a.shells[0].out <- []byte("the last thing it said\r\n")
	waitFor(t, a, "the pane to show what the shell said", func() bool {
		return strings.Contains(paneText(pane), "the last thing it said")
	})

	endTheShell(t, a, 0, pane)

	if !a.agents.listening() {
		t.Fatal("the window stopped listening for agents when the shell ended")
	}
	if a.agents.of(pane) == nil {
		t.Fatal("the handover went with the shell")
	}
	// list_panes takes no pane and has to answer whatever became of the
	// shell.
	var listed []agent.Pane
	offWindow(t, a, "the window to list the agent's panes", func() error {
		var err error
		listed, err = c.Panes()
		return err
	})
	if len(listed) != 1 || listed[0].ID != got.ID {
		t.Fatalf("the agent was told it holds %v", listed)
	}
	// And what the pane printed is still there to read.
	var look agent.Look
	offWindow(t, a, "the window to answer the agent", func() error {
		var err error
		look, err = c.Read(got.ID, 0)
		return err
	})
	if !strings.Contains(look.Screen, "the last thing it said") {
		t.Errorf("the agent read %q", look.Screen)
	}
	if !look.Gone {
		t.Error("the agent was not told the program has finished")
	}

	// The code still names the pane, so an agent that comes back after
	// the shell went gets in.
	again, err := agent.Dial(code)
	if err != nil {
		t.Fatalf("dialling again with the same code: %v", err)
	}
	t.Cleanup(func() { _ = again.Close() })
	offWindow(t, a, "the window to answer the agent that came back", func() error {
		_, err := again.Use(code)
		return err
	})
}

// A pane whose connection dropped stays, the same as one whose shell
// exited: rebooting a server must not close the pane.
func TestAPaneWhoseConnectionDroppedStays(t *testing.T) {
	s := sshtest.New(t)
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)

	a.connect(serverConfig(t, s))
	waitForPanes(t, a, 2)
	host := serverConfig(t, s).Target()
	pane := paneFor(a, host)
	if pane == nil {
		t.Fatal("nothing opened on the machine")
	}

	// The far end goes, the way a server being rebooted goes.
	s.CloseClients()
	waitFor(t, a, "the window to see the machine go", func() bool {
		a.reapExited()
		return a.machines.named(host) == nil && a.Ended(pane)
	})

	if a.panes[pane] == nil {
		t.Fatal("the pane went with the connection")
	}
	if !inTheTree(a, pane) {
		t.Fatal("the pane was taken out of the tree with the connection")
	}
	if got := a.panes[pane].State(time.Now()); got != meter.Closed {
		t.Fatalf("the row is %v, want it to say the shell finished", got)
	}
	checkTree(t, a)

	// And the user can close it once they have read it.
	if err := a.closePane(pane); err != nil {
		t.Fatalf("close the pane: %v", err)
	}
	if a.panes[pane] != nil {
		t.Fatal("the pane would not close")
	}
}
