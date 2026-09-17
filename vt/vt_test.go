package vt

import (
	"image/color"
	"strings"
	"testing"

	"github.com/marrasen/gridterm/grid"
)

// harness drives a terminal and reads the screen back as text, so tests
// read like what a user would see.
type harness struct {
	t    *testing.T
	term *Terminal
	g    *grid.Grid
	// replies collects bytes the terminal sent back to the program.
	replies []string
	bells   int
	titles  []string
	clips   []string
	ends    []cmdEnd
}

// cmdEnd is one CommandDone callback, kept so a test can check what the
// terminal was told about a command finishing.
type cmdEnd struct {
	status int
	ok     bool
}

func newHarness(t *testing.T, cols, rows int) *harness {
	t.Helper()
	h := &harness{t: t}
	h.term = New(cols, rows, DefaultPalette(), 100, Callbacks{
		Bell:         func() { h.bells++ },
		Title:        func(s string) { h.titles = append(h.titles, s) },
		Reply:        func(b []byte) { h.replies = append(h.replies, string(b)) },
		ClipboardSet: func(s string) { h.clips = append(h.clips, s) },
		CommandDone:  func(status int, ok bool) { h.ends = append(h.ends, cmdEnd{status: status, ok: ok}) },
	})
	h.g = grid.New(cols, rows, DefaultPalette().FG, DefaultPalette().BG)
	return h
}

func (h *harness) write(s string) *harness {
	h.t.Helper()
	if _, err := h.term.Write([]byte(s)); err != nil {
		h.t.Fatalf("Write: %v", err)
	}
	return h
}

// lines renders and returns the visible rows with trailing blanks
// trimmed, which keeps expectations readable.
func (h *harness) lines() []string {
	h.t.Helper()
	h.term.Render(h.g)
	cols, rows := h.g.Size()
	out := make([]string, rows)
	for y := 0; y < rows; y++ {
		var sb strings.Builder
		for x := 0; x < cols; x++ {
			c := h.g.At(x, y)
			if c.Width == 0 {
				continue // the continuation half of a wide character
			}
			sb.WriteRune(c.Rune)
			for _, m := range c.Comb {
				sb.WriteRune(m)
			}
		}
		out[y] = strings.TrimRight(sb.String(), " ")
	}
	return out
}

func (h *harness) line(y int) string {
	h.t.Helper()
	l := h.lines()
	if y < 0 || y >= len(l) {
		h.t.Fatalf("row %d out of range (%d rows)", y, len(l))
	}
	return l[y]
}

func (h *harness) cursor() (x, y int) { return h.term.Screen().CursorPos() }

func (h *harness) wantLines(want ...string) {
	h.t.Helper()
	got := h.lines()
	for i, w := range want {
		if i >= len(got) {
			h.t.Errorf("row %d missing, want %q", i, w)
			continue
		}
		if got[i] != w {
			h.t.Errorf("row %d = %q, want %q", i, got[i], w)
		}
	}
}

func (h *harness) wantCursor(wx, wy int) {
	h.t.Helper()
	x, y := h.cursor()
	if x != wx || y != wy {
		h.t.Errorf("cursor at %d,%d, want %d,%d", x, y, wx, wy)
	}
}

// ---------------------------------------------------------------- //

func TestPrintAndNewline(t *testing.T) {
	h := newHarness(t, 20, 4)
	h.write("hello\r\nworld")
	h.wantLines("hello", "world")
	h.wantCursor(5, 1)
}

// The deferred wrap is the classic terminal subtlety: filling the last
// column must not move the cursor or scroll until another character
// actually arrives.
func TestDeferredWrapDoesNotMoveUntilTheNextCharacter(t *testing.T) {
	h := newHarness(t, 5, 3)
	h.write("abcde")
	h.wantCursor(4, 0) // still on the last column
	h.wantLines("abcde", "")

	h.write("f")
	h.wantCursor(1, 1)
	h.wantLines("abcde", "f")
}

