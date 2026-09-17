package vt

import (
	"github.com/marrasen/gridterm/grid"
)

// DefaultScrollback is how many lines of history the primary buffer
// keeps.
const DefaultScrollback = 5000

// cursorState is everything DECSC saves and DECRC restores.
type cursorState struct {
	X, Y int
	Pen  grid.Cell // colours and attributes new cells inherit

	// WrapNext records that the last printable character landed in the
	// final column. The wrap happens when the next one arrives, not
	// immediately — otherwise writing exactly one screen width of text
	// would scroll before anything needed the extra line.
	WrapNext bool

	Origin bool // DECOM: coordinates are relative to the scroll region
}

// modes holds the DEC private and ANSI modes the screen honours.
type modes struct {
	Wrap        bool // DECAWM (7), on by default
	Insert      bool // IRM (4)
	CursorVis   bool // DECTCEM (25)
	AppCursor   bool // DECCKM (1)
	AppKeypad   bool // DECNKM (66)
	Alt         bool // 47 / 1047 / 1049
	Bracketed   bool // 2004
	FocusEvents bool // 1004
	ReverseVid  bool // DECSCNM (5)

	MouseClick  bool // 1000
	MouseDrag   bool // 1002
	MouseMotion bool // 1003
	MouseSGR    bool // 1006
}

// Screen is the terminal's model of what is on the display: two line
// buffers, a cursor, a scroll region and the modes that change how
// writes behave. It knows nothing about escape sequences; Terminal
// drives it.
type Screen struct {
	cols, rows int

	pri, alt *buffer
	cur      *buffer

	cursor cursorState
	// saved holds the DECSC cursor for each buffer. The buffers save
	// separately, which is what makes the 1049 alt-screen switch restore
	// the shell's cursor when a full-screen program exits.
	saved [2]cursorState

	top, bot int // scroll region, inclusive, 0-based

	mode    modes
	tabs    []bool
	palette Palette

	// scrollOff is how many lines back into history the view is.
	scrollOff int

	// gone counts the lines that have left the top of the primary
	// screen, whether history kept them or not. A row's number is this
	// plus the row, which is a name for a line that does not change as
	// the screen scrolls under it. Something marking a place in the
	// output -- where a command's output began, where a clear was --
	// keeps that number.
	//
	// It counts the whole screen scrolling, which is what output does. A
	// program scrolling a region of its own is redrawing, and its rows
	// are not lines of the output.
	gone uint64

	// floor is the line a clear left behind: the top row of the screen
	// when the program last erased the scrollback. The lines above it
	// are still here and the person at this machine still scrolls back
	// to them; something reading the pane from outside starts here.
	floor uint64

	// touched are the rows written since the last Render, and all says
	// the whole screen was. drawnTo is the grid that render went to: any
	// other grid is missing rows this screen no longer knows about, so
	// it gets the lot.
	touched []bool
	all     bool
	drawnTo *grid.Grid

	curStyle grid.CursorStyle
	curBlink bool
}

// NewScreen returns a screen of the given size.
func NewScreen(cols, rows int, pal Palette, scrollback int) *Screen {
	cols = max(cols, 1)
	rows = max(rows, 1)
	s := &Screen{
		cols:    cols,
		rows:    rows,
		palette: pal,
		pri:     newBuffer(cols, rows, scrollback, blankFor(&pal)),
		alt:     newBuffer(cols, rows, 0, blankFor(&pal)),
		top:     0,
		bot:     rows - 1,
	}
	s.cur = s.pri
	s.mode.Wrap = true
	s.mode.CursorVis = true
	// The power-on cursor is a blinking block, which is what xterm and
	// DECSCUSR 0 mean by the default.
	s.curBlink = true
	s.cursor.Pen = s.blank()
	// Restoring a cursor that was never saved must not install the zero
	// Cell as the pen: its colours are transparent black, so every
	// subsequent character would be invisible.
	for i := range s.saved {
		s.saved[i] = s.cursor
	}
	s.resetTabs()
	return s
}

// Size returns the screen dimensions in cells.
func (s *Screen) Size() (cols, rows int) { return s.cols, s.rows }

// blankFor is the empty cell for a palette's defaults. The palette is
// taken by pointer because it is a kilobyte and this is on the path
// every printed character takes.
func blankFor(pal *Palette) grid.Cell {
	return grid.Cell{Rune: ' ', FG: pal.FG, BG: pal.BG, Width: 1}
}

