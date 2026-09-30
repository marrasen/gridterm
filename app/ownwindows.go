package app

import (
	"slices"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/kakel/words"
)

// kakel's own windows on this screen, as against the windows of other
// computers it connects to (see windows.go). Every pane is in one of
// them. The program serves them all: each window shows its own panes,
// with its own pane in front, and the one the user last worked in is
// the window in front, where new panes, questions and notices go.
//
// In the switcher, a pane dragged onto another of the windows moves
// there, and one let go outside every window opens a window of its
// own.

// ownWin is one of kakel's windows.
type ownWin struct {
	id int
	c  gunim.Client
	// gw is the window itself, to open the next window from, and nil in
	// a test.
	gw *gunim.Window
	// focus is the pane with the keyboard here while another window is
	// in front; the window in front keeps its own in the program's
	// state.
	focus string
	// asking says the window asks whether to close, and gone that it is
	// on its way out.
	asking, gone bool
}

// windowIn is an intent from one of the windows, or word that it
// closed.
type windowIn struct {
	w      *ownWin
	env    gunim.Envelope
	closed bool
}

// Intents for the program's own windows.
type (
	// WindowFocused says the window the intent came from has the
	// keyboard, which puts it in front.
	WindowFocused struct{}
	// CloseWindow closes the window it came from, after asking while
	// panes are open there. The last window asks as Exit does.
	CloseWindow struct{}
	// PaneToWindow moves a pane into the window the intent came from,
	// on a stage of its own.
	PaneToWindow struct{ Pane string }
	// PaneToNewWindow moves a pane into a window of its own, opened
	// with its top left corner at At, in the space of the window the
	// intent came from, and Size large.
	PaneToNewWindow struct {
		Pane string
		At   geom.Point
		Size geom.Size
	}
)

// PaneDrag is what a pane dragged out of the switcher carries: the
// pane, and the window it is dragged from.
type PaneDrag struct {
	Pane   string
	Window int
}

// WindowOpener opens another window, placed at at in from's space, and
// returns its client and the window. It runs on a goroutine of its own.
type WindowOpener func(from *gunim.Window, at geom.Point, size geom.Size) (gunim.Client, *gunim.Window, error)

// addWindow adds a window to the program's, and returns it. What it
// asks for is heard once serveWin starts listening.
func (a *app) addWindow(c gunim.Client, gw *gunim.Window) *ownWin {
	a.nextWin++
	w := &ownWin{id: a.nextWin, c: c, gw: gw}
	a.wins = append(a.wins, w)
	if a.cur == nil {
		a.cur = w
	}
	return w
}

// serveWin hands what w asks for to the program's goroutine, and word
// once it has closed.
func (a *app) serveWin(w *ownWin) {
	c := w.c
	go func() {
		for env := range c.Intents() {
			a.intents <- windowIn{w: w, env: env}
		}
		a.intents <- windowIn{w: w, closed: true}
	}()
}

// winByID returns the window numbered id, or nil.
func (a *app) winByID(id int) *ownWin {
	for _, w := range a.wins {
		if w.id == id {
			return w
		}
	}
	return nil
}

// ownerOf returns the window pane id is in, or nil.
func (a *app) ownerOf(id string) *ownWin {
	if n, ok := a.winOf[id]; ok {
		return a.winByID(n)
	}
	return nil
}

// front puts w in front: the program's focus becomes w's.
func (a *app) front(w *ownWin) {
	if w == nil || w == a.cur {
		return
	}
	if a.cur != nil {
		a.cur.focus = a.st.Focus
	}
	a.cur = w
	a.st.Focus = w.focus
}

// focus gives pane id the keyboard, putting its window in front.
func (a *app) focus(id string) {
	a.front(a.ownerOf(id))
	a.st.Focus = id
}

// focusIn returns the pane with the keyboard in w.
func (a *app) focusIn(w *ownWin) string {
	if w == a.cur {
		return a.st.Focus
	}
	return w.focus
}

