package files

import (
	"bufio"
	"errors"
	"fmt"
	"image/color"
	"io"
	"slices"
	"strings"

	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/input"
	"github.com/marrasen/gridterm/ui"
	"github.com/marrasen/gridterm/vfs"
)

// MostReadBytes is the most of a file a Reader holds.
//
// A reader that read whatever it was pointed at would hand a log of a
// hundred megabytes to the garbage collector, and the user would wait
// for every byte of it before seeing the first line. What is past this
// is not read, and the reader says so on the bar rather than pretending
// the file ends there.
const MostReadBytes = 8 << 20

// plainCtrl reports whether ctrl and nothing else is held, which is
// what the keys on the bar are spelled with.
func plainCtrl(ev input.Event) bool { return ev.Mods == input.ModCtrl }

// cannotSave is what Ctrl+S says in a reader with nowhere to write.
// A file on screen is already a file, so there is nothing to save.
const cannotSave = "this is a file already, so there is nothing to save"

// cannotCut is what Ctrl+X says. There is nothing to cut from a file
// being read, and a key that goes quiet reads as a broken one.
const cannotCut = "there is nothing to cut from a file you are reading"

// MostReadLine is the longest line a reader keeps whole. Past it the
// line is cut and the rest dropped, because a file with no newlines in
// it is one line as long as the file.
const MostReadLine = 64 << 10

// Reader shows a file, a screenful at a time.
//
// It is a widget, and everything it shows comes from what it was last
// given: reading happens elsewhere, so a reader whose machine has gone
// still draws what it had and the reason it has no more.
type Reader struct {
	Style Style

	// OnClose is called when the user asks to put the reader away. A nil
	// one leaves the key doing nothing: closing a pane is the window's
	// business, not this package's.
	OnClose func()

	// OnCopy is called with the selected text when the user copies. A
	// nil one leaves the key doing nothing: the clipboard is the
	// window's, not this package's.
	OnCopy func(text string)

	// Read is how the file is fetched. It runs the work somewhere else
	// and calls back with what it found, on the goroutine that draws.
	//
	// A nil one leaves the reader empty: there is nothing that can read
	// for it.
	Read func(then func(lines []string, cut bool, err error))

	// ReadPic is the same for a file the reader shows as a picture. The
	// two are separate because a picture is decoded rather than split
	// into lines.
	ReadPic func(then func(pic Pic, err error))

	// OnSave writes what the reader is showing somewhere the user
	// named. A nil one leaves the key off the bar, which is what a
	// reader on a file that is already saved wants.
	//
	// It is the caller's because a reader reaches no filesystem: it is
	// given lines and shows them. It runs the write somewhere that is
	// not the goroutine that draws -- a path on a share can take
	// seconds -- and calls then with how it went, back on the drawing
	// goroutine.
	OnSave func(at string, lines []string, then func(error))

	// SaveAs is what the save question starts filled in with, so the
	// user edits a path rather than typing one.
	SaveAs string

	// Scrolls are the commands the window scrolls a pane with. A chord
	// bound to one of them is one this reader takes for itself, so
	// shift and a page key picks text out here rather than scrolling.
	//
	// The ids are the window's, not this package's: a widget should
	// not have to know what its window calls things.
	Scrolls []string

	// Expect is how many bytes the file was listed as, for the line
	// shown while it is being read. Zero means nobody said, and the
	// reader then says it is reading without saying how much.
	Expect int64

	name string
	at   string

	// lines is the file split at its newlines, cut is whether any of it
	// was left out, and err is why the read failed.
	lines []string
	cut   bool
	err   error

	// isPic says the file is shown as a picture, and pic is the picture
	// once it has been read.
	isPic bool
	pic   Pic

	// shown is what is drawn: the lines themselves, or the bytes laid
	// out when hex is on. Built from lines rather than read again, so
	// turning hex on costs one pass over what is already held.
	shown []string
	hex   bool

	// colour turns a line into the stretches it is drawn in, picked from
	// what the file is called. A nil one leaves the file plain.
	colour colourer

	// runs and ends are where a line's stretches are worked out: once a
	// line a frame, into the same slices each time.
	runs []run
	ends []int

	// top is the first line drawn and left the first column, both in
	// what is shown rather than in the file.
	top  int
	left int

	// wide is the longest line in columns, and wideOf how many lines it
	// was worked out over, so it is worked out again when they change.
	wide   int
	wideOf int

	// sofar is how many bytes of the file have arrived, while a read is
	// out. It goes back to nothing when one starts.
	sofar int64

	// busy says a read is out and has not come back, and focused whether
	// the keys are here.
	busy    bool
	focused bool

	// asking is what the reader is waiting to be told along the bottom
	// row, and typed what has been typed so far. finding is the last
	// thing searched for, and said is a word to the user in place of the
	// bar: that there is nothing more to find, or that what was typed
	// was not a line number.
	asking  asking
	typed   string
	finding string
	said    string

	// found is the line the last match was on, and -1 when nothing has
	// been found. Stepping from the match rather than from the top of
	// the pane is what makes "n" move between two matches on one
	// screenful.
	found int

	// follow says the reader keeps up with a file that is being written
	// to, the way tail -f does, and stuck says it was at the end when
	// the last read went out so the next answer should stay there.
	follow bool
	stuck  bool

	// sel is what is picked out, in the file's own lines and columns so
	// it stays where it is while the view scrolls. selecting is true
	// between a press and its release.
	sel       span
	selecting bool

	size ui.Size
}

