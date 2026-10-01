package view

import (
	"testing"
	"time"

	"github.com/marrasen/kakel/app"

	"github.com/marrasen/gunim/geom"
	gi "github.com/marrasen/gunim/input"
	"github.com/marrasen/gunim/widget"
)

// twoTabs is a window with two tabs, p1's in front, and p2's.
func twoTabs() app.State {
	b1, b2 := &app.Box{Pane: "p1"}, &app.Box{Pane: "p2"}
	return app.State{
		Window: 1,
		Panes: []app.Pane{
			{ID: "p1", Title: "Jobs", Kind: app.KindJobs},
			{ID: "p2", Title: "Log", Kind: app.KindLog},
		},
		Stage: b1, Focus: "p1",
		Groups: map[string]*app.Box{"p1": b1, "p2": b2},
		Tabs:   []app.Tab{{Group: 1, Pane: "p1", Panes: 1}, {Group: 2, Pane: "p2", Panes: 1}},
	}
}

// settle runs the window a second, for the title bar to slide the tab
// bar into place.
func settle() {
	for range 60 {
		lastWindow.Frame(time.Second / 60)
	}
}

// drain empties the window's intents.
func drain() {
	settle()
	for len(lastWindow.Client().Intents()) > 0 {
		<-lastWindow.Client().Intents()
	}
}

// tabAt is the middle of tab i, in the window's space.
func tabAt(t *testing.T, win *Window, i int) geom.Point {
	t.Helper()
	r, ok := lastUI.Bounds(win.tabs)
	if !ok || i >= len(win.tabs.boxes) {
		t.Fatalf("the tab bar is not drawn, or has no tab %d", i)
	}
	return win.tabs.boxes[i].Center().Add(r.Min)
}

// click presses and lets go the primary button at p.
func click(p geom.Point, b gi.Button) {
	lastWindow.Input(gi.PointerDown{Pos: p, Button: b})
	lastWindow.Input(gi.PointerUp{Pos: p, Button: b})
	lastWindow.Frame(time.Second / 60)
}

// The bar shows with two tabs and names the panes in place of the title
// bar; with one it goes, and the pane's title comes back.
func TestTheTabBarShowsWithTwoTabs(t *testing.T) {
	win, _, publish := windowStage(t)
	publish(twoTabs())
	if !win.tabs.shown() || win.bar.Subtitle != "" || win.bar.Title != "" {
		t.Fatalf("with two tabs the bar shows %v, the title bar says %q, %q", win.tabs.shown(), win.bar.Title, win.bar.Subtitle)
	}
	if win.tabs.front != 1 {
		t.Fatalf("the tab in front is %d, want 1", win.tabs.front)
	}
	st := twoTabs()
	st.Panes, st.Tabs = st.Panes[:1], st.Tabs[:1]
	publish(st)
	if win.tabs.shown() || win.bar.Subtitle != "Jobs" || win.bar.Title != app.ProgramName {
		t.Fatalf("with one tab the bar shows %v, the title bar says %q, %q", win.tabs.shown(), win.bar.Title, win.bar.Subtitle)
	}
}

// A click shows a tab, a middle click closes it, and so does its ×;
// the + opens a terminal.
func TestTheTabsAnswerTheMouse(t *testing.T) {
	win, _, publish := windowStage(t)
	publish(twoTabs())
	drain()
	click(tabAt(t, win, 1), gi.ButtonPrimary)
	if in, ok := nextIntent(t).(app.ShowTab); !ok || in.Group != 2 {
		t.Fatalf("a click sent %#v", in)
	}
	click(tabAt(t, win, 1), gi.ButtonMiddle)
	if in, ok := nextIntent(t).(app.CloseTab); !ok || in.Group != 2 {
		t.Fatalf("a middle click sent %#v", in)
	}
	r, _ := lastUI.Bounds(win.tabs)
	click(win.tabs.crossOf(0).Center().Add(r.Min), gi.ButtonPrimary)
	if in, ok := nextIntent(t).(app.CloseTab); !ok || in.Group != 1 {
		t.Fatalf("a click on the × sent %#v", in)
	}
	click(win.tabs.plus.Center().Add(r.Min), gi.ButtonPrimary)
	if in, ok := nextIntent(t).(app.NewTerminal); !ok {
		t.Fatalf("a click on the + sent %#v", in)
	}
}

