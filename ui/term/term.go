// Package term puts a shell on a widget.
//
// It owns the emulator, the session and the goroutines that move bytes
// between them, so a program embedding a terminal deals only with the
// widget. It is the only part of the toolkit that knows what a terminal
// is: nothing in ui imports it.
package term

import (
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"

	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/input"
	"github.com/marrasen/gridterm/session"
	"github.com/marrasen/gridterm/ui"
	"github.com/marrasen/gridterm/vt"
)

// readChunk is how much session output is taken per read. Large enough
// that a flood of output does not become a syscall per line.
const readChunk = 64 * 1024

// outQueue is how many pending writes are held before input is dropped.
// Deep enough for a large paste, shallow enough that a program which has
// stopped reading cannot make the terminal hold an unbounded amount of
// typing on its behalf.
const outQueue = 256

// wheelLines is how far one wheel notch scrolls.
const wheelLines = 3

// Config describes a terminal. Session is required; the rest have
// workable defaults.
type Config struct {
	// Session is the shell, local or remote. The widget takes it over
	// and closes it.
	Session session.Session

	// Size is how big the terminal starts. Output can arrive before the
	// first Layout, and a shell banner parsed one column wide is
	// destroyed rather than reflowed.
	Size ui.Size

	// Scrollback is how many lines of history to keep.
	Scrollback int

	// Palette sets the default colours.
	Palette *vt.Palette

	// OnTitle is called when the program sets the window title, and
	// OnBell on BEL. Both arrive from the goroutine reading the session,
	// while it holds the emulator lock, so neither may call back into
	// the terminal: the lock is not reentrant and the reader would
	// deadlock against itself. Hand the value to the drawing goroutine
	// and return.
	OnTitle func(string)
	OnBell  func()

	// OnExit is called once when the shell goes, from the goroutine
	// reading the session.
	OnExit func()

	// OnError reports a session failure. There is nowhere to return one
	// from the goroutines moving bytes, and a terminal whose pipe has
	// broken will not recover, so the host is told and decides. A nil
	// one drops the error, which is the caller's choice to make.
	OnError func(error)

	// ReadClipboard and WriteClipboard back the paste and copy
	// shortcuts. A nil one disables that half.
	ReadClipboard  func() string
	WriteClipboard func(string)
}

// Terminal is a shell drawn as a widget.
//
// It is safe to build and use from the drawing goroutine only, apart
// from the callbacks in Config, which arrive from the goroutine reading
// the session.
type Terminal struct {
	cfg Config

	// mu guards term. The goroutine reading the session and the drawing
	// goroutine both reach it, and vt.Terminal is not safe for
	// concurrent use.
	mu   sync.Mutex
	term *vt.Terminal

	// g is the widget's own grid, the size of its area. The emulator
	// renders into it and Draw copies it into whatever view the layout
	// gives, so several terminals can share one layer.
	g *grid.Grid

	// out carries bytes destined for the session. Writing to a pty
	// blocks once the program stops reading its input, and both the
	// reader goroutine and the drawing goroutine produce input, so a
	// dedicated goroutine absorbs the block.
	out chan []byte

	// pending is set by the reader when new output has been parsed, so a
	// frame with nothing to show can skip re-rendering.
	pending atomic.Bool

	// said counts how many times the program has said anything, for
	// something watching from outside that needs to know the screen
	// moved without comparing it.
	said atomic.Uint64

	// exited is set once the shell is gone.
	exited atomic.Bool

	// closed guards against a second Close, and done stops writeLoop.
	// The queue itself is never closed: a device report can be sent from
	// the reader at any moment, and closing under it would panic.
	closed atomic.Bool
	done   chan struct{}

	// title is what the program last called the window, kept out here so
	// the drawing goroutine can read it without waiting on the reader,
	// which holds the lock for as long as it takes to parse a flood.
	title atomic.Pointer[string]

	// size is the last size Layout gave, and haveSize tells a genuine
	// zero size from never having been laid out.
	size     ui.Size
	haveSize bool

	focused bool
	encBuf  []byte

	// selecting is true between a press and its release, so motion is
	// only a drag when a drag started here.
	selecting bool

	// watchMu guards watchers, who are told what the program says from
	// the goroutine reading it and are added and removed from whichever
	// goroutine is carrying the connection they are on.
	watchMu  sync.Mutex
	watchers []Watcher

	// ended records that the program has gone, so a watcher arriving
	// afterwards is told at once rather than waiting for output from a
	// shell that has exited.
	ended bool
}

