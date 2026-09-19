package ui

import (
	"errors"
	"image/color"
	"slices"

	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/input"
)

// How large a form is allowed to get, and how much of the area it leaves
// around itself.
const (
	formMaxCols = 76

	// formFieldCols is the room a field asks for beside its label.
	formFieldCols = 44
	formMargin    = 2

	// formPad is the blank column each side of the text inside the box.
	formPad = 2

	// formLabelGap separates a label from the field beside it.
	formLabelGap = 1
)

// FormStyle colours a form.
type FormStyle struct {
	// FG and BG are the body text and the box behind it. A background
	// with no alpha lets a frosted panel show through.
	FG, BG color.RGBA

	// TitleFG is the name at the top, LabelFG the name beside a field,
	// and HintFG the explanatory lines under the title.
	TitleFG, LabelFG, HintFG color.RGBA

	// FieldFG and FieldBG are a field that is not being typed into, and
	// FocusFG and FocusBG the one that is.
	FieldFG, FieldBG color.RGBA
	FocusFG, FocusBG color.RGBA

	// ButtonFG and ButtonBG are a button, and ActiveFG and ActiveBG the
	// one Enter would press.
	ButtonFG, ButtonBG color.RGBA
	ActiveFG, ActiveBG color.RGBA

	// ErrorFG is the line that says why the last attempt failed.
	ErrorFG color.RGBA

	// BorderFG is the rule around the outside, and ShadowBG darkens the
	// cells it falls on below and to the right. A zero alpha leaves
	// either one out.
	BorderFG color.RGBA
	ShadowBG color.RGBA

	// Rule picks the characters the rule is drawn with.
	Rule Border

	// ButtonShadowBG darkens the cells below and to the right of each
	// button, the way a DOS program drew one. A zero alpha leaves it
	// out, and the dialog is a row shorter for it.
	ButtonShadowBG color.RGBA
}

// Button is something to press at the bottom of a form.
//
// Do runs on the drawing goroutine. Returning an error leaves the form
// open with the error shown, which is what a connection that failed
// wants; returning nil means the button is finished and the form closes.
//
// Keep leaves the form open after Do worked, for a button the user may
// want to press alongside another one.
type Button struct {
	Title string
	Do    func() error
	Keep  bool
}

// formRow is one labelled field.
type formRow struct {
	label string
	field *Field
}

// Form is a dialog of labelled fields and buttons.
//
// It is not a Container. Container.Remove collapses a parent into its
// last child, which is right for a pane that was split and wrong for a
// form whose rows are fixed, so a form routes keys to its own rows
// instead of being walked like a tree.
type Form struct {
	Style FormStyle

	// Title names the dialog, and Lines say more underneath it. A host
	// key fingerprint is the case Lines exists for.
	//
	// Both are read while the dialog is being built. Changing either
	// once it is open does not re-lay the fields, which is what the
	// width they were given comes from. SetLines is how a dialog whose
	// text changes while it is open says so.
	Title string
	Lines []string

	// MinCols is the least room the text inside the box may have, for a
	// dialog whose lines change while it is open: without it the box
	// follows the longest line and re-centres itself every time a number
	// in it grows a digit.
	MinCols int

	// Copy puts text on the clipboard, and CopyChord reports whether a
	// key is the window's copy chord. A nil pair leaves the error on
	// screen and nowhere else: this package cannot reach a clipboard or
	// see a keymap.
	Copy      func(string)
	CopyChord func(input.Event) bool

	// Copyable is what the copy chord copies when the form is showing no
	// error, for a dialog whose point is a line the user has to run
	// somewhere else. Empty leaves the chord with nothing to copy.
	Copyable string

	rows    []formRow
	buttons []Button

	// titles are the buttons' titles, kept alongside them because the
	// layout asks for them several times a frame.
	titles []string

	// cols is where each button starts, worked out again on every frame
	// and kept so that working it out costs no allocation.
	cols []int

	// at is what has focus: a row while it is below len(rows), and a
	// button after that.
	at int

	err error

	// errText is the error as it may be drawn, and wrapped is it broken
	// to wrappedAt columns. Both are thrown away by SetError and by a
	// Layout that changes the width.
	errText   string
	wrapped   []string
	wrappedAt int

	size  Size
	close func()
	buf   buffer
}