// A tab dragged away lifts off, and let go outside every window asks
// for a window of its own, where the image was let go.
func TestATabLetGoOutsideAsksForAWindow(t *testing.T) {
	win, _, publish := windowStage(t)
	publish(twoTabs())
	drain()
	at := tabAt(t, win, 1)
	lastWindow.Input(gi.PointerDown{Pos: at, Button: gi.ButtonPrimary})
	lastWindow.Input(gi.PointerMove{Pos: at.Add(geom.Pt(20, 30))})
	lastWindow.Frame(time.Second / 60)
	if win.tabs.carried != 2 {
		t.Fatalf("the tab did not lift off: %d is carried", win.tabs.carried)
	}
	win.tabs.Handle(gi.DragEnd{Out: true, At: geom.Pt(1200, 300)}, lastUI)
	in, ok := nextIntent(t).(app.TabToNewWindow)
	if !ok || in.Group != 2 || in.At != geom.Pt(1200, 300).Sub(win.tabs.grab) || in.Size != win.size {
		t.Fatalf("let go outside, the bar sent %#v", in)
	}
	if win.tabs.carried != 0 {
		t.Fatal("the tab is still carried")
	}
}

// A tab dropped on the bar lands before the tab whose middle is past
// the pointer, or last.
func TestATabDroppedOnTheBarMovesThere(t *testing.T) {
	win, _, publish := windowStage(t)
	publish(twoTabs())
	drain()
	r, _ := lastUI.Bounds(win.tabs)
	first := win.tabs.boxes[0]
	lastWindow.Input(gi.Drop{Pos: geom.Pt(first.Min.X+4, first.Center().Y).Add(r.Min), Data: app.TabDrag{Group: 2, Window: 1}})
	lastWindow.Frame(time.Second / 60)
	if in, ok := nextIntent(t).(app.MoveTab); !ok || in.Group != 2 || in.Before != 1 {
		t.Fatalf("dropped before the first tab, the bar sent %#v", in)
	}
	lastWindow.Input(gi.Drop{Pos: geom.Pt(win.tabs.plus.Min.X, first.Center().Y).Add(r.Min), Data: app.TabDrag{Group: 9, Window: 3}})
	lastWindow.Frame(time.Second / 60)
	if in, ok := nextIntent(t).(app.MoveTab); !ok || in.Group != 9 || in.Before != 0 {
		t.Fatalf("dropped after the last tab, the bar sent %#v", in)
	}
}

// A tab dropped on a pane joins it in a split on the side it is
// nearest; dropped on its own pane, nothing happens.
func TestATabDroppedOnAPaneDocks(t *testing.T) {
	win, _, publish := windowStage(t)
	publish(twoTabs())
	drain()
	r, ok := lastUI.Bounds(win.paneNode("p1"))
	if !ok {
		t.Fatal("the pane is not drawn")
	}
	left := geom.Pt(r.Min.X+10, r.Center().Y)
	if !win.tabDrop(gi.DragOver{Pos: left, Data: app.TabDrag{Group: 2, Window: 1}}, lastUI) || win.dock.pane != "p1" || !win.dock.first || win.dock.vertical {
		t.Fatalf("over the pane's left edge, the dock is %+v", win.dock)
	}
	lastWindow.Input(gi.Drop{Pos: geom.Pt(r.Center().X, r.Max.Y-10), Data: app.TabDrag{Group: 2, Window: 1}})
	lastWindow.Frame(time.Second / 60)
	if in, ok := nextIntent(t).(app.DockTab); !ok || in.Group != 2 || in.Beside != "p1" || !in.Vertical || in.First {
		t.Fatalf("dropped at the pane's bottom edge, the window sent %#v", in)
	}
	if win.tabDrop(gi.DragOver{Pos: left, Data: app.TabDrag{Group: 1, Window: 1}}, lastUI) {
		t.Fatal("the window took a tab over its own pane")
	}
}

