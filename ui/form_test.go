package ui

import (
	"errors"
	"image/color"
	"strings"
	"testing"

	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/input"
)

// formStyled returns colours a test can tell apart from a blank cell.
func formStyled() FormStyle {
	return FormStyle{
		FG: fg, BG: bg,
		TitleFG: fg, LabelFG: fg, HintFG: fg,
		FieldFG: fg, FieldBG: bg,
		FocusFG: fg, FocusBG: bg,
		ButtonFG: fg, ButtonBG: bg,
		ActiveFG: bg, ActiveBG: fg,
		ErrorFG: fg,
	}
}

// newTestForm returns a form with two fields and two buttons, and counts
// how often it asked to be closed and how often each button ran.
type testForm struct {
	form   *Form
	closed int
	ran    []string
	fail   error
}

func newTestForm(t *testing.T) *testForm {
	t.Helper()
	tf := &testForm{}
	f := NewForm("Connect to a server", func() { tf.closed++ })
	f.Style = formStyled()
	f.AddField("Host", nil)
	f.AddField("User", nil)
	f.AddButton(Button{Title: "Connect", Do: func() error {
		tf.ran = append(tf.ran, "connect")
		return tf.fail
	}})
	f.AddButton(Button{Title: "Cancel", Do: func() error {
		tf.ran = append(tf.ran, "cancel")
		return nil
	}})
	f.Layout(Size{Cols: 60, Rows: 24})
	// The modal stack focuses a dialog when it pushes it, and the form
	// passes that on to its first field.
	f.SetFocus(true)
	tf.form = f
	return tf
}

// drawForm paints a form onto a see-through grid, the way its own layer
// is made.
func drawForm(f *Form, cols, rows int) *grid.Grid {
	g := grid.New(cols, rows, color.RGBA{}, color.RGBA{})
	f.Layout(Size{Cols: cols, Rows: rows})
	f.Draw(g.View())
	return g
}

// gridText reads a whole grid back as one string, for asking whether
// something was drawn at all.
func gridText(g *grid.Grid) string {
	_, rows := g.Size()
	var b strings.Builder
	for y := 0; y < rows; y++ {
		b.WriteString(rowOf(g, y))
		b.WriteByte('\n')
	}
	return b.String()
}

func TestFormOpensOnTheFirstField(t *testing.T) {
	tf := newTestForm(t)
	at, isButton := tf.form.Focused()
	if isButton || at != 0 {
		t.Fatalf("focus is on index %d (button %v), want the first field", at, isButton)
	}
	if !tf.form.Fields()[0].Focused() {
		t.Error("the first field does not have the caret")
	}
}

// Typing has to reach the field with the caret, not the one that was
// added first.
func TestFormTypingGoesToTheFocusedField(t *testing.T) {
	tf := newTestForm(t)
	f := tf.form

	for _, r := range "margit" {
		f.HandleKey(input.Event{Kind: input.Text, Rune: r, NormalText: true})
	}
	f.HandleKey(press(input.KeyTab, 0))
	for _, r := range "marcus" {
		f.HandleKey(input.Event{Kind: input.Text, Rune: r, NormalText: true})
	}

	if got := f.Fields()[0].Text(); got != "margit" {
		t.Errorf("first field = %q, want %q", got, "margit")
	}
	if got := f.Fields()[1].Text(); got != "marcus" {
		t.Errorf("second field = %q, want %q", got, "marcus")
	}
}

// Nothing in the toolkit walks a tab order, so the form has to do it.
func TestFormTabMovesThroughFieldsThenButtons(t *testing.T) {
	tf := newTestForm(t)
	f := tf.form

	want := []struct {
		at     int
		button bool
	}{{1, false}, {0, true}, {1, true}, {0, false}}
	for i, w := range want {
		f.HandleKey(press(input.KeyTab, 0))
		at, isButton := f.Focused()
		if at != w.at || isButton != w.button {
			t.Fatalf("after %d tabs, focus is %d (button %v), want %d (button %v)",
				i+1, at, isButton, w.at, w.button)
		}
	}
}

func TestFormShiftTabGoesBack(t *testing.T) {
	tf := newTestForm(t)
	f := tf.form
	f.HandleKey(press(input.KeyTab, input.ModShift))
	at, isButton := f.Focused()
	if !isButton || at != 1 {
		t.Fatalf("Shift+Tab from the first field went to %d (button %v), want the last button",
			at, isButton)
	}
}

