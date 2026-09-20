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
	"strings"
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

	// Program is what the terminal calls itself when a program asks with
	// XTVERSION, as a name and a version. Empty answers nothing.
	Program string

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

	// OnExit is called when the shell goes, from the goroutine that
	// noticed, and again once the session has said how the program ended.
	// Ending carries the status from that second call on: a program whose
	// input broke goes on running, and the host is told the pane has
	// stopped rather than waiting for it.
	OnExit func()

	// OnError reports a session failure. There is nowhere to return one
	// from the goroutines moving bytes, and a terminal whose pipe has
	// broken will not recover, so the host is told and decides. A nil
	// one drops the error, which is the caller's choice to make.
	OnError func(error)

	// OnLink is called with the address of a hyperlink the user asked
	// to follow. A nil one leaves the link unfollowable: opening one
	// is the window's business, not this package's.
	OnLink func(string)

	// FindPath asks whether text under the pointer names something on
	// the machine this pane is on, with dir the directory the shell
	// last said it was in. It answers the path as it would be opened
	// and whether it is a directory.
	//
	// The window's, because only the window can reach a filesystem.
	// It is what makes finding a path safe where finding an address
	// is a guess: a run of characters that names nothing is not a
	// link, and the disk is what says so.
	FindPath func(text, dir string) (at string, isDir, ok bool)

	// OnPath is called to open what FindPath found: a directory in
	// the file browser and a file in the viewer, at the line the
	// output named when it named one.
	OnPath func(at string, isDir bool, line int)

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

	// secret is the ask the pane is waiting on, and is nil when nobody is
	// waiting. Its own lock, because the goroutine waiting is not the one
	// that draws.
	secretMu sync.Mutex
	secret   *secretAsk

	// end is what the session reported when the program stopped, and nil
	// until it has. Written by whichever goroutine noticed the program
	// go and read by the one that draws.
	end atomic.Pointer[ending]

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

	// pal is the colours the terminal draws in, kept so the window can
	// draw over the screen in them.
	pal vt.Palette

	// caption is the line above the screen, and empty for a pane without
	// one.
	caption string

	// ask is the question drawn on the pane's last row, and nil when
	// there is none. The drawing goroutine owns it.
	ask *asked

	focused bool
	encBuf  []byte

	// selecting is true between a press and its release, so motion is
	// only a drag when a drag started here.
	selecting bool

	// hoverLink is the address the pointer is over while ctrl is held,
	// and hoverRow with hoverFrom and hoverTo are the stretch of the
	// screen it runs across, so it can be underlined. All of it is the
	// drawing goroutine's.
	hoverLink          string
	hoverRow           int
	hoverFrom, hoverTo int

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

	// letGo says the host closed this session itself and kept the pane, so
	// Close and Restart leave it alone rather than handing back the first
	// close's error a second time. The drawing goroutine owns it.
	letGo bool

	// over says the terminal has moved on to another run, so a status
	// landing late belongs to a program the pane no longer shows. It is
	// guarded by the terminal's runMu.
	over bool
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
	t.term.SetProgram(cfg.Program)

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
	if r.letGo {
		// The host closed this session itself and has already been told
		// how that went.
		return nil
	}
	return r.sess.Close()
}

// LetGo stops the goroutine feeding the session, for a host that is
// closing the session itself and keeping the pane.
//
// Nothing is queued for a pane whose program has gone, so that goroutine
// would otherwise sit on its select for the life of the window, holding
// the session and everything behind it.
func (t *Terminal) LetGo() {
	r := t.current()
	r.letGo = true
	r.halt()
}

// retire marks a run as one the terminal has finished with, so a status
// that lands late is not put on the program that took its place.
func (t *Terminal) retire(r *run) {
	t.runMu.Lock()
	r.over = true
	t.runMu.Unlock()
}

// Restart puts a new session under the terminal, keeping what is on the
// screen and in the scrollback, and gives both the room the pane has
// now, which reflows the scrollback and cuts it where the pane has
// narrowed since.
//
// It refuses a terminal whose program is still running, and hands back
// the old session's hangup error rather than starting a second program
// on top of one that would not go. A session the host let go of is left
// alone: the host closed it and has the error already.
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
	t.retire(old)
	var err error
	if !old.letGo {
		err = old.sess.Close()
	}
	old.wg.Wait()
	if err != nil {
		return fmt.Errorf("restart: close the session that ended: %w", err)
	}
	// Input typed at the program that has gone. A new shell would run
	// half a line of it at its own prompt.
	t.drain()

	t.exited.Store(false)
	t.end.Store(nil)
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
	return t.screenBox(t.box)
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

