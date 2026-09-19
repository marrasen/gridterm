package ui

import (
	"image/color"
	"strings"
	"testing"

	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/input"
)

// aTypedField is a focused field holding s, with a clipboard of its own.
func aTypedField(t *testing.T, cols int, s string) (*Field, *string) {
	t.Helper()
	f := newTestField(cols)
	f.SetFocus(true)
	var clip string
	f.WriteClipboard = func(text string) { clip = text }
	f.ReadClipboard = func() string { return clip }
	typeField(t, f, s)
	return f, &clip
}

// pressField sends one key press with the modifiers given.
func pressField(t *testing.T, f *Field, key input.Key, mods input.Mods) bool {
	t.Helper()
	return keyTo(t, f, input.Event{Kind: input.KeyPress, Key: key, Mods: mods})
}

// Shift and a key that moves picks text out, and the caret's other end
// stays where it was.
func TestShiftPicksTextOutOfAField(t *testing.T) {
	f, _ := aTypedField(t, 20, "hello there")
	f.SetCaret(0)

	for range 5 {
		pressField(t, f, input.KeyRight, input.ModShift)
	}

	if got, want := f.Selected(), "hello"; got != want {
		t.Errorf("it picked out %q, want %q", got, want)
	}
}

// Shift and End takes the rest of the text, and shift and Home takes
// the text before the caret.
func TestShiftHomeAndEndPickToTheEnds(t *testing.T) {
	f, _ := aTypedField(t, 20, "hello there")
	f.SetCaret(6)

	pressField(t, f, input.KeyEnd, input.ModShift)
	if got, want := f.Selected(), "there"; got != want {
		t.Errorf("shift and End picked out %q, want %q", got, want)
	}

	f.SetCaret(6)
	pressField(t, f, input.KeyHome, input.ModShift)
	if got, want := f.Selected(), "hello "; got != want {
		t.Errorf("shift and Home picked out %q, want %q", got, want)
	}
}

// Ctrl and shift with a key that moves picks out a word at a time.
func TestCtrlShiftPicksOutAWord(t *testing.T) {
	f, _ := aTypedField(t, 20, "hello there")
	f.SetCaret(0)

	pressField(t, f, input.KeyRight, input.ModCtrl|input.ModShift)

	if got, want := f.Selected(), "hello"; got != want {
		t.Errorf("it picked out %q, want the first word %q", got, want)
	}
}

// A key that moves with no shift drops the selection, and a plain Left
// or Right lands on the edge the move came from.
func TestMovingWithoutShiftDropsTheSelection(t *testing.T) {
	for what, tc := range map[string]struct {
		key  input.Key
		want int
	}{
		"left":  {input.KeyLeft, 0},
		"right": {input.KeyRight, 5},
	} {
		f, _ := aTypedField(t, 20, "hello there")
		f.SetCaret(0)
		for range 5 {
			pressField(t, f, input.KeyRight, input.ModShift)
		}

		pressField(t, f, tc.key, 0)

		if got := f.Selected(); got != "" {
			t.Errorf("%s: it left %q picked out", what, got)
		}
		if got := f.Caret(); got != tc.want {
			t.Errorf("%s: the caret landed at %d, want %d", what, got, tc.want)
		}
	}
}

// Ctrl+C copies the selection and leaves the text alone. Ctrl+X copies
// it and takes it out.
func TestCopyAndCutAField(t *testing.T) {
	f, clip := aTypedField(t, 20, "hello there")
	f.SetCaret(0)
	for range 5 {
		pressField(t, f, input.KeyRight, input.ModShift)
	}

	pressField(t, f, input.KeyC, input.ModCtrl)

	if got, want := *clip, "hello"; got != want {
		t.Errorf("the copy put %q on the clipboard, want %q", got, want)
	}
	if got, want := f.Text(), "hello there"; got != want {
		t.Errorf("the copy changed the field to %q, want %q", got, want)
	}

	pressField(t, f, input.KeyX, input.ModCtrl)

	if got, want := *clip, "hello"; got != want {
		t.Errorf("the cut put %q on the clipboard, want %q", got, want)
	}
	if got, want := f.Text(), " there"; got != want {
		t.Errorf("the cut left %q, want %q", got, want)
	}
	if got := f.Caret(); got != 0 {
		t.Errorf("the cut left the caret at %d, want the start of the gap", got)
	}
}

