package main

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/input"
	"github.com/marrasen/gridterm/internal/sshtest"
	"github.com/marrasen/gridterm/remote"
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
//
// By the line's own key rather than by where it sits: a heading is a row
// and not a line to take, so the two stopped counting alike.
func takeChoice(t *testing.T, c *ui.Chooser, want string) {
	t.Helper()
	for _, row := range c.Rows() {
		if row.Header || !strings.Contains(strings.TrimSpace(row.Text), want) {
			continue
		}
		at, ok := row.Key.(int)
		if !ok {
			t.Fatalf("the line %q has no place to take", row.Text)
		}
		if err := c.Take(at); err != nil {
			t.Fatalf("taking %q: %v", row.Text, err)
		}
		return
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

// The split chooser offers a line per shell this machine has, the way
// the plus on its row does.
func TestSplittingOffersEveryShell(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	scanShells(t, a)

	c := splitChoices(t, a, ui.Columns)
	got := choiceTexts(c)
	for _, want := range []string{"Command Prompt", "PowerShell", "Ubuntu (WSL)"} {
		if !slices.Contains(got, want) {
			t.Errorf("it offers %v, missing %q", got, want)
		}
	}
	// Under the line that opens a terminal here, the way the plus
	// arranges them.
	if got[0] != "New terminal" {
		t.Fatalf("the chooser opens on %q, want the terminal line", got[0])
	}
	if got[1] != "Command Prompt" {
		t.Errorf("the line after the terminal is %q, want the first shell", got[1])
	}
	// A file pane belongs to the file manager and is split inside it,
	// which is the one place this chooser and that menu differ.
	for _, line := range got {
		if strings.Contains(line, "Files") {
			t.Errorf("it offers %q, and a file pane is split inside the manager", line)
		}
	}
}

// Taking a shell line puts a pane running that shell in the split.
func TestSplittingOnAShellLandsInTheSplit(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	scanShells(t, a)

	c := splitChoices(t, a, ui.Columns)
	// The one that takes arguments, so the whole argv is checked.
	takeChoice(t, c, "Ubuntu (WSL)")

	waitForPanes(t, a, 2)
	if got, want := a.lastArgv(t), argvOf(t, "wsl:Ubuntu"); !slices.Equal(got, want) {
		t.Errorf("the pane in the split started on %v, want %v", got, want)
	}
	if got := len(a.stage.Children()); got != 1 {
		t.Fatalf("the stage holds %d things, want the one split", got)
	}
	if _, isSplit := a.stage.Children()[0].(*ui.Split); !isSplit {
		t.Fatalf("the stage holds %T, want the split", a.stage.Children()[0])
	}
	// And the pane it opened is the one the next one opens on.
	if id, ok := a.shellPick.chosen(); !ok || id != "wsl:Ubuntu" {
		t.Errorf("the window remembers %q, want the shell that was picked", id)
	}
	checkTree(t, a)
}

// The split chooser offers a command on every machine with a shell, this
// one included, and never on a gridterm window.
func TestSplittingOffersACommandOnAMachine(t *testing.T) {
	s := sshtest.New(t)
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	pinServers(t, a, s)
	saveHost(t, a, "margit", s, "")

	// A gridterm window is in the list too. It has no shell, so there is
	// nothing to run a command in.
	if err := a.book.Put(remote.Host{Name: "desk", Address: "10.0.0.9", Window: true}, ""); err != nil {
		t.Fatalf("save the window: %v", err)
	}

	c := splitChoices(t, a, ui.Columns)
	got := choiceTexts(c)
	if !slices.Contains(got, "Command on margit…") {
		t.Errorf("it offers %v, missing a command on the saved machine", got)
	}
	if !slices.Contains(got, "Terminal on desk") {
		t.Fatalf("it offers %v, missing the saved window: the test has nothing to check", got)
	}
	// This machine runs one too.
	if !slices.Contains(got, "Command on Local…") {
		t.Errorf("it offers %v, missing a command on this machine", got)
	}
	// And nothing else does, the gridterm window least of all: it has no
	// shell to run one in.
	for _, line := range got {
		if !strings.HasPrefix(line, "Command on ") {
			continue
		}
		if line != "Command on margit…" && line != "Command on Local…" {
			t.Errorf("it offers %q, and only a machine with a shell runs one", line)
		}
	}
}

// With -ssh naming a machine, new panes open there, and the chooser
// still offers a command on it and the shells of this one.
//
// The line that opens a terminal where new panes go is offered first and
// not again further down, so the machine it names used to be skipped
// whole and lost its command line with it.
func TestSplittingOnAWindowOpenedWithSsh(t *testing.T) {
	s := sshtest.New(t)
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	pinServers(t, a, s)
	scanShells(t, a)
	// Connected, because new panes go to the machine -ssh named only
	// while that connection is up.
	a.connectAs("margit", serverConfig(t, s))
	waitForPanes(t, a, 2)
	a.home = "margit"
	if got := a.newPaneHost(); got != "margit" {
		t.Fatalf("new panes open on %q, want the machine -ssh named", got)
	}

	c := splitChoices(t, a, ui.Columns)
	got := choiceTexts(c)
	if !slices.Contains(got, "Command on margit…") {
		t.Errorf("it offers %v, missing a command on the machine new panes open on", got)
	}
	// The shells are this machine's, so they sit with the line that
	// opens a terminal here rather than under the one that opens there.
	terminalHere := slices.Index(got, "Terminal on Local")
	if terminalHere < 0 {
		t.Fatalf("it offers %v, missing a terminal on this machine", got)
	}
	if got[terminalHere+1] != "Command Prompt" {
		t.Errorf("the line after %q is %q, want the first shell of this machine",
			got[terminalHere], got[terminalHere+1])
	}
	if first := slices.Index(got, "Command Prompt"); first < terminalHere {
		t.Errorf("the shells come before the machine they open on: %v", got)
	}
}

// The panes a split could take are grouped under the machine each is on,
// the way the sidebar groups them. One flat list mixed in with the
// machines was hard to find a pane in.
func TestSplittingGroupsThePanesToMoveByMachine(t *testing.T) {
	s := sshtest.New(t)
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	pinServers(t, a, s)
	first := onlyPaneWidget(t, a)
	a.connectAs("margit", serverConfig(t, s))
	waitForPanes(t, a, 2)
	// Opened so the two machines interleave in the tree: this machine, a
	// machine, this machine, a machine. Grouping has to gather each
	// machine's panes into one run, and the pane doing the splitting is
	// left out of the list, so the first one is the one to split from.
	if err := a.openTabHere(); err != nil {
		t.Fatalf("openTabHere: %v", err)
	}
	if err := a.openTerminalOn("margit", nil); err != nil {
		t.Fatalf("a second pane on the machine: %v", err)
	}
	waitForPanes(t, a, 4)
	a.focus(first)

	c := splitChoices(t, a, ui.Columns)
	var under string
	seen := map[string]string{}
	for _, row := range c.Rows() {
		if row.Header {
			under = row.Text
			continue
		}
		if !strings.HasPrefix(strings.TrimSpace(row.Text), "Move ") {
			continue
		}
		if under == "" {
			t.Errorf("%q sits under no machine", strings.TrimSpace(row.Text))
		}
		seen[strings.TrimSpace(row.Text)] = under
	}
	if len(seen) == 0 {
		t.Fatalf("nothing to move is offered: %v", choiceTexts(c))
	}
	// Every pane is under the machine it is on, and each machine gets
	// one heading rather than one per pane.
	headings := map[string]int{}
	for _, row := range c.Rows() {
		if row.Header {
			headings[row.Text]++
		}
	}
	for name, n := range headings {
		if n != 1 {
			t.Errorf("%q has %d headings, want one run of lines", name, n)
		}
	}
	if _, ok := headings["margit"]; !ok {
		t.Errorf("the machine's panes are under no heading of its own: %v", choiceTexts(c))
	}
}

// Typing narrows the chooser to the lines that match, and drops a
// heading with nothing left under it.
func TestSplittingNarrowsToWhatIsTyped(t *testing.T) {
	s := sshtest.New(t)
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	pinServers(t, a, s)
	a.connectAs("margit", serverConfig(t, s))
	waitForPanes(t, a, 2)

	c := splitChoices(t, a, ui.Columns)
	before := len(c.Rows())
	for _, r := range "margit" {
		sendKey(t, a, input.Event{Kind: input.Text, Rune: r})
	}

	got := choiceTexts(c)
	if len(got) >= before {
		t.Fatalf("typing narrowed nothing: %d lines before, %d after", before, len(got))
	}
	for _, line := range got {
		if !strings.Contains(strings.ToLower(line), "margit") {
			t.Errorf("it still offers %q, which is not what was typed", line)
		}
	}
	if c.Query() != "margit" {
		t.Errorf("the chooser holds %q, want what was typed", c.Query())
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

	a.openRoute("margit", []step{{name: "margit", cfg: serverConfig(t, s)}}, opening{}, at)
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

	a.refreshPanel(time.Now())
	var terminals int
	for _, row := range a.panel.Rows() {
		if row.Icon == grid.Icon(grid.IconTerminal) {
			terminals++
		}
	}
	if terminals != 2 {
		t.Fatalf("the sidebar shows %d terminals, want a row for each half", terminals)
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
	dismiss(t, c)

	if a.root.Modal() != nil {
		t.Fatalf("Escape left %T on the stack", a.root.Modal())
	}
	if got := len(a.panes); got != was {
		t.Fatalf("%d panes after leaving the question, want %d", got, was)
	}
	checkTree(t, a)
}

// A pane that closes while the question is up is not spliced back into
// the tree.
//
// The chooser holds the pane in a closure, and anything can close it in
// the meantime: its shell exits, or the connection it rides on drops.
// ui.Detach cannot tell a closed pane from one the window has only just
// made, so the answer has to be asked for again when the line is taken.
func TestAPaneThatClosedWhileAskingIsNotSplicedBack(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	if err := a.openTab(); err != nil {
		t.Fatalf("openTab: %v", err)
	}
	doomed := a.focusedTerminal()
	first := otherTerminal(t, a, doomed)
	a.focus(first)

	c := splitChoices(t, a, ui.Columns)
	// It goes while the question is still up.
	if err := a.closePane(doomed); err != nil {
		t.Fatalf("closePane: %v", err)
	}

	var took bool
	for _, row := range c.Rows() {
		if row.Header || !strings.Contains(row.Text, "Move") {
			continue
		}
		at, ok := row.Key.(int)
		if !ok {
			t.Fatalf("the line %q has no place to take", row.Text)
		}
		took = true
		if err := c.Take(at); err == nil {
			t.Fatal("moving a pane that has closed reported nothing")
		}
	}
	if !took {
		t.Fatalf("the question never offered the pane: %v", choiceTexts(c))
	}
	for _, leaf := range ui.Leaves(a.root.Widget()) {
		if leaf == ui.Widget(doomed) {
			t.Fatal("the closed pane is back in the tree")
		}
	}
	checkTree(t, a)
}

// A file pane belongs to the file manager and is not offered as
// something to move into a split.
//
// Out of the manager it loses every key it has, and a manager left with
// none is taken out of the tree with the window still holding it.
func TestAFilePaneIsNotOfferedToMoveIntoASplit(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	if err := a.openFilesOn(conns.Local); err != nil {
		t.Fatalf("a file pane: %v", err)
	}
	pane := a.files.view.Panes()[0]
	a.focus(a.focusedTerminalAnywhere(t))

	c := splitChoices(t, a, ui.Columns)
	for _, text := range choiceTexts(c) {
		if strings.Contains(text, "Move") {
			t.Fatalf("it offers %q, and the only other pane is a file pane", text)
		}
	}
	dismiss(t, c)

	// And it is refused even when asked for directly.
	if err := a.splitWith(ui.Columns, a.focusedTerminalAnywhere(t), pane); err == nil {
		t.Fatal("a file pane was moved out of the manager")
	}
	if a.files == nil || len(a.files.view.Panes()) != 1 {
		t.Fatal("the manager lost its pane")
	}
	checkTree(t, a)
}

// focusedTerminalAnywhere returns any terminal the window holds.
func (a *testApp) focusedTerminalAnywhere(t *testing.T) *term.Terminal {
	t.Helper()
	for pane := range a.panes {
		return pane
	}
	t.Fatal("the window has no terminal")
	return nil
}

// A terminal that arrives for a split whose pane has gone into the
// background opens in a tab rather than being thrown away.
//
// A backgrounded tab has no room to be split into, and the login the
// user waited for must not be lost because of where they went next.
func TestASplitWhosePaneWentToTheBackgroundOpensATab(t *testing.T) {
	s := sshtest.New(t)
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	pinServers(t, a, s)

	first := a.focusedTerminal()
	at := &spot{beside: first, dir: ui.Columns}
	// Another tab, so the pane the split was meant for is no longer the
	// one showing and has no area to divide.
	if err := a.openTab(); err != nil {
		t.Fatalf("openTab: %v", err)
	}

	a.openRoute("margit", []step{{name: "margit", cfg: serverConfig(t, s)}}, opening{}, at)
	waitForPanes(t, a, 3)
	checkTree(t, a)
	for _, w := range a.stage.Children() {
		if _, split := w.(*ui.Split); split {
			t.Fatal("it split a pane with no room to be split")
		}
	}
	if got := len(a.stage.Children()); got != 3 {
		t.Fatalf("the stage holds %d things, want three tabs", got)
	}
}

// Dragging the divider between two shells is how the user gives one of
// them more room. It starts at the press, on the window's own root, so
// the whole way from the click to the shell being told its new size is
// under test.
func TestDraggingASplitDividerResizesBothShells(t *testing.T) {
	a := newTestApp(t, 80, 24)
	if err := a.splitHere(ui.Columns); err != nil {
		t.Fatalf("splitHere: %v", err)
	}
	split, ok := a.stage.Children()[0].(*ui.Split)
	if !ok {
		t.Fatalf("the stage holds %T, want a split", a.stage.Children()[0])
	}
	left, okLeft := split.Children()[0].(*term.Terminal)
	right, okRight := split.Children()[1].(*term.Terminal)
	if !okLeft || !okRight {
		t.Fatalf("the split holds %T and %T, want two terminals",
			split.Children()[0], split.Children()[1])
	}
	area, shown := a.root.AreaOf(left)
	if !shown {
		t.Fatal("the first pane is not on screen")
	}
	// The divider is the column the first pane ends in.
	at, row := area.X+area.Cols, area.Y+1
	wasLeft, wasRight := left.Size().Cols, right.Size().Cols

	if _, err := a.root.HandleMouse(input.MouseEvent{
		Kind: input.MousePress, Button: input.MouseLeft, Col: at, Row: row,
	}); err != nil {
		t.Fatalf("the press failed: %v", err)
	}
	if _, err := a.root.HandleMouse(input.MouseEvent{
		Kind: input.MouseMove, Button: input.MouseLeft, Col: at - 10, Row: row,
	}); err != nil {
		t.Fatalf("the drag failed: %v", err)
	}
	if _, err := a.root.HandleMouse(input.MouseEvent{
		Kind: input.MouseRelease, Button: input.MouseLeft, Col: at - 10, Row: row,
	}); err != nil {
		t.Fatalf("the release failed: %v", err)
	}

	if got := left.Size().Cols; got != wasLeft-10 {
		t.Errorf("the first shell is %d columns wide, want %d", got, wasLeft-10)
	}
	if got := right.Size().Cols; got != wasRight+10 {
		t.Errorf("the second shell is %d columns wide, want %d", got, wasRight+10)
	}
	// And the pane that was dragged past keeps the keys it had: a drag is
	// not a click on the pane the pointer ended over.
	if ui.FocusedLeaf(a.root.Widget()) != ui.Widget(right) {
		t.Error("the drag moved the keys")
	}
	checkTree(t, a)
}

// The pane picker names a pane on a machine over there by that machine,
// through the window it reads it through.
//
// Two panes on two machines over one window are both filed under the
// window, so the window's name alone would read the same on both lines and
// the user would pick blind.
func TestThePanePickerNamesTheMachineAPaneOverThereReads(t *testing.T) {
	host, client, addr := aWindowConnectedToMargit(t)
	held := windowAt(t, client, addr)

	over := openFilesFromTheFarPlus(t, client, host, addr, "margit")
	here := openFilesFromThePlus(t, client, conns.Local)

	if got, want := client.paneWhere(over), "margit through "+held.name; got != want {
		t.Errorf("the line for the pane over there reads %q, want %q", got, want)
	}
	if client.paneWhere(here) == client.paneWhere(over) {
		t.Errorf("a pane here and one over there both read %q", client.paneWhere(here))
	}
}
