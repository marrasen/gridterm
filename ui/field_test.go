package ui

import (
	"image/color"
	"strings"
	"testing"

	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/input"
)

// newTestField returns a field of a known width with colours a test can
// tell apart from a blank cell.
func newTestField(cols int) *Field {
	f := NewField()
	f.Style = FieldStyle{FG: fg, BG: bg, PlaceholderFG: fg}
	f.Layout(Size{Cols: cols, Rows: 1})
	return f
}

// typeInto sends each rune as the text event a keyboard would produce.
func typeField(f *Field, s string) {
	for _, r := range s {
		f.HandleKey(input.Event{Kind: input.Text, Rune: r, NormalText: true})
	}
}

// drawField paints a field and returns the row it wrote, along with the
// cursor the grid ended up with.
func drawField(f *Field, cols int) (string, grid.Cursor) {
	g := grid.New(cols, 1, color.RGBA{}, color.RGBA{})
	g.ResetCursorClaim()
	f.Draw(g.View())
	return strings.TrimRight(rowOf(g, 0), " "), g.Cursor()
}

func TestFieldTypingAppends(t *testing.T) {
	f := newTestField(20)
	typeField(f, "hello")
	if f.Text() != "hello" {
		t.Fatalf("text = %q, want %q", f.Text(), "hello")
	}
	if f.Caret() != len("hello") {
		t.Errorf("caret = %d, want %d", f.Caret(), len("hello"))
	}
}

// The palette could only ever delete the last character. A field has to
// be editable in the middle, or a mistyped hostname has to be retyped.
func TestFieldTypesInTheMiddle(t *testing.T) {
	f := newTestField(20)
	typeField(f, "helo")
	f.SetCaret(3)
	typeField(f, "l")
	if f.Text() != "hello" {
		t.Fatalf("text = %q, want %q", f.Text(), "hello")
	}
}

func TestFieldCaretMoves(t *testing.T) {
	f := newTestField(20)
	typeField(f, "abc")

	f.HandleKey(press(input.KeyLeft, 0))
	if f.Caret() != 2 {
		t.Errorf("after Left, caret = %d, want 2", f.Caret())
	}
	f.HandleKey(press(input.KeyHome, 0))
	if f.Caret() != 0 {
		t.Errorf("after Home, caret = %d, want 0", f.Caret())
	}
	f.HandleKey(press(input.KeyRight, 0))
	if f.Caret() != 1 {
		t.Errorf("after Right, caret = %d, want 1", f.Caret())
	}
	f.HandleKey(press(input.KeyEnd, 0))
	if f.Caret() != 3 {
		t.Errorf("after End, caret = %d, want 3", f.Caret())
	}
	// The ends stop rather than wrapping.
	f.HandleKey(press(input.KeyRight, 0))
	if f.Caret() != 3 {
		t.Errorf("Right past the end moved the caret to %d", f.Caret())
	}
	f.HandleKey(press(input.KeyHome, 0))
	f.HandleKey(press(input.KeyLeft, 0))
	if f.Caret() != 0 {
		t.Errorf("Left past the start moved the caret to %d", f.Caret())
	}
}

func TestFieldBackspaceAndDelete(t *testing.T) {
	f := newTestField(20)
	typeField(f, "abcd")
	f.SetCaret(2)

	f.HandleKey(press(input.KeyBackspace, 0))
	if f.Text() != "acd" {
		t.Fatalf("after Backspace, text = %q, want %q", f.Text(), "acd")
	}
	f.HandleKey(press(input.KeyDelete, 0))
	if f.Text() != "ad" {
		t.Fatalf("after Delete, text = %q, want %q", f.Text(), "ad")
	}
	// Neither does anything at the edge it cannot move past.
	f.HandleKey(press(input.KeyHome, 0))
	f.HandleKey(press(input.KeyBackspace, 0))
	f.HandleKey(press(input.KeyEnd, 0))
	f.HandleKey(press(input.KeyDelete, 0))
	if f.Text() != "ad" {
		t.Fatalf("editing past the edges changed the text to %q", f.Text())
	}
}