// A masked field copies nothing. It is masked so that what is in it is
// not shown, and the clipboard would show it.
func TestAMaskedFieldCopiesNothing(t *testing.T) {
	f, clip := aTypedField(t, 20, "hunter2")
	f.Mask = '*'
	f.SelectAll()

	if pressField(t, f, input.KeyC, input.ModCtrl) {
		t.Error("a masked field took the copy")
	}
	if *clip != "" {
		t.Errorf("a masked field put %q on the clipboard", *clip)
	}

	if pressField(t, f, input.KeyX, input.ModCtrl) {
		t.Error("a masked field took the cut")
	}
	if got, want := f.Text(), "hunter2"; got != want {
		t.Errorf("the cut left %q, want the text untouched as %q", got, want)
	}
}

// Ctrl+A picks out the whole text.
func TestSelectAllInAField(t *testing.T) {
	f, _ := aTypedField(t, 20, "hello there")
	f.SetCaret(4)

	pressField(t, f, input.KeyA, input.ModCtrl)

	if got, want := f.Selected(), "hello there"; got != want {
		t.Errorf("it picked out %q, want the whole text %q", got, want)
	}
}

// Typing replaces what is picked out, the way it does everywhere else.
func TestTypingReplacesTheSelection(t *testing.T) {
	f, _ := aTypedField(t, 20, "hello there")
	f.SetCaret(0)
	for range 5 {
		pressField(t, f, input.KeyRight, input.ModShift)
	}

	typeField(t, f, "bye")

	if got, want := f.Text(), "bye there"; got != want {
		t.Errorf("typing left %q, want %q", got, want)
	}
	if got := f.Selected(); got != "" {
		t.Errorf("typing left %q picked out", got)
	}
}

// Backspace and Delete take the selection when there is one, rather
// than one character beside the caret.
func TestBackspaceAndDeleteTakeTheSelection(t *testing.T) {
	for what, key := range map[string]input.Key{
		"backspace": input.KeyBackspace,
		"delete":    input.KeyDelete,
	} {
		f, _ := aTypedField(t, 20, "hello there")
		f.SetCaret(0)
		for range 6 {
			pressField(t, f, input.KeyRight, input.ModShift)
		}

		pressField(t, f, key, 0)

		if got, want := f.Text(), "there"; got != want {
			t.Errorf("%s left %q, want %q", what, got, want)
		}
	}
}

// Pasting puts the clipboard in place of the selection.
func TestPastingReplacesTheSelection(t *testing.T) {
	f, clip := aTypedField(t, 20, "hello there")
	*clip = "bye"
	f.SetCaret(0)
	for range 5 {
		pressField(t, f, input.KeyRight, input.ModShift)
	}

	pressField(t, f, input.KeyV, input.ModCtrl)

	if got, want := f.Text(), "bye there"; got != want {
		t.Errorf("the paste left %q, want %q", got, want)
	}
}

// The selection is drawn in the field's own colours swapped, so it
// stands out whatever those two colours are.
func TestTheFieldSelectionIsDrawn(t *testing.T) {
	f, _ := aTypedField(t, 20, "hello there")
	f.SetCaret(0)
	for range 5 {
		pressField(t, f, input.KeyRight, input.ModShift)
	}

	g := grid.New(20, 1, color.RGBA{}, color.RGBA{})
	f.Draw(g.View())

	for x := range 5 {
		c := g.At(x, 0)
		if c.FG != f.Style.BG || c.BG != f.Style.FG {
			t.Errorf("column %d is %v on %v, want the colours swapped", x, c.FG, c.BG)
		}
	}
	if c := g.At(5, 0); c.FG != f.Style.FG || c.BG != f.Style.BG {
		t.Errorf("the column past the selection is %v on %v, want the ordinary colours", c.FG, c.BG)
	}
}

// The keys leaving a field take its selection with them, so nothing is
// left highlighted in a field nobody is typing in.
func TestLeavingAFieldDropsItsSelection(t *testing.T) {
	f, _ := aTypedField(t, 20, "hello there")
	f.SelectAll()

	f.SetFocus(false)

	if got := f.Selected(); got != "" {
		t.Errorf("the field kept %q picked out after the keys left", got)
	}
}