// setFocusIn gives pane id the keyboard in w, leaving the window in
// front as it is.
func (a *app) setFocusIn(w *ownWin, id string) {
	if w == a.cur {
		a.st.Focus = id
		return
	}
	w.focus = id
}

// panesIn returns the panes in w, in the sidebar's order.
func (a *app) panesIn(w *ownWin) []Pane {
	var out []Pane
	for _, p := range a.st.Panes {
		if a.winOf[p.ID] == w.id {
			out = append(out, p)
		}
	}
	return out
}

// refocus gives w's keyboard to another of its panes once the pane
// with it, gone, has left: next when that is in w, or the pane before
// the one gone, at i in the list of every pane, or else nothing.
func (a *app) refocus(w *ownWin, gone, next string, i int) {
	if w == nil || a.focusIn(w) != gone {
		return
	}
	if next != "" && a.winOf[next] == w.id {
		a.setFocusIn(w, next)
		return
	}
	// The nearest pane before it in the sidebar, or else the first.
	before, first := "", ""
	for k, p := range a.st.Panes {
		if a.winOf[p.ID] != w.id || p.ID == gone {
			continue
		}
		if first == "" {
			first = p.ID
		}
		if k < i {
			before = p.ID
		}
	}
	if before == "" {
		before = first
	}
	a.setFocusIn(w, before)
}

// moveToWindow moves pane id into w, on a stage of its own, with the
// keyboard there.
func (a *app) moveToWindow(id string, w *ownWin) {
	from := a.ownerOf(id)
	if from == nil || from == w || a.closing[id] {
		return
	}
	i := slices.IndexFunc(a.st.Panes, func(p Pane) bool { return p.ID == id })
	after := a.tabAfter(id)
	next := a.take(id)
	if next == "" {
		next = after
	}
	a.winOf[id] = w.id
	a.refocus(from, id, next, i)
	a.place(id, Placement{})
	a.setFocusIn(w, id)
}

// paneToNewWindow opens a window at in.At and moves in.Pane into it.
func (a *app) paneToNewWindow(in PaneToNewWindow) {
	from := a.ownerOf(in.Pane)
	if from == nil || a.closing[in.Pane] {
		return
	}
	if len(a.panesIn(from)) == 1 {
		// Its window's only pane: the window is where it was wanted
		// already.
		return
	}
	a.openWindowThen(in.At, in.Size, func(w *ownWin) bool {
		from := a.ownerOf(in.Pane)
		if from == nil || a.closing[in.Pane] || len(a.panesIn(from)) == 1 {
			return false
		}
		a.moveToWindow(in.Pane, w)
		return true
	})
}

// openWindowThen opens a window at at, size large, in the space of the
// window in front, and once it is open hands it to then, and puts it in
// front. When then says there is nothing for it after all, the window
// closes again.
func (a *app) openWindowThen(at geom.Point, size geom.Size, then func(w *ownWin) bool) {
	if a.openWindow == nil {
		a.failed("Couldn't open another window", "This kakel can't open windows.")
		return
	}
	open, gw := a.openWindow, a.cur.gw
	a.opening++
	go func() {
		c, nw, err := open(gw, at, size)
		a.events <- func() {
			a.opening--
			if err != nil {
				a.failed("Couldn't open another window", err.Error())
				return
			}
			w := a.addWindow(c, nw)
			a.serveWin(w)
			_ = c.SetTheme(a.st.Theme)
			if !then(w) {
				a.letWindowGo(w)
				return
			}
			a.front(w)
		}
	}()
}

