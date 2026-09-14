package ui

import (
	"image/color"

	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/input"
)

// listWheel is how many rows one notch of the wheel moves.
const listWheel = 3

// ListStyle colours a list.
type ListStyle struct {
	// FG and BG are an ordinary row.
	FG, BG color.RGBA

	// SelectedFG and SelectedBG mark the row Enter would act on.
	SelectedFG, SelectedBG color.RGBA

	// HeaderFG is a line that names a group rather than being one of it.
	HeaderFG color.RGBA

	// NoteFG is the word at the end of a row: what it is doing, how fast.
	NoteFG color.RGBA
}

// ListRow is one line.
type ListRow struct {
	// Text is the line itself, and Note is a word at the end of it,
	// right aligned and dropped when there is no room for both.
	Text string
	Note string

	// Depth indents the line, for something that belongs to the line
	// above it.
	Depth int

	// Header makes this a name for the rows under it rather than a row
	// of its own. A header cannot be selected.
	Header bool

	// Key is how a caller recognises this row again. The selection
	// follows it, so a list rebuilt every frame does not lose its place
	// when something above it appears or goes.
	Key any

	// FG overrides the row's colour when it has an alpha, for a row that
	// has to read differently from the rest.
	FG color.RGBA
}

// List is rows to look through and choose from.
//
// It is rebuilt from whatever it is showing on every frame, so SetRows
// keeps the selection on the same Key and leaves the scroll where it
// was. Nothing about it survives a rebuild except the user's place in it.
type List struct {
	Style ListStyle

	// OnActivate runs when Enter is pressed or a row is clicked.
	OnActivate func(ListRow) error

	// OnSelect is told when the selection moves, for a caller that
	// mirrors it somewhere else.
	OnSelect func(ListRow)

	rows []ListRow

	// at is the selected row and top the first one drawn. A list longer
	// than the box needs both: moving only the selection lets it walk
	// off the bottom of what is on screen.
	at, top int

	size    Size
	focused bool
	buf     buffer
}

// NewList returns an empty list.
func NewList() *List { return &List{at: -1} }

// SetRows replaces what the list shows, keeping the user's place.
//
// The selection follows its Key rather than its position, because the
// rows above it come and go while the user is looking at them.
func (l *List) SetRows(rows []ListRow) {
	var key any
	if l.at >= 0 && l.at < len(l.rows) {
		key = l.rows[l.at].Key
	}
	l.rows = rows

	l.at = -1
	if key != nil {
		for i, row := range rows {
			if !row.Header && row.Key == key {
				l.at = i
				break
			}
		}
	}
	if l.at < 0 {
		l.at = l.firstSelectable()
	}
	l.scroll()
}

// Rows returns what the list is showing.
func (l *List) Rows() []ListRow { return l.rows }

// Selected returns the row Enter would act on, and whether there is one.
func (l *List) Selected() (ListRow, bool) {
	if l.at < 0 || l.at >= len(l.rows) {
		return ListRow{}, false
	}
	return l.rows[l.at], true
}

// SelectedIndex returns which row is selected, or -1.
func (l *List) SelectedIndex() int { return l.at }

// Select moves the selection to the row with a key, reporting whether
// there is one.
func (l *List) Select(key any) bool {
	for i, row := range l.rows {
		if !row.Header && row.Key == key {
			l.moveTo(i)
			return true
		}
	}
	return false
}

// Layout notes how much room the list has.
func (l *List) Layout(size Size) {
	l.size = size
	l.scroll()
}

// SetFocus marks the list as the one receiving keys, which is what makes
// the selection stand out.
func (l *List) SetFocus(on bool) { l.focused = on }

// Focused reports whether the list has the keys.
func (l *List) Focused() bool { return l.focused }

// HandleKey moves the selection and acts on it. Keys it has no use for
// travel on, so the shortcuts around it still work.
func (l *List) HandleKey(ev input.Event) (bool, error) {
	if ev.Kind != input.KeyPress && ev.Kind != input.KeyRepeat {
		return false, nil
	}
	if ev.Mods != 0 {
		// Ctrl+Tab and the rest belong to whatever is around the list.
		return false, nil
	}

	switch ev.Key {
	case input.KeyUp:
		l.move(-1)
	case input.KeyDown:
		l.move(1)
	case input.KeyPageUp:
		l.move(-max(l.size.Rows-1, 1))
	case input.KeyPageDown:
		l.move(max(l.size.Rows-1, 1))
	case input.KeyHome:
		l.moveTo(l.firstSelectable())
	case input.KeyEnd:
		l.moveTo(l.lastSelectable())
	case input.KeyEnter, input.KeySpace:
		return true, l.activate()
	default:
		return false, nil
	}
	return true, nil
}

