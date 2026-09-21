package main

import (
	"context"
	"image"
	"log"
	"slices"
	"strings"
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
	"github.com/marrasen/gridterm/notify"
	"github.com/marrasen/gridterm/remote"
	"github.com/marrasen/gridterm/render"
	"github.com/marrasen/gridterm/secrets"
	"github.com/marrasen/gridterm/session"
	"github.com/marrasen/gridterm/themes"
	"github.com/marrasen/gridterm/ui"
	"github.com/marrasen/gridterm/ui/files"
	"github.com/marrasen/gridterm/ui/term"
	"github.com/marrasen/gridterm/vfs"
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

	// g is the layer the widget tree draws on. Splits and decks divide it
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

	// bar is the row of menu titles at the top of the window, and
	// statusWas what its status last said.
	bar       *ui.Menubar
	statusWas statusKey

	// serving is the listener letting another window take this one over,
	// and the windows that have.
	serving *serving

	// windows are the other machines' gridterms this one has taken
	// over, and the panes drawn from them.
	windows *windows

	// agents are the panes handed to agents, and the listener that lets
	// those agents in.
	agents *agents

	// shellPick is the shells a pane on this machine can run, and which
	// one the user last chose.
	shellPick *shellPick

	// stats says how long the window is taking, for somebody looking at
	// a slow one. Nil unless it was asked for.
	stats *watchStats

	// dock holds the sidebar beside everything else, panel is the list
	// in it, and stage is what fills the rest: it holds every pane the
	// window has open and shows the one the sidebar picked.
	dock  *ui.Dock
	panel *ui.List
	side  *sidebar
	stage *ui.Deck

	// hostMenus is the machine a menu opened from the sidebar is about.
	hostMenus hostMenus

	// registry is everything the window has open, which is what the
	// panel draws.
	registry *conns.Registry

	// machines are the connections the window is holding, the ones it is
	// making, and the panes running on them.
	machines *machines

	// tunnels are the forwards the window is holding, by the panel row
	// that stands for each. They ride on a connection, so closing that
	// closes them; this is what takes their rows away with it.
	tunnels map[*conns.Entry]*tunnel

	// saved are the commands the user asked to keep, offered by the
	// dialog that runs one.
	saved *savedCommands

	// savedTuns are the tunnels the user asked to keep.
	savedTuns *savedTunnels

	// copies are the file copies the user asked to keep.
	copies *savedCopies

	// sidePads and barPads are where a grid's padding is gathered before
	// it is written.
	sidePads, barPads padTable

	// paneTitles is whether each pane shows a line naming it.
	paneTitles *paneTitles
	shellSetup *shellSetup

	// called is what the window calls itself to the programs it runs.
	called *termProgram

	// far is what a machine at the far end said about the paths its
	// panes printed.
	far *pathsFar

	// tunnelPanes are the panes showing what a tunnel is doing, by the
	// row of the tunnel each one is on.
	tunnelPanes map[*conns.Entry]*term.Terminal

	// noticed is the last message read off each pane, so one message is
	// logged once rather than on every frame.
	noticed map[*term.Terminal]uint64

	// toasts shows a message outside the window, and lastToast is when
	// it last did: a program sending them one after another gets one
	// pop-up rather than a screenful.
	toasts    notify.Toaster
	lastToast time.Time

	// wrote is the note the panel last put on each row, so the next
	// frame can tell its own note from one something else put there.
	wrote map[*conns.Entry]string

	// keyFiles are the key files the user keeps, offered when a
	// connection is made or edited.
	keyFiles *keyIndex

	// recent are the panes in the order they last had focus, newest
	// first, and walk is the walk back through them while Ctrl is held.
	recent []ui.Widget
	walk   *paneWalk

	// modsNow reads the modifier keys held, so a test can say which.
	modsNow func() input.Mods

	// overlay is the list of panes drawn while a walk is on.
	overlay *walkOverlay

	// typed is what an agent has typed in each pane, so the user who
	// handed a pane over can go back over what was done in it.
	typed map[*term.Terminal]*typedLog

	// switcher is every pane drawn small at once, and nil when it is not
	// showing.
	switcher *switcher

	// theme is the colour theme the window is drawn in.
	theme *themePick

	// keysDir is the directory the keyboard shortcuts file lives in.
	// Empty means the one gridterm keeps its files in.
	keysDir string

	// shared is the glowing border over each pane somebody else is in,
	// one layer per pane.
	shared map[*term.Terminal]*sharedMark

	// now is the clock this window runs on, so a test can hold it still.
	now func() time.Time

	// status is the line along the bottom saying that something worked,
	// and nothing while there is nothing to say.
	status status

	// frameAt is when the frame being built began. Everything that glows
	// reads it rather than the clock, so the border and the sidebar row
	// cannot land either side of a step.
	frameAt time.Time

	// pointerGone says the pointer is not on this window, so nothing is
	// under it.
	pointerGone bool

	// The sidebar is built again every frame, because most of what a row
	// says moves on its own: a rate, a job's progress, how long ago
	// something settled. So it is built in these rather than in
	// something new each time. They hold last frame's answer until the
	// next one overwrites it, and nothing outside refreshPanel reads
	// them.
	openRows map[string][]conns.Row
	hostList []string
	hostSeen map[string]bool
	restList []string
	farBy    map[*conns.Entry]remoteHostKey
	rowBuf   []ui.ListRow
	liveRows map[*conns.Entry]bool

	// readers are the file readers open in the window, each with the row
	// it has on the sidebar.
	// sending counts the pictures on their way to another machine, and
	// sendingTo is where the one of them is going. Both belong to the
	// goroutine that draws, which is where the bar is built.
	sending   int
	sendingTo string

	readers map[*files.Reader]*reader

	// readerPics is the picture each reader showing one has on screen,
	// on a layer of its own over the pane.
	readerPics map[*files.Reader]*readerPic

	// termPics are the layers for the inline pictures in each pane's
	// output, one per picture on screen.
	termPics map[*term.Terminal][]*termPic

	// fsHeld counts the readers using each filesystem, and fsGone marks
	// the ones the browser has finished with. A filesystem is closed
	// when both say nobody is left: the browser opens them, and a reader
	// opened from one of its panes goes on reading through it.
	fsHeld map[vfs.FS]int
	fsGone map[vfs.FS]bool

	// paneRows are the rows that stand for a pane of this window, worked
	// out once a frame.
	paneRows map[*conns.Entry]*term.Terminal

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

	// ended are the panes whose program has stopped and which are kept so
	// the user can read what it printed and answer the question on the
	// last row.
	ended map[*term.Terminal]bool

	// started is how each pane's program was started and the session it
	// is reading, so a pane whose program has ended can start it again
	// in place.
	started map[*term.Terminal]*startedAs

	// home is the machine -ssh named, which new panes open on while it
	// is connected. Empty when the window opens them on this machine.
	home string

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

	// secrets is the vault one of those keys opens, read off disk the
	// first time anything asks for it. Locking the keys locks it.
	// secretsAt overrides where it is kept, for a test.
	secrets   *secrets.Vault
	secretsAt string

	// book is the saved list of machines, and serverCommands are the ids
	// registered for what is in it, so one list can be taken away when
	// the next is put up.
	book           *remote.Book
	serverCommands []string

	// builtFor is what the registered commands were built from: every
	// machine the window knows, each marked when a terminal on it means
	// taking a window over, and then the saved names.
	//
	// refreshServers writes it, and only when it rebuilds, so a refresh
	// whose lines match it leaves the commands alone. Rebuilding takes
	// down whatever menu is open, and this runs whenever a connection
	// is made or lost.
	builtFor []string

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

	// panes is every terminal the window holds, with the panel entry
	// that stands for it, so a pane can be found wherever it sits in the
	// tree. A pane stays here after its program has gone, until the user
	// closes it.
	panes map[*term.Terminal]*conns.Entry

	// exits carries "a shell has gone" from the goroutines reading them
	// to the drawing goroutine, which is the only one that may touch the
	// widget tree.
	exits chan struct{}

	// newShell starts a shell on this machine, on the argv given, in dir
	// when dir is not empty. It is a field so a test can drive the tree
	// without spawning anything.
	newShell func(argv []string, dir string, cols, rows int) (session.Session, error)

	// command is what -e named: the program a pane here runs instead of
	// a shell. It is written once in main, before there is a goroutine
	// other than the one writing it, and only read after that.
	command []string

	// What a new pane is started with, kept from the flags.
	scrollback int
	colours    vt.Palette

	// look is the window's furniture as the theme wrote it down. The
	// zero Look is a theme that said nothing, and every colour is
	// derived from the two ends of the theme as it always was.
	look themes.Look
	clip clipboardWriter

	// hasClipText says whether there is text to paste. A nil one asks
	// the system, and a test sets its own.
	hasClipText func() bool

	// readClipImage reads a picture off the clipboard. A nil one reads
	// the system's own, and a test sets its own so a run does not depend
	// on what happens to be on the clipboard of whoever started it.
	readClipImage func() (image.Image, bool, error)

	// readClip reads the clipboard. A nil one reads the system's own,
	// and a test sets its own: a test run must not reach into the
	// clipboard of whoever is running it.
	readClip func() (string, error)

	// fontSize is the current size in points, and font is where that is
	// kept between runs.
	fontSize float64
	font     *fontPick

	// wantFont is the family the theme asked for, kept because the theme
	// is taken before the scan of the system's fonts has finished.
	wantFont string

	// fontFixed says a typeface was named on the command line. A theme
	// names one as a wish, and a wish does not overrule an instruction.
	fontFixed bool

	// fontPicked says the user chose the typeface from the Font menu
	// while this theme was on, so taking the same theme again does not
	// drag them off it. Taking a different theme clears it.
	fontPicked bool

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

	// scaled are the panes whose screen is bigger than the room the
	// layout has for them, each drawn on a grid and a layer of its own
	// and blitted to fit.
	scaled map[*term.Terminal]*scaledPane

	// scaledHeld keeps the pointer for a pane drawn scaled, the way Root
	// keeps it for a widget in the tree: a drag that wandered off the
	// pane still belongs to it.
	scaledHeld ui.MouseCapture

	// pointer is the pixel the mouse was last read at, for routing a
	// click by something other than the window's cells.
	pointer [2]int

	// pointerShape is the shape the mouse pointer was last set to, so it
	// is only set again when it changes.
	pointerShape ui.Cursor

	// geo is where the window's grid lands in pixels, for routing a
	// click and for placing the glass behind a dialog. Kept apart from
	// the one the renderer draws with.
	geo render.Geometry

	// title is what the window is currently called, so it is only set
	// again when it changes.
	title string

	// quit is set once the last pane has been closed; the window closes
	// on the next frame.
	quit atomic.Bool

	// full says the window is filling the screen with nothing around
	// the panes, and sideWas whether the sidebar was open before it
	// did, so coming out puts back what was there.
	full    bool
	sideWas bool

	// leaving says the question about closing is up, so the close
	// button held down does not stack a dialog a frame.
	leaving bool

	// drewFinal records that the frame after quit was set has been
	// drawn. ebiten returns from Update before Draw, so terminating the
	// moment the last pane goes would discard its last output.
	drewFinal bool
}

