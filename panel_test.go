package main

import (
	"image/color"
	"strings"
	"testing"
	"time"

	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/input"
	"github.com/marrasen/gridterm/meter"
	"github.com/marrasen/gridterm/ui"
	"github.com/marrasen/gridterm/ui/term"
)

// now is a fixed moment, so nothing here waits four seconds to watch a
// connection settle.
var panelNow = time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)

// withPanel gives a test app the panel and the dock, the way main does.
func withPanel(t *testing.T, a *testApp) {
	t.Helper()
	a.panel = a.newPanel()
	a.side = a.newSidebar()
	a.dock = ui.NewDock(panelWidth, a.side, a.root.Widget())
	a.root.SetWidget(a.dock)
	a.relayout()
}

// panelText returns what the panel is showing, one line per row, with
// the note in brackets.
func panelText(a *testApp, now time.Time) []string {
	a.refreshPanel(now)
	rows := a.panel.Rows()
	out := make([]string, len(rows))
	for i, row := range rows {
		out[i] = strings.TrimSpace(row.Text)
		if row.Note != "" {
			out[i] += " [" + row.Note + "]"
		}
	}
	return out
}

// panelMarks returns the colour of the dot in front of each row, for the
// rows that have one.
func panelMarks(a *testApp, now time.Time) []color.RGBA {
	a.refreshPanel(now)
	var out []color.RGBA
	for _, row := range a.panel.Rows() {
		if row.Mark != 0 {
			out = append(out, row.MarkFG)
		}
	}
	return out
}

// onlyPane returns the app's single terminal and its panel entry.
func onlyPane(t *testing.T, a *testApp) *conns.Entry {
	t.Helper()
	if len(a.panes) != 1 {
		t.Fatalf("%d panes, want 1", len(a.panes))
	}
	for _, e := range a.panes {
		return e
	}
	return nil
}

// A pane that opens gets a row, under the machine it is running on.
func TestPanelShowsTheShellThatIsOpen(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)

	got := panelText(a, panelNow)
	if len(got) != 2 {
		t.Fatalf("the panel shows %v, want a heading and a row", got)
	}
	if got[0] != "Local" {
		t.Errorf("the heading is %q, want Local", got[0])
	}
	if !strings.HasPrefix(got[1], "Terminal") {
		t.Errorf("the row is %q, want a terminal", got[1])
	}
}

// The four states, seen through the panel: opened until something
// moves, active for four seconds, settled after that, closed at the end.
func TestPanelShowsTheFourStates(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	e := onlyPane(t, a)

	green := a.colours.ANSI[2]
	grey := a.colours.ANSI[8]

	if got := panelMarks(a, panelNow)[0]; got != green {
		t.Fatalf("a row with nothing moved is %v, want the green of something that is there", got)
	}

	// While bytes are going past the dot brightens and dims, so it is
	// green but not the steady green of a row that is only there.
	e.Meter.Moved(64, 0, panelNow)
	busy := panelMarks(a, panelNow)[0]
	if busy == grey {
		t.Fatal("a busy row is grey")
	}
	var moved bool
	for step := 0; step < 8; step++ {
		at := panelNow.Add(time.Duration(step) * pulseStep)
		if panelMarks(a, at)[0] != busy {
			moved = true
		}
	}
	if !moved {
		t.Fatal("the dot does not move while bytes are going past")
	}

	// Nothing is told to change it: the same panel, asked about a later
	// moment, is the steady green again.
	later := panelNow.Add(meter.Settle)
	if got := panelMarks(a, later)[0]; got != green {
		t.Fatalf("a settled row is %v, want the steady green", got)
	}

	e.Meter.Close()
	if got := panelMarks(a, later)[0]; got != grey {
		t.Fatalf("a finished row is %v, want grey", got)
	}
}

// What a row is doing is the dot's business, so the words beside it are
// only what the dot cannot say.
func TestPanelSaysNoStateInWords(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	e := onlyPane(t, a)

	for _, state := range []string{"opened", "active", "settled", "closed"} {
		if got := panelText(a, panelNow)[1]; strings.Contains(got, state) {
			t.Fatalf("the row says %q in words", state)
		}
		e.Meter.Moved(1, 0, panelNow)
	}
	e.Meter.Close()
	if got := panelText(a, panelNow)[1]; strings.Contains(got, "closed") {
		t.Fatalf("the row says %q", got)
	}
}

