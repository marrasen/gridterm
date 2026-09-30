package app

import (
	"slices"

	"github.com/marrasen/gunim/geom"
)

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
	// ToolWindow opens a tool pane, the Servers pane or the secrets by
	// Kind, in a window of its own, opened at At, in the space of the
	// window asking, and Size large. One alone in its window already is
	// shown there.
	ToolWindow struct {
		Kind string
		At   geom.Point
		Size geom.Size
	}
)

// handleServers carries out an intent about the Servers pane, and
// reports whether it was one.
func (a *app) handleServers(in any) bool {
	switch in := in.(type) {
	case ShowServers:
		a.showServers()
	case ToolWindow:
		a.toolWindow(in)
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

// A window holding only tool panes, the Servers pane and the secrets,
// is a tool window: a pane opened from there opens in the window last
// worked in, and a pane asked for again is shown where it is, as a
// toolbar's buttons work on the document under them. work is that
// window.

// isToolKind reports whether a pane of kind is a tool pane.
func isToolKind(kind string) bool { return kind == KindServers || kind == KindSecrets }

// isTool reports whether w holds tool panes and nothing else.
func (a *app) isTool(w *ownWin) bool {
	if w == nil {
		return false
	}
	panes := a.panesIn(w)
	return len(panes) > 0 && !slices.ContainsFunc(panes, func(p Pane) bool { return !isToolKind(p.Kind) })
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

// workFor puts the window last worked in in front, for a pane of kind
// about to open while a tool window is in front, and reports whether it
// did. Asked for in the tool window, that window comes to the front on
// the screen too; a pane arriving by itself later, as a shell once its
// server answers, goes there quietly.
func (a *app) workFor(kind string) bool {
	w := a.work
	if isToolKind(kind) || !a.isTool(a.cur) || w == nil || w.gone || w == a.cur {
		return false
	}
	a.front(w)
	if a.fromTool {
		w.c.ToFront()
	}
	return true
}

// handleFrom carries out an intent from the window in front. From a
// tool window, one that opens a pane does it in the window last worked
// in, as if asked there, on the machine of the pane in front there; that
// window comes to the front once a pane opens. The rest, such as copying
// a secret, stays with the tool window.
func (a *app) handleFrom(in any) {
	tool := a.cur
	if !a.isTool(tool) {
		a.handle(in)
		return
	}
	a.fromTool = true
	defer func() { a.fromTool = false }()
	w := a.work
	if w == nil || w.gone || w == tool || !opensPane(in) {
		a.handle(in)
		return
	}
	before := len(a.st.Panes)
	focus := a.focusIn(w)
	a.front(w)
	a.handle(in)
	switch {
	case a.cur == w && !w.gone && (len(a.st.Panes) != before || a.st.Focus != focus):
		w.c.ToFront()
	case a.cur == w && !tool.gone:
		// Nothing opened, or not yet: the tool window stays in front.
		a.front(tool)
	}
}

// opensPane reports whether intent in opens a pane, or starts one on
// its way, where the user works.
func opensPane(in any) bool {
	switch in.(type) {
	case NewTerminal, SplitPane, ChooseSplit, ConnectTo, OpenFiles, OpenTunnel, OpenSavedTunnel, ShowLog,
		RunCommand, RunSavedCommand, OpenShellNamed, OpenDefaultShell, OpenOn, FilesOn, ShowHelp, ShowCopies,
		AttachWindow, ConnectWindow:
		return true
	}
	return false
}

// toolWindow opens a tool pane in a window of its own.
func (a *app) toolWindow(in ToolWindow) {
	alone := func(id string) {
		if w := a.ownerOf(id); w != nil && len(a.panesIn(w)) > 1 {
			a.paneToNewWindow(PaneToNewWindow{Pane: id, At: in.At, Size: in.Size})
		}
	}
	switch in.Kind {
	case KindServers:
		a.showServers()
		if id := a.serversPane(); id != "" {
			alone(id)
		}
	case KindSecrets:
		a.showSecretsPane(alone)
	}
}
