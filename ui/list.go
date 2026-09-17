package ui

import (
	"image/color"
	"math"
	"reflect"

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

	// CurrentFG and CurrentBG mark the row the list was told is in
	// front. A list that is a list of what is open has to say which one
	// that is whether or not the user is looking at the list. Leaving
	// them with no alpha marks nothing, which is right for a list that
	// is only a list.
	CurrentFG, CurrentBG color.RGBA

	// HeaderFG is a line that names a group rather than being one of it.
	HeaderFG color.RGBA

	// NoteFG is the word at the end of a row: what it is doing, how fast.
	NoteFG color.RGBA

	// FillBG is the ground of the part of a row its Fill covers. Leaving
	// it with no alpha fills nothing, whatever a row asks for.
	FillBG color.RGBA

	// HeaderPad is the room around a header, in quarters of a cell. It
	// is what makes a name for the rows under it read as a heading
	// rather than as another row, without spending a whole line on the
	// gap.
	//
	// Only a list on a grid of its own can have it: a grid has one set
	// of row heights, so a list sharing one with a terminal would put
	// the gap through the terminal's lines as well.
	HeaderPad grid.Pad

	// BGEnd is the background of the last row, when it has an alpha. The
	// rows in between blend from BG to it, which gives a list a ground
	// of its own rather than the window's.
	BGEnd color.RGBA
}

// rowBG is the background of one row, blended down the list when the
// style asks for it.
func (l ListStyle) rowBG(y, rows int) color.RGBA {
	if l.BGEnd.A == 0 || rows <= 1 {
		return l.BG
	}
	return grid.Blend(l.BG, l.BGEnd, min(max(y, 0), rows-1), rows-1)
}

// ListRow is one line.
type ListRow struct {
	// Text is the line itself, and Note is a word at the end of it,
	// right aligned and dropped when there is no room for both.
	Text string
	Note string

	// Fill washes the ground of the row from the left, in the style's
	// FillBG: 0 covers nothing and 1 covers the whole width. It is how a
	// row says how far something has got without spending a column on a
	// bar.
	Fill float64

	// Depth indents the line, for something that belongs to the line
	// above it.
	Depth int

	// Header makes this a name for the rows under it rather than a row
	// of its own. A header cannot be selected.
	Header bool

	// Key is how a caller recognises this row again. The selection
	// follows it, so a list rebuilt every frame does not lose its place
	// when something above it appears or goes.
	//
	// It has to be comparable: a slice, a map or a function in here
	// would panic where it is compared, on the goroutine that draws.
	Key any

	// FG overrides the row's colour when it has an alpha, for a row that
	// has to read differently from the rest.
	FG color.RGBA

	// Mark is a character drawn in the indent in front of the text, and
	// MarkFG is its colour. It is how a row says what state it is in
	// without spending words on it.
	//
	// A row that drew an Icon draws no mark: the icon carries the same
	// colour. The mark is what a row falls back to in a list too narrow
	// for the icon.
	Mark   rune
	MarkFG color.RGBA

	// Button is a character drawn at the end of the row. Clicking it
	// runs the list's OnButton instead of choosing the row, which is
	// how a header -- a row nothing else can be done with -- offers
	// something to do.
	Button rune

	// HoverButton is a Button the row carries only while the pointer is
	// on it. A row that sets Button as well keeps that one.
	HoverButton rune

	// Edge colours the ground of the row's first cells, one cell per
	// colour, for a row that carries the same mark as something drawn
	// elsewhere in the window. A colour with no alpha draws nothing.
	Edge [2]color.RGBA

	// Art is drawn in the cell before the note, for a row with
	// something to show that no words would say as well.
	Art grid.Art

	// Icon is drawn in front of the text, for a row whose kind is better
	// shown than named. It takes the column it sits in and a blank after
	// it, and the text starts beyond them.
	//
	// IconFG is its colour, drawn by the same rules as MarkFG, so one
	// mark can say what a row is and what state it is in at once.
	Icon   grid.Art
	IconFG color.RGBA
}

// pickedOut is the colour a mark or an icon is drawn in: its own where
// it has one, and the row's where its own would be lost.
func pickedOut(own, fg color.RGBA, washed bool) color.RGBA {
	if own.A == 0 || washed {
		return fg
	}
	return own
}