// NewReader returns a reader for one file, showing nothing until Open is
// called.
func NewReader(name, at string) *Reader {
	return &Reader{name: name, at: at, isPic: IsPicture(name), colour: colourerFor(name), found: -1}
}

// Name is what the file is called, for a row that has to name it.
func (r *Reader) Name() string { return r.name }

// Path is where the file is.
func (r *Reader) Path() string { return r.at }

// Lines is how many lines the reader holds.
func (r *Reader) Lines() int { return len(r.shown) }

// Top is the first line shown and Left the first column, both counting
// from zero.
func (r *Reader) Top() int  { return r.top }
func (r *Reader) Left() int { return r.left }

// Busy reports that a read is out.
func (r *Reader) Busy() bool { return r.busy }

// ReadSoFar says how many bytes of the file have arrived, for the line
// shown while a read is out.
//
// It is called on the goroutine that draws, by whoever is running the
// read, and no more often than a frame: the number is only there to be
// looked at. A read that has finished takes no more of them.
func (r *Reader) ReadSoFar(n int64) {
	if !r.busy {
		return
	}
	r.sofar = max(n, 0)
}

// SoFar is how many bytes of the file have arrived, and zero when no
// read is out or nobody is counting.
func (r *Reader) SoFar() int64 { return r.sofar }

// Err is why the last read failed, and nil when it did not.
func (r *Reader) Err() error { return r.err }

// Cut reports that the file is longer than the reader holds.
func (r *Reader) Cut() bool { return r.cut }

// Open reads the file again and reports whether a read went out.
//
// What is shown is kept until the answer arrives, so the pane does not
// blink empty on a reread. A read while one is already out is dropped,
// and says so: a caller that had written down what it was about to read
// has to know it did not.
func (r *Reader) Open() bool {
	if r.busy {
		return false
	}
	if r.isPic {
		if r.ReadPic == nil {
			return false
		}
		r.openPicture()
		return true
	}
	if r.Read == nil {
		return false
	}
	r.busy, r.sofar = true, 0
	// Asked before the read goes out rather than after it comes back: a
	// file that grew in between would have moved the end out from under
	// the question.
	r.stuck = r.follow && r.AtEnd()
	r.Read(func(lines []string, cut bool, err error) {
		// The count goes with the read it belonged to, so nothing is
		// left holding how far a read that has finished got.
		r.busy, r.sofar = false, 0
		r.err = err
		if err != nil {
			// The lines on screen are not the file any more, so what was
			// picked out of them is not either.
			r.sel = span{}
			return
		}
		r.lines, r.cut = lines, cut
		r.remake()
		if r.stuck {
			// Following, and the end is where it was left, so the new
			// lines are what the user is looking at.
			r.End()
			return
		}
		r.clampTop()
	})
	return true
}

// Failed says something went wrong with the file away from a read: the
// question of whether it has changed, for one. The reader shows it in
// place of the file.
func (r *Reader) Failed(err error) {
	r.err = err
	r.sel = span{}
}

