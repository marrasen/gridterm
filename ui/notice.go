package ui

import (
	"image/color"
	"strings"

	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/input"
)

// How large a notice is allowed to get, and how much room it leaves
// around itself.
const (
	noticeMaxCols = 76
	noticeMargin  = 2

	// noticePad is the blank column each side of the text inside the box.
	noticePad = 2

	// noticeWheel is how many rows one notch of the wheel moves.
	noticeWheel = 3
)

// Where each part of a notice sits in its box, counted from the top.
const (
	noticeTitleRow = 1
	noticeTextTop  = 3

	// noticeChrome is every row that is not message: a blank row, the
	// title and a blank row above; a blank row, the buttons and a blank
	// row below.
	noticeChrome = 6
)

// How tall a notice may get before the message scrolls instead, as a
// fraction of the window.
const (
	noticeCeilingOf = 4
	noticeCeilingIn = 5
)

// The buttons along the bottom, left to right. OK is the one the focus
// starts on, so Enter puts the dialog away.
const (
	noticeCopy = iota
	noticeOK
)

var noticeButtons = []string{"Copy", "OK"}

// NoticeStyle colours a notice.
type NoticeStyle struct {
	// FG and BG are the message and the box behind it. A background with
	// no alpha lets a frosted panel show through.
	FG, BG color.RGBA

	// TitleFG is the name at the top, and FailureFG the name at the top
	// of a notice that reports a failure.
	TitleFG, FailureFG color.RGBA

	// SelectionFG and SelectionBG mark the text the user has dragged
	// over.
	SelectionFG, SelectionBG color.RGBA

	// ButtonFG and ButtonBG are a button, and ActiveFG and ActiveBG the
	// one Enter would press.
	ButtonFG, ButtonBG color.RGBA
	ActiveFG, ActiveBG color.RGBA

	// BorderFG is the rule around the outside, and ShadowBG darkens the
	// cells it falls on below and to the right. A zero alpha leaves
	// either one out.
	BorderFG color.RGBA
	ShadowBG color.RGBA
}

// noticeAt is a place in the wrapped message: which line, and how many
// grapheme clusters into it.
type noticeAt struct{ line, cluster int }

// before reports whether a comes earlier in the message than b.
func (a noticeAt) before(b noticeAt) bool {
	return a.line < b.line || (a.line == b.line && a.cluster < b.cluster)
}

// noticeLine is one line of the wrapped message and what stood between
// it and the next line in the message it was wrapped from.
type noticeLine struct {
	text string

	// join is a space where the line was broken between two words,
	// nothing where a word was cut in half, and a line break where the
	// message had one of its own.
	join string
}

// Notice is a dialog that shows a message whole, wrapping and scrolling
// it rather than cutting it off, and letting it be dragged over and
// copied.
type Notice struct {
	Style NoticeStyle

	// Title names the dialog. Failure draws that title in the error
	// colour, for a notice that reports something going wrong.
	Title   string
	Failure bool

	// Copy puts text on the clipboard. A nil one leaves the Copy button
	// with nothing to do: this package cannot reach a clipboard.
	Copy func(string)

	// CopyChord reports whether a key is the window's copy chord, which
	// the notice then answers itself. A nil one leaves that key swallowed
	// like any other: this package cannot see a keymap.
	CopyChord func(input.Event) bool

	message string

	// lines is the message wrapped to wrappedAt columns, and top is the
	// first of them on screen.
	lines     []noticeLine
	wrappedAt int
	top       int

	// wanted is the width the message asks for, and wantedFor the title
	// it was worked out with. Kept, because box asks for it several times
	// a frame.
	wanted    int
	wantedFor string

	// anchor and cursor are the ends of the selection and are both
	// inside it; active says there is one, and holding that the mouse is
	// still dragging it out.
	anchor, cursor  noticeAt
	active, holding bool

	at    int // which button has the focus
	size  Size
	close func()
	buf   buffer
}

// NewNotice returns a dialog showing a message. close is called when the
// dialog is finished with, which is what takes it off the modal stack.
func NewNotice(title, message string, close func()) *Notice {
	return &Notice{Title: title, message: cleanText(message), at: noticeOK, close: close}
}

// SetClose says what takes the dialog away, for a caller that only has
// something to hand back once the dialog exists.
func (n *Notice) SetClose(close func()) { n.close = close }

// Message returns the message in full, unwrapped.
func (n *Notice) Message() string { return n.message }

// SetMessage replaces the message, back at the top with nothing
// selected.
func (n *Notice) SetMessage(s string) {
	n.message = cleanText(s)
	n.lines, n.wrappedAt = nil, 0
	n.wanted = 0
	n.top = 0
	n.active, n.holding = false, false
}