func (a *app) Update() error {
	// Before anything: the operating system asking the window to close
	// is a question here rather than an order, so a window holding a
	// half-finished copy says so.
	a.watchForClosing()
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
	a.reapShellScan()
	a.reapFontTrouble()
	// Worked out afresh every frame, which is what makes a connection
	// fall from active to settled with no timer anywhere. A row whose
	// text has not changed is written with the same value, so an idle
	// panel leaves its layer alone.
	a.refreshJobs()
	a.reportClosed()
	a.frameAt = a.clock()
	// Before the panel, so a file that grew this frame has its row say
	// how long it is now rather than how long it was.
	a.followReaders(a.frameAt)
	a.refreshPanel(a.frameAt)
	if a.shot != nil {
		a.shot.update(a)
	}

	// Shown rather than logged. A key or a click is something the user
	// asked for, and the sidebar is now the way into most of it: a
	// window opened from an icon has no console to find the reason in.
	a.handleKeys(a.reader.Poll())
	// Routed by the measurements the window was last laid out with,
	// which is what the user was looking at when they clicked. They are
	// taken together, in placeRegions: a window measured afresh here
	// and a region measured a frame ago would not agree on where the
	// sidebar's rows are.
	a.handleMouse(a.mouse.Poll(a.cellAt))

	// After the input, so a key or a click that opens or closes the
	// sidebar is drawn this frame rather than the next one. Before it,
	// the frame would be laid out for padding the window no longer has.
	a.stepWalk()
	a.stepSwitcher()
	a.stepStatus()
	a.noteFocus()
	// Before the layout, which is what takes the row off the pane.
	a.refreshCaptions()
	a.applyPads()
	a.placeRegions()
	a.placeScaled()
	a.placeShared()
	a.placeReaderPics()
	a.placeTermPics()
	a.placeWalk()
	a.placeSwitcher()

	// After the layout, so the pointer is the one for the frame about to
	// be drawn rather than the one before it.
	a.updatePointer()
	a.takeDroppedFiles()
	a.updateTitle()
	a.updateStatus()
	return nil
}

