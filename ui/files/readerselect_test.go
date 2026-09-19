package files

import (
	"errors"
	"strings"
	"testing"

	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/input"
	"github.com/marrasen/gridterm/ui"
)

// aReaderOf is a reader holding the lines it is given.
func aReaderOf(t *testing.T, cols, rows int, lines ...string) *Reader {
	t.Helper()
	r := NewReader("notes.txt", "/tmp/notes.txt")
	r.Style = readerStyle()
	r.Read = func(then func([]string, bool, error)) { then(lines, false, nil) }
	r.Layout(ui.Size{Cols: cols, Rows: rows})
	r.Open()
	return r
}

// drag presses at one place, moves to another and lets go.
func drag(t *testing.T, r *Reader, fromCol, fromRow, toCol, toRow int) {
	t.Helper()
	mouse(t, r, input.MouseEvent{
		Kind: input.MousePress, Button: input.MouseLeft, Col: fromCol, Row: fromRow,
	})
	mouse(t, r, input.MouseEvent{
		Kind: input.MouseMove, Button: input.MouseLeft, Col: toCol, Row: toRow,
	})
	mouse(t, r, input.MouseEvent{
		Kind: input.MouseRelease, Button: input.MouseLeft, Col: toCol, Row: toRow,
	})
}

// mouse hands one mouse event to a reader.
func mouse(t *testing.T, r *Reader, ev input.MouseEvent) {
	t.Helper()
	if _, err := r.HandleMouse(ev); err != nil {
		t.Fatalf("the mouse: %v", err)
	}
}

// shiftKey presses a key with shift held.
func shiftKey(r *Reader, key input.Key) {
	_, _ = r.HandleKey(input.Event{Kind: input.KeyPress, Key: key, Mods: input.ModShift})
}

// ctrlKey presses a key with ctrl held.
func ctrlKey(r *Reader, key input.Key) {
	_, _ = r.HandleKey(input.Event{Kind: input.KeyPress, Key: key, Mods: input.ModCtrl})
}

// A drag across one line picks out the text under it, both ends
// included, and the copy hands that text over.
func TestADragPicksOutTextOnOneLine(t *testing.T) {
	r := aReaderOf(t, 40, 10, "hello there", "second line")
	var copied string
	r.OnCopy = func(text string) { copied = text }

	drag(t, r, 0, 1, 4, 1)

	if got, want := r.SelectedText(), "hello"; got != want {
		t.Errorf("it picked out %q, want %q", got, want)
	}
	if !r.Copy() {
		t.Fatal("the copy said there was nothing to copy")
	}
	if got, want := copied, "hello"; got != want {
		t.Errorf("it copied %q, want %q", got, want)
	}
}

// A drag down the file takes the end of the first line, the whole of
// every line between, and the start of the last.
func TestADragAcrossLinesTakesWholeLinesBetween(t *testing.T) {
	r := aReaderOf(t, 40, 10, "one two", "middle", "three four")

	drag(t, r, 4, 1, 4, 3)

	want := "two\nmiddle\nthree"
	if got := r.SelectedText(); got != want {
		t.Errorf("it picked out %q, want %q", got, want)
	}
}

// A drag the other way picks out the same text: which end was pressed
// first does not change what is between them.
func TestADragUpwardsPicksTheSameText(t *testing.T) {
	r := aReaderOf(t, 40, 10, "one two", "middle", "three four")

	drag(t, r, 4, 3, 4, 1)

	want := "two\nmiddle\nthree"
	if got := r.SelectedText(); got != want {
		t.Errorf("it picked out %q, want %q", got, want)
	}
}

// A click that never moved is a click, not one column left highlighted.
func TestAClickThatNeverMovedPicksNothing(t *testing.T) {
	r := aReaderOf(t, 40, 10, "hello there")

	drag(t, r, 3, 1, 3, 1)

	if r.Selected() {
		t.Errorf("a click left %q picked out", r.SelectedText())
	}
}

