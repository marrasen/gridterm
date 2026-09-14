package main

import (
	"strings"
	"testing"
	"time"

	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/input"
	"github.com/marrasen/gridterm/internal/sshtest"
	"github.com/marrasen/gridterm/ui"
	"github.com/marrasen/gridterm/ui/term"
)

// splitChoices opens the split chooser and returns it.
func splitChoices(t *testing.T, a *testApp, dir ui.Dir) *ui.Chooser {
	t.Helper()
	if err := a.splitFocused(dir); err != nil {
		t.Fatalf("splitFocused: %v", err)
	}
	c, ok := a.root.Modal().(*ui.Chooser)
	if !ok {
		t.Fatalf("splitting showed %T, want a chooser", a.root.Modal())
	}
	return c
}

// choiceTexts is what a chooser is offering, one string per line.
func choiceTexts(c *ui.Chooser) []string {
	var out []string
	for _, row := range c.Rows() {
		out = append(out, strings.TrimSpace(row.Text))
	}
	return out
}

// takeChoice runs the line whose text contains want.
func takeChoice(t *testing.T, c *ui.Chooser, want string) {
	t.Helper()
	for i, text := range choiceTexts(c) {
		if strings.Contains(text, want) {
			if err := c.Take(i); err != nil {
				t.Fatalf("taking %q: %v", text, err)
			}
			return
		}
	}
	t.Fatalf("%q is not offered: %v", want, choiceTexts(c))
}

// otherTerminal returns the app's terminal that is not this one.
func otherTerminal(t *testing.T, a *testApp, except *term.Terminal) *term.Terminal {
	t.Helper()
	for pane := range a.panes {
		if pane != except {
			return pane
		}
	}
	t.Fatal("there is only one terminal")
	return nil
}

// Splitting asks what goes in the half that opens up, and leads with a
// shell here.
func TestSplittingAsksWhatGoesBesideIt(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)

	// Something else open, so being first is a place rather than the
	// only place there is.
	if err := a.openTab(); err != nil {
		t.Fatalf("openTab: %v", err)
	}

	c := splitChoices(t, a, ui.Columns)
	got := choiceTexts(c)
	if len(got) < 2 {
		t.Fatalf("it offers %v, too few for the order to mean anything", got)
	}
	if !strings.Contains(got[0], "New terminal") {
		t.Fatalf("it offers %v, want a new terminal first", got)
	}
	// Nothing has happened yet: the question is the whole of it.
	if len(a.panes) != 2 {
		t.Fatalf("%d panes while the question is still up", len(a.panes))
	}

	takeChoice(t, c, "New terminal")
	if len(a.panes) != 3 {
		t.Fatalf("%d panes after taking the first line", len(a.panes))
	}
	checkTree(t, a)
}

// A pane that is already open can be moved into the split rather than a
// second one being started.
func TestSplittingWithAPaneAlreadyOpen(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	if err := a.openTab(); err != nil {
		t.Fatalf("openTab: %v", err)
	}
	moved := a.focusedTerminal()
	first := otherTerminal(t, a, moved)
	a.focus(first)
	was := len(a.panes)

	c := splitChoices(t, a, ui.Columns)
	takeChoice(t, c, "Move")

	if got := len(a.panes); got != was {
		t.Fatalf("%d panes, want the %d there were: nothing new should start", got, was)
	}
	// Both are now in one split, and the stage holds that split alone.
	if got := len(a.stage.Children()); got != 1 {
		t.Fatalf("the stage holds %d things, want the one split", got)
	}
	split, ok := a.stage.Children()[0].(*ui.Split)
	if !ok {
		t.Fatalf("the stage holds %T, want a split", a.stage.Children()[0])
	}
	kids := split.Children()
	if len(kids) != 2 || kids[0] != ui.Widget(first) || kids[1] != ui.Widget(moved) {
		t.Fatalf("the split holds %v", kids)
	}
	checkTree(t, a)
}