// handleKeys gives one frame's keys to the widget tree.
func (a *app) handleKeys(evs []input.Event) {
	for _, ev := range evs {
		if _, err := a.root.HandleKey(ev); err != nil {
			a.reportError(ui.ChordOf(ev).String()+" could not be done", err)
		}
	}
	// A cursor caught in the off half of its blink while somebody is
	// typing reads as a window that has stopped answering, so a key puts
	// it back on and starts its phase again. Letting go of a key is not
	// typing, and neither is holding a modifier down.
	if a.comp != nil && slices.ContainsFunc(evs, typing) {
		a.comp.WakeCursors()
	}
}

// typing reports whether an event is a key going in rather than one
// coming back up.
func typing(ev input.Event) bool {
	switch ev.Kind {
	case input.KeyPress, input.KeyRepeat, input.Text:
		return true
	}
	return false
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
		ebiten.SetWindowTitle(programName)
		return
	}
	ebiten.SetWindowTitle(programName + " — " + title)
}

func (a *app) Draw(screen *ebiten.Image) {
	started := time.Now()
	a.root.Draw(a.g.View())
	// Over the bottom row, while a menu is open and has a line picked
	// out. After the tree so it covers what the tree drew there.
	a.drawHint()
	// After the tree, because the tree is what gave the region its size
	// this frame. Its own grid, so the window's rows are not its rows.
	if a.sideRegion != nil {
		a.sideRegion.draw()
	}
	a.drawScaled()
	a.drawShared(a.frameTime())
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
	a.placeScaled()
	a.placeShared()
	a.placeReaderPics()
	a.placeTermPics()
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
// handleMouse gives a frame's mouse events to whatever they belong to.
//
// Split from the frame so it can be tested: what is left up there is
// the poll, which needs a window.
func (a *app) handleMouse(evs []input.MouseEvent) {
	for _, ev := range evs {
		took, err := a.zoomedFont(ev)
		if err != nil {
			a.reportError("Could not change the font size", err)
			continue
		}
		if took {
			continue
		}
		if _, err := a.routeMouse(ev); err != nil {
			a.reportError("Operation failed", err)
		}
	}
}

// zoomedFont makes the font bigger or smaller for ctrl and the wheel,
// and reports whether it took the event.
//
// Taken before the tree sees it, because a pane scrolls on the wheel
// and one that scrolled as well would do both at once.
func (a *app) zoomedFont(ev input.MouseEvent) (bool, error) {
	if !ev.Button.IsWheel() || !ev.Mods.Has(input.ModCtrl) {
		return false, nil
	}
	step := float64(fontStep)
	if ev.Button == input.MouseWheelDown {
		step = -step
	}
	return true, a.setFontSize(a.fontSize + step)
}

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
	// Written down after the window is drawn at it, so a size that the
	// atlas would not take is never the one remembered.
	return a.font.choose(pt)
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
	title := commandFailed(cmd.Title)
	cmd.Run = func() error {
		if err := run(); err != nil {
			a.reportError(title, err)
		}
		return nil
	}
	return cmd
}

