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
	// and closes it, and Restart puts another one in its place.
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

	// closed guards against a second Close. The queue itself is never
	// closed: a device report can be sent from the reader at any moment,
	// and closing under it would panic.
	closed atomic.Bool

	// runMu guards run, which Restart swaps while the drawing goroutine
	// may be closing or resizing.
	runMu sync.Mutex
	run   *run

	// title is what the program last called the window, kept out here so
	// the drawing goroutine can read it without waiting on the reader,
	// which holds the lock for as long as it takes to parse a flood.
	title atomic.Pointer[string]

	// size is the last size Layout gave, and haveSize tells a genuine
	// zero size from never having been laid out.
	size     ui.Size
	haveSize bool

	// box is the room the layout gave this terminal, which is the same
	// as size unless somebody watching has been given the size. Then
	// the screen is drawn into the top-left of the box: bigger, and what
	// does not fit is not shown here; smaller, and the rest of the box
	// is left blank.
	box ui.Size

	// held says the size is somebody else's. The layout stops setting
	// it while it is, or this window would take it straight back.
	held bool

	// elsewhere says the host paints the screen somewhere other than the
	// room the layout gave it, so Draw leaves that room blank.
	elsewhere bool

	// pal is the colours the terminal was built with, kept so the window
	// can draw over the screen in them.
	pal vt.Palette

	// ask is the question drawn on the pane's last row, and nil when
	// there is none. The drawing goroutine owns it.
	ask *asked

	focused bool
	encBuf  []byte

	// selecting is true between a press and its release, so motion is
	// only a drag when a drag started here.
	selecting bool

	// reported is a bit per button the program was told went down, so a
	// drag or a release from a gesture it never saw the press for is
	// held back.
	reported uint8

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

// run is a session and the two goroutines moving bytes to and from it.
//
// A restart makes a new one rather than pointing the old goroutines at
// another session, so a loop that is about to read cannot pick up a
// session that was swapped under it.
type run struct {
	sess session.Session

	// stop ends writeLoop. Close and Restart both end a run, and either
	// may follow the other, so it is closed once and no more.
	stop     chan struct{}
	stopOnce sync.Once

	// wg falls to zero once both loops have returned.
	wg sync.WaitGroup
}

// halt tells writeLoop to stop, however many times it is called.
func (r *run) halt() { r.stopOnce.Do(func() { close(r.stop) }) }

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
		cfg: cfg,
		pal: pal,
		out: make(chan []byte, outQueue),
	}
	r := t.adopt(cfg.Session)
	// At least one cell: an emulator with no columns has nowhere to put
	// the cursor.
	cols, rows := max(cfg.Size.Cols, 1), max(cfg.Size.Rows, 1)
	// The size the screen really is, before any layout, so a terminal
	// that ends before it is laid out still reports the screen it has.
	t.size = ui.Size{Cols: cols, Rows: rows}
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

	t.begin(r)
	return t, nil
}

// adopt takes a session over and makes it the one this terminal is on.
// The goroutines are begun separately, so a restart can size the session
// before anything reads it.
func (t *Terminal) adopt(sess session.Session) *run {
	r := &run{sess: sess, stop: make(chan struct{})}
	// A resize that fails does so after the drag that asked for it, so
	// the session hands it here rather than to a caller that has gone.
	if late, ok := sess.(lateFailures); ok {
		late.ReportLate(func(err error) {
			// And not at all from a session this terminal has let go of,
			// whose resize failed on a pane that now has a program in it.
			if t.current() == r {
				t.fail(lateResize(err))
			}
		})
	}
	t.runMu.Lock()
	t.run = r
	t.runMu.Unlock()
	return r
}

// begin sets a run's goroutines going.
func (t *Terminal) begin(r *run) {
	r.wg.Add(2)
	go t.writeLoop(r)
	go t.readLoop(r)
}

// current is the run the terminal is on now.
func (t *Terminal) current() *run {
	t.runMu.Lock()
	defer t.runMu.Unlock()
	return t.run
}

// Close stops the terminal and hands back the session's error. It has to
// be called: nothing else ends the goroutines moving bytes.
func (t *Terminal) Close() error {
	if t.closed.Swap(true) {
		return nil
	}
	r := t.current()
	r.halt()
	return r.sess.Close()
}

