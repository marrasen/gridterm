package main

import (
	"strconv"
	"strings"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/marrasen/gridterm/ui"
)

// Full screen is the window filling the screen with nothing around
// the panes: no row of menu titles and no sidebar.
//
// Both go because full screen is asked for to read something, and the
// two of them are what a window puts around what is being read. The
// keys still work: the palette opens, F10 opens the menu bar, and
// Ctrl+Shift+B brings the sidebar back without leaving full screen.

// FullScreen reports whether the window is filling the screen.
func (a *app) FullScreen() bool { return a.full }

// toggleFullScreen fills the screen with the panes, or puts the
// window back the way it was.
func (a *app) toggleFullScreen() error {
	return a.setFullScreen(!a.full)
}

// setFullScreen goes full screen or comes back out of it.
func (a *app) setFullScreen(on bool) error {
	if on == a.full {
		return nil
	}
	a.full = on
	if on {
		// Remembered so coming out puts back what was there, rather
		// than a sidebar somebody had closed themselves.
		a.sideWas = a.dock != nil && !a.dock.Collapsed
	}
	if a.bar != nil {
		a.bar.Hidden = on
	}
	want := a.sideWas
	if on {
		want = false
	}
	if a.dock != nil {
		if err := a.showPanel(want); err != nil {
			return err
		}
	}
	ebiten.SetFullscreen(on)
	a.relayout()
	a.markDirty()
	return nil
}

// askToQuit asks before the window goes, and says what is still open.
//
// However the window is being closed: the menu line, the key, and the
// close button on the title bar all come here. A window holding a
// half-finished copy and three shells is not one to lose to a
// mis-click.
func (a *app) askToQuit() {
	if a.leaving {
		// Already asking. The close button fires for as long as it is
		// held, and a dialog a frame would be unanswerable.
		return
	}
	open := a.whatIsOpen()
	if len(open) == 0 {
		a.quit.Store(true)
		return
	}
	a.leaving = true
	f := a.newConfirm(dlgExit,
		wrapLines("Still open: "+listOf(open)+".", errorLineWidth))
	f.AddButton(ui.Button{Title: btnExit, Do: func() error {
		a.quit.Store(true)
		return nil
	}})
	f.AddButton(ui.Button{Title: btnCancel})
	// Opens on the button that changes nothing.
	f.FocusButton(1)
	a.showForm(f, func() { a.leaving = false })
}

// whatIsOpen is what the window would take with it, worst first.
//
// File work first: a copy part way through is the one thing here that
// cannot simply be started again where it left off.
func (a *app) whatIsOpen() []string {
	var out []string
	if n := len(a.jobs); n > 0 {
		out = append(out, count(n, "piece of file work", "pieces of file work"))
	}
	if n := len(a.panes); n > 0 {
		out = append(out, count(n, "pane", "panes"))
	}
	if n := len(a.tunnels); n > 0 {
		out = append(out, count(n, "tunnel", "tunnels"))
	}
	if n := a.machines.count(); n > 0 {
		out = append(out, count(n, "connection", "connections"))
	}
	if a.agents.sharing() {
		out = append(out, "an agent share")
	}
	return out
}

// count writes a number and the word for it.
func count(n int, one, many string) string {
	what := many
	if n == 1 {
		what = one
	}
	return strconv.Itoa(n) + " " + what
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

// aboutCommand says what this is, and aboutTitle names both the dialog
// and the line that opens it.
const (
	aboutCommand = "app.about"
	aboutTitle   = "About gridterm"
)

// showAbout says what this is and which build it is.
//
// The version is here because it is the first thing a bug report needs
// and nothing else in the window says it. Beside OK is the button that
// asks GitHub whether there is a newer one.
func (a *app) showAbout() error {
	n := a.newNotice(aboutTitle, strings.Join([]string{
		"A GPU-rendered terminal emulator for Windows.",
		"",
		"Version: " + thisVersion(),
	}, "\n"))
	n.Action = ui.NoticeAction{Title: btnCheckUpdates, Do: a.checkForUpdates}
	// Enter dismisses the dialog. Reaching the network is a thing to
	// choose, not a thing to land on.
	n.FocusOK()
	a.presentNotice(n)
	return nil
}

// closingWindow reports whether the operating system is asking the
// window to close, which gridterm answers itself rather than letting
// ebiten end the process.
func (a *app) closingWindow() bool { return ebiten.IsWindowBeingClosed() }

// watchForClosing asks about the close button rather than obeying it.
func (a *app) watchForClosing() {
	if a.quit.Load() || !a.closingWindow() {
		return
	}
	a.askToQuit()
}

// fullScreenCommand fills the screen, and is its own constant because
// the menu, the key and the palette all name it.
const fullScreenCommand = "view.fullScreen"

// panelShowing reports whether the sidebar is open, for the tick on
// the row that turns it off.
func (a *app) panelShowing() bool { return a.dock != nil && !a.dock.Collapsed }
