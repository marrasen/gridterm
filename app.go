package main

import (
	"context"
	"log"
	"sync/atomic"
	"time"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/glyph"
	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/input"
	"github.com/marrasen/gridterm/input/ebitenin"
	"github.com/marrasen/gridterm/jobs"
	"github.com/marrasen/gridterm/meter"
	"github.com/marrasen/gridterm/remote"
	"github.com/marrasen/gridterm/render"
	"github.com/marrasen/gridterm/session"
	"github.com/marrasen/gridterm/ui"
	"github.com/marrasen/gridterm/ui/term"
	"github.com/marrasen/gridterm/vt"
)

// exitQueue is how many "a shell has gone" notices are held before the
// drawing goroutine collects them. One per pane is plenty; the channel
// only has to avoid blocking the goroutine reporting it.
const exitQueue = 64

// jobsGrace is how long a window that is closing waits for the file work
// to stop.
//
// Cancelling cannot interrupt a read or a write already under way, so a
// job on a machine that has stopped answering is let go of rather than
// waited for: the connections close on the way out, which is what ends
// it.
const jobsGrace = 2 * time.Second

// Font size limits and the step the zoom commands move by.
const (
	defaultFontSize = 15
	minFontSize     = 6
	maxFontSize     = 72
	fontStep        = 1
)

// app is the window: it owns the atlas, the compositor and the widget
// tree, and turns ebiten's callbacks into toolkit ones. Everything a
// terminal does lives in the widget, not here.
type app struct {
	atlas    *glyph.Atlas
	renderer *render.Renderer
	comp     *render.Compositor
	root     ui.Root

	// g is the layer the widget tree draws on. Splits and tabs divide it
	// up; a dialog gets a layer of its own above it.
	g     *grid.Grid
	layer *render.Layer

	reader ebitenin.Reader
	mouse  ebitenin.MouseReader

	// modals is the dialog stack, each with a layer of its own above the
	// tree so that closing one costs a blit rather than a repaint of
	// everything underneath.
	modals []*modal

	// palette is the command dialog while it is open, and dismissPalette
	// is what takes it away.
	palette        *ui.Palette
	dismissPalette func()

	// bar is the row of menu titles at the top of the window.
	bar *ui.Menubar

	// dock holds the connections panel beside everything else, and panel
	// is the list in it.
	dock  *ui.Dock
	panel *ui.List

	// registry is everything the window has open, which is what the
	// panel draws.
	registry *conns.Registry

	// machines are the connections the window is holding, by the name
	// the panel calls each one. A second terminal on a machine rides on
	// the connection already here rather than logging in again.
	machines map[string]*machine

	// opening names the machines being connected to right now, so two
	// connections to one machine cannot be made at once: the window
	// would hold the second and close neither.
	opening map[string]bool

	// tunnels are the forwards the window is holding, by the panel row
	// that stands for each. They ride on a connection, so closing that
	// closes them; this is what takes their rows away with it.
	tunnels map[*conns.Entry]*tunnel

	// browsers are the file browsers the window has open, by the widget
	// each one is. They are not terminals, so the pane bookkeeping does
	// not cover them.
	browsers map[ui.Widget]*browser

	// queue is the file work running in the background, and jobs are the
	// panel rows that stand for each piece of it.
	queue *jobs.Queue
	jobs  map[*conns.Entry]*jobs.Job

	// asking is how to take away a question a job is waiting on, by the
	// channel the answer goes back through. A job given up on while its
	// question is up has to take the question with it.
	asking map[chan jobs.Choice]func()

	// paneOn says which connection a pane is running on. Panes are
	// grouped on the panel by a name, and a name can mean two things at
	// once -- the machine -ssh put every pane on, and a connection made
	// from the window -- so closing one connection must find its own
	// panes rather than everything under that name.
	paneOn map[*term.Terminal]*machine

	// localHost is the machine a new pane runs on: this one, unless
	// -ssh named another. Every pane a split or a tab opens goes there,
	// because that is where newSession puts it.
	localHost string

	// rates turn a connection's running totals into a speed. One per
	// connection, because a speed is a difference between two moments
	// and each has its own.
	rates map[*conns.Entry]*meter.Rate

	// pump carries work from the goroutines connecting to machines to
	// this one, which is the only one that may touch the widget tree.
	pump pump

	// ctx is cancelled when the window closes, which lets go of every
	// connection still being made and every dialog waiting for an answer.
	ctx  context.Context
	stop context.CancelFunc

	// keys holds private keys the user has unlocked, so a passphrase is
	// asked for once rather than once per connection.
	keys *remote.Ring

	// book is the saved list of machines, and serverCommands are the ids
	// registered for what is in it, so one list can be taken away when
	// the next is put up.
	book           *remote.Book
	serverCommands []string

	// connecting counts the machines being connected to right now. The
	// panel shows a row for each, so several can be on their way at
	// once; this is only so a test can tell when they have all landed.
	connecting int

	// prepare adjusts every config on its way to being dialled. It is a
	// field so a test can drive the real dialogs and the real server
	// list against a machine whose host key it has pinned; nil in the
	// program, which connects to exactly what was asked for.
	prepare func(remote.Config) remote.Config

	// shot drives a screenshot and then closes the window, for looking
	// at what the drawing code actually produced. Nil in ordinary use.
	shot *shooter

	// onError takes the failures logError reports, so a test can see
	// them. Nil in the program, which logs them.
	onError func(error)

	// panes is every live terminal, with the panel entry that stands for
	// it, so a shell that exits can be found wherever it sits in the
	// tree and taken off the panel with it.
	panes map[*term.Terminal]*conns.Entry

	// exits carries "a shell has gone" from the goroutines reading them
	// to the drawing goroutine, which is the only one that may touch the
	// widget tree.
	exits chan struct{}

	// newSession starts the shell a new pane runs. It is a field so a
	// test can drive the tree without spawning anything.
	newSession func(cols, rows int) (session.Session, error)

	// What a new pane is started with, kept from the flags.
	scrollback int
	colours    vt.Palette
	clip       clipboardWriter

	// fontSize is the current size in points.
	fontSize float64

	// fontFamily names the installed family in use, empty while the
	// typeface compiled into the binary is. What that typeface is comes
	// from bundledFonts, not from a field: a window handed its starting
	// font would offer that as "the bundled font" for ever after.
	fontFamily string

	// families carries the system's monospace fonts from the goroutine
	// that scanned for them, and installed is what it found.
	families  chan scanned
	installed []glyph.Family

	// lastPixels is the window size in device pixels, kept so a font
	// size change can re-derive the grid size from it.
	lastPixels [2]int
	lastSize   [2]int

	// title is what the window is currently called, so it is only set
	// again when it changes.
	title string

	// quit is set once the shell is gone; the window closes on the next
	// frame.
	quit atomic.Bool

	// drewFinal records that the frame after the shell exited has been
	// drawn. ebiten returns from Update before Draw, so terminating the
	// moment the shell goes would discard its last output.
	drewFinal bool
}