// New starts a terminal on the given session.
func New(cfg Config) (*Terminal, error) {
	if cfg.Session == nil {
		return nil, errors.New("terminal has no session")
	}
	pal := vt.DefaultPalette()
	if cfg.Palette != nil {
		pal = *cfg.Palette
	}
	if cfg.Scrollback <= 0 {
		cfg.Scrollback = vt.DefaultScrollback
	}

	t := &Terminal{
		cfg:  cfg,
		out:  make(chan []byte, outQueue),
		done: make(chan struct{}),
	}
	// At least one cell: an emulator with no columns has nowhere to put
	// the cursor.
	cols, rows := max(cfg.Size.Cols, 1), max(cfg.Size.Rows, 1)
	t.g = grid.New(cols, rows, pal.FG, pal.BG)
	t.g.SelectionBG = pal.Selection
	t.term = vt.New(cols, rows, pal, cfg.Scrollback, vt.Callbacks{
		Title: func(title string) {
			kept := title
			t.title.Store(&kept)
			if cfg.OnTitle != nil {
				cfg.OnTitle(title)
			}
		},
		Bell: func() {
			if cfg.OnBell != nil {
				cfg.OnBell()
			}
		},
		// Device reports are produced while the reader holds the lock, so
		// they must not touch the session directly: a program that has
		// stopped reading would block the write and deadlock the reader
		// against every other user of the lock.
		Reply: t.send,
	})

	go t.writeLoop()
	go t.readLoop()
	return t, nil
}

// Close stops the terminal and hands back the session's error. It has to
// be called: nothing else ends the goroutines moving bytes.
func (t *Terminal) Close() error {
	if t.closed.Swap(true) {
		return nil
	}
	close(t.done)
	return t.cfg.Session.Close()
}

// Exited reports whether the shell has gone.
func (t *Terminal) Exited() bool { return t.exited.Load() }

// Dirty reports whether there is new output to draw.
func (t *Terminal) Dirty() bool { return t.pending.Load() }

// Layout resizes the emulator and the session to match the area.
func (t *Terminal) Layout(size ui.Size) {
	cols, rows := max(size.Cols, 1), max(size.Rows, 1)
	if t.haveSize && t.size == size {
		return
	}
	t.size, t.haveSize = size, true

	t.mu.Lock()
	t.g.Resize(cols, rows)
	t.term.Resize(cols, rows)
	t.mu.Unlock()
	t.pending.Store(true)

	// A session that has already gone cannot be resized, and saying so
	// on every window drag would be noise.
	if err := t.cfg.Session.Resize(cols, rows); err != nil && !t.exited.Load() {
		t.fail(fmt.Errorf("resize session: %w", err))
	}
}

// Draw paints the terminal.
func (t *Terminal) Draw(v grid.View) {
	if t.pending.Swap(false) {
		t.mu.Lock()
		t.term.Render(t.g)
		t.mu.Unlock()
	}
	// Copying cell by cell rather than handing the emulator the view
	// keeps the grid's damage tracking: an unchanged cell is not written,
	// so an unchanged row stays clean.
	//
	// Reverse video and the selection highlight are resolved here rather
	// than carried over. Both live on the grid that holds them, and the
	// grid being drawn into belongs to the layout, which knows nothing
	// about either.
	cols, rows := v.Size()
	for y := 0; y < rows; y++ {
		for x := 0; x < cols; x++ {
			c := t.g.At(x, y)
			c.FG, c.BG = t.g.FGOf(x, y), t.g.BGOf(x, y)
			c.Attr &^= grid.AttrReverse
			v.Set(x, y, c)
		}
	}
	// Only the focused terminal touches the cursor. A grid has one and no
	// idea who owns it, so an unfocused widget writing even a hidden
	// cursor would take it from whoever has it. Clearing it once a frame
	// is the container's job.
	if t.focused {
		v.SetCursor(t.g.Cursor())
	}
}

// SetFocus takes the cursor with it: an unfocused terminal shows none.
func (t *Terminal) SetFocus(on bool) {
	t.focused = on
	t.pending.Store(true)
}