// HandleMouse selects what was clicked and acts on it, and scrolls on
// the wheel.
func (l *List) HandleMouse(ev input.MouseEvent) (bool, error) {
	if ev.Kind != input.MousePress {
		return false, nil
	}
	switch ev.Button {
	case input.MouseWheelUp:
		l.scrollBy(-listWheel)
		return true, nil
	case input.MouseWheelDown:
		l.scrollBy(listWheel)
		return true, nil
	}

	row := l.top + ev.Row
	if row < 0 || row >= len(l.rows) || l.rows[row].Header {
		// A header is not a row to act on, and neither is the space
		// below the last one. The press is still the list's: it puts the
		// keys here.
		return true, nil
	}
	l.moveTo(row)
	return true, l.activate()
}

// Draw paints the rows that fit.
func (l *List) Draw(v grid.View) { l.buf.draw(v, l.paint) }

func (l *List) paint(v grid.View) {
	cols, rows := v.Size()
	if cols <= 0 || rows <= 0 {
		return
	}
	v.Fill(grid.Cell{Rune: ' ', FG: l.Style.FG, BG: l.Style.BG, Width: 1})

	for y := 0; y < rows; y++ {
		i := l.top + y
		if i >= len(l.rows) {
			break
		}
		l.paintRow(v.Sub(0, y, cols, 1), l.rows[i], i == l.at)
	}
}

// paintRow draws one line: the text, indented, and the note at the end.
func (l *List) paintRow(v grid.View, row ListRow, selected bool) {
	cols, _ := v.Size()
	fg, bg := l.Style.FG, l.Style.BG
	noteFG := l.Style.NoteFG
	switch {
	case row.Header:
		fg = l.Style.HeaderFG
	case row.FG.A != 0:
		fg = row.FG
	}
	if selected && l.focused {
		fg, bg = l.Style.SelectedFG, l.Style.SelectedBG
		// A colour picked to stand out against the other rows can
		// disappear against the selected one. Whatever the row writes
		// its own text in is the one colour known to show there.
		noteFG = fg
	}
	v.Fill(grid.Cell{Rune: ' ', FG: fg, BG: bg, Width: 1})

	// The note first, right aligned, so the text can use whatever is
	// left without measuring around it.
	room := cols
	if row.Note != "" {
		w := grid.StringWidth(row.Note)
		// Shown only if a column of text survives it: a row holding
		// nothing but a note does not say what it is about.
		if at := cols - w; at > 2 {
			v.SetString(at, 0, row.Note, noteFG, bg, 0)
			room = at - 1
		}
	}

	at := min(row.Depth*2, max(cols-1, 0))
	attr := grid.Attr(0)
	if row.Header {
		attr = grid.AttrBold
	}
	v.SetString(at, 0, trimTo(row.Text, max(room-at, 0)), fg, bg, attr)
}

// activate runs whatever the selected row means.
func (l *List) activate() error {
	row, ok := l.Selected()
	if !ok || l.OnActivate == nil {
		return nil
	}
	return l.OnActivate(row)
}

// move steps the selection, skipping the headers and stopping at the
// ends rather than wrapping: a list you can run off the end of is hard
// to aim at.
func (l *List) move(by int) {
	if by == 0 {
		return
	}
	step := 1
	if by < 0 {
		step, by = -1, -by
	}
	at := l.at
	for ; by > 0; by-- {
		next := l.nextFrom(at, step)
		if next < 0 {
			break
		}
		at = next
	}
	l.moveTo(at)
}

// moveTo puts the selection on one row and tells whoever is listening.
func (l *List) moveTo(at int) {
	if at < 0 || at >= len(l.rows) || l.rows[at].Header || at == l.at {
		l.scroll()
		return
	}
	l.at = at
	l.scroll()
	if l.OnSelect != nil {
		l.OnSelect(l.rows[at])
	}
}

// nextFrom returns the next row that can be selected in a direction, or
// -1 when there is none.
func (l *List) nextFrom(from, step int) int {
	for at := from + step; at >= 0 && at < len(l.rows); at += step {
		if !l.rows[at].Header {
			return at
		}
	}
	return -1
}

func (l *List) firstSelectable() int {
	for i, row := range l.rows {
		if !row.Header {
			return i
		}
	}
	return -1
}

func (l *List) lastSelectable() int {
	for i := len(l.rows) - 1; i >= 0; i-- {
		if !l.rows[i].Header {
			return i
		}
	}
	return -1
}

// scrollBy moves what is shown without moving the selection, for the
// wheel.
func (l *List) scrollBy(by int) {
	l.top = min(max(l.top+by, 0), max(len(l.rows)-l.size.Rows, 0))
}

// scroll brings the selected row into the box, so Enter always acts on
// something the user can see.
func (l *List) scroll() {
	rows := l.size.Rows
	if rows <= 0 {
		l.top = 0
		return
	}
	// A list that has shrunk must not be left scrolled past its end.
	l.top = min(l.top, max(len(l.rows)-rows, 0))
	l.top = max(l.top, 0)
	if l.at < 0 {
		return
	}
	if l.at < l.top {
		l.top = l.at
	}
	if l.at >= l.top+rows {
		l.top = l.at - rows + 1
	}
}
