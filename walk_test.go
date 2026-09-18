package main

import (
	"slices"
	"strings"
	"testing"

	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/input"
	"github.com/marrasen/gridterm/ui"
	"github.com/marrasen/gridterm/ui/term"
)

// aWalkingWindow is a window with three panes and a clock of its own for
// which modifier keys are held.
func aWalkingWindow(t *testing.T) (*testApp, []ui.Widget) {
	t.Helper()
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	withScreen(t, a)
	holdCtrl(a, true)
	for range 2 {
		if err := a.openPane(); err != nil {
			t.Fatalf("open a pane: %v", err)
		}
	}
	var panes []ui.Widget
	for _, leaf := range ui.Leaves(a.root.Widget()) {
		if a.isPane(leaf) {
			panes = append(panes, leaf)
		}
	}
	if len(panes) != 3 {
		t.Fatalf("the window has %d panes, want three", len(panes))
	}
	return a, panes
}

// holdCtrl says whether Ctrl is down.
func holdCtrl(a *testApp, down bool) {
	a.modsNow = func() input.Mods {
		if down {
			return input.ModCtrl
		}
		return 0
	}
}

// visit puts the keys on each pane in turn, so the window remembers the
// order they were used in.
func visit(t *testing.T, a *testApp, panes ...ui.Widget) {
	t.Helper()
	for _, w := range panes {
		a.focus(w)
		a.noteFocus()
	}
}

// Ctrl+Tab goes to the pane used before this one, not to the next one
// along the tree.
func TestCtrlTabGoesToThePaneUsedBefore(t *testing.T) {
	a, panes := aWalkingWindow(t)
	visit(t, a, panes[2], panes[0], panes[1])

	if err := a.walkRecent(1); err != nil {
		t.Fatalf("walk: %v", err)
	}

	if got := ui.FocusedLeaf(a.root.Widget()); got != panes[0] {
		t.Errorf("it went to %p, want the pane used before this one %p", got, panes[0])
	}
}

// Stepping on again reaches the one before that.
func TestSteppingOnReachesTheOneBefore(t *testing.T) {
	a, panes := aWalkingWindow(t)
	visit(t, a, panes[2], panes[0], panes[1])

	if err := a.walkRecent(1); err != nil {
		t.Fatalf("walk: %v", err)
	}
	if err := a.walkRecent(1); err != nil {
		t.Fatalf("walk again: %v", err)
	}

	if got := ui.FocusedLeaf(a.root.Widget()); got != panes[2] {
		t.Errorf("it went to %p, want %p", got, panes[2])
	}
}

// The order does not move while Ctrl is held. A list that re-ordered as
// the user stepped would swap the top two and then bounce between them.
func TestTheOrderDoesNotMoveWhileCtrlIsHeld(t *testing.T) {
	a, panes := aWalkingWindow(t)
	visit(t, a, panes[2], panes[0], panes[1])

	for range 4 {
		if err := a.walkRecent(1); err != nil {
			t.Fatalf("walk: %v", err)
		}
		// A frame between the steps, the way the window runs one: the
		// order must not take the pane stepped onto as the newest.
		frame(t, a)
	}

	// Four steps round three panes comes back to where it started.
	if got := ui.FocusedLeaf(a.root.Widget()); got != panes[0] {
		t.Errorf("four steps reached %p, want %p", got, panes[0])
	}
	if got, want := a.walk.order, []ui.Widget{panes[1], panes[0], panes[2]}; !slices.Equal(got, want) {
		t.Errorf("the order moved to %p, want %p", got, want)
	}
}

// Letting Ctrl go lands, and the pane landed on becomes the newest.
func TestLettingCtrlGoLands(t *testing.T) {
	a, panes := aWalkingWindow(t)
	visit(t, a, panes[2], panes[0], panes[1])
	if err := a.walkRecent(1); err != nil {
		t.Fatalf("walk: %v", err)
	}

	holdCtrl(a, false)
	// A frame, the way the window runs one: it ends the walk and then
	// writes down where it landed.
	frame(t, a)

	if a.walk != nil {
		t.Error("the walk is still on with Ctrl let go")
	}
	// The one it landed on is now the newest, so Ctrl+Tab goes back to
	// the one it came from.
	if err := a.walkRecent(1); err != nil {
		t.Fatalf("walk again: %v", err)
	}
	if got := ui.FocusedLeaf(a.root.Widget()); got != panes[1] {
		t.Errorf("it went to %p, want the pane it came from %p", got, panes[1])
	}
}