func TestExactlyOneScreenWidthDoesNotScroll(t *testing.T) {
	h := newHarness(t, 4, 2)
	h.write("aaaa\r\nbbbb")
	h.wantLines("aaaa", "bbbb")
	if got := h.term.Screen().cur.scrollback; len(got) != 0 {
		t.Fatalf("scrollback has %d lines, want 0: the last column scrolled early", len(got))
	}
}

func TestCarriageReturnCancelsAPendingWrap(t *testing.T) {
	h := newHarness(t, 3, 2)
	h.write("abc\rX")
	h.wantLines("Xbc", "")
	h.wantCursor(1, 0)
}

func TestBackspaceAtAPendingWrapStaysOnTheSameCell(t *testing.T) {
	h := newHarness(t, 3, 2)
	// After "abc" the cursor is on column 2 with a wrap pending; one
	// backspace cancels the wrap rather than moving, so the next
	// character overwrites the 'c'.
	h.write("abc\bX")
	h.wantLines("abX", "")
}

func TestWrapDisabledOverwritesTheLastColumn(t *testing.T) {
	h := newHarness(t, 4, 2)
	h.write("\x1b[?7l") // DECAWM off
	h.write("abcdefg")
	h.wantLines("abcg", "")
	h.wantCursor(3, 0)
}

func TestScrollingPushesLinesIntoScrollback(t *testing.T) {
	h := newHarness(t, 8, 2)
	h.write("one\r\ntwo\r\nthree")
	h.wantLines("two", "three")
	if got := len(h.term.Screen().cur.scrollback); got != 1 {
		t.Fatalf("scrollback has %d lines, want 1", got)
	}
}

func TestScrollViewShowsHistory(t *testing.T) {
	h := newHarness(t, 8, 2)
	h.write("one\r\ntwo\r\nthree")

	h.term.Screen().ScrollView(1)
	h.wantLines("one", "two")

	h.term.Screen().ResetView()
	h.wantLines("two", "three")
}

// The cursor belongs to the live screen. Scrolled back it would sit on
// unrelated text.
func TestCursorHiddenWhileScrolledBack(t *testing.T) {
	h := newHarness(t, 8, 2)
	h.write("one\r\ntwo\r\nthree")
	h.term.Screen().ScrollView(1)
	h.term.Render(h.g)
	if h.g.Cursor().Visible {
		t.Fatal("cursor drawn while the view is scrolled into history")
	}
}

func TestTabStops(t *testing.T) {
	h := newHarness(t, 20, 2)
	h.write("a\tb\tc")
	h.wantLines("a       b       c")
}

func TestBackTab(t *testing.T) {
	h := newHarness(t, 20, 2)
	h.write("\x1b[10G") // column 10
	h.write("\x1b[2Z")  // back two tab stops
	h.wantCursor(0, 0)
}

func TestCursorPositioning(t *testing.T) {
	h := newHarness(t, 10, 5)
	h.write("\x1b[3;4H")
	h.wantCursor(3, 2)
	h.write("\x1b[H")
	h.wantCursor(0, 0)
	h.write("\x1b[5d") // VPA
	h.wantCursor(0, 4)
	h.write("\x1b[7G") // CHA
	h.wantCursor(6, 4)
}

func TestCursorMovementClampsAtEdges(t *testing.T) {
	h := newHarness(t, 5, 3)
	h.write("\x1b[100A\x1b[100D")
	h.wantCursor(0, 0)
	h.write("\x1b[100B\x1b[100C")
	h.wantCursor(4, 2)
}

func TestEraseInLine(t *testing.T) {
	cases := []struct {
		name string
		seq  string
		want string
	}{
		{"to end", "\x1b[0K", "abc"},
		{"to start", "\x1b[1K", "    efg"},
		{"whole line", "\x1b[2K", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, 10, 2)
			h.write("abcdefg")
			h.write("\x1b[4G") // column 4 (0-based 3)
			h.write(tc.seq)
			h.wantLines(tc.want)
		})
	}
}

