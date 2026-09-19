// Package grid holds the character grid: the model a terminal-style UI
// draws into. It knows nothing about GPUs, fonts or escape sequences, so
// it is testable without a display.
package grid

import (
	"image/color"
	"slices"
	"strings"

	"github.com/rivo/uniseg"
)

// Attr is a bitfield of per-cell text attributes.
type Attr uint8

const (
	AttrBold Attr = 1 << iota
	AttrUnderline
	AttrReverse
	AttrItalic
	AttrStrike
	AttrDim
	AttrBlink
	AttrHidden
)

// Cell is one character position.
//
// Width is how many columns the cell occupies: 1 for ordinary text, 2
// for a double-width character such as most CJK and emoji, and 0 for the
// column a double-width character spills into. A width-0 cell carries
// the same colours as its lead cell so that background runs still merge,
// but it draws no glyph of its own.
type Cell struct {
	Rune  rune
	Comb  []rune // combining marks drawn over Rune, if any
	FG    color.RGBA
	BG    color.RGBA
	Attr  Attr
	Width uint8

	// Art is drawn in code rather than looked up in a font, for the
	// things no character stands for. It is drawn over the background
	// and instead of the glyph.
	Art Art
}

// Art is something drawn in code inside one cell: a small graph, a mark,
// anything the fonts have no character for.
//
// It lives in the cell rather than in a table beside it, so the grid's
// damage tracking covers it without being told: art that has changed is
// a cell that has changed, and art that has not is a row left alone.
// That is also why it is a value and not a pointer -- two cells holding
// the same drawing have to compare equal.
type Art struct {
	// Kind says what to draw. The zero kind is nothing at all, so a
	// cell with no art is the zero cell.
	Kind ArtKind

	// Data is the drawing's own, packed into one word. What the bits
	// mean is the kind's business.
	Data uint64
}

// ArtKind names something drawn in code.
type ArtKind uint8

const (
	// ArtNone is no art, which is what an ordinary cell has.
	ArtNone ArtKind = iota

	// ArtGraph is a column chart across the cell: ArtGraphBars samples,
	// each ArtGraphBits wide, oldest on the left.
	ArtGraph

	// ArtIcon is a small picture standing for a kind of thing. Data says
	// which one.
	ArtIcon
)

// IconKind names a small picture drawn in code.
//
// Drawn rather than looked up in a font: no character stands for "a
// terminal" or "a filesystem", and the ones that come close are arrows
// and boxes that read as something else.
type IconKind uint8

const (
	// IconTerminal is a screen with a prompt in it.
	IconTerminal IconKind = iota

	// IconCommand is something that was run and finished.
	IconCommand

	// IconFiles is a listing: a root with things under it.
	IconFiles

	// IconTunnel is traffic going both ways.
	IconTunnel

	// IconReader is a page being read: lines of text on a sheet.
	IconReader

	// IconFollow is a reader keeping up with a file: chevrons running
	// down to the end of it.
	IconFollow

	// IconRemote is a link to another machine, either way: a mast with
	// signal either side of it.
	IconRemote

	// IconCopy is two sheets, one behind the other.
	IconCopy

	// IconMove is a sheet with an arrow taking it away.
	IconMove

	// IconDelete is a bin.
	IconDelete

	// NumIcons is how many there are, for a caller checking one.
	NumIcons
)

// Icon is a piece of art standing for a kind of thing.
func Icon(k IconKind) Art { return Art{Kind: ArtIcon, Data: uint64(k)} }

// Icon returns which picture a piece of art is, and whether it is one.
func (a Art) Icon() (IconKind, bool) {
	if a.Kind != ArtIcon || a.Data >= uint64(NumIcons) {
		return 0, false
	}
	return IconKind(a.Data), true
}

// How a graph is packed: the samples it holds and the bits each one
// takes. Four bits is sixteen heights, which is more than a cell that
// small can show apart. The count of real bars goes above them, so a run
// shorter than the cell holds is not drawn as a run of quiet seconds.
const (
	ArtGraphBars  = 12
	ArtGraphBits  = 4
	ArtGraphMax   = 1<<ArtGraphBits - 1
	artGraphCount = ArtGraphBars * ArtGraphBits
)

