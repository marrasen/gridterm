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

	curStyle grid.CursorStyle
}

// NewScreen returns a screen of the given size.
func NewScreen(cols, rows int, pal Palette, scrollback int) *Screen {
	cols = max(cols, 1)
	rows = max(rows, 1)
	s := &Screen{
		cols:    cols,
		rows:    rows,
		palette: pal,
		pri:     newBuffer(cols, rows, scrollback, blankFor(pal)),
		alt:     newBuffer(cols, rows, 0, blankFor(pal)),
		top:     0,
		bot:     rows - 1,
	}
	s.cur = s.pri
	s.mode.Wrap = true
	s.mode.CursorVis = true
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

// Palette returns the colour scheme in use.
func (s *Screen) Palette() Palette { return s.palette }

// Modes returns a copy of the current mode flags.
func (s *Screen) Modes() modes { return s.mode }

// blankFor is the empty cell for a palette's defaults.
func blankFor(pal Palette) grid.Cell {
	return grid.Cell{Rune: ' ', FG: pal.FG, BG: pal.BG, Width: 1}
}

// blank returns an empty cell in the current default colours. New cells
// take the default background rather than the pen's, so clearing with a
// coloured pen does not paint the screen.
func (s *Screen) blank() grid.Cell { return blankFor(s.palette) }

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
	priShift := s.pri.resize(cols, rows, s.eraseCell(), s.cursor.Y)
	altShift := s.alt.resize(cols, rows, s.eraseCell(), s.cursor.Y)
	shift := priShift
	if s.cur == s.alt {
		shift = altShift
	}

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
}

// line returns the current buffer's row y, or nil when out of range.
func (s *Screen) line(y int) line {
	if y < 0 || y >= len(s.cur.lines) {
		return nil
	}
	return s.cur.lines[y]
}

// Print writes one rune at the cursor, handling deferred wrap, insert
// mode and double-width characters. A combining mark attaches to the
// cell already written rather than taking a cell of its own.
func (s *Screen) Print(r rune) {
	w := grid.RuneWidth(r)
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
		s.historyGrew(s.cur.scrollUp(s.top, s.bot, 1, s.cur == s.pri, s.eraseCell()))
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
	s.historyGrew(s.cur.scrollUp(s.top, s.bot, n, s.cur == s.pri, s.eraseCell()))
}

// historyGrew keeps a scrolled-back view on the same text when new lines
// push into history underneath it. Without this the view drifts forward
// on its own while output arrives, which is disorienting to read.
func (s *Screen) historyGrew(n int) {
	if n > 0 && s.scrollOff > 0 {
		s.scrollOff += n
		s.clampScrollOff()
	}
}

func (s *Screen) ScrollDown(n int) { s.cur.scrollDown(s.top, s.bot, n, s.eraseCell()) }

// InsertLines opens n blank lines at the cursor row, pushing the rest of
// the region down. It is ignored outside the scroll region.
func (s *Screen) InsertLines(n int) {
	if s.cursor.Y < s.top || s.cursor.Y > s.bot {
		return
	}
	s.cur.scrollDown(s.cursor.Y, s.bot, n, s.eraseCell())
	s.cursor.X = 0
	s.cursor.WrapNext = false
}

// DeleteLines removes n lines at the cursor row, pulling the rest of the
// region up.
func (s *Screen) DeleteLines(n int) {
	if s.cursor.Y < s.top || s.cursor.Y > s.bot {
		return
	}
	// Deleting inside the screen is never history, even at row 0.
	_ = s.cur.scrollUp(s.cursor.Y, s.bot, n, false, s.eraseCell())
	s.cursor.X = 0
	s.cursor.WrapNext = false
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
		s.eraseRows(0, s.rows-1)
	case 3:
		s.cur.scrollback = nil
		s.scrollOff = 0
	}
	s.cursor.WrapNext = false
}

func (s *Screen) eraseRows(from, to int) {
	blank := s.eraseCell()
	for y := max(from, 0); y <= min(to, s.rows-1); y++ {
		l := s.line(y)
		for i := range l {
			l[i] = blank
		}
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
}

// Reset returns the screen to its power-on state (RIS).
func (s *Screen) Reset() {
	pal := s.palette
	cols, rows := s.cols, s.rows
	scrollback := s.pri.maxScroll
	*s = *NewScreen(cols, rows, pal, scrollback)
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
}

// Pen returns the current graphic rendition.
func (s *Screen) Pen() grid.Cell { return s.cursor.Pen }

// SetPen replaces the current graphic rendition.
func (s *Screen) SetPen(c grid.Cell) { s.cursor.Pen = c }

// CursorPos returns the cursor's column and row.
func (s *Screen) CursorPos() (x, y int) { return s.cursor.X, s.cursor.Y }

// SetCursorStyle selects how the cursor is drawn.
func (s *Screen) SetCursorStyle(st grid.CursorStyle) { s.curStyle = st }

// ScrollView moves the view n lines back into history, positive for
// older. It has no effect on the alternate buffer, which keeps none.
func (s *Screen) ScrollView(n int) {
	s.scrollOff += n
	s.clampScrollOff()
}

// ResetView jumps back to the live screen.
func (s *Screen) ResetView() { s.scrollOff = 0 }

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
	}
	blank := s.blank()
	for y := 0; y < s.rows; y++ {
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

	cur := grid.Cursor{X: s.cursor.X, Y: s.cursor.Y, Style: s.curStyle}
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
func (s *Screen) RenderLive(g *grid.Grid) {
	was := s.scrollOff
	s.scrollOff = 0
	s.Render(g)
	s.scrollOff = was
}

// Wrap reports whether DECAWM is set, which decides whether a line too
// long for the screen carries on to the next.
func (s *Screen) Wrap() bool { return s.mode.Wrap }