// ReadFile reads a file into lines, stopping at MostReadBytes.
//
// It reports whether any of the file was left out: because there was
// more of it than MostReadBytes, or because a line was longer than
// MostReadLine and the rest of that line was dropped. Either way the
// reader says so rather than showing what it got as the whole file.
//
// The caller runs it somewhere that is not the goroutine that draws: a
// file on another machine comes down a connection.
func ReadFile(f vfs.FS, path string) (lines []string, cut bool, err error) {
	return ReadFileWatched(f, path, nil)
}

// ReadFileWatched is ReadFile with somebody counting.
//
// watch is called with the bytes that have arrived so far, from the
// goroutine doing the reading, as often as the reads come back. A file
// on a machine at the far end takes long enough that a pane saying
// nothing looks stuck, and this is what it says instead.
//
// It is called from the reading goroutine, so it must not touch
// anything the drawing goroutine owns. Whoever passes it is expected to
// hand the number on and to do that no more often than a frame.
func ReadFileWatched(f vfs.FS, path string, watch func(read int64)) (lines []string, cut bool, err error) {
	rc, err := f.Open(path)
	if err != nil {
		return nil, false, err
	}
	defer func() {
		// The close matters: a file left open on a machine at the far
		// end holds a handle there for as long as the window lives.
		err = errors.Join(err, rc.Close())
	}()

	// One byte past the limit is allowed on purpose: reading exactly the
	// limit cannot tell a file that fits from one that does not. What
	// arrives is counted rather than what is kept, because what is kept
	// is already cut down to the lines that fitted.
	counted := &counter{from: io.LimitReader(rc, MostReadBytes+1), watch: watch}
	in := bufio.NewReaderSize(counted, 64<<10)
	for {
		line, clipped, err := readLine(in)
		if clipped {
			cut = true
		}
		if err != nil {
			if !errors.Is(err, io.EOF) {
				return nil, false, err
			}
			// The last line of a file that does not end in a newline.
			// A file that ends in one gives an empty string here, and
			// that is not a line: it is what is after the last one.
			if line != "" {
				lines = append(lines, line)
			}
			return lines, cut || counted.read > MostReadBytes, nil
		}
		lines = append(lines, line)
	}
}

// counter counts what it has passed on, so ReadFile can tell a file that
// fits from one that was stopped at the limit.
type counter struct {
	from io.Reader
	read int64

	// watch is told the running total, for a pane saying how far a read
	// has got. A nil one is nobody counting.
	watch func(read int64)
}

func (c *counter) Read(p []byte) (int, error) {
	n, err := c.from.Read(p)
	c.read += int64(n)
	if c.watch != nil && n > 0 {
		c.watch(c.read)
	}
	return n, err
}

// readLine reads one line without its ending, and reports whether the
// line was longer than MostReadLine and had its tail dropped.
//
// A file with no newlines in it is one line as long as the file, and
// keeping that would hold the whole thing in memory twice over.
func readLine(in *bufio.Reader) (line string, clipped bool, err error) {
	var b strings.Builder
	for {
		chunk, more, err := in.ReadLine()
		if room := MostReadLine - b.Len(); len(chunk) > 0 {
			if len(chunk) > room {
				chunk, clipped = chunk[:max(room, 0)], true
			}
			b.Write(chunk)
		}
		if err != nil {
			return b.String(), clipped, err
		}
		if !more {
			return b.String(), clipped, nil
		}
	}
}

// Size is the room the reader was last given.
func (r *Reader) Size() ui.Size { return r.size }

// The switcher draws a picture of a reader at the size it says it has,
// so a method here with the wrong shape would leave its tile empty.
var _ ui.Sized = (*Reader)(nil)

// Layout tells the reader how much room it has.
//
// A shorter pane moves the end of the file away from where the reader is
// sitting, so a file being followed is put back on its end.
func (r *Reader) Layout(size ui.Size) {
	stuck := r.stuck
	r.size = size
	r.clampTop()
	if stuck {
		r.End()
	}
}

// rows is how many lines of the file are shown, which is the room less
// the line naming the file and the bar of keys.
func (r *Reader) rows() int { return max(r.size.Rows-readerChrome, 0) }

// readerChrome is every row that is not a line of the file: the name at
// the top and the bar of keys at the bottom.
const readerChrome = 2

