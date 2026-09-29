package app

import (
	"testing"
	"time"

	"github.com/marrasen/kakel/screen"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/gunimtest"
)

// twoWindowApp is a program serving two windows, p1 and p2 in the
// first, and p3 in the second, which is in front.
func twoWindowApp(t *testing.T) (a *app, one, two *ownWin) {
	t.Helper()
	a = newApp(gunimtest.New(t, geom.Sz(400, 300), nil).Client(), screen.NewShells())
	a.ctx = t.Context()
	one = a.cur
	a.addPane(Pane{ID: "p1", Title: "one", Kind: KindFiles}, nil, Placement{})
	a.addPane(Pane{ID: "p2", Title: "two", Kind: KindFiles}, nil, Placement{})
	two = a.addWindow(gunimtest.New(t, geom.Sz(400, 300), nil).Client(), nil)
	a.front(two)
	a.addPane(Pane{ID: "p3", Title: "three", Kind: KindFiles}, nil, Placement{})
	return a, one, two
}

// ids returns the panes' ids.
func ids(panes []Pane) []string {
	var out []string
	for _, p := range panes {
		out = append(out, p.ID)
	}
	return out
}

// Each window shows its own panes, with its own pane in front.
func TestEachWindowShowsItsOwnPanes(t *testing.T) {
	a, one, two := twoWindowApp(t)
	st := a.stateFor(one, a.st)
	if got := ids(st.Panes); len(got) != 2 || got[0] != "p1" || got[1] != "p2" {
		t.Fatalf("the first window shows %v, want p1 and p2", got)
	}
	if st.Focus != "p2" || st.Stage == nil || st.Stage.Pane != "p2" || !st.Behind {
		t.Fatalf("the first window has %q in front, on stage %+v, behind %v", st.Focus, st.Stage, st.Behind)
	}
	st = a.stateFor(two, a.st)
	if got := ids(st.Panes); len(got) != 1 || got[0] != "p3" || st.Focus != "p3" || st.Behind {
		t.Fatalf("the second window shows %v with %q in front, behind %v", got, st.Focus, st.Behind)
	}
}

// A pane dropped on another window moves there, into the front, and
// the window it left gives the keyboard to another of its own.
func TestAPaneDroppedOnAnotherWindowMovesThere(t *testing.T) {
	a, one, two := twoWindowApp(t)
	a.handle(PaneToWindow{Pane: "p2"})
	if a.winOf["p2"] != two.id {
		t.Fatal("the pane stayed in its window")
	}
	if a.st.Focus != "p2" || a.groupOf["p2"] == a.groupOf["p3"] {
		t.Fatalf("moved, the focus is on %q, and the pane is in group %d beside p3's %d", a.st.Focus, a.groupOf["p2"], a.groupOf["p3"])
	}
	if one.focus != "p1" {
		t.Fatalf("the window it left has %q in front, want p1", one.focus)
	}
	if got := ids(a.stateFor(one, a.st).Panes); len(got) != 1 || got[0] != "p1" {
		t.Fatalf("the window it left shows %v", got)
	}
}

// A pane in a split, moved to another window, leaves the split to the
// pane beside it.
func TestAPaneMovedOutOfASplitLeavesItsNeighbour(t *testing.T) {
	a, one, two := twoWindowApp(t)
	a.front(one)
	a.handle(MovePane{Pane: "p2", Beside: "p1"})
	a.front(two)
	a.handle(PaneToWindow{Pane: "p2"})
	if b := a.groups[a.groupOf["p1"]]; b == nil || b.Pane != "p1" {
		t.Fatalf("the split it left is %+v, want p1 alone", b)
	}
	if one.focus != "p1" {
		t.Fatalf("the window it left has %q in front, want p1", one.focus)
	}
}

// A pane let go outside every window opens one of its own, which comes
// to the front with the pane.
func TestAPaneLetGoOutsideOpensAWindow(t *testing.T) {
	a, one, _ := twoWindowApp(t)
	a.front(one)
	var at geom.Point
	var size geom.Size
	a.openWindow = func(_ *gunim.Window, p geom.Point, s geom.Size) (gunim.Client, *gunim.Window, error) {
		at, size = p, s
		return gunimtest.New(t, s, nil).Client(), nil, nil
	}
	a.handle(PaneToNewWindow{Pane: "p2", At: geom.Pt(50, 60), Size: geom.Sz(700, 500)})
	select {
	case f := <-a.events:
		f()
	case <-time.After(time.Second):
		t.Fatal("no window opened")
	}
	if at != geom.Pt(50, 60) || size != geom.Sz(700, 500) {
		t.Fatalf("the window opened at %v, %v large", at, size)
	}
	if len(a.wins) != 3 || a.cur != a.wins[2] || a.winOf["p2"] != a.cur.id || a.st.Focus != "p2" {
		t.Fatalf("%d windows, the pane in window %d, the focus on %q", len(a.wins), a.winOf["p2"], a.st.Focus)
	}
	if one.focus != "p1" {
		t.Fatalf("the window it left has %q in front, want p1", one.focus)
	}
}