// Only the field with the caret may place the cursor, and moving focus
// to a button has to take it away entirely.
func TestFormMovesTheCaretWithTheFocus(t *testing.T) {
	tf := newTestForm(t)
	f := tf.form
	f.SetFocus(true)

	f.HandleKey(press(input.KeyTab, 0))
	if f.Fields()[0].Focused() {
		t.Error("the first field kept the caret after focus moved on")
	}
	if !f.Fields()[1].Focused() {
		t.Error("the second field did not take the caret")
	}

	f.HandleKey(press(input.KeyTab, 0)) // onto a button
	for i, fld := range f.Fields() {
		if fld.Focused() {
			t.Errorf("field %d still has the caret while a button is focused", i)
		}
	}
	g := drawForm(f, 60, 24)
	if g.CursorClaimed() {
		t.Error("a form focused on a button still claimed the cursor")
	}
}

// Enter from a field means the first button, which is what the form is
// for. Getting there should not need four tabs.
func TestFormEnterFromAFieldRunsTheFirstButton(t *testing.T) {
	tf := newTestForm(t)
	tf.form.HandleKey(press(input.KeyEnter, 0))
	if len(tf.ran) != 1 || tf.ran[0] != "connect" {
		t.Fatalf("Enter ran %q, want the first button", tf.ran)
	}
	if tf.closed != 1 {
		t.Errorf("the form was closed %d times after its button worked, want 1", tf.closed)
	}
}

func TestFormEnterOnAButtonRunsThatButton(t *testing.T) {
	tf := newTestForm(t)
	f := tf.form
	f.HandleKey(press(input.KeyTab, 0))
	f.HandleKey(press(input.KeyTab, 0))
	f.HandleKey(press(input.KeyTab, 0)) // the second button
	f.HandleKey(press(input.KeyEnter, 0))
	if len(tf.ran) != 1 || tf.ran[0] != "cancel" {
		t.Fatalf("Enter ran %q, want the focused button", tf.ran)
	}
}

// Space is a character in a field and a press on a button. Getting that
// backwards makes a password with a space in it impossible to type.
func TestFormSpaceTypesInAFieldAndPressesAButton(t *testing.T) {
	tf := newTestForm(t)
	f := tf.form

	f.HandleKey(input.Event{Kind: input.Text, Rune: ' ', NormalText: true})
	if got := f.Fields()[0].Text(); got != " " {
		t.Fatalf("a space in a field gave %q, want it typed", got)
	}
	if len(tf.ran) != 0 {
		t.Fatalf("a space in a field ran %q", tf.ran)
	}

	f.HandleKey(press(input.KeyTab, 0))
	f.HandleKey(press(input.KeyTab, 0))
	f.HandleKey(press(input.KeySpace, 0))
	if len(tf.ran) != 1 || tf.ran[0] != "connect" {
		t.Fatalf("Space on a button ran %q, want the button", tf.ran)
	}
}

// A button that failed leaves the form open, with the reason shown and
// what was typed still there to correct.
func TestFormKeepsTheFormOpenWhenAButtonFails(t *testing.T) {
	tf := newTestForm(t)
	tf.fail = errors.New("no route to host")
	f := tf.form
	f.Fields()[0].SetText("margit")

	f.HandleKey(press(input.KeyEnter, 0))
	if tf.closed != 0 {
		t.Fatalf("the form closed after its button failed")
	}
	if f.Error() == nil || f.Error().Error() != "no route to host" {
		t.Fatalf("error = %v, want the one the button returned", f.Error())
	}
	if got := f.Fields()[0].Text(); got != "margit" {
		t.Errorf("the form lost what was typed: %q", got)
	}
	if !strings.Contains(gridText(drawForm(f, 60, 24)), "no route to host") {
		t.Error("the reason was not drawn")
	}
}

func TestFormEscapeCloses(t *testing.T) {
	tf := newTestForm(t)
	tf.form.HandleKey(press(input.KeyEscape, 0))
	if tf.closed != 1 {
		t.Fatalf("Escape closed the form %d times, want 1", tf.closed)
	}
	if len(tf.ran) != 0 {
		t.Errorf("Escape ran %q, want nothing", tf.ran)
	}
}

func TestFormClickOnAFieldMovesTheFocus(t *testing.T) {
	tf := newTestForm(t)
	f := tf.form
	drawForm(f, 60, 24)
	box := f.Box()

	// The second row of fields.
	y := box.Y + f.rowsTop() + 1
	f.HandleMouse(input.MouseEvent{
		Kind: input.MousePress, Button: input.MouseLeft,
		Col: box.X + f.fieldX(), Row: y,
	})
	at, isButton := f.Focused()
	if isButton || at != 1 {
		t.Fatalf("clicking the second field focused %d (button %v)", at, isButton)
	}
}