func (a *app) Update() error {
	if a.quit.Load() {
		// Drained once on the way out: a connection that finished in
		// this very frame is holding a shell that only this queue knows
		// how to close.
		a.pump.run()
		// Give Draw one more frame to paint what the shell wrote last.
		if a.drewFinal {
			return ebiten.Termination
		}
		a.drewFinal = true
		return nil
	}

	// Before anything else: a connection that has finished is waiting to
	// put a terminal on screen, and a dialog it needs is waiting to open.
	a.pump.run()
	a.reapExited()
	a.reapFontScan()
	// Worked out afresh every frame, which is what makes a connection
	// fall from active to settled with no timer anywhere. A row whose
	// text has not changed is written with the same value, so an idle
	// panel leaves its layer alone.
	a.refreshJobs()
	a.refreshPanel(time.Now())
	if a.shot != nil {
		a.shot.update(a)
	}

	for _, ev := range a.reader.Poll() {
		if _, err := a.root.HandleKey(ev); err != nil {
			log.Printf("key %s: %v", ui.ChordOf(ev), err)
		}
	}
	cw, ch := a.renderer.CellSize()
	for _, ev := range a.mouse.Poll(cw, ch) {
		if _, err := a.root.HandleMouse(ev); err != nil {
			log.Printf("mouse: %v", err)
		}
	}

	a.updateTitle()
	return nil
}

// updateTitle shows the focused pane's title. Read rather than pushed:
// a build running in a pane you are not looking at should not rename the
// window.
func (a *app) updateTitle() {
	title := ""
	if t := a.focusedTerminal(); t != nil {
		title = t.Title()
	}
	if title == a.title {
		return
	}
	a.title = title
	if title == "" {
		ebiten.SetWindowTitle("gridterm")
		return
	}
	ebiten.SetWindowTitle("gridterm — " + title)
}

func (a *app) Draw(screen *ebiten.Image) {
	a.root.Draw(a.g.View())
	a.drawModals()
	a.comp.Draw(screen)

	// After the frame is composited, so a picture is what the window
	// shows rather than what it was about to show.
	if a.shot != nil {
		if err := a.shot.captured(screen); err != nil {
			a.logError(err)
		}
	}
}

// Layout satisfies ebiten.Game; LayoutF below takes precedence when the
// runtime supports it.
func (a *app) Layout(w, h int) (int, int) {
	a.resizeTo(w, h)
	return w, h
}

func (a *app) LayoutF(logicalW, logicalH float64) (float64, float64) {
	s := ebiten.Monitor().DeviceScaleFactor()
	a.resizeTo(int(logicalW*s), int(logicalH*s))
	return logicalW * s, logicalH * s
}