// clampTop holds the first line and the first column shown inside the
// file. A file that is read again shorter would otherwise leave the pane
// scrolled past the end of every line it now has, showing nothing.
func (r *Reader) clampTop() {
	r.top = max(min(r.top, r.lastTop()), 0)
	r.left = max(min(r.left, r.lastLeft()), 0)
}

// lastLeft is the furthest the file scrolls across: the end of the
// longest line at the right edge.
func (r *Reader) lastLeft() int { return max(r.widest()-r.size.Cols, 0) }

// lastTop is the furthest the file scrolls: the last screenful, so the
// end of a file sits at the bottom of the pane rather than at the top
// with nothing under it.
func (r *Reader) lastTop() int {
	// A pane with no room for a line has nowhere to scroll to: there is
	// no screenful, so the first line is the only place to be.
	return max(len(r.shown)-max(r.rows(), 1), 0)
}

// Scroll moves n lines down the file, negative for up.
//
// Scrolling back off the end of a file being followed leaves the reader
// where the user put it. The next answer does not drag them to the
// bottom again: they scrolled back to read something.
func (r *Reader) Scroll(n int) {
	r.top += n
	r.clampTop()
	r.stuck = r.follow && r.AtEnd()
}

// ScrollPages moves n screenfuls down the file, negative for up.
func (r *Reader) ScrollPages(n int) { r.Scroll(n * max(r.rows(), 1)) }

// Home goes to the first line, and End to the last screenful. Both
// settle whether the reader is at the end, the same as scrolling does:
// a file being followed sticks to the end only while the user is there.
func (r *Reader) Home() {
	r.top, r.left = 0, 0
	r.stuck = false
}

func (r *Reader) End() {
	r.top = r.lastTop()
	r.clampTop()
	r.stuck = r.follow
}

// AtEnd reports that the last line of the file is on screen, which is
// what a reader following a file has to stay at.
func (r *Reader) AtEnd() bool { return r.top >= r.lastTop() }

// Follow sets whether the reader keeps up with a file being written to.
//
// A reader that is following and is at the end of the file stays there
// as the file grows. One the user has scrolled back through stays where
// they put it: following is about the end moving, not about taking the
// pane away from whoever is reading it.
func (r *Reader) Follow(on bool) {
	if r.isPic {
		// A picture is not appended to, so there is nothing to follow.
		// Without this, tailing one from the browser leaves a pane
		// labelled "(following)" for good, asking a machine at the far
		// end about the file three times a second.
		return
	}
	r.follow = on
	if on {
		r.End()
		return
	}
	r.stuck = false
}

// Following reports whether the reader is keeping up with the file.
func (r *Reader) Following() bool { return r.follow }

// Sideways moves n columns across, for a line wider than the pane.
//
// It stops with the end of the longest line at the right edge. Without
// that, holding the key walks past every line and leaves the pane blank,
// with nothing on screen saying how far across it went.
func (r *Reader) Sideways(n int) {
	r.left = max(min(r.left+n, r.lastLeft()), 0)
}

// widest is the longest line the reader holds, in columns, and is worked
// out once per set of lines rather than once per key.
func (r *Reader) widest() int {
	if r.wideOf == len(r.shown) {
		return r.wide
	}
	n := 0
	for _, line := range r.shown {
		n = max(n, grid.StringWidth(line))
	}
	r.wide, r.wideOf = n, len(r.shown)
	return n
}

// keys is what the bar offers for this file.
func (r *Reader) keys() []Key {
	if r.isPic {
		return PictureKeys()
	}
	keys := ReaderKeys()
	if r.Selected() {
		keys = append(keys, CopyKey())
	}
	if r.OnSave != nil {
		keys = append(keys, SaveKey())
	}
	return keys
}

// CopyKey is the key that copies the selection. It is on the bar only
// while there is something to copy.
func CopyKey() Key {
	return Key{Chord: chord(input.KeyC, input.ModCtrl), Shown: "^C", Title: "Copy"}
}

// SaveKey is the key that writes what the reader is showing to a file.
// It is on the bar only for a reader that has somewhere to write.
func SaveKey() Key {
	return Key{Chord: chord(input.KeyS, input.ModCtrl), Shown: "^S", Title: "Save"}
}

