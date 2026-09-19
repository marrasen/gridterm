package files

import (
	"image/color"
	"testing"

	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/input"
	"github.com/marrasen/gridterm/ui"
)

// aColouredFile is a reader on a file with a name, so it is drawn the
// way that kind of file is.
func aColouredFile(t *testing.T, name string, cols, rows int, lines ...string) *Reader {
	t.Helper()
	r := NewReader(name, "/tmp/"+name)
	r.Style = readerStyle()
	r.Style.NoteFG = color.RGBA{R: 0x60, G: 0x60, B: 0x60, A: 0xff}
	r.Style.LinkFG = color.RGBA{G: 0x80, B: 0x80, A: 0xff}
	r.Style.MarkedFG = color.RGBA{R: 0xff, G: 0xc0, A: 0xff}
	r.Read = func(then func([]string, bool, error)) { then(lines, false, nil) }
	r.Layout(ui.Size{Cols: cols, Rows: rows})
	r.Open()
	return r
}

// A comment is drawn in the colour a note is, and the code before it is
// not.
func TestACommentIsDrawnAsANote(t *testing.T) {
	r := aColouredFile(t, "main.go", 40, 6, "x := 1 // why")
	g := drawReader(r, 40, 6)

	if got, want := g.At(0, 1).FG, r.Style.FG; got != want {
		t.Errorf("the code is %v, want the ordinary %v", got, want)
	}
	if got, want := g.At(7, 1).FG, r.Style.NoteFG; got != want {
		t.Errorf("the comment is %v, want a note's %v", got, want)
	}
}

// Each family of languages has its own comment mark, and a file is drawn
// by the one its name says.
func TestEachFamilyHasItsOwnCommentMark(t *testing.T) {
	for what, tc := range map[string]struct {
		name string
		line string
		at   int
	}{
		"a hash":  {"run.sh", "ls # listing", 3},
		"a dash":  {"q.sql", "select 1 -- why", 9},
		"a slash": {"a.rs", "let x = 1; // why", 11},
	} {
		r := aColouredFile(t, tc.name, 40, 6, tc.line)
		g := drawReader(r, 40, 6)

		if got, want := g.At(tc.at, 1).FG, r.Style.NoteFG; got != want {
			t.Errorf("%s: the comment is %v, want a note's %v", what, got, want)
		}
	}
}

// A hash in a Go file is not a comment, and nor is a slash in a shell
// script: the mark belongs to the language.
func TestACommentMarkBelongsToItsLanguage(t *testing.T) {
	for what, tc := range map[string]struct {
		name string
		line string
		at   int
	}{
		"a hash in Go":              {"main.go", "x # not a comment", 2},
		"a slash in a shell script": {"run.sh", "x // not a comment", 2},
		"a dash in Go":              {"main.go", "x -- not a comment", 2},
	} {
		r := aColouredFile(t, tc.name, 40, 6, tc.line)
		g := drawReader(r, 40, 6)

		if got, want := g.At(tc.at, 1).FG, r.Style.FG; got != want {
			t.Errorf("%s: it came out %v, want the ordinary %v", what, got, want)
		}
	}
}

// An apostrophe is not a string. A single quote counts only when the
// line closes it, because "don't" is far commoner than a one-character
// string.
func TestAnApostropheIsNotAString(t *testing.T) {
	r := aColouredFile(t, "run.sh", 40, 8, "echo don't", "echo 'yes'")
	g := drawReader(r, 40, 8)

	if got, want := g.At(9, 1).FG, r.Style.FG; got != want {
		t.Errorf("the word after an apostrophe is %v, want the ordinary %v", got, want)
	}
	if got, want := g.At(5, 2).FG, r.Style.LinkFG; got != want {
		t.Errorf("a closed single-quoted string is %v, want a string's %v", got, want)
	}
}

// A string is drawn apart from the code around it, and the code after
// it is ordinary again.
func TestAStringIsDrawnApart(t *testing.T) {
	r := aColouredFile(t, "main.go", 40, 6, `s := "hi" + z`)
	g := drawReader(r, 40, 6)

	if got, want := g.At(5, 1).FG, r.Style.LinkFG; got != want {
		t.Errorf("the string is %v, want a string's %v", got, want)
	}
	if got, want := g.At(8, 1).FG, r.Style.LinkFG; got != want {
		t.Errorf("the closing quote is %v, want it part of the string", got)
	}
	if got, want := g.At(12, 1).FG, r.Style.FG; got != want {
		t.Errorf("the code after the string is %v, want the ordinary %v", got, want)
	}
}