func TestEraseInDisplay(t *testing.T) {
	cases := []struct {
		name string
		seq  string
		want []string
	}{
		{"to end", "\x1b[0J", []string{"aaa", "bb", ""}},
		{"to start", "\x1b[1J", []string{"", "   b", "ccc"}},
		{"all", "\x1b[2J", []string{"", "", ""}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, 6, 3)
			h.write("aaa\r\nbbbb\r\nccc")
			h.write("\x1b[2;3H") // row 2, column 3
			h.write(tc.seq)
			h.wantLines(tc.want...)
		})
	}
}

func TestInsertAndDeleteLines(t *testing.T) {
	h := newHarness(t, 6, 4)
	h.write("a\r\nb\r\nc\r\nd")
	h.write("\x1b[2;1H\x1b[L") // insert a line at row 2
	h.wantLines("a", "", "b", "c")

	h.write("\x1b[2;1H\x1b[M") // delete it again
	h.wantLines("a", "b", "c", "")
}

func TestInsertAndDeleteChars(t *testing.T) {
	h := newHarness(t, 8, 2)
	h.write("abcdef")
	h.write("\x1b[3G\x1b[2@") // insert two blanks at column 3
	h.wantLines("ab  cdef")

	h.write("\x1b[3G\x1b[2P") // delete them
	h.wantLines("abcdef")
}

func TestEraseChars(t *testing.T) {
	h := newHarness(t, 8, 2)
	h.write("abcdef")
	h.write("\x1b[3G\x1b[2X")
	h.wantLines("ab  ef")
}

func TestInsertModeShiftsRatherThanOverwrites(t *testing.T) {
	h := newHarness(t, 8, 2)
	h.write("abcd")
	h.write("\x1b[1G") // back to the start
	h.write("\x1b[4h") // IRM on
	h.write("XY")
	h.wantLines("XYabcd")
}

func TestScrollRegion(t *testing.T) {
	h := newHarness(t, 6, 5)
	h.write("1\r\n2\r\n3\r\n4\r\n5")
	h.write("\x1b[2;4r") // region rows 2..4
	h.write("\x1b[4;1H") // bottom of the region
	h.write("\n")        // scroll inside the region only
	h.wantLines("1", "3", "4", "", "5")
}

// Only a scroll of the whole screen is history. A program that has
// reserved a status line scrolls a region whose top is row 0, and those
// lines are its own interface being redrawn.
func TestScrollRegionDoesNotFeedScrollback(t *testing.T) {
	cases := []struct {
		name   string
		region string
		home   string
	}{
		{"region below the top", "\x1b[2;3r", "\x1b[3;1H"},
		{"region starting at row 0", "\x1b[1;3r", "\x1b[3;1H"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, 6, 4)
			h.write(tc.region + tc.home)
			before := len(h.term.Screen().cur.scrollback)
			h.write("\n\n\n")
			if got := len(h.term.Screen().cur.scrollback); got != before {
				t.Fatalf("scrollback grew by %d; a region scroll is a redraw, not history",
					got-before)
			}
		})
	}
}

func TestReverseIndexScrollsDownAtTheTop(t *testing.T) {
	h := newHarness(t, 6, 3)
	h.write("a\r\nb\r\nc")
	h.write("\x1b[H") // home
	h.write("\x1bM")  // RI
	h.wantLines("", "a", "b")
}

func TestOriginModeConfinesTheCursorToTheRegion(t *testing.T) {
	h := newHarness(t, 6, 6)
	h.write("\x1b[2;4r") // region rows 2..4
	h.write("\x1b[?6h")  // origin mode
	h.write("\x1b[1;1H") // row 1 of the region
	h.wantCursor(0, 1)
	h.write("\x1b[100;1H") // past the bottom of the region
	h.wantCursor(0, 3)
}

func TestSaveAndRestoreCursor(t *testing.T) {
	h := newHarness(t, 10, 5)
	h.write("\x1b[3;5H\x1b7") // DECSC
	h.write("\x1b[1;1H")
	h.write("\x1b8") // DECRC
	h.wantCursor(4, 2)
}