func TestFieldWordMoves(t *testing.T) {
	f := newTestField(40)
	typeField(f, "deploy@web1 example")

	f.HandleKey(press(input.KeyLeft, input.ModCtrl))
	if got := f.Text()[f.Caret():]; got != "example" {
		t.Errorf("Ctrl+Left left the caret before %q, want %q", got, "example")
	}
	// Ctrl+Backspace takes the word before the caret, and the space it
	// was separated by with it.
	f.HandleKey(press(input.KeyBackspace, input.ModCtrl))
	if f.Text() != "deploy@example" {
		t.Errorf("text = %q, want %q", f.Text(), "deploy@example")
	}
	f.HandleKey(press(input.KeyRight, input.ModCtrl))
	if f.Caret() != len("deploy@example") {
		t.Errorf("Ctrl+Right left the caret at %d, want the end", f.Caret())
	}
}

func TestFieldCtrlUClearsToTheStart(t *testing.T) {
	f := newTestField(20)
	typeField(f, "abcdef")
	f.SetCaret(4)
	f.HandleKey(press(input.KeyU, input.ModCtrl))
	if f.Text() != "ef" {
		t.Fatalf("text = %q, want %q", f.Text(), "ef")
	}
}

// A password field must not show what was typed, and must still know how
// long it is so the caret lands in the right column.
func TestFieldMaskHidesTheTextButNotTheCaret(t *testing.T) {
	f := newTestField(20)
	f.Mask = '*'
	f.SetFocus(true)
	typeField(f, "hunter2")

	row, cur := drawField(f, 20)
	if row != "*******" {
		t.Fatalf("drew %q, want seven stars", row)
	}
	if f.Text() != "hunter2" {
		t.Fatalf("the mask changed the text to %q", f.Text())
	}
	if cur.X != 7 || !cur.Visible {
		t.Fatalf("cursor = %+v, want a visible one at column 7", cur)
	}
}

// A value longer than the box has to scroll, or the caret walks off the
// edge and the user is typing blind.
func TestFieldScrollsToKeepTheCaretInView(t *testing.T) {
	f := newTestField(10)
	f.SetFocus(true)
	typeField(f, "abcdefghijklmno")

	row, cur := drawField(f, 10)
	if !strings.HasSuffix(row, "o") {
		t.Fatalf("drew %q, want the end of the text", row)
	}
	if cur.X < 0 || cur.X > 9 {
		t.Fatalf("cursor at column %d, outside a field 10 wide", cur.X)
	}

	// Back to the start, and the other end shows instead.
	f.HandleKey(press(input.KeyHome, 0))
	row, cur = drawField(f, 10)
	if !strings.HasPrefix(row, "abc") {
		t.Fatalf("after Home, drew %q, want the start of the text", row)
	}
	if cur.X != 0 {
		t.Fatalf("after Home, cursor at column %d, want 0", cur.X)
	}
}

// A base character and its combining mark share one cell, so the caret
// has to step over both at once or it lands inside a character.
func TestFieldCaretStepsByCluster(t *testing.T) {
	f := newTestField(20)
	// "e" followed by a combining acute accent.
	const combined = "é"
	typeField(f, "a")
	f.insert(combined)
	typeField(f, "b")

	f.HandleKey(press(input.KeyLeft, 0)) // past "b"
	f.HandleKey(press(input.KeyLeft, 0)) // past the whole cluster
	if got := f.Caret(); got != 1 {
		t.Fatalf("caret = %d, want 1: the caret stopped inside a cluster", got)
	}
	f.HandleKey(press(input.KeyDelete, 0))
	if f.Text() != "ab" {
		t.Fatalf("text = %q, want %q: Delete took half a cluster", f.Text(), "ab")
	}
}

func TestFieldPasteTakesOneLine(t *testing.T) {
	f := newTestField(40)
	f.ReadClipboard = func() string { return "one\ntwo" }
	f.HandleKey(press(input.KeyV, input.ModCtrl))
	if f.Text() != "one" {
		t.Fatalf("text = %q, want the first line only", f.Text())
	}
}