// resizeTo tells the grid and the widget tree about a new window size.
func (a *app) resizeTo(pxW, pxH int) {
	a.lastPixels = [2]int{pxW, pxH}
	cols, rows := a.renderer.GridSizeFor(pxW, pxH)
	a.setGridSize(cols, rows)
}

// setGridSize tells the grids and the widget tree about a new size in
// cells. Every layer is resized, not just the tree's: a dialog on its
// own layer has to follow the window or it is measured for the old one.
func (a *app) setGridSize(cols, rows int) {
	if a.lastSize == [2]int{cols, rows} {
		return
	}
	a.lastSize = [2]int{cols, rows}

	a.g.Resize(cols, rows)
	a.resizeModals(cols, rows)
	a.root.Layout(ui.Rect{Cols: cols, Rows: rows})
	a.markDirty()
}

// setFontSize rebuilds the atlas and re-derives the grid size, because
// changing the font changes how many cells fit in the window.
func (a *app) setFontSize(pt float64) error {
	pt = min(max(pt, minFontSize), maxFontSize)
	if pt == a.fontSize {
		return nil
	}
	if err := a.atlas.SetSize(pt); err != nil {
		return err
	}
	a.fontSize = pt
	// The cell box changed, so the cached size is meaningless and every
	// glyph quad has to be re-measured.
	a.lastSize = [2]int{0, 0}
	a.resizeTo(a.lastPixels[0], a.lastPixels[1])
	a.g.MarkAllDirty()
	return nil
}

// logError reports a failure that could not be returned: the goroutines
// moving bytes, and the compositor, have nowhere to hand one back to.
//
// onError is a field so a test can see what was reported. A nil one logs.
func (a *app) logError(err error) {
	if a.onError != nil {
		a.onError(err)
		return
	}
	log.Print(err)
}

// onFocused wraps a command that acts on the focused pane, doing nothing
// when the focus is somewhere that is not a terminal.
func (a *app) onFocused(fn func(*term.Terminal) error) func() error {
	return func() error {
		t := a.focusedTerminal()
		if t == nil {
			return nil
		}
		return fn(t)
	}
}

// reporting wraps a command so a failure reaches the user.
//
// A command is something the user asked for. One that returns an error
// nobody shows does nothing at all as far as they can tell, and a window
// opened from an icon has no console to find the reason in.
//
// Safe to open a dialog from: both the menu and the palette close
// themselves before running a command, so nothing is left above this on
// the stack to take it away again.
func (a *app) reporting(cmd ui.Command) ui.Command {
	run := cmd.Run
	cmd.Run = func() error {
		if err := run(); err != nil {
			a.reportError(cmd.Title, err)
		}
		return nil
	}
	return cmd
}

// reportingAll wraps every command in a list.
func (a *app) reportingAll(cmds []ui.Command) []ui.Command {
	for i, cmd := range cmds {
		cmds[i] = a.reporting(cmd)
	}
	return cmds
}

