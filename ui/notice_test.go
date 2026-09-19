package ui

import (
	"fmt"
	"image/color"
	"strings"
	"testing"

	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/input"
)

// red is a colour a test can tell apart from fg and bg, for the title of
// a notice that reports a failure.
var red = color.RGBA{0xff, 0x00, 0x00, 0xff}

// noticeStyled returns colours a test can tell apart from each other.
func noticeStyled() NoticeStyle {
	return NoticeStyle{
		FG: fg, BG: bg,
		TitleFG: fg, FailureFG: red,
		SelectionFG: bg, SelectionBG: fg,
		ButtonFG: fg, ButtonBG: bg,
		ActiveFG: bg, ActiveBG: fg,
	}
}

// testNotice is a notice with somewhere to record what it did.
type testNotice struct {
	notice *Notice
	closed int
	copied []string
}

func newTestNotice(title, message string) *testNotice {
	tn := &testNotice{}
	n := NewNotice(title, message, func() { tn.closed++ })
	n.Style = noticeStyled()
	n.Copy = func(s string) { tn.copied = append(tn.copied, s) }
	tn.notice = n
	return tn
}

// tapNotice sends one key to a notice. A notice never reports trouble,
// so anything it does report is worth stopping for.
func tapNotice(t *testing.T, n *Notice, ev input.Event) {
	t.Helper()
	if _, err := n.HandleKey(ev); err != nil {
		t.Fatalf("key %v: %v", ev.Key, err)
	}
}

// clickNotice sends one mouse event to a notice.
func clickNotice(t *testing.T, n *Notice, ev input.MouseEvent) {
	t.Helper()
	if _, err := n.HandleMouse(ev); err != nil {
		t.Fatalf("mouse %v at %d,%d: %v", ev.Kind, ev.Col, ev.Row, err)
	}
}

// drawNotice paints a notice onto a see-through grid, the way its own
// layer is made.
func drawNotice(n *Notice, cols, rows int) *grid.Grid {
	g := grid.New(cols, rows, color.RGBA{}, color.RGBA{})
	n.Layout(Size{Cols: cols, Rows: rows})
	n.Draw(g.View())
	return g
}

// noticeLines reads the message back off the screen, one string per row
// of the box, with the padding taken off.
//
// Cell by cell rather than by slicing a row: a column is not a byte, and
// the message can hold a character that takes more than one of either.
func noticeLines(n *Notice, g *grid.Grid) []string {
	box := n.Box()
	out := make([]string, 0, max(box.Rows-noticeChrome, 0))
	for y := 0; y < max(box.Rows-noticeChrome, 0); y++ {
		var b strings.Builder
		for x := box.X + noticePad; x < box.X+box.Cols-noticePad; x++ {
			c := g.At(x, box.Y+noticeTextTop+y)
			if c.Width == 0 {
				continue
			}
			b.WriteRune(c.Rune)
			for _, r := range c.Comb {
				b.WriteRune(r)
			}
		}
		out = append(out, strings.TrimRight(b.String(), " "))
	}
	return out
}

// drawnButtonCol finds the column a button label is drawn at, read off
// the grid. Asking ButtonColsIn would be asking the layout the hit test
// uses whether it agrees with itself.
func drawnButtonCol(t *testing.T, g *grid.Grid, box Rect, label string) int {
	t.Helper()
	row := rowOf(g, box.Y+box.Rows-2)
	at := strings.Index(row, label)
	if at < 0 {
		t.Fatalf("the button %q is not on the button row: %q", label, row)
	}
	return at
}

// selectAll drags from the first character on screen to past the last,
// which is every line of a message the box has room for.
func selectAll(t *testing.T, n *Notice, lines int) {
	t.Helper()
	box := n.Box()
	clickNotice(t, n, pressAt(box.X+noticePad, box.Y+noticeTextTop))
	to := box.Y + noticeTextTop + lines - 1
	clickNotice(t, n, moveTo(box.X+box.Cols, to))
	clickNotice(t, n, releaseAt(box.X+box.Cols, to))
}