// Selection returns the text the user has dragged over as the message
// wrote it rather than as the box wrapped it, and "" when there is no
// selection.
func (n *Notice) Selection() string {
	if !n.active {
		return ""
	}
	lines := n.wrap()
	from, to := n.span()
	if from.line >= len(lines) {
		return ""
	}
	to.line = min(to.line, len(lines)-1)
	var b strings.Builder
	for li := from.line; li <= to.line; li++ {
		cs := grid.Clusters(lines[li].text)
		lo, hi := 0, len(cs)-1
		if li == from.line {
			lo = from.cluster
		}
		if li == to.line {
			hi = min(hi, to.cluster)
		}
		if li > from.line {
			b.WriteString(lines[li-1].join)
		}
		for i := max(lo, 0); i <= hi; i++ {
			b.WriteString(cs[i])
		}
	}
	return b.String()
}

// CopyText returns what the copy chord puts on the clipboard: the
// selection, or the whole message when nothing is selected.
func (n *Notice) CopyText() string {
	if s := n.Selection(); s != "" {
		return s
	}
	return n.message
}

// CopyNow copies what CopyText returns.
func (n *Notice) CopyNow() { n.put(n.CopyText()) }

// put hands text to whatever holds the clipboard.
func (n *Notice) put(s string) {
	if n.Copy != nil && s != "" {
		n.Copy(s)
	}
}

// Box returns where the dialog sits in the view it draws through, so
// whatever is showing it can treat that part differently.
func (n *Notice) Box() Rect { return n.box() }

// Layout notes how much room the dialog has to place itself in.
func (n *Notice) Layout(size Size) {
	n.size = size
	n.clampTop()
}

// Draw paints the box over whatever is behind it.
func (n *Notice) Draw(v grid.View) { n.buf.draw(v, n.paint) }

// HandleKey scrolls the message, moves between the buttons and presses
// one.
//
// Every other key is swallowed, the way a modal does (see KeyHandler).
// The copy chord is the exception: the notice answers that itself.
func (n *Notice) HandleKey(ev input.Event) (bool, error) {
	if ev.Kind != input.KeyPress && ev.Kind != input.KeyRepeat {
		return true, nil
	}
	if n.CopyChord != nil && n.CopyChord(ev) {
		n.CopyNow()
		return true, nil
	}
	// A dialog with no room to be drawn takes nothing but Escape: every
	// other key would act on a message that is not on screen.
	if n.box().Empty() {
		if ev.Key == input.KeyEscape && isPlainKey(ev) {
			n.dismiss()
		}
		return true, nil
	}
	if !isPlainKey(ev) {
		return true, nil
	}

	switch ev.Key {
	case input.KeyEscape:
		n.dismiss()
	case input.KeyUp:
		n.scrollBy(-1)
	case input.KeyDown:
		n.scrollBy(1)
	case input.KeyPageUp:
		n.scrollBy(-max(n.textRows()-1, 1))
	case input.KeyPageDown:
		n.scrollBy(max(n.textRows()-1, 1))
	case input.KeyHome:
		n.top = 0
		n.clampTop()
	case input.KeyEnd:
		n.top = len(n.wrap())
		n.clampTop()
	case input.KeyTab:
		if ev.Mods == input.ModShift {
			n.move(-1)
		} else {
			n.move(1)
		}
	case input.KeyLeft:
		n.move(-1)
	case input.KeyRight:
		n.move(1)
	case input.KeyEnter, input.KeySpace:
		// Only a fresh press. A dialog opens from the pump and input is
		// polled in the same frame, so the repeats of the key that
		// opened it would otherwise answer it before it is read.
		if ev.Kind == input.KeyPress {
			n.press(n.at)
		}
	}
	return true, nil
}

// HandleMouse scrolls on the wheel, presses a button, and drags a
// selection out of the message.
func (n *Notice) HandleMouse(ev input.MouseEvent) (bool, error) {
	box := n.box()
	if box.Empty() {
		return true, nil
	}
	if ev.Button.IsWheel() {
		if ev.Kind == input.MousePress {
			if ev.Button == input.MouseWheelUp {
				n.scrollBy(-noticeWheel)
			} else {
				n.scrollBy(noticeWheel)
			}
		}
		return true, nil
	}
	if ev.Button != input.MouseLeft && ev.Kind != input.MouseMove {
		// Everything else is swallowed: the dialog covers the whole
		// area, and letting a press through would act on a pane the user
		// cannot see.
		return true, nil
	}

	x, y := box.Local(ev.Col, ev.Row)
	switch ev.Kind {
	case input.MousePress:
		if !box.Contains(ev.Col, ev.Row) {
			n.dismiss()
			return true, nil
		}
		if y == box.Rows-2 {
			if at, ok := buttonAtCol(noticeButtons, box.Cols, noticePad, x); ok {
				n.at = at
				n.press(at)
				return true, nil
			}
		}
		if at, ok := n.pointAt(x, y, false); ok {
			n.anchor, n.cursor = at, at
			n.active, n.holding = true, true
		}
	case input.MouseMove:
		if !n.holding {
			return true, nil
		}
		// Clamped, so dragging past an edge selects to the edge rather
		// than stranding the drag.
		if at, ok := n.pointAt(x, y, true); ok {
			n.cursor = at
		}
	case input.MouseRelease:
		n.holding = false
		// A click that never moved is a click, not a selection of one
		// character.
		if n.anchor == n.cursor {
			n.active = false
		}
	}
	return true, nil
}