// Clicking in the middle of a value puts the caret there, so a long
// hostname can be corrected where the mistake is.
func TestFormClickPutsTheCaretWhereItLanded(t *testing.T) {
	tf := newTestForm(t)
	f := tf.form
	f.Fields()[0].SetText("margit.skalarit.net")
	drawForm(f, 60, 24)
	box := f.Box()

	f.HandleMouse(input.MouseEvent{
		Kind: input.MousePress, Button: input.MouseLeft,
		Col: box.X + f.fieldX() + 6, Row: box.Y + f.rowsTop(),
	})
	if got := f.Fields()[0].Caret(); got != 6 {
		t.Fatalf("caret = %d, want 6", got)
	}
}

func TestFormClickOnAButtonRunsIt(t *testing.T) {
	tf := newTestForm(t)
	f := tf.form
	drawForm(f, 60, 24)
	box := f.Box()

	at := f.buttonCols()[1]
	f.HandleMouse(input.MouseEvent{
		Kind: input.MousePress, Button: input.MouseLeft,
		Col: box.X + at, Row: box.Y + f.buttonsRow(),
	})
	if len(tf.ran) != 1 || tf.ran[0] != "cancel" {
		t.Fatalf("clicking the second button ran %q", tf.ran)
	}
}

func TestFormClickOutsideCloses(t *testing.T) {
	tf := newTestForm(t)
	f := tf.form
	drawForm(f, 60, 24)

	f.HandleMouse(input.MouseEvent{
		Kind: input.MousePress, Button: input.MouseLeft, Col: 0, Row: 0,
	})
	if tf.closed != 1 {
		t.Fatalf("a click outside closed the form %d times, want 1", tf.closed)
	}
}

// The dialog covers the whole area, so a drag must not reach the pane
// behind it and select text nobody can see.
func TestFormSwallowsDrags(t *testing.T) {
	tf := newTestForm(t)
	took, _ := tf.form.HandleMouse(input.MouseEvent{Kind: input.MouseMove, Col: 0, Row: 0})
	if !took {
		t.Error("a drag travelled through the dialog")
	}
}

func TestFormDrawsItsTitleLabelsAndButtons(t *testing.T) {
	tf := newTestForm(t)
	text := gridText(drawForm(tf.form, 60, 24))
	for _, want := range []string{"Connect to a server", "Host", "User", "Connect", "Cancel"} {
		if !strings.Contains(text, want) {
			t.Errorf("%q was not drawn", want)
		}
	}
}

// A confirm dialog has no fields: the lines are the whole point, and a
// host key fingerprint has to be readable.
func TestConfirmDrawsItsLines(t *testing.T) {
	closed := 0
	lines := []string{"margit.skalarit.net:22", "SHA256:abcdef0123456789"}
	f := NewConfirm("Unknown host key", lines, func() { closed++ })
	f.Style = formStyled()
	f.AddButton(Button{Title: "Connect", Do: func() error { return nil }})
	f.AddButton(Button{Title: "Cancel", Do: func() error { return nil }})

	text := gridText(drawForm(f, 60, 24))
	for _, want := range append(lines, "Unknown host key") {
		if !strings.Contains(text, want) {
			t.Errorf("%q was not drawn", want)
		}
	}
	// With no fields, the first button has the focus and Enter presses it.
	at, isButton := f.Focused()
	if !isButton || at != 0 {
		t.Fatalf("focus is %d (button %v), want the first button", at, isButton)
	}
}

// A form must draw in the room it is given rather than trusting the size
// it was told, or a window shrunk while a dialog is open draws outside it.
func TestFormInATinyAreaDrawsNothing(t *testing.T) {
	tf := newTestForm(t)
	f := tf.form
	f.Layout(Size{Cols: 6, Rows: 3})
	if box := f.Box(); !box.Empty() {
		t.Fatalf("box = %+v in a 6x3 area, want nothing", box)
	}
	// Drawing anyway must not panic or write outside the view.
	drawForm(f, 6, 3)
}