// ReaderKeys is what the bar offers for a file of lines.
func ReaderKeys() []Key {
	return []Key{
		{Chord: chord(input.KeyHome, 0), Shown: "Home", Title: "Top"},
		{Chord: chord(input.KeyEnd, 0), Shown: "End", Title: "Bottom"},
		{Chord: chord(input.KeyR, input.ModCtrl), Shown: "^R", Title: "Reread"},
		{Chord: chord(input.KeyF, input.ModCtrl), Shown: "^F", Title: "Follow"},
		{Typed: '/', Shown: "/", Title: "Find"},
		{Chord: chord(input.KeyH, input.ModCtrl), Shown: "^H", Title: "Hex"},
		{Typed: ':', Shown: ":", Title: "Line"},
		{Chord: chord(input.KeyD, input.ModCtrl), Shown: "^D", Title: "Close"},
	}
}

// ClaimsChord takes shift and a page key, so a screenful can be picked
// out with the keyboard the way a line is.
//
// The window scrolls the pane in front on those two, and an accelerator
// runs before any widget sees the key. A user who has just learned
// Shift+Down tries Shift+PageDown next, and the pane scrolling under a
// selection that stayed where it was is not what they asked for.
func (r *Reader) ClaimsChord(ev input.Event, bound string) bool {
	if r.isPic || r.asking != askingNothing || !r.picking() {
		return false
	}
	if ev.Mods != input.ModShift {
		return false
	}
	if ev.Key != input.KeyPageUp && ev.Key != input.KeyPageDown {
		return false
	}
	// Only from the commands that scroll. A user who has pointed this
	// chord at something else meant that, and a shortcut that worked
	// everywhere but in a file viewer would be a puzzle.
	return bound == "" || slices.Contains(r.Scrolls, bound)
}

// HandleKey moves through the file.
func (r *Reader) HandleKey(ev input.Event) (bool, error) {
	if r.isPic {
		return r.pictureKey(ev)
	}
	if r.asking != askingNothing {
		return r.askKey(ev)
	}
	// The keys that start a question, which arrive as text rather than
	// as a key: the character is what names them, not where it sits on
	// the keyboard.
	if ev.Kind == input.Text && ev.NormalText {
		switch ev.Rune {
		case '/':
			r.ask(askingFind)
			return true, nil
		case ':':
			r.ask(askingGoTo)
			return true, nil
		case 'n':
			r.FindNext(false)
			return true, nil
		case 'N':
			r.FindNext(true)
			return true, nil
		}
	}
	if ev.Kind != input.KeyPress && ev.Kind != input.KeyRepeat {
		return false, nil
	}
	// Whatever was said last is said once: the next key is the user
	// having read it.
	r.said = ""
	// A chord the bar never offered is not the bar's key. Ctrl+Shift+H
	// is not Ctrl+H: the window opens its help on that one, and a
	// reader that turned hex on underneath would be acting on a key
	// pressed for something else.
	//
	// Ctrl and shift together is allowed, because that is how a
	// selection is carried by word and to the ends of a file
	// everywhere else. Only the keys that move take it, below: the
	// bar's own letters still want a plain Ctrl.
	switch ev.Mods {
	case 0, input.ModCtrl, input.ModShift, input.ModCtrl | input.ModShift:
	default:
		return true, nil
	}
	switch {
	// Shift and a key that moves takes the loose end of the selection
	// with it, which is how text is picked out without a mouse.
	case ev.Shift() && ev.Key == input.KeyUp:
		r.extend(-1, 0)
	case ev.Shift() && ev.Key == input.KeyDown:
		r.extend(1, 0)
	case ev.Shift() && ev.Key == input.KeyLeft:
		r.extend(0, -1)
	case ev.Shift() && ev.Key == input.KeyRight:
		r.extend(0, 1)
	case ev.Shift() && ev.Key == input.KeyPageUp:
		r.extendPages(-1)
	case ev.Shift() && ev.Key == input.KeyPageDown:
		r.extendPages(1)
	case ev.Shift() && ev.Key == input.KeyHome:
		r.extendTo(0)
	case ev.Shift() && ev.Key == input.KeyEnd:
		r.extendTo(-1)
	case ev.Key == input.KeyA && plainCtrl(ev):
		r.SelectAll()
	case ev.Key == input.KeyC && plainCtrl(ev):
		r.Copy()
	case ev.Key == input.KeyEscape:
		r.ClearSelection()
	case ev.Key == input.KeyUp:
		r.Scroll(-1)
	case ev.Key == input.KeyDown:
		r.Scroll(1)
	case ev.Key == input.KeyLeft:
		r.Sideways(-1)
	case ev.Key == input.KeyRight:
		r.Sideways(1)
	case ev.Key == input.KeyPageUp:
		r.ScrollPages(-1)
	case ev.Key == input.KeyPageDown, ev.Key == input.KeySpace:
		r.ScrollPages(1)
	case ev.Key == input.KeyHome:
		r.Home()
	case ev.Key == input.KeyEnd:
		r.End()
	case ev.Key == input.KeyR && plainCtrl(ev):
		r.Open()
	case ev.Key == input.KeyF && plainCtrl(ev):
		r.Follow(!r.follow)
	case ev.Key == input.KeyH && plainCtrl(ev):
		r.Hex(!r.hex)
	case ev.Key == input.KeyD && plainCtrl(ev), ev.Key == input.KeyQ:
		if r.OnClose != nil {
			r.OnClose()
		}
	case ev.Key == input.KeyS && plainCtrl(ev):
		if r.OnSave == nil {
			r.said = cannotSave
			break
		}
		r.ask(askingSave)
	case ev.Key == input.KeyX && plainCtrl(ev):
		// Ctrl+C copies here, so Ctrl+X is the next thing a hand tries.
		// Saying why beats a key that looks broken.
		r.said = cannotCut
	default:
		// Every other key is swallowed all the same: a reader is not a
		// terminal, and a letter typed into one must not reach the shell
		// behind it.
		return true, nil
	}
	return true, nil
}

