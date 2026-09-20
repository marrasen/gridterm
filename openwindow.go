package main

import (
	"context"
	"fmt"
	"os"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/marrasen/gridterm/appicon"
	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/glyph"
	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/jobs"
	"github.com/marrasen/gridterm/meter"
	"github.com/marrasen/gridterm/notify"
	"github.com/marrasen/gridterm/remote"
	"github.com/marrasen/gridterm/render"
	"github.com/marrasen/gridterm/session"
	"github.com/marrasen/gridterm/ui"
	"github.com/marrasen/gridterm/ui/term"
	"github.com/marrasen/gridterm/vt"
)

// wanted is what a window is opened with: what the flags asked for, and
// the two seams a test fills in for itself.
type wanted struct {
	atlas      *glyph.Atlas
	fontSize   float64
	fontFamily string

	// sizeFixed says a size was named on the command line, which the size
	// remembered from the last run does not override.
	sizeFixed bool

	// fontFixed says a typeface was named on the command line, which a
	// theme does not overrule.
	fontFixed  bool
	command    []string
	scrollback int
	ssh        string
	stats      bool
	shot       string

	// newShell starts a shell. Nil starts a real one.
	newShell func(argv []string, dir string, cols, rows int) (session.Session, error)

	// scan says to go looking for the fonts and the shells this machine
	// has. Both reach the machine and take their time over it.
	scan bool
}

// openWindowCols and openWindowRows are the size a window opens at,
// before the window system has said how big it really is.
const openWindowCols, openWindowRows = 100, 32