// blank returns an empty cell in the current default colours. New cells
// take the default background rather than the pen's, so clearing with a
// coloured pen does not paint the screen.
func (s *Screen) blank() grid.Cell { return blankFor(&s.palette) }

// eraseCell is what erase operations write. It keeps the pen's
// background, which is how `clear` with a coloured background works,
// but drops the pen's character attributes.
func (s *Screen) eraseCell() grid.Cell {
	return grid.Cell{
		Rune:  ' ',
		FG:    s.cursor.Pen.FG,
		BG:    s.cursor.Pen.BG,
		Width: 1,
	}
}

func (s *Screen) resetTabs() {
	s.tabs = make([]bool, s.cols)
	for i := 8; i < s.cols; i += 8 {
		s.tabs[i] = true
	}
}

// resizeTabs keeps the stops a program has set. Rebuilding the default
// grid instead would silently discard them, and a program that set its
// own columns has no way to know it must set them again.
func (s *Screen) resizeTabs(cols int) {
	next := make([]bool, cols)
	copy(next, s.tabs)
	for i := len(s.tabs); i < cols; i += 1 {
		if i%8 == 0 && i > 0 {
			next[i] = true
		}
	}
	s.tabs = next
}

// Resize changes the screen dimensions. The cursor is clamped into the
// new bounds and the scroll region is reset, which is what xterm does.
func (s *Screen) Resize(cols, rows int) {
	cols = max(cols, 1)
	rows = max(rows, 1)
	if cols == s.cols && rows == s.rows {
		return
	}
	// Lines below the floor are what a clear put behind the screen, and
	// they stay behind it however much taller the window gets.
	priShift := s.pri.resize(cols, rows, s.eraseCell(), s.cursor.Y, int(s.gone-min(s.floor, s.gone)))
	altShift := s.alt.resize(cols, rows, s.eraseCell(), s.cursor.Y, rows)
	shift := priShift
	if s.cur == s.alt {
		shift = altShift
	}

	// Lines revived from history come back onto the screen and lines
	// taken from the top go into it, so the boundary a row number is
	// counted from moves with them.
	s.gone = uint64(max(int64(s.gone)-int64(priShift), 0))

	s.cols, s.rows = cols, rows
	s.top, s.bot = 0, rows-1
	// The cursor has to travel with the text it was sitting on. Lines
	// revived from history push the screen down; lines taken from the
	// top pull it up. Clamping alone would leave the prompt stranded in
	// the middle of old output.
	s.cursor.X = min(s.cursor.X, cols-1)
	s.cursor.Y = min(max(s.cursor.Y+shift, 0), rows-1)
	s.cursor.WrapNext = false
	for i := range s.saved {
		s.saved[i].X = min(s.saved[i].X, cols-1)
		s.saved[i].Y = min(max(s.saved[i].Y+shift, 0), rows-1)
	}
	s.resizeTabs(cols)
	s.clampScrollOff()
	s.touchAll()
}

// line returns the current buffer's row y, or nil when out of range.
func (s *Screen) line(y int) line {
	if y < 0 || y >= len(s.cur.lines) {
		return nil
	}
	return s.cur.lines[y]
}

// touch records that row y has to be drawn again. A row outside the
// screen means the caller worked on something this cannot name, so the
// whole screen is drawn again.
func (s *Screen) touch(y int) {
	if y < 0 || y >= len(s.touched) {
		s.all = true
		return
	}
	s.touched[y] = true
}

// touchAll records that the whole screen has to be drawn again.
func (s *Screen) touchAll() { s.all = true }

// Print writes one rune at the cursor, handling deferred wrap, insert
// mode and double-width characters. A combining mark attaches to the
// cell already written rather than taking a cell of its own.
func (s *Screen) Print(r rune) { s.print(r, grid.RuneWidth(r)) }

