package app

import (
	"slices"
	"strconv"

	"github.com/marrasen/gunim/geom"
)

// A window's tabs are its groups: each group of panes, alone or in
// splits, is a tab, and the tab in front is the group on stage. The
// tabs keep an order of their own, new ones last, which the user
// changes by dragging them. A tab dragged to another window moves there
// whole; one dropped on a pane joins it in a split; one let go outside
// every window opens a window of its own.

// Tab is one of a window's tabs.
type Tab struct {
	// Group numbers it, for the intents about it.
	Group int
	// Pane is the pane in it that last had the keyboard, whose title
	// the tab shows, and Panes counts the panes in it.
	Pane  string
	Panes int
}

// TabDrag is what a tab dragged out of its window's tab bar carries:
// the tab, and the window it is dragged from.
type TabDrag struct {
	Group  int
	Window int
}

// Intents for tabs.
type (
	// ShowTab puts a tab in front, with the keyboard on the pane in it
	// that had it last.
	ShowTab struct{ Group int }
	// NextTab puts the tab after the one in front in front, or the one
	// before with Back, going round.
	NextTab struct{ Back bool }
	// MoveTab moves a tab to just before the tab Before, or last when
	// Before is 0, in the window the intent came from, and puts it in
	// front. A tab of another window moves into this one.
	MoveTab struct{ Group, Before int }
	// ShiftTab moves the tab in front one place later, or one earlier
	// with Back.
	ShiftTab struct{ Back bool }
	// DockTab takes a tab's panes into a split beside the pane Beside:
	// to its right, or below it with Vertical, or to its left or above
	// it with First. The tab goes.
	DockTab struct {
		Group           int
		Beside          string
		Vertical, First bool
	}
	// CloseTab closes every pane in a tab, or in the tab in front when
	// Group is 0.
	CloseTab struct{ Group int }
	// TabToNewWindow moves a tab into a window of its own, opened with
	// its top left corner at At, in the space of the window the intent
	// came from, and Size large. Group 0 is the tab in front.
	TabToNewWindow struct {
		Group int
		At    geom.Point
		Size  geom.Size
	}
)

// handleTab carries out an intent about tabs, and reports whether it
// was one.
func (a *app) handleTab(in any) bool {
	switch in := in.(type) {
	case ShowTab:
		// Only a tab of the window it was asked in, whose bar may be a
		// step behind.
		if a.groupWin(in.Group) == a.cur {
			a.showTab(in.Group)
		}
	case NextTab:
		a.nextTab(in.Back)
	case MoveTab:
		a.moveTab(in.Group, in.Before)
	case ShiftTab:
		a.shiftTab(in.Back)
	case DockTab:
		a.dockTab(in)
	case CloseTab:
		g := in.Group
		if g == 0 {
			g = a.groupOf[a.st.Focus]
		}
		if a.groupWin(g) == a.cur {
			a.closeTab(g)
		}
	case TabToNewWindow:
		a.tabToNewWindow(in)
	default:
		return false
	}
	return true
}

// closeTab closes every pane in group g at once: folding them one by
// one would hand the keyboard to panes on their way out.
func (a *app) closeTab(g int) {
	for _, id := range a.groups[g].leaves(nil) {
		if !a.closing[id] {
			a.remove(id)
		}
	}
}

// groupWin returns the window group g is in, or nil for no such group.
func (a *app) groupWin(g int) *ownWin {
	l := a.groups[g].leaves(nil)
	if len(l) == 0 {
		return nil
	}
	return a.ownerOf(l[0])
}

// sortTabs brings the tabs' order up to date: groups gone leave it, and
// new ones join it last, oldest first.
func (a *app) sortTabs() {
	a.tabOrder = slices.DeleteFunc(a.tabOrder, func(g int) bool { return a.groups[g] == nil })
	var fresh []int
	for g := range a.groups {
		if !slices.Contains(a.tabOrder, g) {
			fresh = append(fresh, g)
		}
	}
	slices.Sort(fresh)
	a.tabOrder = append(a.tabOrder, fresh...)
}

// tabsOf returns w's tabs, in order.
func (a *app) tabsOf(w *ownWin) []Tab {
	a.sortTabs()
	var out []Tab
	for _, g := range a.tabOrder {
		if a.groupWin(g) != w {
			continue
		}
		l := a.groups[g].leaves(nil)
		out = append(out, Tab{Group: g, Pane: a.tabPane(g), Panes: len(l)})
	}
	return out
}

// tabPane returns the pane in group g that last had the keyboard, or
// its first when that has left it.
func (a *app) tabPane(g int) string {
	if id := a.groupFocus[g]; id != "" && a.groupOf[id] == g {
		return id
	}
	if l := a.groups[g].leaves(nil); len(l) > 0 {
		return l[0]
	}
	return ""
}

// noteTabFocus keeps, for each window, which pane of its tab in front
// has the keyboard, so the tab brings it back when it is shown again.
func (a *app) noteTabFocus() {
	for _, w := range a.liveWins() {
		if id := a.focusIn(w); id != "" {
			if g, ok := a.groupOf[id]; ok {
				a.groupFocus[g] = id
			}
		}
	}
	for g := range a.groupFocus {
		if a.groups[g] == nil {
			delete(a.groupFocus, g)
		}
	}
}

// showTab puts group g in front, in its window, with that window in
// front.
func (a *app) showTab(g int) {
	a.noteTabFocus()
	if id := a.tabPane(g); id != "" {
		a.focus(id)
	}
}