// CancelGesture lets go of a drag whose release will never arrive,
// because a dialog opened over this one or it left the screen.
func (n *Notice) CancelGesture() { n.holding = false }

// paint draws the box, the title, the message and the buttons.
func (n *Notice) paint(v grid.View) {
	box := n.box()
	if box.Empty() {
		return
	}
	drawShadow(v, box, n.Style.ShadowBG)
	in := box.In(v)
	in.Fill(grid.Cell{Rune: ' ', FG: n.Style.FG, BG: n.Style.BG, Width: 1})
	drawFrame(v, box, n.Style.BorderFG, n.Style.BG)

	title := n.Style.TitleFG
	if n.Failure {
		title = n.Style.FailureFG
	}
	in.SetString(noticePad, noticeTitleRow, grid.Trim(n.Title, box.Cols-noticePad*2),
		title, n.Style.BG, grid.AttrBold)

	n.paintText(in, box)
	n.paintButtons(in, box)
}

// paintText draws the rows of the message that are on screen, with the
// selected part marked out.
func (n *Notice) paintText(in grid.View, box Rect) {
	lines := n.wrap()
	for y := 0; y < max(box.Rows-noticeChrome, 0); y++ {
		li := n.top + y
		if li < 0 || li >= len(lines) {
			continue
		}
		row, x := noticeTextTop+y, noticePad
		// One call per run of the same colour
		run, on := "", false
		flush := func() {
			if run == "" {
				return
			}
			fg, bg := n.Style.FG, n.Style.BG
			if on {
				fg, bg = n.Style.SelectionFG, n.Style.SelectionBG
			}
			x = in.SetString(x, row, run, fg, bg, 0)
			run = ""
		}
		for i, c := range grid.Clusters(lines[li].text) {
			sel := n.selects(li, i)
			if i == 0 {
				on = sel
			}
			if sel != on {
				flush()
				on = sel
			}
			run += c
		}
		flush()
	}
}

// paintButtons draws the buttons along the bottom, right aligned.
func (n *Notice) paintButtons(in grid.View, box Rect) {
	for i, at := range buttonColsIn(noticeButtons, box.Cols, noticePad) {
		if at < 0 {
			// No room for this one. Drawing it would land it on top of
			// the buttons that did fit.
			continue
		}
		fg, bg := n.Style.ButtonFG, n.Style.ButtonBG
		if i == n.at {
			fg, bg = n.Style.ActiveFG, n.Style.ActiveBG
		}
		drawButton(in, at, box.Rows-2, noticeButtons[i], fg, bg)
	}
}

// press runs one of the buttons, leaving the dialog open for Copy so the
// user can go on reading it.
func (n *Notice) press(at int) {
	switch at {
	case noticeCopy:
		n.CopyNow()
	default:
		n.dismiss()
	}
}

// move steps the focus through the buttons.
func (n *Notice) move(by int) {
	count := len(noticeButtons)
	n.at = ((n.at+by)%count + count) % count
}

// dismiss closes the dialog.
func (n *Notice) dismiss() {
	if n.close != nil {
		n.close()
	}
}

// scrollBy moves the message under the box.
func (n *Notice) scrollBy(by int) {
	n.top += by
	n.clampTop()
}

// clampTop keeps what is shown within the message, for a box that has
// changed size or a message that has been replaced.
func (n *Notice) clampTop() {
	rows := n.textRows()
	if rows <= 0 {
		n.top = 0
		return
	}
	n.top = min(max(n.top, 0), max(len(n.wrap())-rows, 0))
}

// textRows is how many rows of the message are on screen.
func (n *Notice) textRows() int { return max(n.box().Rows-noticeChrome, 0) }

// span returns the selection's ends in reading order.
func (n *Notice) span() (from, to noticeAt) {
	from, to = n.anchor, n.cursor
	if to.before(from) {
		from, to = to, from
	}
	return from, to
}

// selects reports whether one cluster of one line is inside the
// selection.
func (n *Notice) selects(line, cluster int) bool {
	if !n.active {
		return false
	}
	from, to := n.span()
	p := noticeAt{line: line, cluster: cluster}
	return !p.before(from) && !to.before(p)
}

