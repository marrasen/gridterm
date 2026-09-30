package app

import (
	"testing"
	"time"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/gunimtest"
)

// groups returns the groups of the tabs, in order.
func groups(tabs []Tab) []int {
	var out []int
	for _, t := range tabs {
		out = append(out, t.Group)
	}
	return out
}

// Each window lists its own tabs, oldest first, each naming the pane
// that has its keyboard.
func TestEachWindowListsItsTabs(t *testing.T) {
	a, one, two := twoWindowApp(t)
	st := a.stateFor(one, a.st)
	if len(st.Tabs) != 2 || st.Tabs[0].Pane != "p1" || st.Tabs[1].Pane != "p2" {
		t.Fatalf("the first window's tabs are %+v", st.Tabs)
	}
	if st = a.stateFor(two, a.st); len(st.Tabs) != 1 || st.Tabs[0].Pane != "p3" || st.Tabs[0].Panes != 1 {
		t.Fatalf("the second window's tabs are %+v", st.Tabs)
	}
}

// A tab shown again gives the keyboard back to the pane in it that
// had it last, not the first.
func TestATabRemembersItsPane(t *testing.T) {
	a, one, _ := twoWindowApp(t)
	a.front(one)
	a.handle(MovePane{Pane: "p2", Beside: "p1"})
	a.addPane(Pane{ID: "p4", Kind: KindFiles}, nil, Placement{})
	a.focus("p2")
	a.noteTabFocus()
	g := a.groupOf["p2"]
	a.handle(ShowTab{Group: a.groupOf["p4"]})
	if a.st.Focus != "p4" {
		t.Fatalf("the focus is on %q, want p4", a.st.Focus)
	}
	a.handle(ShowTab{Group: g})
	if a.st.Focus != "p2" {
		t.Fatalf("shown again, the tab gave the keyboard to %q, want p2", a.st.Focus)
	}
}

// Next Tab goes round the window's tabs, and Back the other way.
func TestNextTabGoesRound(t *testing.T) {
	a, one, _ := twoWindowApp(t)
	a.front(one)
	a.focus("p1")
	a.handle(NextTab{})
	if a.st.Focus != "p2" {
		t.Fatalf("next went to %q, want p2", a.st.Focus)
	}
	a.handle(NextTab{})
	if a.st.Focus != "p1" {
		t.Fatalf("next from the last went to %q, want p1", a.st.Focus)
	}
	a.handle(NextTab{Back: true})
	if a.st.Focus != "p2" {
		t.Fatalf("back from the first went to %q, want p2", a.st.Focus)
	}
}

// A tab dragged along the bar lands where it was let go, and moving
// the tab in front shifts it one place.
func TestMoveTabReorders(t *testing.T) {
	a, one, _ := twoWindowApp(t)
	a.front(one)
	a.addPane(Pane{ID: "p4", Kind: KindFiles}, nil, Placement{})
	g1, g2, g4 := a.groupOf["p1"], a.groupOf["p2"], a.groupOf["p4"]
	a.handle(MoveTab{Group: g4, Before: g1})
	if got := groups(a.tabsOf(one)); len(got) != 3 || got[0] != g4 || got[1] != g1 || got[2] != g2 {
		t.Fatalf("the tabs are %v, want %v", got, []int{g4, g1, g2})
	}
	if a.st.Focus != "p4" {
		t.Fatalf("the tab moved is not in front: %q is", a.st.Focus)
	}
	a.handle(ShiftTab{})
	if got := groups(a.tabsOf(one)); got[0] != g1 || got[1] != g4 {
		t.Fatalf("shifted, the tabs are %v", got)
	}
	a.handle(ShiftTab{})
	a.handle(ShiftTab{})
	if got := groups(a.tabsOf(one)); got[2] != g4 {
		t.Fatalf("shifted past the end, the tabs are %v", got)
	}
	a.handle(ShiftTab{Back: true})
	if got := groups(a.tabsOf(one)); got[1] != g4 {
		t.Fatalf("shifted back, the tabs are %v", got)
	}
}