// Graph packs bar heights into a piece of art, oldest first.
//
// The newest is the last bar, so a run shorter than the cell holds fills
// from the right and grows leftwards as it lengthens: the most recent
// second is always in the same place. Heights are clamped to
// ArtGraphMax, and anything older than the last ArtGraphBars is dropped.
func Graph(heights []int) Art {
	if len(heights) == 0 {
		// No run is no art. A graph of nothing draws a rule across the
		// cell, which says there was a run and it was quiet.
		return Art{}
	}
	if len(heights) > ArtGraphBars {
		heights = heights[len(heights)-ArtGraphBars:]
	}
	at := ArtGraphBars - len(heights)
	data := uint64(len(heights)) << artGraphCount
	for i, h := range heights {
		data |= uint64(min(max(h, 0), ArtGraphMax)) << ((at + i) * ArtGraphBits)
	}
	return Art{Kind: ArtGraph, Data: data}
}

// Bars is how many of a graph's bars were really measured. The rest are
// seconds that had not happened yet, and are not drawn.
func (a Art) Bars() int {
	if a.Kind != ArtGraph {
		return 0
	}
	return min(int(a.Data>>artGraphCount), ArtGraphBars)
}

// Bar returns the height of one bar of a graph.
func (a Art) Bar(i int) int {
	if a.Kind != ArtGraph || i < 0 || i >= ArtGraphBars {
		return 0
	}
	return int(a.Data>>(i*ArtGraphBits)) & ArtGraphMax
}

// Equal reports whether two cells would draw identically. Cell contains
// a slice, so it cannot be compared with ==.
func (c Cell) Equal(o Cell) bool {
	return c.Rune == o.Rune &&
		c.FG == o.FG &&
		c.BG == o.BG &&
		c.Attr == o.Attr &&
		c.Width == o.Width &&
		c.Art == o.Art &&
		slices.Equal(c.Comb, o.Comb)
}

// CursorStyle selects how the cursor is drawn.
type CursorStyle uint8

const (
	CursorBlock CursorStyle = iota
	CursorUnderline
	CursorBar
)

// Cursor is the text cursor's position and appearance. A cursor outside
// the grid bounds is simply not drawn, which saves callers from clamping
// during a resize.
type Cursor struct {
	X, Y    int
	Visible bool
	Style   CursorStyle

	// Blink asks for the cursor to be shown and hidden in turn. The grid
	// keeps no clock, so whoever draws the cursor decides the phase.
	Blink bool
}

// Grid is a rectangular buffer of cells with per-row damage tracking.
// Damage lets the renderer redraw only the rows that changed, which is
// what keeps a mostly-idle screen cheap.
type Grid struct {
	cols, rows int
	cells      []Cell
	dirty      []bool
	allDirty   bool
	cursor     Cursor
	sel        Selection

	// cursorClaimed records that something placed the cursor since the
	// claim was last reset. Clearing an unclaimed cursor after the fact
	// costs nothing, where clearing it first and having it written back
	// dirties a row on every idle frame.
	cursorClaimed bool

	// colPad and rowPad are the space around columns and rows. Either
	// table may stop short of the grid, or be nil when nothing is
	// padded.
	colPad, rowPad []Pad

	// DefaultFG and DefaultBG fill cells cleared by Clear and Resize.
	DefaultFG color.RGBA
	DefaultBG color.RGBA

	// SelectionBG is painted behind selected cells. The foreground is
	// left alone, so selected text keeps whatever colour the program
	// gave it.
	SelectionBG color.RGBA
}

// New returns a grid of the given size, filled with spaces.
func New(cols, rows int, fg, bg color.RGBA) *Grid {
	g := &Grid{
		DefaultFG: fg,
		DefaultBG: bg,
		// A mid grey reads as a selection against both a dark and a
		// light theme; callers with a theme of their own should override it.
		SelectionBG: color.RGBA{0x3a, 0x44, 0x55, 0xff},
	}
	g.Resize(cols, rows)
	return g
}

// Size returns the grid dimensions in cells.
func (g *Grid) Size() (cols, rows int) { return g.cols, g.rows }

// Blank returns an empty cell in the default colours.
func (g *Grid) Blank() Cell {
	return Cell{Rune: ' ', FG: g.DefaultFG, BG: g.DefaultBG, Width: 1}
}

