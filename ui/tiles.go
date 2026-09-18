package ui

import (
	"image/color"
	"math"
	"slices"

	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/input"
)

// leastTile is the smallest a tile may be, in cells. Below this there is
// no room for the gap, the rule and a name inside them.
const (
	leastTileCols = 16
	// Six leaves two rows of picture inside the rule. Any less and the
	// tile has a name and nothing to go with it.
	leastTileRows = 6
)

// tileGap is the room left around a tile, in cells, so two pictures side
// by side do not touch.
const tileGap = 1

// TilesStyle colours the grid of tiles.
type TilesStyle struct {
	// FG and BG are the ground the tiles are laid on and the name under
	// each one.
	FG, BG color.RGBA

	// Border is the rule around a tile, and Marked the rule around the
	// one that is chosen.
	Border, Marked color.RGBA

	// MarkedFG and MarkedBG are the name of the one that is chosen.
	MarkedFG, MarkedBG color.RGBA
}

// Tiles lays a name out per pane on a grid, for picking one by eye.
//
// It draws the frames and the names. What goes inside a tile is the
// caller's: Inside says where each picture goes, within the rule rather
// than over it.
type Tiles struct {
	Style TilesStyle

	// Pick runs when one is chosen, with its place in the list.
	Pick func(int) error

	// Close runs when the user gives up.
	Close func()

	names []string
	at    int
	size  Size
	areas []Rect
}

// NewTiles returns a grid of tiles, one per name, with the first marked.
// It keeps a copy of the names, which Rename writes to.
func NewTiles(names []string) *Tiles { return &Tiles{names: slices.Clone(names)} }

// Len is how many tiles there are.
func (t *Tiles) Len() int { return len(t.names) }

// Rename gives a tile the name it goes by now.
func (t *Tiles) Rename(at int, name string) {
	if at >= 0 && at < len(t.names) {
		t.names[at] = name
	}
}

// Name is what a tile is called, and empty for a tile that is not there.
func (t *Tiles) Name(at int) string {
	if at < 0 || at >= len(t.names) {
		return ""
	}
	return t.names[at]
}

// At is which tile is marked.
func (t *Tiles) At() int { return t.at }

// Mark moves the mark to a tile, if there is one there.
func (t *Tiles) Mark(at int) {
	if at >= 0 && at < len(t.names) {
		t.at = at
	}
}

// Areas is where each tile sits, in the room Layout last gave. It is
// what a click lands in, and it takes in the gap around the tile.
func (t *Tiles) Areas() []Rect { return t.areas }

// Inside is the box a tile's picture goes in: its own box, less the gap
// between tiles and the rule around it, so the rule is around the
// picture rather than under it. It is empty when there is no room left.
func (t *Tiles) Inside(i int) Rect {
	if i < 0 || i >= len(t.areas) {
		return Rect{}
	}
	return inset(framed(t.areas[i]), 1)
}

// framed is the box a tile's rule is drawn on.
func framed(area Rect) Rect { return inset(area, tileGap) }

// inset shrinks a box by the same amount on every side, and gives back
// an empty one when there is nothing left.
func inset(r Rect, by int) Rect {
	out := Rect{X: r.X + by, Y: r.Y + by, Cols: r.Cols - 2*by, Rows: r.Rows - 2*by}
	if out.Cols <= 0 || out.Rows <= 0 {
		return Rect{}
	}
	return out
}

// Title names the grid of tiles, for a caller looking for it among the
// dialogs that are open.
func (t *Tiles) Title() string { return TilesTitle }

// TilesTitle is what the grid of tiles is called.
const TilesTitle = "Every pane"

// Layout works out where the tiles go.
func (t *Tiles) Layout(size Size) {
	t.size = size
	t.areas = TileAreas(len(t.names), size)
}

// Draw paints the ground, a frame and a name per tile, and the mark on
// the one that is chosen.
func (t *Tiles) Draw(v grid.View) {
	cols, rows := v.Size()
	if cols <= 0 || rows <= 0 {
		return
	}
	v.Fill(grid.Cell{Rune: ' ', FG: t.Style.FG, BG: t.Style.BG, Width: 1})
	for i, area := range t.areas {
		box := framed(area)
		if box.Empty() {
			continue
		}
		border := t.Style.Border
		if i == t.at {
			border = t.Style.Marked
		}
		drawFrame(v, box, border, t.Style.BG)
		t.name(v, i, box)
	}
}

// name writes a tile's name along the bottom rule of its frame, cropped
// to the room there is.
func (t *Tiles) name(v grid.View, i int, area Rect) {
	if area.Cols < 4 || area.Rows < 2 {
		return
	}
	fg, bg := t.Style.FG, t.Style.BG
	if i == t.at {
		fg, bg = t.Style.MarkedFG, t.Style.MarkedBG
	}
	in := area.In(v)
	cols, rows := in.Size()
	if cols < 4 || rows < 2 {
		return
	}
	// One cell in from each corner, so the name sits inside the rule
	// rather than on top of it.
	room := cols - 2
	text := crop(t.names[i], room)
	in.SetString(1+(room-grid.StringWidth(text))/2, rows-1, text, fg, bg, 0)
}

