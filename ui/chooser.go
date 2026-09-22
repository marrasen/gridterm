package ui

import (
	"image/color"
	"slices"

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

	// ButtonFG and ButtonBG are a button along the bottom, and ActiveFG
	// and ActiveBG the one Enter would press. The same pair a form and
	// a notice draw with, because they are the same button.
	ButtonFG, ButtonBG color.RGBA
	ActiveFG, ActiveBG color.RGBA

	// ButtonShadowBG darkens the cells below and to the right of each
	// button. A zero alpha leaves it out, and the chooser is a row
	// shorter for it.
	ButtonShadowBG color.RGBA

	// BorderFG is the rule around the outside, and ShadowBG darkens the
	// cells it falls on below and to the right.
	BorderFG color.RGBA
	ShadowBG color.RGBA

	// Rule picks the characters the rule is drawn with.
	Rule Border
}

// A menu names commands, so that a menu, a key binding and the palette
// are three ways into one list. A chooser is for picking one of a set of
// things that exist only right now -- a pane to move, a machine to open
// on -- which no command can name, because they come and go while the
// program runs.
//
// chooserLine is one thing to pick, or a heading over the lines under
// it. A heading has no do and is never taken.
type chooserLine struct {
	text, note string
	under      string
	do         func() error
}

// ChooserAction is a button along the bottom of a chooser.
//
// Left and right move between them, and Enter runs the one that is
// highlighted on the line that is picked. Do is given the line's index.
type ChooserAction struct {
	Label string
	Do    func(i int) error
}

// Chooser is a modal list of things to pick one of.
type Chooser struct {
	Style ChooserStyle

	// Button is a character drawn at the end of every line but a
	// heading, and OnPress is what clicking it does with the line it was
	// on. A chooser with no OnPress draws none.
	Button  rune
	OnPress func(i int) error

	// Actions are the buttons along the bottom. With none, Enter takes
	// the line and the chooser is the plain list it has always been.
	Actions []ChooserAction

	// Filter draws what has been typed on a row of its own, for a list
	// long enough that narrowing it is the way in rather than an aside.
	// Without it the letters go beside the title, which is enough for a
	// list of three.
	Filter bool

	title string
	list  *List
	lns   []chooserLine
	dos   []func() error
	close func()

	// act is the action the arrows have landed on, and what Enter runs.
	act int

	// query is what has been typed to narrow the list, and under is the
	// heading the next line added goes under.
	query string
	under string

	size Size
	buf  buffer
}

// NewChooser returns an empty chooser. close is called when it is
// finished with, which is what takes it off the modal stack: the chooser
// does not know what is showing it.
func NewChooser(title string, close func()) *Chooser {
	c := &Chooser{title: title, close: close, list: NewList()}
	c.list.OnActivate = func(row ListRow) error { return c.take(row) }
	c.list.OnButton = func(row ListRow) error { return c.press(row) }
	// The keys arrive with the chooser: a modal is pushed and focused in
	// one go, and a list that had to wait to be told would open with no
	// line marked.
	c.list.SetFocus(true)
	return c
}

// Add puts a line at the bottom, under the last heading. note is shown
// at its end, and do is what taking the line does.
func (c *Chooser) Add(text, note string, do func() error) {
	c.dos = append(c.dos, do)
	c.lns = append(c.lns, chooserLine{text: text, note: note, under: c.under, do: do})
	c.fill()
}

// Under starts a heading. Every line added after it sits under it, until
// the next one. An empty name puts the lines back at the top level.
func (c *Chooser) Under(heading string) {
	c.under = heading
}

// Query is what has been typed to narrow the list.
func (c *Chooser) Query() string { return c.query }

// fill builds the rows from the lines the query keeps, dropping a
// heading with nothing left under it.
func (c *Chooser) fill() {
	var rows []ListRow
	heading := ""
	for i, ln := range c.lns {
		if _, _, ok := matchTitle(c.query, ln.text); !ok {
			continue
		}
		// A heading is written when the first line under it is kept, so
		// one with nothing left never appears.
		if ln.under != heading {
			heading = ln.under
			if heading != "" {
				rows = append(rows, ListRow{Text: heading, Header: true, Key: "head:" + heading})
			}
		}
		depth := 0
		if ln.under != "" {
			depth = 1
		}
		row := ListRow{Text: ln.text, Note: ln.note, Depth: depth, Key: i}
		if c.OnPress != nil {
			row.Button = c.Button
		}
		rows = append(rows, row)
	}
	c.list.SetRows(rows)
}

// Selected is the line the bar is on, and whether it is on one.
func (c *Chooser) Selected() (ListRow, bool) { return c.list.Selected() }

// Select puts the bar on the line at i, and reports whether there is
// one to put it on.
func (c *Chooser) Select(i int) bool { return c.list.Select(i) }

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

// Press does to the line at i what its button does.
func (c *Chooser) Press(i int) error {
	if c.OnPress == nil || i < 0 || i >= len(c.dos) {
		return nil
	}
	return c.OnPress(i)
}