// Ctrl+Shift+Tab walks the other way.
func TestShiftWalksTheOtherWay(t *testing.T) {
	a, panes := aWalkingWindow(t)
	visit(t, a, panes[1], panes[0], panes[2])

	if err := a.walkRecent(-1); err != nil {
		t.Fatalf("walk back: %v", err)
	}

	// Back from the newest is the oldest.
	if got := ui.FocusedLeaf(a.root.Widget()); got != panes[1] {
		t.Errorf("it went to %p, want %p", got, panes[1])
	}
}

// A pane that closes while the walk is on comes off it.
func TestAPaneThatClosesComesOffTheWalk(t *testing.T) {
	a, panes := aWalkingWindow(t)
	visit(t, a, panes[2], panes[0], panes[1])
	if err := a.walkRecent(1); err != nil {
		t.Fatalf("walk: %v", err)
	}
	if got := a.walk.on(); got != panes[0] {
		t.Fatalf("the walk is on %p, want %p", got, panes[0])
	}

	if err := a.closePane(panes[0].(*term.Terminal)); err != nil {
		t.Fatalf("close it: %v", err)
	}
	a.stepWalk()

	if slices.Contains(a.walk.order, panes[0]) {
		t.Error("the walk still holds the pane that closed")
	}
	// The mark moves to the one after it, which is where the one that
	// closed was standing.
	if got := a.walk.on(); got != panes[2] {
		t.Errorf("the walk is on %p, want the one after the pane that closed %p", got, panes[2])
	}
	if got := ui.FocusedLeaf(a.root.Widget()); got != panes[2] {
		t.Errorf("the keys are on %p, want %p", got, panes[2])
	}
}

// And when the last of the list closes the mark comes round to the
// start, because the list is a ring everywhere else.
func TestClosingTheLastOfTheWalkComesRound(t *testing.T) {
	a, panes := aWalkingWindow(t)
	visit(t, a, panes[2], panes[0], panes[1])
	// On to the last of the three.
	for range 2 {
		if err := a.walkRecent(1); err != nil {
			t.Fatalf("walk: %v", err)
		}
	}
	last := a.walk.on()
	if got := a.walk.at; got != 2 {
		t.Fatalf("the walk is at %d, want the last of the three", got)
	}

	if err := a.closePane(last.(*term.Terminal)); err != nil {
		t.Fatalf("close it: %v", err)
	}
	a.stepWalk()

	if got := a.walk.at; got != 0 {
		t.Errorf("the mark is at %d, want it come round to the start", got)
	}
}

// The walk shows what it is on, because more than a step or two is
// counting in the dark otherwise.
func TestTheWalkShowsWhereItIs(t *testing.T) {
	a, panes := aWalkingWindow(t)
	visit(t, a, panes[2], panes[0], panes[1])

	if err := a.walkRecent(1); err != nil {
		t.Fatalf("walk: %v", err)
	}
	frame(t, a)

	if a.overlay == nil {
		t.Fatal("nothing says what the walk is on")
	}
	if !inStack(a.comp, a.overlay.layer) {
		t.Error("the list is not on the stack, so nothing draws it")
	}
	lines, at := a.walkLines()
	if len(lines) != 3 {
		t.Errorf("it lists %v, want a line per pane", lines)
	}
	if at != 1 {
		t.Errorf("it marks line %d, want the one the walk is on", at)
	}
}

// And it goes when the walk does.
func TestTheWalkListGoesWhenTheWalkDoes(t *testing.T) {
	a, panes := aWalkingWindow(t)
	visit(t, a, panes[2], panes[0], panes[1])
	if err := a.walkRecent(1); err != nil {
		t.Fatalf("walk: %v", err)
	}
	frame(t, a)
	was := a.overlay
	if was == nil {
		t.Fatal("nothing says what the walk is on")
	}

	holdCtrl(a, false)
	frame(t, a)

	if a.overlay != nil {
		t.Error("the list is still up with the walk over")
	}
	if inStack(a.comp, was.layer) {
		t.Error("the list's layer is still on the stack")
	}
}