// print writes one rune whose width has already been worked out. Width
// is the most expensive thing the emulator asks about a character, and
// the caller has usually asked already.
func (s *Screen) print(r rune, w int) {
	if w == 0 {
		s.attachCombining(r)
		return
	}

	if s.cursor.WrapNext && s.mode.Wrap {
		s.cursor.X = 0
		s.lineFeed()
		s.cursor.WrapNext = false
	}

	// A double-width character will not straddle the right edge. With
	// wrapping on it moves to the next line; with wrapping off it is
	// dropped, because overwriting the last column with half a glyph is
	// worse than losing it.
	if s.cursor.X+w > s.cols {
		if !s.mode.Wrap {
			return
		}
		s.cursor.X = 0
		s.lineFeed()
	}
	// A screen narrower than the character itself: wrapping did not
	// help and writing it would run off the end of the row.
	if s.cursor.X+w > s.cols {
		return
	}

	l := s.line(s.cursor.Y)
	if l == nil {
		return
	}
	if s.mode.Insert {
		s.insertBlanks(w)
		l = s.line(s.cursor.Y)
	}

	s.touch(s.cursor.Y)

	// Overwriting either half of an existing double-width character
	// leaves the other half orphaned, which draws a stray glyph.
	clearWideNeighbours(l, s.cursor.X, s.blank())
	if w == 2 {
		clearWideNeighbours(l, s.cursor.X+1, s.blank())
	}

	c := s.cursor.Pen
	c.Rune = r
	c.Comb = nil
	c.Width = uint8(w)
	l[s.cursor.X] = c
	if w == 2 {
		cont := s.cursor.Pen
		cont.Rune = 0
		cont.Comb = nil
		cont.Width = 0
		l[s.cursor.X+1] = cont
	}

	if s.cursor.X+w >= s.cols {
		// Stay put and remember the wrap; see cursorState.WrapNext.
		s.cursor.X = s.cols - 1
		s.cursor.WrapNext = true
	} else {
		s.cursor.X += w
	}
}

// attachCombining adds a zero-width mark to the cell the cursor last
// wrote, which is the cell to the left unless a wrap is pending.
func (s *Screen) attachCombining(r rune) {
	l := s.line(s.cursor.Y)
	if l == nil {
		return
	}
	x := s.cursor.X
	if !s.cursor.WrapNext {
		x--
	}
	if x < 0 || x >= s.cols {
		return
	}
	// Land on the lead cell, never the continuation half.
	if l[x].Width == 0 && x > 0 {
		x--
	}
	// A hostile stream can send combining marks forever. Without a cap
	// this grows without bound and, because each append copies, does so
	// quadratically: 400 KB of input froze the emulator for ten seconds.
	if len(l[x].Comb) >= maxCombining {
		return
	}
	// Copy on append rather than appending in place: Render hands this
	// slice to the grid, and growing it later would mutate what the grid
	// is showing without dirtying the row.
	marks := make([]rune, 0, len(l[x].Comb)+1)
	marks = append(marks, l[x].Comb...)
	l[x].Comb = append(marks, r)
	s.touch(s.cursor.Y)
}

// maxCombining is how many marks one cell may carry. Even the most
// enthusiastic real text stays well under this; beyond it the marks are
// unreadable anyway.
const maxCombining = 8

// clearWideNeighbours blanks the other half of any double-width
// character covering column x.
func clearWideNeighbours(l line, x int, blank grid.Cell) {
	if x < 0 || x >= len(l) {
		return
	}
	switch l[x].Width {
	case 2:
		if x+1 < len(l) {
			l[x+1] = blank
		}
	case 0:
		if x > 0 {
			l[x-1] = blank
		}
	}
}

// insertBlanks shifts the rest of the cursor's row right by n.
func (s *Screen) insertBlanks(n int) {
	l := s.line(s.cursor.Y)
	if l == nil || n <= 0 {
		return
	}
	x := s.cursor.X
	n = min(n, s.cols-x)
	copy(l[x+n:], l[x:s.cols-n])
	for i := x; i < x+n; i++ {
		l[i] = s.eraseCell()
	}
	s.repair(l)
}

// repair restores the double-width invariant after cells have been moved
// or blanked within a row. Every operation that shifts or erases cells
// can cut a wide character in half, and the renderer draws whatever half
// is left over the cell next to it.
func (s *Screen) repair(l line) {
	if l != nil {
		grid.RepairWidths(l, s.eraseCell())
	}
	// Every caller works on the cursor's own row, and every one of them
	// has just written to it.
	s.touch(s.cursor.Y)
}

// MoveTo places the cursor, honouring origin mode and clamping into
// range.
func (s *Screen) MoveTo(x, y int) {
	if s.cursor.Origin {
		y += s.top
		y = min(max(y, s.top), s.bot)
	} else {
		y = min(max(y, 0), s.rows-1)
	}
	s.cursor.X = min(max(x, 0), s.cols-1)
	s.cursor.Y = y
	s.cursor.WrapNext = false
}

