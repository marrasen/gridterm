// Package screen keeps the programs running in panes, each with its
// screen: the program side starts them, and the window draws them.
package screen

import (
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/marrasen/kakel/grid"
	"github.com/marrasen/kakel/internal/build"
	"github.com/marrasen/kakel/session"
	"github.com/marrasen/kakel/ui"
	uiterm "github.com/marrasen/kakel/ui/term"
	"github.com/marrasen/kakel/vt"
)

// Shell is a running program and its screen, on kakel's own terminal:
// the emulator, the selection, the mouse, the history and what an agent
// reads. The window draws the screen into view, a grid of its own, and
// copies the rows that changed into the pane.
type Shell struct {
	T *uiterm.Terminal
	// mu guards view, which the window's goroutine draws into and
	// reads from.
	mu   sync.Mutex
	view *grid.Grid
	// wrote is when the program last wrote, in nanoseconds, for the
	// sidebar's mark to breathe while output comes.
	wrote *atomic.Int64
}

// Hooks are what a shell tells the program: that it wrote, that
// it named itself, that it exited, and what it put on the clipboard.
// They run on the shell's reader goroutine.
type Hooks struct {
	Output    func()
	Title     func(string)
	Exit      func()
	Clipboard func(string)
	Bell      func()
	// CommandDone says a command finished, as a shell that marks its
	// commands says so: its exit status, whether the shell gave one, and
	// how long it ran.
	CommandDone func(status int, ok bool, took time.Duration)
	// Link, FindPath and OpenPath follow the links in the pane; nil
	// follows none.
	Link     func(string)
	FindPath func(text, dir string) (at string, isDir, ok bool)
	OpenPath func(at string, isDir bool, line int)
}

// Cols and Rows are a new shell's size, until its pane lays
// out and tells it its own.
const Cols, Rows = 80, 24

// ScrollbackLines is how many lines of history a pane keeps, which
// -scrollback sets.
var ScrollbackLines = vt.DefaultScrollback

// Open puts a screen on a running session, local or remote,
// drawing with pal.
func Open(sess session.Session, pal vt.Palette, hooks Hooks) *Shell {
	wrote := new(atomic.Int64)
	t, err := uiterm.New(uiterm.Config{
		Session:       sess,
		Size:          ui.Size{Cols: Cols, Rows: Rows},
		Scrollback:    ScrollbackLines,
		Program:       build.Name + " " + build.Version(),
		Palette:       &pal,
		OnTitle:       hooks.Title,
		OnExit:        hooks.Exit,
		OnOutput:      func() { wrote.Store(time.Now().UnixNano()); hooks.Output() },
		OnClipboard:   hooks.Clipboard,
		OnBell:        hooks.Bell,
		OnCommandDone: hooks.CommandDone,
		OnLink:        hooks.Link,
		FindPath:      hooks.FindPath,
		OnPath:        hooks.OpenPath,
		// A session's failures have nowhere else to go; the window log
		// keeps them.
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
	return &Shell{T: t, view: grid.New(Cols, Rows, pal.FG, pal.BG), wrote: wrote}
}

// Drawn draws the screen into its grid, at the size the screen is, and
// runs f on the grid while no other goroutine draws into it.
func (sh *Shell) Drawn(f func(*grid.Grid)) {
	sh.mu.Lock()
	defer sh.mu.Unlock()
	size := sh.T.Size()
	if cols, rows := sh.view.Size(); cols != size.Cols || rows != size.Rows {
		sh.view.Resize(size.Cols, size.Rows)
	}
	sh.T.DrawScreen(sh.view.View())
	f(sh.view)
}

// Resize gives the shell a new size in cells. It reports whether the
// size changed.
func (sh *Shell) Resize(cols, rows int) bool {
	if s := sh.T.Size(); s.Cols == cols && s.Rows == rows {
		return false
	}
	sh.T.Layout(ui.Size{Cols: cols, Rows: rows})
	return true
}

// Close ends the program and its screen.
func (sh *Shell) Close() { _ = sh.T.Close() }

// SetPalette draws the screen in pal from now on, what is on it
// included.
func (sh *Shell) SetPalette(pal vt.Palette) { sh.T.SetPalette(pal) }

// Shells holds the running shells by pane, for the window to draw.
type Shells struct {
	mu sync.Mutex
	m  map[string]*Shell
}

// NewShells holds no shells yet.
func NewShells() *Shells { return &Shells{m: map[string]*Shell{}} }

// Get is the shell of pane id, or nil.
func (s *Shells) Get(id string) *Shell {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.m[id]
}

// All returns the running shells.
func (s *Shells) All() []*Shell {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*Shell, 0, len(s.m))
	for _, sh := range s.m {
		out = append(out, sh)
	}
	return out
}

// Set keeps sh as the shell of pane id, or forgets it for nil.
func (s *Shells) Set(id string, sh *Shell) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sh == nil {
		delete(s.m, id)
		return
	}
	s.m[id] = sh
}

// Wrote is when the program last wrote, the zero time before it has.
func (sh *Shell) Wrote() time.Time {
	if n := sh.wrote.Load(); n != 0 {
		return time.Unix(0, n)
	}
	return time.Time{}
}
