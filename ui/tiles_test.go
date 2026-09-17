package ui

import (
	"image/color"
	"strings"
	"testing"

	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/input"
)

// aTiles is a grid of tiles laid out in a window of the given size.
func aTiles(names []string, size Size) *Tiles {
	t := NewTiles(names)
	t.Style = TilesStyle{
		FG: color.RGBA{0xff, 0xff, 0xff, 0xff}, BG: color.RGBA{0, 0, 0, 0xff},
		Border: color.RGBA{0x80, 0x80, 0x80, 0xff}, Marked: color.RGBA{0, 0xff, 0, 0xff},
		MarkedFG: color.RGBA{0, 0, 0, 0xff}, MarkedBG: color.RGBA{0, 0xff, 0, 0xff},
	}
	t.Layout(size)
	return t
}

// tileNames is n names to fill a grid with.
func tileNames(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = string(rune('a' + i))
	}
	return out
}

// The tiles cover the room without overlapping, and none reaches outside
// the window.
func TestTheTilesCoverTheRoomWithoutOverlapping(t *testing.T) {
	size := Size{Cols: 120, Rows: 40}
	for _, n := range []int{1, 2, 3, 4, 5, 6, 9, 12, 16} {
		areas := TileAreas(n, size)

		if len(areas) != n {
			t.Fatalf("%d panes gave %d tiles", n, len(areas))
		}
		seen := make([]int, size.Cols*size.Rows)
		for _, area := range areas {
			if area.Cols <= 0 || area.Rows <= 0 {
				t.Fatalf("%d panes: a tile is %dx%d", n, area.Cols, area.Rows)
			}
			for y := area.Y; y < area.Y+area.Rows; y++ {
				for x := area.X; x < area.X+area.Cols; x++ {
					if x < 0 || y < 0 || x >= size.Cols || y >= size.Rows {
						t.Fatalf("%d panes: a tile reaches %d,%d, outside the window", n, x, y)
					}
					seen[y*size.Cols+x]++
				}
			}
		}
		// Covered as well as not doubled: tiles of one cell each would
		// overlap nothing and use almost none of the window.
		cols, rows := TileShape(n, size)
		full := (n / cols) * cols
		want := 0
		for i := 0; i < n; i++ {
			want += areas[i].Cols * areas[i].Rows
		}
		covered := 0
		for _, times := range seen {
			if times > 1 {
				t.Fatalf("%d panes: a cell is in two tiles", n)
			}
			covered += times
		}
		if covered != want {
			t.Errorf("%d panes cover %d cells, want the %d their boxes add up to",
				n, covered, want)
		}
		// The full rows of the grid leave nothing between them.
		if least := full * (size.Cols / cols) * (size.Rows / rows); covered < least {
			t.Errorf("%d panes cover %d cells, want at least the %d in %d full tiles",
				n, covered, least, full)
		}
	}
}

// A full grid of tiles leaves no cell of the window unused, so the
// pictures are as big as the room allows.
func TestAFullGridOfTilesUsesEveryCell(t *testing.T) {
	size := Size{Cols: 120, Rows: 40}
	// Four tiles go two by two, which fills the window exactly.
	areas := TileAreas(4, size)

	covered := 0
	for _, area := range areas {
		covered += area.Cols * area.Rows
	}

	if want := size.Cols * size.Rows; covered != want {
		t.Errorf("four tiles cover %d cells of %d", covered, want)
	}
}

// The tiles are laid out left to right and then down, so the arrow keys
// move the way the eye reads.
func TestTheTilesAreLaidOutLeftToRightThenDown(t *testing.T) {
	size := Size{Cols: 120, Rows: 40}
	areas := TileAreas(6, size)
	cols, _ := TileShape(6, size)

	if cols < 2 {
		t.Fatalf("six panes in a %v window went into %d columns", size, cols)
	}
	if areas[1].X <= areas[0].X || areas[1].Y != areas[0].Y {
		t.Errorf("the second tile is at %v, want it beside the first at %v", areas[1], areas[0])
	}
	if areas[cols].Y <= areas[0].Y {
		t.Errorf("the tile below the first is at %v, want it under %v", areas[cols], areas[0])
	}
}

