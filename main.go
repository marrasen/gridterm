// Command gridterm is a GPU-rendered terminal emulator.
//
// It runs a shell on a local pseudo-terminal — a PTY on Unix, a ConPTY
// on Windows — feeds its output through a VT emulator, and draws the
// resulting character grid as batched triangles.
package main

import (
	"errors"
	"flag"
	"io"
	"log"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/hajimehoshi/ebiten/v2"
	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/gofont/gomonobold"

	"github.com/marcus/gridterm/glyph"
	"github.com/marcus/gridterm/grid"
	"github.com/marcus/gridterm/input"
	"github.com/marcus/gridterm/input/ebitenin"
	"github.com/marcus/gridterm/render"
	"github.com/marcus/gridterm/session"
	"github.com/marcus/gridterm/vt"
)

// readChunk is how much pty output is taken per read. Large enough that
// a flood of output does not become a syscall per line.
const readChunk = 64 * 1024

type app struct {
	atlas    *glyph.Atlas
	renderer *render.Renderer
	g        *grid.Grid
	reader   ebitenin.Reader

	// mu guards term. The pty pump runs on its own goroutine while
	// ebiten drives Update and Draw from another, and vt.Terminal is not
	// safe for concurrent use.
	mu   sync.Mutex
	term *vt.Terminal

	sess session.Session

	// pending is set by the pump when new output has been parsed, so a
	// frame with nothing to show can skip re-rendering the grid.
	pending atomic.Bool

	// exited is set once the shell is gone; the window closes on the
	// next frame.
	exited atomic.Bool

	title    atomic.Pointer[string]
	lastSize [2]int
	encBuf   []byte
}

func (a *app) Update() error {
	if a.exited.Load() {
		return ebiten.Termination
	}

	mode := a.mode()
	for _, ev := range a.reader.Poll() {
		a.handle(ev, mode)
	}

	if t := a.title.Swap(nil); t != nil {
		ebiten.SetWindowTitle("gridterm — " + *t)
	}
	return nil
}

// mode reads the terminal state the input encoder needs.
func (a *app) mode() input.Mode {
	a.mu.Lock()
	defer a.mu.Unlock()
	return input.Mode{AppCursor: a.term.Screen().AppCursor()}
}

func (a *app) handle(ev input.Event, mode input.Mode) {
	// Shift+PageUp and Shift+PageDown scroll the view rather than
	// reaching the program, which is the usual terminal convention.
	if ev.Kind == input.KeyPress || ev.Kind == input.KeyRepeat {
		if ev.Shift() && (ev.Key == input.KeyPageUp || ev.Key == input.KeyPageDown) {
			_, rows := a.g.Size()
			n := max(rows/2, 1)
			if ev.Key == input.KeyPageDown {
				n = -n
			}
			a.mu.Lock()
			a.term.Screen().ScrollView(n)
			a.mu.Unlock()
			a.pending.Store(true)
			return
		}
	}

	a.encBuf = input.EncodeMode(ev, mode, a.encBuf[:0])
	if len(a.encBuf) == 0 {
		return
	}

	// Typing jumps back to the live screen, as every terminal does.
	a.mu.Lock()
	scrolled := a.term.Screen().ViewOffset() != 0
	if scrolled {
		a.term.Screen().ResetView()
	}
	a.mu.Unlock()
	if scrolled {
		a.pending.Store(true)
	}

	if _, err := a.sess.Write(a.encBuf); err != nil {
		a.exited.Store(true)
	}
}

func (a *app) Draw(screen *ebiten.Image) {
	if a.pending.Swap(false) {
		a.mu.Lock()
		a.term.Render(a.g)
		a.mu.Unlock()
	}
	a.renderer.Draw(screen, a.g)
}

// Layout satisfies ebiten.Game; LayoutF below takes precedence when the
// runtime supports it.
func (a *app) Layout(w, h int) (int, int) {
	a.resizeTo(w, h)
	return w, h
}

// LayoutF sizes the grid in device pixels, so the cell box lands on
// whole pixels on a HiDPI display instead of being scaled.
func (a *app) LayoutF(logicalW, logicalH float64) (float64, float64) {
	s := ebiten.Monitor().DeviceScaleFactor()
	a.resizeTo(int(logicalW*s), int(logicalH*s))
	return logicalW * s, logicalH * s
}

// resizeTo tells the grid, the emulator and the shell about a new window
// size. All three have to agree or the shell wraps its prompt at the
// wrong column.
func (a *app) resizeTo(pxW, pxH int) {
	cols, rows := a.renderer.GridSizeFor(pxW, pxH)
	if a.lastSize == [2]int{cols, rows} {
		return
	}
	a.lastSize = [2]int{cols, rows}

	a.g.Resize(cols, rows)
	a.mu.Lock()
	a.term.Resize(cols, rows)
	a.mu.Unlock()
	a.pending.Store(true)

	if err := a.sess.Resize(cols, rows); err != nil {
		log.Printf("resize session: %v", err)
	}
}

// pump copies shell output into the emulator until the session ends.
func (a *app) pump() {
	buf := make([]byte, readChunk)
	for {
		n, err := a.sess.Read(buf)
		if n > 0 {
			a.mu.Lock()
			_, _ = a.term.Write(buf[:n])
			a.mu.Unlock()
			a.pending.Store(true)
		}
		if err != nil {
			if !errors.Is(err, io.EOF) {
				log.Printf("read session: %v", err)
			}
			a.exited.Store(true)
			return
		}
	}
}

func main() {
	var (
		fontSize = flag.Float64("font-size", 15, "font size in points")
		cmdline  = flag.String("e", "", "run this command instead of the login shell")
		scroll   = flag.Int("scrollback", vt.DefaultScrollback, "lines of history to keep")
	)
	flag.Parse()

	atlas, err := glyph.NewAtlas(gomono.TTF, gomonobold.TTF, *fontSize, 96)
	if err != nil {
		log.Fatalf("build glyph atlas: %v", err)
	}
	m := atlas.Metrics()

	a := &app{atlas: atlas, renderer: render.New(atlas)}

	const initCols, initRows = 100, 32
	pal := vt.DefaultPalette()
	a.g = grid.New(initCols, initRows, pal.FG, pal.BG)
	a.lastSize = [2]int{initCols, initRows}

	sess, err := session.StartLocal(session.LocalConfig{
		Command: strings.Fields(*cmdline),
		Cols:    initCols,
		Rows:    initRows,
	})
	if err != nil {
		log.Fatalf("start shell: %v", err)
	}
	a.sess = sess

	a.term = vt.New(initCols, initRows, pal, *scroll, vt.Callbacks{
		Title: func(s string) {
			t := s
			a.title.Store(&t)
		},
		Reply: func(b []byte) {
			// Device reports are produced while the pump holds the lock.
			// Writing to the pty from here is safe: Write is independent
			// of Read, and the reply is short enough not to block.
			if _, err := a.sess.Write(b); err != nil {
				a.exited.Store(true)
			}
		},
	})
	go a.pump()

	ebiten.SetWindowTitle("gridterm")
	ebiten.SetWindowSize(m.CellW*initCols, m.CellH*initRows)
	ebiten.SetWindowResizingMode(ebiten.WindowResizingModeEnabled)
	// Damage tracking only pays off if ebiten keeps the previous frame.
	ebiten.SetScreenClearedEveryFrame(false)
	ebiten.SetVsyncEnabled(true)

	err = ebiten.RunGame(a)
	_ = sess.Close()
	if err != nil && !errors.Is(err, ebiten.Termination) {
		log.Fatal(err)
	}
}
