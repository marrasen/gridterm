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

// startShell starts the user's shell at 80 by 24, calling changed each
// time output arrives.
func startShell(changed func()) (*shell, error) {
	const cols, rows = 80, 24
	sess, err := session.StartLocal(session.LocalConfig{Cols: cols, Rows: rows})
	if err != nil {
		return nil, fmt.Errorf("gunimterm: start the shell: %w", err)
	}
	pal := vt.DefaultPalette()
	sh := &shell{
		pal:  pal,
		grid: grid.New(cols, rows, pal.FG, pal.BG),
		sess: sess,
		out:  make(chan []byte, 1024),
		done: make(chan struct{}),
	}
	sh.vt = vt.New(cols, rows, pal, 5000, vt.Callbacks{Reply: sh.send})
	go sh.read(changed)
	go sh.write()
	return sh, nil
}

func (sh *shell) read(changed func()) {
	defer close(sh.done)
	buf := make([]byte, 64<<10)
	for {
		n, err := sh.sess.Read(buf)
		if n > 0 {
			sh.mu.Lock()
			_, _ = sh.vt.Write(buf[:n])
			sh.mu.Unlock()
			changed()
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