// A field with no clipboard does not take the copy, so whatever is
// showing it can still act on the key.
func TestAFieldWithNoClipboardDoesNotTakeTheCopy(t *testing.T) {
	f := newTestField(20)
	f.SetFocus(true)
	typeField(t, f, "hello")
	f.SelectAll()

	if pressField(t, f, input.KeyC, input.ModCtrl) {
		t.Error("a field with no clipboard took the copy")
	}
}

// A tick box takes none of this: there is nothing in it to pick out.
func TestATickBoxTakesNoSelection(t *testing.T) {
	f := NewTick(false)
	f.Style = FieldStyle{FG: fg, BG: bg}
	f.Layout(Size{Cols: 10, Rows: 1})
	f.SetFocus(true)

	for _, key := range []input.Key{input.KeyC, input.KeyX, input.KeyA} {
		if pressField(t, f, key, input.ModCtrl) {
			t.Errorf("a tick box took ctrl and %v", key)
		}
	}
	if got := f.Selected(); got != "" {
		t.Errorf("a tick box has %q picked out", got)
	}
}

// A word move and a jump to either end drop the selection as well.
func TestAWordMoveAndAJumpDropTheSelection(t *testing.T) {
	for what, tc := range map[string]struct {
		key  input.Key
		mods input.Mods
	}{
		"ctrl and left":  {input.KeyLeft, input.ModCtrl},
		"ctrl and right": {input.KeyRight, input.ModCtrl},
		"home":           {input.KeyHome, 0},
		"end":            {input.KeyEnd, 0},
	} {
		f, _ := aTypedField(t, 20, "hello there")
		f.SetCaret(6)
		f.SelectAll()

		pressField(t, f, tc.key, tc.mods)

		if got := f.Selected(); got != "" {
			t.Errorf("%s: it left %q picked out", what, got)
		}
	}
}

// Ctrl+Shift+A is the window's, not the field's, unlike Ctrl+Shift+C.
// The palette hands on whatever its query line declines, so a field
// taking it would take the pane switcher away there.
func TestCtrlShiftAIsNotTheFields(t *testing.T) {
	f, _ := aTypedField(t, 20, "hello there")

	if pressField(t, f, input.KeyA, input.ModCtrl|input.ModShift) {
		t.Error("the field took ctrl+shift+A")
	}
	if got := f.Selected(); got != "" {
		t.Errorf("ctrl+shift+A picked out %q", got)
	}
}

// Ctrl+Shift+U is the window's too: it unsplits a pane, and a field
// that cleared itself on it would take that away.
func TestCtrlShiftUIsNotTheFields(t *testing.T) {
	f, _ := aTypedField(t, 20, "hello there")
	f.SetCaret(len("hello there"))

	if pressField(t, f, input.KeyU, input.ModCtrl|input.ModShift) {
		t.Error("the field took ctrl+shift+U")
	}
	if got, want := f.Text(), "hello there"; got != want {
		t.Errorf("it left %q, want the text untouched as %q", got, want)
	}

	// Ctrl alone still clears back to the start.
	if !pressField(t, f, input.KeyU, input.ModCtrl) {
		t.Error("the field let ctrl+U past")
	}
	if got := f.Text(); got != "" {
		t.Errorf("ctrl+U left %q, want the field cleared", got)
	}
}

// Ctrl+A in an empty field takes nothing, so the key travels on.
func TestSelectAllInAnEmptyFieldTakesNothing(t *testing.T) {
	f, _ := aTypedField(t, 20, "")

	if pressField(t, f, input.KeyA, input.ModCtrl) {
		t.Error("an empty field took ctrl+A")
	}
}

// Ctrl+C and Ctrl+X with nothing picked out take nothing, so whatever
// is showing the field can still act on the key.
func TestCopyAndCutWithNoSelectionTakeNothing(t *testing.T) {
	for what, key := range map[string]input.Key{
		"copy": input.KeyC,
		"cut":  input.KeyX,
	} {
		f, clip := aTypedField(t, 20, "hello there")

		if pressField(t, f, key, input.ModCtrl) {
			t.Errorf("the field took the %s with nothing picked out", what)
		}
		if *clip != "" {
			t.Errorf("the %s put %q on the clipboard", what, *clip)
		}
	}
}