// A pane moved into a split is taken out of where it was: left in both
// places it would be drawn twice and closed once.
func TestAPaneMovedIntoASplitLeavesItsOldPlace(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	if err := a.openTab(); err != nil {
		t.Fatalf("openTab: %v", err)
	}
	moved := a.focusedTerminal()
	first := otherTerminal(t, a, moved)
	a.focus(first)

	if err := a.splitWith(ui.Columns, first, moved); err != nil {
		t.Fatalf("splitWith: %v", err)
	}
	// Once, and only once: the stage holds the split and nothing else.
	var found int
	for _, leaf := range ui.Leaves(a.root.Widget()) {
		if leaf == ui.Widget(moved) {
			found++
		}
	}
	if found != 1 {
		t.Fatalf("the pane is in the tree %d times", found)
	}
	if got := len(a.stage.Children()); got != 1 {
		t.Fatalf("the stage holds %d things, want the one split", got)
	}
	checkTree(t, a)
}

// A pane cannot be split with itself, or with what it is inside: either
// would put a widget in the tree under itself.
func TestAPaneCannotBeSplitWithItself(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	if err := a.splitHere(ui.Columns); err != nil {
		t.Fatalf("split: %v", err)
	}
	pane := a.focusedTerminal()
	split := a.stage.Children()[0]

	if err := a.splitWith(ui.Columns, pane, pane); err == nil {
		t.Fatal("a pane was split with itself")
	}
	if err := a.splitWith(ui.Columns, pane, split); err == nil {
		t.Fatal("a pane was split with the split it is in")
	}
	if err := a.splitWith(ui.Columns, pane, nil); err == nil {
		t.Fatal("a pane was split with nothing")
	}
	checkTree(t, a)
}

// Every machine is offered, whether or not anything is connected, and
// the line says which costs a login.
func TestSplittingOffersEveryMachine(t *testing.T) {
	s := sshtest.New(t)
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	pinServers(t, a, s)
	saveHost(t, a, "margit", s, "")
	a.connectAs("live", serverConfig(t, s))
	waitForPanes(t, a, 2)

	c := splitChoices(t, a, ui.Columns)
	notes := map[string]string{}
	for _, row := range c.Rows() {
		notes[strings.TrimSpace(row.Text)] = row.Note
	}
	if _, ok := notes["Terminal on margit"]; !ok {
		t.Fatalf("it offers %v, missing the saved machine", choiceTexts(c))
	}
	if got := notes["Terminal on margit"]; got != "connects" {
		t.Errorf("a machine nothing is connected to says %q", got)
	}
	if got := notes["Terminal on live"]; got != "connected" {
		t.Errorf("a machine already connected says %q", got)
	}
}

// A terminal opened on a machine for a split lands in that split, not in
// a tab of its own.
func TestSplittingWithAMachineLandsInTheSplit(t *testing.T) {
	s := sshtest.New(t)
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	pinServers(t, a, s)
	saveHost(t, a, "margit", s, "")

	first := a.focusedTerminal()
	c := splitChoices(t, a, ui.Columns)
	takeChoice(t, c, "Terminal on margit")

	// The connection is made on another goroutine, so the pane arrives
	// later: the point of the test is that it still lands in the split.
	waitForPanes(t, a, 2)
	if got := len(a.stage.Children()); got != 1 {
		t.Fatalf("the stage holds %d things, want the one split", got)
	}
	split, ok := a.stage.Children()[0].(*ui.Split)
	if !ok {
		t.Fatalf("the terminal landed in %T rather than a split", a.stage.Children()[0])
	}
	kids := split.Children()
	if len(kids) != 2 || kids[0] != ui.Widget(first) {
		t.Fatalf("the split holds %v, want the pane that was split first", kids)
	}
	checkTree(t, a)
}

// A pane the split was meant for, closed while the connection was being
// made, leaves the terminal in a tab of its own rather than losing it.
func TestASplitWhosePaneWentOpensATabInstead(t *testing.T) {
	s := sshtest.New(t)
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	pinServers(t, a, s)

	if err := a.openTab(); err != nil {
		t.Fatalf("openTab: %v", err)
	}
	doomed := a.focusedTerminal()
	at := &spot{beside: doomed, dir: ui.Columns}
	if err := a.closePane(doomed); err != nil {
		t.Fatalf("closePane: %v", err)
	}

	a.openRoute("margit", []step{{name: "margit", cfg: serverConfig(t, s)}}, nil, at)
	waitForPanes(t, a, 2)
	checkTree(t, a)
	// It is on the stage, as a tab: losing the terminal because the pane
	// it was to sit beside has gone would be worse.
	for _, w := range a.stage.Children() {
		if _, split := w.(*ui.Split); split {
			t.Fatal("it split something that was not there")
		}
	}
}