// A field with no clipboard has not handled the key, so the window's own
// paste binding still runs. Swallowing it would make Ctrl+Shift+V do
// nothing at all inside a dialog.
func TestFieldWithoutClipboardLetsPasteThrough(t *testing.T) {
	f := newTestField(40)
	took, err := f.HandleKey(press(input.KeyV, input.ModCtrl))
	if err != nil {
		t.Fatalf("paste: %v", err)
	}
	if took {
		t.Error("the field swallowed a paste it could not do")
	}
	if f.Text() != "" {
		t.Errorf("text = %q, want it left empty", f.Text())
	}
}

// AltGr on a Windows layout arrives as Ctrl+Alt and delivers ordinary
// text, so AltGr+V must compose its character rather than pasting.
func TestFieldAltGrDoesNotPaste(t *testing.T) {
	f := newTestField(40)
	f.ReadClipboard = func() string { return "clipboard" }

	took, _ := f.HandleKey(press(input.KeyV, input.ModCtrl|input.ModAlt))
	if took {
		t.Error("AltGr+V was taken as a paste")
	}
	if f.Text() != "" {
		t.Fatalf("AltGr+V pasted %q", f.Text())
	}
}

// The keys a form needs have to travel on, or Tab and Enter would be
// swallowed by whichever field has the caret.
func TestFieldLetsTheFormsKeysThrough(t *testing.T) {
	f := newTestField(20)
	for _, k := range []input.Key{input.KeyEnter, input.KeyEscape, input.KeyTab, input.KeyUp, input.KeyDown} {
		if took, _ := f.HandleKey(press(k, 0)); took {
			t.Errorf("the field swallowed %s", k)
		}
	}
}

// Only the widget holding focus may place the cursor. One that did
// without it would take the cursor from whoever has it.
func TestFieldWithoutFocusPlacesNoCursor(t *testing.T) {
	f := newTestField(20)
	typeField(f, "abc")

	g := grid.New(20, 1, color.RGBA{}, color.RGBA{})
	g.ResetCursorClaim()
	f.Draw(g.View())
	if g.CursorClaimed() {
		t.Fatal("an unfocused field claimed the cursor")
	}
}

// The hint stays while the caret is in an empty field: that is exactly
// when the user is wondering what to type.
func TestFieldPlaceholderShowsWhileEmpty(t *testing.T) {
	f := newTestField(20)
	f.Placeholder = "user@host"

	if row, _ := drawField(f, 20); row != "user@host" {
		t.Fatalf("drew %q, want the placeholder", row)
	}
	f.SetFocus(true)
	row, cur := drawField(f, 20)
	if row != "user@host" {
		t.Fatalf("drew %q with the caret in it, want the placeholder", row)
	}
	if !cur.Visible || cur.X != 0 {
		t.Fatalf("cursor = %+v, want a visible one at the start", cur)
	}
	typeField(f, "a")
	if row, _ := drawField(f, 20); row != "a" {
		t.Fatalf("drew %q, want what was typed", row)
	}
}

// SetCaret takes a byte offset from a mouse click, which can land in the
// middle of a multi-byte character.
func TestFieldSetCaretSnapsToAClusterBoundary(t *testing.T) {
	f := newTestField(20)
	// "a", then "e" with a combining acute accent: one cell, three bytes.
	f.SetText("aéllo")
	f.SetCaret(2) // inside the cluster
	if got := f.Caret(); got != 1 {
		t.Fatalf("caret = %d, want 1: it stopped inside a cluster", got)
	}
}

func TestFieldSetTextReportsTheChange(t *testing.T) {
	f := newTestField(20)
	var seen []string
	f.OnChange = func(s string) { seen = append(seen, s) }

	f.SetText("abc")
	typeField(f, "d")
	f.HandleKey(press(input.KeyBackspace, 0))
	// Setting the same text again changes nothing and must say nothing.
	f.SetText("abc")

	want := []string{"abc", "abcd", "abc"}
	if len(seen) != len(want) {
		t.Fatalf("OnChange saw %q, want %q", seen, want)
	}
	for i := range want {
		if seen[i] != want[i] {
			t.Fatalf("OnChange saw %q, want %q", seen, want)
		}
	}
}