// Resize changes the grid dimensions, preserving the top-left overlap.
// Growing fills the new area with blanks in the default colours.
func (g *Grid) Resize(cols, rows int) {
	cols = max(cols, 0)
	rows = max(rows, 0)
	if cols == g.cols && rows == g.rows {
		return
	}
	next := make([]Cell, cols*rows)
	blank := g.Blank()
	for i := range next {
		next[i] = blank
	}
	// Copy the overlapping region so a resize does not blank the screen.
	copyCols := min(cols, g.cols)
	copyRows := min(rows, g.rows)
	for y := range copyRows {
		copy(next[y*cols:y*cols+copyCols], g.cells[y*g.cols:y*g.cols+copyCols])
	}
	g.cells = next
	g.cols, g.rows = cols, rows
	g.dirty = make([]bool, rows)
	g.allDirty = true

	// Padding for columns and rows that are gone. The whole grid is
	// already marked dirty, so there is nothing else to say.
	g.colPad = trimPads(g.colPad, cols)
	g.rowPad = trimPads(g.rowPad, rows)

	// Narrowing can cut a double-width character in half. Repair the
	// whole row rather than just its edges: blanking one broken pair can
	// expose another, and a lead cell with no continuation makes
	// clearWideAt blank an innocent neighbour later on.
	for y := range copyRows {
		RepairWidths(g.cells[y*cols:(y+1)*cols], blank)
	}
}

// RepairWidths blanks any half of a double-width character whose partner
// is missing, leaving the row's width invariant intact.
//
// Anything that moves cells around within a row — erasing, inserting,
// deleting — can cut a double-width character in half. A lone lead cell
// draws its glyph over the cell that was just cleared; a lone
// continuation makes the next write blank an innocent neighbour.
func RepairWidths(row []Cell, blank Cell) {
	for x := range row {
		switch row[x].Width {
		case 2:
			if x+1 >= len(row) || row[x+1].Width != 0 {
				row[x] = blank
			}
		case 0:
			if x == 0 || row[x-1].Width != 2 {
				row[x] = blank
			}
		}
	}
}

// Clear fills the whole grid with blanks in the default colours.
func (g *Grid) Clear() {
	blank := g.Blank()
	for i := range g.cells {
		g.cells[i] = blank
	}
	g.allDirty = true
}

// At returns the cell at x,y. Out-of-range coordinates return a blank
// cell rather than panicking, so drawing code can be sloppy at edges.
func (g *Grid) At(x, y int) Cell {
	if !g.inBounds(x, y) {
		return g.Blank()
	}
	return g.cells[y*g.cols+x]
}

// Set writes a cell and marks its row dirty. Writing a cell identical to
// the one already there does not dirty the row: an idle repaint of
// unchanged content costs nothing.
//
// Set is the low-level primitive and does not maintain the width
// invariant between a lead cell and its continuation. Callers writing
// double-width characters should use SetWide.
func (g *Grid) Set(x, y int, c Cell) {
	if !g.inBounds(x, y) {
		return
	}
	i := y*g.cols + x
	if g.cells[i].Equal(c) {
		return
	}
	g.cells[i] = c
	g.dirty[y] = true
}

// SetWide writes a cell that may occupy two columns, keeping the lead
// and continuation cells consistent. It reports the number of columns
// consumed, which is 0 when the cell does not fit.
//
// Overwriting either half of an existing double-width character blanks
// the other half first; leaving it behind would draw a stray glyph.
func (g *Grid) SetWide(x, y int, c Cell) int {
	if !g.inBounds(x, y) {
		return 0
	}
	w := min(max(int(c.Width), 1), 2)
	if x+w > g.cols {
		return 0
	}
	c.Width = uint8(w)
	cont := Cell{FG: c.FG, BG: c.BG, Attr: c.Attr, Width: 0}

	// Check for equality before touching anything. clearWideAt writes a
	// blank over the continuation cell, which would dirty the row even
	// when the character being written is the one already there — and an
	// idle screen full of CJK would then repaint every frame.
	if g.cells[y*g.cols+x].Equal(c) &&
		(w == 1 || g.cells[y*g.cols+x+1].Equal(cont)) {
		return w
	}

	g.clearWideAt(x, y)
	if w == 2 {
		g.clearWideAt(x+1, y)
	}
	g.Set(x, y, c)
	if w == 2 {
		g.Set(x+1, y, cont)
	}
	return w
}

// clearWideAt blanks the other half of any double-width character
// covering column x, so no continuation is left without its lead or the
// reverse.
//
// Both halves are checked before blanking anything. A cell claiming to
// be half of a pair is not proof that the pair exists, and trusting it
// blanks an innocent neighbour.
func (g *Grid) clearWideAt(x, y int) {
	if !g.inBounds(x, y) {
		return
	}
	switch g.cells[y*g.cols+x].Width {
	case 2:
		if other := g.At(x+1, y); other.Width == 0 {
			g.Set(x+1, y, blankLike(other))
		}
	case 0:
		if other := g.At(x-1, y); other.Width == 2 {
			g.Set(x-1, y, blankLike(other))
		}
	}
}