// A string with no closing quote runs to the end of its line and no
// further: the reader is not a compiler, and a wrong guess that stops at
// a line end is a small one.
func TestAnUnclosedStringStopsAtTheEndOfItsLine(t *testing.T) {
	r := aColouredFile(t, "main.go", 40, 6, `t := "no end`)
	g := drawReader(r, 40, 6)

	if got, want := g.At(11, 1).FG, r.Style.LinkFG; got != want {
		t.Errorf("the last column of the line is %v, want the string's %v", got, want)
	}
}

// A number on its own is marked, and digits inside a name are not.
func TestANumberIsMarkedAndDigitsInANameAreNot(t *testing.T) {
	r := aColouredFile(t, "main.go", 40, 6, "x := 42", "sha256 := y")
	g := drawReader(r, 40, 6)

	if got, want := g.At(5, 1).FG, r.Style.MarkedFG; got != want {
		t.Errorf("the number is %v, want a mark's %v", got, want)
	}
	if got, want := g.At(3, 2).FG, r.Style.FG; got != want {
		t.Errorf("the digits in a name are %v, want the ordinary %v", got, want)
	}
}

// A file with no name this knows is drawn plain.
func TestAFileOfNoKnownKindIsDrawnPlain(t *testing.T) {
	r := aColouredFile(t, "notes.txt", 40, 6, "x := 1 // why")
	g := drawReader(r, 40, 6)

	if got, want := g.At(7, 1).FG, r.Style.FG; got != want {
		t.Errorf("a plain file's text is %v, want the ordinary %v", got, want)
	}
}

// A markdown heading is bold, and its hashes are marked.
func TestAMarkdownHeadingIsBold(t *testing.T) {
	r := aColouredFile(t, "notes.md", 40, 6, "## A heading", "ordinary")
	g := drawReader(r, 40, 6)

	if got, want := g.At(0, 1).FG, r.Style.MarkedFG; got != want {
		t.Errorf("the hashes are %v, want a mark's %v", got, want)
	}
	if got := g.At(5, 1).Attr; got&grid.AttrBold == 0 {
		t.Errorf("the first letter of the heading is %v, want it bold", got)
	}
	if got := g.At(0, 2).Attr; got&grid.AttrBold != 0 {
		t.Error("an ordinary line came out bold")
	}
}

// A bullet is marked and a quote reads as one.
func TestABulletAndAQuoteReadAsThemselves(t *testing.T) {
	r := aColouredFile(t, "notes.md", 40, 8, "- one", "> quoted", "  2. two")
	g := drawReader(r, 40, 8)

	if got, want := g.At(0, 1).FG, r.Style.MarkedFG; got != want {
		t.Errorf("the bullet is %v, want a mark's %v", got, want)
	}
	if got, want := g.At(0, 2).FG, r.Style.NoteFG; got != want {
		t.Errorf("the quote is %v, want a note's %v", got, want)
	}
	// A numbered item, indented, is a list too.
	if got, want := g.At(2, 3).FG, r.Style.MarkedFG; got != want {
		t.Errorf("the number is %v, want a mark's %v", got, want)
	}
}

// Emphasis in markdown is drawn as emphasis, and a lone star is not.
func TestMarkdownEmphasis(t *testing.T) {
	r := aColouredFile(t, "notes.md", 40, 8, "a **bold** word", "an *italic* one", "2 * 3")
	g := drawReader(r, 40, 8)

	if got := g.At(2, 1).Attr; got&grid.AttrBold == 0 {
		t.Error("the bold words are not bold")
	}
	if got := g.At(3, 2).Attr; got&grid.AttrItalic == 0 {
		t.Error("the italic words are not italic")
	}
	if got := g.At(2, 3).Attr; got != 0 {
		t.Errorf("a lone star came out %v, want nothing", got)
	}
}

// Code in backticks is drawn apart from the words around it.
func TestMarkdownCodeIsDrawnApart(t *testing.T) {
	r := aColouredFile(t, "notes.md", 40, 6, "run `ls -l` for it")
	g := drawReader(r, 40, 6)

	if got, want := g.At(4, 1).FG, r.Style.LinkFG; got != want {
		t.Errorf("the code is %v, want a string's %v", got, want)
	}
	if got, want := g.At(0, 1).FG, r.Style.FG; got != want {
		t.Errorf("the words are %v, want the ordinary %v", got, want)
	}
}