// press does what a line's button means, leaving the chooser up so the
// user can press another.
func (c *Chooser) press(row ListRow) error {
	i, ok := row.Key.(int)
	if !ok {
		return nil
	}
	return c.Press(i)
}

// Forget takes the line at i off the list, leaving the bar on the line
// it was on.
//
// A line's key is where it sits, so taking one out moves every key after
// it down one and the bar would otherwise land on the next line.
func (c *Chooser) Forget(i int) {
	if i < 0 || i >= len(c.lns) {
		return
	}
	on := -1
	if row, ok := c.list.Selected(); ok {
		if key, isLine := row.Key.(int); isLine {
			on = key
		}
	}
	c.lns = slices.Delete(c.lns, i, i+1)
	c.dos = slices.Delete(c.dos, i, i+1)
	c.fill()
	switch {
	case on < 0 || on == i:
	case on > i:
		c.list.Select(on - 1)
	default:
		c.list.Select(on)
	}
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

// narrow sets what has been typed and builds the list again.
func (c *Chooser) narrow(q string) {
	if q == c.query {
		return
	}
	c.query = q
	c.fill()
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
	showing := max(len(c.list.Rows()), 1)
	cols := min(c.size.Cols-chooserMargin*2, c.width())
	// The title, a blank row, the lines, and the rule at each end, plus
	// whatever the filter row and the action bar ask for.
	rows := min(c.size.Rows-chooserMargin*2,
		min(showing, chooserMaxRow)+2+c.aboveLines()+c.belowLines()+chooserFrame*2)
	// Room for the rule, the title, its blank row and a line to pick.
	if cols < chooserMinCol || rows < 3+c.aboveLines()+c.belowLines()+chooserFrame*2 {
		return Rect{}
	}
	return Rect{
		X:    (c.size.Cols - cols) / 2,
		Y:    (c.size.Rows - rows) / 2,
		Cols: cols,
		Rows: rows,
	}
}

// aboveLines is how many rows the filter takes between the title's
// blank row and the list.
func (c *Chooser) aboveLines() int {
	if !c.Filter {
		return 0
	}
	return 1
}

// belowLines is how many rows the action bar takes under the list: a
// blank row, the buttons, and the row their shadow falls on.
func (c *Chooser) belowLines() int {
	if len(c.Actions) == 0 {
		return 0
	}
	return 2 + c.shadowRows()
}

// shadowRows is the row a shadow under the buttons needs, and none when
// the theme casts none.
func (c *Chooser) shadowRows() int {
	if c.Style.ButtonShadowBG.A == 0 {
		return 0
	}
	return 1
}

// actionTitles is what the buttons say, which is all the layout needs
// to know about them.
func (c *Chooser) actionTitles() []string {
	out := make([]string, 0, len(c.Actions))
	for _, a := range c.Actions {
		out = append(out, a.Label)
	}
	return out
}

// actionPad is the blank column each side of the button row, which is
// the rule and the padding the rest of the box is drawn inside.
const actionPad = chooserFrame + chooserPad

// actionCols is the column each button starts at, or -1 for one there
// was no room for. Laid out from the right, so the last is nearest the
// corner the eye lands on.
func (c *Chooser) actionCols() []int {
	return ButtonColsIn(c.actionTitles(), c.box().Cols, actionPad)
}

// actionRow is the row inside the box the buttons are drawn on.
func (c *Chooser) actionRow(rows int) int {
	return rows - chooserFrame - 1 - c.shadowRows()
}

// width is how wide the chooser would like to be.
func (c *Chooser) width() int {
	want := grid.StringWidth(c.title)
	if len(c.Actions) > 0 {
		// The whole row of them. The padding added below is what leaves
		// the blank column each side that ButtonColsIn lays out within.
		want = max(want, buttonsWidth(c.actionTitles()))
	}
	for _, row := range c.list.Rows() {
		w := grid.StringWidth(row.Text)
		if row.Note != "" {
			w += 2 + grid.StringWidth(row.Note)
		}
		want = max(want, w)
	}
	if c.OnPress != nil {
		// The button is drawn over the last two columns, so a line as
		// wide as the box would lose its end to it.
		want += 2
	}
	return min(max(want+(chooserFrame+chooserPad)*2, chooserMinCol), chooserMaxCol)
}

// lines is the part of the box the list goes in.
func (c *Chooser) lines() Rect {
	box := c.box()
	if box.Empty() {
		return box
	}
	// The rule, the title, the blank row under it, and the filter row
	// when there is one.
	top := chooserFrame + 2 + c.aboveLines()
	return Rect{
		X: box.X + chooserFrame + chooserPad, Y: box.Y + top,
		Cols: max(box.Cols-(chooserFrame+chooserPad)*2, 0),
		Rows: max(box.Rows-top-chooserFrame-c.belowLines(), 0),
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
	drawFrame(v, box, c.Style.BorderFG, c.Style.BG, c.Style.Rule)

	cols, _ := in.Size()
	room := max(cols-(chooserFrame+chooserPad)*2, 0)
	left := chooserFrame + chooserPad
	title := c.title
	if c.query != "" && !c.Filter {
		title += "  " + c.query
	}
	in.SetString(left, chooserFrame,
		grid.Trim(title, room), c.Style.TitleFG, c.Style.BG, grid.AttrBold)
	if c.Filter {
		c.paintFilter(in, left, chooserFrame+2, room)
	}
	if len(c.Actions) > 0 {
		_, rows := in.Size()
		c.paintActions(in, c.actionRow(rows))
	}

	c.list.Style = ListStyle{
		FG: c.Style.FG, BG: c.Style.BG,
		SelectedFG: c.Style.SelectedFG, SelectedBG: c.Style.SelectedBG,
		HeaderFG: c.Style.TitleFG, NoteFG: c.Style.NoteFG,
	}
	if lines := c.lines(); !lines.Empty() {
		c.list.Draw(lines.In(v))
	}
}

// paintFilter draws the row the typing narrows the list from.
//
// A prompt mark and what has been typed, with a hint in its place while
// nothing has been: a blank row says nothing about what typing would do.
func (c *Chooser) paintFilter(in grid.View, x, y, room int) {
	in.SetString(x, y, grid.Trim("> ", room), c.Style.NoteFG, c.Style.BG, 0)
	rest := max(room-2, 0)
	if c.query == "" {
		in.SetString(x+2, y, grid.Trim("type to narrow the list", rest),
			c.Style.NoteFG, c.Style.BG, 0)
		return
	}
	in.SetString(x+2, y, grid.Trim(c.query, rest), c.Style.FG, c.Style.BG, 0)
}

// paintActions draws the buttons along the bottom, right aligned, the
// one the arrows have landed on picked out.
//
// The same buttons a form and a notice draw, through the same helpers:
// a row of names in brackets was a different thing to learn in a dialog
// that answers to the same keys.
func (c *Chooser) paintActions(in grid.View, y int) {
	titles := c.actionTitles()
	for i, at := range c.actionCols() {
		if at < 0 {
			// No room for this one. Drawing it would land it on top of
			// the buttons that did fit.
			continue
		}
		fg, bg := c.Style.ButtonFG, c.Style.ButtonBG
		if i == c.action() {
			fg, bg = c.Style.ActiveFG, c.Style.ActiveBG
		}
		DrawButtonShadow(in, at, y, ButtonWidth(titles[i]), c.Style.ButtonShadowBG, c.Style.BG)
		DrawButton(in, at, y, titles[i], fg, bg)
	}
}

// action is which button the arrows have landed on, kept inside the
// list however the actions have changed since.
func (c *Chooser) action() int {
	if len(c.Actions) == 0 {
		return 0
	}
	return min(max(c.act, 0), len(c.Actions)-1)
}

// runAction runs the highlighted button on the line that is picked.
func (c *Chooser) runAction() error {
	row, ok := c.list.Selected()
	if !ok || len(c.Actions) == 0 {
		return nil
	}
	at, ok := row.Key.(int)
	if !ok {
		return nil
	}
	do := c.Actions[c.action()].Do
	if do == nil {
		return nil
	}
	// Through run, so the chooser goes before the action shows anything
	// of its own, the way taking a line has always worked.
	return c.run(func() error { return do(at) })
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
		if c.query != "" {
			// The letters first: Escape on a narrowed list puts the
			// whole list back before it puts the chooser away.
			c.narrow("")
			return true, nil
		}
		c.dismiss()
		return true, nil
	}
	if ev.Kind == input.KeyPress && ev.Key == input.KeyBackspace && isPlainKey(ev) {
		if r := []rune(c.query); len(r) > 0 {
			c.narrow(string(r[:len(r)-1]))
		}
		return true, nil
	}
	// The buttons before the list: left and right are the list's to
	// decline, but Enter is not, and with a bar on screen Enter means
	// the button that is highlighted.
	if len(c.Actions) > 0 && ev.Kind != input.Text && isPlainKey(ev) &&
		(ev.Kind == input.KeyPress || ev.Kind == input.KeyRepeat) {

		switch ev.Key {
		case input.KeyLeft:
			c.act = (c.action() + len(c.Actions) - 1) % len(c.Actions)
			return true, nil
		case input.KeyRight:
			c.act = (c.action() + 1) % len(c.Actions)
			return true, nil
		case input.KeyEnter:
			return true, c.runAction()
		}
	}
	// The list declines every chord it was not offered, so Ctrl+Down
	// moves nothing here.
	if took, err := c.list.HandleKey(ev); took {
		return true, err
	}
	if ev.Kind == input.Text && ev.Rune >= ' ' {
		c.narrow(c.query + string(ev.Rune))
		return true, nil
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
	// A button under the pointer, before the list: the buttons sit
	// below it, and one drawn is one that can be pressed.
	if len(c.Actions) > 0 {
		x, y := box.Local(ev.Col, ev.Row)
		if y == c.actionRow(box.Rows) {
			if at, on := ButtonAtCol(c.actionTitles(), box.Cols, actionPad, x); on {
				c.act = at
				return true, c.runAction()
			}
			return true, nil
		}
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

// Title is what the chooser draws at its top.
func (c *Chooser) Title() string { return c.title }