// The list names the panes, so the user can tell them apart.
func TestTheWalkListNamesThePanes(t *testing.T) {
	a, panes := aWalkingWindow(t)
	visit(t, a, panes[2], panes[0], panes[1])
	a.panes[panes[0].(*term.Terminal)].Label = "vim"
	if err := a.walkRecent(1); err != nil {
		t.Fatalf("walk: %v", err)
	}

	lines, _ := a.walkLines()

	if !slices.ContainsFunc(lines, func(s string) bool { return strings.Contains(s, "vim") }) {
		t.Errorf("it lists %v, want the pane named", lines)
	}
}

// A window with one pane has nowhere to walk to, and says nothing.
func TestOnePaneShowsNoList(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	withScreen(t, a)
	holdCtrl(a, true)

	if err := a.walkRecent(1); err != nil {
		t.Fatalf("walk: %v", err)
	}
	frame(t, a)

	if a.overlay != nil {
		t.Error("a window with one pane put a list up")
	}
}

// Walking does not itself count as using a pane. Only where the walk
// lands is remembered, so the next walk follows the order the user
// worked in rather than the order they stepped through.
func TestWalkingPastAPaneIsNotUsingIt(t *testing.T) {
	a, panes := aWalkingWindow(t)
	// Newest first: panes[1], panes[0], panes[2].
	visit(t, a, panes[2], panes[0], panes[1])

	// On to panes[0], on to panes[2], then back to panes[0], and land.
	for _, step := range []int{1, 1, -1} {
		if err := a.walkRecent(step); err != nil {
			t.Fatalf("walk: %v", err)
		}
		frame(t, a)
	}
	if got := ui.FocusedLeaf(a.root.Widget()); got != panes[0] {
		t.Fatalf("the walk is on %p, want %p", got, panes[0])
	}
	holdCtrl(a, false)
	frame(t, a)
	holdCtrl(a, true)

	// panes[2] was stepped past and never landed on, so the pane used
	// before this one is still panes[1].
	if err := a.walkRecent(1); err != nil {
		t.Fatalf("walk again: %v", err)
	}
	if got := ui.FocusedLeaf(a.root.Widget()); got != panes[1] {
		t.Errorf("it went to %p, want the pane used before %p", got, panes[1])
	}
}

// A pane in a tab that is not in front is in the walk, and landing on it
// brings that tab forward. A new pane goes in the stage's deck, so the
// panes of this window are tabs and only one is on screen.
func TestTheWalkReachesAPaneInAnotherTab(t *testing.T) {
	a, panes := aWalkingWindow(t)
	visit(t, a, panes[0], panes[1])
	behind := panes[0].(*term.Terminal)
	frame(t, a)
	if _, shown := a.paneArea(behind); shown {
		t.Fatal("the pane behind is on screen, so this proves nothing")
	}

	if err := a.walkRecent(1); err != nil {
		t.Fatalf("walk: %v", err)
	}
	frame(t, a)

	if got := ui.FocusedLeaf(a.root.Widget()); got != ui.Widget(behind) {
		t.Fatalf("it went to %p, want the pane in the other tab %p", got, behind)
	}
	if _, shown := a.paneArea(behind); !shown {
		t.Error("landing on it did not bring its tab forward")
	}
}

// A file browser's panes are in the walk too, named by where they are
// looking.
func TestTheWalkIncludesFilePanes(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	withScreen(t, a)
	holdCtrl(a, true)
	p := openFilesFromThePlus(t, a, conns.Local)
	waitFor(t, a, "the pane to land somewhere", func() bool { return p.At() != "" })
	a.noteFocus()

	if err := a.walkRecent(1); err != nil {
		t.Fatalf("walk: %v", err)
	}

	lines, _ := a.walkLines()
	if len(lines) != 2 {
		t.Fatalf("it lists %v, want the shell and the file pane", lines)
	}
	if !slices.Contains(a.walk.order, ui.Widget(p)) {
		t.Error("the file pane is not in the walk")
	}
}