// Restart puts a new session under the terminal, keeping what is on the
// screen and in the scrollback, and gives both the room the pane has
// now, which reflows the scrollback and cuts it where the pane has
// narrowed since.
//
// It refuses a terminal whose program is still running, and hands back
// the old session's hangup error rather than starting a second program
// on top of one that would not go.
func (t *Terminal) Restart(sess session.Session) error {
	if sess == nil {
		return errors.New("restart: no session to put in the pane")
	}
	if t.closed.Load() {
		return errors.New("restart: that pane is closed")
	}
	if !t.exited.Load() {
		return errors.New("restart: the program in that pane is still running")
	}

	// The old session first and all the way: its reader may still be
	// blocked on it, and two readers would take the bytes in turns.
	old := t.current()
	old.halt()
	err := old.sess.Close()
	old.wg.Wait()
	if err != nil {
		return fmt.Errorf("restart: close the session that ended: %w", err)
	}
	// Input typed at the program that has gone. A new shell would run
	// half a line of it at its own prompt.
	t.drain()

	t.exited.Store(false)
	t.revive()
	// A pane that is alive again has nothing to answer.
	t.ask = nil

	r := t.adopt(sess)
	// The pane may have been given other room while it was dead, which
	// the emulator did not take because there was nothing to take it for.
	t.setSize(t.restartSize())
	t.begin(r)
	return nil
}

// restartSize is the size a restarted terminal takes: the room the
// layout has for it, unless somebody watching holds the size.
func (t *Terminal) restartSize() ui.Size {
	if t.held || !t.haveSize {
		return t.size
	}
	return t.box
}

// drain throws away input queued for a session that has gone.
func (t *Terminal) drain() {
	for {
		select {
		case <-t.out:
		default:
			return
		}
	}
}

// Exited reports whether the shell has gone.
func (t *Terminal) Exited() bool { return t.exited.Load() }

// Dirty reports whether there is new output to draw.
func (t *Terminal) Dirty() bool { return t.pending.Load() }

// Say writes a line of the host's own onto the screen, as though the
// program had printed it, so it lands in the transcript the user then
// scrolls back through.
//
// It is for what the host has to tell the user about the pane itself.
// Nobody watching is told: a watcher reads the program, not this
// window's remarks about it.
func (t *Terminal) Say(line string) {
	t.mu.Lock()
	_, _ = t.term.Write([]byte("\r\n" + line + "\r\n"))
	// Counted like anything else that moved the screen, so a reader
	// holding the last one knows to take it again.
	t.said.Add(1)
	t.mu.Unlock()
	t.pending.Store(true)
}

// Layout resizes the emulator and the session to match the area, unless
// somebody else has the size or the program has gone.
func (t *Terminal) Layout(size ui.Size) {
	t.box = size
	if t.held {
		// The size belongs to somebody watching. The screen keeps the
		// size they asked for and is drawn in whatever room this window
		// has for it, which is what lets their screen be the right shape
		// while this one still shows it.
		return
	}
	t.resize(size)
}

// resize gives the terminal a size, whoever decided it.
//
// A terminal whose program has gone keeps the size it was written at,
// because reflowing its screen at another width cuts the scrollback
// rather than moving it.
func (t *Terminal) resize(size ui.Size) {
	if t.exited.Load() {
		return
	}
	if t.haveSize && t.size == size {
		return
	}
	t.setSize(size)
}

// setSize resizes the emulator and the session whether the size has
// changed or not, which is how a restart tells a new session a size the
// pane has had all along.
func (t *Terminal) setSize(size ui.Size) {
	cols, rows := max(size.Cols, 1), max(size.Rows, 1)
	t.size, t.haveSize = size, true

	t.mu.Lock()
	t.g.Resize(cols, rows)
	t.term.Resize(cols, rows)
	t.mu.Unlock()
	t.pending.Store(true)

	// A session that has already gone cannot be resized, and saying so
	// on every window drag would be noise.
	if err := t.current().sess.Resize(cols, rows); err != nil && !t.exited.Load() {
		t.fail(lateResize(err))
	}
}

// lateResize words a resize failure as the late news it is: a remote
// session sends the size on a goroutine, so what comes back here failed
// on an earlier drag.
func lateResize(err error) error {
	return fmt.Errorf("an earlier resize of this pane failed: %w", err)
}

// lateFailures is a session that reports a failure landing after the
// call that caused it has returned. A remote session's window-change is
// one: it is sent on a goroutine so the drag is not held up.
type lateFailures interface {
	ReportLate(func(error))
}

// Draw paints the terminal, or blanks its room when the host is drawing
// the screen elsewhere.
func (t *Terminal) Draw(v grid.View) {
	if t.elsewhere {
		// Blank rather than nothing: the screen drawn elsewhere need not
		// cover the whole of this room, and what is left of it would keep
		// whatever was painted there before.
		v.Clear()
		return
	}
	t.draw(v)
}

// DrawScreen paints the whole screen onto a view of its own, for a host
// drawing this terminal somewhere other than where the layout put it.
func (t *Terminal) DrawScreen(v grid.View) { t.draw(v) }

// SetElsewhere says the host paints this terminal's screen itself, so
// the room the layout gave it is left blank.
func (t *Terminal) SetElsewhere(on bool) { t.elsewhere = on }

// Elsewhere reports whether the host is painting the screen rather than
// the layout.
func (t *Terminal) Elsewhere() bool { return t.elsewhere }