func TestFormBoxStaysInsideTheArea(t *testing.T) {
	tf := newTestForm(t)
	for _, size := range []Size{{Cols: 20, Rows: 10}, {Cols: 60, Rows: 24}, {Cols: 200, Rows: 60}} {
		tf.form.Layout(size)
		box := tf.form.Box()
		if box.Empty() {
			continue
		}
		if box.X < 0 || box.Y < 0 || box.X+box.Cols > size.Cols || box.Y+box.Rows > size.Rows {
			t.Errorf("box %+v falls outside a %dx%d area", box, size.Cols, size.Rows)
		}
	}
}

// What is drawn and what a click lands on have to be the same rows.
//
// They used to be worked out twice: the buttons from the height the box
// actually got, everything else from the height it asked for. At 24x9
// that put the buttons on top of the first field, and clicking the
// Connect button you could see focused a field you could not.
func TestFormDrawsAndRoutesTheSameRows(t *testing.T) {
	for _, size := range []Size{
		{Cols: 24, Rows: 9}, {Cols: 16, Rows: 10}, {Cols: 40, Rows: 12},
		{Cols: 60, Rows: 24}, {Cols: 80, Rows: 40},
	} {
		tf := newTestForm(t)
		f := tf.form
		g := drawForm(f, size.Cols, size.Rows)
		box := f.Box()
		if box.Empty() {
			continue
		}
		l := f.layout()

		// Nothing may share the button row.
		if l.buttonRow <= l.fieldsTop+l.fields-1 && l.fields > 0 {
			t.Errorf("%dx%d: fields end at row %d and the buttons are at %d",
				size.Cols, size.Rows, l.fieldsTop+l.fields-1, l.buttonRow)
		}
		// Every field the form will route a click to must be drawn.
		if l.fields != len(f.Fields()) {
			t.Errorf("%dx%d: %d of %d fields fit, so one is reachable but not on screen",
				size.Cols, size.Rows, l.fields, len(f.Fields()))
		}
		// And everything lands inside the box.
		if l.buttonRow >= box.Rows || l.errRow >= box.Rows || l.title >= box.Rows {
			t.Errorf("%dx%d: layout %+v falls outside a box %d rows tall",
				size.Cols, size.Rows, l, box.Rows)
		}
		// The button the user can see is the button they can press.
		for i, at := range f.buttonCols() {
			if at < 0 {
				continue
			}
			got, ok := f.buttonAt(at)
			if !ok || got != i {
				t.Errorf("%dx%d: the column button %d is drawn at routes to %d (%v)",
					size.Cols, size.Rows, i, got, ok)
			}
			if at < formPad || at >= box.Cols {
				t.Errorf("%dx%d: button %d is drawn at column %d, outside the box",
					size.Cols, size.Rows, i, at)
			}
		}
		_ = g
	}
}

// A dialog with no room for its fields does not open at all. Drawing it
// without them leaves the user typing a password into a row that is not
// on screen.
func TestFormWithNoRoomForItsFieldsDoesNotOpen(t *testing.T) {
	tf := newTestForm(t)
	f := tf.form
	// Two fields need more than eight rows once the title, the error
	// line and the buttons are counted.
	f.Layout(Size{Cols: 40, Rows: 8})
	if box := f.Box(); !box.Empty() {
		t.Fatalf("box = %+v in a 40x8 area, want nothing", box)
	}

	// And it takes nothing but Escape while it cannot be seen.
	for _, r := range "secret" {
		f.HandleKey(input.Event{Kind: input.Text, Rune: r, NormalText: true})
	}
	if got := f.Fields()[0].Text(); got != "" {
		t.Errorf("an invisible dialog took %q", got)
	}
	f.HandleKey(press(input.KeyEnter, 0))
	if len(tf.ran) != 0 {
		t.Errorf("an invisible dialog ran %q", tf.ran)
	}
	f.HandleKey(press(input.KeyEscape, 0))
	if tf.closed != 1 {
		t.Errorf("Escape closed an invisible dialog %d times, want 1", tf.closed)
	}
}

// Space with the caret in a field is a character, not a button press. A
// password with a space in it has to be typeable.
func TestFormSpaceKeyInAFieldPressesNothing(t *testing.T) {
	tf := newTestForm(t)
	took, _ := tf.form.HandleKey(press(input.KeySpace, 0))
	if len(tf.ran) != 0 {
		t.Fatalf("Space with the caret in a field ran %q", tf.ran)
	}
	if took {
		t.Error("Space in a field was taken, so nothing else can have it")
	}
}

