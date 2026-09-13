// Command gridterm is a GPU-rendered terminal emulator.
//
// It runs a shell either on a local pseudo-terminal — a PTY on Unix, a
// ConPTY on Windows — or on another machine over SSH, feeds the output
// through a VT emulator, and draws the resulting character grid as
// batched triangles.
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

// outQueue is how many pending writes to the session are held before
// input is dropped. Deep enough for a large paste, shallow enough that a
// program which has stopped reading cannot make the terminal hold an
// unbounded amount of typing on its behalf.
const outQueue = 256

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

	// out carries bytes destined for the session. Writing to a pty
	// blocks once the program stops reading its input, and both the pump
	// — which holds a.mu — and the ebiten UI thread produce input. If
	// either wrote directly, a program that stopped reading would wedge
	// the whole terminal. A dedicated goroutine absorbs the block.
	out chan []byte

	// pending is set by the pump when new output has been parsed, so a
	// frame with nothing to show can skip re-rendering the grid.
	pending atomic.Bool

	// exited is set once the shell is gone; the window closes on the
	// next frame.
	exited atomic.Bool

	title    atomic.Pointer[string]
	lastSize [2]int
	encBuf   []byte

	// drewFinal records that the frame after the shell exited has been
	// drawn. ebiten returns from Update before Draw, so terminating the
	// moment the shell goes would discard its last output.
	drewFinal bool
}

// send queues bytes for the session. It never blocks: a program that has
// stopped reading its input cannot be helped by queueing more, and
// blocking here would freeze the window.
func (a *app) send(b []byte) {
	if len(b) == 0 {
		return
	}
	// The caller reuses its buffer and the write happens later.
	cp := append([]byte(nil), b...)
	select {
	case a.out <- cp:
	default:
	}
}

// writeLoop drains the queue into the session.
func (a *app) writeLoop() {
	for b := range a.out {
		if _, err := a.sess.Write(b); err != nil {
			a.exited.Store(true)
			return
		}
	}
}

func (a *app) Update() error {
	if a.exited.Load() {
		// Give Draw one more frame to paint what the shell wrote last.
		if a.drewFinal {
			return ebiten.Termination
		}
		a.drewFinal = true
		return nil
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

	a.send(a.encBuf)
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

	if err := a.sess.Resize(cols, rows); err != nil && !a.exited.Load() {
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
		cmdline  = flag.String("e", "",
			"run this command instead of the login shell; split on spaces, no quoting")
		scroll = flag.Int("scrollback", vt.DefaultScrollback, "lines of history to keep")
		remote = flag.String("ssh", "",
			"connect to [user@]host[:port] over SSH instead of running a local shell")
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

	sess, err := startSession(*remote, strings.Fields(*cmdline), initCols, initRows)
	if err != nil {
		log.Fatalf("start session: %v", err)
	}
	a.sess = sess
	a.out = make(chan []byte, outQueue)
	go a.writeLoop()

	a.term = vt.New(initCols, initRows, pal, *scroll, vt.Callbacks{
		Title: func(s string) {
			t := s
			a.title.Store(&t)
		},
		// Device reports are produced while the pump holds the lock, so
		// they must not touch the pty directly: a program that has
		// stopped reading would block the write and deadlock the pump
		// against every other user of the lock.
		Reply: a.send,
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

// startSession opens either a local shell or an SSH connection. The rest
// of the program cannot tell the difference: both are a byte stream and
// a size.
func startSession(target string, command []string, cols, rows int) (session.Session, error) {
	if target == "" {
		return session.StartLocal(session.LocalConfig{
			Command: command,
			Cols:    cols,
			Rows:    rows,
		})
	}
	cfg, err := session.ParseSSHTarget(target)
	if err != nil {
		return nil, err
	}
	cfg.Command = command
	cfg.Cols, cfg.Rows = cols, rows
	// Without these, SSH works only with an agent or an unencrypted key
	// on disk: a passphrase-protected key is skipped and password
	// authentication is never even offered.
	cfg.Passphrase = func(keyfile string) (string, error) {
		return promptSecret("passphrase for " + keyfile + ": ")
	}
	cfg.Password = func() (string, error) {
		return promptSecret("password for " + cfg.User + "@" + cfg.Host + ": ")
	}
	return session.StartSSH(cfg)
}