// A tab dropped on another window's bar moves there whole, splits and
// all, and the window it left shows its next tab.
func TestATabMovesToAnotherWindowWhole(t *testing.T) {
	a, one, two := twoWindowApp(t)
	a.front(one)
	a.addPane(Pane{ID: "p4", Kind: KindFiles}, nil, Placement{})
	a.handle(MovePane{Pane: "p4", Beside: "p2"})
	a.focus("p1")
	g := a.groupOf["p2"]
	a.front(two)
	a.handle(MoveTab{Group: g})
	if a.winOf["p2"] != two.id || a.winOf["p4"] != two.id {
		t.Fatal("the tab's panes stayed in their window")
	}
	if b := a.groups[g]; b == nil || b.Pane != "" {
		t.Fatalf("the tab's split is %+v", b)
	}
	if got := groups(a.tabsOf(two)); len(got) != 2 || got[1] != g {
		t.Fatalf("the window has tabs %v", got)
	}
	if one.focus != "p1" || a.cur != two || a.groupOf[a.st.Focus] != g {
		t.Fatalf("the window left has %q in front; the focus is on %q", one.focus, a.st.Focus)
	}
}

// The window a tab in front leaves shows the tab after it.
func TestAWindowShowsTheNextTabWhenOneLeaves(t *testing.T) {
	a, one, two := twoWindowApp(t)
	a.front(one)
	a.addPane(Pane{ID: "p4", Kind: KindFiles}, nil, Placement{})
	a.focus("p1")
	a.front(two)
	a.handle(MoveTab{Group: a.groupOf["p1"]})
	if one.focus != "p2" {
		t.Fatalf("the window left shows %q, want p2, the tab after", one.focus)
	}
}

// A tab dropped on a pane joins it in a split, on the side it was
// dropped on, and the tab goes.
func TestATabDockedJoinsTheSplit(t *testing.T) {
	a, one, two := twoWindowApp(t)
	a.front(two)
	a.handle(DockTab{Group: a.groupOf["p2"], Beside: "p3", Vertical: true, First: true})
	b := a.groups[a.groupOf["p3"]]
	if b == nil || !b.Vertical || b.A == nil || b.A.Pane != "p2" || b.B.Pane != "p3" {
		t.Fatalf("the split is %+v", b)
	}
	if a.groupOf["p2"] != a.groupOf["p3"] || a.winOf["p2"] != two.id || a.st.Focus != "p2" {
		t.Fatalf("p2 is in group %d, window %d; the focus is on %q", a.groupOf["p2"], a.winOf["p2"], a.st.Focus)
	}
	if got := a.tabsOf(two); len(got) != 1 || got[0].Panes != 2 {
		t.Fatalf("the window's tabs are %+v", got)
	}
	if one.focus != "p1" {
		t.Fatalf("the window left shows %q", one.focus)
	}
}

// A tab dropped on its own pane stays as it is.
func TestATabDockedOnItselfStays(t *testing.T) {
	a, _, two := twoWindowApp(t)
	a.front(two)
	g := a.groupOf["p3"]
	a.handle(DockTab{Group: g, Beside: "p3"})
	if b := a.groups[g]; b == nil || b.Pane != "p3" {
		t.Fatalf("the tab is %+v", b)
	}
}

// Closing the tab in front closes its panes and shows the tab beside
// it, not the pane before it in the sidebar.
func TestClosingATabShowsTheOneBeside(t *testing.T) {
	a, one, _ := twoWindowApp(t)
	a.front(one)
	a.addPane(Pane{ID: "p4", Kind: KindFiles}, nil, Placement{})
	a.handle(MoveTab{Group: a.groupOf["p4"], Before: a.groupOf["p1"]})
	a.focus("p1")
	a.handle(CloseTab{Group: a.groupOf["p1"]})
	if a.has("p1") {
		t.Fatal("the tab's pane is still open")
	}
	if a.st.Focus != "p2" {
		t.Fatalf("the focus went to %q, want p2, the tab after", a.st.Focus)
	}
}