// The selection is drawn, not only recorded: the cells it covers carry
// the selected background and the ones beside it do not.
func TestTheSelectionIsDrawn(t *testing.T) {
	r := aReaderOf(t, 40, 10, "hello there")

	drag(t, r, 0, 1, 4, 1)
	g := drawReader(r, 40, 10)

	for x := 0; x <= 4; x++ {
		if got, want := g.At(x, 1).BG, r.Style.SelectedBG; got != want {
			t.Errorf("column %d is %v, want the selected %v", x, got, want)
		}
	}
	if got, want := g.At(5, 1).BG, r.Style.BG; got != want {
		t.Errorf("the column past the selection is %v, want the ordinary %v", got, want)
	}
}

// The selection stays on the text it was made on while the view
// scrolls: it is kept in the file's own lines, not the screen's rows.
func TestTheSelectionStaysOnItsTextWhileScrolling(t *testing.T) {
	lines := make([]string, 40)
	for i := range lines {
		lines[i] = "line " + strings.Repeat("x", i)
	}
	r := aReaderOf(t, 40, 10, lines...)

	drag(t, r, 0, 1, 3, 1)
	if got, want := r.SelectedText(), "line"; got != want {
		t.Fatalf("it picked out %q, want %q", got, want)
	}
	r.Scroll(5)

	if got, want := r.SelectedText(), "line"; got != want {
		t.Errorf("after scrolling it holds %q, want %q", got, want)
	}
	g := drawReader(r, 40, 10)
	if got, want := g.At(0, 1).BG, r.Style.BG; got != want {
		t.Errorf("the top row is %v, want the ordinary %v: the selection scrolled off", got, want)
	}
}

// Shift and a key that moves picks text out without a mouse, and the
// view follows the end of it.
func TestShiftAndAKeyPicksTextOut(t *testing.T) {
	r := aReaderOf(t, 40, 10, "one", "two", "three")

	shiftKey(r, input.KeyRight)
	shiftKey(r, input.KeyRight)

	if got, want := r.SelectedText(), "one"; got != want {
		t.Errorf("it picked out %q, want %q", got, want)
	}

	shiftKey(r, input.KeyDown)
	if got, want := r.SelectedText(), "one\ntwo"; got != want {
		t.Errorf("a line further down it holds %q, want %q", got, want)
	}
}

// Shift and End takes the selection to the end of its line, and shift
// and Home back to the start.
func TestShiftAndEndTakesTheWholeLine(t *testing.T) {
	r := aReaderOf(t, 40, 10, "one two three", "second")

	shiftKey(r, input.KeyEnd)

	if got, want := r.SelectedText(), "one two three"; got != want {
		t.Errorf("it picked out %q, want the whole line %q", got, want)
	}
}

// The view scrolls to follow a selection reaching past the bottom of
// the pane. A selection nobody can see is one nobody can judge.
func TestThePaneFollowsASelectionPastItsBottom(t *testing.T) {
	lines := make([]string, 40)
	for i := range lines {
		lines[i] = "line"
	}
	r := aReaderOf(t, 40, 6, lines...)
	rows := r.Lines()
	if rows < 20 {
		t.Fatalf("the reader holds %d lines", rows)
	}

	for range 20 {
		shiftKey(r, input.KeyDown)
	}

	if r.Top() == 0 {
		t.Error("the pane did not follow the selection down the file")
	}
}

// Ctrl+A picks out the whole file, and Escape drops it.
func TestSelectAllAndEscape(t *testing.T) {
	r := aReaderOf(t, 40, 10, "one", "two")

	ctrlKey(r, input.KeyA)

	if got, want := r.SelectedText(), "one\ntwo"; got != want {
		t.Errorf("it picked out %q, want the whole file %q", got, want)
	}

	if _, err := r.HandleKey(input.Event{Kind: input.KeyPress, Key: input.KeyEscape}); err != nil {
		t.Fatalf("escape: %v", err)
	}
	if r.Selected() {
		t.Errorf("escape left %q picked out", r.SelectedText())
	}
}