// MoveRel moves the cursor by a delta without leaving the screen, or the
// scroll region when origin mode is on.
func (s *Screen) MoveRel(dx, dy int) {
	s.cursor.X = min(max(s.cursor.X+dx, 0), s.cols-1)

	// The margin only stops the cursor if it is on the far side of it
	// already. Moving up from below the top margin stops at the margin;
	// moving up from above it is unconstrained. Clamping to the whole
	// region regardless would trap a cursor that started outside.
	y := s.cursor.Y + dy
	switch {
	case dy < 0:
		lo := 0
		if s.cursor.Y >= s.top {
			lo = s.top
		}
		y = max(y, lo)
	case dy > 0:
		hi := s.rows - 1
		if s.cursor.Y <= s.bot {
			hi = s.bot
		}
		y = min(y, hi)
	}
	s.cursor.Y = min(max(y, 0), s.rows-1)
	s.cursor.WrapNext = false
}

// MoveToCol sets the column without touching the row. CHA and HPA are
// horizontal-only; routing them through MoveTo would re-apply the origin
// offset to a row that already has it and walk the cursor down the
// screen.
func (s *Screen) MoveToCol(x int) {
	s.cursor.X = min(max(x, 0), s.cols-1)
	s.cursor.WrapNext = false
}

// MoveToRow sets the row, honouring origin mode, without touching the
// column.
func (s *Screen) MoveToRow(y int) {
	if s.cursor.Origin {
		y = min(max(y+s.top, s.top), s.bot)
	} else {
		y = min(max(y, 0), s.rows-1)
	}
	s.cursor.Y = y
	s.cursor.WrapNext = false
}

// CursorRow returns the cursor row relative to the scroll region when
// origin mode is on, which is what a cursor position report must send.
func (s *Screen) CursorRow() int {
	if s.cursor.Origin {
		return s.cursor.Y - s.top
	}
	return s.cursor.Y
}

// lineFeed moves down one row, scrolling the region when it is already
// at the bottom.
func (s *Screen) lineFeed() {
	if s.cursor.Y == s.bot {
		s.scrolledOff(s.cur.scrollUp(s.top, s.bot, 1, s.cur == s.pri, s.eraseCell()))
		s.touchAll()
		return
	}
	if s.cursor.Y < s.rows-1 {
		s.cursor.Y++
	}
}

// LineFeed is the public form, used for LF, VT and FF.
func (s *Screen) LineFeed() {
	s.cursor.WrapNext = false
	s.lineFeed()
}

// ReverseIndex moves up one row, scrolling the region down when already
// at the top.
func (s *Screen) ReverseIndex() {
	s.cursor.WrapNext = false
	if s.cursor.Y == s.top {
		s.cur.scrollDown(s.top, s.bot, 1, s.eraseCell())
		s.touchAll()
		return
	}
	if s.cursor.Y > 0 {
		s.cursor.Y--
	}
}

// CarriageReturn returns to column 0.
func (s *Screen) CarriageReturn() {
	s.cursor.X = 0
	s.cursor.WrapNext = false
}

// Backspace moves left one column without wrapping to the previous row.
func (s *Screen) Backspace() {
	if s.cursor.WrapNext {
		s.cursor.WrapNext = false
		return
	}
	if s.cursor.X > 0 {
		s.cursor.X--
	}
}

// Tab moves to the next tab stop, or the last column if there is none.
func (s *Screen) Tab(n int) {
	s.cursor.WrapNext = false
	for ; n > 0; n-- {
		x := s.cursor.X + 1
		for x < s.cols && !s.tabs[x] {
			x++
		}
		s.cursor.X = min(x, s.cols-1)
	}
}

// BackTab moves to the previous tab stop.
func (s *Screen) BackTab(n int) {
	s.cursor.WrapNext = false
	for ; n > 0; n-- {
		x := s.cursor.X - 1
		for x > 0 && !s.tabs[x] {
			x--
		}
		s.cursor.X = max(x, 0)
	}
}

// SetTab sets or clears a tab stop at the cursor column.
func (s *Screen) SetTab(on bool) {
	if s.cursor.X >= 0 && s.cursor.X < len(s.tabs) {
		s.tabs[s.cursor.X] = on
	}
}

