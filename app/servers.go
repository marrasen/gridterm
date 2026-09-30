package app

import "slices"

// The Servers pane lists every machine kakel knows, with how its
// connection is doing, and under each the panes open on it in every
// window. It replaces the sidebar each window had: there is one, which
// is a tab like any other pane, and can have a window of its own.
// Picking a pane there brings that pane's window to the front.

// KindServers is the Servers pane.
const KindServers = "servers"

// Intents for the Servers pane.
type (
	// ShowServers opens the Servers pane in a tab of its own, or goes
	// to it where it is, bringing its window to the front.
	ShowServers struct{}
	// ToggleServers opens the Servers pane, or closes it while it is
	// open.
	ToggleServers struct{}
)

// handleServers carries out an intent about the Servers pane, and
// reports whether it was one.
func (a *app) handleServers(in any) bool {
	switch in.(type) {
	case ShowServers:
		a.showServers()
	case ToggleServers:
		// Closed only where it is what the user is looking at; out of
		// sight, the toggle brings it.
		if id := a.serversPane(); id != "" && a.focusIn(a.cur) == id {
			a.closePane(id)
		} else {
			a.showServers()
		}
	default:
		return false
	}
	return true
}

// serversPane returns the Servers pane, or "" while it is closed.
func (a *app) serversPane() string {
	for _, p := range a.st.Panes {
		if p.Kind == KindServers && !a.closing[p.ID] {
			return p.ID
		}
	}
	return ""
}

// showServers goes to the Servers pane, opening it in the window in
// front when it is closed.
func (a *app) showServers() {
	if id := a.serversPane(); id != "" {
		a.focusRaised(id)
		return
	}
	a.next++
	a.addPane(Pane{ID: "p" + itoa(a.next), Title: "Servers", Kind: KindServers}, nil, Placement{})
}

// focusRaised gives pane id the keyboard, and brings its window to the
// front on the screen when that is not the window asking.
func (a *app) focusRaised(id string) {
	if w := a.ownerOf(id); w != nil && w != a.cur && !w.gone {
		w.c.ToFront()
	}
	a.focus(id)
}

// allPanes returns every window's panes, each saying its window.
func (a *app) allPanes() []Pane {
	out := make([]Pane, 0, len(a.st.Panes))
	for _, p := range a.st.Panes {
		p.Window = a.winOf[p.ID]
		out = append(out, p)
	}
	return out
}

// A window holding the Servers pane alone is a tool window: what is
// asked for there is for the window last worked in, as a toolbar's
// buttons are for the document under them. work is that window.

// isTool reports whether w holds the Servers pane and nothing else.
func (a *app) isTool(w *ownWin) bool {
	panes := a.panesIn(w)
	return len(panes) > 0 && !slices.ContainsFunc(panes, func(p Pane) bool { return p.Kind != KindServers })
}

// noteWork keeps the window in front as the one worked in, unless it is
// a tool window.
func (a *app) noteWork() {
	if a.cur != nil && !a.cur.gone && !a.isTool(a.cur) && len(a.panesIn(a.cur)) > 0 {
		a.work = a.cur
	}
	if a.work != nil && a.work.gone {
		a.work = nil
	}
}

// handleFrom carries out an intent from the window in front. From a
// tool window, one that opens or brings a pane does it in the window
// last worked in, which comes to the front on the screen as well.
func (a *app) handleFrom(in any) {
	tool := a.cur
	w := a.work
	if !a.isTool(tool) || w == nil || w.gone || w == tool || !actsElsewhere(in) {
		a.handle(in)
		return
	}
	a.front(w)
	a.handle(in)
	if a.cur == w && !w.gone {
		w.c.ToFront()
	}
}

// actsElsewhere reports whether intent in, from a tool window, is for
// the window last worked in: all but what is about the tool window
// itself, its tabs, and the questions it shows.
func actsElsewhere(in any) bool {
	switch in.(type) {
	case WindowFocused, CloseWindow, ShowTab, NextTab, MoveTab, ShiftTab, DockTab, CloseTab, TabToNewWindow,
		ShowServers, ToggleServers, SplitMoved, DialogClosed, AskAnswered, PaneToWindow, PaneToNewWindow:
		return false
	}
	return true
}
