package ui

import (
	"image/color"

	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/input"
)

// How a chooser is measured: the margin round it, the rule, and the
// blank column each side of a line.
const (
	chooserMargin = 2
	chooserFrame  = 1
	chooserPad    = 1
	chooserMinCol = 16
	chooserMaxCol = 60
	chooserMaxRow = 16
)

// ChooserStyle colours a chooser.
type ChooserStyle struct {
	FG, BG color.RGBA

	// TitleFG is the line at the top saying what is being picked.
	TitleFG color.RGBA

	// SelectedFG and SelectedBG mark the line Enter would take.
	SelectedFG, SelectedBG color.RGBA

	// NoteFG is the word at the end of a line, saying more about it.
	NoteFG color.RGBA

	// BorderFG is the rule around the outside, and ShadowBG darkens the
	// cells it falls on below and to the right.
	BorderFG color.RGBA
	ShadowBG color.RGBA
}

// Chooser is a modal list of things to pick one of.
//
// A menu names commands, so that a menu, a key binding and the palette
// are three ways into one list. A chooser is for picking one of a set of
// things that exist only right now -- a pane to move, a machine to open
// on -- which no command can name, because they come and go while the
// program runs.
type Chooser struct {
	Style ChooserStyle

	title string
	list  *List
	dos   []func() error
	close func()

	size Size
	buf  buffer
}

// NewChooser returns an empty chooser. close is called when it is
// finished with, which is what takes it off the modal stack: the chooser
// does not know what is showing it.
func NewChooser(title string, close func()) *Chooser {
	c := &Chooser{title: title, close: close, list: NewList()}
	c.list.OnActivate = func(row ListRow) error { return c.take(row) }
	// The keys arrive with the chooser: a modal is pushed and focused in
	// one go, and a list that had to wait to be told would open with no
	// line marked.
	c.list.SetFocus(true)
	return c
}

// Add puts a line at the bottom. note is shown at its end, and do is
// what taking the line does.
func (c *Chooser) Add(text, note string, do func() error) {
	c.dos = append(c.dos, do)
	rows := append(c.list.Rows(), ListRow{Text: text, Note: note, Key: len(c.dos) - 1})
	c.list.SetRows(rows)
}

// Len is how many lines there are to pick from.
func (c *Chooser) Len() int { return len(c.dos) }

// Rows returns what the chooser is showing, for a caller checking what
// it offered.
func (c *Chooser) Rows() []ListRow { return c.list.Rows() }

// Take runs the line at i, the way Enter on it would.
func (c *Chooser) Take(i int) error {
	if i < 0 || i >= len(c.dos) {
		return nil
	}
	return c.run(c.dos[i])
}

// take runs what a line means.
func (c *Chooser) take(row ListRow) error {
	i, ok := row.Key.(int)
	if !ok {
		return nil
	}
	return c.Take(i)
}

// run dismisses the chooser and then does the thing.
//
// That way round, because what was picked may open a dialog of its own:
// closing one takes everything stacked above it, so a chooser still up
// would take that dialog down with it.
func (c *Chooser) run(do func() error) error {
	c.dismiss()
	if do == nil {
		return nil
	}
	return do()
}

// dismiss takes the chooser away, once.
func (c *Chooser) dismiss() {
	close := c.close
	c.close = nil
	if close != nil {
		close()
	}
}

// Box returns where the chooser sits, so whatever is showing it can
// treat that part differently.
func (c *Chooser) Box() Rect { return c.box() }

// box returns where the chooser goes, centred in the area it was given.
func (c *Chooser) box() Rect {
	if c.size.Empty() || len(c.dos) == 0 {
		return Rect{}
	}
	cols := min(c.size.Cols-chooserMargin*2, c.width())
	// The title, a blank row, the lines, and the rule at each end.
	rows := min(c.size.Rows-chooserMargin*2,
		min(len(c.dos), chooserMaxRow)+2+chooserFrame*2)
	// Room for the rule, the title, its blank row and a line to pick.
	if cols < chooserMinCol || rows < 3+chooserFrame*2 {
		return Rect{}
	}
	return Rect{
		X:    (c.size.Cols - cols) / 2,
		Y:    (c.size.Rows - rows) / 2,
		Cols: cols,
		Rows: rows,
	}
}