// A window's only pane, let go outside, stays: the window is already
// where it is wanted.
func TestAWindowsOnlyPaneLetGoOutsideStays(t *testing.T) {
	a, _, _ := twoWindowApp(t)
	a.openWindow = func(*gunim.Window, geom.Point, geom.Size) (gunim.Client, *gunim.Window, error) {
		t.Fatal("a window opened for the only pane of one")
		return gunim.Client{}, nil, nil
	}
	a.handle(PaneToNewWindow{Pane: "p3"})
	if a.opening != 0 {
		t.Fatal("a window is on its way")
	}
}

// A window whose last pane moves away goes, and the other stays.
func TestAWindowLeftEmptyGoes(t *testing.T) {
	a, one, two := twoWindowApp(t)
	a.front(one)
	a.handle(PaneToWindow{Pane: "p3"})
	a.leaveEmpty()
	if !two.gone || one.gone || a.cur != one {
		t.Fatalf("the empty window gone %v, the other gone %v, in front %d", two.gone, one.gone, a.cur.id)
	}
}

// Closing a window with panes asks first, there, and closes them.
func TestClosingAWindowAsksAndClosesItsPanes(t *testing.T) {
	a, one, two := twoWindowApp(t)
	a.handle(CloseWindow{})
	if len(a.st.Asks) != 1 || a.st.Asks[0].win != two.id {
		t.Fatalf("closing asked %+v", a.st.Asks)
	}
	if st := a.stateFor(one, a.st); len(st.Asks) != 0 {
		t.Fatal("the other window asks too")
	}
	a.handle(AskAnswered{ID: a.st.Asks[0].ID, Yes: true})
	select {
	case f := <-a.events:
		f()
	case <-time.After(time.Second):
		t.Fatal("the answer went unheard")
	}
	if a.has("p3") || !two.gone || a.cur != one || !a.has("p1") {
		t.Fatalf("closed, p3 open %v, the window gone %v, in front %d", a.has("p3"), two.gone, a.cur.id)
	}
}

// The last window's close button asks as Exit does.
func TestTheLastWindowClosesAsExitDoes(t *testing.T) {
	a := newApp(gunimtest.New(t, geom.Sz(400, 300), nil).Client(), screen.NewShells())
	a.ctx = t.Context()
	a.addPane(Pane{ID: "p1", Title: "one", Kind: KindFiles}, nil, Placement{})
	a.handle(CloseWindow{})
	if !a.leaving {
		t.Fatal("closing the last window did not ask to exit")
	}
}

// A notice shows in the window in front, and in no other.
func TestANoticeShowsInTheWindowInFront(t *testing.T) {
	a, one, two := twoWindowApp(t)
	a.notify("Copied", "", "")
	if n := a.stateFor(two, a.st).Notices; len(n) != 1 {
		t.Fatalf("the window in front shows %d notices", len(n))
	}
	if n := a.stateFor(one, a.st).Notices; len(n) != 0 {
		t.Fatalf("the window behind shows %d notices", len(n))
	}
}

// Going to a pane open in another window, as the jobs pane asked for
// again, brings it into the window in front.
func TestAPaneAskedForAgainComesToTheWindowInFront(t *testing.T) {
	a, one, two := twoWindowApp(t)
	a.bringHere("p1")
	if a.winOf["p1"] != two.id || a.st.Focus != "p1" || one.focus != "p2" {
		t.Fatalf("p1 is in window %d, the focus on %q, and the other window's on %q", a.winOf["p1"], a.st.Focus, one.focus)
	}
}

// Closing the pane in front of a window behind gives that window's
// keyboard to another of its panes, and leaves the front alone.
func TestClosingAPaneBehindRefocusesItsWindow(t *testing.T) {
	a, one, _ := twoWindowApp(t)
	a.remove("p2")
	if one.focus != "p1" || a.st.Focus != "p3" {
		t.Fatalf("the window behind has %q in front, and the one in front %q", one.focus, a.st.Focus)
	}
}

// Each window has its own sidebar: hidden in one, it still shows in
// the other, and each keeps its own width.
func TestEachWindowHasItsOwnSidebar(t *testing.T) {
	a, one, two := twoWindowApp(t)
	a.front(two)
	a.handle(ToggleSidebar{})
	a.handle(SidebarMoved{Width: 300})
	if st := a.stateFor(two, a.st); st.Sidebar || st.SidebarWidth != 300 {
		t.Fatalf("hidden in the second window, it shows %v at %v", st.Sidebar, st.SidebarWidth)
	}
	if st := a.stateFor(one, a.st); !st.Sidebar || st.SidebarWidth != 220 {
		t.Fatalf("the first window's sidebar shows %v at %v", st.Sidebar, st.SidebarWidth)
	}
}
