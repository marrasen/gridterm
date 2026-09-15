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

	// serving is the listener letting another window take this one over,
	// and the windows that have.
	serving *serving

	// windows are the other machines' gridterms this one has taken
	// over, by the address each was reached at.
	windows map[string]*taken

	// knownWindowsAt is where the keys of the windows reached are
	// recorded, empty in the program and set by a test to a file of its
	// own: the real one belongs to whoever is running gridterm.
	knownWindowsAt string

	// paneOnWindow says which taken-over window a pane is drawn from,
	// so letting go of one takes its panes with it.
	paneOnWindow map[*term.Terminal]*taken

	// watching says what each of those panes is watching over there, so
	// choosing the same thing again brings the pane forward rather than
	// opening a second one onto one shell.
	watching map[*term.Terminal]remoteKey

	// agents are the panes handed to agents, and the listener that lets
	// those agents in.
	agents *agents

	// reachPatience is how long a window being taken over has to get
	// through the handshake. Zero asks the serve package for its own;
	// a test asks for less so it does not wait out the real one.
	reachPatience time.Duration

	// stats says how long the window is taking, for somebody looking at
	// a slow one. Nil unless it was asked for.
	stats *watchStats

	// kept are the panes that stay when what was in them ends, rather
	// than going the way a shell that exited does.
	//
	// A connection that could not be made is the case: the pane is the
	// only account of what happened, and taking it away the moment it
	// finished writing that account would leave the user with nothing
	// again.
	kept map[*term.Terminal]bool

	// dock holds the sidebar beside everything else, panel is the list
	// in it, and stage is what fills the rest: it holds every pane the
	// window has open and shows the one the sidebar picked.
	dock  *ui.Dock
	panel *ui.List
	side  *sidebar
	stage *ui.Tabs

	// hostMenus is the machine a menu opened from the sidebar is about.
	hostMenus hostMenus

	// registry is everything the window has open, which is what the
	// panel draws.
	registry *conns.Registry

	// machines are the connections the window is holding, by the name
	// the panel calls each one. A second terminal on a machine rides on
	// the connection already here rather than logging in again.
	machines map[string]*machine

	// savedWindows names the servers saved as gridterm windows rather
	// than machines to log in to. Kept as a set because the panel asks
	// about every row of every frame, and reading it off the book each
	// time would clone a machine and its key files for one flag.
	savedWindows map[string]bool

	// opening names every machine being connected to right now, so two
	// connections to one machine cannot be made at once: the window
	// would hold the second and close neither. Every name of a route
	// points at the same dialling.
	//
	// A connection still being made is the one most likely to be given
	// up on, because it is the one that is taking too long.
	opening map[string]*dialling

	// tunnels are the forwards the window is holding, by the panel row
	// that stands for each. They ride on a connection, so closing that
	// closes them; this is what takes their rows away with it.
	tunnels map[*conns.Entry]*tunnel

	// shown is the sidebar row for whatever the stage last had in front.
	// The bar follows it when it changes, and is left alone in between.
	shown *conns.Entry

	// files is the window's file manager, or nil when there is none.
	// There is one of it: a pane is added to the manager rather than a
	// second manager being opened beside it.
	files *browser

	// closes are the filesystems being let go of on goroutines of their
	// own, and what those attempts reported.
	closes closer

	// queue is the file work running in the background, and jobs are the
	// panel rows that stand for each piece of it.
	queue *jobs.Queue
	jobs  map[*conns.Entry]*jobs.Job

	// asking is how to take away a question a job is waiting on, by the
	// channel the answer goes back through. A job given up on while its
	// question is up has to take the question with it.
	asking map[chan jobs.Choice]func()

	// ended are the panes whose program has stopped and which are being
	// kept only so the user can read what it printed.
	ended map[*term.Terminal]bool

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

	// serverHosts is the list the registered commands were built from,
	// so a rebuild that would change nothing is skipped. Rebuilding
	// closes whatever menu is open, and this runs whenever a connection
	// is made or lost.
	serverHosts []string

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

	// lastPad is how much room the window's padding was last given, in
	// quarters of a cell, so a change to it can re-derive the grid size.
	lastPad [2]int

	// sideRegion is the sidebar, drawn on a grid of its own so its rows
	// can have room around them without moving the terminal's.
	sideRegion *region

	// sideGeo is where that grid lands in pixels, for routing a click
	// by the sidebar's rows rather than the window's.
	sideGeo render.Geometry

	// geo is where the window's grid lands in pixels, for routing a
	// click and for placing the glass behind a dialog. Kept apart from
	// the one the renderer draws with.
	geo render.Geometry

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
	for _, err := range a.closes.reported() {
		a.reportError("Could not let go of a filesystem", err)
	}
	a.refreshPanel(time.Now())
	if a.shot != nil {
		a.shot.update(a)
	}

	// Shown rather than logged. A key or a click is something the user
	// asked for, and the sidebar is now the way into most of it: a
	// window opened from an icon has no console to find the reason in.
	for _, ev := range a.reader.Poll() {
		if _, err := a.root.HandleKey(ev); err != nil {
			a.reportError(ui.ChordOf(ev).String()+" could not be done", err)
		}
	}
	// Routed by the measurements the window was last laid out with,
	// which is what the user was looking at when they clicked. They are
	// taken together, in placeRegions: a window measured afresh here
	// and a region measured a frame ago would not agree on where the
	// sidebar's rows are.
	for _, ev := range a.mouse.Poll(a.cellAt) {
		if _, err := a.root.HandleMouse(ev); err != nil {
			a.reportError("That could not be done", err)
		}
	}

	// After the input, so a key or a click that opens or closes the
	// sidebar is drawn this frame rather than the next one. Before it,
	// the frame would be laid out for padding the window no longer has.
	a.applyPads()
	a.placeRegions()

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
	started := time.Now()
	a.root.Draw(a.g.View())
	// After the tree, because the tree is what gave the region its size
	// this frame. Its own grid, so the window's rows are not its rows.
	if a.sideRegion != nil {
		a.sideRegion.draw()
	}
	a.drawModals()
	a.comp.Draw(screen)
	if a.stats != nil {
		a.stats.frame(time.Since(started), a.comp.Stats(), a.bytesRead())
	}

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
//
// The window's own padding comes off the top first: it is space the
// grid gains rather than space a cell gives up, so the cells have to be
// counted in what is left.
func (a *app) resizeTo(pxW, pxH int) {
	a.lastPixels = [2]int{pxW, pxH}
	a.renderer.SetWindow(pxW, pxH)
	padX, padY := a.padsWanted()
	a.lastPad = [2]int{padX, padY}
	cols, rows := a.renderer.GridSizeWithin(pxW, pxH, padX, padY)
	a.setGridSize(cols, rows)
	// Measured here as well as at the end of the frame, so the window
	// is never routing clicks by measurements it has never taken: this
	// runs before the first frame, and again before the input of any
	// frame the window was resized in.
	a.placeRegions()
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
	a.padGrids()
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
		ui.Command{ID: copyCommand, Title: "Copy", Run: a.onFocused(
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
		ui.Command{ID: "pane.unsplit", Title: "Take this pane out of its split",
			Run: a.unsplitFocused},
		ui.Command{ID: "pane.close", Title: "Close pane", Run: a.closeFocused},
		ui.Command{ID: "tab.open", Title: "New tab", Run: a.openTab},
		ui.Command{ID: "server.connect", Title: "Connect to a server", Run: a.openServer},
		ui.Command{ID: "server.add", Title: "Add a server", Run: a.openAddServer},
		ui.Command{ID: "server.reload", Title: "Reread the server list", Run: a.reloadBook},
		ui.Command{ID: "serve.window", Title: "Serve this window…", Run: a.openServing},
		ui.Command{ID: "serve.takeOver", Title: "Take over a window…", Run: a.openTakeOver},
		ui.Command{ID: "agent.hand", Title: "Let an agent work in this pane…",
			Run: a.handHere},
		ui.Command{ID: "agent.take", Title: "Take this pane back from the agent",
			Run: a.takeBackHere},
		ui.Command{ID: "panel.toggle", Title: "Show or hide the connections",
			Run: a.togglePanel},
		ui.Command{ID: "panel.focus", Title: "Go to the connections", Run: a.focusPanel},
		ui.Command{ID: "conn.terminal", Title: "Open a terminal here",
			Run: a.openTerminalHere},
		ui.Command{ID: "conn.command", Title: "Run a command…", Run: a.openCommandHere},
		ui.Command{ID: "conn.tunnel", Title: "Open a tunnel…", Run: a.openTunnelHere},
		ui.Command{ID: "conn.socks", Title: "Open a SOCKS proxy…", Run: a.openSocksHere},
		ui.Command{ID: "conn.files", Title: "Browse files here", Run: a.openFilesHere},
		ui.Command{ID: "files.goTo", Title: "Go to a directory…", Run: a.openGoTo},
		ui.Command{ID: "conn.disconnect", Title: "Close the connection to this machine",
			Run: a.disconnectHere},
		ui.Command{ID: "server.editThis", Title: "Edit this server…",
			Run: a.editThisServer},
		ui.Command{ID: "server.forget", Title: "Forget this server…",
			Run: a.forgetThisServer},
		ui.Command{ID: "conn.close", Title: "Close this connection",
			Run: a.closeSelectedConnection},
		ui.Command{ID: "conn.clearFinished", Title: "Clear finished connections",
			Run: a.clearFinished},
		ui.Command{ID: "keys.lock", Title: "Forget unlocked keys and try the agent again",
			Run: a.lockKeys},
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
		{Key: input.KeyC, Mods: input.ModCtrl | input.ModShift}: copyCommand,
		{Key: input.KeyV, Mods: input.ModCtrl | input.ModShift}: "edit.paste",
		// The X11 spelling of paste, which plenty of people have in
		// their fingers and no terminal has a meaning for.
		{Key: input.KeyInsert, Mods: input.ModShift}: "edit.paste",
		{Key: input.KeyInsert, Mods: input.ModCtrl}:  copyCommand,
		{Key: input.KeyEquals, Mods: input.ModCtrl}:  "font.increase",
		// Ctrl+plus is Ctrl+Shift+= on a US layout, and the shift shows
		// up in the modifiers, so the obvious way to ask for a bigger
		// font needs its own binding.
		{Key: input.KeyEquals, Mods: input.ModCtrl | input.ModShift}: "font.increase",
		{Key: input.KeyMinus, Mods: input.ModCtrl}:                   "font.decrease",
		{Key: input.Key0, Mods: input.ModCtrl}:                       "font.reset",
		{Key: input.KeyPageUp, Mods: input.ModShift}:                 "view.scrollUp",
		{Key: input.KeyPageDown, Mods: input.ModShift}:               "view.scrollDown",
		{Key: input.KeyD, Mods: input.ModCtrl | input.ModShift}:      "pane.splitRight",
		{Key: input.KeyU, Mods: input.ModCtrl | input.ModShift}:      "pane.unsplit",
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
		{Key: input.KeyG, Mods: input.ModCtrl | input.ModShift}:      "files.goTo",
		{Key: input.KeyK, Mods: input.ModCtrl}:                       "palette.open",
		{Key: input.KeyF10}:                                          "menu.open",
	})

	a.root.Commands = cmds
	// These are accelerators rather than ordinary bindings because the
	// terminal has a meaning for every key and would swallow them.
	a.root.Accelerators = keys
}