// width is how wide the chooser would like to be.
func (c *Chooser) width() int {
	want := grid.StringWidth(c.title)
	for _, row := range c.list.Rows() {
		w := grid.StringWidth(row.Text)
		if row.Note != "" {
			w += 2 + grid.StringWidth(row.Note)
		}
		want = max(want, w)
	}
	return min(max(want+(chooserFrame+chooserPad)*2, chooserMinCol), chooserMaxCol)
}

// lines is the part of the box the list goes in.
func (c *Chooser) lines() Rect {
	box := c.box()
	if box.Empty() {
		return box
	}
	// The rule, the title, and the blank row under it.
	top := chooserFrame + 2
	return Rect{
		X: box.X + chooserFrame + chooserPad, Y: box.Y + top,
		Cols: max(box.Cols-(chooserFrame+chooserPad)*2, 0),
		Rows: max(box.Rows-top-chooserFrame, 0),
	}
}

// Layout notes how much room the chooser has to place itself in.
func (c *Chooser) Layout(size Size) {
	c.size = size
	c.list.Layout(c.lines().Size())
}

// Draw paints the chooser over whatever is behind it.
func (c *Chooser) Draw(v grid.View) { c.buf.draw(v, c.paint) }

func (c *Chooser) paint(v grid.View) {
	box := c.box()
	if box.Empty() {
		return
	}
	drawShadow(v, box, c.Style.ShadowBG)
	in := box.In(v)
	in.Fill(grid.Cell{Rune: ' ', FG: c.Style.FG, BG: c.Style.BG, Width: 1})
	drawFrame(v, box, c.Style.BorderFG, c.Style.BG)

	cols, _ := in.Size()
	room := max(cols-(chooserFrame+chooserPad)*2, 0)
	in.SetString(chooserFrame+chooserPad, chooserFrame,
		trimTo(c.title, room), c.Style.TitleFG, c.Style.BG, grid.AttrBold)

	c.list.Style = ListStyle{
		FG: c.Style.FG, BG: c.Style.BG,
		SelectedFG: c.Style.SelectedFG, SelectedBG: c.Style.SelectedBG,
		HeaderFG: c.Style.TitleFG, NoteFG: c.Style.NoteFG,
	}
	if lines := c.lines(); !lines.Empty() {
		c.list.Draw(lines.In(v))
	}
}

// SetFocus passes the keys on to the list, which is what they are for.
//
// A chooser is the top modal while it is up, but it is still told when
// focus leaves: a dialog pushed over it takes the keys, and a chooser
// still drawing an active bar under that dialog would say two things
// have them.
func (c *Chooser) SetFocus(on bool) { c.list.SetFocus(on) }

// HandleKey drives the chooser. Escape leaves without picking anything.
func (c *Chooser) HandleKey(ev input.Event) (bool, error) {
	if c.box().Empty() {
		// Nowhere to draw it, so there is nothing on screen to pick
		// from. It is still the top modal, so Enter here would take a
		// line nobody has read.
		if ev.Kind == input.KeyPress && ev.Key == input.KeyEscape && isPlainKey(ev) {
			c.dismiss()
		}
		return true, nil
	}
	if ev.Kind == input.KeyPress && ev.Key == input.KeyEscape && isPlainKey(ev) {
		c.dismiss()
		return true, nil
	}
	// The list declines every chord it was not offered, so Ctrl+Down
	// moves nothing here.
	if took, err := c.list.HandleKey(ev); took {
		return true, err
	}
	// Everything else is swallowed: the chooser is a question, and a key
	// reaching a pane behind it would be typed into something the user
	// cannot see.
	return true, nil
}

// HandleMouse takes the line that was clicked. A press outside closes
// the chooser without picking anything.
func (c *Chooser) HandleMouse(ev input.MouseEvent) (bool, error) {
	if ev.Kind != input.MousePress || ev.Button.IsWheel() {
		// A release or a drag is swallowed rather than acted on: the
		// press that opened this was released over it.
		return true, nil
	}
	box := c.box()
	if box.Empty() || !box.Contains(ev.Col, ev.Row) {
		c.dismiss()
		return true, nil
	}
	lines := c.lines()
	if lines.Empty() || !lines.Contains(ev.Col, ev.Row) {
		// The rule, the title, or the blank under it: none of them is a
		// line to take.
		return true, nil
	}
	ev.Col, ev.Row = lines.Local(ev.Col, ev.Row)
	return c.list.HandleMouse(ev)
}

// CancelGesture is here because the chooser swallows drags: it keeps
// nothing between a press and its release, and saying so keeps the rule
// visible.
func (c *Chooser) CancelGesture() {}