// Ctrl+Insert copies, the way Shift+Insert pastes.
func TestCtrlInsertCopies(t *testing.T) {
	f, clip := aTypedField(t, 20, "hello there")
	f.SelectAll()

	pressField(t, f, input.KeyInsert, input.ModCtrl)

	if got, want := *clip, "hello there"; got != want {
		t.Errorf("ctrl+insert put %q on the clipboard, want %q", got, want)
	}
}

// Shift+Insert pastes over the selection, the same as ctrl+V.
func TestShiftInsertPastesOverTheSelection(t *testing.T) {
	f, clip := aTypedField(t, 20, "hello there")
	*clip = "bye"
	f.SetCaret(0)
	for range 5 {
		pressField(t, f, input.KeyRight, input.ModShift)
	}

	pressField(t, f, input.KeyInsert, input.ModShift)

	if got, want := f.Text(), "bye there"; got != want {
		t.Errorf("it left %q, want %q", got, want)
	}
}

// Shift and a key that moves back shrinks the selection rather than
// starting another one.
func TestShiftBackShrinksTheSelection(t *testing.T) {
	f, _ := aTypedField(t, 20, "hello there")
	f.SetCaret(0)
	for range 5 {
		pressField(t, f, input.KeyRight, input.ModShift)
	}

	for range 2 {
		pressField(t, f, input.KeyLeft, input.ModShift)
	}

	if got, want := f.Selected(), "hel"; got != want {
		t.Errorf("it picked out %q, want %q", got, want)
	}
}

// Ctrl and shift with Left picks out the word behind the caret.
func TestCtrlShiftLeftPicksOutAWordBack(t *testing.T) {
	f, _ := aTypedField(t, 20, "hello there")
	f.SetCaret(len("hello there"))

	pressField(t, f, input.KeyLeft, input.ModCtrl|input.ModShift)

	if got, want := f.Selected(), "there"; got != want {
		t.Errorf("it picked out %q, want the last word %q", got, want)
	}
}

// Shift and Right at the end of the text picks text out rather than
// taking the rest of the answer the caller is offering.
func TestShiftRightDoesNotTakeTheGhost(t *testing.T) {
	for what, key := range map[string]input.Key{
		"right": input.KeyRight,
		"end":   input.KeyEnd,
	} {
		f, _ := aTypedField(t, 30, "/home/mar")
		f.Ghost = "cus/"
		drawField(f, 30)
		f.SelectAll()

		pressField(t, f, key, input.ModShift)

		if got, want := f.Text(), "/home/mar"; got != want {
			t.Errorf("shift and %s left %q, want the text untouched as %q", what, got, want)
		}
		if got, want := f.Selected(), "/home/mar"; got != want {
			t.Errorf("shift and %s left %q picked out, want %q", what, got, want)
		}
	}
}

// Typing over a selection reports the change once. A caller watching
// the field would otherwise see a value the user never had.
func TestTypingOverASelectionReportsOneChange(t *testing.T) {
	f, _ := aTypedField(t, 20, "hello there")
	f.SetCaret(0)
	for range 5 {
		pressField(t, f, input.KeyRight, input.ModShift)
	}
	var said []string
	f.OnChange = func(s string) { said = append(said, s) }

	typeField(t, f, "b")

	if len(said) != 1 {
		t.Errorf("it reported %q, want the one value %q", said, "b there")
	}
}

// Putting text in a field takes the selection with it, so nothing
// points into text that has gone.
func TestReplacingTheTextDropsTheSelection(t *testing.T) {
	f, _ := aTypedField(t, 20, "hello there")
	f.SelectAll()

	f.SetText("hi")

	if got := f.Selected(); got != "" {
		t.Errorf("it kept %q picked out", got)
	}
}

// Stepping through the options takes the selection with it. A selection
// reaching past the end of a shorter option would point at text that is
// not there.
func TestSteppingThroughOptionsDropsTheSelection(t *testing.T) {
	f, _ := aTypedField(t, 20, "a long first answer")
	f.Options = []string{"a long first answer", "no"}
	f.SelectAll()

	pressField(t, f, input.KeyDown, input.ModCtrl)

	if got := f.Selected(); got != "" {
		t.Errorf("it kept %q picked out", got)
	}
}