// WaitForSecret says the user has been asked to type something into this
// pane, and gives back a channel that is closed when they have.
//
// The characters are the program's. Nothing here reads them, keeps them
// or passes them on: all this reports is that a line was typed, which is
// what lets something else stop waiting. The line says what is wanted
// and goes into the transcript like anything else the window says.
//
// Stop takes the question away, for a caller that gave up first. Asking
// again replaces the first ask, whose channel is closed as though the
// user had typed: a caller cannot tell the two apart and neither is
// waiting for anything any more.
func (t *Terminal) WaitForSecret(line string) (answered <-chan bool, stop func(), err error) {
	t.secretMu.Lock()
	if t.secret != nil {
		t.secretMu.Unlock()
		return nil, nil, ErrAlreadyAsked
	}
	// Room for one answer, so whoever ends the ask never waits for the
	// goroutine that is waiting on it.
	ask := &secretAsk{done: make(chan bool, 1)}
	t.secret = ask
	t.secretMu.Unlock()
	t.Say(line)
	return ask.done, func() { t.EndSecret(ask) }, nil
}

// ErrAlreadyAsked says the pane is already waiting for the user to type
// something, so a second ask would take the first one's answer.
var ErrAlreadyAsked = errors.New("that pane is already waiting for something to be typed")

// secretAsk is one ask: what the user has typed so far, and where the
// answer goes.
type secretAsk struct {
	done chan bool

	// typed says the user has typed something other than the return
	// itself. A bare return answers nothing: the user pressing Enter on
	// an empty line has told nobody anything.
	typed bool
}

// watchForSecret follows what the user types while the pane is waiting
// for a secret: the characters are the program's, and the return at the
// end of them is what says they have finished.
func (t *Terminal) watchForSecret(ev input.Event) {
	t.secretMu.Lock()
	ask := t.secret
	if ask == nil {
		t.secretMu.Unlock()
		return
	}
	if ev.Kind == input.KeyPress && ev.Key == input.KeyEnter {
		if !ask.typed {
			// A return with nothing before it. The ask stands.
			t.secretMu.Unlock()
			return
		}
		t.secret = nil
		t.secretMu.Unlock()
		ask.done <- true
		return
	}
	// Anything that puts characters in front of the cursor counts as the
	// user typing, whatever they are: nothing here reads them.
	if ev.Kind == input.Text || typing(ev) {
		ask.typed = true
	}
	t.secretMu.Unlock()
}

// typing reports whether a key press puts a character in, as against
// moving the cursor or running a shortcut.
func typing(ev input.Event) bool {
	switch ev.Key {
	case input.KeySpace, input.KeyTab:
		return true
	}
	return false
}

// Pasted says the user pasted into the pane, which answers an ask as
// typing does: a password out of a password manager arrives this way.
func (t *Terminal) pastedSecret(text string) {
	if text == "" {
		return
	}
	t.secretMu.Lock()
	ask := t.secret
	if ask == nil {
		t.secretMu.Unlock()
		return
	}
	ask.typed = true
	// A paste that carries a return is the whole answer, the way typing
	// one is.
	if !strings.ContainsAny(text, "\r\n") {
		t.secretMu.Unlock()
		return
	}
	t.secret = nil
	t.secretMu.Unlock()
	ask.done <- true
}

// EndSecret ends an ask that nobody answered, and says so on the pane so
// that a line asking for a password is not left sitting there with
// nothing behind it.
//
// A later ask is left alone: this ends the one it was given.
func (t *Terminal) EndSecret(which *secretAsk) {
	t.secretMu.Lock()
	if which != nil && t.secret != which {
		t.secretMu.Unlock()
		return
	}
	ask := t.secret
	t.secret = nil
	t.secretMu.Unlock()
	if ask == nil {
		return
	}
	ask.done <- false
	t.Say("-- gridterm: nothing is waiting for that any more. --")
}

// AskedForASecret reports whether the pane is waiting for one to be
// typed.
func (t *Terminal) AskedForASecret() bool {
	t.secretMu.Lock()
	defer t.secretMu.Unlock()
	return t.secret != nil
}

