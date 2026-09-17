package term

import (
	"errors"
	"strings"
	"sync"

	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/vt"
)

// Watcher is somebody else looking at this terminal.
//
// It is how a window taken over from another machine shows a pane that
// is already running there. The pane keeps running here: the bytes are
// copied to the watcher rather than handed over, so the screen on this
// machine stays right and is still right when the watcher goes.
//
// Screen is the whole screen as it stands. Anything the watcher has
// queued and not passed on is older than it and must be thrown away.
// Write is what the program said next, in order.
//
// Both are called with a lock held that the terminal needs back before
// it can draw anything, so neither may block -- a queue with somewhere
// to put the bytes, not a socket to write them down.
//
// Ended is called once, when the program has gone. A watcher that was
// never told would wait for output from a shell that has exited.
type Watcher interface {
	Screen(p []byte) error
	Write(p []byte) (int, error)
	Ended()
}

// ErrEnded says the program in a terminal has gone, so there is nothing
// left to watch.
var ErrEnded = errors.New("that program has finished")

// Watch shows this terminal to somebody else and returns what stops it.
//
// The watcher is given the screen as it stands and then what the
// program says next, with nothing lost in between and nothing shown
// twice. That ordering is the whole of this function: taken separately,
// a chunk can arrive before the screen it is already part of and be
// painted over by it, leaving a screen that is wrong until the program
// happens to redraw -- which a shell sitting at a prompt never does.
//
// It fails when the program has already gone, rather than handing back
// a watch on a dead screen that would end the moment it was read.
func (t *Terminal) Watch(w Watcher) (func(), error) {
	// The emulator's lock stops the reader parsing anything new, and
	// the watchers' lock stops it handing anything on. Both are held
	// across the snapshot and the append, so what the watcher is given
	// is the screen at one moment and its stream starts from there.
	t.mu.Lock()
	screen := t.liveScreen()

	t.watchMu.Lock()
	if t.ended {
		t.watchMu.Unlock()
		t.mu.Unlock()
		return nil, ErrEnded
	}
	t.watchers = append(t.watchers, w)
	err := w.Screen([]byte(screen))
	t.watchMu.Unlock()
	t.mu.Unlock()

	if err != nil {
		t.Unwatch(w)
		return nil, err
	}
	var once sync.Once
	return func() { once.Do(func() { t.Unwatch(w) }) }, nil
}

// Resync gives a watcher the screen again, for one that fell behind and
// had what it missed thrown away.
//
// Under the same locks as Watch, so the screen and the throwing away
// happen together: taken separately, the chunks that arrived in between
// would be handed on after the screen that already contains them.
func (t *Terminal) Resync(w Watcher) error {
	t.mu.Lock()
	screen := t.liveScreen()

	t.watchMu.Lock()
	defer t.mu.Unlock()
	defer t.watchMu.Unlock()
	if t.ended {
		return ErrEnded
	}
	return w.Screen([]byte(screen))
}

// liveScreen is the escape sequences that would draw this terminal's
// live screen. The emulator's lock is already held.
//
// The live screen, not the view: somebody at this machine may have
// scrolled back into history, and what a watcher wants is the screen
// that the next output will land on.
func (t *Terminal) liveScreen() string {
	cols, rows := t.g.Size()
	g := grid.New(cols, rows, t.g.DefaultFG, t.g.DefaultBG)
	t.term.RenderLive(g)
	full := t.term.Screenful()
	if full.Alt {
		// And the screen the full-screen program is covering, so that
		// the watcher has something to go back to when it quits.
		under := grid.New(cols, rows, t.g.DefaultFG, t.g.DefaultBG)
		if t.term.RenderUnder(under) {
			full.Under = under
		}
	}
	return vt.Repaint(g, full)
}

// Unwatch stops showing this terminal to somebody.
func (t *Terminal) Unwatch(w Watcher) {
	t.watchMu.Lock()
	defer t.watchMu.Unlock()
	t.forget(w)
}

// forget takes a watcher off the list. The lock is already held.
func (t *Terminal) forget(w Watcher) {
	for i, have := range t.watchers {
		if have != w {
			continue
		}
		copy(t.watchers[i:], t.watchers[i+1:])
		t.watchers[len(t.watchers)-1] = nil
		t.watchers = t.watchers[:len(t.watchers)-1]
		return
	}
}

// Watched reports how many are looking at this terminal from elsewhere.
func (t *Terminal) Watched() int {
	t.watchMu.Lock()
	defer t.watchMu.Unlock()
	return len(t.watchers)
}

// tell passes what the program said to whoever is watching.
//
// Under the same lock Watch appends with, and called with the
// emulator's lock still held, so a watcher is never handed a chunk that
// the screen it was given already contained and never misses one that
// it did not.
func (t *Terminal) tell(b []byte) {
	t.watchMu.Lock()
	defer t.watchMu.Unlock()
	for _, w := range append([]Watcher(nil), t.watchers...) {
		if _, err := w.Write(b); err != nil {
			// A connection that has gone. Nothing here can do anything
			// about it, and the window is told the client left by the
			// thing that carries the client.
			t.forget(w)
		}
	}
}

// endWatchers tells everyone watching that the program has gone.
//
// Without it a watcher waits for output from a shell that has exited:
// its pane sits on a dead screen with nothing to say why, and the
// session carrying it is never closed.
func (t *Terminal) endWatchers() {
	t.watchMu.Lock()
	t.ended = true
	watching := t.watchers
	t.watchers = nil
	t.watchMu.Unlock()
	for _, w := range watching {
		w.Ended()
	}
}