// commandFailed heads the notice a command's failure is shown in.
//
// The command's own title and nothing else, so the heading says which
// of the things on the palette did not work. The ellipsis goes: it says
// the command is about to ask something, which is no longer true by the
// time it has failed.
func commandFailed(title string) string {
	return strings.TrimSuffix(title, "…") + " failed"
}

// reportingAll wraps every command in a list.
func (a *app) reportingAll(cmds []ui.Command) []ui.Command {
	for i, cmd := range cmds {
		cmds[i] = a.reporting(cmd)
	}
	return cmds
}

// scrollUpCommand and scrollDownCommand move a pane through what it
// holds, and scrollCommands is the pair, for a widget that takes those
// chords for itself.
const (
	scrollUpCommand   = "view.scrollUp"
	scrollDownCommand = "view.scrollDown"
)

var scrollCommands = []string{scrollUpCommand, scrollDownCommand}

// commands registers everything the window can do and binds the default
// keys to it. Accelerators are the ones the terminal must not swallow.
func (a *app) commands() {
	cmds := ui.NewCommands()
	cmds.MustRegister(a.reportingAll([]ui.Command{
		ui.Command{ID: "font.increase", Title: "Increase Font Size",
			AlsoFind: []string{"zoom in", "bigger", "larger"}, Run: func() error {
				return a.setFontSize(a.fontSize + fontStep)
			}},
		ui.Command{ID: "font.decrease", Title: "Decrease Font Size",
			AlsoFind: []string{"zoom out", "smaller"}, Run: func() error {
				return a.setFontSize(a.fontSize - fontStep)
			}},
		ui.Command{ID: "font.reset", Title: "Reset Font Size",
			AlsoFind: []string{"zoom", "default", "actual size"}, Run: func() error {
				return a.setFontSize(defaultFontSize)
			}},
		ui.Command{ID: copyCommand, Title: "Copy", Run: a.copySelection},
		ui.Command{ID: "edit.paste", Title: "Paste", Run: a.onFocused(a.paste)},
		ui.Command{ID: "edit.pasteImage", Title: "Paste Image as File",
			AlsoFind: []string{"picture", "screenshot", "path"}, Run: a.onFocused(a.pasteImage)},
		ui.Command{ID: "secrets.open", Title: showSecretsTitle + "…",
			AlsoFind: []string{"secrets", "password", "vault", "note", "credential"},
			Run:      a.openSecrets},
		ui.Command{ID: "secrets.add", Title: addSecretTitle + "…",
			AlsoFind: []string{"password", "vault", "keep", "new"},
			Run:      a.addSecret},
		ui.Command{ID: "secrets.addNote", Title: addNoteTitle + "…",
			AlsoFind: []string{"secret", "vault", "recovery", "licence", "license", "keep"},
			Run:      a.addNote},
		ui.Command{ID: "secrets.change", Title: changeSecretTitle + "…",
			AlsoFind: []string{"password", "vault", "note", "edit", "rename"},
			Run:      a.changeSecret},
		ui.Command{ID: "secrets.addKey", Title: addSecretsKeyTitle + "…",
			AlsoFind: []string{"let another key open the secrets", "vault", "password",
				"machine", "key"},
			Run: a.addVaultKey},
		ui.Command{ID: "secrets.removeKey", Title: removeSecretsKeyTitle + "…",
			AlsoFind: []string{"stop a key opening the secrets", "vault", "password",
				"machine", "key", "revoke"},
			Run: a.removeVaultKey},
		ui.Command{ID: "secrets.forget", Title: removeSecretTitle + "…",
			AlsoFind: []string{"forget", "password", "vault", "note", "delete"},
			Run:      a.forgetSecret},
		ui.Command{ID: scrollbackCommand, Title: scrollbackTitle,
			AlsoFind: []string{"search", "history", "buffer", "save", "view", "open"},
			Run:      a.showScrollback},
		ui.Command{ID: scrollUpCommand, Title: "Scroll Page Up",
			AlsoFind: []string{"back", "scrollback"},
			Run:      func() error { return a.scrollFocused(1) }},
		ui.Command{ID: scrollDownCommand, Title: "Scroll Page Down",
			AlsoFind: []string{"forward", "scrollback"},
			Run:      func() error { return a.scrollFocused(-1) }},
		ui.Command{ID: "pane.splitRight", Title: "Split Right…",
			AlsoFind: []string{"vertical"}, Run: func() error {
				return a.splitFocused(ui.Columns)
			}},
		ui.Command{ID: "pane.splitDown", Title: "Split Down…",
			AlsoFind: []string{"horizontal"}, Run: func() error {
				return a.splitFocused(ui.Rows)
			}},
		ui.Command{ID: "pane.popOut", Title: "Pop Out Pane",
			AlsoFind: []string{"unsplit", "detach", "take out of its split"},
			Run:      a.unsplitFocused},
		ui.Command{ID: "pane.close", Title: "Close Pane", Run: a.closeFocused},
		ui.Command{ID: fullScreenCommand, Title: "Full Screen",
			AlsoFind: []string{"fill", "maximise", "maximize", "hide the sidebar"},
			On:       a.FullScreen, Run: a.toggleFullScreen},
		ui.Command{ID: "app.exit", Title: "Exit",
			AlsoFind: []string{"quit", "close this window"}, Run: func() error {
				a.askToQuit()
				return nil
			}},
		ui.Command{ID: "app.about", Title: "About gridterm",
			AlsoFind: []string{"version"}, Run: a.showAbout},
		ui.Command{ID: "pane.open", Title: "New Terminal",
			AlsoFind: []string{"pane", "shell"}, Run: a.openPane},
		ui.Command{ID: defaultShellCommand, Title: "New Terminal, Default Shell",
			AlsoFind: []string{"pane"},
			Run:      a.openPaneOnDefault},
		ui.Command{ID: "server.connect", Title: "Connect to Server…",
			AlsoFind: []string{"ssh", "host", "machine"}, Run: a.openServer},
		ui.Command{ID: "server.add", Title: "Add Server…",
			AlsoFind: []string{"new", "save"}, Run: a.openAddServer},
		ui.Command{ID: "server.reload", Title: "Reload Server List",
			AlsoFind: []string{"reread"}, Run: a.reloadBook},
		ui.Command{ID: copiesCommand, Title: copiesTitle + "…",
			AlsoFind: []string{"remembered", "file", "again", "repeat"}, Run: a.openCopies},
		ui.Command{ID: "serve.window", Title: "Serve This Window…",
			AlsoFind: []string{"share", "listen", "remote"}, Run: a.openServing},
		ui.Command{ID: "serve.attach", Title: "Connect to Window…", Run: a.openTakeOver,
			AlsoFind: []string{"another", "attach", "take over", "remote", "share panes"}},
		ui.Command{ID: "agent.hand", Title: "Share Pane with Agent…",
			AlsoFind: []string{"hand over", "add this pane to the share"},
			Run:      a.handHere},
		ui.Command{ID: "agent.take", Title: "Stop Sharing Pane",
			AlsoFind: []string{"take this pane out of the share", "remove", "unshare"},
			Run:      a.takeBackHere},
		ui.Command{ID: "agent.share", Title: shareTitle,
			AlsoFind: []string{"show the share", "code", "prompt", "skill", "setup"},
			Run:      a.showShare},
		ui.Command{ID: typedCommand, Title: typedTitle,
			AlsoFind: []string{"what the agent typed", "input", "sent"},
			Run:      a.showTyped},
		ui.Command{ID: "view.theme", Title: themeTitle + "…", Run: a.openThemePick,
			AlsoFind: []string{"colour", "color", "colors", "scheme"}},
		ui.Command{ID: "view.themesReload", Title: "Reload Themes",
			Run:      a.reloadThemes,
			AlsoFind: []string{"colour", "color", "colors", "reread"}},
		ui.Command{ID: "view.themesStart", Title: "New Theme File",
			Run:      a.writeThemeStart,
			AlsoFind: []string{"colour", "color", "colors", "write", "create", "edit"}},
		ui.Command{ID: "pane.titles", Title: "Show Pane Titles",
			AlsoFind: []string{"hide", "toggle", "names", "line"},
			On:       a.paneTitles.on, Run: a.togglePaneTitles},
		ui.Command{ID: "shell.setup", Title: "Shell Setup",
			AlsoFind: []string{"shell integration", "working directory", "osc 7", "on", "off"},
			On:       a.shellSetup.on, Run: a.toggleShellSetup},
		ui.Command{ID: "shell.termProgram", Title: "Terminal Identity…",
			AlsoFind: []string{"term_program", "compatibility", "pictures", "images", "calls itself"},
			Run:      a.openTermProgram},
		ui.Command{ID: "sshkey.make", Title: makeKeyTitle + "…",
			AlsoFind: []string{"make", "create", "generate", "keygen", "ed25519"},
			Run:      a.openMakeKey},
		ui.Command{ID: "sidebar.toggle", Title: "Show Sidebar",
			AlsoFind: []string{"hide", "toggle", "connections", "panel"},
			On:       a.panelShowing, Run: a.togglePanel},
		ui.Command{ID: "sidebar.focus", Title: "Focus Sidebar",
			AlsoFind: []string{"go to the connections", "panel"}, Run: a.focusPanel},
		ui.Command{ID: "conn.terminal", Title: "New Terminal Here",
			AlsoFind: []string{"like this one", "same shell", "same server", "duplicate", "clone"},
			Run:      a.openTerminalHere},
		ui.Command{ID: "conn.command", Title: "Run Command…",
			AlsoFind: []string{"here", "program", "execute"}, Run: a.openCommandHere},
		ui.Command{ID: "conn.tunnel", Title: "Open Tunnel…",
			AlsoFind: []string{"forward", "port", "local", "remote"}, Run: a.openTunnelHere},
		ui.Command{ID: "conn.socks", Title: "Open SOCKS Proxy…",
			AlsoFind: []string{"tunnel", "dynamic"}, Run: a.openSocksHere},
		ui.Command{ID: "conn.files", Title: "Browse Files Here",
			AlsoFind: []string{"file browser", "sftp", "folder", "directory"}, Run: a.openFilesHere},
		ui.Command{ID: "files.goTo", Title: "Go to Directory…",
			AlsoFind: []string{"folder", "path", "cd", "drive"}, Run: a.openGoTo},
		ui.Command{ID: "conn.disconnect", Title: "Disconnect",
			AlsoFind: []string{"close the connection", "machine", "server", "log out"},
			Run:      a.disconnectHere},
		ui.Command{ID: connLogCommand, Title: connLogName,
			AlsoFind: []string{"how this was reached", "route", "hops"},
			Run:      a.showConnLogHere},
		ui.Command{ID: helpCommand, Title: helpTitle,
			AlsoFind: []string{"keys", "keyboard", "help"}, Run: a.showHelp},
		ui.Command{ID: filesCommand, Title: filesTitle,
			AlsoFind: []string{"where gridterm keeps its files", "settings", "config",
				"folder", "portable"}, Run: a.showWhereFiles},
		ui.Command{ID: logCommand, Title: logTitle, Run: a.showLog,
			AlsoFind: []string{"show what the window has logged", "debug", "errors",
				"what went wrong"}},
		ui.Command{ID: keysCommand, Title: keysTitle,
			AlsoFind: []string{"write", "starting", "keyboard", "create"},
			Run:      a.writeShortcutStart},
		ui.Command{ID: keysReloadCommand, Title: keysReloadTitle,
			AlsoFind: []string{"keyboard", "reread"},
			Run:      a.reloadShortcuts},
		ui.Command{ID: "server.editThis", Title: "Edit This Server…",
			Run: a.editThisServer},
		ui.Command{ID: "server.forget", Title: "Remove This Server…",
			AlsoFind: []string{"forget", "delete"},
			Run:      a.forgetThisServer},
		ui.Command{ID: "sidebar.closeRow", Title: "Close Selected Row",
			AlsoFind: []string{"close this connection", "sidebar"},
			Run:      a.closeSelectedConnection},
		ui.Command{ID: "conn.clearFinished", Title: "Clear Finished",
			AlsoFind: []string{"connections", "rows", "sidebar", "ended"},
			Run:      a.clearFinished},
		ui.Command{ID: "sshkey.lock", Title: "Lock SSH Keys",
			AlsoFind: []string{"forget unlocked keys", "passphrase", "agent", "try again"},
			Run:      a.lockKeys},
		ui.Command{ID: "palette.open", Title: "All Commands…",
			AlsoFind: []string{"show", "palette", "search"}, Run: a.openPalette},
		ui.Command{ID: "menu.open", Title: "Focus Menu Bar",
			AlsoFind: []string{"show"}, Run: a.openMenu},
		ui.Command{ID: switcherCommand, Title: switcherTitle + "…", Run: a.openSwitcher,
			AlsoFind: []string{"show every pane", "switcher", "overview", "grid"}},
		ui.Command{ID: "pane.nextInSidebar", Title: "Next Pane",
			AlsoFind: []string{"down the sidebar"}, Run: func() error {
				return a.focusInSidebarOrder(1)
			}},
		ui.Command{ID: "pane.previousInSidebar", Title: "Previous Pane",
			AlsoFind: []string{"up the sidebar"}, Run: func() error {
				return a.focusInSidebarOrder(-1)
			}},
		ui.Command{ID: "pane.next", Title: "Next Recent Pane",
			AlsoFind: []string{"last used", "used before", "mru", "switch"},
			Run:      func() error { return a.walkRecent(1) }},
		ui.Command{ID: "pane.previous", Title: "Previous Recent Pane",
			AlsoFind: []string{"last used", "back the other way", "mru", "switch"},
			Run:      func() error { return a.walkRecent(-1) }},
	})...)

	a.root.Commands = cmds
	// These are accelerators rather than ordinary bindings because the
	// terminal has a meaning for every key and would swallow them.
	a.root.Accelerators = defaultShortcuts()
}

