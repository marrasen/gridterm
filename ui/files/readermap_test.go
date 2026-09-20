package files

import (
	"strings"
	"testing"

	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/ui"
)

// aLongFile is more lines than a pane can show, of varying lengths so
// the strip has a shape to draw.
func aLongFile(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = strings.Repeat("x", i%40)
	}
	return out
}

// readerOn is a reader holding a file, laid out.
func readerOn(t *testing.T, name string, lines []string, cols, rows int) *Reader {
	t.Helper()
	r := NewReader(name, "/tmp/"+name)
	r.Read = func(then func([]string, bool, error)) { then(lines, false, nil) }
	r.Layout(ui.Size{Cols: cols, Rows: rows})
	r.Open()
	return r
}

// paintedReader paints a reader and answers the grid.
func paintedReader(r *Reader, cols, rows int) *grid.Grid {
	g := grid.New(cols, rows, r.Style.FG, r.Style.BG)
	r.Draw(g.View())
	return g
}

// The strip takes a column of the pane, so the file is drawn in what
// is left rather than under it.
func TestTheStripTakesAColumnFromTheFile(t *testing.T) {
	r := readerOn(t, "notes.txt", aLongFile(200), 40, 12)

	if got := r.mapWidth(); got != 1 {
		t.Fatalf("the strip is %d columns for a plain file, want one", got)
	}
	if got, want := r.bodyCols(), 39; got != want {
		t.Errorf("the file is drawn in %d columns, want %d", got, want)
	}

	r.ShowMap(false)

	if r.mapWidth() != 0 {
		t.Error("the strip is still taking a column with it turned off")
	}
	if got := r.bodyCols(); got != 40 {
		t.Errorf("the file is drawn in %d columns with the strip off", got)
	}
}

// A log gets a second column, for how bad it got. That is what a log
// is read for.
func TestALogGetsASecondColumn(t *testing.T) {
	r := readerOn(t, "app.log", aJSONLog(), 40, 12)

	if got := r.mapWidth(); got != 2 {
		t.Errorf("a log's strip is %d columns, want two", got)
	}
}

// A pane too narrow to spare a column does not get one.
func TestANarrowPaneGetsNoStrip(t *testing.T) {
	r := readerOn(t, "notes.txt", aLongFile(200), mapLeast-1, 12)

	if got := r.mapWidth(); got != 0 {
		t.Errorf("a pane %d columns wide still drew a strip", mapLeast-1)
	}
}

// The strip says where in the file the pane is, as a box rather than a
// line, and the box moves as the file is scrolled.
func TestTheStripSaysWhereInTheFileThePaneIs(t *testing.T) {
	rows := 12
	r := readerOn(t, "notes.txt", aLongFile(1000), 40, rows)
	body := rows - readerChrome

	first, last := r.viewBand(body)
	if first != 0 {
		t.Errorf("the box starts at %d at the top of the file", first)
	}
	if last <= first {
		t.Errorf("the box is %d rows tall", last-first)
	}

	r.End()

	atEnd, _ := r.viewBand(body)
	if atEnd <= first {
		t.Errorf("the box is at %d at the end of the file, was %d at the top", atEnd, first)
	}
}

// Even a screenful of a very long file gets a box that can be seen.
func TestTheBoxIsNeverInvisible(t *testing.T) {
	r := readerOn(t, "notes.txt", aLongFile(500000), 40, 12)

	first, last := r.viewBand(10)

	if last-first < 1 {
		t.Errorf("the box is %d rows for a screenful of half a million lines", last-first)
	}
}

// A press on the strip goes to that part of the file.
func TestAPressOnTheStripGoesThere(t *testing.T) {
	rows := 12
	r := readerOn(t, "notes.txt", aLongFile(1000), 40, rows)
	body := rows - readerChrome

	// Two thirds of the way down the strip.
	at := 1 + body*2/3
	if !r.mapPress(39, at, rows) {
		t.Fatal("the press was not taken as one on the strip")
	}

	want := (at - 1) * 1000 / body
	if got := r.Top(); got < want-r.rows() || got > want {
		t.Errorf("it went to line %d, want about %d", got, want)
	}
}

// A press anywhere else is not the strip's, so text can still be
// picked out of the column beside it.
func TestAPressOffTheStripIsNotTheStrips(t *testing.T) {
	rows := 12
	r := readerOn(t, "notes.txt", aLongFile(1000), 40, rows)

	if r.mapPress(38, 3, rows) {
		t.Error("a press in the file was taken as one on the strip")
	}
	if r.mapPress(39, 0, rows) {
		t.Error("a press on the name at the top was taken as one on the strip")
	}
	if r.mapPress(39, rows-1, rows) {
		t.Error("a press on the key bar was taken as one on the strip")
	}
}

// The strip is drawn in the last column, and a band with more text in
// it is drawn darker than one with less.
func TestTheStripDrawsTheShapeOfTheFile(t *testing.T) {
	// Empty at the top and full at the bottom, so the two ends differ.
	lines := make([]string, 400)
	for i := range lines {
		if i >= 200 {
			lines[i] = strings.Repeat("x", 39)
		}
	}
	r := readerOn(t, "notes.txt", lines, 40, 12)
	r.Style = Style{}

	g := paintedReader(r, 40, 12)

	top := g.At(39, 1).Rune
	bottom := g.At(39, 12-readerChrome).Rune
	if shadeAt(top) >= shadeAt(bottom) {
		t.Errorf("the empty half drew %q and the full half %q", top, bottom)
	}
}

// shadeAt is how full a block character is, for comparing two of them.
func shadeAt(r rune) int {
	for i, c := range mapShades {
		if c == r {
			return i
		}
	}
	return -1
}

// The bands are worked out once for a file and a height, not on every
// frame: a file of a million lines is measured once.
func TestTheBandsAreWorkedOutOnce(t *testing.T) {
	r := readerOn(t, "notes.txt", aLongFile(1000), 40, 12)

	one := r.bands(10)
	two := r.bands(10)

	if &one[0] != &two[0] {
		t.Error("the bands were worked out twice for the same file and height")
	}
	if three := r.bands(9); &three[0] == &one[0] {
		t.Error("the bands were kept for a height they were not worked out at")
	}
}

// The worst line in a band is what the second column shows, so a
// single error in a thousand quiet lines is still visible.
func TestTheWorstLineInABandShows(t *testing.T) {
	lines := make([]string, 400)
	for i := range lines {
		lines[i] = `{"level":"info","msg":"fine"}`
	}
	lines[300] = `{"level":"error","msg":"not fine"}`
	r := readerOn(t, "app.log", lines, 40, 12)

	bands := r.bands(10)

	var bad int
	for _, b := range bands {
		if b.worst == colourBad {
			bad++
		}
	}
	if bad != 1 {
		t.Errorf("%d bands of ten are marked bad for one error in four hundred lines", bad)
	}
	if got := bands[0].worst; got != colourText {
		t.Errorf("a band of info lines came out as colour %d, want %d", got, colourText)
	}
}