// A connection moving a lot says how fast rather than just that it is
// busy.
func TestPanelShowsASpeedWhileBytesAreMoving(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	e := onlyPane(t, a)

	e.Meter.Moved(1, 0, panelNow)
	panelText(a, panelNow) // the first sample, with nothing to compare to
	e.Meter.Moved(64*1024, 0, panelNow.Add(time.Second))

	got := panelText(a, panelNow.Add(time.Second))[1]
	if !strings.Contains(got, "/s") {
		t.Fatalf("row = %q, want a speed on it", got)
	}
}

// What the program in a pane called the window is what the panel shows.
func TestPanelShowsWhatTheProgramCalledItself(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	var pane *term.Terminal
	for p := range a.panes {
		pane = p
	}
	a.setTitle(t, 0, pane, "vim README.md")

	got := panelText(a, panelNow)[1]
	if !strings.Contains(got, "vim README.md") {
		t.Fatalf("row = %q, want the title in it", got)
	}
}

// A machine with nothing open on it is not a heading worth keeping, and
// this machine is always there so a local shell has somewhere to go.
func TestPanelGroupsByMachine(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)

	e := &conns.Entry{Host: "margit", Kind: conns.Tunnel, Label: ":5432", Meter: meter.New()}
	a.registry.Add(e)
	got := panelText(a, panelNow)
	if len(got) != 4 {
		t.Fatalf("the panel shows %v, want two headings and two rows", got)
	}
	if got[2] != "margit" {
		t.Errorf("the second heading is %q, want margit", got[2])
	}
	if !strings.Contains(got[3], ":5432") {
		t.Errorf("the tunnel row is %q", got[3])
	}

	a.registry.Drop(e)
	if got := panelText(a, panelNow); len(got) != 2 {
		t.Fatalf("the panel shows %v after the tunnel went, want the machine gone too", got)
	}
}

// Closing a pane takes its row with it, and nothing is left holding the
// rate it was measured with.
func TestPanelRowGoesWithItsPane(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	if err := a.openTab(); err != nil {
		t.Fatalf("openTab: %v", err)
	}
	if got := panelText(a, panelNow); len(got) != 3 {
		t.Fatalf("the panel shows %v, want a heading and two rows", got)
	}

	// Give each a rate to remember.
	for _, e := range a.panes {
		e.Meter.Moved(1, 0, panelNow)
	}
	panelText(a, panelNow)

	if err := a.closeFocused(); err != nil {
		t.Fatalf("closeFocused: %v", err)
	}
	if got := panelText(a, panelNow); len(got) != 2 {
		t.Fatalf("the panel shows %v after a pane closed", got)
	}
	if len(a.rates) > len(a.panes) {
		t.Fatalf("%d rates are held for %d panes", len(a.rates), len(a.panes))
	}
}

// A shell that has gone stays on the panel: what a command did after it
// stopped is worth reading. Clearing is what takes it off.
func TestPanelKeepsAFinishedConnectionUntilItIsCleared(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	e := onlyPane(t, a)
	e.Meter.Close()

	if got := panelText(a, panelNow); len(got) != 2 {
		t.Fatalf("the panel shows %v, want the finished row kept", got)
	}
	if err := a.clearFinished(); err != nil {
		t.Fatalf("clearFinished: %v", err)
	}
	if got := panelText(a, panelNow); len(got) != 1 {
		t.Fatalf("the panel shows %v after clearing, want the heading alone", got)
	}
}

// Choosing a row puts what it names in front of the user.
func TestPanelActivatingARowRevealsIt(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	if err := a.openTab(); err != nil {
		t.Fatalf("openTab: %v", err)
	}
	panelText(a, panelNow)

	// The first pane, which is not the one with focus.
	first := a.panel.Rows()[1]
	want, ok := first.Key.(*conns.Entry)
	if !ok {
		t.Fatal("the row does not name a connection")
	}
	a.panel.Select(want)
	if err := a.revealRow(first); err != nil {
		t.Fatalf("reveal: %v", err)
	}

	got := a.panes[a.focusedTerminal()]
	if got != want {
		t.Fatal("choosing a row did not focus the pane it names")
	}
}