// closeWindow closes w, after asking while it holds panes. The last
// window asks as Exit does.
func (a *app) closeWindow(w *ownWin) {
	if len(a.liveWins()) <= 1 {
		a.askToQuit()
		return
	}
	// The Servers pane has nothing to lose, and is not asked about.
	panes := slices.DeleteFunc(a.panesIn(w), func(p Pane) bool { return p.Kind == KindServers })
	if len(panes) == 0 {
		for _, p := range a.panesIn(w) {
			a.remove(p.ID)
		}
		a.letWindowGo(w)
		return
	}
	if w.asking {
		return
	}
	w.asking = true
	a.askThen(a.ctx, Ask{
		Title: "Close this window?", Text: "Still open here: " + words.ManyOf(len(panes), "pane", "panes") + ".",
		Yes: "Close", Danger: true,
	}, func(ans AskAnswered) {
		w.asking = false
		if !ans.Yes || w.gone {
			return
		}
		for _, p := range a.panesIn(w) {
			a.remove(p.ID)
		}
		a.letWindowGo(w)
	})
}

// liveWins returns the windows not on their way out.
func (a *app) liveWins() []*ownWin {
	var out []*ownWin
	for _, w := range a.wins {
		if !w.gone {
			out = append(out, w)
		}
	}
	return out
}

// letWindowGo has w leave, and puts another window in front if it was.
func (a *app) letWindowGo(w *ownWin) {
	if w.gone {
		return
	}
	w.gone = true
	w.c.Leave()
	a.windowLeft(w)
}

// windowLeft puts another window in front of w, gone, and hands it
// w's questions.
func (a *app) windowLeft(w *ownWin) {
	if a.cur != w {
		return
	}
	for _, o := range a.wins {
		if !o.gone {
			a.front(o)
			break
		}
	}
	for i := range a.st.Asks {
		if a.st.Asks[i].win == w.id {
			a.st.Asks[i].win = a.cur.id
		}
	}
}

// windowClosed forgets w, closed, and closes any panes left in it.
func (a *app) windowClosed(w *ownWin) {
	w.gone = true
	a.windowLeft(w)
	for _, p := range a.panesIn(w) {
		a.remove(p.ID)
	}
	a.wins = slices.DeleteFunc(a.wins, func(o *ownWin) bool { return o == w })
}

// leaveEmpty lets a window with no panes go, while another window
// stays open.
func (a *app) leaveEmpty() {
	for _, w := range a.liveWins() {
		if len(a.liveWins()) > 1 && len(a.panesIn(w)) == 0 && !(w == a.cur && (len(a.machines.Dialing()) > 0 || a.opening > 0 || a.starting > 0)) {
			a.letWindowGo(w)
		}
	}
}

// stateFor is st, the state of every window, as w shows it: its own
// panes, its own pane in front, and the questions and notices for it.
func (a *app) stateFor(w *ownWin, st State) State {
	st.Window, st.Behind = w.id, w != a.cur
	st.Panes = a.panesIn(w)
	st.Focus = a.focusIn(w)
	st.Stage = nil
	if st.Focus != "" {
		st.Stage = a.groups[a.groupOf[st.Focus]].clone()
	}
	st.Tabs = a.tabsOf(w)
	st.Groups = map[string]*Box{}
	byGroup := map[int]*Box{}
	for _, p := range st.Panes {
		g, ok := a.groupOf[p.ID]
		if !ok {
			continue
		}
		if byGroup[g] == nil {
			byGroup[g] = a.groups[g].clone()
		}
		st.Groups[p.ID] = byGroup[g]
	}
	st.Asks = slices.DeleteFunc(slices.Clone(st.Asks), func(q Ask) bool { return q.win != w.id })
	st.Notices = slices.DeleteFunc(slices.Clone(st.Notices), func(n Notice) bool { return n.win != w.id })
	return st
}

// bringHere gives pane id the keyboard in the window in front, moving
// it there from another window first, for a pane asked for again: the
// jobs, a log, the help.
func (a *app) bringHere(id string) {
	if w := a.ownerOf(id); w != nil && w != a.cur {
		a.moveToWindow(id, a.cur)
	}
	a.st.Focus = id
}

// frontID numbers the window in front, and is 0 before there is one.
func (a *app) frontID() int {
	if a.cur == nil {
		return 0
	}
	return a.cur.id
}
