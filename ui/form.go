package ui

import (
	"image/color"

	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/input"
)

// How large a form is allowed to get, and how much of the area it leaves
// around itself.
const (
	formMaxCols = 60
	formMargin  = 2

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
}

// Button is something to press at the bottom of a form.
//
// Do runs on the drawing goroutine. Returning an error leaves the form
// open with the error shown, which is what a connection that failed
// wants; returning nil means the button is finished and the form closes.
type Button struct {
	Title string
	Do    func() error
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
	Title string
	Lines []string

	rows    []formRow
	buttons []Button

	// at is what has focus: a row while it is below len(rows), and a
	// button after that.
	at int

	err   error
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

// AddField puts a labelled field at the bottom of the form and returns
// it, so a caller can read what was typed without keeping its own list.
func (f *Form) AddField(label string, field *Field) *Field {
	if field == nil {
		field = NewField()
	}
	f.rows = append(f.rows, formRow{label: label, field: field})
	// The first field takes focus, or a form would open with the caret
	// on a button and nowhere to type.
	if len(f.rows) == 1 {
		f.at = 0
		field.SetFocus(true)
	}
	return field
}

// AddButton puts a button at the bottom. The first one added is the one
// Enter presses from a field.
func (f *Form) AddButton(b Button) { f.buttons = append(f.buttons, b) }

// Fields returns the fields in the order they were added.
func (f *Form) Fields() []*Field {
	out := make([]*Field, len(f.rows))
	for i, r := range f.rows {
		out[i] = r.field
	}
	return out
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

// SetError shows a line saying why the last attempt failed, or clears it
// when err is nil.
func (f *Form) SetError(err error) { f.err = err }

// Box returns where the dialog sits in the view it draws through, so
// whatever is showing it can treat that part differently.
func (f *Form) Box() Rect { return f.box() }

// Layout notes how much room the dialog has to place itself in.
func (f *Form) Layout(size Size) {
	f.size = size
	f.layoutFields()
}

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
func (f *Form) HandleKey(ev input.Event) (bool, error) {
	if fld := f.focusedField(); fld != nil {
		if took, err := fld.HandleKey(ev); took {
			return true, err
		}
	}
	if ev.Kind != input.KeyPress && ev.Kind != input.KeyRepeat {
		return false, nil
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
	return false, nil
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
		// The caret goes where it was clicked, so a long value can be
		// corrected in the middle rather than only at the end.
		fld := f.rows[row].field
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
	in := box.In(v)
	in.Fill(grid.Cell{Rune: ' ', FG: f.Style.FG, BG: f.Style.BG, Width: 1})
	cols, _ := in.Size()
	room := cols - formPad*2

	y := 1
	in.SetString(formPad, y, f.Title, f.Style.TitleFG, f.Style.BG, grid.AttrBold)
	y += 2

	for _, line := range f.Lines {
		in.SetString(formPad, y, line, f.Style.HintFG, f.Style.BG, 0)
		y++
	}
	if len(f.Lines) > 0 {
		y++
	}

	for i, r := range f.rows {
		in.SetString(formPad, y, r.label, f.Style.LabelFG, f.Style.BG, 0)
		fg, bg := f.Style.FieldFG, f.Style.FieldBG
		if i == f.at {
			fg, bg = f.Style.FocusFG, f.Style.FocusBG
		}
		r.field.Style = FieldStyle{FG: fg, BG: bg, PlaceholderFG: f.Style.HintFG}
		width := max(room-f.fieldX()+formPad, 1)
		r.field.Draw(in.Sub(f.fieldX(), y, width, 1))
		y++
	}
	if len(f.rows) > 0 {
		y++
	}

	if f.err != nil {
		in.SetString(formPad, y, trimTo(f.err.Error(), room), f.Style.ErrorFG, f.Style.BG, 0)
	}
	f.paintButtons(in, cols)
}

// paintButtons draws the buttons in a row along the bottom, right
// aligned so the one Enter presses is nearest the corner the eye lands
// on.
func (f *Form) paintButtons(in grid.View, cols int) {
	y := f.buttonsRow()
	for i, at := range f.buttonCols() {
		b := f.buttons[i]
		fg, bg := f.Style.ButtonFG, f.Style.ButtonBG
		if focused, isButton := f.Focused(); isButton && focused == i {
			fg, bg = f.Style.ActiveFG, f.Style.ActiveBG
		}
		label := " " + b.Title + " "
		line := in.Sub(at, y, grid.StringWidth(label), 1)
		line.Fill(grid.Cell{Rune: ' ', FG: fg, BG: bg, Width: 1})
		line.SetString(0, 0, label, fg, bg, 0)
	}
}

// buttonCols returns the column each button starts at.
func (f *Form) buttonCols() []int {
	if len(f.buttons) == 0 {
		return nil
	}
	cols := f.box().Cols
	at := make([]int, len(f.buttons))
	// Laid out from the right, then read back left to right, so the
	// first button added ends up nearest the corner.
	x := cols - formPad
	for i := len(f.buttons) - 1; i >= 0; i-- {
		w := grid.StringWidth(f.buttons[i].Title) + 2
		x -= w
		at[i] = x
		x--
	}
	return at
}

// buttonAt returns which button covers a column.
func (f *Form) buttonAt(x int) (int, bool) {
	for i, at := range f.buttonCols() {
		w := grid.StringWidth(f.buttons[i].Title) + 2
		if x >= at && x < at+w {
			return i, true
		}
	}
	return 0, false
}

// press runs a button and closes the form when it worked.
func (f *Form) press(at int) error {
	if at < 0 || at >= len(f.buttons) {
		return nil
	}
	b := f.buttons[at]
	if b.Do == nil {
		f.dismiss()
		return nil
	}
	if err := b.Do(); err != nil {
		// The form stays open showing why, so what was typed is still
		// there to correct.
		f.err = err
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

// move steps the focus through the fields and then the buttons.
func (f *Form) move(by int) {
	n := len(f.rows) + len(f.buttons)
	if n == 0 {
		return
	}
	// Go's % keeps the sign of the dividend, so a step back from the
	// first needs the extra turn to land on the last.
	f.focus(((f.at+by)%n + n) % n)
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
func (f *Form) rowsTop() int {
	// A blank line under the title, then the hint lines and a blank
	// after them when there are any.
	y := 3
	if len(f.Lines) > 0 {
		y += len(f.Lines) + 1
	}
	return y
}

// buttonsRow returns the row of the box the buttons are drawn on.
func (f *Form) buttonsRow() int { return f.box().Rows - 2 }

// box returns where the dialog goes, centred in the area it was given.
func (f *Form) box() Rect {
	cols := min(f.size.Cols-formMargin*2, max(f.wantCols(), 20))
	rows := min(f.size.Rows-formMargin*2, f.wantRows())
	if cols < 12 || rows < 5 {
		return Rect{}
	}
	return Rect{
		X:    (f.size.Cols - cols) / 2,
		Y:    (f.size.Rows - rows) / 2,
		Cols: cols,
		Rows: rows,
	}
}

// wantCols returns how wide the dialog would like to be: enough for the
// longest thing in it, capped.
func (f *Form) wantCols() int {
	width := grid.StringWidth(f.Title)
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
		// Room for a label and something worth typing beside it.
		width = max(width, labels+formLabelGap+24)
	}
	buttons := 0
	for _, b := range f.buttons {
		buttons += grid.StringWidth(b.Title) + 3
	}
	width = max(width, buttons)
	return min(width+formPad*2, formMaxCols)
}

// wantRows returns how tall the dialog would like to be.
func (f *Form) wantRows() int {
	// A blank row top and bottom, the title, a blank under it, the body,
	// the error line and the buttons.
	rows := 1 + 1 + 1
	if len(f.Lines) > 0 {
		rows += len(f.Lines) + 1
	}
	if len(f.rows) > 0 {
		rows += len(f.rows) + 1
	}
	// The error line is always there, so the buttons do not jump down
	// the moment something goes wrong.
	rows += 1 + 1 + 1
	return rows
}

// trimTo cuts a string to a width, by cluster so a wide character is not
// halved.
func trimTo(s string, width int) string {
	if grid.StringWidth(s) <= width {
		return s
	}
	out, at := "", 0
	for _, c := range grid.Clusters(s) {
		w := grid.StringWidth(c)
		if at+w > width {
			break
		}
		out += c
		at += w
	}
	return out
}