// The bar offers a copy while something is picked out, and does not
// while nothing is: a key that would do nothing is not worth a place.
func TestTheBarOffersACopyOnlyWhenThereIsOne(t *testing.T) {
	r := aReaderOf(t, 60, 10, "hello there")

	if has, keys := offersCopy(r); has {
		t.Errorf("the bar offers a copy with nothing picked out: %v", keys)
	}

	drag(t, r, 0, 1, 4, 1)

	if has, keys := offersCopy(r); !has {
		t.Errorf("the bar offers no copy with %q picked out: %v", r.SelectedText(), keys)
	}
}

// offersCopy reports whether the bar has a copy on it, and what it has.
func offersCopy(r *Reader) (bool, []string) {
	var titles []string
	has := false
	for _, k := range r.keys() {
		titles = append(titles, k.Title)
		if k.Title == "Copy" {
			has = true
		}
	}
	return has, titles
}

// Turning hex on drops the selection. The lines are not the lines it
// was made on, so what it covered is not there any more.
func TestHexDropsTheSelection(t *testing.T) {
	r := aReaderOf(t, 60, 10, "hello there")

	drag(t, r, 0, 1, 4, 1)
	r.Hex(true)

	if r.Selected() {
		t.Errorf("hex left %q picked out", r.SelectedText())
	}
}

// A selection over a double-width character takes the character whole.
// Half of one is not a character, and a cell is not always one column.
func TestASelectionTakesAWideCharacterWhole(t *testing.T) {
	line := "ab世界cd"
	r := aReaderOf(t, 40, 10, line)
	if grid.StringWidth(line) != 8 {
		t.Fatalf("the line is %d columns wide", grid.StringWidth(line))
	}

	// Columns 2 to 5 are the two wide characters.
	drag(t, r, 2, 1, 5, 1)

	if got, want := r.SelectedText(), "世界"; got != want {
		t.Errorf("it picked out %q, want %q", got, want)
	}
}

// A selection that starts on the second half of a wide character takes
// that character whole rather than half of it.
func TestASelectionStartingMidCharacterTakesItWhole(t *testing.T) {
	r := aReaderOf(t, 40, 10, "a世b")

	drag(t, r, 2, 1, 3, 1)

	if got, want := r.SelectedText(), "世b"; got != want {
		t.Errorf("it picked out %q, want %q", got, want)
	}
}

// A selection made while the view is scrolled sideways lands on the
// text under the pointer, not on the start of the line.
func TestASelectionFollowsTheSidewaysScroll(t *testing.T) {
	r := aReaderOf(t, 10, 10, "0123456789abcdef")

	r.Sideways(4)
	drag(t, r, 0, 1, 3, 1)

	if got, want := r.SelectedText(), "4567"; got != want {
		t.Errorf("it picked out %q, want %q", got, want)
	}
}

// A drag that runs past the end of a short line stops at the end of it
// rather than picking out blanks.
func TestADragPastTheEndOfALineStopsThere(t *testing.T) {
	r := aReaderOf(t, 40, 10, "short")

	drag(t, r, 0, 1, 30, 1)

	if got, want := r.SelectedText(), "short"; got != want {
		t.Errorf("it picked out %q, want %q", got, want)
	}
}

// A selection ending on the first half of a double-width character
// takes that character whole.
func TestASelectionEndingMidCharacterTakesItWhole(t *testing.T) {
	r := aReaderOf(t, 40, 10, "a\u4e16b")

	// Column 1 is the first half of the wide character.
	drag(t, r, 0, 1, 1, 1)

	if got, want := r.SelectedText(), "a\u4e16"; got != want {
		t.Errorf("it picked out %q, want %q", got, want)
	}
}

// The highlight stops at the end of a line rather than running on to
// the edge of the pane: what is drawn is what would be copied.
func TestTheHighlightStopsAtTheEndOfALine(t *testing.T) {
	r := aReaderOf(t, 40, 10, "short", "a much longer second line")

	drag(t, r, 0, 1, 20, 2)
	g := drawReader(r, 40, 10)

	if got, want := g.At(4, 1).BG, r.Style.SelectedBG; got != want {
		t.Errorf("the last column of the short line is %v, want the selected %v", got, want)
	}
	if got, want := g.At(5, 1).BG, r.Style.BG; got != want {
		t.Errorf("the column past the short line is %v, want the ordinary %v", got, want)
	}
}