// readerWheel is how many lines one turn of the wheel moves, which is
// what a list moves by: the two are read the same way.
const readerWheel = 3

// HandleMouse picks text out, scrolls with the wheel and runs a key from
// the bar.
//
// The wheel is the first thing anybody tries in a pager, and a widget
// that takes no mouse gets none of it.
func (r *Reader) HandleMouse(ev input.MouseEvent) (bool, error) {
	switch ev.Kind {
	case input.MouseMove:
		if r.selecting {
			r.sel.to = r.spotAt(ev.Col, ev.Row)
			r.sel.on = true
			r.settle()
		}
		// A move or a release over a reader is still the reader's: it
		// covers its pane, and a drag that started here has nowhere
		// else to go.
		return true, nil
	case input.MouseRelease:
		if r.selecting && ev.Button == input.MouseLeft {
			r.selecting = false
			// A click that never moved is a click, not one column left
			// highlighted.
			if r.sel.from == r.sel.to {
				r.sel = span{}
			}
			r.settle()
		}
		return true, nil
	case input.MousePress:
	default:
		return true, nil
	}
	switch ev.Button {
	case input.MouseWheelUp:
		r.Scroll(-readerWheel)
		return true, nil
	case input.MouseWheelDown:
		r.Scroll(readerWheel)
		return true, nil
	case input.MouseLeft:
	default:
		return true, nil
	}
	// The bar along the bottom: a click on a key does what the key does.
	if rows := r.size.Rows; rows > 1 && ev.Row == rows-1 {
		keys := r.keys()
		if i, ok := keyAt(ev.Col, r.size.Cols, len(keys)); ok {
			return r.HandleKey(keys[i].press())
		}
		return true, nil
	}
	if !r.picking() || !r.inBody(ev.Row) {
		return true, nil
	}
	// A press in the file starts picking text out, and one with shift
	// held carries on from what is already picked.
	at := r.spotAt(ev.Col, ev.Row)
	if ev.Mods.Has(input.ModShift) && r.sel.on {
		r.sel.to = at
		r.settle()
	} else {
		r.sel = span{from: at, to: at}
	}
	r.selecting = true
	return true, nil
}

// FocusesFirst says a press that moves the keys to this reader does
// nothing else, so the press that starts a selection is the next one.
func (r *Reader) FocusesFirst() bool { return true }

// CancelGesture says the release that would end a drag is never coming.
// Left alone, the next time the pointer crossed the reader with no
// button down it would carry on picking text out.
func (r *Reader) CancelGesture() { r.selecting = false }

// SetFocus takes or gives up the keys, and Focused says which it is. A
// reader draws its bar differently without them, so the pane says
// whether the keys are here rather than looking the same either way.
func (r *Reader) SetFocus(on bool) { r.focused = on }