func TestAltBufferRoundTrip(t *testing.T) {
	h := newHarness(t, 8, 3)
	h.write("shell\r\nprompt")
	h.write("\x1b[?1049h") // enter the alternate screen
	h.wantLines("", "", "")
	// 1049 clears the alternate screen but deliberately does not home the
	// cursor, which is why every full-screen program sends CUP itself.
	h.write("\x1b[H")
	h.write("fullscreen")
	h.wantLines("fullscre", "en")

	h.write("\x1b[?1049l") // leave it
	h.wantLines("shell", "prompt")
	h.wantCursor(6, 1)
}

func TestAltBufferKeepsNoScrollback(t *testing.T) {
	h := newHarness(t, 4, 2)
	h.write("\x1b[?1049h")
	h.write("a\r\nb\r\nc\r\nd")
	if got := len(h.term.Screen().cur.scrollback); got != 0 {
		t.Fatalf("alternate buffer kept %d scrollback lines, want 0", got)
	}
}

func TestSGRColours(t *testing.T) {
	pal := DefaultPalette()
	cases := []struct {
		name   string
		seq    string
		wantFG color.RGBA
		wantBG color.RGBA
	}{
		{"basic", "\x1b[31;42m", pal.ANSI[1], pal.ANSI[2]},
		{"bright", "\x1b[91;102m", pal.ANSI[9], pal.ANSI[10]},
		{"256 semicolon", "\x1b[38;5;196m", pal.ANSI[196], pal.BG},
		{"truecolor semicolon", "\x1b[38;2;10;20;30m",
			color.RGBA{10, 20, 30, 0xff}, pal.BG},
		{"default", "\x1b[31m\x1b[39m", pal.FG, pal.BG},
		{"reset", "\x1b[31;42m\x1b[0m", pal.FG, pal.BG},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, 6, 2)
			h.write(tc.seq + "x")
			h.term.Render(h.g)
			c := h.g.At(0, 0)
			if c.FG != tc.wantFG {
				t.Errorf("fg = %v, want %v", c.FG, tc.wantFG)
			}
			if c.BG != tc.wantBG {
				t.Errorf("bg = %v, want %v", c.BG, tc.wantBG)
			}
		})
	}
}

func TestSGRAttributes(t *testing.T) {
	h := newHarness(t, 6, 2)
	h.write("\x1b[1;4;7mx")
	h.term.Render(h.g)
	got := h.g.At(0, 0).Attr
	want := grid.AttrBold | grid.AttrUnderline | grid.AttrReverse
	if got != want {
		t.Fatalf("attrs = %b, want %b", got, want)
	}

	h.write("\x1b[22;24;27my")
	h.term.Render(h.g)
	if got := h.g.At(1, 0).Attr; got != 0 {
		t.Fatalf("attrs after reset = %b, want 0", got)
	}
}

// An out-of-range 256-colour index arrives from the far end of a pipe
// and must not index past the palette.
func TestSGROutOfRangeColourIsClamped(t *testing.T) {
	pal := DefaultPalette()
	h := newHarness(t, 4, 2)

	h.write("\x1b[38;5;999mx")
	h.term.Render(h.g)
	if got := h.g.At(0, 0).FG; got != pal.FG {
		t.Errorf("out-of-range palette index gave %v, want the default fg %v", got, pal.FG)
	}

	h.write("\x1b[38;2;999;999;999my")
	h.term.Render(h.g)
	if got := h.g.At(1, 0).FG; got != (color.RGBA{255, 255, 255, 255}) {
		t.Errorf("clamped truecolor = %v, want white", got)
	}
}

func TestWideCharacterTakesTwoColumns(t *testing.T) {
	h := newHarness(t, 6, 2)
	h.write("a世b")
	h.term.Render(h.g)
	if got := h.g.At(1, 0); got.Rune != '世' || got.Width != 2 {
		t.Errorf("At(1,0) = %+v, want 世 width 2", got)
	}
	if got := h.g.At(2, 0).Width; got != 0 {
		t.Errorf("At(2,0).Width = %d, want 0", got)
	}
	if got := h.g.At(3, 0).Rune; got != 'b' {
		t.Errorf("At(3,0) = %q, want 'b'", got)
	}
}

