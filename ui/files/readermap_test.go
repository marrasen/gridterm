package files

import (
	"image"
	"image/color"
	"strings"
	"testing"

	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/input"
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
	if !r.mapPress(39, at) {
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

	if r.mapPress(38, 3) {
		t.Error("a press in the file was taken as one on the strip")
	}
	if r.mapPress(39, 0) {
		t.Error("a press on the name at the top was taken as one on the strip")
	}
	if r.mapPress(39, rows-1) {
		t.Error("a press on the key bar was taken as one on the strip")
	}
}

// The strip is drawn in pixels, and a band with more text in it draws
// a longer bar than one with less.
func TestTheStripDrawsTheShapeOfTheFile(t *testing.T) {
	// Empty at the top and full at the bottom, so the two ends differ.
	lines := make([]string, 400)
	for i := range lines {
		if i >= 200 {
			lines[i] = strings.Repeat("x", 39)
		}
	}
	r := readerOn(t, "notes.txt", lines, 40, 12)
	r.Style = readerStyle()

	img := r.MapPicture(8, 100)
	if img == nil {
		t.Fatal("the strip drew no picture")
	}
	if top, bottom := barLength(img, 10), barLength(img, 90); top >= bottom {
		t.Errorf("the empty half drew a bar %d long and the full half %d", top, bottom)
	}
}

// barLength is how many pixels of a row of the strip are drawn in.
func barLength(img *image.RGBA, y int) int {
	n := 0
	for x := range img.Bounds().Dx() {
		if img.RGBAAt(x, y).A != 0 {
			n++
		}
	}
	return n
}

// The strip's own cells carry no characters: what is in the file is in
// the picture, and the cells only say where the pane is.
func TestTheStripHasNoCharactersInIt(t *testing.T) {
	r := readerOn(t, "notes.txt", aLongFile(1000), 40, 12)
	r.Style = readerStyle()

	g := paintedReader(r, 40, 12)

	for y := 1; y <= 12-readerChrome; y++ {
		if c := g.At(39, y).Rune; c != ' ' {
			t.Fatalf("row %d of the strip is %q, want nothing drawn in text", y, c)
		}
	}
	// And the rows the pane is showing are on a ground of their own.
	if got := g.At(39, 1).BG; got != r.Style.OffBG {
		t.Errorf("the top of the strip is on %v, want the box saying where the pane is", got)
	}
}

// The picture is drawn once for a file and a size, not on every frame:
// a file of a million lines is measured once, and the window builds a
// texture only when it changes.
func TestThePictureIsKeptUntilItChanges(t *testing.T) {
	r := readerOn(t, "notes.txt", aLongFile(1000), 40, 12)

	one := r.MapPicture(8, 100)
	if two := r.MapPicture(8, 100); two != one {
		t.Error("the strip was drawn twice for the same file and size")
	}
	if three := r.MapPicture(8, 90); three == one {
		t.Error("the strip was kept for a size it was not drawn at")
	}
	kept := r.MapPicture(8, 100)
	r.Hex(true)
	if again := r.MapPicture(8, 100); again == kept {
		t.Error("the strip was kept after the lines it stands for changed")
	}
}

// The worst line in a band is drawn in its colour, so a single error in
// a thousand quiet lines is still visible.
func TestTheWorstLineInABandShows(t *testing.T) {
	lines := make([]string, 400)
	for i := range lines {
		lines[i] = `{"level":"info","msg":"fine"}`
	}
	lines[300] = `{"level":"error","msg":"not fine"}`
	r := readerOn(t, "app.log", lines, 40, 12)
	r.Style = readerStyle()

	img := r.MapPicture(8, 100)

	var bad []int
	for y := range 100 {
		if img.RGBAAt(7, y) == r.Style.ErrorFG {
			bad = append(bad, y)
		}
	}
	if len(bad) != 1 || bad[0] != 75 {
		t.Errorf("rows %v of a hundred are marked bad for one error at line 300 of 400", bad)
	}
}

// The strip is a scrollbar too: dragged, the file follows the pointer
// until the button comes up, off the strip as well as on it.
func TestDraggingTheStripMovesTheFile(t *testing.T) {
	rows := 12
	r := readerOn(t, "notes.txt", aLongFile(1000), 40, rows)
	body := rows - readerChrome
	press := func(kind input.MouseKind, col, row int) {
		t.Helper()
		if _, err := r.HandleMouse(input.MouseEvent{
			Kind: kind, Button: input.MouseLeft, Col: col, Row: row,
		}); err != nil {
			t.Fatalf("mouse: %v", err)
		}
	}

	press(input.MousePress, 39, 1)
	if r.Top() != 0 {
		t.Fatalf("a press at the top of the strip went to line %d", r.Top())
	}
	press(input.MouseMove, 39, body)
	low := r.Top()
	if low <= 0 {
		t.Fatal("dragging down the strip did not move the file")
	}
	// Off the strip and into the file, still dragging.
	press(input.MouseMove, 10, 1+body/2)
	if got := r.Top(); got >= low {
		t.Errorf("dragging back up, off the strip, left the file at %d", got)
	}
	if r.Selected() {
		t.Error("dragging over the file picked text out")
	}

	press(input.MouseRelease, 10, 1+body/2)
	stopped := r.Top()
	press(input.MouseMove, 39, body)
	if r.Top() != stopped {
		t.Error("the file still follows the pointer once the button is up")
	}
}

// A drag the window gives up on -- a dialog opening while the button
// is down, say -- stops. Moves after it with no button held do not
// scroll the file.
func TestACancelledDragOfTheStripStops(t *testing.T) {
	rows := 12
	r := readerOn(t, "notes.txt", aLongFile(1000), 40, rows)
	body := rows - readerChrome
	if _, err := r.HandleMouse(input.MouseEvent{
		Kind: input.MousePress, Button: input.MouseLeft, Col: 39, Row: 1,
	}); err != nil {
		t.Fatalf("press: %v", err)
	}

	r.CancelGesture()
	if _, err := r.HandleMouse(input.MouseEvent{
		Kind: input.MouseMove, Button: input.MouseNone, Col: 39, Row: body,
	}); err != nil {
		t.Fatalf("move: %v", err)
	}
	if got := r.Top(); got != 0 {
		t.Errorf("a move after the drag was given up scrolled the file to %d", got)
	}
}

// A move with no button held ends a drag whose release never arrived,
// however it was lost.
func TestAMoveWithNothingHeldEndsADragOfTheStrip(t *testing.T) {
	rows := 12
	r := readerOn(t, "notes.txt", aLongFile(1000), 40, rows)
	body := rows - readerChrome
	if _, err := r.HandleMouse(input.MouseEvent{
		Kind: input.MousePress, Button: input.MouseLeft, Col: 39, Row: 1,
	}); err != nil {
		t.Fatalf("press: %v", err)
	}

	for _, row := range []int{body, body - 1} {
		if _, err := r.HandleMouse(input.MouseEvent{
			Kind: input.MouseMove, Button: input.MouseNone, Col: 39, Row: row,
		}); err != nil {
			t.Fatalf("move: %v", err)
		}
	}
	if got := r.Top(); got != 0 {
		t.Errorf("moves with nothing held scrolled the file to %d", got)
	}
}

// The strip is drawn again in a new theme's colours, and against a new
// width of the file: both are baked into the picture.
func TestTheStripIsDrawnAgainForNewColoursOrWidth(t *testing.T) {
	r := readerOn(t, "notes.txt", aLongFile(1000), 40, 12)
	r.Style = readerStyle()
	one := r.MapPicture(8, 100)

	r.Style.NoteFG = color.RGBA{R: 1, G: 2, B: 3, A: 0xff}
	two := r.MapPicture(8, 100)
	if two == one {
		t.Fatal("the strip kept the old theme's colours")
	}

	r.Layout(ui.Size{Cols: 60, Rows: 12})
	if three := r.MapPicture(8, 100); three == two {
		t.Error("the strip kept bars measured against the old width")
	}
}