// revive lets watchers attach again, after a restart has put another
// program in the pane.
//
// Nobody is carried across: the watchers this terminal had were told the
// program had gone and dropped when it did. One arriving now is given
// the screen as it stands, with the old transcript above where the new
// program is writing.
func (t *Terminal) revive() {
	t.watchMu.Lock()
	t.ended = false
	t.watchMu.Unlock()
}

// Text is the live screen as plain text: one line per row, trailing
// spaces cut, nothing else.
//
// It is what somebody reads off the screen, which is what an agent
// working in this pane is given. Not the escape sequences that would
// draw it: those are for another terminal, and this is for a reader.
func (t *Terminal) Text() string { return t.TextLines(0) }

// TextLines is the last n lines as plain text, ending at the bottom of
// the live screen and reaching back into the scrollback when n is more
// than the screen holds.
//
// Zero or less is the screen. More lines than there are gives what there
// is.
func (t *Terminal) TextLines(n int) string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.textLinesLocked(n)
}

// Reading is a pane as it stood at one moment.
type Reading struct {
	// Text is the last lines that were asked for, as plain text.
	Text string

	// Row and Col are where the cursor was on the screen, counted from
	// zero at the top left, and Alt says a full-screen program was
	// drawing.
	Row, Col int
	Alt      bool

	// Said is how many times the program had said anything by then.
	Said uint64

	// Cmd is what the shell's own marks said about the command line: a
	// shell that sends none leaves it zero.
	Cmd vt.Command

	// Line names the line the cursor was on, counted from the first line
	// the screen ever had. It does not change as the screen scrolls, so
	// a caller that wrote one down can tell the cursor has moved past
	// it. It is the primary screen's count, and means nothing while Alt.
	Line uint64

	// Before is the text on the cursor's row in front of the cursor. For
	// a shell that marks nothing, it is the prompt the user or an agent
	// is typing at.
	Before string
}

// ReadLines is the last n lines of the pane, where the cursor is, what
// the shell said about the command line and how much the program has
// said, all from one moment. Zero or less is the screen.
//
// One lock for all of it, because a screen from one moment and a cursor
// from another describe a pane that never existed.
func (t *Terminal) ReadLines(n int) Reading {
	t.mu.Lock()
	defer t.mu.Unlock()
	text := t.textLinesLocked(n)
	scr := t.term.Screen()
	col, row := scr.CursorPos()
	return Reading{
		Text:   text,
		Row:    row,
		Col:    col,
		Alt:    scr.OnAltBuffer(),
		Said:   t.said.Load(),
		Cmd:    t.term.Command(),
		Line:   scr.LineNumber(row),
		Before: t.beforeCursorLocked(row, col),
	}
}

// beforeCursorLocked is the text on a row up to a column, with trailing
// spaces cut. The emulator's lock is already held.
func (t *Terminal) beforeCursorLocked(row, col int) string {
	cols, rows := t.g.Size()
	if row < 0 || row >= rows || col <= 0 {
		return ""
	}
	g := grid.New(cols, rows, t.g.DefaultFG, t.g.DefaultBG)
	t.term.RenderLive(g)
	return strings.TrimRight(plainRow(g, row, min(col, cols)), " ")
}

// textLinesLocked is TextLines with the emulator's lock already held.
func (t *Terminal) textLinesLocked(n int) string {
	cols, rows := t.g.Size()
	if rows <= 0 {
		// A terminal with no rows has nothing to read, and the walk back
		// through history below would step by nothing and never end.
		return ""
	}
	history := t.term.History()
	if n <= 0 {
		n = rows
	}
	n = min(n, rows+history)

	// One grid, rendered once per screenful going back through history,
	// each row put where it belongs in the answer. first is the line
	// wanted at the top, counted from the oldest line still kept.
	g := grid.New(cols, rows, t.g.DefaultFG, t.g.DefaultBG)
	out := make([]string, n)
	first := history + rows - n
	for back := n - rows; ; back -= rows {
		back = max(back, 0)
		t.term.RenderBack(g, back)
		for y := 0; y < rows; y++ {
			if i := history - back + y - first; i >= 0 && i < n {
				out[i] = plainRow(g, y, cols)
			}
		}
		if back == 0 {
			break
		}
	}
	return strings.Join(out, "\n")
}

// ViewOffset is how far back into the scrollback the user has scrolled,
// in lines, and zero when they are looking at the live screen.
func (t *Terminal) ViewOffset() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.term.Screen().ViewOffset()
}

// Cursor is where the cursor is on the live screen, counted from zero
// at the top left, and whether a full-screen program is drawing.
func (t *Terminal) Cursor() (row, col int, alt bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	scr := t.term.Screen()
	col, row = scr.CursorPos()
	return row, col, scr.OnAltBuffer()
}

// plainRow is one row of a grid as plain text, trailing spaces cut.
func plainRow(g *grid.Grid, y, cols int) string {
	var line strings.Builder
	for x := 0; x < cols; x++ {
		c := g.At(x, y)
		if c.Width == 0 {
			// The second half of a double-width character, already
			// written by the first.
			continue
		}
		if c.Rune == 0 {
			line.WriteByte(' ')
		} else {
			line.WriteRune(c.Rune)
		}
		for _, cb := range c.Comb {
			line.WriteRune(cb)
		}
	}
	return strings.TrimRight(line.String(), " ")
}

// Said counts how many times the program has said anything.
//
// It is how something watching from outside knows a screen has moved
// without comparing it: output that redraws the same picture is still
// the program working.
func (t *Terminal) Said() uint64 { return t.said.Load() }

// Send puts input into the terminal as though it had been typed here.
//
// It is how somebody watching from another machine types into a pane
// that is running on this one.
func (t *Terminal) Send(b []byte) { t.send(b) }