// A double-width character must not be split across the right edge.
func TestWideCharacterWrapsRatherThanStraddling(t *testing.T) {
	h := newHarness(t, 3, 2)
	h.write("ab世")
	h.wantLines("ab", "世")
}

func TestCombiningMarkJoinsThePreviousCell(t *testing.T) {
	h := newHarness(t, 6, 2)
	h.write("éx")
	h.term.Render(h.g)
	c := h.g.At(0, 0)
	if c.Rune != 'e' || len(c.Comb) != 1 || c.Comb[0] != '́' {
		t.Fatalf("At(0,0) = %+v, want e with one combining mark", c)
	}
	if got := h.g.At(1, 0).Rune; got != 'x' {
		t.Errorf("At(1,0) = %q, want 'x'; the mark took a cell of its own", got)
	}
}

func TestOverwritingHalfAWideCharacterClearsTheOther(t *testing.T) {
	h := newHarness(t, 6, 2)
	h.write("世")
	h.write("\x1b[1Ga") // back to column 1, overwrite the lead half
	h.term.Render(h.g)
	if got := h.g.At(1, 0); got.Width != 1 || got.Rune != ' ' {
		t.Fatalf("orphaned half = %+v, want a blank width-1 cell", got)
	}
}

func TestRepeat(t *testing.T) {
	h := newHarness(t, 10, 2)
	h.write("a\x1b[4b")
	h.wantLines("aaaaa")
}

// REP is bounded by what is left of the line. Without that, eight bytes
// of input buy a screenful of work and a megabyte of them buys minutes.
func TestRepeatIsBoundedByTheRestOfTheLine(t *testing.T) {
	h := newHarness(t, 10, 4)
	h.write("ab\x1b[65535b")
	h.wantLines("abbbbbbbbb", "")
	if x, y := h.cursor(); y != 0 {
		t.Fatalf("cursor at %d,%d; REP scrolled off its own line", x, y)
	}
}

func TestRepeatIgnoresCombiningMarks(t *testing.T) {
	h := newHarness(t, 10, 2)
	// The last printable character is 'e', not the mark that followed.
	h.write("e\u0301\x1b[2b")
	h.term.Render(h.g)
	if got := h.g.At(1, 0).Rune; got != 'e' {
		t.Fatalf("At(1,0) = %q, want 'e'; REP repeated the combining mark", got)
	}
}

func TestBellCallback(t *testing.T) {
	h := newHarness(t, 4, 2)
	h.write("\x07")
	if h.bells != 1 {
		t.Fatalf("bells = %d, want 1", h.bells)
	}
}

func TestTitleCallback(t *testing.T) {
	h := newHarness(t, 4, 2)
	h.write("\x1b]0;hello\x07")
	if len(h.titles) != 1 || h.titles[0] != "hello" {
		t.Fatalf("titles = %v, want [hello]", h.titles)
	}
	if h.term.Title() != "hello" {
		t.Errorf("Title() = %q, want hello", h.term.Title())
	}
}

func TestCursorPositionReport(t *testing.T) {
	h := newHarness(t, 10, 5)
	h.write("\x1b[3;4H\x1b[6n")
	if len(h.replies) != 1 || h.replies[0] != "\x1b[3;4R" {
		t.Fatalf("replies = %q, want [\\x1b[3;4R]", h.replies)
	}
}

func TestDeviceAttributes(t *testing.T) {
	h := newHarness(t, 4, 2)
	h.write("\x1b[c")
	if len(h.replies) != 1 || h.replies[0] != "\x1b[?6c" {
		t.Fatalf("replies = %q, want [\\x1b[?6c]", h.replies)
	}
}

func TestClipboardSet(t *testing.T) {
	h := newHarness(t, 4, 2)
	h.write("\x1b]52;c;aGVsbG8=\x07") // "hello"
	if len(h.clips) != 1 || h.clips[0] != "hello" {
		t.Fatalf("clips = %v, want [hello]", h.clips)
	}
}