// Layout resizes the emulator and the session to match the area, unless
// somebody else has the size or the program has gone.
func (t *Terminal) Layout(size ui.Size) {
	t.box = size
	size = t.screenBox(size)
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
	if n := t.capRows(); n > 0 {
		cols, rows := v.Size()
		t.paintCaption(v.Sub(0, 0, cols, n))
		t.draw(v.Sub(0, n, cols, rows-n))
		return
	}
	t.draw(v)
}

// Caption is the line above the screen naming the pane, and empty for a
// pane with no line above it. The program gets one row fewer while there
// is one.
func (t *Terminal) Caption() string { return t.caption }

// SetCaption puts a line above the screen, or takes it away.
func (t *Terminal) SetCaption(text string) {
	if text == t.caption {
		return
	}
	t.caption = text
	if t.held {
		// The size belongs to somebody watching, the same reason Layout
		// leaves it alone.
		return
	}
	t.resize(t.screenBox(t.box))
}

// capRows is how many rows the line above the screen takes.
//
// None while the host paints the screen itself, and none unless the
// screen has left a row for it: a size that is somebody else's -- a
// watcher holding it, or a program that has ended and kept the size it
// was written at -- is not the layout's to take a row from, and a line
// over such a screen would cut its bottom row off.
func (t *Terminal) capRows() int {
	if t.caption == "" || t.elsewhere || t.box.Rows < 2 {
		return 0
	}
	if t.size.Rows > t.box.Rows-1 {
		return 0
	}
	return 1
}

// screenBox is the room left for the program once the line above it has
// its row.
//
// From the room rather than from capRows, which asks whether the screen
// has already left a row: this is what leaves it.
func (t *Terminal) screenBox(box ui.Size) ui.Size {
	if t.caption != "" && box.Rows >= 2 {
		box.Rows--
	}
	return box
}

// paintCaption draws the line above the screen.
func (t *Terminal) paintCaption(v grid.View) {
	cols, rows := v.Size()
	if cols <= 0 || rows <= 0 {
		return
	}
	bg := askBG(t.pal)
	v.Fill(grid.Cell{Rune: ' ', FG: t.pal.FG, BG: bg, Width: 1})
	v.SetString(0, 0, grid.TrimTail(t.caption, cols), t.pal.FG, bg, 0)
}

// underlined reports whether a cell is part of the link under the
// pointer.
//
// The run is counted along the joined line, so a link that the
// terminal wrapped is underlined across both rows.
func (t *Terminal) underlined(x, y, cols int) bool {
	if t.hoverLink == "" || cols <= 0 || y < t.hoverRow {
		return false
	}
	at := (y-t.hoverRow)*cols + x
	return at >= t.hoverFrom && at < t.hoverTo
}