// The panel can close what it has selected, which is how a connection
// with no pane of its own is ever ended.
func TestPanelClosesTheSelectedConnection(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	if err := a.openTab(); err != nil {
		t.Fatalf("openTab: %v", err)
	}
	panelText(a, panelNow)

	row := a.panel.Rows()[1]
	e := row.Key.(*conns.Entry)
	a.panel.Select(e)
	if err := a.closeSelectedConnection(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if len(a.panes) != 1 {
		t.Fatalf("%d panes, want the other one closed", len(a.panes))
	}
	checkTree(t, a)
}

// A connection the panel cannot close says so rather than doing nothing.
func TestPanelSaysWhenAConnectionCannotBeClosed(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	e := &conns.Entry{Host: "margit", Kind: conns.Tunnel, Label: "held open"}
	a.registry.Add(e)
	panelText(a, panelNow)

	a.panel.Select(e)
	if err := a.closeSelectedConnection(); err == nil {
		t.Fatal("a connection with no way to close reported success")
	}
}

func TestPanelOpensAndCloses(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	a.dock.Collapsed = true

	if err := a.togglePanel(); err != nil {
		t.Fatalf("toggle: %v", err)
	}
	if a.dock.Collapsed {
		t.Fatal("the panel did not open")
	}
	if err := a.togglePanel(); err != nil {
		t.Fatalf("toggle: %v", err)
	}
	if !a.dock.Collapsed {
		t.Fatal("the panel did not close")
	}
}

// A hidden panel is not worth building. The window draws sixty times a
// second and nobody can see it.
func TestPanelBuildsNothingWhileHidden(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	if got := panelText(a, panelNow); len(got) == 0 {
		t.Fatal("the panel showed nothing while open")
	}

	a.dock.Collapsed = true
	a.panel.SetRows(nil)
	a.refreshPanel(panelNow)
	if got := a.panel.Rows(); len(got) != 0 {
		t.Fatalf("a hidden panel built %d rows", len(got))
	}

	// And it comes back the moment it is shown.
	if err := a.showPanel(true); err != nil {
		t.Fatalf("showPanel: %v", err)
	}
	if got := panelText(a, panelNow); len(got) == 0 {
		t.Fatal("the panel stayed empty after it was shown")
	}
}

// Going to the panel opens it first: a command that puts the keys
// somewhere invisible is a command that loses them.
func TestFocusPanelOpensItFirst(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	if err := a.focusPanel(); err != nil {
		t.Fatalf("focusPanel: %v", err)
	}
	if a.dock.Collapsed {
		t.Fatal("the panel is still hidden")
	}
	if a.dock.Focused() != ui.Widget(a.side) {
		t.Fatal("the keys did not go to the panel")
	}
}

// The panel takes the arrow keys while it has the focus, and the pane
// underneath does not see them.
func TestPanelTakesTheKeysWhileItHasThem(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	if err := a.openTab(); err != nil {
		t.Fatalf("openTab: %v", err)
	}
	panelText(a, panelNow)
	if err := a.focusPanel(); err != nil {
		t.Fatalf("focusPanel: %v", err)
	}

	first, _ := a.panel.Selected()
	if _, err := a.root.HandleKey(press(input.KeyDown, 0)); err != nil {
		t.Fatalf("down: %v", err)
	}
	second, _ := a.panel.Selected()
	if first.Key == second.Key {
		t.Fatal("Down did not move the panel selection")
	}
	// And the shell was not typed into.
	for _, shell := range a.shells {
		if shell.sentText() != "" {
			t.Fatalf("the shell was sent %q while the panel had the keys", shell.sentText())
		}
	}
}

// An idle panel leaves its layer alone, which is what keeps a window
// with nothing happening from redrawing itself sixty times a second.
func TestPanelDoesNotDirtyAnIdleFrame(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	e := onlyPane(t, a)
	e.Meter.Moved(10, 0, panelNow)

	a.refreshPanel(panelNow)
	a.root.Draw(a.g.View())
	a.g.ClearDirty()

	// A second frame at the same moment changes nothing.
	a.refreshPanel(panelNow)
	a.root.Draw(a.g.View())
	if a.g.AnyDirty() {
		t.Fatal("an idle panel dirtied the layer")
	}

	// And the moment the state changes, it does: the dot goes from the
	// one that moves to the one that does not.
	a.refreshPanel(panelNow.Add(meter.Settle))
	a.root.Draw(a.g.View())
	if !a.g.AnyDirty() {
		t.Fatal("a row that changed from active to settled did not redraw")
	}
}

// The bytes a shell actually sends are what makes its row active. The
// other state tests drive the meter directly; this one goes through the
// session the terminal is reading.
func TestPanelSeesBytesFromTheShellItself(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	e := onlyPane(t, a)

	if got := e.State(time.Now()); got != meter.Opened {
		t.Fatalf("a shell that has said nothing is %v, want opened", got)
	}
	a.shells[0].out <- []byte("hello")
	waitUntil(t, func() bool { return e.State(time.Now()) == meter.Active })
}

// A shell that has gone says so rather than settling and looking merely
// quiet.
func TestPanelSeesTheShellGo(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	e := onlyPane(t, a)

	a.shells[0].Close()
	waitUntil(t, func() bool { return e.State(time.Now()) == meter.Closed })
}

// Enter on the panel has to do something. Calling revealRow by hand
// tests the function, not that anything is wired to it.
func TestPanelEnterRevealsThroughTheList(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	if err := a.openTab(); err != nil {
		t.Fatalf("openTab: %v", err)
	}
	panelText(a, panelNow)
	if err := a.focusPanel(); err != nil {
		t.Fatalf("focusPanel: %v", err)
	}

	// The first row is the pane that does not have focus.
	first := a.panel.Rows()[1]
	want := first.Key.(*conns.Entry)
	a.panel.Select(want)
	if _, err := a.root.HandleKey(press(input.KeyEnter, 0)); err != nil {
		t.Fatalf("enter: %v", err)
	}
	if got := a.panes[a.focusedTerminal()]; got != want {
		t.Fatal("Enter on the panel did not focus the pane the row names")
	}
}

// A connection that is still being made says so, in its own words.
func TestPanelSaysAConnectionIsOnItsWay(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	a.registry.Add(&conns.Entry{
		Host: "margit", Kind: conns.Terminal, Label: "connecting", Note: "opening",
	})

	var found bool
	for _, row := range panelText(a, panelNow) {
		if strings.Contains(row, "connecting") && strings.Contains(row, "[opening]") {
			found = true
		}
	}
	if !found {
		t.Fatalf("the panel shows %v, want a row saying it is opening", panelText(a, panelNow))
	}
}

// A finished row has to read as finished rather than as one more thing
// running.
func TestPanelDimsAFinishedRow(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	e := onlyPane(t, a)

	a.refreshPanel(panelNow)
	running := a.panel.Rows()[1].FG

	e.Meter.Close()
	a.refreshPanel(panelNow)
	finished := a.panel.Rows()[1].FG

	if finished == running {
		t.Fatal("a finished row is drawn the same as a running one")
	}
	if finished.A == 0 {
		t.Fatal("a finished row has no colour of its own")
	}
}

// A shell that ends on its own leaves its row behind, saying what it
// did. A pane the user closed takes its row with it: they know.
func TestPanelKeepsTheRowOfAShellThatEndedOnItsOwn(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	if err := a.openTab(); err != nil {
		t.Fatalf("openTab: %v", err)
	}
	panelText(a, panelNow)

	// The shell on the first pane goes.
	a.shells[0].Close()
	waitUntil(t, func() bool {
		a.reapExited()
		return len(a.panes) == 1
	})

	var closed int
	for _, mark := range panelMarks(a, panelNow) {
		if mark == a.colours.ANSI[8] {
			closed++
		}
	}
	if closed != 1 {
		t.Fatalf("the panel shows %v, want the shell that ended left behind",
			panelText(a, panelNow))
	}

	// And clearing takes it off.
	if err := a.clearFinished(); err != nil {
		t.Fatalf("clearFinished: %v", err)
	}
	for _, row := range panelText(a, panelNow) {
		if strings.Contains(row, "[closed]") {
			t.Fatalf("clearing left %q", row)
		}
	}
}

// The close-pane key with the panel focused used to detach the panel and
// leave the window with no way to get it back.
func TestClosePaneWillNotTakeThePanelOutOfTheTree(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	if err := a.focusPanel(); err != nil {
		t.Fatalf("focusPanel: %v", err)
	}

	if err := a.closeFocused(); err != nil {
		t.Fatalf("closeFocused: %v", err)
	}
	if a.dock.Panel() != ui.Widget(a.side) {
		t.Fatal("the panel was taken out of the dock")
	}
	if len(a.panes) != 1 {
		t.Fatalf("%d panes, want the one that was there", len(a.panes))
	}
	// And the panel still works.
	if err := a.togglePanel(); err != nil {
		t.Fatalf("toggle: %v", err)
	}
}

// A window too narrow for the panel must not take the keys to somewhere
// that is never drawn: the keyboard would stop working with no
// explanation.
func TestFocusPanelRefusesAWindowWithNoRoom(t *testing.T) {
	a := newTestApp(t, 30, 10)
	withPanel(t, a)
	a.relayout()
	if _, shown := a.dock.ChildArea(a.panel); shown {
		t.Skip("this window has room for the panel after all")
	}

	if err := a.focusPanel(); err == nil {
		t.Fatal("the keys went to a panel with no room to be drawn")
	}
	if a.dock.Focused() == ui.Widget(a.side) {
		t.Fatal("the panel has the keys and is not on screen")
	}
}

// Clicking a terminal takes the keys back from the panel. Without it the
// user types into something they are not looking at.
func TestClickingATerminalTakesTheKeysBack(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	if err := a.focusPanel(); err != nil {
		t.Fatalf("focusPanel: %v", err)
	}
	if a.dock.Focused() != ui.Widget(a.side) {
		t.Fatal("the panel does not have the keys")
	}

	area, shown := a.dock.ChildArea(a.dock.Rest())
	if !shown {
		t.Fatal("the rest of the window is not drawn")
	}
	if _, err := a.root.HandleMouse(input.MouseEvent{
		Kind: input.MousePress, Button: input.MouseLeft,
		Col: area.X + 2, Row: area.Y + 2,
	}); err != nil {
		t.Fatalf("click: %v", err)
	}
	if a.dock.Focused() == ui.Widget(a.side) {
		t.Fatal("clicking the terminal left the keys on the panel")
	}

	// And typing reaches the shell again.
	a.root.HandleKey(input1('x'))
	waitUntil(t, func() bool { return strings.Contains(a.shells[0].sentText(), "x") })
}

// A speed shown when a connection wakes up is the speed now, not the
// average over however long it was quiet.
func TestPanelSpeedIsNotAveragedOverTheQuietSpell(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	e := onlyPane(t, a)

	e.Meter.Moved(1<<20, 0, panelNow)
	panelText(a, panelNow)

	// A minute of nothing, with the panel drawing throughout.
	quiet := panelNow.Add(time.Minute)
	for at := panelNow.Add(time.Second); !at.After(quiet); at = at.Add(time.Second) {
		panelText(a, at)
	}

	// And then a megabyte in one second.
	e.Meter.Moved(1<<20, 0, quiet.Add(time.Second))
	got := panelText(a, quiet.Add(time.Second))[1]
	if !strings.Contains(got, "1.0 MB/s") {
		t.Fatalf("row = %q, want 1.0 MB/s: the quiet spell was averaged in", got)
	}
}

// The sidebar has a ground of its own, shading down the list, so it
// reads as part of the window's frame rather than as one more thing
// running in it.
func TestThePanelHasItsOwnGround(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)

	if a.panel.Style.BG == a.colours.BG {
		t.Fatal("the sidebar is the same colour as everything else")
	}
	if a.panel.Style.BGEnd == a.panel.Style.BG {
		t.Fatal("the sidebar does not shade at all")
	}

	// And what it draws really is two colours, top and bottom.
	a.refreshPanel(panelNow)
	a.root.Draw(a.g.View())
	area, shown := a.root.AreaOf(a.side)
	if !shown {
		t.Fatal("the sidebar is not on screen")
	}
	top := a.g.At(area.X, area.Y)
	bottom := a.g.At(area.X, area.Y+area.Rows-1)
	if top.BG == bottom.BG {
		t.Fatalf("the sidebar is one flat colour: %v", top.BG)
	}
}