// Answering an OSC 52 read would let any program that can write to the
// terminal exfiltrate the clipboard.
func TestClipboardReadIsNotAnswered(t *testing.T) {
	h := newHarness(t, 4, 2)
	h.write("\x1b]52;c;?\x07")
	if len(h.replies) != 0 {
		t.Fatalf("replied %q to a clipboard read", h.replies)
	}
	if len(h.clips) != 0 {
		t.Fatalf("a clipboard read was treated as a write: %v", h.clips)
	}
}

func TestCursorStyle(t *testing.T) {
	cases := []struct {
		seq   string
		want  grid.CursorStyle
		blink bool
	}{
		{"\x1b[1 q", grid.CursorBlock, true},
		{"\x1b[2 q", grid.CursorBlock, false},
		{"\x1b[3 q", grid.CursorUnderline, true},
		{"\x1b[4 q", grid.CursorUnderline, false},
		{"\x1b[5 q", grid.CursorBar, true},
		{"\x1b[6 q", grid.CursorBar, false},
	}
	for _, tc := range cases {
		h := newHarness(t, 4, 2)
		h.write(tc.seq)
		h.term.Render(h.g)
		if got := h.g.Cursor().Style; got != tc.want {
			t.Errorf("%q -> style %d, want %d", tc.seq, got, tc.want)
		}
		if got := h.g.Cursor().Blink; got != tc.blink {
			t.Errorf("%q -> blink %v, want %v", tc.seq, got, tc.blink)
		}
	}
}

// The cursor a terminal starts with is a blinking block. DECSCUSR 0 asks
// for that default back, and so does RIS.
func TestTheDefaultCursorBlinks(t *testing.T) {
	h := newHarness(t, 4, 2)
	h.term.Render(h.g)
	if cur := h.g.Cursor(); !cur.Blink || cur.Style != grid.CursorBlock {
		t.Errorf("a fresh screen has cursor %+v, want a blinking block", cur)
	}

	h.write("\x1b[2 q\x1b[0 q")
	h.term.Render(h.g)
	if cur := h.g.Cursor(); !cur.Blink || cur.Style != grid.CursorBlock {
		t.Errorf("DECSCUSR 0 gave cursor %+v, want a blinking block", cur)
	}

	h.write("\x1b[4 q")
	h.term.Render(h.g)
	if cur := h.g.Cursor(); cur.Blink {
		t.Fatalf("DECSCUSR 4 gave cursor %+v, want a steady underline", cur)
	}
	h.write("\x1bc") // RIS
	h.term.Render(h.g)
	if cur := h.g.Cursor(); !cur.Blink || cur.Style != grid.CursorBlock {
		t.Errorf("after RIS the cursor is %+v, want a blinking block", cur)
	}
}

func TestCursorVisibility(t *testing.T) {
	h := newHarness(t, 4, 2)
	h.write("\x1b[?25l")
	h.term.Render(h.g)
	if h.g.Cursor().Visible {
		t.Error("cursor still visible after DECTCEM off")
	}
	h.write("\x1b[?25h")
	h.term.Render(h.g)
	if !h.g.Cursor().Visible {
		t.Error("cursor not visible after DECTCEM on")
	}
}

func TestResizePreservesContent(t *testing.T) {
	h := newHarness(t, 10, 4)
	h.write("hello\r\nworld")
	h.term.Resize(20, 6)
	h.g.Resize(20, 6)
	h.wantLines("hello", "world")
}

func TestResizeClampsTheCursor(t *testing.T) {
	h := newHarness(t, 20, 6)
	h.write("\x1b[6;20H")
	h.term.Resize(10, 3)
	x, y := h.cursor()
	if x > 9 || y > 2 {
		t.Fatalf("cursor at %d,%d after shrinking to 10x3", x, y)
	}
}