// A hex dump is bytes rather than the language the file is in, so it is
// drawn plain however the file is named.
func TestAHexDumpIsDrawnPlain(t *testing.T) {
	r := aColouredFile(t, "main.go", 80, 6, "x := 1 // why")

	r.Hex(true)
	g := drawReader(r, 80, 6)

	for x := 0; x < 40; x++ {
		if got := g.At(x, 1); got.FG != r.Style.FG {
			t.Fatalf("the dump is coloured at column %d: %v on %q", x, got.FG, got.Rune)
		}
	}
}

// A coloured line scrolled sideways keeps its colours where they belong.
func TestAColouredLineScrollsSideways(t *testing.T) {
	// Long enough that scrolling by eleven is not clamped back.
	r := aColouredFile(t, "main.go", 10, 6, "abcdefghij // why, and more besides")

	r.Sideways(11)
	g := drawReader(r, 10, 6)

	// Column 0 is now the start of the comment, and it runs on from
	// there rather than leaving a gap where the code used to be.
	for x := 0; x < 5; x++ {
		if got, want := g.At(x, 1).FG, r.Style.NoteFG; got != want {
			t.Errorf("column %d of the scrolled comment is %v, want a note's %v", x, got, want)
		}
	}
	if got, want := readerRow(g, 1), "// why, a\u2026"; got != want {
		t.Errorf("the scrolled line reads %q, want %q", got, want)
	}
}

// A tab counts as one column, the same as it does in a plain line, so a
// tab-indented file scrolled sideways does not lose a character or grow
// a gap.
func TestATabCountsAsOneColumnInAColouredLine(t *testing.T) {
	// Narrower than the line, or there is nothing to scroll.
	r := aColouredFile(t, "main.go", 12, 6, "\tx := 1 // why")

	r.Sideways(1)
	g := drawReader(r, 12, 6)

	if got, want := g.At(0, 1).Rune, 'x'; got != want {
		t.Errorf("the first column holds %q, want %q: the tab is one column", got, want)
	}
	if got, want := g.At(7, 1).FG, r.Style.NoteFG; got != want {
		t.Errorf("the comment starts at %v, want a note's %v", got, want)
	}
	if got, want := g.At(6, 1).FG, r.Style.FG; got != want {
		t.Errorf("the column before the comment is %v, want the ordinary %v", got, want)
	}
}

// A coloured line and a plain one put the same characters in the same
// columns, whatever is in them.
func TestAColouredLineSitsWhereAPlainOneWould(t *testing.T) {
	for what, line := range map[string]string{
		"a tab":            "\tx := 1",
		"a wide character": `s := "日本" + z`,
		"a combining mark": "e\u0301 := 1",
		"a keycap":         "x := 1\ufe0f\u20e3 + 2",
	} {
		for _, left := range []int{0, 1, 2, 3} {
			coloured := aColouredFile(t, "main.go", 20, 6, line)
			plain := aColouredFile(t, "notes.txt", 20, 6, line)
			coloured.Sideways(left)
			plain.Sideways(left)

			want := readerRow(drawReader(plain, 20, 6), 1)
			got := readerRow(drawReader(coloured, 20, 6), 1)

			if got != want {
				t.Errorf("%s at %d columns across: coloured reads %q, plain reads %q",
					what, left, got, want)
			}
		}
	}
}

// A combining mark keeps the cell of the character it belongs to, so a
// colour boundary that falls inside one is moved past it.
func TestAColourBoundaryDoesNotSplitACharacter(t *testing.T) {
	// The keycap is a digit, a variation selector and a combining mark:
	// one cell. The digit alone would be coloured as a number.
	r := aColouredFile(t, "main.go", 20, 6, "x := 1\ufe0f\u20e3")
	g := drawReader(r, 20, 6)

	if got, want := g.At(5, 1).Rune, '1'; got != want {
		t.Fatalf("the cell holds %q, want the keycap's own %q", got, want)
	}
	if got := g.At(6, 1).Rune; got != ' ' && got != 0 {
		t.Errorf("the cell after the keycap holds %q, want nothing", got)
	}
}

// A markdown fence and a horizontal rule are marked out.
func TestAFenceAndARuleAreMarked(t *testing.T) {
	r := aColouredFile(t, "notes.md", 20, 8, "```go", "---", "~~~")
	g := drawReader(r, 20, 8)

	for y, what := range map[int]string{1: "a fence", 2: "a rule", 3: "a tilde fence"} {
		if got, want := g.At(0, y).FG, r.Style.MarkedFG; got != want {
			t.Errorf("%s is %v, want a mark's %v", what, got, want)
		}
	}
}

