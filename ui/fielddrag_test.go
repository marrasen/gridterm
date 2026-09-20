package ui

import (
	"testing"

	"github.com/marrasen/gridterm/input"
)

// dragField presses in a form field, moves the pointer and lets go. The
// columns are the field's own, counting from its first cell.
func dragField(t *testing.T, f *Form, row, fromCol, toCol int, mods input.Mods) {
	t.Helper()
	box := f.Box()
	y := box.Y + f.rowsTop() + row
	at := func(col int) int { return box.X + f.fieldX() + col }
	mouseTo(t, f, input.MouseEvent{
		Kind: input.MousePress, Button: input.MouseLeft, Col: at(fromCol), Row: y, Mods: mods,
	})
	mouseTo(t, f, input.MouseEvent{
		Kind: input.MouseMove, Button: input.MouseLeft, Col: at(toCol), Row: y,
	})
	mouseTo(t, f, input.MouseEvent{
		Kind: input.MouseRelease, Button: input.MouseLeft, Col: at(toCol), Row: y,
	})
}

// A drag across a field picks out the text under it. Every other place
// text is shown selects with the mouse, and the box people expect to
// drag in most was the one that did not.
func TestADragOverAFormFieldPicksTextOut(t *testing.T) {
	tf := newTestForm(t)
	f := tf.form
	f.Fields()[0].SetText("margit.skalarit.net")
	drawForm(f, 60, 24)

	dragField(t, f, 0, 0, 6, 0)

	if got, want := f.Fields()[0].Selected(), "margit"; got != want {
		t.Errorf("the drag picked out %q, want %q", got, want)
	}
}

// A drag the other way picks out the same text: which end the pointer
// started at is not something the user should have to think about.
func TestADragBackwardsPicksOutTheSameText(t *testing.T) {
	tf := newTestForm(t)
	f := tf.form
	f.Fields()[0].SetText("margit.skalarit.net")
	drawForm(f, 60, 24)

	dragField(t, f, 0, 6, 0, 0)

	if got, want := f.Fields()[0].Selected(), "margit"; got != want {
		t.Errorf("the drag picked out %q, want %q", got, want)
	}
}

// A click is a click. Without this a press and release on one spot
// would leave a one-character selection that the next thing typed
// would eat.
func TestAClickOnAFieldPicksNothingOut(t *testing.T) {
	tf := newTestForm(t)
	f := tf.form
	f.Fields()[0].SetText("margit.skalarit.net")
	drawForm(f, 60, 24)

	dragField(t, f, 0, 6, 6, 0)

	if got := f.Fields()[0].Selected(); got != "" {
		t.Errorf("a click picked out %q, want nothing", got)
	}
	if got := f.Fields()[0].Caret(); got != 6 {
		t.Errorf("caret = %d, want the click to have left it at 6", got)
	}
}

// A drag off the right-hand end runs to the end of the text, which is
// how a value wider than its box is picked out whole.
func TestADragOffTheEndOfAFieldTakesTheRest(t *testing.T) {
	tf := newTestForm(t)
	f := tf.form
	f.Fields()[0].SetText("margit.skalarit.net")
	drawForm(f, 60, 24)

	dragField(t, f, 0, 0, 500, 0)

	if got, want := f.Fields()[0].Selected(), "margit.skalarit.net"; got != want {
		t.Errorf("the drag picked out %q, want the whole value", got)
	}
}

// Shift and a click carry the loose end of the selection to where it
// landed, the same as shift and a key that moves.
func TestShiftClickCarriesTheSelectionToTheClick(t *testing.T) {
	tf := newTestForm(t)
	f := tf.form
	fld := f.Fields()[0]
	fld.SetText("margit.skalarit.net")
	drawForm(f, 60, 24)
	dragField(t, f, 0, 0, 6, 0)

	dragField(t, f, 0, 15, 15, input.ModShift)

	if got, want := fld.Selected(), "margit.skalarit"; got != want {
		t.Errorf("shift and a click left %q picked out, want %q", got, want)
	}
}

// The release belongs to the press, so a drag that leaves the row still
// picks text out rather than stopping at the edge of the field.
func TestADragOffAFieldsRowKeepsPicking(t *testing.T) {
	tf := newTestForm(t)
	f := tf.form
	fld := f.Fields()[0]
	fld.SetText("margit.skalarit.net")
	drawForm(f, 60, 24)
	box := f.Box()
	y := box.Y + f.rowsTop()

	mouseTo(t, f, input.MouseEvent{
		Kind: input.MousePress, Button: input.MouseLeft,
		Col: box.X + f.fieldX(), Row: y,
	})
	mouseTo(t, f, input.MouseEvent{
		Kind: input.MouseMove, Button: input.MouseLeft,
		Col: box.X + f.fieldX() + 6, Row: y + 3,
	})
	mouseTo(t, f, input.MouseEvent{
		Kind: input.MouseRelease, Button: input.MouseLeft,
		Col: box.X + f.fieldX() + 6, Row: y + 3,
	})

	if got, want := fld.Selected(), "margit"; got != want {
		t.Errorf("a drag off the row picked out %q, want %q", got, want)
	}
}