func TestResetReturnsToADefaultScreen(t *testing.T) {
	h := newHarness(t, 6, 3)
	h.write("\x1b[31mred\x1b[?25l")
	h.write("\x1bc") // RIS
	h.term.Render(h.g)
	h.wantLines("", "", "")
	if !h.g.Cursor().Visible {
		t.Error("cursor still hidden after RIS")
	}
	if got := h.g.At(0, 0).FG; got != DefaultPalette().FG {
		t.Errorf("pen colour survived RIS: %v", got)
	}
}

func TestDecaln(t *testing.T) {
	h := newHarness(t, 4, 2)
	h.write("\x1b#8")
	h.wantLines("EEEE", "EEEE")
}

// A DCS payload must be consumed, not printed.
func TestDcsPayloadIsNotPrinted(t *testing.T) {
	h := newHarness(t, 10, 2)
	h.write("\x1bP1$rhello\x1b\\x")
	h.wantLines("x")
}

func TestUnknownSequencesAreIgnored(t *testing.T) {
	h := newHarness(t, 10, 2)
	h.write("\x1b[99;99;99!pX")
	h.wantLines("X")
}

// Rendering an unchanged screen must not dirty anything, or the
// renderer repaints every frame for nothing.
func TestIdleRenderDirtiesNothing(t *testing.T) {
	h := newHarness(t, 10, 3)
	h.write("hello")
	h.term.Render(h.g)
	h.g.ClearDirty()

	h.term.Render(h.g)

	if h.g.AnyDirty() {
		t.Fatal("re-rendering an unchanged screen dirtied the grid")
	}
}