// buttonOf is the character row i draws at its end, which is its
// HoverButton only while the pointer is on it.
//
// The pointer is held as a drawn row rather than as a place in the list,
// so that scrolling and rebuilding the rows cannot move it out from
// under the pointer for a frame.
func (l *List) buttonOf(i int) rune {
	if l.rows[i].Button != 0 {
		return l.rows[i].Button
	}
	if i-l.place.top == l.hovered {
		return l.rows[i].HoverButton
	}
	return 0
}

// Hover says which drawn row the pointer is on, counted from the top of
// what is showing, and -1 for none. Whatever is showing the list works it
// out: a list cannot see the pointer.
func (l *List) Hover() int { return l.hovered }

// SetHover says which drawn row the pointer is on, counted the way a
// mouse event counts them. Below zero means none.
func (l *List) SetHover(row int) {
	if row < 0 {
		row = -1
	}
	l.hovered = row
}

// buttonCol is the column a row's button is drawn in, or -1 when the
// list is too narrow to spare one.
//
// Not the last column: a list is usually docked against something, and
// a character hard up against a divider reads as part of it.
func buttonCol(cols int) int {
	if cols < 6 {
		return -1
	}
	return cols - 2
}

// ButtonCol is the column this list draws a row's button in, at the width
// it was last laid out for, or -1 when it is too narrow to spare one.
//
// It is what a caller pointing at the button asks, so nothing has to keep
// a copy of where the button goes.
func (l *List) ButtonCol() int { return buttonCol(l.size.Cols) }

// List is rows to look through and choose from. NewList builds one; the
// zero value is not a list.
//
// It is rebuilt from whatever it is showing on every frame, so SetRows
// keeps the selection on the same Key and leaves the scroll where it
// was. Nothing about it survives a rebuild except the user's place in it.
type List struct {
	Style ListStyle

	// OnActivate runs when Enter is pressed or a row is clicked.
	OnActivate func(ListRow) error

	// OnButton runs when a row's Button is clicked.
	OnButton func(ListRow) error

	rows []ListRow

	// place is the selected row and the first one drawn.
	place listState

	// current is the key of the row that is in front, which is not the
	// same as the row the bar is on: the bar is the user's, and they
	// move it to look at something else without that changing what is
	// showing.
	current any

	// pads is the answer RowPads last gave, kept so that settling a
	// height, which asks several times over, does not allocate.
	pads []grid.Pad

	// hovered is the row the pointer is on, counted from the top of what
	// is showing, and -1 when it is on none of them.
	hovered int

	size    Size
	focused bool
	buf     buffer
}

// NewList returns an empty list.
func NewList() *List {
	l := &List{hovered: -1}
	l.place = listState{
		at: -1, count: l.rowCount, rows: l.boxRows, selectable: l.notAHeader,
	}
	return l
}

// rowCount, boxRows and notAHeader are what the list looks like now: its
// rows, the room for them, and the headers the bar may not land on.
func (l *List) rowCount() int         { return len(l.rows) }
func (l *List) boxRows() int          { return l.size.Rows }
func (l *List) notAHeader(i int) bool { return !l.rows[i].Header }

// sameKey compares two row keys.
//
// Comparing two interfaces panics when either holds something that
// cannot be compared, and this runs on the goroutine that draws: a panic
// here takes the window with it. A key like that simply never matches,
// which costs the user their place in the list and nothing else.
func sameKey(a, b any) bool {
	ta, tb := reflect.TypeOf(a), reflect.TypeOf(b)
	if ta == nil || tb == nil || ta != tb || !ta.Comparable() {
		return false
	}
	return a == b
}

// SetRows replaces what the list shows, keeping the user's place.
//
// The selection follows its Key rather than its position, because the
// rows above it come and go while the user is looking at them.
func (l *List) SetRows(rows []ListRow) {
	var key any
	if l.place.at >= 0 && l.place.at < len(l.rows) {
		key = l.rows[l.place.at].Key
	}
	l.rows = rows

	l.place.at = -1
	if key != nil {
		for i, row := range rows {
			if !row.Header && sameKey(row.Key, key) {
				l.place.at = i
				break
			}
		}
	}
	if l.place.at < 0 {
		l.place.at = l.place.first()
	}
	// Only clamped, not pulled back to the selection: the panel is
	// rebuilt every frame, and bringing the selection into view here
	// would undo the wheel within a frame of the user turning it.
	l.place.clamp()
}

