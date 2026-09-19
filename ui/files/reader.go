package files

import (
	"bufio"
	"errors"
	"fmt"
	"image/color"
	"io"
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
	r.busy = true
	// Asked before the read goes out rather than after it comes back: a
	// file that grew in between would have moved the end out from under
	// the question.
	r.stuck = r.follow && r.AtEnd()
	r.Read(func(lines []string, cut bool, err error) {
		r.busy = false
		r.err = err
		if err != nil {
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
func (r *Reader) Failed(err error) { r.err = err }

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
	counted := &counter{from: io.LimitReader(rc, MostReadBytes+1)}
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
}

func (c *counter) Read(p []byte) (int, error) {
	n, err := c.from.Read(p)
	c.read += int64(n)
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

// Layout takes the room the reader has.
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
	return ReaderKeys()
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
	switch {
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
	case ev.Key == input.KeyR && ev.Ctrl():
		r.Open()
	case ev.Key == input.KeyF && ev.Ctrl():
		r.Follow(!r.follow)
	case ev.Key == input.KeyH && ev.Ctrl():
		r.Hex(!r.hex)
	case ev.Key == input.KeyD && ev.Ctrl(), ev.Key == input.KeyQ:
		if r.OnClose != nil {
			r.OnClose()
		}
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

// HandleMouse scrolls with the wheel and runs a key from the bar.
//
// The wheel is the first thing anybody tries in a pager, and a widget
// that takes no mouse gets none of it.
func (r *Reader) HandleMouse(ev input.MouseEvent) (bool, error) {
	if ev.Kind != input.MousePress {
		// A move or a release over a reader is still the reader's: it
		// covers its pane, and a drag that started here has nowhere
		// else to go.
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
	}
	return true, nil
}

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
		return r.pictureNote()
	}
	if len(r.shown) == 0 {
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

// howMany is the number of lines the reader holds, with a mark when
// there is more of the file than that.
func (r *Reader) howMany() string {
	if r.cut {
		return fmt.Sprintf("%d+", len(r.shown))
	}
	return fmt.Sprintf("%d", len(r.shown))
}