// The highlight moves with the sideways scroll, so it stays on the text
// it was made on.
func TestTheHighlightFollowsTheSidewaysScroll(t *testing.T) {
	r := aReaderOf(t, 10, 10, "0123456789abcdef")

	drag(t, r, 0, 1, 3, 1)
	r.Sideways(4)
	g := drawReader(r, 10, 10)

	// Columns 0 to 3 of the file are now four columns off the left edge,
	// so nothing on screen is highlighted.
	for x := range 10 {
		if got, want := g.At(x, 1).BG, r.Style.BG; got != want {
			t.Errorf("column %d is %v, want the ordinary %v", x, got, want)
		}
	}
	r.Sideways(-2)
	g = drawReader(r, 10, 10)
	if got, want := g.At(0, 1).BG, r.Style.SelectedBG; got != want {
		t.Errorf("column 0 is %v, want the selected %v: column 2 of the file is picked out", got, want)
	}
}

// A file that shrank past the selection drops it. The text it was made
// on has gone, and moving the highlight onto whatever is at the end now
// would say it covered something nobody picked.
func TestAShrunkFileDropsTheSelection(t *testing.T) {
	lines := []string{"one", "two", "three", "four", "five"}
	r := NewReader("notes.txt", "/tmp/notes.txt")
	r.Style = readerStyle()
	r.Read = func(then func([]string, bool, error)) { then(lines, false, nil) }
	r.Layout(ui.Size{Cols: 40, Rows: 10})
	r.Open()

	drag(t, r, 0, 4, 3, 5)
	if got, want := r.SelectedText(), "four\nfive"; got != want {
		t.Fatalf("it picked out %q, want %q", got, want)
	}

	// Shrunk past the end of the selection but not past its start, which
	// is the case that would otherwise leave half of it highlighted.
	lines = []string{"one", "two", "three", "four"}
	r.Open()

	if r.Selected() {
		t.Errorf("the file shrank and left %q picked out", r.SelectedText())
	}
}

// Extending past the end of a line stops there, so one press back the
// other way moves the selection rather than twenty.
func TestExtendingPastTheEndOfALineStopsThere(t *testing.T) {
	r := aReaderOf(t, 40, 10, "short")

	for range 30 {
		shiftKey(r, input.KeyRight)
	}
	if got, want := r.SelectedText(), "short"; got != want {
		t.Fatalf("it picked out %q, want the whole line %q", got, want)
	}

	shiftKey(r, input.KeyLeft)

	if got, want := r.SelectedText(), "shor"; got != want {
		t.Errorf("one press back left %q, want %q", got, want)
	}
}

// A drag that starts and ends past the end of a line picks nothing out,
// and the bar offers no copy for it.
func TestADragPastTheEndOfALinePicksNothing(t *testing.T) {
	r := aReaderOf(t, 40, 10, "short")

	drag(t, r, 20, 1, 30, 1)

	if r.Selected() {
		t.Errorf("a drag over blank columns left %q picked out", r.SelectedText())
	}
	if has, keys := offersCopy(r); has {
		t.Errorf("the bar offers a copy of nothing: %v", keys)
	}
}

// A press on the name at the top starts nothing. It is not a line of
// the file.
func TestAPressOnTheNameStartsNothing(t *testing.T) {
	r := aReaderOf(t, 40, 10, "hello there")

	mouse(t, r, input.MouseEvent{
		Kind: input.MousePress, Button: input.MouseLeft, Col: 0, Row: 0,
	})
	mouse(t, r, input.MouseEvent{
		Kind: input.MouseMove, Button: input.MouseLeft, Col: 5, Row: 0,
	})

	if r.Selected() {
		t.Errorf("a press on the name left %q picked out", r.SelectedText())
	}
}