// ClearTabs removes every tab stop.
func (s *Screen) ClearTabs() { clear(s.tabs) }

// SetScrollRegion sets the DECSTBM margins, given 0-based inclusive
// rows, and homes the cursor as the standard requires.
func (s *Screen) SetScrollRegion(top, bot int) {
	top = min(max(top, 0), s.rows-1)
	bot = min(max(bot, 0), s.rows-1)
	if top >= bot {
		// DEC ignores the whole sequence when the region is inverted or
		// a single line: the margins and the cursor are both left alone.
		// Resetting them instead loses the margins of a program that
		// merely probed with a degenerate value.
		return
	}
	s.top, s.bot = top, bot
	s.MoveTo(0, 0)
}

// ScrollUp and ScrollDown are the SU and SD sequences, which move the
// region without moving the cursor.
func (s *Screen) ScrollUp(n int) {
	s.scrolledOff(s.cur.scrollUp(s.top, s.bot, n, s.cur == s.pri, s.eraseCell()))
	s.touchAll()
}

// scrolledOff counts lines that have left the top of the screen and
// keeps a scrolled-back view on the same text.
//
// left names lines and kept moves the view: a line that left with no
// history to go into still happened, and a view pinned to text that was
// thrown away has nothing to be pinned to.
func (s *Screen) scrolledOff(left, kept int) {
	if left > 0 {
		s.gone += uint64(left)
	}
	if kept > 0 && s.scrollOff > 0 {
		s.scrollOff += kept
		s.clampScrollOff()
	}
}

func (s *Screen) ScrollDown(n int) {
	s.cur.scrollDown(s.top, s.bot, n, s.eraseCell())
	s.touchAll()
}

// InsertLines opens n blank lines at the cursor row, pushing the rest of
// the region down. It is ignored outside the scroll region.
func (s *Screen) InsertLines(n int) {
	if s.cursor.Y < s.top || s.cursor.Y > s.bot {
		return
	}
	s.cur.scrollDown(s.cursor.Y, s.bot, n, s.eraseCell())
	s.cursor.X = 0
	s.cursor.WrapNext = false
	s.touchAll()
}

// DeleteLines removes n lines at the cursor row, pulling the rest of the
// region up.
// DeleteLines and InsertLines edit the screen rather than scrolling it,
// so nothing leaves the top and no line changes its number.
func (s *Screen) DeleteLines(n int) {
	if s.cursor.Y < s.top || s.cursor.Y > s.bot {
		return
	}
	// Deleting inside the screen is never history, even at row 0.
	_, _ = s.cur.scrollUp(s.cursor.Y, s.bot, n, false, s.eraseCell())
	s.cursor.X = 0
	s.cursor.WrapNext = false
	s.touchAll()
}

// InsertChars shifts the rest of the row right by n.
func (s *Screen) InsertChars(n int) {
	s.insertBlanks(n)
	s.cursor.WrapNext = false
}

// DeleteChars removes n cells at the cursor, pulling the rest left.
func (s *Screen) DeleteChars(n int) {
	l := s.line(s.cursor.Y)
	if l == nil || n <= 0 {
		return
	}
	x := s.cursor.X
	n = min(n, s.cols-x)
	copy(l[x:], l[x+n:s.cols])
	for i := s.cols - n; i < s.cols; i++ {
		l[i] = s.eraseCell()
	}
	s.repair(l)
	s.cursor.WrapNext = false
}

// EraseChars blanks n cells from the cursor without moving anything.
func (s *Screen) EraseChars(n int) {
	l := s.line(s.cursor.Y)
	if l == nil || n <= 0 {
		return
	}
	end := min(s.cursor.X+n, s.cols)
	for i := s.cursor.X; i < end; i++ {
		l[i] = s.eraseCell()
	}
	s.repair(l)
	s.cursor.WrapNext = false
}

// EraseInLine implements EL: mode 0 to the end, 1 to the start, 2 all.
func (s *Screen) EraseInLine(mode int) {
	l := s.line(s.cursor.Y)
	if l == nil {
		return
	}
	lo, hi := 0, s.cols
	switch mode {
	case 0:
		lo = s.cursor.X
	case 1:
		hi = min(s.cursor.X+1, s.cols)
	case 2:
	default:
		return
	}
	for i := lo; i < hi; i++ {
		l[i] = s.eraseCell()
	}
	s.repair(l)
	s.cursor.WrapNext = false
}