// blankLike returns an empty cell in c's own colours, so blanking the
// orphaned half of a pair does not repaint it in the grid's defaults.
// Repairing a whole row is RepairWidths' job, and it blanks with the
// cell the caller passes instead.
func blankLike(c Cell) Cell {
	return Cell{Rune: ' ', FG: c.FG, BG: c.BG, Width: 1}
}

// SetString writes s starting at x,y in one style, stopping at the row
// end. It returns the column just past the last cluster written.
//
// Text is split into grapheme clusters, so a base character and its
// combining marks share one cell, and double-width clusters take two.
func (g *Grid) SetString(x, y int, s string, fg, bg color.RGBA, attr Attr) int {
	return g.View().SetString(x, y, s, fg, bg, attr)
}

// ClusterCell builds a cell from one grapheme cluster and its display
// width. Width is clamped to 1 or 2: a terminal grid has no room for
// anything else, and a zero-width cluster still has to land somewhere.
//
// Only true zero-width marks join the base rune. The remaining runes of
// an emoji ZWJ sequence or a regional-indicator flag are full-size
// glyphs; stacking them in one cell draws them on top of each other, so
// they are dropped and only the first is shown.
func ClusterCell(cluster string, width int) Cell {
	c := Cell{Rune: ' ', Width: 1}
	if width >= 2 {
		c.Width = 2
	}
	first := true
	for _, r := range cluster {
		if first {
			c.Rune = printable(r)
			first = false
			continue
		}
		if RuneWidth(r) == 0 {
			c.Comb = append(c.Comb, r)
		}
	}
	return c
}

// printable maps control characters to a space. They have no glyph, and
// letting one reach the atlas would cache a miss under a rune the caller
// never meant to display.
func printable(r rune) rune {
	if r < 0x20 || r == 0x7f {
		return ' '
	}
	return r
}

// ScrollUp moves every row up by n, discarding the top n rows and
// filling the bottom with blanks. This is the hot path in a terminal
// under load, so it is a slice copy rather than a per-cell loop.
func (g *Grid) ScrollUp(n int) {
	if n <= 0 || g.rows == 0 {
		return
	}
	if n >= g.rows {
		g.Clear()
		return
	}
	copy(g.cells, g.cells[n*g.cols:])
	blank := g.Blank()
	for i := (g.rows - n) * g.cols; i < len(g.cells); i++ {
		g.cells[i] = blank
	}
	g.allDirty = true
}

// Cursor returns the current cursor.
func (g *Grid) Cursor() Cursor { return g.cursor }

// SetCursor moves the cursor, dirtying both the row it left and the row
// it arrived at so the old cell is repainted without it.
func (g *Grid) SetCursor(c Cursor) {
	g.cursorClaimed = true
	if c == g.cursor {
		return
	}
	old := g.cursor
	g.cursor = c
	if old.Visible {
		g.dirtyRow(old.Y)
	}
	if c.Visible {
		g.dirtyRow(c.Y)
	}
}

// BGRuns calls fn for each maximal horizontal run of cells in row y that
// share a background colour, as [x0,x1). AttrReverse swaps the cell's
// foreground and background, so it is resolved here rather than by the
// caller.
//
// A terminal row is usually one background colour end to end, so this
// normally collapses a whole row into a single rectangle.
func (g *Grid) BGRuns(y int, fn func(x0, x1 int, c color.RGBA)) {
	if y < 0 || y >= g.rows || g.cols == 0 {
		return
	}
	start := 0
	cur := g.bgOf(0, y)
	for x := 1; x < g.cols; x++ {
		c := g.bgOf(x, y)
		if c != cur {
			fn(start, x, cur)
			start, cur = x, c
		}
	}
	fn(start, g.cols, cur)
}

// bgOf returns the effective background of a cell, honouring AttrReverse
// and the selection.
//
// Resolving the selection here rather than in the renderer means the
// background run merging and the damage tracking handle it without
// knowing it exists.
func (g *Grid) bgOf(x, y int) color.RGBA {
	if g.sel.Contains(x, y) && g.SelectionBG.A != 0 {
		return g.SelectionBG
	}
	c := g.cells[y*g.cols+x]
	if c.Attr&AttrReverse != 0 {
		return c.FG
	}
	return c.BG
}