// NewForm returns an empty form. close is called when the dialog is
// finished with, which is what takes it off the modal stack: the form
// does not know what is showing it.
func NewForm(title string, close func()) *Form {
	return &Form{Title: title, close: close}
}

// SetClose says what takes the dialog away.
//
// A dialog cannot be built with it: whatever shows the dialog only has
// something to hand back once the dialog exists.
func (f *Form) SetClose(close func()) { f.close = close }

// NewConfirm returns a form with no fields: a question and the buttons
// that answer it.
func NewConfirm(title string, lines []string, close func()) *Form {
	f := NewForm(title, close)
	f.Lines = lines
	return f
}

// SetLines replaces the lines under the title, for a dialog whose text
// changes while it is open.
//
// The fields are laid out again, because how much room they have comes
// from a width the lines are part of.
func (f *Form) SetLines(lines []string) {
	f.Lines = lines
	f.layoutFields()
}

// AddField puts a labelled field at the bottom of the form and returns
// it, so a caller can read what was typed without keeping its own list.
func (f *Form) AddField(label string, field *Field) *Field {
	if field == nil {
		field = NewField()
	}
	f.rows = append(f.rows, formRow{label: label, field: field})
	// The first field is what the form opens on, or it would open with
	// the caret on a button and nowhere to type. The focus itself
	// arrives with the dialog, through SetFocus.
	if len(f.rows) == 1 {
		f.at = 0
	}
	return field
}

// AddTick puts a tick box at the bottom of the fields. Space turns it
// over, as do the keys that step through a field's options.
func (f *Form) AddTick(label string, on bool) *Field {
	return f.AddField(label, NewTick(on))
}

// AddButton puts a button at the bottom. The first one added is the one
// Enter presses from a field, and the one that has the focus when the
// form has no fields.
//
// Do must not open another dialog. The form closes after it, and closing
// a dialog takes anything stacked on top of it -- which would be the
// dialog Do had just opened. Post the work instead, so it happens once
// this form has gone.
func (f *Form) AddButton(b Button) {
	f.buttons = append(f.buttons, b)
	f.titles = append(f.titles, b.Title)
}

// SetButtons replaces the buttons, for a dialog whose choices change
// while it is open.
//
// The focus stays where it is, unless there are fewer buttons than
// there were and it was on one of the ones that went.
func (f *Form) SetButtons(buttons []Button) {
	// A fresh list rather than the old one refilled: Buttons hands the
	// slice itself out, and a caller holding one would find it changed
	// under them.
	f.buttons, f.titles = nil, nil
	for _, b := range buttons {
		f.AddButton(b)
	}
	if n := len(f.rows) + len(f.buttons); f.at >= n {
		f.focus(max(n-1, 0))
	}
	// The buttons are part of what the box is wide enough for, so the
	// fields have to be told what they have left.
	f.layoutFields()
}

// FocusButton puts the focus on one of the buttons, for a question whose
// safe answer should be the one already under the finger.
func (f *Form) FocusButton(at int) {
	if at < 0 || at >= len(f.buttons) {
		return
	}
	f.focus(len(f.rows) + at)
}

// Fields returns the fields in the order they were added.
func (f *Form) Fields() []*Field {
	out := make([]*Field, len(f.rows))
	for i, r := range f.rows {
		out[i] = r.field
	}
	return out
}

// Field returns the field with a label, or nil when there is none.
//
// By label rather than by position, so adding a row does not move every
// caller that used the ones after it.
func (f *Form) Field(label string) *Field {
	for _, r := range f.rows {
		if r.label == label {
			return r.field
		}
	}
	return nil
}

// Buttons returns the buttons in the order they were added.
func (f *Form) Buttons() []Button { return f.buttons }