// Rows returns what the list is showing.
func (l *List) Rows() []ListRow { return l.rows }

// SetCurrent says which row is in front, by its key. It is marked with
// the style's Current colours wherever it happens to be.
//
// Separate from the selection: the bar is the user's and moves where
// they put it, while what is in front changes for its own reasons. A
// list that marked the bar instead would confidently point at something
// that is not on screen.
func (l *List) SetCurrent(key any) { l.current = key }

// Current returns the key of the row that is in front.
func (l *List) Current() any { return l.current }

// Selected returns the row Enter would act on, and whether there is one.
func (l *List) Selected() (ListRow, bool) {
	if l.place.at < 0 || l.place.at >= len(l.rows) {
		return ListRow{}, false
	}
	return l.rows[l.place.at], true
}

// SelectedIndex returns which row is selected, or -1.
func (l *List) SelectedIndex() int { return l.place.at }

// RowTop returns how far down the box the row with a key is drawn, or
// -1 when there is no such row or it is scrolled out of sight.
//
// It is how a caller anchors something to a row: a menu dropped under
// the line the user clicked.
func (l *List) RowTop(key any) int {
	for i, row := range l.rows {
		if !sameKey(row.Key, key) {
			continue
		}
		if y := i - l.place.top; y >= 0 && y < l.size.Rows {
			return y
		}
		return -1
	}
	return -1
}

// Move steps the bar through the rows, for a caller that acts on a row
// and then wants the next one: marking a run of names is one key held
// down rather than two alternating.
func (l *List) Move(by int) { l.place.move(by) }

// Reveal scrolls until the selected row is on screen.
//
// Setting rows does not do this, because the list is rebuilt every frame
// and it would undo the wheel. A caller that has just made the list
// shorter does need it: the selection is where the user put it, and it
// has to still be somewhere they can see.
func (l *List) Reveal() { l.place.ensureVisible() }

// Select moves the selection to the row with a key, reporting whether
// there is one.
func (l *List) Select(key any) bool {
	for i, row := range l.rows {
		if !row.Header && sameKey(row.Key, key) {
			l.place.moveTo(i)
			return true
		}
	}
	return false
}

// Layout notes how much room the list has.
func (l *List) Layout(size Size) {
	l.size = size
	l.place.clamp()
}

// RowPads is the room the list wants around each of its rows, were its
// box this many rows tall.
//
// Only the headings it would be showing at that height get any: room
// for one scrolled out of sight is a row taken off the list for a gap
// nobody can see.
//
// Nothing is changed by asking. Where the list would start is worked
// out rather than set, because the height is still being settled and
// laying the list out to find out would drag the selection with it.
//
// The slice is reused between calls.
func (l *List) RowPads(rows int) []grid.Pad {
	if l.Style.HeaderPad.Empty() || rows <= 0 {
		return nil
	}
	top := min(max(l.place.top, 0), max(len(l.rows)-rows, 0))
	l.pads = l.pads[:0]
	for y := 0; y < rows; y++ {
		i := top + y
		if i < 0 || i >= len(l.rows) || !l.rows[i].Header {
			l.pads = append(l.pads, grid.Pad{})
			continue
		}
		l.pads = append(l.pads, l.Style.HeaderPad)
	}
	return l.pads
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
		l.place.move(-1)
	case input.KeyDown:
		l.place.move(1)
	case input.KeyPageUp:
		l.place.page(-1)
	case input.KeyPageDown:
		l.place.page(1)
	case input.KeyHome:
		l.place.moveTo(l.place.first())
	case input.KeyEnd:
		l.place.moveTo(l.place.last())
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

	row := l.place.top + ev.Row
	if row < 0 || row >= len(l.rows) {
		// The space below the last row. The press is still the list's:
		// it puts the keys here.
		return true, nil
	}
	// The button first, because it is the one thing a header can be
	// clicked for and it sits on rows that can be chosen as well.
	if at := buttonCol(l.size.Cols); at >= 0 && l.buttonOf(row) != 0 && ev.Col == at {
		if l.OnButton == nil {
			return true, nil
		}
		return true, l.OnButton(l.rows[row])
	}
	if l.rows[row].Header {
		// A header names the rows under it rather than being one to act
		// on.
		return true, nil
	}
	l.place.moveTo(row)
	return true, l.activate()
}

