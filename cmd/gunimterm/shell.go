package main

import (
	"log"
	"sync"

	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/internal/build"
	"github.com/marrasen/gridterm/session"
	"github.com/marrasen/gridterm/ui"
	uiterm "github.com/marrasen/gridterm/ui/term"
	"github.com/marrasen/gridterm/vt"
)

// shell is a running program and its screen, on gridterm's own
// terminal: the emulator, the selection, the mouse, the history and
// what an agent reads all behave as they do in gridterm. The window
// draws the screen into view, a grid of its own, and copies the rows
// that changed into the pane.
type shell struct {
	t *uiterm.Terminal
	// mu guards view, which the window's goroutine draws into and
	// reads from.
	mu   sync.Mutex
	view *grid.Grid
}

// shellHooks are what a shell tells the program: that it wrote, that
// it named itself, that it exited, and what it put on the clipboard.
// They run on the shell's reader goroutine.
type shellHooks struct {
	output    func()
	title     func(string)
	exit      func()
	clipboard func(string)
	bell      func()
	// link, findPath and openPath follow the links in the pane; nil
	// follows none.
	link     func(string)
	findPath func(text, dir string) (at string, isDir, ok bool)
	openPath func(at string, isDir bool, line int)
}

// shellCols and shellRows are a new shell's size, until its pane lays
// out and tells it its own.
const shellCols, shellRows = 80, 24

// scrollbackLines is how many lines of history a pane keeps, which
// -scrollback sets.
var scrollbackLines = vt.DefaultScrollback

// openShell puts a screen on a running session, local or remote,
// drawing with pal.
func openShell(sess session.Session, pal vt.Palette, hooks shellHooks) *shell {
	t, err := uiterm.New(uiterm.Config{
		Session:     sess,
		Size:        ui.Size{Cols: shellCols, Rows: shellRows},
		Scrollback:  scrollbackLines,
		Program:     build.Name + " " + build.Version(),
		Palette:     &pal,
		OnTitle:     hooks.title,
		OnExit:      hooks.exit,
		OnOutput:    hooks.output,
		OnClipboard: hooks.clipboard,
		OnBell:      hooks.bell,
		OnLink:      hooks.link,
		FindPath:    hooks.findPath,
		OnPath:      hooks.openPath,
		// A session's failures have nowhere else to go; the window log
		// keeps them, as gridterm's does.
		OnError: func(err error) { log.Printf("a pane's session: %v", err) },
	})
	if err != nil {
		// Only a missing session fails, and every caller has one.
		panic(err)
	}
	// Always focused, as far as the terminal knows, so it draws the
	// cursor into the view; the pane draws it hollow when it lacks the
	// keyboard.
	t.SetFocus(true)
	return &shell{t: t, view: grid.New(shellCols, shellRows, pal.FG, pal.BG)}
}

// draw draws the screen into view, at the size the screen is.
func (sh *shell) draw() {
	sh.mu.Lock()
	defer sh.mu.Unlock()
	size := sh.t.Size()
	if cols, rows := sh.view.Size(); cols != size.Cols || rows != size.Rows {
		sh.view.Resize(size.Cols, size.Rows)
	}
	sh.t.DrawScreen(sh.view.View())
}

// resize gives the shell a new size in cells. It reports whether the
// size changed.
func (sh *shell) resize(cols, rows int) bool {
	if s := sh.t.Size(); s.Cols == cols && s.Rows == rows {
		return false
	}
	sh.t.Layout(ui.Size{Cols: cols, Rows: rows})
	return true
}

func (sh *shell) close() { _ = sh.t.Close() }

// setPalette draws the screen in pal from now on, what is on it
// included.
func (sh *shell) setPalette(pal vt.Palette) { sh.t.SetPalette(pal) }