// A dialog opens from the pump and input is polled in the same frame, so
// the repeats of the key that opened it must not answer it.
func TestFormIgnoresAHeldKeyOnAButton(t *testing.T) {
	tf := newTestForm(t)
	f := tf.form
	f.FocusButton(0)

	f.HandleKey(input.Event{Kind: input.KeyRepeat, Key: input.KeyEnter})
	f.HandleKey(input.Event{Kind: input.KeyRepeat, Key: input.KeySpace})
	if len(tf.ran) != 0 {
		t.Fatalf("a held key ran %q", tf.ran)
	}
	f.HandleKey(press(input.KeyEnter, 0))
	if len(tf.ran) != 1 {
		t.Fatalf("a fresh press ran %q, want the button once", tf.ran)
	}
}

// A question whose safe answer is "no" opens on the button that says no.
func TestFormFocusButtonChoosesWhereItOpens(t *testing.T) {
	closed := 0
	f := NewConfirm("Unknown host key", []string{"a fingerprint"}, func() { closed++ })
	f.Style = formStyled()
	var ran []string
	f.AddButton(Button{Title: "Connect", Do: func() error { ran = append(ran, "connect"); return nil }})
	f.AddButton(Button{Title: "Cancel", Do: func() error { ran = append(ran, "cancel"); return nil }})
	f.FocusButton(1)
	f.Layout(Size{Cols: 60, Rows: 24})

	at, isButton := f.Focused()
	if !isButton || at != 1 {
		t.Fatalf("focus is %d (button %v), want the second button", at, isButton)
	}
	f.HandleKey(press(input.KeyEnter, 0))
	if len(ran) != 1 || ran[0] != "cancel" {
		t.Fatalf("Enter ran %q, want the focused button", ran)
	}
}

// A button with no room is left out rather than drawn over the ones that
// fit. grid.View.Sub shifts a negative origin to zero instead of
// clipping it, so a button laid out past the left edge would print its
// middle at the left edge.
func TestFormDropsAButtonThatDoesNotFit(t *testing.T) {
	closed := 0
	f := NewForm("Narrow", func() { closed++ })
	f.Style = formStyled()
	f.AddButton(Button{Title: "A rather long button"})
	f.AddButton(Button{Title: "Another long one"})
	f.Layout(Size{Cols: 24, Rows: 20})

	box := f.Box()
	if box.Empty() {
		t.Skip("no box at this size")
	}
	for i, at := range f.buttonCols() {
		if at >= 0 && at < formPad {
			t.Errorf("button %d is drawn at column %d, left of the padding", i, at)
		}
	}
}

// Clicking the label you can see focuses the field beside it.
//
// This reads the drawn grid rather than asking the form where it put
// things: the painter and the click router each used to work the rows
// out for themselves, so they could drift apart and every test that
// asked the router would agree with the router.
func TestFormClickLandsOnTheFieldThatWasDrawn(t *testing.T) {
	for _, size := range []Size{{Cols: 24, Rows: 12}, {Cols: 40, Rows: 14}, {Cols: 60, Rows: 24}} {
		tf := newTestForm(t)
		f := tf.form
		g := drawForm(f, size.Cols, size.Rows)
		box := f.Box()
		if box.Empty() {
			continue
		}

		// Find the row the second field's label was actually drawn on.
		want := -1
		_, rows := g.Size()
		for y := 0; y < rows; y++ {
			if strings.Contains(rowOf(g, y), "User") {
				want = y
				break
			}
		}
		if want < 0 {
			t.Fatalf("%dx%d: the User label was not drawn", size.Cols, size.Rows)
		}

		f.HandleMouse(input.MouseEvent{
			Kind: input.MousePress, Button: input.MouseLeft,
			Col: box.X + f.fieldX(), Row: want,
		})
		at, isButton := f.Focused()
		if isButton || at != 1 {
			t.Errorf("%dx%d: clicking the row the User label is drawn on focused %d (button %v)",
				size.Cols, size.Rows, at, isButton)
		}
	}
}

// The same from the other side: the button the user can see is the one
// the click runs.
func TestFormClickLandsOnTheButtonThatWasDrawn(t *testing.T) {
	tf := newTestForm(t)
	f := tf.form
	g := drawForm(f, 40, 16)
	box := f.Box()

	want := -1
	_, rows := g.Size()
	for y := 0; y < rows; y++ {
		if strings.Contains(rowOf(g, y), "Cancel") {
			want = y
			break
		}
	}
	if want < 0 {
		t.Fatal("the Cancel button was not drawn")
	}

	at := f.buttonCols()[1]
	f.HandleMouse(input.MouseEvent{
		Kind: input.MousePress, Button: input.MouseLeft,
		Col: box.X + at, Row: want,
	})
	if len(tf.ran) != 1 || tf.ran[0] != "cancel" {
		t.Fatalf("clicking the row Cancel is drawn on ran %q", tf.ran)
	}
}