// A window with no room for tiles says so rather than drawing a grid
// nobody can read. A narrow one still fits: the tiles go in one column.
func TestAWindowWithNoRoomForTilesSaysSo(t *testing.T) {
	for _, c := range []struct {
		n    int
		size Size
		fits bool
	}{
		{4, Size{Cols: 120, Rows: 40}, true},
		// Narrow but tall: one column of four, which still reads.
		{4, Size{Cols: 20, Rows: 40}, true},
		// Too narrow for a tile at all, and too short for four rows.
		{4, Size{Cols: 10, Rows: 40}, false},
		{4, Size{Cols: 120, Rows: 6}, false},
		{0, Size{Cols: 120, Rows: 40}, false},
	} {
		if got := TilesFit(c.n, c.size); got != c.fits {
			t.Errorf("%d panes in %v fit = %v, want %v", c.n, c.size, got, c.fits)
		}
	}
}

// The arrows walk the tiles, and stop at either end rather than coming
// back round the other side.
func TestTheArrowsWalkTheTilesAndStopAtTheEnds(t *testing.T) {
	size := Size{Cols: 120, Rows: 40}
	tiles := aTiles(tileNames(6), size)
	cols, _ := TileShape(6, size)

	press := func(k input.Key) {
		t.Helper()
		took, err := tiles.HandleKey(input.Event{Kind: input.KeyPress, Key: k})
		if !took || err != nil {
			t.Fatalf("%v: took %v, %v", k, took, err)
		}
	}

	press(input.KeyRight)
	if tiles.At() != 1 {
		t.Errorf("right went to %d, want 1", tiles.At())
	}
	press(input.KeyDown)
	if want := 1 + cols; tiles.At() != want {
		t.Errorf("down went to %d, want %d", tiles.At(), want)
	}
	press(input.KeyUp)
	if tiles.At() != 1 {
		t.Errorf("up went to %d, want 1", tiles.At())
	}
	press(input.KeyLeft)
	press(input.KeyLeft)
	if tiles.At() != 0 {
		t.Errorf("left past the start went to %d, want 0", tiles.At())
	}
	press(input.KeyEnd)
	press(input.KeyRight)
	if tiles.At() != 5 {
		t.Errorf("right past the end went to %d, want 5", tiles.At())
	}
}

// Enter picks the marked tile and Escape gives up.
func TestEnterPicksTheMarkedTileAndEscapeGivesUp(t *testing.T) {
	tiles := aTiles(tileNames(4), Size{Cols: 120, Rows: 40})
	picked, closed := -1, false
	tiles.Pick = func(at int) error { picked = at; return nil }
	tiles.Close = func() { closed = true }

	if _, err := tiles.HandleKey(input.Event{Kind: input.KeyPress, Key: input.KeyRight}); err != nil {
		t.Fatalf("right: %v", err)
	}
	if _, err := tiles.HandleKey(input.Event{Kind: input.KeyPress, Key: input.KeyEnter}); err != nil {
		t.Fatalf("enter: %v", err)
	}
	if picked != 1 {
		t.Errorf("it picked %d, want the marked 1", picked)
	}
	if closed {
		t.Error("picking one also gave up")
	}

	picked = -1
	if _, err := tiles.HandleKey(input.Event{Kind: input.KeyPress, Key: input.KeyEscape}); err != nil {
		t.Fatalf("escape: %v", err)
	}
	if !closed {
		t.Error("escape did not give up")
	}
	if picked != -1 {
		t.Errorf("escape picked %d", picked)
	}
}

// Every key is taken. The tiles cover the window, so a key that fell
// through would be typed into a pane the user cannot see.
func TestTheTilesTakeEveryKey(t *testing.T) {
	tiles := aTiles(tileNames(4), Size{Cols: 120, Rows: 40})

	for _, k := range []input.Key{input.KeyA, input.KeyF5, input.KeyTab, input.KeyBackspace} {
		took, err := tiles.HandleKey(input.Event{Kind: input.KeyPress, Key: k})
		if err != nil {
			t.Fatalf("%v: %v", k, err)
		}
		if !took {
			t.Errorf("%v fell through to whatever is behind the tiles", k)
		}
	}
	// A key going up is not a key press, and belongs to whatever is
	// watching for one.
	if took, _ := tiles.HandleKey(input.Event{Kind: input.KeyRelease, Key: input.KeyA}); took {
		t.Error("a key going up was taken")
	}
}