// EraseInDisplay implements ED: mode 0 to the end of the screen, 1 to
// the start, 2 the whole screen, 3 the scrollback as well.
func (s *Screen) EraseInDisplay(mode int) {
	switch mode {
	case 0:
		s.EraseInLine(0)
		s.eraseRows(s.cursor.Y+1, s.rows-1)
	case 1:
		s.EraseInLine(1)
		s.eraseRows(0, s.cursor.Y-1)
	case 2:
		s.clearScreen()
	case 3:
		// The scrollback stays. Erasing it is what the sequence means,
		// and what the person at this machine wants is to scroll up
		// afterwards and still see what was there; a reader from outside
		// gets the floor instead. The alternate screen keeps no history,
		// so a clear there marks nothing.
		s.clearedTo(s.gone)
		s.touchAll()
	}
	s.cursor.WrapNext = false
}

// clearScreen empties the screen, keeping what was on it when the cursor
// is at the top left.
//
// That is what `clear` sends, and what it means to the person who typed
// it is a blank screen they can scroll back out of. So the rows go into
// history rather than being written over, and the floor moves past them
// so that a reader from outside starts below the clear. A program
// erasing the screen from anywhere else is redrawing, and its rows are
// written over as they always were.
func (s *Screen) clearScreen() {
	home := s.cursor.X == 0 && s.cursor.Y == 0
	if !home || s.cur != s.pri {
		s.eraseRows(0, s.rows-1)
		return
	}
	if n := s.written(); n > 0 {
		s.scrolledOff(s.cur.scrollUp(0, s.rows-1, n, true, s.eraseCell()))
	}
	s.eraseRows(0, s.rows-1)
	s.clearedTo(s.gone)
}

// written is how many rows from the top have anything on them.
func (s *Screen) written() int {
	for y := s.rows - 1; y >= 0; y-- {
		for _, c := range s.line(y) {
			if c.Rune != 0 && c.Rune != ' ' {
				return y + 1
			}
		}
	}
	return 0
}

// clearedTo marks a line as the one a clear left behind.
//
// It only ever moves forward. A resize pulls lines back out of history
// and moves the count of what has left the screen back with them, and a
// floor that followed it down would offer lines a clear had put out of
// reach.
func (s *Screen) clearedTo(line uint64) {
	if s.cur == s.pri {
		s.floor = max(s.floor, line)
	}
}

func (s *Screen) eraseRows(from, to int) {
	blank := s.eraseCell()
	for y := max(from, 0); y <= min(to, s.rows-1); y++ {
		l := s.line(y)
		for i := range l {
			l[i] = blank
		}
		s.touch(y)
	}
}

// SaveCursor and RestoreCursor implement DECSC and DECRC, per buffer.
func (s *Screen) SaveCursor() { s.saved[s.bufIndex()] = s.cursor }

func (s *Screen) RestoreCursor() {
	c := s.saved[s.bufIndex()]
	s.cursor = c
	s.cursor.X = min(max(c.X, 0), s.cols-1)
	s.cursor.Y = min(max(c.Y, 0), s.rows-1)
}

func (s *Screen) bufIndex() int {
	if s.cur == s.alt {
		return 1
	}
	return 0
}

// UseAltBuffer switches between the primary and alternate buffers.
// Entering clears the alternate buffer, which is what a full-screen
// program expects; leaving does not touch the primary, which is how the
// shell's output reappears intact.
func (s *Screen) UseAltBuffer(on, clearOnEntry bool) {
	if on == (s.cur == s.alt) {
		return
	}
	if on {
		s.cur = s.alt
		s.mode.Alt = true
		if clearOnEntry {
			blank := s.blank()
			for _, l := range s.alt.lines {
				for i := range l {
					l[i] = blank
				}
			}
		}
	} else {
		s.cur = s.pri
		s.mode.Alt = false
	}
	// The new buffer's last column is not the old one's, so a pending
	// wrap makes no sense across the switch.
	s.cursor.WrapNext = false
	s.scrollOff = 0
	s.touchAll()
}