// With more hint lines than fit, the fields move up. A click router that
// counted all the lines would then aim below every one of them.
func TestFormClickLandsWhenHintLinesAreElided(t *testing.T) {
	closed := 0
	f := NewForm("Sign in", func() { closed++ })
	f.Style = formStyled()
	f.Lines = []string{"one", "two", "three", "four", "five", "six", "seven"}
	f.AddField("User", nil)
	f.AddField("Password", nil)
	f.AddButton(Button{Title: "Go"})
	f.SetFocus(true)

	g := drawForm(f, 40, 14)
	box := f.Box()
	if box.Empty() {
		t.Fatal("no box")
	}
	if l := f.layout(); len(l.lines) == len(f.Lines) {
		t.Skip("every hint line fitted, so nothing was elided")
	}

	want := -1
	_, rows := g.Size()
	for y := 0; y < rows; y++ {
		if strings.Contains(rowOf(g, y), "Password") {
			want = y
			break
		}
	}
	if want < 0 {
		t.Fatal("the Password label was not drawn")
	}
	f.HandleMouse(input.MouseEvent{
		Kind: input.MousePress, Button: input.MouseLeft,
		Col: box.X + f.fieldX(), Row: want,
	})
	if at, isButton := f.Focused(); isButton || at != 1 {
		t.Fatalf("clicking the Password row focused %d (button %v)", at, isButton)
	}
}

// A row of buttons that does not fit in the usual width makes the dialog
// wider rather than dropping one.
//
// A button that is not drawn cannot be clicked, and the one dropped is
// the first -- which is the one Enter presses from a field. The user
// would be pressing a button that is not on screen.
func TestFormGrowsToFitItsButtons(t *testing.T) {
	f := NewForm("Tunnel over a machine with a long name", nil)
	f.Style = formStyled()
	f.AddField("Listen on", nil)
	f.AddButton(Button{Title: "Listen here and also over there"})
	f.AddButton(Button{Title: "Listen on the other machine"})
	f.AddButton(Button{Title: "Cancel"})
	f.Layout(Size{Cols: 120, Rows: 24})

	for i, at := range f.buttonCols() {
		if at < 0 {
			t.Fatalf("button %d (%q) has nowhere to be drawn in a box %d wide",
				i, f.buttons[i].Title, f.box().Cols)
		}
	}
	// And it is still inside the window.
	if box := f.box(); box.X < 0 || box.X+box.Cols > 120 {
		t.Fatalf("the box is %d wide at column %d, in a window 120 wide", box.Cols, box.X)
	}
}

// A window too narrow for every button leaves some undrawn. One that is
// not drawn is not pressed either, and the focus steps over it: a dialog
// whose focus is on something invisible has nowhere the user can see.
func TestFormWillNotPressAButtonItCannotShow(t *testing.T) {
	f := NewForm("Tunnel", nil)
	f.Style = formStyled()
	f.AddField("Listen on", nil)
	var ran []string
	f.AddButton(Button{Title: "Listen here", Do: func() error {
		ran = append(ran, "here")
		return nil
	}})
	f.AddButton(Button{Title: "Cancel", Do: func() error {
		ran = append(ran, "cancel")
		return nil
	}})
	// Room for the box and one button, not two.
	f.Layout(Size{Cols: 22, Rows: 24})

	cols := f.buttonCols()
	if cols[0] >= 0 {
		t.Skipf("both buttons fit in a box %d wide, so there is nothing to test", f.box().Cols)
	}

	// Enter from the field means the first button, which is not there.
	f.SetFocus(true)
	if _, err := f.HandleKey(press(input.KeyEnter, 0)); err != nil {
		t.Fatalf("Enter: %v", err)
	}
	if len(ran) != 0 {
		t.Fatalf("a button nobody can see was pressed: %v", ran)
	}
	if f.Error() == nil {
		t.Fatal("nothing said why Enter did nothing")
	}

	// And Tab steps over it, onto the one that is drawn.
	f.HandleKey(press(input.KeyTab, 0))
	at, isButton := f.Focused()
	if !isButton {
		t.Fatalf("Tab left the focus on field %d", at)
	}
	if cols[at] < 0 {
		t.Fatalf("Tab landed on button %d, which has nowhere to be drawn", at)
	}
}