// A line keeps its number as the screen scrolls under it, which is what
// lets something mark a place in the output and find it again.
func TestALineKeepsItsNumberAsTheScreenScrolls(t *testing.T) {
	h := newHarness(t, 20, 4)
	scr := h.term.Screen()

	// Nothing has scrolled, so the top row is the first line there was.
	if got := scr.LineNumber(0); got != 0 {
		t.Errorf("the top row is line %d, want 0", got)
	}
	h.write("one\r\ntwo\r\nthree\r\n")
	// Three lines written on a four row screen: still nothing off the top.
	if got := scr.LineNumber(0); got != 0 {
		t.Errorf("after three lines the top row is line %d, want 0", got)
	}

	// The fourth pushes the first off, and the line the cursor is on is
	// the fourth line this screen ever had.
	h.write("four\r\n")
	if got := scr.LineNumber(0); got != 1 {
		t.Errorf("after four lines the top row is line %d, want 1", got)
	}
	_, row := scr.CursorPos()
	if got := scr.LineNumber(row); got != 4 {
		t.Errorf("the cursor is on line %d, want 4", got)
	}

	// And it goes on counting past what history keeps, so a number
	// written down long ago is still a number the cursor has passed.
	small := New(20, 2, DefaultPalette(), 1, Callbacks{})
	const wrote = 500
	for i := 0; i < wrote; i++ {
		if _, err := small.Write([]byte("x\r\n")); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	if kept := small.Screen().History(); kept >= wrote {
		t.Fatalf("history kept %d of %d lines, so nothing was thrown away here", kept, wrote)
	}
	if got := small.Screen().LineNumber(0); got != wrote-1 {
		t.Errorf("the top row is line %d, want %d", got, wrote-1)
	}
}

// Making the window taller pulls lines back out of history, so the rows
// they land on carry the numbers they had.
func TestLineNumbersFollowAResize(t *testing.T) {
	h := newHarness(t, 20, 2)
	h.write("one\r\ntwo\r\nthree\r\nfour")
	scr := h.term.Screen()
	// Two rows showing "three" and "four", so two lines have gone off
	// the top.
	if got := scr.LineNumber(0); got != 2 {
		t.Fatalf("the top row is line %d, want 2", got)
	}

	// Two rows taller, and both lines come back out of history with it.
	h.term.Resize(20, 4)
	if got := scr.LineNumber(0); got != 0 {
		t.Errorf("after growing, the top row is line %d, want 0", got)
	}
	if got := scr.LineNumber(3); got != 3 {
		t.Errorf("the bottom row is line %d, want 3", got)
	}

	// Back down again, and the rows taken off the top count again.
	h.term.Resize(20, 2)
	if got := scr.LineNumber(0); got != 2 {
		t.Errorf("after shrinking, the top row is line %d, want 2", got)
	}
}

// A full-screen program scrolling its own screen does not move the
// numbers of the lines underneath it: the alternate screen keeps no
// history and is not part of the output.
func TestTheAlternateScreenDoesNotMoveLineNumbers(t *testing.T) {
	h := newHarness(t, 20, 3)
	h.write("one\r\ntwo\r\nthree\r\nfour\r\n")
	scr := h.term.Screen()
	was := scr.LineNumber(0)

	h.write("\x1b[?1049h")
	for i := 0; i < 20; i++ {
		h.write("filling\r\n")
	}
	h.write("\x1b[?1049l")

	if got := scr.LineNumber(0); got != was {
		t.Errorf("the top row is line %d, want %d as before the full-screen program", got, was)
	}
}

// A resize while a full-screen program is up moves the primary screen's
// line numbers, not the alternate screen's.
//
// The two buffers resize separately and by different amounts: the
// alternate screen keeps no history, so it has nothing to revive and
// nothing to push. A window resized in vim and then left there would
// otherwise leave every line number written down before it wrong.
func TestLineNumbersFollowAResizeTakenOnTheAlternateScreen(t *testing.T) {
	h := newHarness(t, 20, 2)
	h.write("one\r\ntwo\r\nthree\r\nfour")
	scr := h.term.Screen()
	if got := scr.LineNumber(0); got != 2 {
		t.Fatalf("the top row is line %d, want 2", got)
	}

	// Into a full-screen program, resize there, and out again.
	h.write("\x1b[?1049h")
	h.term.Resize(20, 4)
	h.write("\x1b[?1049l")

	// Two lines came back out of the primary screen's history, so the
	// top row is the second line this screen ever had.
	if got := scr.LineNumber(0); got != 0 {
		t.Errorf("after the resize the top row is line %d, want 0", got)
	}
	if got := scr.LineNumber(3); got != 3 {
		t.Errorf("the bottom row is line %d, want 3", got)
	}
}

// A screen with no history at all still counts the lines that leave it,
// because a line that nothing kept still happened.
func TestLinesAreCountedWithNoScrollbackToKeepThem(t *testing.T) {
	term := New(20, 2, DefaultPalette(), 0, Callbacks{})
	for i := 0; i < 5; i++ {
		if _, err := term.Write([]byte("x\r\n")); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	scr := term.Screen()
	if kept := scr.History(); kept != 0 {
		t.Fatalf("a screen with no scrollback kept %d lines", kept)
	}
	if got := scr.LineNumber(0); got != 4 {
		t.Errorf("the top row is line %d, want 4", got)
	}
}

// A program scrolling a region of its own is redrawing, so the lines of
// the screen keep their numbers.
func TestAScrollRegionDoesNotMoveLineNumbers(t *testing.T) {
	h := newHarness(t, 20, 4)
	h.write("one\r\ntwo\r\nthree")
	scr := h.term.Screen()
	was := scr.LineNumber(0)

	// A region holding the top row still, and output scrolling inside it.
	h.write("\x1b[2;4r")
	for i := 0; i < 6; i++ {
		h.write("\x1b[4;1Hfilling\n")
	}
	h.write("\x1b[r")

	if got := scr.LineNumber(0); got != was {
		t.Errorf("the top row is line %d, want %d as before the region", got, was)
	}
}

// A reset clears the screen and goes on counting. Everything on it has
// gone, which is lines leaving rather than lines never having been
// there, and a line number written down before has to stay behind the
// cursor.
func TestAResetGoesOnCountingLines(t *testing.T) {
	h := newHarness(t, 20, 3)
	h.write("one\r\ntwo\r\nthree\r\nfour\r\n")
	scr := h.term.Screen()
	was := scr.LineNumber(scr.CursorRow())

	h.write("\x1bc")
	if got := scr.LineNumber(0); got <= was {
		t.Errorf("after a reset the top row is line %d, and the cursor was on %d before it",
			got, was)
	}
}
