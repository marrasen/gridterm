package main

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/marrasen/gridterm/agent"
	"github.com/marrasen/gridterm/conns"
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
		return endedAndSaid(a, pane)
	})
}

// endTheRemoteShell types the line the test server ends a session on and
// waits for the window to see that pane end.
func endTheRemoteShell(t *testing.T, a *testApp, pane *term.Terminal) {
	t.Helper()
	pane.Send([]byte("bye\n"))
	waitFor(t, a, "the window to see the remote shell end", func() bool {
		a.reapExited()
		return endedAndSaid(a, pane)
	})
}

// aConnectedWindow is a window with a shell open on a test server, and
// the name the server is held under.
func aConnectedWindow(t *testing.T, cols, rows int) (*testApp, *sshtest.Server, string) {
	t.Helper()
	s := sshtest.New(t)
	a := newTestApp(t, cols, rows)
	withDialogs(t, a)
	withPanel(t, a)
	a.connect(serverConfig(t, s))
	waitForPanes(t, a, 2)
	host := serverConfig(t, s).Target()
	if a.machines.named(host) == nil {
		t.Fatalf("nothing connected: %v", a.machines.names())
	}
	return a, s, host
}

// paneWithNoRow is a pane the window still holds with no row on the
// sidebar to reach it by, and nil when every pane has one.
func paneWithNoRow(a *testApp, now time.Time) *term.Terminal {
	listed := map[*conns.Entry]bool{}
	for _, group := range a.registry.Groups(now) {
		for _, row := range group.Rows {
			listed[row.Entry] = true
		}
	}
	for pane, e := range a.panes {
		if !listed[e] {
			return pane
		}
	}
	return nil
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
		got, err = firstOf(c.Use(code))
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

// wideLine is a line long enough to be cut by a narrower window, so a
// resize that reflowed the scrollback would be visible in it.
func wideLine(n int) string {
	return fmt.Sprintf("line %02d %s", n, strings.Repeat("x", 28))
}

// aPaneFullOfScrollback is a window whose one shell has printed more
// long lines than the screen holds, and has then ended.
func aPaneFullOfScrollback(t *testing.T) (*testApp, *term.Terminal) {
	t.Helper()
	// No sidebar: it takes columns off the pane, and these tests are
	// about what a column costs the scrollback.
	a := newTestApp(t, 40, 6)
	withDialogs(t, a)
	pane, ok := onlyPaneWidget(t, a).(*term.Terminal)
	if !ok {
		t.Fatal("the window opened on something that is not a terminal")
	}
	if got := pane.Size().Cols; got != 40 {
		t.Fatalf("the pane is %d columns wide, want the window's 40", got)
	}
	for i := 1; i <= 20; i++ {
		a.shells[0].out <- []byte(wideLine(i) + "\r\n")
	}
	waitFor(t, a, "the pane to show what the shell printed", func() bool {
		return strings.Contains(paneText(pane), wideLine(20))
	})
	endTheShell(t, a, 0, pane)
	return a, pane
}

// Making the window narrower and wide again leaves the scrollback of a
// pane whose shell ended exactly as it was.
//
// Every pane is laid out, not only the one in front, so a drag of the
// window edge reaches every pane the user has ever finished with. The
// emulator reflows the whole scrollback at the new width, and what is
// cut off does not come back when the window is made wide again.
func TestAResizeKeepsTheScrollbackOfAPaneWhoseShellEnded(t *testing.T) {
	a, pane := aPaneFullOfScrollback(t)
	was := pane.ReadLines(30).Text
	if !strings.Contains(was, wideLine(3)) {
		t.Fatalf("the scrollback did not survive the shell, so this proves nothing: %q", was)
	}

	// Narrower, then wide again, the way a window drag goes.
	a.setGridSize(20, 6)
	a.setGridSize(40, 6)

	got := pane.ReadLines(30).Text
	if !strings.Contains(got, wideLine(3)) {
		t.Errorf("the resize cut a scrollback line: %q", got)
	}
	if got != was {
		t.Errorf("the resize changed what the pane holds:\nwas %q\nnow %q", was, got)
	}
}

// The scrollback of a pane whose shell ended can be scrolled, and the
// screen it then draws shows a line from early on.
//
// Reading it the way an agent does proves the buffer is there. Marcus
// asked to be able to scroll back through it, which is the half a read
// does not touch.
func TestTheScrollbackOfAPaneWhoseShellEndedCanBeScrolled(t *testing.T) {
	a, pane := aPaneFullOfScrollback(t)
	if strings.Contains(paneText(pane), wideLine(1)) {
		t.Fatal("the first line is still on the screen, so this proves nothing")
	}

	pane.ScrollPages(10)

	if !strings.Contains(paneText(pane), wideLine(1)) {
		t.Errorf("scrolling back showed %q", paneText(pane))
	}
	// And it is still the window's pane, laid out where it always was.
	if !inTheTree(a, pane) {
		t.Error("the pane is no longer in the tree")
	}
}

// A pane whose program has finished says so on its own screen, and stops
// drawing a cursor.
//
// The row going grey is the whole of what the window said, and the
// sidebar can be hidden. Left alone, a dead pane looks exactly like a
// shell sitting at a prompt and swallows every keystroke in silence.
func TestAPaneSaysItsProgramHasFinished(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	a.commands()
	pane, ok := onlyPaneWidget(t, a).(*term.Terminal)
	if !ok {
		t.Fatal("the window opened on something that is not a terminal")
	}
	a.focus(pane)
	if !screenOf(pane).Cursor().Visible {
		t.Fatal("the pane draws no cursor while its shell is running, so this proves nothing")
	}

	endTheShell(t, a, 0, pane)

	said := paneText(pane)
	if !strings.Contains(said, "the program has finished") {
		t.Errorf("the pane says nothing about the program finishing: %q", said)
	}
	// And the last row asks what to do next, which is where closing the
	// pane is offered. The line above only records that the program
	// went, so naming a chord there would say it twice.
	if got := pane.Asking(); got == "" {
		t.Error("the pane asks nothing, so there is nothing to close it with")
	}
	if !strings.Contains(said, "Close") {
		t.Errorf("the pane draws no way to close it: %q", said)
	}
	if screenOf(pane).Cursor().Visible {
		t.Error("the pane still draws a cursor, so it looks like a shell at a prompt")
	}
	// Once, however many frames go by.
	a.reapExited()
	a.reapExited()
	if n := strings.Count(paneText(pane), "the program has finished"); n != 1 {
		t.Errorf("the line is on the screen %d times", n)
	}
}

// Clearing the finished connections between the meter closing and the
// window reaping the pane leaves no pane without a row.
//
// The meter closes on the goroutine reading the session, and the reap
// happens on the one that draws, so a pane really is finished for a
// while before it is reaped. Forced here rather than waited for.
func TestClearingFinishedBeforeTheReapLeavesNoPaneWithoutARow(t *testing.T) {
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

	// The shell goes and its meter closes with it. Nothing reaps.
	if err := a.shells[0].Close(); err != nil {
		t.Fatalf("ending the shell: %v", err)
	}
	waitFor(t, a, "the meter of the shell that went to close", func() bool {
		return a.panes[first].State(time.Now()) == meter.Closed
	})
	if a.Ended(first) {
		t.Fatal("the pane was reaped, so this proves nothing")
	}

	if err := a.clearFinished(); err != nil {
		t.Fatalf("clearFinished: %v", err)
	}

	if pane := paneWithNoRow(a, time.Now()); pane != nil {
		t.Errorf("a pane is open with no row on the sidebar to reach it by: %q",
			paneText(pane))
	}
	checkTree(t, a)
}

// A pane handed to an agent whose connection then drops is still named
// by its code and still readable, and its row stops saying an agent is
// working in it.
//
// The hand-over outliving the program and the connection dropping have
// been tested apart. Crossed is what Marcus hit.
func TestAHandoverOnARemotePaneOutlivesTheConnection(t *testing.T) {
	a, s, host := aConnectedWindow(t, 90, 30)
	pane := paneFor(a, host)
	if pane == nil {
		t.Fatal("nothing opened on the machine")
	}
	waitFor(t, a, "the machine to say something", func() bool {
		return strings.Contains(paneText(pane), "READY")
	})
	if err := a.handPane(pane); err != nil {
		t.Fatalf("hand it over: %v", err)
	}
	h := a.agents.of(pane)
	if h == nil {
		t.Fatal("the window did not record the handover")
	}
	c, err := agent.Dial(a.agents.code())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	var got agent.Pane
	offWindow(t, a, "the window to answer the agent", func() error {
		var err error
		got, err = firstOf(c.Use(a.agents.code()))
		return err
	})

	// The far end goes, the way a server being rebooted goes.
	s.CloseClients()
	waitFor(t, a, "the window to see the machine go", func() bool {
		a.reapExited()
		return a.machines.named(host) == nil && a.Ended(pane)
	})

	if a.agents.of(pane) == nil {
		t.Fatal("the handover went with the connection")
	}
	// The code still names the pane, and says plainly that there is
	// nothing left to type into.
	var again agent.Pane
	offWindow(t, a, "the window to answer the agent again", func() error {
		var err error
		again, err = firstOf(c.Use(a.agents.code()))
		return err
	})
	if again.ID != got.ID {
		t.Errorf("the code now names %q, want the pane it named before", again.ID)
	}
	if !again.Ended {
		t.Error("the agent was not told the program has finished")
	}
	// And what the machine printed is still there to read.
	var look agent.Look
	offWindow(t, a, "the window to answer the agent", func() error {
		var err error
		look, err = c.Read(got.ID, 0)
		return err
	})
	if !strings.Contains(look.Screen, "READY") {
		t.Errorf("the agent read %q", look.Screen)
	}
	// The row no longer says somebody is working in it: nothing is.
	a.refreshPanel(time.Now())
	if note := a.panes[pane].Note; isAgentNote(note) {
		t.Errorf("the row of a finished pane says %q", note)
	}
}

// Closing a connection leaves the transcript of a pane that had already
// ended on it.
//
// A pane with nothing running is not on the connection any more. The
// user exited a shell, was told the pane is kept, and closing the
// connection an hour later must not take it back.
func TestClosingAConnectionKeepsAPaneThatAlreadyEnded(t *testing.T) {
	a, _, host := aConnectedWindow(t, 80, 24)
	dead := paneFor(a, host)
	if dead == nil {
		t.Fatal("nothing opened on the machine")
	}
	// A second shell on the same connection, so closing it has
	// something live to take.
	if err := a.openOn(host, nil, nil); err != nil {
		t.Fatalf("a second terminal: %v", err)
	}
	waitForPanes(t, a, 3)
	endTheRemoteShell(t, a, dead)
	was := dead.ReadLines(30).Text

	if err := a.dropMachine(host); err != nil {
		t.Fatalf("close the connection: %v", err)
	}

	if a.panes[dead] == nil {
		t.Fatal("closing the connection took the pane of the shell that had already ended")
	}
	if !inTheTree(a, dead) {
		t.Fatal("closing the connection took the pane out of the tree")
	}
	if got := dead.ReadLines(30).Text; got != was {
		t.Errorf("the transcript changed:\nwas %q\nnow %q", was, got)
	}
	checkTree(t, a)
}

// Choosing a machine's row puts a pane still running in front, not one
// whose shell has ended.
func TestChoosingAMachineRowPutsALivePaneInFront(t *testing.T) {
	a, _, host := aConnectedWindow(t, 80, 24)
	dead := paneFor(a, host)
	if dead == nil {
		t.Fatal("nothing opened on the machine")
	}
	if err := a.openOn(host, nil, nil); err != nil {
		t.Fatalf("a second terminal: %v", err)
	}
	waitForPanes(t, a, 3)
	m := a.machines.named(host)
	var live *term.Terminal
	for _, pane := range a.machines.panesOn(m) {
		if pane != dead {
			live = pane
		}
	}
	if live == nil {
		t.Fatal("the second terminal is not on the connection")
	}
	endTheRemoteShell(t, a, dead)

	// Asked over and over: the record of what runs where is a map, so
	// one try could land on the live pane by luck.
	for try := 1; try <= 20; try++ {
		a.focus(dead)
		a.revealMachine(m)
		if a.focusedTerminal() != live {
			t.Fatalf("try %d put the pane whose shell ended in front", try)
		}
	}
}

// A pane whose shell ended stays when the pane it was split with is
// closed, and the window stays open with only dead panes left.
func TestADeadPaneInASplitOutlivesItsNeighbour(t *testing.T) {
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
	second := a.focusedTerminal()
	if second == nil || second == first {
		t.Fatal("the tab opened no second pane")
	}
	// The pane being divided has to be the one showing, or there is no
	// room measured for it.
	a.focus(first)
	if err := a.splitWith(ui.Columns, first, second); err != nil {
		t.Fatalf("splitWith: %v", err)
	}

	endTheShell(t, a, 0, first)
	if err := a.closePane(second); err != nil {
		t.Fatalf("close the live pane: %v", err)
	}

	if a.panes[first] == nil {
		t.Fatal("closing the live pane took the dead one with it")
	}
	if !inTheTree(a, first) {
		t.Fatal("the dead pane was left out of the tree")
	}
	if a.quit.Load() {
		t.Error("the window closed while a pane that had ended was still open")
	}
	checkTree(t, a)
}

// Connecting to a machine again works while the panes of the last
// connection are still open, and those panes say what became of them.
func TestReconnectingToAMachineThatStillHasDeadPanes(t *testing.T) {
	a, s, host := aConnectedWindow(t, 80, 24)
	dead := paneFor(a, host)
	if dead == nil {
		t.Fatal("nothing opened on the machine")
	}

	// The far end goes, the way a server being rebooted goes.
	s.CloseClients()
	waitFor(t, a, "the window to see the machine go", func() bool {
		a.reapExited()
		return a.machines.named(host) == nil && a.Ended(dead)
	})
	// The pane's own row says the connection went, not that a shell
	// exited: the machine's row carries the reason and the user can
	// clear it.
	a.refreshPanel(time.Now())
	if got := a.panes[dead].Label; got != transportLost {
		t.Errorf("the row of the pane says %q, want %q", got, transportLost)
	}

	a.connect(serverConfig(t, s))
	waitForPanes(t, a, 3)

	m := a.machines.named(host)
	if m == nil {
		t.Fatalf("the machine would not connect again: %v", a.machines.names())
	}
	live := paneFor(a, host)
	if live == nil || live == dead {
		t.Fatal("the new connection opened no pane of its own")
	}
	if a.panes[dead] == nil {
		t.Fatal("connecting again took the pane of the last connection")
	}
	// And the new connection does not own the old pane either.
	if err := a.dropMachine(host); err != nil {
		t.Fatalf("close the new connection: %v", err)
	}
	if a.panes[dead] == nil {
		t.Fatal("closing the new connection took the old pane")
	}
	checkTree(t, a)
}

// A pane whose program has finished can be handed to an agent: reading
// it is worth something, and typing into it is refused where the typing
// happens.
func TestAPaneWhoseProgramHasEndedCanBeHandedOver(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	pane := onlyPaneOn(t, a)
	a.shells[0].out <- []byte("what it left behind\r\n")
	waitFor(t, a, "the pane to show what the shell said", func() bool {
		return strings.Contains(paneText(pane), "what it left behind")
	})
	endTheShell(t, a, 0, pane)

	if err := a.handPane(pane); err != nil {
		t.Fatalf("hand over a pane whose program has finished: %v", err)
	}
	h := a.agents.of(pane)
	if h == nil {
		t.Fatal("the window did not record the handover")
	}
	c, err := agent.Dial(a.agents.code())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	var got agent.Pane
	offWindow(t, a, "the window to answer the agent", func() error {
		var err error
		got, err = firstOf(c.Use(a.agents.code()))
		return err
	})
	if !got.Ended {
		t.Error("the agent was not told the program has finished")
	}

	var look agent.Look
	offWindow(t, a, "the window to answer the agent", func() error {
		var err error
		look, err = c.Read(got.ID, 0)
		return err
	})
	if !strings.Contains(look.Screen, "what it left behind") {
		t.Errorf("the agent read %q", look.Screen)
	}

	// And typing is still refused, which is what makes handing it over
	// safe.
	done := make(chan error, 1)
	go func() { done <- c.Send(got.ID, "rm -rf /\r", nil) }()
	waitFor(t, a, "the window to answer", func() bool { return len(done) > 0 })
	if err := <-done; err == nil {
		t.Error("the agent typed into a pane whose program has finished")
	}
}