// Title returns the title the program last set, or "" if it set none.
func (t *Terminal) Title() string {
	if s := t.title.Load(); s != nil {
		return *s
	}
	return ""
}

// Focused reports whether this terminal is the one receiving keys.
func (t *Terminal) Focused() bool { return t.focused }

// Size returns the terminal's size in cells.
func (t *Terminal) Size() ui.Size { return t.size }

// HandleKey encodes a key for the program and sends it.
func (t *Terminal) HandleKey(ev input.Event) (bool, error) {
	t.encBuf = input.EncodeMode(ev, t.mode(), t.encBuf[:0])
	if len(t.encBuf) == 0 {
		return false, nil
	}

	// Typing jumps back to the live screen, as every terminal does.
	t.mu.Lock()
	scrolled := t.term.Screen().ViewOffset() != 0
	if scrolled {
		t.term.Screen().ResetView()
	}
	t.mu.Unlock()
	if scrolled {
		t.pending.Store(true)
	}

	// Typing replaces a selection, as it does everywhere else.
	t.g.ClearSelection()
	t.send(t.encBuf)
	return true, nil
}

// HandleMouse either reports to the program or drives the selection.
//
// A program with mouse reporting on owns the mouse, except while Shift
// is held. That is how xterm lets you select text inside a program that
// has taken the mouse over, and every terminal since has copied it.
func (t *Terminal) HandleMouse(ev input.MouseEvent) (bool, error) {
	mode, onAlt := t.mouseMode()
	if mode.Enabled() && !ev.Mods.Has(input.ModShift) {
		t.send(input.EncodeMouse(ev, mode, nil))
		return true, nil
	}

	switch ev.Button {
	case input.MouseWheelUp, input.MouseWheelDown:
		if ev.Kind != input.MousePress {
			return true, nil
		}
		n := wheelLines
		if ev.Button == input.MouseWheelDown {
			n = -n
		}
		if onAlt {
			// The alternate screen has no scrollback to move through, so
			// the wheel becomes arrow keys, which is what lets less and
			// man scroll with it.
			t.sendArrows(n)
			return true, nil
		}
		t.ScrollView(n)
		return true, nil

	case input.MouseMiddle:
		// The X11 convention. Harmless elsewhere.
		if ev.Kind == input.MousePress {
			t.Paste(t.readClipboard())
		}
		return true, nil
	}

	if ev.Button != input.MouseLeft && ev.Kind != input.MouseMove {
		return false, nil
	}
	switch ev.Kind {
	case input.MousePress:
		t.selecting = true
		t.g.SetSelection(grid.Selection{
			Anchor: grid.Point{X: ev.Col, Y: ev.Row},
			Cursor: grid.Point{X: ev.Col, Y: ev.Row},
			Active: true,
			Block:  ev.Mods.Has(input.ModAlt),
		})
	case input.MouseMove:
		if !t.selecting {
			return false, nil
		}
		sel := t.g.Selection()
		sel.Cursor = grid.Point{X: ev.Col, Y: ev.Row}
		t.g.SetSelection(sel)
	case input.MouseRelease:
		t.selecting = false
		// A click that never moved is a click, not an empty selection
		// left highlighting one cell.
		sel := t.g.Selection()
		if sel.Anchor == sel.Cursor {
			t.g.ClearSelection()
		}
	}
	t.pending.Store(true)
	return true, nil
}

// CancelGesture lets go of a drag whose release will never arrive,
// because a dialog opened over the terminal or its pane left the screen.
// Left alone, the next time the pointer crossed the terminal with no
// button down it would carry on extending the selection.
func (t *Terminal) CancelGesture() { t.selecting = false }

// SelectionText returns the text currently selected, or "" when nothing
// is.
func (t *Terminal) SelectionText() string { return t.g.SelectedText() }

// Copy puts the selection on the clipboard. It reports whether there was
// anything to copy.
func (t *Terminal) Copy() bool {
	text := t.g.SelectedText()
	if text == "" || t.cfg.WriteClipboard == nil {
		return false
	}
	t.cfg.WriteClipboard(text)
	return true
}

// Paste sends text to the program, bracketed if it asked for that.
func (t *Terminal) Paste(text string) {
	if text == "" {
		return
	}
	t.mu.Lock()
	bracketed := t.term.Screen().Bracketed()
	t.mu.Unlock()
	t.send(input.EncodePaste(text, bracketed, nil))
}