// draw copies the emulator's cells into a view, bounded by its size.
func (t *Terminal) draw(v grid.View) {
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
	//
	// And none at all once the program has gone, or a pane that swallows
	// every keystroke would look like a shell sitting at a prompt.
	if t.focused && !t.exited.Load() {
		v.SetCursor(t.g.Cursor())
	}
	// Last, over the screen: the question is the window talking, not a
	// line the program printed.
	t.paintAsk(v)
}

// Hold gives the size to somebody watching from another machine.
//
// A terminal's size normally comes from the layout around it, and while
// it is held it does not: the watcher's screen is the right shape, and
// this window draws what fits in the room it has. Release gives it
// back.
//
// It is the difference between looking over somebody's shoulder and
// taking a machine over. The first must not resize a screen somebody
// may be sitting in front of; the second is the case where nobody is.
func (t *Terminal) Hold(cols, rows int) {
	t.held = true
	t.resize(ui.Size{Cols: max(cols, 1), Rows: max(rows, 1)})
}

// Release gives the size back to the layout.
func (t *Terminal) Release() {
	if !t.held {
		return
	}
	t.held = false
	t.resize(t.box)
}

// Held reports whether somebody else has this terminal's size.
func (t *Terminal) Held() bool { return t.held }

// Box is the room the layout has for this terminal, which differs from
// its size only while the size is held.
func (t *Terminal) Box() ui.Size {
	if !t.haveSize {
		return t.size
	}
	return t.box
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

// EncodeKey is the bytes a key press puts into the program running
// here, encoded for the modes it has asked for.
//
// It is how something outside this window presses a key without
// touching the view: HandleKey jumps back to the live screen and drops
// the selection, which is right for somebody typing here and wrong for
// an agent working in the pane.
func (t *Terminal) EncodeKey(ev input.Event) []byte {
	return input.EncodeMode(ev, t.mode(), nil)
}

// HandleKey encodes a key for the program and sends it, or answers the
// question on the pane's last row while one is up.
func (t *Terminal) HandleKey(ev input.Event) (bool, error) {
	if t.ask != nil {
		return t.askKey(ev)
	}
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
	// Before the program, which may have taken the mouse over: the
	// question is about the program and has to be answerable.
	if took, err := t.askMouse(ev); took {
		return true, err
	}
	mode, onAlt := t.mouseMode()
	if mode.Enabled() && !ev.Mods.Has(input.ModShift) {
		if t.reportable(ev) {
			t.send(input.EncodeMouse(ev, mode, nil))
		}
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

// reportable reports whether ev belongs to a gesture the program was
// told about, remembering a press and forgetting its release. It is what
// holds back the drag and the release that follow a press a container
// kept to move the keys here.
//
// Motion with no button held belongs to no gesture, so it is always
// reported.
func (t *Terminal) reportable(ev input.MouseEvent) bool {
	if ev.Button == input.MouseNone || ev.Button.IsWheel() {
		return true
	}
	bit := uint8(1) << ev.Button
	switch ev.Kind {
	case input.MousePress:
		t.reported |= bit
		return true
	case input.MouseRelease:
		was := t.reported&bit != 0
		t.reported &^= bit
		return was
	}
	return t.reported&bit != 0
}

// FocusesFirst says a press that moves the keys to this pane does
// nothing else, so the press that starts a selection is the next one.
func (t *Terminal) FocusesFirst() bool { return true }

// CancelGesture lets go of a drag whose release will never arrive,
// because a dialog opened over the terminal or its pane left the screen.
// Left alone, the next time the pointer crossed the terminal with no
// button down it would carry on extending the selection.
func (t *Terminal) CancelGesture() { t.selecting, t.reported = false, 0 }

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

// writeLoop moves queued bytes into this run's session until the run
// ends, which abandons whatever is still queued. Whatever ends a run
// hangs its session up in the same breath, so those bytes had nowhere to
// go.
func (t *Terminal) writeLoop(r *run) {
	defer r.wg.Done()
	for {
		select {
		case <-r.stop:
			return
		case b := <-t.out:
			if _, err := r.sess.Write(b); err != nil {
				t.fail(fmt.Errorf("write session: %w", err))
				t.finish()
				return
			}
		}
	}
}

// readLoop copies this run's session output into the emulator until it
// ends.
func (t *Terminal) readLoop(r *run) {
	defer r.wg.Done()
	buf := make([]byte, readChunk)
	for {
		n, err := r.sess.Read(buf)
		if n > 0 {
			t.mu.Lock()
			_, _ = t.term.Write(buf[:n])
			// And to anyone watching from another machine, who is
			// shown the same bytes rather than a second rendering of
			// them: what they see is then what is on this screen.
			// Still under the lock, so a chunk cannot be handed on
			// after a screen that was taken once it was parsed.
			t.tell(buf[:n])
			// Counted under the lock, so a reader that takes the lock
			// sees the screen and the count from the same moment.
			t.said.Add(1)
			t.mu.Unlock()
			t.pending.Store(true)
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