// Focused returns what has focus: the index, and whether it is a button
// rather than a field.
func (f *Form) Focused() (int, bool) {
	if f.at < len(f.rows) {
		return f.at, false
	}
	return f.at - len(f.rows), true
}

// Error returns what the last attempt failed with.
func (f *Form) Error() error { return f.err }

// SetError shows why the last attempt failed, or clears it when err is
// nil.
//
// The text is cleaned, because a far end can put anything in an error
// and this is drawn into a grid. An error also changes the shape of the
// box, so the fields are laid out again: a field scrolls to keep the
// caret in view and can only do that once it knows how much room it has.
func (f *Form) SetError(err error) {
	f.err = err
	f.errText = ""
	if err != nil {
		f.errText = cleanText(err.Error())
	}
	f.wrapped, f.wrappedAt = nil, 0
	f.layoutFields()
}

// ErrorText is the error as the dialog shows it, cleaned of anything a
// grid cannot draw.
func (f *Form) ErrorText() string { return f.errText }

// CopyNow puts the error on the clipboard, or Copyable when the form is
// showing no error, for the window's copy chord.
//
// The error comes first: a connection that failed says why in words the
// user will want to paste somewhere, and that is the reason they reached
// for the chord.
func (f *Form) CopyNow() {
	if f.Copy == nil {
		return
	}
	if text := f.CopyText(); text != "" {
		f.Copy(text)
	}
}

// CopyText is what the copy chord would put on the clipboard.
func (f *Form) CopyText() string {
	if f.errText != "" {
		return f.errText
	}
	return f.Copyable
}

// Box returns where the dialog sits in the view it draws through, so
// whatever is showing it can treat that part differently.
func (f *Form) Box() Rect { return f.box() }

// Layout notes how much room the dialog has to place itself in.
func (f *Form) Layout(size Size) {
	f.size = size
	f.layoutFields()
}

// wrapWidth is the width the error is wrapped to.
func (f *Form) wrapWidth() int { return f.boxCols() - formPad*2 }

// SetFocus passes focus on to the field that has it, so the caret
// appears and disappears with the dialog.
func (f *Form) SetFocus(on bool) {
	if fld := f.focusedField(); fld != nil {
		fld.SetFocus(on)
	}
}

// HandleKey drives the dialog. The focused field sees a key first, so
// typing goes where the caret is; what it does not want moves the focus
// or presses a button.
//
// Every other key is swallowed, the way a modal does (see KeyHandler).
// The copy chord is the exception: the form answers that itself. Paste
// is not one, because the field takes that chord before this sees it.
func (f *Form) HandleKey(ev input.Event) (bool, error) {
	if f.CopyChord != nil && f.CopyChord(ev) {
		// A field with something picked out copies that: the chord means
		// copy what is selected, and the dialog's own text is what there
		// is to copy when nothing is.
		if fld := f.focusedField(); fld != nil && fld.Copy() {
			return true, nil
		}
		f.CopyNow()
		return true, nil
	}
	// A dialog with no room to be drawn takes nothing but Escape. It is
	// still the top modal, so typing would land in a field the user
	// cannot see, and Enter would send it wherever the dialog goes.
	if f.box().Empty() {
		if ev.Kind == input.KeyPress && ev.Key == input.KeyEscape && isPlainKey(ev) {
			f.dismiss()
		}
		return true, nil
	}
	if fld := f.focusedField(); fld != nil {
		if took, err := fld.HandleKey(ev); took {
			return true, err
		}
	}
	if ev.Kind != input.KeyPress && ev.Kind != input.KeyRepeat {
		return true, nil
	}
	if !isPlainKey(ev) {
		return true, nil
	}

	switch ev.Key {
	case input.KeyEscape:
		f.dismiss()
		return true, nil
	case input.KeyTab:
		if ev.Shift() {
			f.move(-1)
		} else {
			f.move(1)
		}
		return true, nil
	case input.KeyUp:
		f.move(-1)
		return true, nil
	case input.KeyDown:
		f.move(1)
		return true, nil
	case input.KeyEnter, input.KeySpace:
		// Only a fresh press. A dialog opens from the pump and input is
		// polled in the same frame, so the repeats of the key that
		// opened it would otherwise answer it before it is read.
		if ev.Kind != input.KeyPress {
			return true, nil
		}
		// Space presses the focused button, but only a button: in a
		// field it is a character to type, which the field took above.
		if at, isButton := f.Focused(); isButton {
			return true, f.press(at)
		}
		if ev.Key == input.KeyEnter && len(f.buttons) > 0 {
			// Enter from a field means the first button, which is what
			// the form is for.
			return true, f.press(0)
		}
	}
	// Everything else is swallowed, because the dialog covers what is
	// behind it: a key handed on would reach an accelerator that types
	// into a pane the user cannot see.
	return true, nil
}