// A release that never comes leaves nothing held, so the next time the
// pointer crosses a field with no button down it does not carry on
// picking text out.
func TestCancellingAGestureEndsTheDrag(t *testing.T) {
	tf := newTestForm(t)
	f := tf.form
	fld := f.Fields()[0]
	fld.SetText("margit.skalarit.net")
	drawForm(f, 60, 24)
	box := f.Box()
	y := box.Y + f.rowsTop()
	mouseTo(t, f, input.MouseEvent{
		Kind: input.MousePress, Button: input.MouseLeft,
		Col: box.X + f.fieldX(), Row: y,
	})

	f.CancelGesture()
	mouseTo(t, f, input.MouseEvent{
		Kind: input.MouseMove, Col: box.X + f.fieldX() + 6, Row: y,
	})

	if fld.Dragging() {
		t.Error("the field is still being dragged over")
	}
	if got := fld.Selected(); got != "" {
		t.Errorf("a move after the gesture was cancelled picked out %q", got)
	}
}

// A tick box has nothing to pick out, and a press on one turns it over.
// Dragging away from it must not undo that or start a selection.
func TestADragOffATickBoxPicksNothingOut(t *testing.T) {
	closed := 0
	f := NewForm("Hand this pane over", func() { closed++ })
	f.Style = formStyled()
	f.AddTick("Read only", false)
	f.AddButton(Button{Title: "Hand over", Do: func() error { return nil }})
	f.Layout(Size{Cols: 60, Rows: 24})
	f.SetFocus(true)
	drawForm(f, 60, 24)

	dragField(t, f, 0, 1, 8, 0)

	fld := f.Fields()[0]
	if !fld.On() {
		t.Error("the press did not turn the tick box over")
	}
	if got := fld.Selected(); got != "" {
		t.Errorf("a drag over a tick box picked out %q", got)
	}
}

// A field scrolled along puts the caret on the first character shown
// when its first column is clicked, not on the start of the text.
func TestClickingTheFirstColumnOfAScrolledFieldLandsOnWhatIsShown(t *testing.T) {
	tf := newTestForm(t)
	f := tf.form
	fld := f.Fields()[0]
	fld.SetText("margit.skalarit.net")
	f.Layout(Size{Cols: 28, Rows: 24})
	drawForm(f, 28, 24)
	was := fld.left
	if was == 0 {
		t.Fatal("the field is not scrolled, so this proves nothing")
	}

	dragField(t, f, 0, 0, 0, 0)

	if got := fld.Caret(); got != was {
		t.Errorf("caret = %d, want the first character shown, at %d", got, was)
	}
}

// The palette's query is a text box too, so it drags the same way.
func TestADragOverThePaletteQueryPicksTextOut(t *testing.T) {
	p, _ := newTestPalette(t, testCommands("Copy", "Paste", "Close pane"))
	typeInto(t, p, "close")
	box := p.Box()
	y := box.Y + paletteFrame
	at := func(col int) int { return box.X + paletteFrame + 2 + col }

	mouseTo(t, p, input.MouseEvent{
		Kind: input.MousePress, Button: input.MouseLeft, Col: at(0), Row: y,
	})
	mouseTo(t, p, input.MouseEvent{
		Kind: input.MouseMove, Button: input.MouseLeft, Col: at(3), Row: y,
	})
	mouseTo(t, p, input.MouseEvent{
		Kind: input.MouseRelease, Button: input.MouseLeft, Col: at(3), Row: y,
	})

	if got, want := p.q.Selected(), "clo"; got != want {
		t.Errorf("the drag picked out %q, want %q", got, want)
	}
}

// A press on the query line must not run whatever the list is on.
func TestAPressOnThePaletteQueryRunsNothing(t *testing.T) {
	p, closed := newTestPalette(t, testCommands("Copy", "Paste", "Close pane"))
	typeInto(t, p, "close")
	box := p.Box()

	mouseTo(t, p, input.MouseEvent{
		Kind: input.MousePress, Button: input.MouseLeft,
		Col: box.X + paletteFrame + 2, Row: box.Y + paletteFrame,
	})

	if *closed != 0 {
		t.Errorf("a press on the query line closed the palette %d times", *closed)
	}
}