// Unsplitting takes the focused pane out of its split and gives it a tab
// of its own. Nothing is closed.
func TestUnsplittingGivesAPaneItsOwnTab(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	if err := a.splitHere(ui.Columns); err != nil {
		t.Fatalf("split: %v", err)
	}
	out := a.focusedTerminal()
	was := len(a.panes)
	if got := len(a.stage.Children()); got != 1 {
		t.Fatalf("the stage holds %d things before the unsplit", got)
	}

	if err := a.unsplitFocused(); err != nil {
		t.Fatalf("unsplit: %v", err)
	}
	if got := len(a.panes); got != was {
		t.Fatalf("%d panes after unsplitting, want the %d there were", got, was)
	}
	kids := a.stage.Children()
	if len(kids) != 2 {
		t.Fatalf("the stage holds %d things, want the one that stayed and the one taken out",
			len(kids))
	}
	if kids[1] != ui.Widget(out) {
		t.Fatalf("the stage holds %v, want the pane taken out last", kids)
	}
	// The one left behind has the whole of the room the split had.
	if _, split := kids[0].(*ui.Split); split {
		t.Fatal("the split is still there with one pane in it")
	}
	if a.focusedTerminal() != out {
		t.Fatal("the keys are not on the pane that was taken out")
	}
	checkTree(t, a)
}

// A pane that is not in a split has nothing to be taken out of, and says
// so rather than doing something else.
func TestUnsplittingAPaneThatIsNotSplit(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)

	if err := a.unsplitFocused(); err == nil {
		t.Fatal("unsplitting a pane that is not in a split reported nothing")
	}
	if len(a.stage.Children()) != 1 {
		t.Fatalf("the stage holds %d things", len(a.stage.Children()))
	}
	checkTree(t, a)
}

// A file pane is part of the file manager, which holds panes and nothing
// else. Splitting one used to take the other pane out of the tree and
// then fail to put the split in.
func TestAFilePaneSaysItCannotBeSplit(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	if err := a.openFilesOn(conns.Local); err != nil {
		t.Fatalf("a file pane: %v", err)
	}
	a.focus(a.files.view.Panes()[0])

	err := a.splitFocused(ui.Columns)
	if err == nil {
		t.Fatal("splitting a file pane reported nothing")
	}
	if !strings.Contains(err.Error(), "file manager") {
		t.Fatalf("it said %q", err)
	}
	if _, asking := a.root.Modal().(*ui.Chooser); asking {
		t.Fatal("it asked what to split with before saying it could not")
	}
	checkTree(t, a)
}

// The sidebar has a row for each half of a split, and the bar follows
// whichever half has the keys.
func TestBothHalvesOfASplitAreOnTheSidebar(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	first := a.focusedTerminal()
	if err := a.splitHere(ui.Columns); err != nil {
		t.Fatalf("split: %v", err)
	}
	second := a.focusedTerminal()

	rows := panelText(a, time.Now())
	var terminals int
	for _, row := range rows {
		if strings.HasPrefix(row, string(terminalIcon)) {
			terminals++
		}
	}
	if terminals != 2 {
		t.Fatalf("the sidebar shows %v, want a row for each half", rows)
	}

	a.refreshPanel(panelNow)
	if got, _ := a.panel.Selected(); got.Key != any(a.panes[second]) {
		t.Fatal("the bar is not on the half that has the keys")
	}
	a.focus(first)
	a.refreshPanel(panelNow)
	if got, _ := a.panel.Selected(); got.Key != any(a.panes[first]) {
		t.Fatal("the bar did not follow the keys to the other half")
	}
}

// Escape leaves the tree exactly as it was.
func TestLeavingTheSplitQuestionChangesNothing(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	was := len(a.panes)

	c := splitChoices(t, a, ui.Columns)
	c.HandleKey(input.Event{Kind: input.KeyPress, Key: input.KeyEscape})

	if a.root.Modal() != nil {
		t.Fatalf("Escape left %T on the stack", a.root.Modal())
	}
	if got := len(a.panes); got != was {
		t.Fatalf("%d panes after leaving the question, want %d", got, was)
	}
	checkTree(t, a)
}
