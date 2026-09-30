package app

import (
	"testing"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/gunimtest"
	"github.com/marrasen/kakel/screen"
)

// The Servers pane opens once, in a tab of its own; asked for again
// from another window, it stays where it is and its window comes to the
// front.
func TestTheServersPaneOpensOnce(t *testing.T) {
	a, one, two := twoWindowApp(t)
	// Past the panes made by hand.
	a.next = 100
	a.front(one)
	a.handle(ShowServers{})
	id := a.serversPane()
	if id == "" || a.winOf[id] != one.id || a.st.Focus != id {
		t.Fatalf("the Servers pane is %q, in window %d; the focus is on %q", id, a.winOf[id], a.st.Focus)
	}
	if len(a.tabsOf(one)) != 3 {
		t.Fatalf("the window has %d tabs, want 3", len(a.tabsOf(one)))
	}
	a.front(two)
	a.handle(ShowServers{})
	if a.serversPane() != id || a.winOf[id] != one.id || a.cur != one || a.st.Focus != id {
		t.Fatalf("asked again, the pane is %q in window %d, window %d in front", a.serversPane(), a.winOf[id], a.cur.id)
	}
}

// Servers on the View menu opens the pane, and closes it again.
func TestToggleServersOpensAndCloses(t *testing.T) {
	a, _, _ := twoWindowApp(t)
	a.next = 100
	a.handle(ToggleServers{})
	id := a.serversPane()
	if id == "" {
		t.Fatal("the Servers pane did not open")
	}
	a.handle(ToggleServers{})
	if a.has(id) && !a.closing[id] {
		t.Fatal("the Servers pane stayed open")
	}
}

// Every window's panes are published to each, saying where each is,
// for the Servers pane.
func TestEveryWindowSeesEveryPane(t *testing.T) {
	a, one, two := twoWindowApp(t)
	all := a.allPanes()
	if len(all) != 3 || all[0].Window != one.id || all[2].Window != two.id {
		t.Fatalf("every pane is %+v", all)
	}
}

// Picking a pane of another window brings that window to the front.
func TestPickingAPaneElsewhereRaisesItsWindow(t *testing.T) {
	w1, w2 := gunimtest.New(t, geom.Sz(400, 300), nil), gunimtest.New(t, geom.Sz(400, 300), nil)
	a := newApp(w1.Client(), screen.NewShells())
	a.ctx = t.Context()
	one := a.cur
	a.addPane(Pane{ID: "p1", Kind: KindFiles}, nil, Placement{})
	two := a.addWindow(w2.Client(), nil)
	a.front(two)
	a.addPane(Pane{ID: "p2", Kind: KindFiles}, nil, Placement{})
	a.handle(FocusPane{Pane: "p2"})
	if w2.Offscreen().Raised() != 0 {
		t.Fatal("the window asking was raised")
	}
	a.handle(FocusPane{Pane: "p1"})
	if a.cur != one || a.st.Focus != "p1" || w1.Offscreen().Raised() != 1 {
		t.Fatalf("window %d in front, the focus on %q, raised %d times", a.cur.id, a.st.Focus, w1.Offscreen().Raised())
	}
}

// toolWindows is a program with the Servers pane alone in a window of
// its own, in front, and the window last worked in behind it holding
// p1 and the jobs.
func toolWindows(t *testing.T) (a *app, work, tool *ownWin, raised func() int) {
	t.Helper()
	w1, w2 := gunimtest.New(t, geom.Sz(400, 300), nil), gunimtest.New(t, geom.Sz(400, 300), nil)
	a = newApp(w1.Client(), screen.NewShells())
	a.ctx = t.Context()
	a.next = 100
	work = a.cur
	a.addPane(Pane{ID: "p1", Kind: KindFiles}, nil, Placement{})
	a.showJobsPane()
	a.focus("p1")
	a.noteWork()
	tool = a.addWindow(w2.Client(), nil)
	a.front(tool)
	a.showServers()
	a.noteWork()
	return a, work, tool, w1.Offscreen().Raised
}

// What the Servers pane in a window of its own asks for happens in the
// window last worked in, which comes to the front: the tool window
// takes no pane.
func TestAToolWindowActsInTheWindowWorkedIn(t *testing.T) {
	a, work, tool, raised := toolWindows(t)
	a.handle(ShowJobs{})
	if a.winOf[a.paneOfKindHere(KindJobs)] != work.id || a.cur != work || raised() != 1 {
		t.Fatalf("the jobs are in window %d, window %d in front, raised %d times", a.winOf[a.paneOfKindHere(KindJobs)], a.cur.id, raised())
	}
	if work.gone || !a.isTool(tool) {
		t.Fatal("the window worked in closed, or the tool window took a pane")
	}
	a.front(tool)
	a.handle(ShowTab{Group: a.groupOf[a.serversPane()]})
	if a.cur != tool {
		t.Fatal("a tab intent of the tool window acted elsewhere")
	}
}