// Moving the caret from outside takes the selection with it, which is
// what a click into a field does.
func TestSettingTheCaretDropsTheSelection(t *testing.T) {
	f, _ := aTypedField(t, 20, "hello there")
	f.SelectAll()

	f.SetCaret(3)

	if got := f.Selected(); got != "" {
		t.Errorf("it kept %q picked out", got)
	}
}

// A field without the keys draws no highlight. A selection in a field
// nobody is typing in would read as the live one.
func TestAnUnfocusedFieldDrawsNoHighlight(t *testing.T) {
	f, _ := aTypedField(t, 20, "hello there")
	f.SelectAll()
	f.picked = true

	f.focused = false
	g := grid.New(20, 1, color.RGBA{}, color.RGBA{})
	f.Draw(g.View())

	if c := g.At(0, 0); c.FG != f.Style.FG || c.BG != f.Style.BG {
		t.Errorf("column 0 is %v on %v, want the ordinary colours", c.FG, c.BG)
	}
}

// The highlight lands on the right columns in a field scrolled
// sideways, where the first byte drawn is not the first byte of the
// text.
func TestTheHighlightFollowsAScrolledField(t *testing.T) {
	f, _ := aTypedField(t, 6, "0123456789")
	f.SetCaret(len("0123456789"))
	for range 3 {
		pressField(t, f, input.KeyLeft, input.ModShift)
	}
	if got, want := f.Selected(), "789"; got != want {
		t.Fatalf("it picked out %q, want %q", got, want)
	}

	// The last column is kept for the caret, so five characters show.
	text, _ := drawField(f, 6)
	if got, want := text, "56789"; got != want {
		t.Fatalf("the field shows %q, want %q", got, want)
	}
	g := grid.New(6, 1, color.RGBA{}, color.RGBA{})
	f.Draw(g.View())

	// "789" is columns 2 to 4 of what is shown.
	for x := range 6 {
		swapped := g.At(x, 0).BG == f.Style.FG
		if want := x >= 2 && x <= 4; swapped != want {
			t.Errorf("column %d swapped=%v, want %v", x, swapped, want)
		}
	}
}

// A selection over a double-width character takes it whole and
// highlights both of its columns.
func TestAFieldSelectionTakesAWideCharacterWhole(t *testing.T) {
	f, clip := aTypedField(t, 20, "a\u4e16b")
	f.SetCaret(0)

	pressField(t, f, input.KeyRight, input.ModShift)
	pressField(t, f, input.KeyRight, input.ModShift)

	if got, want := f.Selected(), "a\u4e16"; got != want {
		t.Errorf("it picked out %q, want %q", got, want)
	}
	pressField(t, f, input.KeyC, input.ModCtrl)
	if got, want := *clip, "a\u4e16"; got != want {
		t.Errorf("it copied %q, want %q", got, want)
	}

	g := grid.New(20, 1, color.RGBA{}, color.RGBA{})
	f.Draw(g.View())
	for x := range 3 {
		if got := g.At(x, 0).BG; got != f.Style.FG {
			t.Errorf("column %d is %v, want the colours swapped", x, got)
		}
	}
	if got := g.At(3, 0).BG; got != f.Style.BG {
		t.Errorf("the column past the selection is %v, want the ordinary %v", got, f.Style.BG)
	}
}

// A combining mark stays with the character it belongs to, both in what
// is picked out and in what is copied.
func TestAFieldSelectionKeepsACombiningMark(t *testing.T) {
	f, _ := aTypedField(t, 20, "ae\u0301b")
	f.SetCaret(0)

	pressField(t, f, input.KeyRight, input.ModShift)
	pressField(t, f, input.KeyRight, input.ModShift)

	if got, want := f.Selected(), "ae\u0301"; got != want {
		t.Errorf("it picked out %q, want %q", got, want)
	}
}

// A masked field still edits what is picked out, even though it copies
// nothing: the mask is about showing it, not about changing it.
func TestAMaskedFieldStillEditsTheSelection(t *testing.T) {
	f, _ := aTypedField(t, 20, "hunter2")
	f.Mask = '*'
	f.SetCaret(0)
	for range 6 {
		pressField(t, f, input.KeyRight, input.ModShift)
	}

	pressField(t, f, input.KeyBackspace, 0)

	if got, want := f.Text(), "2"; got != want {
		t.Errorf("backspace left %q, want %q", got, want)
	}
}