// A tab let go outside every window opens one of its own; a window's
// only tab stays.
func TestATabLetGoOutsideOpensAWindow(t *testing.T) {
	a, one, two := twoWindowApp(t)
	opened := 0
	a.openWindow = func(_ *gunim.Window, _ geom.Point, s geom.Size) (gunim.Client, *gunim.Window, error) {
		opened++
		return gunimtest.New(t, s, nil).Client(), nil, nil
	}
	a.front(two)
	a.handle(TabToNewWindow{Group: a.groupOf["p3"]})
	a.front(one)
	a.handle(TabToNewWindow{Group: a.groupOf["p2"]})
	select {
	case f := <-a.events:
		f()
	case <-time.After(time.Second):
		t.Fatal("no window opened")
	}
	if opened != 1 || len(a.wins) != 3 || a.winOf["p2"] != a.cur.id || a.st.Focus != "p2" {
		t.Fatalf("%d opened, %d windows, p2 in window %d, the focus on %q", opened, len(a.wins), a.winOf["p2"], a.st.Focus)
	}
}

// Closing a split's tab closes every pane in it at once, and the
// keyboard goes to the tab beside, never to a pane on its way out.
func TestClosingASplitsTabHandsNoKeysToItsPanes(t *testing.T) {
	a, one, _ := twoWindowApp(t)
	a.front(one)
	a.addPane(Pane{ID: "p4", Kind: KindFiles}, nil, Placement{Beside: "p1"})
	a.focus("p4")
	a.handle(CloseTab{})
	if a.has("p1") || a.has("p4") || len(a.closing) != 0 {
		t.Fatalf("p1 open %v, p4 open %v, %d folding", a.has("p1"), a.has("p4"), len(a.closing))
	}
	if a.st.Focus != "p2" {
		t.Fatalf("the focus went to %q, want p2", a.st.Focus)
	}
}

// A tab of another window is neither shown nor closed from this one,
// whose bar was a step behind.
func TestATabOfAnotherWindowIsLeftAlone(t *testing.T) {
	a, one, two := twoWindowApp(t)
	a.front(two)
	a.handle(ShowTab{Group: a.groupOf["p1"]})
	a.handle(CloseTab{Group: a.groupOf["p1"]})
	if a.cur != two || !a.has("p1") || one.focus != "p2" {
		t.Fatalf("window %d in front, p1 open %v", a.cur.id, a.has("p1"))
	}
}

// A tab left its window's only one while the new window opened stays,
// and the window opened for it closes again.
func TestATabLeftAloneStaysWhenItsWindowOpens(t *testing.T) {
	a, one, _ := twoWindowApp(t)
	a.openWindow = func(_ *gunim.Window, _ geom.Point, s geom.Size) (gunim.Client, *gunim.Window, error) {
		return gunimtest.New(t, s, nil).Client(), nil, nil
	}
	a.front(one)
	a.handle(TabToNewWindow{Group: a.groupOf["p2"]})
	a.remove("p1")
	select {
	case f := <-a.events:
		f()
	case <-time.After(time.Second):
		t.Fatal("no window opened")
	}
	if a.winOf["p2"] != one.id || a.cur != one || !a.wins[len(a.wins)-1].gone {
		t.Fatalf("p2 in window %d, window %d in front", a.winOf["p2"], a.cur.id)
	}
}

// A single pane moved to another window leaves its own showing the tab
// beside it, as closing it would.
func TestAPaneMovedAwayShowsTheTabBeside(t *testing.T) {
	a, one, _ := twoWindowApp(t)
	a.front(one)
	a.addPane(Pane{ID: "p4", Kind: KindFiles}, nil, Placement{})
	a.handle(MoveTab{Group: a.groupOf["p4"], Before: a.groupOf["p1"]})
	a.focus("p4")
	a.moveToWindow("p4", a.wins[1])
	if got := a.focusIn(one); got != "p1" {
		t.Fatalf("the window left shows %q, want p1, the tab after", got)
	}
}