// HandleMouse moves focus to what was clicked and presses a button.
func (f *Form) HandleMouse(ev input.MouseEvent) (bool, error) {
	if ev.Kind != input.MousePress || ev.Button.IsWheel() {
		// Everything else is swallowed: the dialog covers the whole
		// area, and letting a drag through would select text behind it
		// that nobody can see.
		return true, nil
	}
	box := f.box()
	if box.Empty() || !box.Contains(ev.Col, ev.Row) {
		f.dismiss()
		return true, nil
	}

	x, y := box.Local(ev.Col, ev.Row)
	if row := y - f.rowsTop(); row >= 0 && row < len(f.rows) {
		f.focus(row)
		fld := f.rows[row].field
		// A tick box is turned over by clicking it, which is what a box
		// drawn on screen looks like it does.
		if fld.Tick {
			fld.Toggle()
			return true, nil
		}
		// The caret goes where it was clicked, so a long value can be
		// corrected in the middle rather than only at the end.
		fld.SetCaret(f.caretFor(fld, x-f.fieldX()))
		return true, nil
	}
	if y == f.buttonsRow() {
		if at, ok := f.buttonAt(x); ok {
			f.focus(len(f.rows) + at)
			return true, f.press(at)
		}
	}
	return true, nil
}

// CancelGesture is here because the dialog swallows drags: nothing is
// held between a press and a release, and saying so keeps the rule
// visible.
func (f *Form) CancelGesture() {}

// Draw paints the box over whatever is behind it.
func (f *Form) Draw(v grid.View) { f.buf.draw(v, f.paint) }

func (f *Form) paint(v grid.View) {
	box := f.box()
	if box.Empty() {
		return
	}
	l := f.layout()
	drawShadow(v, box, f.Style.ShadowBG)
	in := box.In(v)
	in.Fill(grid.Cell{Rune: ' ', FG: f.Style.FG, BG: f.Style.BG, Width: 1})
	// The rule goes on the blank ring the layout already leaves: a row
	// above the title, a row under the buttons, and the pad each side.
	drawFrame(v, box, f.Style.BorderFG, f.Style.BG, f.Style.Rule)
	cols, _ := in.Size()
	room := cols - formPad*2

	in.SetString(formPad, l.title, grid.Trim(f.Title, room), f.Style.TitleFG, f.Style.BG, grid.AttrBold)

	for i, line := range l.lines {
		in.SetString(formPad, l.linesTop+i, grid.Trim(line, room), f.Style.HintFG, f.Style.BG, 0)
	}

	for i := 0; i < l.fields; i++ {
		r := f.rows[i]
		y := l.fieldsTop + i
		in.SetString(formPad, y, grid.Trim(r.label, room), f.Style.LabelFG, f.Style.BG, 0)
		fg, bg := f.Style.FieldFG, f.Style.FieldBG
		if i == f.at {
			fg, bg = f.Style.FocusFG, f.Style.FocusBG
		}
		r.field.Style = FieldStyle{FG: fg, BG: bg, PlaceholderFG: f.Style.HintFG}
		width := max(room-f.fieldX()+formPad, 1)
		r.field.Draw(in.Sub(f.fieldX(), y, width, 1))
	}

	for i, line := range l.errLines {
		in.SetString(formPad, l.errRow+i, line, f.Style.ErrorFG, f.Style.BG, 0)
	}
	f.paintButtons(in, l.buttonRow)
}