// A hash with no space after it is not a heading, and an underscore
// inside a word is not emphasis.
func TestMarkdownMarksNeedTheirRoom(t *testing.T) {
	r := aColouredFile(t, "notes.md", 30, 8, "#tag is not a heading", "snake_case_name")
	g := drawReader(r, 30, 8)

	if got, want := g.At(0, 1).FG, r.Style.FG; got != want {
		t.Errorf("a hash with no space is %v, want the ordinary %v", got, want)
	}
	if got := g.At(6, 2).Attr; got != 0 {
		t.Errorf("snake_case came out %v, want nothing", got)
	}
}

// A search match is marked out on a coloured line, in the right columns.
func TestASearchMatchLandsRightOnAColouredLine(t *testing.T) {
	r := aColouredFile(t, "main.go", 40, 8, "x := 1 // the needle here")

	typed(t, r, "/needle")
	press(t, r, input.KeyEnter)
	g := drawReader(r, 40, 8)

	// "x := 1 // the " is fourteen columns, then the match.
	if got, want := g.At(14, 1).BG, r.Style.SelectedBG; got != want {
		t.Errorf("the match sits on %v, want the marked-out ground %v", got, want)
	}
	if got, want := g.At(13, 1).BG, r.Style.SelectedBG; got == want {
		t.Error("the column before the match is marked out too")
	}
	if got, want := g.At(19, 1).BG, r.Style.SelectedBG; got != want {
		t.Errorf("the last column of the match is %v, want it marked out", got)
	}
	if got, want := g.At(20, 1).BG, r.Style.SelectedBG; got == want {
		t.Error("the column after the match is marked out too")
	}
}

// A name in JSON is bold and its value is not, so the shape of a record
// reads at a glance.
func TestAJSONNameIsBold(t *testing.T) {
	r := aColouredFile(t, "conf.json", 40, 6, `  "host": "margit",`)
	g := drawReader(r, 40, 6)

	if got := g.At(2, 1); got.Attr&grid.AttrBold == 0 {
		t.Errorf("the name is drawn %v, want it bold", got.Attr)
	}
	if got := g.At(10, 1); got.Attr&grid.AttrBold != 0 {
		t.Errorf("the value is drawn %v, want it not bold", got.Attr)
	}
	if got, want := g.At(10, 1).FG, r.Style.LinkFG; got != want {
		t.Errorf("the value is %v, want a string's %v", got, want)
	}
}

// A number, true, false and null are marked in JSON, and the same words
// inside a string are not.
func TestJSONNumbersAndKeywordsAreMarked(t *testing.T) {
	for what, tc := range map[string]struct {
		line string
		at   int
	}{
		"a number":   {`{"n": 42}`, 6},
		"a negative": {`{"n": -1.5e-3}`, 6},
		"true":       {`{"n": true}`, 6},
		"false":      {`{"n": false}`, 6},
		"null":       {`{"n": null}`, 6},
	} {
		r := aColouredFile(t, "conf.json", 40, 6, tc.line)
		g := drawReader(r, 40, 6)

		if got, want := g.At(tc.at, 1).FG, r.Style.MarkedFG; got != want {
			t.Errorf("%s: it is %v, want a mark's %v", what, got, want)
		}
	}
}

// A word that only looks like a keyword is not marked: "nullable" is a
// name, and digits inside one are part of it.
func TestAJSONKeywordIsAWholeWord(t *testing.T) {
	r := aColouredFile(t, "conf.json", 40, 6, `{nullable: x1}`)
	g := drawReader(r, 40, 6)

	for _, at := range []int{1, 12} {
		if got, want := g.At(at, 1).FG, r.Style.MarkedFG; got == want {
			t.Errorf("column %d is marked, want it ordinary", at)
		}
	}
}

// A string with a colon inside it is still a value, not a name. What
// makes a name is a colon after the closing quote.
func TestAColonInsideAJSONStringDoesNotMakeAName(t *testing.T) {
	r := aColouredFile(t, "conf.json", 40, 6, `["a: b", "c"]`)
	g := drawReader(r, 40, 6)

	if got := g.At(1, 1); got.Attr&grid.AttrBold != 0 {
		t.Errorf("the string is drawn %v, want it not bold", got.Attr)
	}
}