// Reset returns the screen to its power-on state (RIS).
//
// The count of lines that have left the top survives it. Everything on
// the screen has gone, which is lines leaving rather than lines never
// having been there, and a caller holding a line number has to go on
// being able to tell that the cursor is past it.
func (s *Screen) Reset() {
	pal := s.palette
	cols, rows := s.cols, s.rows
	scrollback := s.pri.maxScroll
	gone := s.gone + uint64(rows)
	*s = *NewScreen(cols, rows, pal, scrollback)
	s.gone, s.floor = gone, gone
	s.touchAll()
}

// DECALN fills the screen with E, a self-test pattern.
func (s *Screen) DecAln() {
	c := s.blank()
	c.Rune = 'E'
	for _, l := range s.cur.lines {
		for i := range l {
			l[i] = c
		}
	}
	s.top, s.bot = 0, s.rows-1
	s.MoveTo(0, 0)
	s.touchAll()
}

// Pen returns the current graphic rendition.
func (s *Screen) Pen() grid.Cell { return s.cursor.Pen }

// SetPen replaces the current graphic rendition.
func (s *Screen) SetPen(c grid.Cell) { s.cursor.Pen = c }

// CursorPos returns the cursor's column and row.
func (s *Screen) CursorPos() (x, y int) { return s.cursor.X, s.cursor.Y }

// SetCursorStyle selects how the cursor is drawn and whether it blinks.
func (s *Screen) SetCursorStyle(st grid.CursorStyle, blink bool) {
	s.curStyle, s.curBlink = st, blink
}

// ScrollView moves the view n lines back into history, positive for
// older. It has no effect on the alternate buffer, which keeps none.
func (s *Screen) ScrollView(n int) {
	s.scrollOff += n
	s.clampScrollOff()
	s.touchAll()
}

// ResetView jumps back to the live screen.
func (s *Screen) ResetView() {
	s.scrollOff = 0
	s.touchAll()
}

// ViewOffset returns how many lines back the view currently is.
func (s *Screen) ViewOffset() int { return s.scrollOff }

func (s *Screen) clampScrollOff() {
	s.scrollOff = min(max(s.scrollOff, 0), len(s.cur.scrollback))
}

// Render copies the visible view into g, including the cursor. Only
// cells that actually changed dirty a row, so an idle screen costs
// nothing downstream.
func (s *Screen) Render(g *grid.Grid) {
	g.DefaultFG, g.DefaultBG = s.palette.FG, s.palette.BG
	if c, r := g.Size(); c != s.cols || r != s.rows {
		g.Resize(s.cols, s.rows)
		s.touchAll()
	}
	// Any grid but the one the last render went to is missing whatever
	// this screen has forgotten it wrote, so it gets every row.
	all := s.all || g != s.drawnTo || len(s.touched) != s.rows
	if len(s.touched) != s.rows {
		s.touched = make([]bool, s.rows)
	}

	blank := s.blank()
	for y := 0; y < s.rows; y++ {
		if !all && !s.touched[y] {
			continue
		}
		l := s.cur.view(y, s.scrollOff)
		for x := 0; x < s.cols; x++ {
			c := blank
			if l != nil && x < len(l) {
				c = l[x]
			}
			if s.mode.ReverseVid {
				c.FG, c.BG = c.BG, c.FG
			}
			g.Set(x, y, c)
		}
	}

	clear(s.touched)
	s.all = false
	s.drawnTo = g

	cur := grid.Cursor{X: s.cursor.X, Y: s.cursor.Y, Style: s.curStyle, Blink: s.curBlink}
	// The cursor belongs to the live screen; scrolled back into history
	// it would sit on unrelated text.
	cur.Visible = s.mode.CursorVis && s.scrollOff == 0
	g.SetCursor(cur)
}

// AppCursor reports whether DECCKM is set, which changes how the cursor
// keys are encoded on the way back to the program.
func (s *Screen) AppCursor() bool { return s.mode.AppCursor }

// Bracketed reports whether bracketed paste is enabled.
func (s *Screen) Bracketed() bool { return s.mode.Bracketed }

// MouseEnabled reports whether the program asked for mouse reports.
func (s *Screen) MouseEnabled() bool {
	return s.mode.MouseClick || s.mode.MouseDrag || s.mode.MouseMotion
}

// OnAltBuffer reports whether the alternate screen is in use. Scrollback
// belongs to the primary buffer, so the mouse wheel should send arrow
// keys instead of scrolling the view while this is true.
func (s *Screen) OnAltBuffer() bool { return s.cur == s.alt }