// A click picks the tile under the pointer.
func TestAClickPicksTheTileUnderThePointer(t *testing.T) {
	tiles := aTiles(tileNames(4), Size{Cols: 120, Rows: 40})
	picked := -1
	tiles.Pick = func(at int) error { picked = at; return nil }
	want := 3
	area := tiles.Areas()[want]

	_, err := tiles.HandleMouse(input.MouseEvent{
		Kind: input.MousePress, Button: input.MouseLeft,
		Col: area.X + area.Cols/2, Row: area.Y + area.Rows/2,
	})

	if err != nil {
		t.Fatalf("click: %v", err)
	}
	if picked != want {
		t.Errorf("it picked %d, want %d", picked, want)
	}
	if tiles.At() != want {
		t.Errorf("the mark is on %d, want %d", tiles.At(), want)
	}
}

// Each tile is drawn with its name under it.
func TestEachTileIsDrawnWithItsNameUnderIt(t *testing.T) {
	size := Size{Cols: 120, Rows: 40}
	tiles := aTiles([]string{"Terminal on margit", "Files on this machine", "short", "x"}, size)
	g := grid.New(size.Cols, size.Rows, color.RGBA{}, color.RGBA{})

	tiles.Draw(g.View())

	text := gridText(g)
	for _, want := range []string{"Terminal on margit", "Files on this machine", "short"} {
		if !strings.Contains(text, want) {
			t.Errorf("the tiles do not name %q:\n%s", want, text)
		}
	}
}

// The marked tile is drawn in the marked colours, so the user can see
// which one Enter would go to.
func TestTheMarkedTileIsDrawnDifferently(t *testing.T) {
	size := Size{Cols: 120, Rows: 40}
	tiles := aTiles(tileNames(4), size)
	tiles.Mark(2)
	g := grid.New(size.Cols, size.Rows, color.RGBA{}, color.RGBA{})

	tiles.Draw(g.View())

	marked := tiles.Areas()[2]
	other := tiles.Areas()[0]
	if got := g.At(marked.X, marked.Y).FG; got != tiles.Style.Marked {
		t.Errorf("the marked tile's corner is %v, want %v", got, tiles.Style.Marked)
	}
	if got := g.At(other.X, other.Y).FG; got != tiles.Style.Border {
		t.Errorf("an unmarked tile's corner is %v, want %v", got, tiles.Style.Border)
	}
}

// A name too long for its tile is cut with an ellipsis rather than
// running into the tile beside it.
func TestANameTooLongForItsTileIsCut(t *testing.T) {
	for _, c := range []struct {
		name string
		room int
		want string
	}{
		{"short", 10, "short"},
		// Two columns a character, so the cut counts columns and not
		// letters.
		{"日本語のターミナル", 6, "日本…"},
		{"日本語", 2, "…"},
		{"a very long name indeed", 8, "a very …"},
		{"abc", 1, "…"},
		{"abc", 0, ""},
	} {
		got := crop(c.name, c.room)
		if got != c.want {
			t.Errorf("%q in %d columns is %q, want %q", c.name, c.room, got, c.want)
		}
		if grid.StringWidth(got) > c.room {
			t.Errorf("%q is %d wide, past the %d there is", got, grid.StringWidth(got), c.room)
		}
	}
}

// The pointer passing over a tile does not move the mark. The mark says
// where Enter would go, and a nudged mouse would move it without the
// user asking.
func TestThePointerPassingOverDoesNotMoveTheMark(t *testing.T) {
	tiles := aTiles(tileNames(4), Size{Cols: 120, Rows: 40})
	picked := -1
	tiles.Pick = func(at int) error { picked = at; return nil }
	tiles.Mark(1)
	over := tiles.Areas()[3]

	took, err := tiles.HandleMouse(input.MouseEvent{
		Kind: input.MouseMove,
		Col:  over.X + over.Cols/2, Row: over.Y + over.Rows/2,
	})

	if err != nil || !took {
		t.Fatalf("a move over the tiles: took %v, %v", took, err)
	}
	if tiles.At() != 1 {
		t.Errorf("the mark moved to %d", tiles.At())
	}
	if picked != -1 {
		t.Errorf("passing over picked %d", picked)
	}
}