// openWindow builds a window: everything it remembers, every command it
// can run, the widget tree, and the pane it opens on.
//
// It stops short of the game loop, so a test can build the same window
// this does and see that a feature really is wired into it.
func openWindow(w wanted) (*app, error) {

	a := &app{atlas: w.atlas, renderer: render.New(w.atlas), fontSize: w.fontSize}
	// What -font-family chose, so the font menu treats it as the one in
	// use rather than offering to switch to it again.
	a.fontFamily, a.fontFixed = w.fontFamily, w.fontFixed
	a.comp = render.NewCompositor(a.renderer)
	// The compositor is called by the game loop and has nowhere to hand
	// a failure back to.
	a.comp.OnError = a.logError

	const openWindowCols, openWindowRows = 100, 32
	pal := vt.DefaultPalette()
	a.g = grid.New(openWindowCols, openWindowRows, pal.FG, pal.BG)
	a.layer = &render.Layer{Grid: a.g}
	a.comp.Add(a.layer)
	a.lastSize = [2]int{openWindowCols, openWindowRows}

	// What every pane is started with, so a split can open another.
	a.command = w.command
	a.keys = remote.NewRing()
	a.ctx, a.stop = context.WithCancel(context.Background())
	a.book = loadBook()
	a.newShell = w.newShell
	if a.newShell == nil {
		a.newShell = func(argv []string, dir string, cols, rows int) (session.Session, error) {
			return session.StartLocal(session.LocalConfig{
				Command: argv,
				Dir:     dir,
				Cols:    cols,
				Rows:    rows,
			})
		}
	}
	if w.stats {
		a.stats = newWatchStats(os.Stderr)
	}
	a.registry = conns.New()
	// A copy that could not be made is said. Something the user was
	// told is on the clipboard and is not is worth knowing about.
	a.clip.failed = func(err error) {
		a.pump.post(func() { a.reportError("Could not copy to the clipboard", err) })
	}
	a.rates = make(map[*conns.Entry]*meter.Rate)
	a.machines = newMachines()
	a.windows = newWindows(a.book)
	a.serving = newServing()
	a.agents = newAgents()
	a.shellPick = newShellPick()
	a.saved = newSavedCommands()
	a.paneTitles = newPaneTitles()
	a.shellSetup = newShellSetup()
	a.far = newPathsFar()
	a.toasts = notify.New(programName)
	a.keyFiles = newKeyIndex()
	a.theme = newThemePick()
	a.copies = newSavedCopies()
	a.font = newFontPick(w.sizeFixed)
	a.useSettings(openSettings())
	a.useStartFontSize()
	a.tunnels = make(map[*conns.Entry]*tunnel)
	a.queue = jobs.New(0)
	a.jobs = make(map[*conns.Entry]*jobs.Job)
	a.asking = make(map[chan jobs.Choice]func())
	a.scrollback = w.scrollback
	a.colours = pal
	a.loadThemes()
	a.offerToServeAgain()
	if err := a.useTheme(a.startTheme()); err != nil {
		// The theme the window opens on comes from the list, and every
		// theme in it was read and checked when the file was. A typeface
		// it names is not a reason to fail: useWantedFont says so itself
		// and leaves the colours alone.
		a.logError(err)
	}
	a.panes = make(map[*term.Terminal]*conns.Entry)
	a.scaled = make(map[*term.Terminal]*scaledPane)
	a.shared = make(map[*term.Terminal]*sharedMark)
	a.ended = make(map[*term.Terminal]bool)
	a.started = make(map[*term.Terminal]*startedAs)
	a.exits = make(chan struct{}, exitQueue)

	first, err := a.openFirst(startup{target: w.ssh, command: a.command})
	if err != nil {
		return nil, err
	}

	a.commands()
	if err := a.loadShortcuts(); err != nil {
		return nil, err
	}
	shot, err := parseShotScript(w.shot)
	if err != nil {
		return nil, fmt.Errorf("-shot: %w", err)
	}
	a.shot = shot
	// Off the drawing goroutine: reading every font file the system has
	// takes long enough to be seen as the window failing to open, and
	// wsl.exe is slow to say which distributions it has.
	if w.scan {
		a.startFontScan()
		a.startShellScan()
	}

	// The tree: the menu bar over the sidebar and the stage beside it.
	// Everything the window opens goes on the stage, which shows one at
	// a time; the sidebar is what chooses.
	a.panel = a.newPanel()
	a.side = a.newSidebar()
	a.stage = a.newDeck(startingPanes(first)...)
	a.dock = a.newDock(a.stage)
	// The sidebar is painted onto a grid of its own, over the window's,
	// so that its rows can have room around them while the terminal
	// beside it keeps every line the same height.
	a.sideRegion = newRegion(a.side, grid.New(0, 0, a.colours.FG, a.colours.BG), &a.sideGeo)
	a.dock.PanelElsewhere = true
	a.comp.Add(a.sideRegion.layer)
	// Open to begin with: it is how everything in the window is reached,
	// so a window that hid it would open with no way in.
	a.dock.Collapsed = false
	a.bar = a.newMenubar(a.dock)
	a.refreshServers()
	// Told once there is a window to tell them in: this runs before one
	// exists, so the message waits for the first frame.
	a.reportBookError()
	a.root.SetWidget(a.bar)
	a.root.Layout(ui.Rect{Cols: openWindowCols, Rows: openWindowRows})

	return a, nil
}

// sizeTheWindow tells the window system how big to open, which only a
// real window has to do.
func (a *app) sizeTheWindow(m glyph.Metrics) {
	// What the window frame and the taskbar show while gridterm runs.
	ebiten.SetWindowIcon(appicon.Images())
	ebiten.SetWindowTitle(programName)
	// Room for the padding on top of the cells, or the window opens a
	// column and a row short of the size it was asked for.
	padX, padY := a.padsWanted()
	ebiten.SetWindowSize(
		m.CellW*openWindowCols+padX*m.CellW/grid.PadUnit,
		m.CellH*openWindowRows+padY*m.CellH/grid.PadUnit)
	ebiten.SetWindowResizingMode(ebiten.WindowResizingModeEnabled)
	// Damage tracking only pays off if ebiten keeps the previous frame.
	ebiten.SetScreenClearedEveryFrame(false)
	ebiten.SetVsyncEnabled(true)

}