// crop cuts a name to the columns there are for it, ending in an
// ellipsis when it did not fit.
func crop(s string, room int) string {
	if room <= 0 {
		return ""
	}
	if grid.StringWidth(s) <= room {
		return s
	}
	if room == 1 {
		return "…"
	}
	out := []rune(s)
	for len(out) > 0 && grid.StringWidth(string(out))+1 > room {
		out = out[:len(out)-1]
	}
	return string(out) + "…"
}

// HandleKey walks the tiles with the arrows, picks one with Enter and
// gives up on Escape.
//
// It takes every other key too, which suspends the window's shortcuts
// while the tiles are up: a key meant for the picker would otherwise
// also do something to a pane nobody can see.
func (t *Tiles) HandleKey(ev input.Event) (bool, error) {
	if ev.Kind != input.KeyPress && ev.Kind != input.KeyRepeat {
		return false, nil
	}
	cols, _ := TileShape(len(t.names), t.size)
	switch ev.Key {
	case input.KeyLeft:
		t.step(-1)
	case input.KeyRight:
		t.step(1)
	case input.KeyUp:
		t.step(-cols)
	case input.KeyDown:
		t.step(cols)
	case input.KeyHome:
		t.Mark(0)
	case input.KeyEnd:
		t.Mark(len(t.names) - 1)
	case input.KeyEnter, input.KeySpace:
		return true, t.pick(t.at)
	case input.KeyEscape:
		if t.Close != nil {
			t.Close()
		}
	}
	return true, nil
}

// step moves the mark by n tiles, stopping at either end.
func (t *Tiles) step(n int) { t.Mark(min(max(t.at+n, 0), len(t.names)-1)) }

// pick chooses a tile.
func (t *Tiles) pick(at int) error {
	if t.Pick == nil || at < 0 || at >= len(t.names) {
		return nil
	}
	return t.Pick(at)
}

// HandleMouse picks the tile a press lands on.
//
// Only a press, not the pointer passing over: the mark says where Enter
// would go, and a mouse nudged after the arrows had chosen would move it
// without the user asking.
func (t *Tiles) HandleMouse(ev input.MouseEvent) (bool, error) {
	if ev.Kind != input.MousePress || ev.Button != input.MouseLeft {
		return true, nil
	}
	at := t.tileAt(ev.Col, ev.Row)
	if at < 0 {
		return true, nil
	}
	t.Mark(at)
	return true, t.pick(at)
}

// tileAt is which tile a cell is in, and -1 for the ground between them.
func (t *Tiles) tileAt(col, row int) int {
	for i, area := range t.areas {
		if col >= area.X && col < area.X+area.Cols &&
			row >= area.Y && row < area.Y+area.Rows {
			return i
		}
	}
	return -1
}

// TileShape is how many columns and rows n tiles go in: as near square
// as the count allows, narrowed when the room will not take that many.
func TileShape(n int, in Size) (cols, rows int) {
	if n <= 0 {
		return 0, 0
	}
	cols = int(math.Ceil(math.Sqrt(float64(n))))
	// Only as wide as the room will take at the smallest a tile may be.
	if most := in.Cols / leastTileCols; most > 0 {
		cols = min(cols, most)
	}
	cols = max(cols, 1)
	rows = (n + cols - 1) / cols
	return cols, rows
}

// TileAreas is where each of n tiles sits in the room given, left to
// right and then down. The last row is left ragged rather than
// stretched, so every tile is the same size.
func TileAreas(n int, in Size) []Rect {
	cols, rows := TileShape(n, in)
	if cols == 0 || rows == 0 || in.Cols <= 0 || in.Rows <= 0 {
		return nil
	}
	out := make([]Rect, 0, n)
	for i := 0; i < n; i++ {
		x, y := i%cols, i/cols
		// Measured from the edges each time, so the rounding is shared
		// out and the tiles reach the far edge exactly.
		left := in.Cols * x / cols
		right := in.Cols * (x + 1) / cols
		top := in.Rows * y / rows
		bottom := in.Rows * (y + 1) / rows
		out = append(out, Rect{X: left, Y: top, Cols: right - left, Rows: bottom - top})
	}
	return out
}

// TilesFit reports whether n tiles have room to be worth drawing.
func TilesFit(n int, in Size) bool {
	areas := TileAreas(n, in)
	if len(areas) == 0 {
		return false
	}
	for _, area := range areas {
		if area.Cols < leastTileCols || area.Rows < leastTileRows {
			return false
		}
	}
	return true
}