// BGOf returns the effective background of a cell, honouring
// AttrReverse and the selection. It is the counterpart of FGOf.
func (g *Grid) BGOf(x, y int) color.RGBA {
	if !g.inBounds(x, y) {
		return g.DefaultBG
	}
	return g.bgOf(x, y)
}

// FGOf returns the effective foreground of a cell, honouring AttrReverse.
func (g *Grid) FGOf(x, y int) color.RGBA {
	c := g.At(x, y)
	if c.Attr&AttrReverse != 0 {
		return c.BG
	}
	return c.FG
}

// RowDirty reports whether row y changed since the last ClearDirty.
func (g *Grid) RowDirty(y int) bool {
	if g.allDirty {
		return true
	}
	if y < 0 || y >= g.rows {
		return false
	}
	return g.dirty[y]
}

// AnyDirty reports whether anything changed since the last ClearDirty.
func (g *Grid) AnyDirty() bool {
	if g.allDirty {
		return true
	}
	return slices.Contains(g.dirty, true)
}

// ClearDirty marks the whole grid clean. Whatever draws the grid calls
// this once everything showing it has been drawn.
func (g *Grid) ClearDirty() {
	g.allDirty = false
	clear(g.dirty)
}

// CursorClaimed reports whether the cursor has been placed since the
// claim was last reset.
func (g *Grid) CursorClaimed() bool { return g.cursorClaimed }

// ResetCursorClaim forgets who placed the cursor, before a fresh pass of
// drawing decides again.
func (g *Grid) ResetCursorClaim() { g.cursorClaimed = false }

// MarkAllDirty forces a full repaint on the next frame, for when
// something outside the cell contents changed (window resize, theme).
func (g *Grid) MarkAllDirty() { g.allDirty = true }

// MarkRowDirty marks one row for repainting, for a change to how the row
// looks that no cell of it records.
func (g *Grid) MarkRowDirty(y int) { g.dirtyRow(y) }

func (g *Grid) dirtyRow(y int) {
	if y >= 0 && y < g.rows {
		g.dirty[y] = true
	}
}

func (g *Grid) inBounds(x, y int) bool {
	return x >= 0 && y >= 0 && x < g.cols && y < g.rows
}

// RuneWidth returns how many columns r occupies: 0 for a combining
// mark, 2 for a double-width character, 1 otherwise.
//
// A terminal that disagrees with its own grid about how wide a character
// is will corrupt the screen, so width is asked of this package and
// nowhere else. StringWidth answers the same question for a run of text,
// where the unit is a grapheme cluster rather than a rune.
func RuneWidth(r rune) int {
	if r >= 0x20 && r < 0x7f {
		// Printable ASCII, which is nearly every character a terminal is
		// sent, and the one range where the answer is always one.
		return 1
	}
	switch w := uniseg.StringWidth(string(r)); {
	case w <= 0:
		return 0
	case w >= 2:
		return 2
	default:
		return 1
	}
}

// Clusters splits a string into grapheme clusters, which is the unit a
// grid draws: a base character and its combining marks share one cell,
// and so do the halves of a flag.
//
// Anything colouring part of a string has to cut it here. Writing runes
// one at a time gives a combining mark a cell of its own and a flag two.
func Clusters(s string) []string {
	var out []string
	state, at := -1, 0
	for at < len(s) {
		size, next := NextCluster(s[at:], state)
		if size == 0 {
			break
		}
		out = append(out, s[at:at+size])
		at, state = at+size, next
	}
	return out
}

// NextCluster is how many bytes the first grapheme cluster of s takes,
// and the state to hand the next call. Start with a state of -1. A size
// of zero means there is nothing left.
//
// It is Clusters for a caller that only walks the string: the slice
// Clusters builds is an allocation, and one per line per frame is one
// the window pays for while nothing moves.
func NextCluster(s string, state int) (size, next int) {
	if s == "" {
		return 0, state
	}
	cluster, _, _, next := uniseg.FirstGraphemeClusterInString(s, state)
	return len(cluster), next
}

// StringWidth returns how many columns a string takes when written into
// a grid, which is not how many runes it holds: a CJK character takes
// two, and a combining mark shares its base character's cell.
//
// It counts what SetString will actually spend rather than asking for a
// display width, because a grid gives every cluster at least one column.
// A control character or a zero-width space has no display width and
// still takes a cell.
func StringWidth(s string) int {
	total, state := 0, -1
	for len(s) > 0 {
		var w int
		_, s, w, state = uniseg.FirstGraphemeClusterInString(s, state)
		if w >= 2 {
			total += 2
			continue
		}
		total++
	}
	return total
}

