package main

import (
	"fmt"
	"sync"

	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/session"
	"github.com/marrasen/gridterm/vt"
)

// shell is a running shell and the screen its output draws on. The
// reader goroutine writes the screen and the window's goroutine reads
// it, each holding mu.
type shell struct {
	mu   sync.Mutex
	vt   *vt.Terminal
	grid *grid.Grid
	pal  vt.Palette

	sess session.Session
	// out carries keys to the writer goroutine, so a shell that stops
	// reading holds up that goroutine and never the window.
	out  chan []byte
	done chan struct{}
}

// shellHooks are what a shell tells the program: that it wrote, that
// it named itself, and that it exited. They run on the shell's reader
// goroutine.
type shellHooks struct {
	output    func()
	title     func(string)
	exit      func()
	clipboard func(string)
}

// shellCols and shellRows are a new shell's size, until its pane lays
// out and tells it its own.
const shellCols, shellRows = 80, 24

// startLocal starts the user's shell on this machine, drawing with pal.
func startLocal(pal vt.Palette, hooks shellHooks) (*shell, error) {
	sess, err := session.StartLocal(session.LocalConfig{Cols: shellCols, Rows: shellRows})
	if err != nil {
		return nil, fmt.Errorf("gunimterm: start the shell: %w", err)
	}
	return openShell(sess, pal, hooks), nil
}

// openShell puts a screen on a running session, local or remote,
// drawing with pal.
func openShell(sess session.Session, pal vt.Palette, hooks shellHooks) *shell {
	const cols, rows = shellCols, shellRows
	sh := &shell{
		pal:  pal,
		grid: grid.New(cols, rows, pal.FG, pal.BG),
		sess: sess,
		out:  make(chan []byte, 1024),
		done: make(chan struct{}),
	}
	sh.grid.SelectionBG = pal.Selection
	sh.vt = vt.New(cols, rows, pal, 5000, vt.Callbacks{Reply: sh.send, Title: hooks.title, ClipboardSet: hooks.clipboard})
	go sh.read(hooks)
	go sh.write()
	return sh
}

func (sh *shell) read(hooks shellHooks) {
	defer hooks.exit()
	defer close(sh.done)
	buf := make([]byte, 64<<10)
	for {
		n, err := sh.sess.Read(buf)
		if n > 0 {
			sh.mu.Lock()
			_, _ = sh.vt.Write(buf[:n])
			sh.mu.Unlock()
			hooks.output()
		}
		if err != nil {
			return
		}
	}
}

func (sh *shell) write() {
	for b := range sh.out {
		if _, err := sh.sess.Write(b); err != nil {
			return
		}
	}
}

// send queues bytes for the shell, dropping them when the shell has
// stopped reading and the queue is full, as gridterm does.
func (sh *shell) send(b []byte) {
	if len(b) == 0 {
		return
	}
	select {
	case sh.out <- append([]byte(nil), b...):
	default:
	}
}

// resize gives the shell a new size in cells. It reports whether the
// size changed.
func (sh *shell) resize(cols, rows int) bool {
	sh.mu.Lock()
	defer sh.mu.Unlock()
	if c, r := sh.vt.Screen().Size(); c == cols && r == rows {
		return false
	}
	sh.vt.Resize(cols, rows)
	_ = sh.sess.Resize(cols, rows)
	return true
}

func (sh *shell) close() { _ = sh.sess.Close() }

// setPalette draws the screen in pal from now on, what is on it
// included.
func (sh *shell) setPalette(pal vt.Palette) {
	sh.mu.Lock()
	defer sh.mu.Unlock()
	sh.pal = pal
	sh.grid.SelectionBG = pal.Selection
	sh.vt.Screen().SetPalette(pal)
}