// commands registers everything the window can do and binds the default
// keys to it. Accelerators are the ones the terminal must not swallow.
func (a *app) commands() {
	cmds := ui.NewCommands()
	cmds.MustRegister(a.reportingAll([]ui.Command{
		ui.Command{ID: "font.increase", Title: "Increase font size", Run: func() error {
			return a.setFontSize(a.fontSize + fontStep)
		}},
		ui.Command{ID: "font.decrease", Title: "Decrease font size", Run: func() error {
			return a.setFontSize(a.fontSize - fontStep)
		}},
		ui.Command{ID: "font.reset", Title: "Reset font size", Run: func() error {
			return a.setFontSize(defaultFontSize)
		}},
		ui.Command{ID: "edit.copy", Title: "Copy", Run: a.onFocused(
			func(t *term.Terminal) error { t.Copy(); return nil })},
		ui.Command{ID: "edit.paste", Title: "Paste", Run: a.onFocused(
			func(t *term.Terminal) error { t.PasteClipboard(); return nil })},
		ui.Command{ID: "view.scrollUp", Title: "Scroll back", Run: a.onFocused(
			func(t *term.Terminal) error { t.ScrollPages(1); return nil })},
		ui.Command{ID: "view.scrollDown", Title: "Scroll forward", Run: a.onFocused(
			func(t *term.Terminal) error { t.ScrollPages(-1); return nil })},
		ui.Command{ID: "pane.splitRight", Title: "Split right", Run: func() error {
			return a.splitFocused(ui.Columns)
		}},
		ui.Command{ID: "pane.splitDown", Title: "Split down", Run: func() error {
			return a.splitFocused(ui.Rows)
		}},
		ui.Command{ID: "pane.close", Title: "Close pane", Run: a.closeFocused},
		ui.Command{ID: "tab.open", Title: "New tab", Run: a.openTab},
		ui.Command{ID: "server.connect", Title: "Connect to a server", Run: a.openServer},
		ui.Command{ID: "server.add", Title: "Add a server", Run: a.openAddServer},
		ui.Command{ID: "server.reload", Title: "Reread the server list", Run: a.reloadBook},
		ui.Command{ID: "panel.toggle", Title: "Show or hide the connections",
			Run: a.togglePanel},
		ui.Command{ID: "panel.focus", Title: "Go to the connections", Run: a.focusPanel},
		ui.Command{ID: "conn.terminal", Title: "Open another terminal here",
			Run: a.openTerminalHere},
		ui.Command{ID: "conn.command", Title: "Run a command…", Run: a.openCommandHere},
		ui.Command{ID: "conn.tunnel", Title: "Open a tunnel…", Run: a.openTunnelHere},
		ui.Command{ID: "conn.socks", Title: "Open a SOCKS proxy…", Run: a.openSocksHere},
		ui.Command{ID: "conn.files", Title: "Browse files here", Run: a.openFilesHere},
		ui.Command{ID: "conn.close", Title: "Close this connection",
			Run: a.closeSelectedConnection},
		ui.Command{ID: "conn.clearFinished", Title: "Clear finished connections",
			Run: a.clearFinished},
		ui.Command{ID: "keys.lock", Title: "Forget unlocked keys", Run: a.lockKeys},
		ui.Command{ID: "palette.open", Title: "Show all commands", Run: a.openPalette},
		ui.Command{ID: "menu.open", Title: "Show the menu bar", Run: a.openMenu},
		ui.Command{ID: "tab.next", Title: "Next tab", Run: func() error {
			return a.focusTab(1)
		}},
		ui.Command{ID: "tab.previous", Title: "Previous tab", Run: func() error {
			return a.focusTab(-1)
		}},
		ui.Command{ID: "pane.next", Title: "Next pane", Run: func() error {
			return a.focusPane(1)
		}},
		ui.Command{ID: "pane.previous", Title: "Previous pane", Run: func() error {
			return a.focusPane(-1)
		}},
	})...)

	// Ctrl+Shift is the usual escape hatch: Ctrl+C has to stay available
	// to the program, so copy cannot live there.
	keys := ui.NewKeymap()
	keys.MustBind(map[ui.Chord]string{
		{Key: input.KeyC, Mods: input.ModCtrl | input.ModShift}: "edit.copy",
		{Key: input.KeyV, Mods: input.ModCtrl | input.ModShift}: "edit.paste",
		{Key: input.KeyEquals, Mods: input.ModCtrl}:             "font.increase",
		// Ctrl+plus is Ctrl+Shift+= on a US layout, and the shift shows
		// up in the modifiers, so the obvious way to ask for a bigger
		// font needs its own binding.
		{Key: input.KeyEquals, Mods: input.ModCtrl | input.ModShift}: "font.increase",
		{Key: input.KeyMinus, Mods: input.ModCtrl}:                   "font.decrease",
		{Key: input.Key0, Mods: input.ModCtrl}:                       "font.reset",
		{Key: input.KeyPageUp, Mods: input.ModShift}:                 "view.scrollUp",
		{Key: input.KeyPageDown, Mods: input.ModShift}:               "view.scrollDown",
		{Key: input.KeyD, Mods: input.ModCtrl | input.ModShift}:      "pane.splitRight",
		{Key: input.KeyE, Mods: input.ModCtrl | input.ModShift}:      "pane.splitDown",
		{Key: input.KeyW, Mods: input.ModCtrl | input.ModShift}:      "pane.close",
		{Key: input.KeyTab, Mods: input.ModCtrl}:                     "pane.next",
		{Key: input.KeyTab, Mods: input.ModCtrl | input.ModShift}:    "pane.previous",
		{Key: input.KeyT, Mods: input.ModCtrl | input.ModShift}:      "tab.open",
		{Key: input.KeyN, Mods: input.ModCtrl | input.ModShift}:      "server.connect",
		{Key: input.KeyB, Mods: input.ModCtrl | input.ModShift}:      "panel.toggle",
		{Key: input.KeyL, Mods: input.ModCtrl | input.ModShift}:      "panel.focus",
		{Key: input.KeyPageDown, Mods: input.ModCtrl}:                "tab.next",
		{Key: input.KeyPageUp, Mods: input.ModCtrl}:                  "tab.previous",
		{Key: input.KeyK, Mods: input.ModCtrl}:                       "palette.open",
		{Key: input.KeyF10}:                                          "menu.open",
	})

	a.root.Commands = cmds
	// These are accelerators rather than ordinary bindings because the
	// terminal has a meaning for every key and would swallow them.
	a.root.Accelerators = keys
}