// defaultShortcuts is the keymap gridterm comes with, built fresh.
//
// Its own function because a reload has to start from it: applying the
// file's changes again on top of a keymap they have already changed
// would not give a deleted line's built-in chord back.
func defaultShortcuts() *ui.Keymap {
	// Ctrl+Shift is the usual escape hatch: Ctrl+C has to stay available
	// to the program, so copy cannot live there.
	keys := ui.NewKeymap()
	keys.MustBind(map[ui.Chord]string{
		{Key: input.KeyC, Mods: input.ModCtrl | input.ModShift}: copyCommand,
		{Key: input.KeyV, Mods: input.ModCtrl | input.ModShift}: "edit.paste",
		// The picture written to a file and its path typed, for when a
		// name to hand to a program is what is wanted.
		{Key: input.KeyV, Mods: input.ModCtrl | input.ModAlt}: "edit.pasteImage",
		// The X11 spelling of paste, which plenty of people have in
		// their fingers and no terminal has a meaning for.
		{Key: input.KeyInsert, Mods: input.ModShift}: "edit.paste",
		{Key: input.KeyInsert, Mods: input.ModCtrl}:  copyCommand,
		{Key: input.KeyEquals, Mods: input.ModCtrl}:  "font.increase",
		{Key: input.KeyPlus, Mods: input.ModCtrl}:    "font.increase",
		// Ctrl+plus is Ctrl+Shift+= on a US layout, and the shift shows
		// up in the modifiers, so the obvious way to ask for a bigger
		// font needs its own binding.
		{Key: input.KeyEquals, Mods: input.ModCtrl | input.ModShift}: "font.increase",
		{Key: input.KeyMinus, Mods: input.ModCtrl}:                   "font.decrease",
		{Key: input.Key0, Mods: input.ModCtrl}:                       "font.reset",
		{Key: input.KeyPageUp, Mods: input.ModShift}:                 "view.scrollUp",
		{Key: input.KeyPageDown, Mods: input.ModShift}:               "view.scrollDown",
		{Key: input.KeyD, Mods: input.ModCtrl | input.ModShift}:      "pane.splitRight",
		{Key: input.KeyU, Mods: input.ModCtrl | input.ModShift}:      "pane.popOut",
		{Key: input.KeyE, Mods: input.ModCtrl | input.ModShift}:      "pane.splitDown",
		{Key: input.KeyW, Mods: input.ModCtrl | input.ModShift}:      "pane.close",
		{Key: input.KeyTab, Mods: input.ModCtrl}:                     "pane.next",
		{Key: input.KeyTab, Mods: input.ModCtrl | input.ModShift}:    "pane.previous",
		{Key: input.KeyT, Mods: input.ModCtrl | input.ModShift}:      "conn.terminal",
		{Key: input.KeyN, Mods: input.ModCtrl | input.ModShift}:      "server.connect",
		{Key: input.KeyB, Mods: input.ModCtrl | input.ModShift}:      "sidebar.toggle",
		{Key: input.KeyL, Mods: input.ModCtrl | input.ModShift}:      "sidebar.focus",
		{Key: input.KeyPageDown, Mods: input.ModCtrl}:                "pane.nextInSidebar",
		{Key: input.KeyPageUp, Mods: input.ModCtrl}:                  "pane.previousInSidebar",
		{Key: input.KeyG, Mods: input.ModCtrl | input.ModShift}:      "files.goTo",
		{Key: input.KeyA, Mods: input.ModCtrl | input.ModShift}:      switcherCommand,
		// Ctrl+Shift+K, not Ctrl+K: Ctrl+K is readline's kill-to-end-of-
		// line, and an accelerator runs before any widget sees the key,
		// so the shell would never get it.
		{Key: input.KeyK, Mods: input.ModCtrl | input.ModShift}: "palette.open",
		{Key: input.KeyF10}: "menu.open",
		{Key: input.KeyF11}: fullScreenCommand,
		// Not F1: that one belongs to whatever is running in the shell,
		// and every chord this window takes is Ctrl+Shift and a letter.
		{Key: input.KeyH, Mods: input.ModCtrl | input.ModShift}: helpCommand,
	})
	return keys
}

// programName is what the title bar and the notification area call
// this program.
const programName = "gridterm"