// formLayout is where each part of the dialog goes in the box it got.
//
// Every row comes from here, so what is drawn, what a click lands on and
// what the buttons sit above cannot disagree. Working them out twice is
// what let the buttons be painted over a field.
type formLayout struct {
	title     int
	linesTop  int
	lines     []string // as many hint lines as there was room for
	fieldsTop int
	fields    int // how many fields fit
	errRow    int
	errLines  []string // the error, wrapped
	buttonRow int
}

// errLines is the error wrapped to the width it is drawn in, no more
// lines than rows.
//
// Wrapped once per width rather than once per frame: the box is scanned
// to work out how tall it is and again to draw it, and an error from a
// far end can be long.
func (f *Form) errLines(room, rows int) []string {
	if f.err == nil || room <= 0 || rows <= 0 {
		return nil
	}
	if f.wrapped == nil || f.wrappedAt != room {
		lines := wrapText(f.errText, room, false)
		f.wrapped = make([]string, len(lines))
		for i, line := range lines {
			f.wrapped[i] = line.text
		}
		f.wrappedAt = room
	}
	return f.wrapped[:min(len(f.wrapped), rows)]
}

// layout divides the box up. The fields always fit -- box() refuses to
// open a dialog with no room for them -- and the hint lines are what
// gets dropped when the window is short.
func (f *Form) layout() formLayout {
	var l formLayout
	box := f.box()
	if box.Empty() {
		return l
	}
	// From the bottom: the rule, the buttons, then the error, which is
	// one line unless it needs more and the box has room for more. A
	// shadow under the buttons wants the row between them and the rule.
	l.buttonRow = box.Rows - 2 - f.shadowRows()
	// A blank row, the title, a blank row, then the fields: what is
	// between that and the buttons is what the error may have.
	above := 3
	if len(f.rows) > 0 {
		above += len(f.rows) + 1
	}
	l.errLines = f.errLines(box.Cols-formPad*2, max(l.buttonRow-above, 1))
	l.errRow = l.buttonRow - max(len(l.errLines), 1)

	// From the top: a blank row, the title, a blank row.
	l.title = 1
	y := 3
	l.linesTop = y

	// The fields are spoken for before the hint lines get any room:
	// box() promised every field would fit, and a hint is what the
	// dialog will do without.
	fields := len(f.rows)
	if fields > 0 {
		fields++ // the blank row under them
	}
	for _, line := range f.Lines {
		if y+1 >= l.errRow-fields {
			break
		}
		l.lines = append(l.lines, line)
		y++
	}
	if len(l.lines) > 0 {
		y++
	}

	l.fieldsTop = y
	for range f.rows {
		if y >= l.errRow {
			break
		}
		l.fields++
		y++
	}
	return l
}

// paintButtons draws the buttons in a row along the bottom, right
// aligned so the one Enter presses is nearest the corner the eye lands
// on.
func (f *Form) paintButtons(in grid.View, y int) {
	for i, at := range f.buttonCols() {
		if at < 0 {
			// No room for this one. Drawing it would land it on top of
			// the buttons that did fit.
			continue
		}
		fg, bg := f.Style.ButtonFG, f.Style.ButtonBG
		if focused, isButton := f.Focused(); isButton && focused == i {
			fg, bg = f.Style.ActiveFG, f.Style.ActiveBG
		}
		DrawButtonShadow(in, at, y, ButtonWidth(f.buttons[i].Title), f.Style.ButtonShadowBG, f.Style.BG)
		DrawButton(in, at, y, f.buttons[i].Title, fg, bg)
	}
}

// buttonTitles returns the buttons' titles, which is all the layout
// needs to know about them.
func (f *Form) buttonTitles() []string { return f.titles }