// CutLeft drops the first n columns of s and reports how many columns it
// dropped, which is more than n when a double-width character straddles
// the cut: a character goes whole or stays whole.
//
// Columns are counted the way SetString spends them, so text cut here
// lines up with the same text written whole.
func CutLeft(s string, n int) (rest string, cut int) {
	at, state := 0, -1
	for at < n && len(s) > 0 {
		var w int
		_, s, w, state = uniseg.FirstGraphemeClusterInString(s, state)
		if w >= 2 {
			at += 2
			continue
		}
		at++
	}
	return s, at
}

// SnapToClusters moves each offset in ends forward to the end of the
// grapheme cluster it falls inside, and clamps one past the end of s.
//
// It is for a caller colouring parts of a string: a boundary in the
// middle of a cluster gives a combining mark a cell of its own. The
// offsets have to be in order, which is what a run of stretches is.
func SnapToClusters(s string, ends []int) {
	i, at, state := 0, 0, -1
	for i < len(ends) && at < len(s) {
		var cluster string
		cluster, _, _, state = uniseg.FirstGraphemeClusterInString(s[at:], state)
		next := at + len(cluster)
		for ; i < len(ends) && ends[i] < next; i++ {
			if ends[i] > at {
				ends[i] = next
			}
		}
		at = next
	}
	for ; i < len(ends); i++ {
		ends[i] = min(ends[i], len(s))
	}
}

// Point is a cell coordinate.
type Point struct{ X, Y int }

// Selection is a highlighted range of cells.
//
// Anchor is where the drag started and Cursor is where it is now, in
// either order — normalising them is the caller's business only if it
// wants to know which end is which. A block selection covers the
// rectangle between them rather than the flowing range.
type Selection struct {
	Anchor, Cursor Point
	Active         bool
	Block          bool
}

// normalised returns the selection with its ends in reading order.
func (s Selection) normalised() (from, to Point) {
	from, to = s.Anchor, s.Cursor
	if s.Block {
		if from.X > to.X {
			from.X, to.X = to.X, from.X
		}
		if from.Y > to.Y {
			from.Y, to.Y = to.Y, from.Y
		}
		return from, to
	}
	if to.Y < from.Y || (to.Y == from.Y && to.X < from.X) {
		from, to = to, from
	}
	return from, to
}

// Contains reports whether x,y falls inside the selection.
func (s Selection) Contains(x, y int) bool {
	if !s.Active {
		return false
	}
	from, to := s.normalised()
	if y < from.Y || y > to.Y {
		return false
	}
	if s.Block {
		return x >= from.X && x <= to.X
	}
	switch {
	case from.Y == to.Y:
		return x >= from.X && x <= to.X
	case y == from.Y:
		return x >= from.X
	case y == to.Y:
		return x <= to.X
	default:
		return true
	}
}

// Selection returns the current selection.
func (g *Grid) Selection() Selection { return g.sel }

// SetSelection replaces the selection, dirtying every row either the old
// or the new one covered so the highlight is repainted.
func (g *Grid) SetSelection(s Selection) {
	if s == g.sel {
		return
	}
	old := g.sel
	g.sel = s
	for _, cur := range []Selection{old, s} {
		if !cur.Active {
			continue
		}
		from, to := cur.normalised()
		for y := max(from.Y, 0); y <= min(to.Y, g.rows-1); y++ {
			g.dirtyRow(y)
		}
	}
}

// ClearSelection removes the highlight.
func (g *Grid) ClearSelection() { g.SetSelection(Selection{}) }

// SelectedText returns the selected cells as text, with a newline
// between rows and trailing blanks trimmed from each — which is what
// makes pasting a selected command line work.
func (g *Grid) SelectedText() string {
	if !g.sel.Active {
		return ""
	}
	from, to := g.sel.normalised()
	var sb strings.Builder
	for y := max(from.Y, 0); y <= min(to.Y, g.rows-1); y++ {
		var row strings.Builder
		for x := 0; x < g.cols; x++ {
			if !g.sel.Contains(x, y) {
				continue
			}
			c := g.cells[y*g.cols+x]
			// The continuation half of a wide character carries no rune
			// of its own; its lead cell already contributed one.
			if c.Width == 0 {
				continue
			}
			row.WriteRune(c.Rune)
			for _, m := range c.Comb {
				row.WriteRune(m)
			}
		}
		if y > max(from.Y, 0) {
			sb.WriteByte('\n')
		}
		sb.WriteString(strings.TrimRight(row.String(), " "))
	}
	return sb.String()
}