// Focused reports whether the keys are here.
func (r *Reader) Focused() bool { return r.focused }

// Draw paints the name, the lines and the bar.
func (r *Reader) Draw(v grid.View) {
	cols, rows := v.Size()
	if cols <= 0 || rows <= 0 {
		return
	}
	v.Fill(grid.Cell{Rune: ' ', FG: r.Style.FG, BG: r.Style.BG, Width: 1})

	// The name at the top, with where in the file this is at the end of
	// it, so a reader says both without a second row.
	head := r.name
	if r.hex && !r.isPic {
		head += " (hex)"
	}
	if r.follow {
		head += " (following)"
	}
	if r.busy {
		head += " …"
	}
	v.SetString(0, 0, grid.TrimTail(head, cols), r.Style.HeaderFG, r.Style.BG, 0)
	if note := r.place(); note != "" && grid.StringWidth(note) < cols {
		v.SetString(cols-grid.StringWidth(note), 0, note, r.Style.NoteFG, r.Style.BG, 0)
	}

	switch {
	case r.err != nil:
		v.SetString(0, 1, grid.TrimTail(r.err.Error(), cols), r.Style.ErrorFG, r.Style.BG, 0)
	case r.isPic:
		// The picture goes on a layer over the pane, so the body is left
		// as it is: a picture is pixels, and the grid is for text.
	default:
		r.paintLines(v, cols, rows)
	}
	switch {
	case rows <= 1:
	case r.asking != askingNothing:
		r.paintAsking(v, rows-1, cols)
	case r.said != "":
		v.SetString(0, rows-1, grid.TrimTail(r.said, cols), r.Style.ErrorFG, r.Style.BG, 0)
	default:
		drawKeys(v, rows-1, cols, r.keys(), r.Style, func(Key) bool { return r.focused })
	}
}

// paintLines writes the screenful the reader is on.
func (r *Reader) paintLines(v grid.View, cols, rows int) {
	for y := 0; y < rows-readerChrome; y++ {
		i := r.top + y
		if i >= len(r.shown) {
			break
		}
		line := r.shown[i]
		at, wide, found := r.findsOn(line)
		r.paintLine(v, y+1, line, cols)
		if r.sel.on {
			r.markSelected(v, y+1, i, cols)
		}
		if r.left > 0 {
			at -= r.left
		}
		if !found {
			continue
		}
		// What was searched for, marked out where it falls. Written over
		// the line rather than in place of it, so a match part way off
		// the left edge still marks the part that is on screen.
		for x := max(at, 0); x < min(at+wide, cols); x++ {
			c := v.At(x, y+1)
			c.FG, c.BG = r.Style.MarkedFG, r.Style.SelectedBG
			v.Set(x, y+1, c)
		}
	}
}

// paintLine writes one line, in the stretches the file's own colouring
// gives it. A hex dump is bytes rather than a language, so it is drawn
// plain.
func (r *Reader) paintLine(v grid.View, y int, line string, cols int) {
	if r.colour == nil || r.hex {
		r.paintPlain(v, y, line, cols)
		return
	}
	r.runs = r.snap(line, r.colour(line, r.runs))
	if len(r.runs) == 0 {
		r.paintPlain(v, y, line, cols)
		return
	}
	// Stretch by stretch, each at the column it has in the whole line,
	// so the colours land where the plain line's characters would.
	at, x := 0, -r.left
	for _, piece := range r.runs {
		end := min(piece.end, len(line))
		text := line[at:end]
		at = end
		width := grid.StringWidth(text)
		if x+width > 0 && x < cols {
			r.paintPiece(v, y, x, cols, text, piece)
		}
		x += width
		if x >= cols {
			return
		}
	}
}

// snap moves the colouring's boundaries onto grapheme clusters, so a
// combining mark keeps the cell of the character it belongs to.
//
// Only for a line with a character outside ASCII in it, because that is
// the only kind that has a cluster longer than one byte.
func (r *Reader) snap(line string, runs []run) []run {
	if len(runs) < 2 || !hasHighByte(line) {
		return runs
	}
	r.ends = r.ends[:0]
	for _, piece := range runs {
		r.ends = append(r.ends, piece.end)
	}
	grid.SnapToClusters(line, r.ends)

	// A stretch whose end moved onto the same cluster as the one before
	// it now covers nothing, and is dropped.
	out, last := runs[:0], 0
	for i, piece := range runs {
		if r.ends[i] <= last {
			continue
		}
		piece.end, last = r.ends[i], r.ends[i]
		out = append(out, piece)
	}
	return out
}