// paneOfKindHere is the first pane of kind, or "".
func (a *app) paneOfKindHere(kind string) string {
	for _, p := range a.st.Panes {
		if p.Kind == kind {
			return p.ID
		}
	}
	return ""
}

// The Servers toggle closes the pane only where it is being looked at;
// elsewhere it brings it.
func TestServersToggleClosesOnlyWhatIsSeen(t *testing.T) {
	a, work, _, _ := toolWindows(t)
	id := a.serversPane()
	a.front(work)
	a.handle(ToggleServers{})
	if a.closing[id] || !a.has(id) || a.st.Focus != id {
		t.Fatalf("toggled out of sight, the pane closed %v, the focus on %q", a.closing[id], a.st.Focus)
	}
	a.handle(ToggleServers{})
	if a.has(id) && !a.closing[id] {
		t.Fatal("toggled where it is seen, the pane stayed")
	}
}

// A window with the Servers pane alone closes without asking.
func TestAToolWindowClosesWithoutAsking(t *testing.T) {
	a, _, tool, _ := toolWindows(t)
	a.closeWindow(tool)
	if !tool.gone || len(a.st.Asks) != 0 || a.serversPane() != "" {
		t.Fatalf("gone %v, %d questions, the Servers pane %q", tool.gone, len(a.st.Asks), a.serversPane())
	}
}

// Open Servers Window opens the Servers pane in a window of its own,
// and Move Tab to New Window moves the tab in front.
func TestServersAndTabsGetWindowsOfTheirOwn(t *testing.T) {
	a, one, _ := twoWindowApp(t)
	a.next = 100
	a.openWindow = func(_ *gunim.Window, _ geom.Point, s geom.Size) (gunim.Client, *gunim.Window, error) {
		return gunimtest.New(t, s, nil).Client(), nil, nil
	}
	a.front(one)
	a.handle(ToolWindow{Kind: KindServers})
	waitFor(t, a, "the Servers window", func() bool { return len(a.wins) == 3 })
	if id := a.serversPane(); !a.isTool(a.ownerOf(id)) {
		t.Fatalf("the Servers pane is in window %d with %d panes", a.ownerOf(id).id, len(a.panesIn(a.ownerOf(id))))
	}
	a.front(one)
	a.focus("p1")
	a.handle(TabToNewWindow{})
	waitFor(t, a, "the tab's window", func() bool { return len(a.wins) == 4 })
	if a.winOf["p1"] == one.id || a.winOf["p2"] != one.id {
		t.Fatalf("p1 is in window %d, p2 in %d", a.winOf["p1"], a.winOf["p2"])
	}
}

// A split asked for in a tool window splits the pane worked in, in its
// window, and a pane arriving by itself later raises nothing.
func TestAToolWindowSplitsWhereTheUserWorks(t *testing.T) {
	a, work, tool, raised := toolWindows(t)
	a.handleFrom(ChooseSplit{})
	if !a.isTool(tool) || a.cur != work || raised() != 1 {
		t.Fatalf("the tool window holds %d panes, window %d in front, raised %d times", len(a.panesIn(tool)), a.cur.id, raised())
	}
	if b := a.groups[a.groupOf["p1"]]; b == nil || b.Pane != "" {
		t.Fatalf("p1's group is %+v, want a split", b)
	}
	a.front(tool)
	a.addPane(Pane{ID: "late", Kind: KindFiles}, nil, Placement{})
	if raised() != 1 || a.winOf["late"] != work.id || !a.isTool(tool) {
		t.Fatalf("a pane arriving by itself raised the window %d times, and is in window %d", raised(), a.winOf["late"])
	}
}

// Copying a secret, or anything else that opens no pane, stays with the
// tool window.
func TestAToolWindowKeepsWhatOpensNothing(t *testing.T) {
	a, _, tool, raised := toolWindows(t)
	a.handleFrom(FontSize{Step: 1})
	if a.cur != tool || raised() != 0 {
		t.Fatalf("window %d in front, raised %d times", a.cur.id, raised())
	}
}