// The caret stays on a cluster boundary after typing, even where what
// was typed joins with the text after it into one character.
//
// A caret inside a cluster would let a selection hand half a character
// to the clipboard.
func TestTypingLeavesTheCaretOnABoundary(t *testing.T) {
	f, _ := aTypedField(t, 20, "")
	f.SetText("\u200d\u200dx")
	f.SetCaret(0)

	typeField(t, f, "Z")

	at := f.Caret()
	for _, m := range f.bounds() {
		if m == at {
			return
		}
	}
	t.Errorf("the caret is at %d, which is not one of the boundaries %v", at, f.bounds())
}

// Pasting nothing over a selection leaves the field alone. A clipboard
// holding a picture rather than text reads as empty, and replacing the
// selection with nothing is text the user cannot get back.
func TestPastingNothingLeavesTheSelectionAlone(t *testing.T) {
	for what, ev := range map[string]input.Event{
		"ctrl+V":       {Kind: input.KeyPress, Key: input.KeyV, Mods: input.ModCtrl},
		"shift+insert": {Kind: input.KeyPress, Key: input.KeyInsert, Mods: input.ModShift},
	} {
		f, clip := aTypedField(t, 20, "margit.skalarit.net")
		*clip = ""
		f.SelectAll()

		keyTo(t, f, ev)

		if got, want := f.Text(), "margit.skalarit.net"; got != want {
			t.Errorf("%s left %q, want the text untouched as %q", what, got, want)
		}
	}
}

// Shift and a key that cannot move picks nothing out, so the next plain
// key still does what it does.
//
// Left alone, shift at the end of the text leaves the two ends of the
// selection together, and a plain Left then lands where the caret
// already is and reads as a dead key.
func TestShiftThatCannotMovePicksNothing(t *testing.T) {
	f, _ := aTypedField(t, 20, "hello")
	f.SetCaret(len("hello"))

	pressField(t, f, input.KeyRight, input.ModShift)

	if f.picked {
		t.Error("shift at the end of the text left a selection with nothing in it")
	}

	pressField(t, f, input.KeyLeft, 0)

	if got, want := f.Caret(), len("hell"); got != want {
		t.Errorf("the plain left landed at %d, want %d", got, want)
	}
}

// The completion is still taken after a shift that could not move. It
// is exactly what somebody does while looking at one.
func TestTheGhostIsTakenAfterAShiftThatCannotMove(t *testing.T) {
	f, _ := aTypedField(t, 30, "hel")
	f.Ghost = "lo there"
	drawField(f, 30)

	pressField(t, f, input.KeyRight, input.ModShift)
	pressField(t, f, input.KeyRight, 0)

	if got, want := f.Text(), "hello there"; got != want {
		t.Errorf("it left %q, want the completion taken as %q", got, want)
	}
}

// The caret lands past what was typed, even where what was typed joins
// with the text after it into one character.
func TestTypingLeavesTheCaretPastWhatWentIn(t *testing.T) {
	for what, tc := range map[string]struct {
		have, typed, want string
	}{
		"a combining mark": {"\u0301x", "e", "e\u0301"},
		"a flag":           {"\U0001f1ea", "\U0001f1f8", "\U0001f1f8\U0001f1ea"},
	} {
		f, _ := aTypedField(t, 20, "")
		f.SetText(tc.have)
		f.SetCaret(0)

		typeField(t, f, tc.typed)

		if got, want := f.Caret(), len(tc.want); got != want {
			t.Errorf("%s: the caret is at %d, want %d, past %q", what, got, want, tc.want)
		}
	}
}

// The first byte drawn stays on a character boundary when text is taken
// out from under it, so the field never draws half a character.
func TestTakingTextOutKeepsTheDrawingOnABoundary(t *testing.T) {
	f, clip := aTypedField(t, 16, "abcdefghijklmnopqrstuvwxyz0123456789ABCDEFGHIJ")
	*clip = "caf\u00e9"
	f.SetCaret(0)
	for range 19 {
		pressField(t, f, input.KeyRight, input.ModShift)
	}

	pressField(t, f, input.KeyV, input.ModCtrl)

	text, _ := drawField(f, 16)
	if strings.ContainsRune(text, '\ufffd') {
		t.Errorf("the field draws %q, which holds half a character", text)
	}
}