// paintLinkTarget writes the address under the pointer along a row of
// the screen, the way a browser writes it along the bottom.
//
// A program can put any address under any words, so "click here" can
// go anywhere. Nothing else in the pane says where a link leads, and
// the user is about to click it.
//
// Along the bottom, unless the link itself is down there, in which
// case along the top: covering the thing being pointed at would be
// worse than moving.
//
// It answers the row it wrote on, or -1, so the cursor can keep off
// it.
func (t *Terminal) paintLinkTarget(v grid.View) int {
	if t.hoverLink == "" {
		return -1
	}
	cols, rows := v.Size()
	if cols <= 0 || rows <= 0 {
		return -1
	}
	y := rows - 1
	if cols > 0 && t.hoverRow+(t.hoverTo-1)/cols >= y {
		// The link reaches the bottom row, which it can across a wrap.
		y = 0
	}
	// Trimmed from the front, so the machine it goes to stays readable
	// when the path is long: that is the half that says where it goes.
	shown := t.hoverLink
	if grid.StringWidth(shown) > cols {
		shown = grid.Trim(shown, cols)
	}
	bg := askBG(t.pal)
	line := v.Sub(0, y, cols, 1)
	line.Fill(grid.Cell{Rune: ' ', FG: t.pal.FG, BG: bg, Width: 1})
	line.SetString(0, 0, shown, t.pal.FG, bg, grid.AttrUnderline)
	return y
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
	for y := range rows {
		for x := range cols {
			c := t.g.At(x, y)
			c.FG, c.BG = t.g.FGOf(x, y), t.g.BGOf(x, y)
			c.Attr &^= grid.AttrReverse
			// The link under the pointer is underlined, so the user
			// can see what a click would follow. Only while ctrl is
			// held, which is the only time a click would follow it.
			if t.underlined(x, y, cols) {
				c.Attr |= grid.AttrUnderline
			}
			v.Set(x, y, c)
		}
	}
	named := t.paintLinkTarget(v)
	// Only the focused terminal touches the cursor. A grid has one and no
	// idea who owns it, so an unfocused widget writing even a hidden
	// cursor would take it from whoever has it. Clearing it once a frame
	// is the container's job.
	//
	// And none at all once the program has gone, or a pane that swallows
	// every keystroke would look like a shell sitting at a prompt.
	if t.focused && !t.exited.Load() {
		cur := t.g.Cursor()
		if cur.Y == named {
			// The address is written over the row the cursor is on, so
			// the cursor would sit in the middle of it.
			cur.Visible = false
		}
		v.SetCursor(cur)
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
	t.resize(t.screenBox(t.box))
}

// Held reports whether somebody else has this terminal's size.
func (t *Terminal) Held() bool { return t.held }

// ScreenRoom is the room the program's screen is drawn in: the pane's
// own room, less the line above it when there is one.
func (t *Terminal) ScreenRoom() ui.Size { return t.screenBox(t.Box()) }

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
	// Something is waiting to hear that the user has typed a secret
	// here. The keys go to the program as they always do; what this
	// watches for is the return at the end of them.
	if ev.Kind == input.KeyPress || ev.Kind == input.Text {
		t.watchForSecret(ev)
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
	// question is about the program and has to be answerable. It is
	// asked in the pane's own rows, so it comes before the line above
	// the screen is taken off them.
	if took, err := t.askMouse(ev); took {
		return true, err
	}
	if n := t.capRows(); n > 0 {
		if ev.Row < n {
			if ev.Kind == input.MousePress {
				// The line above the screen takes the press.
				return true, nil
			}
			// A drag or a release that wandered onto the line belongs to
			// the row under it: swallowing a release leaves the button
			// down in the program and the selection following the
			// pointer with nothing held.
			ev.Row = n
		}
		ev.Row -= n
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
		// Ctrl and a press follows a link rather than starting a
		// selection, which is what every other terminal does.
		if ev.Mods.Has(input.ModCtrl) && t.followLink(ev.Col, ev.Row) {
			return true, nil
		}
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

// LinkAt is the address of the hyperlink under a cell of the screen,
// and empty where there is none.
//
// The row is the screen's own, counted from the top of the program's
// output rather than from the top of the pane.
func (t *Terminal) LinkAt(col, row int) string {
	at, _, _ := t.linkSpanAt(col, row)
	return at
}

// linkSpanAt is the hyperlink under a cell and the columns it runs
// between, the second one past the end.
//
// A program that means a link says so with OSC 8, and that is looked
// at first. Failing that the row is read for something that looks
// like an address, which is a guess and is treated as one: only a
// written-out scheme counts.
func (t *Terminal) linkSpanAt(col, row int) (at string, from, to int) {
	cols, rows := t.g.Size()
	if col < 0 || row < 0 || col >= cols || row >= rows {
		return "", 0, 0
	}
	if id := t.g.At(col, row).Link; id != 0 {
		t.mu.Lock()
		said := t.term.LinkURL(id)
		t.mu.Unlock()
		if said != "" {
			a, b := t.runOfLink(id, col, row)
			return said, a, b
		}
	}
	// Rows the terminal wrapped are one line, so an address split
	// across two is looked at whole. The answer comes back in
	// positions along the joined line, counted from the first row.
	joined, first := t.joinedRow(row)
	pos := (row-first)*cols + col
	if found, a, b, ok := findLink(joined, pos); ok {
		return found, a, b
	}
	// Then something on the disk. After an address, because an
	// address is also a run of characters with no spaces in it.
	if found, _, a, b, ok := t.pathUnder(joined, pos); ok {
		return found, a, b
	}
	return "", 0, 0
}

// pathUnder is the file or directory named under a position along a
// joined line, as the window would open it.
func (t *Terminal) pathUnder(joined []rune, at int) (open string, line, from, to int, ok bool) {
	if t.cfg.FindPath == nil {
		return "", 0, 0, 0, false
	}
	text, line, from, to, found := findPathText(joined, at)
	if !found {
		return "", 0, 0, 0, false
	}
	t.mu.Lock()
	dir, _ := t.term.Dir()
	t.mu.Unlock()
	open, isDir, real := t.cfg.FindPath(text, dir)
	_ = isDir
	if !real {
		return "", 0, 0, 0, false
	}
	return open, line, from, to, true
}

// joinedRow is the whole logical line a row belongs to, one rune per
// column, and the screen row it starts on.
//
// A row the terminal wrapped is exactly as wide as the screen, so a
// position along the joined line divides by the width into a row and
// a column with nothing left over.
func (t *Terminal) joinedRow(row int) (joined []rune, first int) {
	cols, rows := t.g.Size()
	if cols <= 0 {
		return nil, row
	}
	first = row
	for first > 0 && t.g.At(cols-1, first-1).Wrapped {
		first--
	}
	last := row
	for last < rows-1 && t.g.At(cols-1, last).Wrapped {
		last++
	}
	joined = make([]rune, 0, (last-first+1)*cols)
	for y := first; y <= last; y++ {
		joined = append(joined, t.rowRunes(y)...)
	}
	return joined, first
}

// runOfLink is the stretch of one row carrying the same link number,
// which is the text the program put the address under.
func (t *Terminal) runOfLink(id uint32, col, row int) (from, to int) {
	cols, _ := t.g.Size()
	from, to = col, col+1
	for from > 0 && t.g.At(from-1, row).Link == id {
		from--
	}
	for to < cols && t.g.At(to, row).Link == id {
		to++
	}
	return from, to
}

// rowRunes is one row of the screen, one rune per column, so a column
// and an index into it are the same thing.
//
// The second half of a double-width character has no rune of its own
// and comes back as a space, which no address holds: a link is not
// found across one, which is right.
func (t *Terminal) rowRunes(row int) []rune {
	cols, _ := t.g.Size()
	out := make([]rune, cols)
	for x := range cols {
		c := t.g.At(x, row)
		if c.Rune == 0 || c.Width == 0 {
			out[x] = ' '
			continue
		}
		out[x] = c.Rune
	}
	return out
}

// SetHover says where the pointer is over the screen and what is
// held with it, so a link under it can be marked and named.
//
// The row is the screen's own. A row below zero is the pointer being
// somewhere else, which takes the marking off.
func (t *Terminal) SetHover(col, row int, mods input.Mods) {
	at, from, to := "", 0, 0
	if row >= 0 && mods&input.ModCtrl != 0 && (t.cfg.OnLink != nil || t.cfg.OnPath != nil) {
		at, from, to = t.linkSpanAt(col, row)
	}
	// The row the run is counted from is the first of the wrapped line
	// it is on, which is where linkSpanAt counted it from too.
	on := row
	if at != "" {
		_, on = t.joinedRow(row)
	}
	if at == t.hoverLink && on == t.hoverRow && from == t.hoverFrom && to == t.hoverTo {
		return
	}
	t.hoverLink, t.hoverRow, t.hoverFrom, t.hoverTo = at, on, from, to
	t.pending.Store(true)
}

// HoveredLink is the address the pointer is over, and empty when it
// is over none. It is what a window shows so the user can see where
// a link goes before following it.
func (t *Terminal) HoveredLink() string { return t.hoverLink }

// CursorAt is the pointer to draw over a cell: the hand where holding
// ctrl and clicking would follow a link, and the ordinary one
// otherwise.
//
// It is also where the pane learns where the pointer is, because the
// window asks this once a frame: a link under it is marked and named
// from what this records.
//
// Ctrl, because a plain press picks text out and a link the user
// cannot select around would be worse than one they have to hold a
// key for. The same rule VS Code and Windows Terminal use.
func (t *Terminal) CursorAt(col, row int, mods input.Mods) (ui.Cursor, bool) {
	if n := t.capRows(); n > 0 {
		if row < n {
			t.SetHover(col, -1, mods)
			return ui.CursorDefault, false
		}
		row -= n
	}
	t.SetHover(col, row, mods)
	if t.hoverLink == "" {
		return ui.CursorDefault, false
	}
	return ui.CursorPointing, true
}

// followLink opens the link under a cell, and reports whether there
// was one. The press is taken either way: a ctrl press over a pane is
// not the start of a selection.
func (t *Terminal) followLink(col, row int) bool {
	cols, rows := t.g.Size()
	if col < 0 || row < 0 || col >= cols || row >= rows {
		return false
	}
	joined, first := t.joinedRow(row)
	pos := (row-first)*cols + col
	if t.cfg.OnLink != nil {
		if found, _, _, ok := findLinkOrDeclared(t, joined, pos, col, row); ok {
			t.cfg.OnLink(found)
			return true
		}
	}
	if t.cfg.OnPath == nil {
		return false
	}
	open, line, _, _, ok := t.pathUnder(joined, pos)
	if !ok {
		return false
	}
	_, isDir, _ := t.cfg.FindPath(open, "")
	t.cfg.OnPath(open, isDir, line)
	return true
}

// findLinkOrDeclared is the address under a position: the one the
// program declared with OSC 8, or failing that one written out in the
// text.
func findLinkOrDeclared(t *Terminal, joined []rune, at, col, row int) (string, int, int, bool) {
	if id := t.g.At(col, row).Link; id != 0 {
		t.mu.Lock()
		said := t.term.LinkURL(id)
		t.mu.Unlock()
		if said != "" {
			from, to := t.runOfLink(id, col, row)
			return said, from, to, true
		}
	}
	found, a, b, ok := findLink(joined, at)
	return found, a, b, ok
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
	// A password out of a password manager arrives this way, and answers
	// an ask as typing one does.
	t.pastedSecret(text)
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
				t.finish(r)
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
			t.finish(r)
			return
		}
	}
}

// ending is what a session reported when its program stopped. A nil
// error is a program that ended with nothing to report.
type ending struct{ err error }

// Ending is what the session said when the program stopped, and whether
// it has stopped at all.
//
// It is the exit status, for a host that has to say how the program
// ended: an error the caller can unwrap for a status, or nil for a
// program that ended with nothing to report.
func (t *Terminal) Ending() (error, bool) {
	e := t.end.Load()
	if e == nil {
		return nil, false
	}
	return e.err, true
}

// finish records that the shell has gone and tells the host, first that
// it went and then what it ended with.
func (t *Terminal) finish(r *run) {
	if t.exited.Swap(true) {
		return
	}
	// Whoever is watching from elsewhere, before the window is told:
	// their pane is drawing this program and has no other way to learn
	// it has gone.
	t.endWatchers()
	// And anything waiting for the user to type into that program:
	// typing into a pane whose program has gone tells nobody anything.
	if t.AskedForASecret() {
		t.EndSecret(nil)
	}
	t.tellHost()
	go t.collect(r)
}

// collect waits for the program's status and tells the host again once
// there is one.
//
// On a goroutine of its own, because Wait blocks for as long as the
// program runs and a run ends here on a broken input as well as a broken
// output.
func (t *Terminal) collect(r *run) {
	if !t.keepEnding(r, r.sess.Wait()) {
		return
	}
	t.tellHost()
}

// keepEnding records what a run's program ended with and reports whether
// it was kept. A status landing after the pane took another program is
// dropped: it is not that program's.
func (t *Terminal) keepEnding(r *run, err error) bool {
	t.runMu.Lock()
	defer t.runMu.Unlock()
	if r.over || t.run != r {
		return false
	}
	t.end.Store(&ending{err: err})
	return true
}

// tellHost says the program has gone, if the host asked to be told.
func (t *Terminal) tellHost() {
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

// SetPalette gives the terminal the colours it draws in, and moves what
// the program has already printed into the new theme.
func (t *Terminal) SetPalette(pal vt.Palette) {
	t.mu.Lock()
	t.term.Screen().SetPalette(pal)
	t.pal = pal
	t.g.DefaultFG, t.g.DefaultBG = pal.FG, pal.BG
	t.g.SelectionBG = pal.Selection
	t.g.MarkAllDirty()
	t.sendScreen()
	t.mu.Unlock()
	t.pending.Store(true)
}

// PressPaste sends the key a program reads as paste, which is Ctrl+V.
//
// It is for a picture that has been put on the machine's clipboard from
// somewhere else: the program reads that clipboard itself, so what it
// needs is the keystroke rather than any text.
func (t *Terminal) PressPaste() {
	t.send(input.EncodeMode(
		input.Event{Kind: input.KeyPress, Key: input.KeyV, Mods: input.ModCtrl}, t.mode(), nil))
}