// buttonCols returns the column each button starts at, or -1 for one
// there was no room for.
func (f *Form) buttonCols() []int {
	f.cols = ButtonColsInto(f.cols[:0], f.buttonTitles(), f.box().Cols, formPad)
	return f.cols
}

// buttonAt returns which button covers a column.
func (f *Form) buttonAt(x int) (int, bool) {
	return ButtonAtCol(f.buttonTitles(), f.box().Cols, formPad, x)
}

// press runs a button and closes the form when it worked.
func (f *Form) press(at int) error {
	if at < 0 || at >= len(f.buttons) {
		return nil
	}
	if cols := f.buttonCols(); at < len(cols) && cols[at] < 0 {
		// There was no room to draw it. Doing what an invisible button
		// says is worse than saying why nothing happened.
		f.SetError(errors.New("the window is too narrow to show that button"))
		return nil
	}
	b := f.buttons[at]
	if b.Do == nil {
		f.dismiss()
		return nil
	}
	// Whatever an earlier press failed with is no longer what the form
	// has to say. Cleared before the button runs, not after: a button
	// that answers later sets the reason itself, and clearing after
	// would wipe it.
	f.SetError(nil)
	if err := b.Do(); err != nil {
		// The form stays open showing why, so what was typed is still
		// there to correct.
		f.SetError(err)
		return nil
	}
	if b.Keep {
		return nil
	}
	f.dismiss()
	return nil
}

// dismiss closes the dialog, once.
func (f *Form) dismiss() {
	if f.close != nil {
		f.close()
	}
}

// move steps the focus through the fields and then the buttons,
// stepping over any button there was no room to draw.
func (f *Form) move(by int) {
	n := len(f.rows) + len(f.buttons)
	if n == 0 {
		return
	}
	step := 1
	if by < 0 {
		step = -1
	}
	// Go's % keeps the sign of the dividend, so a step back from the
	// first needs the extra turn to land on the last.
	at := ((f.at+by)%n + n) % n
	// One turn at most: when every button is hidden and there are no
	// fields, the focus stays where it was.
	for i := 0; i < n && f.hidden(at); i++ {
		at = ((at+step)%n + n) % n
	}
	if f.hidden(at) {
		return
	}
	f.focus(at)
}

// hidden reports whether a place in the focus order is a button there
// was no room to draw. Landing on one would leave the dialog with the
// focus nowhere the user can see.
func (f *Form) hidden(at int) bool {
	i := at - len(f.rows)
	if i < 0 {
		return false
	}
	cols := f.buttonCols()
	return i < len(cols) && cols[i] < 0
}

// focus puts the caret on one field or button and takes it off whatever
// had it.
func (f *Form) focus(at int) {
	if at == f.at {
		return
	}
	if fld := f.focusedField(); fld != nil {
		fld.SetFocus(false)
	}
	f.at = at
	if fld := f.focusedField(); fld != nil {
		fld.SetFocus(true)
	}
}

// focusedField returns the field being typed into, or nil when a button
// has the focus.
func (f *Form) focusedField() *Field {
	if f.at < 0 || f.at >= len(f.rows) {
		return nil
	}
	return f.rows[f.at].field
}

// layoutFields tells every field how wide it is, so one that is not on
// screen still scrolls its text correctly.
func (f *Form) layoutFields() {
	box := f.box()
	if box.Empty() {
		return
	}
	width := max(box.Cols-formPad-f.fieldX(), 1)
	for _, r := range f.rows {
		r.field.Layout(Size{Cols: width, Rows: 1})
	}
}

// caretFor turns a column inside a field into a byte offset in its text.
func (f *Form) caretFor(fld *Field, col int) int {
	if col <= 0 {
		return 0
	}
	at, width := fld.left, 0
	for _, c := range fld.clustersFrom(fld.left) {
		w := grid.StringWidth(c)
		if fld.Mask != 0 {
			w = max(grid.RuneWidth(fld.Mask), 1)
		}
		if width+w > col {
			break
		}
		width += w
		at += len(c)
	}
	return at
}