// A drag down onto the bar stops at the last line on screen rather than
// picking out one below it.
func TestADragOntoTheBarStopsAtTheLastLineShown(t *testing.T) {
	lines := []string{"one", "two", "three", "four", "five", "six", "seven"}
	r := aReaderOf(t, 40, 6, lines...)

	// Six rows: the name, four lines, the bar. The bar is row 5.
	drag(t, r, 0, 1, 40, 5)

	want := "one\ntwo\nthree\nfour"
	if got := r.SelectedText(); got != want {
		t.Errorf("it picked out %q, want the four lines on screen %q", got, want)
	}
}

// Shift and a click carries on from what is already picked out.
func TestShiftAndAClickExtendsTheSelection(t *testing.T) {
	r := aReaderOf(t, 40, 10, "one two", "middle", "three four")

	drag(t, r, 0, 1, 2, 1)
	if got, want := r.SelectedText(), "one"; got != want {
		t.Fatalf("it picked out %q, want %q", got, want)
	}

	mouse(t, r, input.MouseEvent{
		Kind: input.MousePress, Button: input.MouseLeft, Col: 5, Row: 2, Mods: input.ModShift,
	})
	mouse(t, r, input.MouseEvent{
		Kind: input.MouseRelease, Button: input.MouseLeft, Col: 5, Row: 2, Mods: input.ModShift,
	})

	if got, want := r.SelectedText(), "one two\nmiddle"; got != want {
		t.Errorf("shift and a click gave %q, want %q", got, want)
	}
}

// Moving the pointer after the button is let go changes nothing.
func TestMovingAfterTheReleaseDoesNotExtend(t *testing.T) {
	r := aReaderOf(t, 40, 10, "one two", "middle", "three four")

	drag(t, r, 0, 1, 2, 1)
	mouse(t, r, input.MouseEvent{Kind: input.MouseMove, Col: 6, Row: 3})

	if got, want := r.SelectedText(), "one"; got != want {
		t.Errorf("a move after the release gave %q, want %q", got, want)
	}
}

// A drag whose release never arrives is called off, so the pointer does
// not go on picking text out with no button held.
func TestACancelledGestureStopsTheDrag(t *testing.T) {
	r := aReaderOf(t, 40, 10, "one two", "middle", "three four")

	mouse(t, r, input.MouseEvent{
		Kind: input.MousePress, Button: input.MouseLeft, Col: 0, Row: 1,
	})
	r.CancelGesture()
	mouse(t, r, input.MouseEvent{Kind: input.MouseMove, Col: 6, Row: 3})

	if r.Selected() {
		t.Errorf("the pointer went on picking out %q after the drag was called off", r.SelectedText())
	}
}

// Ctrl+C copies from the reader, which is the chord every other pane
// uses for a copy.
func TestCtrlCCopiesFromTheReader(t *testing.T) {
	r := aReaderOf(t, 40, 10, "hello there")
	var copied string
	r.OnCopy = func(text string) { copied = text }

	drag(t, r, 0, 1, 4, 1)
	ctrlKey(r, input.KeyC)

	if got, want := copied, "hello"; got != want {
		t.Errorf("ctrl+C copied %q, want %q", got, want)
	}
}

// Shift with each key that moves takes the loose end of the selection
// with it, and leaves the other end where it was.
func TestEveryShiftKeyMovesTheLooseEnd(t *testing.T) {
	const picked = "four\nfive si"
	for what, tc := range map[string]struct {
		key  input.Key
		want string
	}{
		"up":   {input.KeyUp, "four"},
		"down": {input.KeyDown, picked},
		"left": {input.KeyLeft, "four\nfive s"},
		"home": {input.KeyHome, "four\nf"},
		"end":  {input.KeyEnd, "four\nfive six"},
	} {
		r := aReaderOf(t, 40, 10, "one", "two", "three", "four", "five six")
		// From the start of "four" to the "i" of "six".
		drag(t, r, 0, 4, 6, 5)
		if got := r.SelectedText(); got != picked {
			t.Fatalf("%s: the drag picked out %q, want %q", what, got, picked)
		}

		shiftKey(r, tc.key)

		if got := r.SelectedText(); got != tc.want {
			t.Errorf("%s: it picked out %q, want %q", what, got, tc.want)
		}
	}
}