// pointAt turns a place in the box into a place in the message, with
// clamp pulling a point outside the text back into it, which is what a
// drag past an edge wants.
func (n *Notice) pointAt(x, y int, clamp bool) (noticeAt, bool) {
	rows, lines := n.textRows(), n.wrap()
	if rows <= 0 || len(lines) == 0 {
		return noticeAt{}, false
	}
	y -= noticeTextTop
	switch {
	case clamp:
		y = min(max(y, 0), rows-1)
	case y < 0 || y >= rows:
		return noticeAt{}, false
	}
	li := n.top + y
	if li >= len(lines) {
		if !clamp {
			return noticeAt{}, false
		}
		li = len(lines) - 1
	}
	return noticeAt{line: li, cluster: clusterAt(lines[li].text, x-noticePad)}, true
}

// clusterAt returns which grapheme cluster of a line covers a column,
// and the last one for a column past the end of it.
func clusterAt(line string, col int) int {
	cs := grid.Clusters(line)
	if col <= 0 || len(cs) == 0 {
		return 0
	}
	width := 0
	for i, c := range cs {
		width += grid.StringWidth(c)
		if col < width {
			return i
		}
	}
	return len(cs) - 1
}

// wrap returns the message broken to the width of the box it is being
// shown in.
func (n *Notice) wrap() []noticeLine { return n.linesAt(n.box().Cols - noticePad*2) }

// linesAt returns the message wrapped to a width, wrapping it again only
// when the width has changed.
func (n *Notice) linesAt(width int) []noticeLine {
	width = max(width, 1)
	if n.lines == nil || n.wrappedAt != width {
		n.lines, n.wrappedAt = wrapText(n.message, width), width
	}
	return n.lines
}

// box returns where the dialog goes, centred in the area it was given,
// and nothing at all when there is no room for one row of the message.
func (n *Notice) box() Rect {
	cols := min(n.size.Cols-noticeMargin*2, max(n.wantCols(), 20))
	if cols < 12 {
		return Rect{}
	}
	rows := min(noticeChrome+len(n.linesAt(cols-noticePad*2)), n.size.Rows-noticeMargin*2)
	// The ceiling never pushes the box below what it needs: a short
	// window still gets a dialog, with one row of the message showing.
	rows = min(rows, max(n.size.Rows*noticeCeilingOf/noticeCeilingIn, noticeChrome+1))
	if rows < noticeChrome+1 {
		return Rect{}
	}
	return Rect{
		X:    (n.size.Cols - cols) / 2,
		Y:    (n.size.Rows - rows) / 2,
		Cols: cols,
		Rows: rows,
	}
}

// wantCols returns how wide the dialog would like to be: enough for the
// longest line of the message as it was written, capped, and never less
// than the buttons need.
func (n *Notice) wantCols() int {
	if n.wanted != 0 && n.wantedFor == n.Title {
		return n.wanted
	}
	width := grid.StringWidth(n.Title)
	for _, para := range strings.Split(n.message, "\n") {
		width = max(width, grid.StringWidth(para))
	}
	buttons := buttonsWidth(noticeButtons)
	n.wanted = max(min(max(width, buttons)+noticePad*2, noticeMaxCols), buttons+noticePad*2)
	n.wantedFor = n.Title
	return n.wanted
}

// wrapText breaks a message into lines no wider than width, keeping the
// line breaks it already has and recording what joined each line to the
// next.
func wrapText(s string, width int) []noticeLine {
	var out []noticeLine
	paras := strings.Split(s, "\n")
	for i, para := range paras {
		lines := wrapOne(para, width)
		if i < len(paras)-1 {
			lines[len(lines)-1].join = "\n"
		}
		out = append(out, lines...)
	}
	return out
}

// wrapOne breaks one line at spaces, cutting a word wider than the box.
func wrapOne(s string, width int) []noticeLine {
	var out []noticeLine
	line := ""
	for _, word := range strings.Fields(s) {
		switch {
		case line == "":
			line = word
		case grid.StringWidth(line)+1+grid.StringWidth(word) <= width:
			line += " " + word
		default:
			// Broken between two words, so the break stands for the
			// space that was there.
			out = append(out, noticeLine{text: line, join: " "})
			line = word
		}
		for grid.StringWidth(line) > width {
			head := grid.Trim(line, width)
			if head == "" {
				// A cluster wider than the whole box. It goes on a line
				// of its own rather than stopping the wrap dead.
				head = grid.Clusters(line)[0]
			}
			// Cut inside a word, so nothing stood between the two
			// halves.
			out = append(out, noticeLine{text: head})
			line = line[len(head):]
		}
	}
	if line != "" || len(out) == 0 {
		out = append(out, noticeLine{text: line})
	}
	return out
}