// fieldX returns the column the fields start at, past the widest label.
func (f *Form) fieldX() int {
	width := 0
	for _, r := range f.rows {
		width = max(width, grid.StringWidth(r.label))
	}
	if width == 0 {
		return formPad
	}
	return formPad + width + formLabelGap
}

// rowsTop returns the first row of the box the fields are drawn on.
func (f *Form) rowsTop() int { return f.layout().fieldsTop }

// buttonsRow returns the row of the box the buttons are drawn on.
func (f *Form) buttonsRow() int { return f.layout().buttonRow }

// box returns where the dialog goes, centred in the area it was given.
//
// It gives up rather than shrinking past the point where every field
// fits. A field drawn off the bottom is still reachable with Tab, so the
// user would be typing a password into a row that is not on screen.
func (f *Form) box() Rect {
	cols := f.boxCols()
	rows := min(f.size.Rows-formMargin*2, f.wantRows())
	if cols < 12 || rows < f.needRows() {
		return Rect{}
	}
	return Rect{
		X:    (f.size.Cols - cols) / 2,
		Y:    (f.size.Rows - rows) / 2,
		Cols: cols,
		Rows: rows,
	}
}

// boxCols is how wide the box is: what it wants, in the room it has.
func (f *Form) boxCols() int {
	return min(f.size.Cols-formMargin*2, max(f.wantCols(), 20))
}

// wantCols returns how wide the dialog would like to be: enough for the
// longest thing in it, capped.
func (f *Form) wantCols() int {
	width := max(grid.StringWidth(f.Title), f.MinCols)
	for _, l := range f.Lines {
		width = max(width, grid.StringWidth(l))
	}
	if f.err != nil {
		width = max(width, grid.StringWidth(f.err.Error()))
	}
	labels := 0
	for _, r := range f.rows {
		labels = max(labels, grid.StringWidth(r.label))
	}
	if len(f.rows) > 0 {
		// Room for a label and something worth typing beside it. A path
		// or an address is what these fields usually hold, and neither
		// fits in a handful of columns.
		width = max(width, labels+formLabelGap+formFieldCols)
	}
	buttons := buttonsWidth(f.buttonTitles())
	width = max(width, buttons)
	// The cap stops one long line making the dialog as wide as the
	// window, which costs nothing because a line is trimmed. It does not
	// apply to the buttons: one that does not fit is not drawn at all,
	// and a button nobody can see is a button nobody can press.
	return max(min(width+formPad*2, formMaxCols), buttons+formPad*2)
}

// wantRows returns how tall the dialog would like to be.
func (f *Form) wantRows() int {
	rows := f.needRows()
	if len(f.Lines) > 0 {
		rows += len(f.Lines) + 1
	}
	// An error too long for the one line the dialog always keeps makes
	// the dialog taller, rather than being drawn over a field.
	return rows + f.errExtra()
}

// needRows returns the least the dialog can be drawn in: everything but
// the hint lines, which are the only part it will do without.
func (f *Form) needRows() int {
	// A blank row, the title, a blank row.
	rows := 3
	if len(f.rows) > 0 {
		rows += len(f.rows) + 1
	}
	// One error line is always there, so the buttons do not jump down
	// the moment something goes wrong. Then the buttons and a blank row.
	return rows + 3 + f.shadowRows()
}

// shadowRows is the row a shadow under the buttons needs, and none when
// the style casts no shadow.
func (f *Form) shadowRows() int {
	if f.Style.ButtonShadowBG.A == 0 {
		return 0
	}
	return 1
}

// errExtra is how many rows beyond the one the dialog always keeps this
// error needs, and never more than the window has left.
func (f *Form) errExtra() int {
	if f.err == nil {
		return 0
	}
	spare := f.size.Rows - formMargin*2 - f.needRows()
	if len(f.Lines) > 0 {
		spare -= len(f.Lines) + 1
	}
	if spare <= 0 {
		return 0
	}
	return min(len(f.errLines(f.wrapWidth(), spare+1)), spare+1) - 1
}

