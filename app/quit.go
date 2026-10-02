package app

import (
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/marrasen/kakel/jobs"
	"github.com/marrasen/kakel/machines"
	"github.com/marrasen/kakel/settings"
	"github.com/marrasen/kakel/words"
)

// Closing the window, however it is asked for: the menu, the key, or
// the close button on the title bar. It asks first while anything is
// open, and says what: a window holding a copy half
// done and three shells is not one to lose to a slip of the mouse.

// askToQuit asks before the window goes, and says what is still open.
func (a *app) askToQuit() {
	if a.leaving {
		// Already asking.
		return
	}
	open := a.whatIsOpen()
	if len(open) == 0 {
		a.exitNow()
		return
	}
	a.leaving = true
	go func() {
		_, err := a.ask(a.ctx, Ask{
			Title: "Exit kakel?", Text: "Still open: " + listOf(open) + ".",
			Yes: "Exit", Danger: true,
		})
		a.events <- func() {
			a.leaving = false
			if err == nil {
				a.exitNow()
			} else {
				// A restart asked for is not done by a quit long after.
				restartInto = ""
			}
		}
	}()
}

// exitNow closes every pane, which closes the window.
func (a *app) exitNow() {
	a.takeSecretBack()
	a.leave()
}

// leave lets every window animate out with what it shows, and closes
// the panes once they have gone: see the run loop. Nothing is
// published meanwhile, so each window leaves as the user last saw it.
func (a *app) leave() {
	if a.gone {
		return
	}
	a.gone = true
	a.closeLauncher()
	a.closeFileManager()
	a.keepWindowPlace()
	for _, w := range a.wins {
		if !w.gone {
			w.c.Leave()
		}
	}
}

// closeAll closes every pane, as the window goes.
func (a *app) closeAll() {
	for len(a.st.Panes) > 0 {
		a.remove(a.st.Panes[0].ID)
	}
	a.hangUp()
}

// hangUpWait is the longest the program waits, on its way out, for its
// connections to close politely.
const hangUpWait = 2 * time.Second

// hangUp closes every connection, to servers, through jump hosts and to
// other windows, so each far end hears goodbye rather than a socket that
// went. It waits hangUpWait at most.
func (a *app) hangUp() {
	var closers []func() error
	a.machines.Each(func(_ machines.ID, m *machines.Machine) {
		if m.Conn != nil {
			closers = append(closers, m.Conn.Close)
		}
		if w := m.Window; w != nil {
			w.Leaving = true
			closers = append(closers, w.Serve.Close)
		}
	})
	for _, c := range a.machines.Hops() {
		closers = append(closers, c.Close)
	}
	if len(closers) == 0 {
		return
	}
	var wg sync.WaitGroup
	for _, c := range closers {
		wg.Go(func() { _ = c() })
	}
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(hangUpWait):
	}
}

// whatIsOpen is what the window would take with it, worst first: file
// work part way through is the one thing that cannot be started again
// where it left off. A connection with nothing on it is not asked
// about: closing it loses nothing.
func (a *app) whatIsOpen() []string {
	var out []string
	// Copies and deletes running, kakel's own and those of the file
	// manager's windows: these, a file manager window counts itself.
	copies, deletes := 0, 0
	for _, r := range a.running {
		if r.quiet || r.job.Progress().Done {
			continue
		}
		if r.op.Kind == jobs.Delete {
			deletes++
		} else {
			copies++
		}
	}
	for _, w := range a.fileWins {
		copies += w.Running()
	}
	if copies > 0 {
		out = append(out, words.ManyOf(copies, "copy running", "copies running"))
	}
	if deletes > 0 {
		out = append(out, words.ManyOf(deletes, "delete running", "deletes running"))
	}
	// The tool panes have nothing to lose, and a connection with nothing
	// on it nothing to end.
	panes := slices.DeleteFunc(slices.Clone(a.st.Panes), func(p Pane) bool { return isToolKind(p.Kind) })
	if n := len(panes); n > 0 {
		out = append(out, words.ManyOf(n, "pane", "panes"))
	}
	if n := len(a.tunnels); n > 0 {
		out = append(out, words.ManyOf(n, "tunnel", "tunnels"))
	}
	if len(a.agents.by) > 0 {
		out = append(out, "an agent share")
	}
	if a.serving.server != nil {
		out = append(out, "this window, served")
	}
	return out
}

// listOf writes a few things as a person would say them.
func listOf(what []string) string {
	switch len(what) {
	case 0:
		return "nothing"
	case 1:
		return what[0]
	}
	return strings.Join(what[:len(what)-1], ", ") + " and " + what[len(what)-1]
}

// keepWindowPlace writes down where the window in front is, and how
// big, and whether it is maximized, for the next start to open it
// there. A minimized one is kept as it was before.
func (a *app) keepWindowPlace() { a.keepPlaceOf(a.cur) }

// keepPlaceOf keeps where w is, for the next window opened with none
// open.
func (a *app) keepPlaceOf(w *ownWin) {
	if a.settings == nil || w == nil || w.gw == nil {
		return
	}
	p, ok := w.gw.Placement()
	if !ok || p.Bounds.Empty() {
		return
	}
	b := p.Bounds
	a.keep("where the window is", a.settings.PutWindow(settings.WindowPlace{X: b.Min.X, Y: b.Min.Y, W: b.Size().W, H: b.Size().H, Maximized: p.Maximized}))
}