// A tab carried away while its window drops to one tab still hears how
// the drag ends, and the bar works when it shows again.
func TestTheBarWorksAfterHidingMidDrag(t *testing.T) {
	win, _, publish := windowStage(t)
	publish(twoTabs())
	drain()
	at := tabAt(t, win, 1)
	lastWindow.Input(gi.PointerDown{Pos: at, Button: gi.ButtonPrimary})
	lastWindow.Input(gi.PointerMove{Pos: at.Add(geom.Pt(20, 30))})
	lastWindow.Frame(time.Second / 60)
	one := twoTabs()
	one.Panes, one.Tabs = one.Panes[:1], one.Tabs[:1]
	publish(one)
	lastWindow.Input(gi.PointerUp{Pos: at.Add(geom.Pt(20, 30)), Button: gi.ButtonPrimary})
	lastWindow.Frame(time.Second / 60)
	if win.tabs.carried != 0 || win.tabs.pressed != -1 {
		t.Fatalf("hidden, the bar still carries %d, pressed %d", win.tabs.carried, win.tabs.pressed)
	}
	publish(twoTabs())
	drain()
	click(tabAt(t, win, 1), gi.ButtonPrimary)
	if in := nextIntent(t); in != (app.ShowTab{Group: 2}) {
		t.Fatalf("shown again, a click sent %#v", in)
	}
}

// However many tabs there are, the + stays on the bar and room is left
// after it to move the window by; a tab too narrow for its × closes by
// no click on its icon.
func TestManyTabsKeepTheBarUsable(t *testing.T) {
	win, _, publish := windowStage(t)
	st := twoTabs()
	st.Tabs = nil
	for i := range 30 {
		id := "p" + string(rune('a'+i))
		b := &app.Box{Pane: id}
		st.Panes = append(st.Panes, app.Pane{ID: id, Title: "rdp@somewhere: ~/src/kakel", Kind: app.KindJobs})
		st.Groups[id] = b
		st.Tabs = append(st.Tabs, app.Tab{Group: i + 10, Pane: id, Panes: 1})
	}
	publish(st)
	drain()
	r, _ := lastUI.Bounds(win.tabs)
	if win.tabs.plus.Max.X > r.Size().W {
		t.Fatalf("the + ends at %.0f on a bar %.0f wide", win.tabs.plus.Max.X, r.Size().W)
	}
	if got := win.tabs.CaptionRects(r.Size()); len(got) != 1 || got[0].Size().W < captionLeast-1 {
		t.Fatalf("the bar leaves %v to move the window by", got)
	}
	icon := win.tabs.boxes[3].Center().Add(r.Min)
	lastWindow.Input(gi.PointerMove{Pos: icon})
	click(icon, gi.ButtonPrimary)
	if in, ok := nextIntent(t).(app.ShowTab); !ok || in.Group != 13 {
		t.Fatalf("a click on a narrow tab sent %#v", in)
	}
}

// Out of the tree, the Servers pane's list keeps no frames coming for a
// row it had last, and Escape in the list goes back to the pane last
// worked in.
func TestTheServersListRestsOffStage(t *testing.T) {
	win, _, publish := windowStage(t)
	st := withServers(twoPanes("p2", nil))
	st.Focus = "ps"
	st.Working = "p2"
	publish(st)
	drain()
	if !win.listShown || lastUI.Focused() == nil {
		t.Fatal("on stage, the list is not shown or has no keyboard")
	}
	lastWindow.Input(gi.KeyPress{Key: gi.KeyEscape})
	lastWindow.Frame(time.Second / 60)
	if in := nextIntent(t); in != (app.FocusPane{Pane: "p2"}) {
		t.Fatalf("Escape sent %#v", in)
	}
	publish(twoPanes("p2", nil))
	if win.listShown || win.anyBreathing(time.Now()) {
		t.Fatal("off stage, the list is taken as shown")
	}
}