// frontTab returns the window in front's tabs, and where the one in
// front is among them, -1 for none.
func (a *app) frontTab() ([]Tab, int) {
	tabs := a.tabsOf(a.cur)
	g, ok := a.groupOf[a.st.Focus]
	if !ok {
		return tabs, -1
	}
	return tabs, slices.IndexFunc(tabs, func(t Tab) bool { return t.Group == g })
}

func (a *app) nextTab(back bool) {
	tabs, i := a.frontTab()
	if len(tabs) < 2 || i < 0 {
		return
	}
	step := 1
	if back {
		step = len(tabs) - 1
	}
	a.showTab(tabs[(i+step)%len(tabs)].Group)
}

func (a *app) shiftTab(back bool) {
	tabs, i := a.frontTab()
	if i < 0 {
		return
	}
	switch {
	case back && i > 0:
		a.moveTab(tabs[i].Group, tabs[i-1].Group)
	case !back && i+2 < len(tabs):
		a.moveTab(tabs[i].Group, tabs[i+2].Group)
	case !back && i+1 < len(tabs):
		a.moveTab(tabs[i].Group, 0)
	}
}

// moveTab moves group g to just before group before, 0 for last, in
// the window in front, and puts it in front there.
func (a *app) moveTab(g, before int) {
	if a.groups[g] == nil || g == before || !a.movable(g) {
		return
	}
	a.groupToWindow(g, a.cur)
	a.sortTabs()
	a.tabOrder = slices.DeleteFunc(a.tabOrder, func(o int) bool { return o == g })
	i := slices.Index(a.tabOrder, before)
	if before == 0 || i < 0 || a.groupWin(before) != a.cur {
		// Last among this window's tabs: last of all does that.
		i = len(a.tabOrder)
	}
	a.tabOrder = slices.Insert(a.tabOrder, i, g)
	a.showTab(g)
}

// movable reports whether group g may move: not while a pane in it is
// folding away.
func (a *app) movable(g int) bool {
	for _, id := range a.groups[g].leaves(nil) {
		if a.closing[id] {
			return false
		}
	}
	return true
}

// groupToWindow moves group g's panes into w, whole. The window it
// left puts its next tab in front.
func (a *app) groupToWindow(g int, w *ownWin) {
	from := a.groupWin(g)
	if from == nil || from == w {
		return
	}
	a.noteTabFocus()
	after := a.tabBeside(from, g)
	gone := a.focusIn(from)
	for _, id := range a.groups[g].leaves(nil) {
		a.winOf[id] = w.id
	}
	if a.groupOf[gone] == g {
		a.setFocusIn(from, a.tabPane(after))
	}
	a.setFocusIn(w, a.tabPane(g))
}

// tabAfter returns the pane to give the keyboard to when pane id
// leaves its window, where it is its tab's only pane: the pane of the
// tab beside, or "" otherwise.
func (a *app) tabAfter(id string) string {
	g, ok := a.groupOf[id]
	if !ok || a.groups[g].Pane != id {
		return ""
	}
	w := a.ownerOf(id)
	if w == nil {
		return ""
	}
	if after := a.tabBeside(w, g); after != 0 {
		return a.tabPane(after)
	}
	return ""
}

// tabBeside returns the tab w puts in front when group g leaves it: the
// one after it, or the one before when it was last, and 0 when it was
// the only one.
func (a *app) tabBeside(w *ownWin, g int) int {
	tabs := a.tabsOf(w)
	i := slices.IndexFunc(tabs, func(t Tab) bool { return t.Group == g })
	switch {
	case i < 0 || len(tabs) < 2:
		return 0
	case i+1 < len(tabs):
		return tabs[i+1].Group
	}
	return tabs[i-1].Group
}

// dockTab takes a tab's panes into a split beside a pane of another
// tab, in the window in front.
func (a *app) dockTab(in DockTab) {
	to, ok := a.groupOf[in.Beside]
	// Only beside a pane of the window the tab was dropped on.
	if !ok || a.groups[in.Group] == nil || to == in.Group || !a.movable(in.Group) || a.closing[in.Beside] || a.ownerOf(in.Beside) != a.cur {
		return
	}
	a.groupToWindow(in.Group, a.cur)
	moved := a.groups[in.Group]
	a.splits++
	box := &Box{
		ID: "s" + strconv.Itoa(a.splits), Vertical: in.Vertical, Share: 0.5, Opening: true,
		A: &Box{Pane: in.Beside}, B: moved,
	}
	if in.First {
		box.A, box.B = moved, box.A
	}
	a.groups[to] = a.groups[to].replace(in.Beside, box)
	pane := a.tabPane(in.Group)
	for _, id := range moved.leaves(nil) {
		a.groupOf[id] = to
	}
	delete(a.groups, in.Group)
	delete(a.groupFocus, in.Group)
	a.sortTabs()
	a.focus(pane)
}

// tabToNewWindow opens a window at in.At and moves a tab into it.
func (a *app) tabToNewWindow(in TabToNewWindow) {
	if in.Group == 0 {
		in.Group = a.groupOf[a.st.Focus]
	}
	from := a.groupWin(in.Group)
	if from == nil || !a.movable(in.Group) {
		return
	}
	if len(a.tabsOf(from)) == 1 {
		// Its window's only tab: the window is where it was wanted
		// already.
		return
	}
	a.openWindowThen(in.At, in.Size, func(w *ownWin) bool {
		// Asked again now it is open: the tab may have closed, or been
		// left its window's only one, meanwhile.
		from := a.groupWin(in.Group)
		if from == nil || !a.movable(in.Group) || len(a.tabsOf(from)) == 1 {
			return false
		}
		a.groupToWindow(in.Group, w)
		return true
	})
}