// Draw paints the rows that fit.
func (l *List) Draw(v grid.View) { l.buf.draw(v, l.paint) }

func (l *List) paint(v grid.View) {
	cols, rows := v.Size()
	if cols <= 0 || rows <= 0 {
		return
	}
	// Row by row, because the ground can be a blend down the list rather
	// than one colour.
	for y := 0; y < rows; y++ {
		line := v.Sub(0, y, cols, 1)
		line.Fill(grid.Cell{
			Rune: ' ', FG: l.Style.FG, BG: l.Style.rowBG(y, rows), Width: 1,
		})
	}

	for y := 0; y < rows; y++ {
		i := l.place.top + y
		if i >= len(l.rows) {
			break
		}
		row := l.rows[i]
		row.Button = l.buttonOf(i)
		l.paintRow(v.Sub(0, y, cols, 1), row, i == l.place.at, y, rows)
	}
}

// paintRow draws one line: the mark in the indent, the text, and the
// note at the end.
func (l *List) paintRow(v grid.View, row ListRow, selected bool, y, rows int) {
	cols, _ := v.Size()
	fg, bg := l.Style.FG, l.Style.rowBG(y, rows)
	noteFG := l.Style.NoteFG
	switch {
	case row.FG.A != 0:
		// A row's own colour wins, headers included.
		fg = row.FG
	case row.Header:
		fg = l.Style.HeaderFG
	}
	// washed says the row's ground is light enough to swallow a mark
	// picked out against the ordinary one, and lifted says the row has a
	// ground of its own rather than the list's.
	washed, lifted := false, false
	switch {
	case selected && l.focused:
		fg, bg = l.Style.SelectedFG, l.Style.SelectedBG
		// A colour picked to stand out against the other rows can
		// disappear against the selected one. Whatever the row writes
		// its own text in is the one colour known to show there.
		noteFG, washed, lifted = fg, true, true
	case l.Style.CurrentBG.A != 0 && l.current != nil && sameKey(row.Key, l.current):
		// Only the ground changes. The mark keeps its own colour,
		// because what it says is the whole reason it is there and this
		// is the row the user is looking at.
		fg, bg = l.Style.CurrentFG, l.Style.CurrentBG
		noteFG, lifted = fg, true
	}
	v.Fill(grid.Cell{Rune: ' ', FG: fg, BG: bg, Width: 1})

	// The button, then the note, then whatever is left is the text's.
	// Each takes from the right, so the text needs no measuring around
	// them.
	room := cols
	if at := buttonCol(cols); at >= 0 && row.Button != 0 {
		v.Set(at, 0, grid.Cell{Rune: row.Button, FG: fg, BG: bg, Width: 1})
		room = at
	}
	if row.Art.Kind != grid.ArtNone {
		// A cell of its own, before the note: the two say different
		// things about the same connection.
		if at := room - 2; at > 2 {
			v.Set(at, 0, grid.Cell{
				Rune: ' ', FG: noteFG, BG: bg, Width: 1, Art: row.Art,
			})
			room = at - 1
		}
	}
	if row.Note != "" {
		w := grid.StringWidth(row.Note)
		// A blank before it as well as after, so a note and a button do
		// not run into one another.
		if at := room - w - 1; at > 2 {
			v.SetString(at, 0, row.Note, noteFG, bg, 0)
			room = at - 1
		}
	}

	at := min(row.Depth*2, max(cols-1, 0))
	// The icon goes where the text would start, and the text moves along
	// to make room: a picture of what a row is says it in one column
	// where the word for it took eight.
	drewIcon := row.Icon.Kind != grid.ArtNone && at+2 < room
	if drewIcon {
		icon := pickedOut(row.IconFG, fg, washed)
		v.Set(at, 0, grid.Cell{Rune: ' ', FG: icon, BG: bg, Width: 1, Art: row.Icon})
		v.Set(at+1, 0, grid.Cell{Rune: ' ', FG: fg, BG: bg, Width: 1})
		at += 2
	}
	// The mark sits in the indent the text leaves in front of it, so it
	// costs no column of its own. Left out where the icon was drawn,
	// which says the same thing in the same colour.
	if row.Mark != 0 && !drewIcon && row.Depth*2 >= 2 {
		mark := pickedOut(row.MarkFG, fg, washed)
		v.Set(row.Depth*2-2, 0, grid.Cell{Rune: row.Mark, FG: mark, BG: bg, Width: 1})
	}
	attr := grid.Attr(0)
	if row.Header {
		attr = grid.AttrBold
	}
	v.SetString(at, 0, grid.Trim(row.Text, max(room-at, 0)), fg, bg, attr)
	l.paintFill(v, row.Fill, fg, bg, lifted)
	paintEdge(v, row.Edge)
}