// A field with known answers steps through them, so a value that is
// usually one of a few need not be typed from memory.
func TestFieldCyclesItsOptions(t *testing.T) {
	f := NewField()
	f.Options = []string{"", "edge", "db", "app"}
	f.Layout(Size{Cols: 20, Rows: 1})

	down := func() bool {
		took, _ := f.HandleKey(input.Event{
			Kind: input.KeyPress, Key: input.KeyDown, Mods: input.ModCtrl,
		})
		return took
	}
	up := func() bool {
		took, _ := f.HandleKey(input.Event{
			Kind: input.KeyPress, Key: input.KeyUp, Mods: input.ModCtrl,
		})
		return took
	}

	for _, want := range []string{"edge", "db", "app", "", "edge"} {
		if !down() {
			t.Fatal("the field did not take ctrl+down")
		}
		if got := f.Text(); got != want {
			t.Fatalf("stepping on gave %q, want %q", got, want)
		}
	}
	for _, want := range []string{"", "app", "db", "edge"} {
		if !up() {
			t.Fatal("the field did not take ctrl+up")
		}
		if got := f.Text(); got != want {
			t.Fatalf("stepping back gave %q, want %q", got, want)
		}
	}
}

// Something typed that is not one of the answers is left alone until the
// user asks for one, and then the list starts from its own beginning.
func TestFieldCyclesFromSomethingTyped(t *testing.T) {
	f := NewField()
	f.Options = []string{"edge", "db"}
	f.Layout(Size{Cols: 20, Rows: 1})
	f.SetText("somewhere else")

	f.HandleKey(input.Event{Kind: input.KeyPress, Key: input.KeyDown, Mods: input.ModCtrl})
	if got := f.Text(); got != "edge" {
		t.Fatalf("stepping on from something typed gave %q", got)
	}
	f.SetText("somewhere else")
	f.HandleKey(input.Event{Kind: input.KeyPress, Key: input.KeyUp, Mods: input.ModCtrl})
	if got := f.Text(); got != "db" {
		t.Fatalf("stepping back from something typed gave %q", got)
	}
}

// A field with no answers to offer leaves the keys alone, so whatever
// they are bound to elsewhere still works.
func TestFieldWithNoOptionsLeavesTheKeys(t *testing.T) {
	f := NewField()
	f.Layout(Size{Cols: 20, Rows: 1})
	for _, key := range []input.Key{input.KeyDown, input.KeyUp} {
		if took, _ := f.HandleKey(input.Event{
			Kind: input.KeyPress, Key: key, Mods: input.ModCtrl,
		}); took {
			t.Errorf("the field swallowed ctrl+%v with nothing to offer", key)
		}
	}
	if got := f.Text(); got != "" {
		t.Fatalf("it typed %q", got)
	}
}

// A key that would change nothing is not taken, so it is still whatever
// it is bound to further out.
//
// The Through field of a dialog with one saved server has one option and
// a blank, and once the blank is in the field there is nowhere to go.
func TestFieldLeavesAKeyThatChangesNothing(t *testing.T) {
	f := NewField()
	f.Options = []string{""}
	f.Layout(Size{Cols: 20, Rows: 1})

	for _, key := range []input.Key{input.KeyDown, input.KeyUp} {
		if took, _ := f.HandleKey(input.Event{
			Kind: input.KeyPress, Key: key, Mods: input.ModCtrl,
		}); took {
			t.Errorf("ctrl+%v was taken with nowhere to go", key)
		}
	}
	// And with something else typed, the one option is somewhere to go.
	f.SetText("elsewhere")
	if took, _ := f.HandleKey(input.Event{
		Kind: input.KeyPress, Key: input.KeyDown, Mods: input.ModCtrl,
	}); !took {
		t.Error("ctrl+down was left alone with an option to reach")
	}
}