// hasHighByte reports whether a line holds a byte outside ASCII.
func hasHighByte(line string) bool {
	for i := 0; i < len(line); i++ {
		if line[i] >= 0x80 {
			return true
		}
	}
	return false
}

// paintPlain writes a line in the reader's own colour.
func (r *Reader) paintPlain(v grid.View, y int, line string, cols int) {
	x := 0
	if r.left > 0 {
		var cut int
		line, cut = grid.CutLeft(line, r.left)
		// A double-width character straddling the left edge goes whole,
		// and the column it half filled is left blank.
		x = cut - r.left
	}
	v.SetString(x, y, grid.TrimTail(line, cols-x), r.Style.FG, r.Style.BG, 0)
}

// paintPiece writes one stretch, cutting what is off either edge.
func (r *Reader) paintPiece(v grid.View, y, x, cols int, text string, piece run) {
	if x < 0 {
		var cut int
		text, cut = grid.CutLeft(text, -x)
		x += cut
	}
	v.SetString(x, y, grid.TrimTail(text, cols-x), r.colourOf(piece.col), r.Style.BG, piece.attr)
}

// colourOf turns a colouring's name for a stretch into a colour from the
// reader's own style.
func (r *Reader) colourOf(c colour) color.RGBA {
	switch c {
	case colourNote:
		return r.Style.NoteFG
	case colourText:
		return r.Style.LinkFG
	case colourMark:
		return r.Style.MarkedFG
	}
	return r.Style.FG
}

// Gone says what the reader was reading is no longer there, without
// taking away what it has.
//
// A scrollback viewer whose pane has closed still holds text worth
// reading, and rereading it is what would find nothing. The name says
// so and Ctrl+R stops asking.
func (r *Reader) Gone(why string) {
	r.Read, r.ReadPic = nil, nil
	if why != "" && !strings.HasSuffix(r.name, ")") {
		r.name += " (" + why + ")"
	}
}

// Where is what the top line says about where in the file the reader is,
// or how big the picture is, for a row elsewhere that has to say the
// same thing.
func (r *Reader) Where() string { return r.place() }

// place is what the top line says about where in the file this is: the
// lines on screen out of the whole, and whether there is more of the
// file than was read.
func (r *Reader) place() string {
	if r.err != nil {
		return ""
	}
	if r.isPic {
		if note := r.pictureNote(); note != "" {
			return note
		}
		return r.reading()
	}
	if len(r.shown) == 0 {
		// Nothing to show yet is not the same as nothing to show. A
		// file of a few megabytes down an SSH connection takes long
		// enough that "empty" reads as the answer rather than as the
		// question still being asked.
		if r.busy {
			return r.reading()
		}
		return "empty"
	}
	if r.rows() <= 0 {
		// No room for a line, so there is no range to give. The count
		// is still worth saying: it is all that fits.
		return r.howMany()
	}
	last := min(r.top+r.rows(), len(r.shown))
	return fmt.Sprintf("%d-%d of %s", r.top+1, last, r.howMany())
}

// reading is what the top line says while the file is being read and
// there is nothing to show yet, with how big it is when the caller
// said.
func (r *Reader) reading() string {
	switch {
	case !r.busy:
		return ""
	// How far of how much, which is the one that says how long is left.
	// A file that grew since it was listed reads past its own size, so
	// the total is dropped rather than shown as less than what has
	// already arrived.
	case r.sofar > 0 && r.Expect >= r.sofar:
		return "reading " + sizeIn(r.sofar, r.Expect) + " of " + size(r.Expect) + "…"
	case r.sofar > 0:
		return "reading " + size(r.sofar) + "…"
	case r.Expect > 0:
		return "reading " + size(r.Expect) + "…"
	}
	return "reading…"
}

// howMany is the number of lines the reader holds, with a mark when
// there is more of the file than that.
func (r *Reader) howMany() string {
	if r.cut {
		return fmt.Sprintf("%d+", len(r.shown))
	}
	return fmt.Sprintf("%d", len(r.shown))
}