// sameLines reports whether two screenfuls are identical, which is how a
// test knows scrolling has stopped.
func sameLines(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// words collapses runs of whitespace, so wrapped text can be compared
// with the message it was wrapped from.
func words(s string) string { return strings.Join(strings.Fields(s), " ") }

// longMessage is longer than any dialog will be, and every word of it is
// different, so a missing line cannot hide behind an identical one.
func longMessage() string {
	var b strings.Builder
	for i := range 40 {
		fmt.Fprintf(&b, "reason%d the directory named in it could not be opened. ", i)
	}
	return strings.TrimSpace(b.String())
}

// A long message is all there, however much of it fits: what does not
// fit is scrolled to rather than cut off.
func TestNoticeShowsTheWholeMessageAcrossScrollPositions(t *testing.T) {
	tn := newTestNotice("It failed", longMessage())
	n := tn.notice

	g := drawNotice(n, 60, 18)
	was := noticeLines(n, g)
	got := append([]string{}, was...)
	steps := 0
	for ; steps < 500; steps++ {
		tapNotice(t, n, press(input.KeyDown, 0))
		now := noticeLines(n, drawNotice(n, 60, 18))
		if sameLines(now, was) {
			break
		}
		got = append(got, now[len(now)-1])
		was = now
	}
	if steps == 0 {
		t.Fatal("the message fitted, so nothing about scrolling was tested")
	}
	if steps >= 500 {
		t.Fatal("scrolling down never stopped")
	}

	if want := words(longMessage()); words(strings.Join(got, " ")) != want {
		t.Errorf("the message read back off the screen is\n%s\nwant\n%s",
			words(strings.Join(got, " ")), want)
	}
}

// Scrolling stops at both ends rather than running off into blank rows.
func TestNoticeScrollingClampsAtBothEnds(t *testing.T) {
	tn := newTestNotice("It failed", longMessage())
	n := tn.notice
	drawNotice(n, 60, 18)

	for range 50 {
		tapNotice(t, n, press(input.KeyPageDown, 0))
	}
	bottom := noticeLines(n, drawNotice(n, 60, 18))
	if strings.TrimSpace(bottom[len(bottom)-1]) == "" {
		t.Errorf("the last row at the bottom is blank:\n%s", strings.Join(bottom, "\n"))
	}
	if !strings.Contains(strings.Join(bottom, " "), "reason39") {
		t.Errorf("the end of the message is not on screen:\n%s", strings.Join(bottom, "\n"))
	}
	tapNotice(t, n, press(input.KeyDown, 0))
	if now := noticeLines(n, drawNotice(n, 60, 18)); !sameLines(now, bottom) {
		t.Error("scrolling past the end of the message moved the text")
	}

	for range 50 {
		tapNotice(t, n, press(input.KeyPageUp, 0))
	}
	top := noticeLines(n, drawNotice(n, 60, 18))
	if !strings.HasPrefix(top[0], "reason0 ") {
		t.Errorf("the top of the message is %q, want it to start the message", top[0])
	}
	tapNotice(t, n, press(input.KeyUp, 0))
	if now := noticeLines(n, drawNotice(n, 60, 18)); !sameLines(now, top) {
		t.Error("scrolling past the start of the message moved the text")
	}
}

// The wheel scrolls it too, which is what a pointer on a dialog does.
func TestNoticeScrollsOnTheWheel(t *testing.T) {
	tn := newTestNotice("It failed", longMessage())
	n := tn.notice
	drawNotice(n, 60, 18)
	was := noticeLines(n, drawNotice(n, 60, 18))

	clickNotice(t, n, input.MouseEvent{
		Kind: input.MousePress, Button: input.MouseWheelDown, Col: 30, Row: 9})

	if now := noticeLines(n, drawNotice(n, 60, 18)); sameLines(now, was) {
		t.Error("a wheel notch did not scroll the message")
	}
}

// Dragging across the message picks out what was dragged over.
func TestNoticeDragSelectsTheTextItIsDraggedOver(t *testing.T) {
	tn := newTestNotice("It failed", "hello there world")
	n := tn.notice
	drawNotice(n, 60, 18)
	box := n.Box()
	row := box.Y + noticeTextTop
	at := func(cluster int) int { return box.X + noticePad + cluster }

	clickNotice(t, n, pressAt(at(6), row))
	clickNotice(t, n, moveTo(at(10), row))
	clickNotice(t, n, releaseAt(at(10), row))

	if got := n.Selection(); got != "there" {
		t.Errorf("selection = %q, want %q", got, "there")
	}
	// And it is marked out on screen.
	g := drawNotice(n, 60, 18)
	if got := g.BGOf(at(6), row); got != fg {
		t.Errorf("the first selected cell is on %v, want the selection colour %v", got, fg)
	}
	if got := g.BGOf(at(5), row); got == fg {
		t.Error("the space before the selection is marked as selected")
	}
}

// A click that never moved is a click, not a selection of one letter.
func TestNoticeAClickSelectsNothing(t *testing.T) {
	tn := newTestNotice("It failed", "hello there world")
	n := tn.notice
	drawNotice(n, 60, 18)
	box := n.Box()
	row := box.Y + noticeTextTop

	clickNotice(t, n, pressAt(box.X+noticePad+6, row))
	clickNotice(t, n, releaseAt(box.X+noticePad+6, row))

	if got := n.Selection(); got != "" {
		t.Errorf("selection = %q, want nothing", got)
	}
}

// The copy chord copies the selection, and the whole message when there
// is no selection.
func TestNoticeCopiesTheSelectionOrTheWholeMessage(t *testing.T) {
	tn := newTestNotice("It failed", "hello there world")
	n := tn.notice
	drawNotice(n, 60, 18)

	n.CopyNow()
	if len(tn.copied) != 1 || tn.copied[0] != "hello there world" {
		t.Fatalf("copied %q, want the whole message", tn.copied)
	}

	box := n.Box()
	row := box.Y + noticeTextTop
	clickNotice(t, n, pressAt(box.X+noticePad+6, row))
	clickNotice(t, n, moveTo(box.X+noticePad+10, row))
	clickNotice(t, n, releaseAt(box.X+noticePad+10, row))
	n.CopyNow()

	if len(tn.copied) != 2 || tn.copied[1] != "there" {
		t.Fatalf("copied %q, want the selection", tn.copied)
	}
}

// A message longer than the box is copied whole, not only the part that
// is on screen.
func TestNoticeCopiesTheWholeOfAScrolledMessage(t *testing.T) {
	tn := newTestNotice("It failed", longMessage())
	n := tn.notice
	drawNotice(n, 60, 18)
	for range 10 {
		tapNotice(t, n, press(input.KeyPageDown, 0))
	}

	n.CopyNow()

	if len(tn.copied) != 1 || tn.copied[0] != longMessage() {
		t.Fatalf("copied %d characters, want the whole %d",
			len(strings.Join(tn.copied, "")), len(longMessage()))
	}
}

// The Copy button does what the copy chord does: the selection when
// there is one, and the whole message when there is not.
func TestNoticeTheCopyButtonCopiesTheSelectionOrEverything(t *testing.T) {
	tn := newTestNotice("It failed", "hello there world")
	n := tn.notice
	g := drawNotice(n, 60, 18)
	box := n.Box()
	copyAt := drawnButtonCol(t, g, box, "Copy")

	clickNotice(t, n, pressAt(copyAt, box.Y+box.Rows-2))

	if len(tn.copied) != 1 || tn.copied[0] != "hello there world" {
		t.Fatalf("with nothing selected it copied %q, want the whole message", tn.copied)
	}

	row := box.Y + noticeTextTop
	clickNotice(t, n, pressAt(box.X+noticePad+6, row))
	clickNotice(t, n, moveTo(box.X+noticePad+10, row))
	clickNotice(t, n, releaseAt(box.X+noticePad+10, row))
	clickNotice(t, n, pressAt(copyAt, box.Y+box.Rows-2))

	if len(tn.copied) != 2 || tn.copied[1] != "there" {
		t.Fatalf("with a selection it copied %q, want the selection", tn.copied)
	}
	if tn.closed != 0 {
		t.Error("copying closed the dialog, so there was no reading the rest of it")
	}
}

// Escape and Enter both put it away, because a notice has nothing to
// decide.
func TestNoticeEscapeAndEnterDismissIt(t *testing.T) {
	for _, key := range []input.Key{input.KeyEscape, input.KeyEnter} {
		tn := newTestNotice("It failed", "hello there world")
		drawNotice(tn.notice, 60, 18)

		tapNotice(t, tn.notice, press(key, 0))

		if tn.closed != 1 {
			t.Errorf("%v closed it %d times, want once", key, tn.closed)
		}
	}
}

// The OK button puts it away when it is clicked.
func TestNoticeTheOKButtonDismissesIt(t *testing.T) {
	tn := newTestNotice("It failed", "hello there world")
	g := drawNotice(tn.notice, 60, 18)
	box := tn.notice.Box()

	clickNotice(t, tn.notice, pressAt(drawnButtonCol(t, g, box, "OK"), box.Y+box.Rows-2))

	if tn.closed != 1 {
		t.Errorf("it closed %d times, want once", tn.closed)
	}
}

// A notice that reports a failure has to read as a failure before it is
// read as words.
func TestNoticeAFailurePaintsItsTitleInTheErrorColour(t *testing.T) {
	tn := newTestNotice("It failed", "hello there world")
	tn.notice.Failure = true
	g := drawNotice(tn.notice, 60, 18)
	box := tn.notice.Box()

	if got := g.FGOf(box.X+noticePad, box.Y+noticeTitleRow); got != red {
		t.Errorf("the title is drawn in %v, want the error colour %v", got, red)
	}

	plain := newTestNotice("It worked", "hello there world")
	g = drawNotice(plain.notice, 60, 18)
	// Its own box: a different title is a box of a different width.
	box = plain.notice.Box()
	if got := g.FGOf(box.X+noticePad, box.Y+noticeTitleRow); got != fg {
		t.Errorf("a notice that is not a failure draws its title in %v, want %v", got, fg)
	}
}

// A notice covers what is behind it, so every key it has no use for
// stops here rather than acting on a pane the user cannot see. The copy
// chord is the one exception, and the notice answers that itself.
func TestNoticeSwallowsEveryKeyButTheCopyChord(t *testing.T) {
	tn := newTestNotice("It failed", "hello there world")
	n := tn.notice
	n.CopyChord = func(ev input.Event) bool {
		return ev.Key == input.KeyC && ev.Mods == input.ModCtrl|input.ModShift
	}
	drawNotice(n, 60, 18)

	for _, ev := range []input.Event{
		press(input.KeyV, input.ModCtrl|input.ModShift), // paste
		press(input.KeyW, input.ModCtrl|input.ModShift), // close the pane
		press(input.KeyK, input.ModCtrl|input.ModShift), // the palette
		press(input.KeyF10, 0),                          // the menu bar
		press(input.KeyA, 0),
	} {
		took, err := n.HandleKey(ev)
		if err != nil {
			t.Fatalf("%v: %v", ChordOf(ev), err)
		}
		if !took {
			t.Errorf("%v travelled past the dialog to whatever is behind it", ChordOf(ev))
		}
	}
	if len(tn.copied) != 0 || tn.closed != 0 {
		t.Fatalf("those keys copied %q and closed it %d times", tn.copied, tn.closed)
	}

	took, err := n.HandleKey(press(input.KeyC, input.ModCtrl|input.ModShift))

	if err != nil {
		t.Fatalf("the copy chord: %v", err)
	}
	if !took {
		t.Error("the copy chord travelled past the dialog that answers it")
	}
	if len(tn.copied) != 1 || tn.copied[0] != "hello there world" {
		t.Errorf("the copy chord copied %q, want the whole message", tn.copied)
	}
	if tn.closed != 0 {
		t.Error("copying closed the dialog, so there was no reading the rest of it")
	}
}

// A notice nobody is touching leaves its layer alone.
func TestAnIdleNoticeDirtiesNothing(t *testing.T) {
	tn := newTestNotice("It failed", longMessage())
	n := tn.notice
	n.Failure = true
	n.Style.BorderFG = fg
	n.Style.ShadowBG = color.RGBA{A: 0x60}

	g := grid.New(60, 18, color.RGBA{}, color.RGBA{})
	n.Layout(Size{Cols: 60, Rows: 18})
	n.Draw(g.View())
	if !g.AnyDirty() {
		t.Fatal("the first draw put nothing on the layer, so there is nothing to leave alone")
	}
	g.ClearDirty()
	for range 5 {
		n.Draw(g.View())
	}

	if g.AnyDirty() {
		t.Fatal("an idle notice dirtied its layer")
	}

	// And it is idleness being checked, not a notice that draws nothing:
	// scrolling it changes the layer again.
	tapNotice(t, n, press(input.KeyDown, 0))
	n.Draw(g.View())
	if !g.AnyDirty() {
		t.Fatal("scrolling the message left the layer untouched")
	}
}

// The line breaks a message already has are the ones it keeps: an error
// made of several is a list, not a paragraph.
func TestNoticeKeepsTheLineBreaksItWasGiven(t *testing.T) {
	// Short enough to have fitted on one line, so a break that was lost
	// shows up.
	tn := newTestNotice("It failed", "first\nsecond")
	n := tn.notice
	g := drawNotice(n, 60, 18)

	lines := noticeLines(n, g)
	if lines[0] != "first" || lines[1] != "second" {
		t.Errorf("the message is drawn as %q, want one line each", lines[:2])
	}
}

// A line that fills the width exactly is one line, not two.
func TestWrapTextFillsTheWidthBeforeBreaking(t *testing.T) {
	lines := wrapText("aaaa bbbb", 9, false)
	if len(lines) != 1 || lines[0].text != "aaaa bbbb" {
		t.Fatalf("wrapText at 9 = %+v, want the one line that fits", lines)
	}
	lines = wrapText("aaaa bbbb", 8, false)
	if len(lines) != 2 || lines[0].text != "aaaa" || lines[0].join != " " {
		t.Fatalf("wrapText at 8 = %+v, want two lines joined by a space", lines)
	}
}

// A word wider than the box is cut rather than pushing the box wider
// than the window, and none of it is lost.
func TestNoticeBreaksAWordTooWideForTheBox(t *testing.T) {
	// Taller than the box as well as wider, so the cut word has to be
	// scrolled through as well as wrapped. Each group is numbered, so
	// rows read back and stuck together cannot look whole by accident,
	// and each starts with a cluster of two runes, so a cut counting
	// bytes as columns would come apart in the middle of one.
	var b strings.Builder
	for i := range 60 {
		fmt.Fprintf(&b, "é%03dfghij", i)
	}
	word := b.String()
	tn := newTestNotice("It failed", word)
	n := tn.notice

	got := strings.Join(noticeLines(n, drawNotice(n, 40, 20)), "")
	was := noticeLines(n, drawNotice(n, 40, 20))
	steps := 0
	for ; steps < 500; steps++ {
		tapNotice(t, n, press(input.KeyDown, 0))
		now := noticeLines(n, drawNotice(n, 40, 20))
		if sameLines(now, was) {
			break
		}
		got += now[len(now)-1]
		was = now
	}
	if steps == 0 {
		t.Fatal("the word fitted, so nothing about cutting it over rows was tested")
	}
	if steps >= 500 {
		t.Fatal("scrolling down never stopped")
	}

	if got != word {
		t.Errorf("the word read back off the screen is\n%q\nwant\n%q", got, word)
	}
}

// A selection comes back as the message was written, not as the box
// wrapped it: a path cut in half by the wrap is copied whole.
func TestNoticeSelectingAPathTheWrapCutGivesThePathBack(t *testing.T) {
	path := "/home/marcus/" + strings.Repeat("a-directory-with-a-long-name/", 6) + "file.txt"
	tn := newTestNotice("It failed", path)
	n := tn.notice
	lines := noticeLines(n, drawNotice(n, 60, 24))
	if strings.TrimSpace(lines[1]) == "" {
		t.Fatalf("the path fitted on one line, so the wrap never cut it:\n%s", lines[0])
	}

	selectAll(t, n, len(lines))

	if got := n.Selection(); got != path {
		t.Errorf("the selection is\n%q\nwant the path as it was written\n%q", got, path)
	}
}

// The line breaks the message has survive a selection, and the ones the
// box put in do not.
func TestNoticeSelectingTwoParagraphsKeepsTheBreakBetweenThem(t *testing.T) {
	message := "The first paragraph is long enough that the box has to wrap it " +
		"over more than one line of its own.\n\nThe second paragraph."
	tn := newTestNotice("It failed", message)
	n := tn.notice
	lines := noticeLines(n, drawNotice(n, 60, 24))

	selectAll(t, n, len(lines))

	if got := n.Selection(); got != message {
		t.Errorf("the selection is\n%q\nwant\n%q", got, message)
	}
}

// A far end can put anything in an error, so what arrives is cleaned
// before it is shown.
func TestCleanTextKeepsLineBreaksAndDropsTheRest(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"one\r\ntwo", "one\ntwo"},
		{"one\rtwo", "onetwo"},
		{"one\ttwo", "one two"},
		{"one\x07\x1b[31mtwo", "one[31mtwo"},
		{"one\u009btwo", "onetwo"},
		{"first\n\nsecond", "first\n\nsecond"},
		{"plain å text", "plain å text"},
	} {
		if got := cleanText(tc.in); got != tc.want {
			t.Errorf("cleanText(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// A drag arrives the way the window delivers one: through the root, to
// whichever widget took the press.
func TestANoticeTakesADragThroughTheRoot(t *testing.T) {
	tn := newTestNotice("It failed", "hello there world")
	n := tn.notice
	r := rootOver(&fake{name: "behind"}, 60, 18)
	if !r.PushModal(n) {
		t.Fatal("the notice would not go on the stack")
	}
	box := n.Box()
	row := box.Y + noticeTextTop
	at := func(cluster int) int { return box.X + noticePad + cluster }

	if _, err := r.HandleMouse(pressAt(at(6), row)); err != nil {
		t.Fatalf("the press: %v", err)
	}
	if held := r.Holding(); held != Widget(n) {
		t.Fatalf("the pointer is held by %T, want the notice", held)
	}
	if _, err := r.HandleMouse(moveTo(at(10), row)); err != nil {
		t.Fatalf("the drag: %v", err)
	}
	if _, err := r.HandleMouse(releaseAt(at(10), row)); err != nil {
		t.Fatalf("the release: %v", err)
	}

	if got := n.Selection(); got != "there" {
		t.Errorf("selection = %q, want %q", got, "there")
	}

	// And a dialog opening over it ends the drag, because the release is
	// never coming.
	if _, err := r.HandleMouse(pressAt(at(0), row)); err != nil {
		t.Fatalf("the second press: %v", err)
	}
	r.PushModal(&fake{name: "over"})
	if n.holding {
		t.Error("the notice is still dragging a selection nothing will release")
	}
}

// A preformatted notice keeps the spacing it was given, so a message
// laid out in columns still reads as columns.
func TestAPreformattedNoticeKeepsItsSpacing(t *testing.T) {
	n := NewNotice("Keys", "Copy      ctrl+shift+C\nPaste     ctrl+shift+V", nil)
	n.Preformatted = true

	lines := noticeLines(n, drawNotice(n, 60, 18))
	for i, want := range []string{"Copy      ctrl+shift+C", "Paste     ctrl+shift+V"} {
		if i >= len(lines) || lines[i] != want {
			t.Fatalf("line %d is %q, want %q: %v", i, lines[min(i, len(lines)-1)], want, lines)
		}
	}
}

// A preformatted line wider than the box is broken where it runs out of
// room rather than being cut off.
func TestAPreformattedLineTooWideIsBrokenWhereItRunsOut(t *testing.T) {
	n := NewNotice("Keys", "a  bbbbbbbbbb  cccccccccc", nil)
	n.Preformatted = true

	// Narrow enough that the line has to break.
	lines := noticeLines(n, drawNotice(n, 20, 18))
	if len(lines) < 2 {
		t.Fatalf("it fitted on %v, so nothing was broken", lines)
	}
	if got := strings.Join(lines, ""); !strings.Contains(got, "a  bbbbbbbbbb") {
		t.Errorf("the spacing was lost: %v", lines)
	}
}

// A notice given an action draws it beside the other buttons, and
// pressing it does the work.
func TestNoticeAnActionButtonDoesTheWork(t *testing.T) {
	done := 0
	n := NewNotice("Where the files are", "They are over there.", func() {})
	n.Action = NoticeAction{Title: "Do it", Do: func() { done++ }}
	n.FocusOK()
	n.Layout(Size{Cols: 60, Rows: 12})

	// Tab back onto the action, which sits before Copy and OK.
	keyTo(t, n, press(input.KeyTab, input.ModShift))
	keyTo(t, n, press(input.KeyTab, input.ModShift))
	keyTo(t, n, press(input.KeyEnter, 0))

	if done != 1 {
		t.Errorf("the action ran %d times, want once", done)
	}
}

// Without one it draws the two it always had, and Enter closes it.
func TestNoticeWithNoActionClosesOnEnter(t *testing.T) {
	closed := 0
	n := NewNotice("Something", "happened.", func() { closed++ })
	n.Layout(Size{Cols: 60, Rows: 12})

	keyTo(t, n, press(input.KeyEnter, 0))

	if closed != 1 {
		t.Errorf("it closed %d times, want once", closed)
	}
}

// The action never swallows Copy: a notice whose action is called Copy
// still has a Copy button of its own.
func TestNoticeAnActionDoesNotReplaceCopy(t *testing.T) {
	n := NewNotice("Something", "happened.", func() {})
	n.Action = NoticeAction{Title: "Do it", Do: func() {}}

	got := n.buttons()

	if len(got) != 3 || got[0] != "Do it" || got[1] != "Copy" || got[2] != "OK" {
		t.Errorf("the buttons are %v, want the action then Copy and OK", got)
	}
}

// A notice's buttons cast the same shadow, and what a click lands on
// follows the row they moved to.
func TestANoticesButtonsCastAShadowAndStillTakeAClick(t *testing.T) {
	n := NewNotice("Could not connect", "The server said no.", nil)
	n.Style = noticeStyled()
	n.Style.ButtonShadowBG = color.RGBA{R: 1, G: 2, B: 3, A: 0xff}
	n.Layout(Size{Cols: 60, Rows: 24})

	g := grid.New(60, 24, color.RGBA{}, color.RGBA{})
	n.Draw(g.View())

	// A row taller than the same notice casting none, so the shadow has
	// somewhere to fall that is not the rule along the bottom.
	plain := NewNotice("Could not connect", "The server said no.", nil)
	plain.Style = noticeStyled()
	plain.Layout(Size{Cols: 60, Rows: 24})
	box := n.box()
	if got, want := box.Rows, plain.box().Rows+1; got != want {
		t.Errorf("a notice casting shadows is %d rows, want %d", got, want)
	}
	row := n.buttonRow(box)
	if row != box.Rows-3 {
		t.Fatalf("the buttons are on row %d of %d, want a row kept under them", row, box.Rows)
	}
	if got := plain.buttonRow(plain.box()); row != got {
		t.Errorf("the buttons moved to row %d, want the %d they were on", row, got)
	}
	at := ButtonColsIn(n.buttons(), box.Cols, noticePad)
	width := ButtonWidth(n.buttons()[0])
	// The bottom half of the cell, drawn on the notice's own ground: the
	// shadow beside a button is the same thickness as the one under it.
	beside := g.At(box.X+at[0]+width, box.Y+row)
	if beside.Rune != shadowBeside || beside.FG != n.Style.ButtonShadowBG {
		t.Errorf("beside the button is %q in %v, want the bottom half of the cell in the shadow %v",
			beside.Rune, beside.FG, n.Style.ButtonShadowBG)
	}
	// A click on the row the buttons moved to still presses one.
	pressed := 0
	n.Action = NoticeAction{Title: "Retry", Do: func() { pressed++ }}
	n.Layout(Size{Cols: 60, Rows: 24})
	box = n.box()
	row = n.buttonRow(box)
	at = ButtonColsIn(n.buttons(), box.Cols, noticePad)
	if _, err := n.HandleMouse(pressAt(box.X+at[0], box.Y+row)); err != nil {
		t.Fatalf("click: %v", err)
	}
	if pressed != 1 {
		t.Errorf("a click on the row the buttons moved to pressed %d buttons, want the one", pressed)
	}
}