// In a narrow pane the Servers pane's buttons keep to their icons, and
// stay inside it.
func TestTheServersButtonsFitANarrowPane(t *testing.T) {
	win, _, publish := windowStageOf(t, geom.Sz(300, 400))
	publish(withServers(app.State{}))
	settle()
	pane, _ := lastUI.Bounds(win.serversView)
	for _, b := range []*widget.Button{win.serversView.quick, win.serversView.add, win.serversView.attach} {
		r, _ := lastUI.Bounds(b)
		if r.Min.X < pane.Min.X || r.Max.X > pane.Max.X || b.Label != "" || b.Tooltip == "" {
			t.Fatalf("a button is at %v in a pane at %v, labelled %q", r, pane, b.Label)
		}
	}
}

// threeTabs is twoTabs with a third tab, p3's, last.
func threeTabs() app.State {
	st := twoTabs()
	b3 := &app.Box{Pane: "p3"}
	st.Panes = append(st.Panes, app.Pane{ID: "p3", Title: "Files", Kind: app.KindJobs})
	st.Groups["p3"] = b3
	st.Tabs = append(st.Tabs, app.Tab{Group: 3, Pane: "p3", Panes: 1})
	return st
}

// A tab opened grows in where it lands, and one closed shrinks away
// while the tabs after it slide into its place. The window's first
// tabs are simply there.
func TestTabsOpenAndCloseInMotion(t *testing.T) {
	win, _, publish := windowStage(t)
	publish(twoTabs())
	settle()
	b := win.tabs
	if r, open := b.drawnAt(1); r != b.boxes[1] || open != 1 {
		t.Fatalf("a first tab is drawn at %v, %v open, not where it is", r, open)
	}
	publish(threeTabs())
	lastWindow.Frame(time.Second / 60)
	lastWindow.Frame(time.Second / 60)
	r, open := b.drawnAt(2)
	if r.Size().W >= b.boxes[2].Size().W || open >= 1 || r.Min.X != b.boxes[2].Min.X {
		t.Fatalf("an opened tab starts at %v, %v open, want narrower than %v", r, open, b.boxes[2])
	}
	settle()
	if r, open := b.drawnAt(2); r != b.boxes[2] || open != 1 {
		t.Fatalf("settled, the opened tab is at %v, %v open, want %v", r, open, b.boxes[2])
	}
	// The middle tab closes: it shrinks where it was, and the last
	// slides left into its place.
	was := b.boxes[2]
	st := threeTabs()
	st.Panes = append(st.Panes[:1], st.Panes[2])
	st.Tabs = append(st.Tabs[:1], st.Tabs[2])
	delete(st.Groups, "p2")
	publish(st)
	lastWindow.Frame(time.Second / 60)
	lastWindow.Frame(time.Second / 60)
	if len(b.gone) != 1 || b.gone[0].tab.Group != 2 {
		t.Fatalf("closing, the bar draws %d gone tabs", len(b.gone))
	}
	if r, _ := b.drawnAt(1); r.Min.X <= b.boxes[1].Min.X || r.Min.X >= was.Min.X {
		t.Fatalf("the last tab is at %v, want between %v and %v", r, was, b.boxes[1])
	}
	settle()
	if len(b.gone) != 0 {
		t.Fatalf("settled, %d closed tabs are still drawn", len(b.gone))
	}
	if r, _ := b.drawnAt(1); r != b.boxes[1] {
		t.Fatalf("settled, the last tab is at %v, want %v", r, b.boxes[1])
	}
}