// A walk started with no Ctrl held is over on the same frame, so running
// the command from the palette moves the keys and leaves nothing up.
func TestAWalkWithoutCtrlEndsAtOnce(t *testing.T) {
	a, panes := aWalkingWindow(t)
	visit(t, a, panes[2], panes[0], panes[1])
	holdCtrl(a, false)

	if err := a.walkRecent(1); err != nil {
		t.Fatalf("walk: %v", err)
	}
	frame(t, a)

	if a.walk != nil {
		t.Error("the walk is still on with nothing held")
	}
	if a.overlay != nil {
		t.Error("the list is still up")
	}
	if got := ui.FocusedLeaf(a.root.Widget()); got != panes[0] {
		t.Errorf("the keys are on %p, want the pane used before %p", got, panes[0])
	}
}

// A dialog has the keys, so Ctrl+Tab does nothing: walking would move
// the focus behind it and mark a pane that cannot be typed in.
func TestAWalkIsRefusedWhileADialogIsUp(t *testing.T) {
	a, panes := aWalkingWindow(t)
	visit(t, a, panes[2], panes[0], panes[1])
	was := ui.FocusedLeaf(a.root.Widget())
	a.showNotice("Something happened", "and here is what", false)

	if err := a.walkRecent(1); err != nil {
		t.Fatalf("walk: %v", err)
	}
	frame(t, a)

	if a.walk != nil {
		t.Error("a walk started behind a dialog")
	}
	if a.overlay != nil {
		t.Error("the list is up over a dialog")
	}
	if got := ui.FocusedLeaf(a.root.Widget()); got != was {
		t.Errorf("the keys moved to %p behind the dialog", got)
	}
}

// Panes that close leave nothing behind: the order they were used in
// holds no pane the window has let go of.
func TestTheOrderLetsGoOfClosedPanes(t *testing.T) {
	a, panes := aWalkingWindow(t)
	visit(t, a, panes[2], panes[0], panes[1])
	holdCtrl(a, false)

	for _, w := range panes[:2] {
		if err := a.closePane(w.(*term.Terminal)); err != nil {
			t.Fatalf("close: %v", err)
		}
	}
	// A frame with the keys somewhere else, which is what writes the
	// order down again.
	frame(t, a)

	for _, gone := range panes[:2] {
		if slices.Contains(a.recent, gone) {
			t.Errorf("the order still holds %p, which has closed", gone)
		}
	}
}

// From the sidebar the walk comes in at the pane used last, rather than
// stepping past it.
func TestFromTheSidebarTheWalkComesInAtTheNewest(t *testing.T) {
	a, panes := aWalkingWindow(t)
	visit(t, a, panes[2], panes[0], panes[1])
	a.focus(a.side)
	if got := ui.FocusedLeaf(a.root.Widget()); got != ui.Widget(a.side) {
		t.Fatalf("the keys are on %T, want the sidebar", got)
	}

	if err := a.walkRecent(1); err != nil {
		t.Fatalf("walk: %v", err)
	}

	if got := ui.FocusedLeaf(a.root.Widget()); got != panes[1] {
		t.Errorf("it came in at %p, want the pane used last %p", got, panes[1])
	}
}

// A pane that closes while the keys are somewhere else is let go of too.
// Nothing moves the focus, so nothing else would notice it had gone.
func TestAPaneClosedInTheBackgroundIsLetGoOf(t *testing.T) {
	a, panes := aWalkingWindow(t)
	visit(t, a, panes[0], panes[1])
	holdCtrl(a, false)
	frame(t, a)
	was := ui.FocusedLeaf(a.root.Widget())

	// One that does not have the keys, so the focus does not move.
	if err := a.closePane(panes[2].(*term.Terminal)); err != nil {
		t.Fatalf("close: %v", err)
	}
	frame(t, a)

	if got := ui.FocusedLeaf(a.root.Widget()); got != was {
		t.Fatalf("the keys moved to %p, so this proves nothing", got)
	}
	if slices.Contains(a.recent, panes[2]) {
		t.Error("the order still holds the pane that closed")
	}
}