// A picture takes no selection. There are no lines in it to pick out.
func TestAPictureTakesNoSelection(t *testing.T) {
	r := NewReader("shot.png", "/tmp/shot.png")
	r.Style = readerStyle()
	r.Layout(ui.Size{Cols: 40, Rows: 10})

	r.SelectAll()
	shiftKey(r, input.KeyDown)
	shiftKey(r, input.KeyEnd)
	drag(t, r, 0, 1, 10, 2)

	if r.Selected() {
		t.Errorf("a picture left %q picked out", r.SelectedText())
	}
}

// A read that failed drops the selection. The lines on screen are not
// the file any more.
func TestAFailedReadDropsTheSelection(t *testing.T) {
	r := aReaderOf(t, 40, 10, "hello there")

	drag(t, r, 0, 1, 4, 1)
	r.Failed(errors.New("it went away"))

	if r.Selected() {
		t.Errorf("a failed read left %q picked out", r.SelectedText())
	}
	if has, keys := offersCopy(r); has {
		t.Errorf("the bar offers a copy of a file that could not be read: %v", keys)
	}
}

// A file that could not be read copies nothing, whatever was picked out
// of the lines it used to show. The pane shows the reason instead of
// those lines, and copying them would hand over what is not on screen.
func TestAFailedReadCopiesNothing(t *testing.T) {
	r := aReaderOf(t, 40, 10, "secret one", "secret two")
	var copied string
	r.OnCopy = func(text string) { copied = text }
	r.Failed(errors.New("permission denied"))

	r.SelectAll()
	ctrlKey(r, input.KeyC)
	shiftKey(r, input.KeyDown)
	drag(t, r, 0, 1, 9, 2)

	if r.Selected() {
		t.Errorf("it picked out %q from a file it could not read", r.SelectedText())
	}
	if copied != "" {
		t.Errorf("it copied %q from a file it could not read", copied)
	}
	if has, keys := offersCopy(r); has {
		t.Errorf("the bar offers a copy of a file that could not be read: %v", keys)
	}
}

// Picking out a file with nothing in it picks nothing out, so the bar
// offers no copy that would copy nothing.
func TestSelectAllOnAFileOfBlankLinesPicksNothing(t *testing.T) {
	r := aReaderOf(t, 40, 10, "", "")

	r.SelectAll()

	if r.Selected() || r.sel.on {
		t.Errorf("it picked out %q from a file of blank lines", r.SelectedText())
	}
	if has, keys := offersCopy(r); has {
		t.Errorf("the bar offers a copy of nothing: %v", keys)
	}
}

// Picking text out of a file that could not be read picks nothing out,
// down to the selection itself rather than only what it hands over.
func TestAFailedReadPicksNothingOut(t *testing.T) {
	r := aReaderOf(t, 40, 10, "secret one", "secret two")
	r.Failed(errors.New("permission denied"))

	r.SelectAll()

	if r.sel.on {
		t.Error("it picked out lines the pane is not showing")
	}
}

// Letting go of another button does not end a drag. The button that
// started it is the one that ends it.
func TestAnotherButtonDoesNotEndTheDrag(t *testing.T) {
	r := aReaderOf(t, 40, 10, "hello there", "second line")

	mouse(t, r, input.MouseEvent{
		Kind: input.MousePress, Button: input.MouseLeft, Col: 0, Row: 1,
	})
	mouse(t, r, input.MouseEvent{
		Kind: input.MouseRelease, Button: input.MouseRight, Col: 4, Row: 1,
	})
	mouse(t, r, input.MouseEvent{
		Kind: input.MouseMove, Button: input.MouseLeft, Col: 9, Row: 1,
	})

	if got, want := r.SelectedText(), "hello ther"; got != want {
		t.Errorf("it picked out %q, want %q: the drag was still going", got, want)
	}
}