// ButtonWidth is how many columns a button takes: its title with a blank
// column each side.
func ButtonWidth(title string) int { return grid.StringWidth(title) + 2 }

// buttonsWidth is the room a row of buttons asks for: each button and a
// column beside it.
func buttonsWidth(titles []string) int {
	total := 0
	for _, t := range titles {
		total += ButtonWidth(t) + 1
	}
	return total
}

// ButtonColsIn returns the column each button starts at inside a box
// cols wide, or -1 for one there was no room for.
//
// They are laid out from the right, so the last button is nearest the
// corner and the first is the first to be dropped. A dropped button is
// marked with -1 rather than a column off the left edge, because
// grid.View.Sub shifts a negative origin to zero instead of clipping it.
func ButtonColsIn(titles []string, cols, pad int) []int {
	return ButtonColsInto(nil, titles, cols, pad)
}

// ButtonColsInto is ButtonColsIn writing into a slice the caller keeps,
// for a painter that runs on every frame. Pass into[:0].
func ButtonColsInto(into []int, titles []string, cols, pad int) []int {
	if len(titles) == 0 {
		return into[:0]
	}
	at := slices.Grow(into, len(titles))[:len(titles)]
	x := cols - pad
	for i := len(titles) - 1; i >= 0; i-- {
		w := ButtonWidth(titles[i])
		if x-w < pad {
			at[i] = -1
			continue
		}
		x -= w
		at[i] = x
		x--
	}
	return at
}

// ButtonAtCol returns which button covers a column.
func ButtonAtCol(titles []string, cols, pad, x int) (int, bool) {
	for i, at := range ButtonColsIn(titles, cols, pad) {
		if at < 0 {
			continue
		}
		if x >= at && x < at+ButtonWidth(titles[i]) {
			return i, true
		}
	}
	return 0, false
}

// DrawButton paints one button at a column, blank cell each side.
func DrawButton(v grid.View, x, y int, title string, fg, bg color.RGBA) {
	label := " " + title + " "
	line := v.Sub(x, y, grid.StringWidth(label), 1)
	line.Fill(grid.Cell{Rune: ' ', FG: fg, BG: bg, Width: 1})
	line.SetString(0, 0, label, fg, bg, 0)
}

// shadowUnder is the top half of a cell and shadowBeside the bottom
// half, which is how the shadow round a button is drawn.
//
// A cell is about twice as tall as it is wide, so a whole cell either way
// would be twice the thickness of the shadow measured across. Half a cell
// each way is what Turbo Pascal drew, and it reads as one thickness all
// the way round the corner.
const (
	shadowUnder  = '▀'
	shadowBeside = '▄'
)

// DrawButtonShadow darkens the cell to the right of a button and the row
// under it, a column further right, which is the shadow a DOS program
// cast. A colour with no alpha draws nothing.
//
// ground is what the shadow is laid on, which the half-height parts show
// the rest of.
func DrawButtonShadow(v grid.View, x, y, width int, shadow, ground color.RGBA) {
	if shadow.A == 0 || width <= 0 {
		return
	}
	cols, rows := v.Size()
	set := func(x, y int, cell grid.Cell) {
		if x < 0 || y < 0 || x >= cols || y >= rows {
			return
		}
		v.Set(x, y, cell)
	}
	// Beside the button, the bottom half of the row, so the shadow meets
	// the one under it at the corner rather than standing half a cell
	// taller than it.
	set(x+width, y, grid.Cell{Rune: shadowBeside, FG: shadow, BG: ground, Width: 1})
	// And under it, the top half of the row only, so the shadow is the
	// same thickness whichever way it is measured.
	under := grid.Cell{Rune: shadowUnder, FG: shadow, BG: ground, Width: 1}
	for i := 1; i <= width; i++ {
		set(x+i, y+1, under)
	}
}