// clearAlt blanks the alternate buffer. Kept separate from
// UseAltBuffer because the different alt-screen modes clear at
// different moments: 1049 on entry, 1047 on exit, 47 never.
func (s *Screen) clearAlt() {
	s.touchAll()
	blank := s.blank()
	for _, l := range s.alt.lines {
		for i := range l {
			l[i] = blank
		}
	}
}

// MouseModes reports what the program asked for with DECSET 1000, 1002,
// 1003 and 1006. The flags are returned rather than a struct so this
// package does not have to know about the input encoder.
func (s *Screen) MouseModes() (click, drag, motion, sgr bool) {
	return s.mode.MouseClick, s.mode.MouseDrag, s.mode.MouseMotion, s.mode.MouseSGR
}

// RenderLive copies the live screen into g, ignoring how far back into
// history the view has been scrolled.
func (s *Screen) RenderLive(g *grid.Grid) { s.RenderBack(g, 0) }

// RenderBack copies the screen as it stands back lines into history into
// g, ignoring how far back the view has been scrolled. More than there
// is history for reads as far back as there is.
func (s *Screen) RenderBack(g *grid.Grid, back int) {
	was := s.scrollOff
	defer func() {
		s.scrollOff = was
		s.drawnTo = nil
	}()
	s.scrollOff = min(max(back, 0), len(s.cur.scrollback))
	// A different view of the same rows, so what was drawn last time
	// says nothing about what this one needs -- and what this one leaves
	// in g is not the live screen either, so the next render starts
	// again whatever grid it is given.
	s.touchAll()
	s.Render(g)
}

// Floor is the line a clear left behind, and zero when nothing has
// cleared this screen.
//
// Everything above it is still kept and still drawn. It is what a reader
// from outside is offered down to, so that clearing the screen means
// what it looks like it means to whoever typed it.
func (s *Screen) Floor() uint64 { return s.floor }

// LineNumber names the line showing at a row of the primary screen.
//
// It counts from the first line the screen ever had and goes on counting
// past what history keeps, so it names a line for as long as the screen
// lives: a caller that wrote one down can tell whether the cursor has
// passed it. Rows of the alternate screen are not lines of this at all,
// and it says what the primary screen has there.
func (s *Screen) LineNumber(row int) uint64 { return s.gone + uint64(max(row, 0)) }

// History is how many lines have scrolled off the top and are kept.
func (s *Screen) History() int { return len(s.cur.scrollback) }

// RenderUnder draws the ordinary screen that an alternate one is
// covering, and reports whether there was one.
//
// It is what a watcher is given along with the alternate screen, so
// that the program quitting leaves the right thing behind rather than
// a blank pane nothing will redraw.
func (s *Screen) RenderUnder(g *grid.Grid) bool {
	if s.cur != s.pri {
		was, wasOff := s.cur, s.scrollOff
		s.cur, s.scrollOff = s.pri, 0
		s.touchAll()
		s.Render(g)
		s.cur, s.scrollOff = was, wasOff
		s.drawnTo = nil
		return true
	}
	return false
}

// Wrap reports whether DECAWM is set, which decides whether a line too
// long for the screen carries on to the next.
func (s *Screen) Wrap() bool { return s.mode.Wrap }

// WrapNext reports whether the last character filled the final column
// and the wrap it owes has not happened yet.
func (s *Screen) WrapNext() bool { return s.cursor.WrapNext }

// SetPalette gives the screen the colours it resolves into from now on.
//
// Cells already written keep the colours they were written in: a cell
// holds what it is drawn in, not which entry it came from. New output,
// and anything the screen blanks, takes the new scheme.
func (s *Screen) SetPalette(pal Palette) {
	was := s.palette
	s.palette = pal
	// What a blank line is made of comes from the pen, which every
	// resize and every scroll sets afresh, so only the pen is moved
	// here. The pen holds colours rather than which entry they came
	// from, so
	// one still writing in the old scheme's default takes the new one's.
	// A pen an escape put a colour in keeps it.
	retint := func(pen *grid.Cell) {
		if pen.FG == was.FG {
			pen.FG = pal.FG
		}
		if pen.BG == was.BG {
			pen.BG = pal.BG
		}
	}
	retint(&s.cursor.Pen)
	for i := range s.saved {
		retint(&s.saved[i].Pen)
	}
}