// paintEdge washes the ground of the row's first cells, one cell per
// colour, and leaves whatever is drawn in them alone. It goes on after
// the fill, which washes the same cells.
func paintEdge(v grid.View, edge [2]color.RGBA) {
	cols, _ := v.Size()
	n := 0
	for i, c := range edge {
		if c.A != 0 {
			n = i + 1
		}
	}
	if n < cols && v.At(n, 0).Width == 0 {
		// The far half of a double-width character. Both halves take the
		// same ground, or one character is drawn on two.
		n--
	}
	for x := 0; x < n; x++ {
		c := v.At(x, 0)
		if c.BG.A == 0 {
			// A see-through ground shows nothing whatever is washed over
			// it, and writing it would dirty the row for no pixels.
			continue
		}
		hue := edge[x]
		if c.Width == 0 && x > 0 {
			// The far half of a wide character takes the colour its lead
			// took, or one character is drawn on two grounds.
			hue = edge[x-1]
		}
		was := c.BG.A
		c.BG = grid.Blend(c.BG, hue, int(hue.A), 0xff)
		c.BG.A = was
		v.Set(x, 0, c)
	}
}

// paintFill changes the ground of the first cells of a row to the style's
// FillBG, for a row saying how far something has got.
//
// It goes over the drawn row rather than under it, so the text, the mark,
// the note and the button keep the colours they were drawn in. Writing a
// cell twice costs nothing here: the list paints through a buffer of its
// own (l.buf, buffer.go) and copies each cell out once, unlike a widget
// that draws straight onto a layer.
//
// lifted says the row has a ground of its own -- the selected row, or the
// one in front. Such a row keeps that ground, taken half way towards the
// fill pulled towards fg: a fill that only lifts an already lifted ground
// cannot be told from no fill, and fg is the one colour that row is known
// to show.
func (l *List) paintFill(v grid.View, fill float64, fg, bg color.RGBA, lifted bool) {
	if math.IsNaN(fill) || fill <= 0 || l.Style.FillBG.A == 0 {
		// A fill that is not a number fills nothing. It cannot be
		// clamped, and it must not reach the conversion below.
		return
	}
	cols, _ := v.Size()
	ground := l.Style.FillBG
	if lifted {
		ground = grid.Blend(bg, grid.Blend(ground, fg, 1, 3), 1, 2)
	}
	// Clamped before the conversion, so nothing outside 0 to 1 -- an
	// infinity among it -- becomes a width.
	n := int(math.Round(min(max(fill, 0), 1) * float64(cols)))
	if n < cols && v.At(n, 0).Width == 0 {
		// The far half of a double-width character. Both halves take the
		// same ground, or one character is drawn on two.
		n--
	}
	for x := 0; x < n; x++ {
		c := v.At(x, 0)
		c.BG = ground
		v.Set(x, 0, c)
	}
}

// activate runs whatever the selected row means.
func (l *List) activate() error {
	row, ok := l.Selected()
	if !ok || l.OnActivate == nil {
		return nil
	}
	return l.OnActivate(row)
}

// scrollBy moves what is shown without moving the selection, for the
// wheel.
func (l *List) scrollBy(by int) {
	l.place.top += by
	l.place.clamp()
}