// PasteClipboard sends whatever is on the clipboard.
func (t *Terminal) PasteClipboard() { t.Paste(t.readClipboard()) }

// ScrollView moves through the scrollback, positive for backwards.
func (t *Terminal) ScrollView(n int) {
	t.mu.Lock()
	t.term.Screen().ScrollView(n)
	t.mu.Unlock()
	t.pending.Store(true)
}

// ScrollPages moves n screenfuls through the scrollback.
func (t *Terminal) ScrollPages(n int) {
	rows := max(t.size.Rows/2, 1)
	t.ScrollView(n * rows)
}

func (t *Terminal) readClipboard() string {
	if t.cfg.ReadClipboard == nil {
		return ""
	}
	return t.cfg.ReadClipboard()
}

// mode reads the terminal state the key encoder needs.
func (t *Terminal) mode() input.Mode {
	t.mu.Lock()
	defer t.mu.Unlock()
	return input.Mode{AppCursor: t.term.Screen().AppCursor()}
}

// mouseMode reads the mouse state and which buffer is in use, in one
// pass under the lock rather than two.
func (t *Terminal) mouseMode() (input.MouseMode, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	scr := t.term.Screen()
	click, drag, motion, sgr := scr.MouseModes()
	return input.MouseMode{Click: click, Drag: drag, Motion: motion, SGR: sgr},
		scr.OnAltBuffer()
}

// sendArrows sends n arrow keys, up for positive.
func (t *Terminal) sendArrows(n int) {
	key := input.KeyUp
	if n < 0 {
		key, n = input.KeyDown, -n
	}
	mode := t.mode()

	var buf []byte
	for i := 0; i < n; i++ {
		buf = input.EncodeMode(
			input.Event{Kind: input.KeyPress, Key: key}, mode, buf)
	}
	t.send(buf)
}

// send queues bytes for the session. It never blocks: a program that has
// stopped reading its input cannot be helped by queueing more, and
// blocking here would freeze the window.
func (t *Terminal) send(b []byte) {
	if len(b) == 0 {
		return
	}
	// The caller reuses its buffer and the write happens later.
	cp := append([]byte(nil), b...)
	select {
	case t.out <- cp:
	default:
	}
}

// writeLoop moves queued bytes into the session until Close, which
// abandons whatever is still queued. Close hangs up the session in the
// same breath, so those bytes had nowhere to go.
func (t *Terminal) writeLoop() {
	for {
		select {
		case <-t.done:
			return
		case b := <-t.out:
			if _, err := t.cfg.Session.Write(b); err != nil {
				t.fail(fmt.Errorf("write session: %w", err))
				t.finish()
				return
			}
		}
	}
}

// readLoop copies session output into the emulator until it ends.
func (t *Terminal) readLoop() {
	buf := make([]byte, readChunk)
	for {
		n, err := t.cfg.Session.Read(buf)
		if n > 0 {
			t.mu.Lock()
			_, _ = t.term.Write(buf[:n])
			// And to anyone watching from another machine, who is
			// shown the same bytes rather than a second rendering of
			// them: what they see is then what is on this screen.
			// Still under the lock, so a chunk cannot be handed on
			// after a screen that was taken once it was parsed.
			t.tell(buf[:n])
			t.mu.Unlock()
			t.pending.Store(true)
			t.said.Add(1)
		}
		if err != nil {
			if !errors.Is(err, io.EOF) {
				t.fail(fmt.Errorf("read session: %w", err))
			}
			t.finish()
			return
		}
	}
}

// finish records that the shell has gone and tells the host once.
func (t *Terminal) finish() {
	if t.exited.Swap(true) {
		return
	}
	// Whoever is watching from elsewhere, before the window is told:
	// their pane is drawing this program and has no other way to learn
	// it has gone.
	t.endWatchers()
	if t.cfg.OnExit != nil {
		t.cfg.OnExit()
	}
}

// fail reports a session error to the host. There is nowhere to return
// one from a goroutine, and a terminal whose pipe has broken is not
// going to recover, so the host is told and decides.
func (t *Terminal) fail(err error) {
	if t.cfg.OnError != nil {
		t.cfg.OnError(err)
	}
}
