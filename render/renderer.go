// Package render draws a grid.Grid onto an ebiten image using a glyph
// atlas. Everything is batched: cell backgrounds in one pass, then the
// text in one pass per atlas page.
package render

import (
	"image/color"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/marrasen/gridterm/glyph"
	"github.com/marrasen/gridterm/grid"
)

// maxBatchVerts caps how many vertices go into one DrawTriangles call.
// ebiten indexes vertices with uint16, so 65536 is the hard ceiling; a
// quad is 4 vertices, so the cap must be a multiple of 4. At 16383 quads
// per batch this only splits on grids past roughly 200x80, but getting
// it wrong corrupts geometry rather than failing loudly.
const maxBatchVerts = 65532

// barFraction sets how thick a bar or underline cursor is, as a
// fraction of the cell height. Kept proportional so it stays visible at
// small sizes without swallowing the glyph at large ones.
const barFraction = 8

// lineFraction sets how thick an underline or strikethrough rule is.
// Thinner than the cursor: it sits under text rather than replacing it.
const lineFraction = 14

// dimPart of dimWhole is how far a dim cell's foreground is blended
// towards its background. SGR 2 has no exact definition; this matches
// what most terminals do.
const (
	dimPart  = 55
	dimWhole = 100
)

// Stats reports what the last frame cost, so a caller can see whether
// batching and damage tracking are actually doing anything.
type Stats struct {
	RowsDrawn  int // rows repainted this frame
	Quads      int // background + glyph quads submitted
	DrawCalls  int // DrawTriangles calls issued
	CellsTotal int
}

// batch accumulates quads that share one source texture.
type batch struct {
	verts []ebiten.Vertex
	idx   []uint16
	src   *ebiten.Image
}

// Renderer owns the scratch vertex buffers. Reusing them across frames
// is what keeps a full-screen repaint allocation-free.
type Renderer struct {
	atlas *glyph.Atlas

	// white is a 1x1 opaque texture used as the source for solid
	// background quads, so backgrounds batch the same way glyphs do.
	white *ebiten.Image

	bg batch
	fg []batch // one per atlas page

	stats Stats
	opts  ebiten.DrawTrianglesOptions

	// winW and winH are the window in pixels, so a grid that falls a
	// few pixels short of it can be stretched to reach the edge.
	winW, winH int
}

// New returns a renderer drawing with the given atlas.
func New(a *glyph.Atlas) *Renderer {
	w := ebiten.NewImage(1, 1)
	w.Fill(color.White)
	return &Renderer{
		atlas: a,
		white: w,
		bg:    batch{src: w},
		opts: ebiten.DrawTrianglesOptions{
			ColorScaleMode: ebiten.ColorScaleModePremultipliedAlpha,
		},
	}
}

// Stats returns the cost of the most recent Draw.
func (r *Renderer) Stats() Stats { return r.stats }

// Generation reports the atlas rebuild count, so a caller caching
// anything drawn from it can tell when the glyphs moved.
func (r *Renderer) Generation() uint64 { return r.atlas.Generation() }

// Metrics returns the font's measurements, for a caller laying a grid
// out itself rather than through Measure.
func (r *Renderer) Metrics() glyph.Metrics { return r.atlas.Metrics() }

// CellSize returns the pixel size of one cell.
func (r *Renderer) CellSize() (w, h int) {
	m := r.atlas.Metrics()
	return m.CellW, m.CellH
}

// GridSizeFor returns how many columns and rows fit in a window of the
// given pixel size.
func (r *Renderer) GridSizeFor(pxW, pxH int) (cols, rows int) {
	return r.GridSizeWithin(pxW, pxH, 0, 0)
}

// GridSizeWithin returns how many columns and rows fit in a window of
// the given pixel size, once padX and padY quarters of a cell have been
// set aside for padding.
//
// Padding is space the grid gains, so a caller that means to pad has to
// ask for fewer cells. It says how much here rather than being told
// afterwards, because being told would mean laying the window out again
// at a different size, and again after that.
func (r *Renderer) GridSizeWithin(pxW, pxH, padX, padY int) (cols, rows int) {
	m := r.atlas.Metrics()
	spareW := max(pxW-max(padX, 0)*m.CellW/grid.PadUnit, 0)
	spareH := max(pxH-max(padY, 0)*m.CellH/grid.PadUnit, 0)
	return max(spareW/m.CellW, 1), max(spareH/m.CellH, 1)
}

// SetWindow tells the renderer how big the window is.
//
// A window is rarely a whole number of cells across. The pixels over
// are given to the last column and row, so no strip of the window is
// left with nothing to paint it.
func (r *Renderer) SetWindow(pxW, pxH int) { r.winW, r.winH = pxW, pxH }

// Measure fills in where a grid's columns and rows land in pixels.
//
// The geometry is handed in rather than returned, so a caller measuring
// every frame does not allocate, and so the mouse keeps its own rather
// than sharing the one the renderer draws with.
func (r *Renderer) Measure(g *grid.Grid, geo *Geometry) {
	r.MeasureAt(g, 0, 0, geo)
}

// MeasureAt is Measure for a grid drawn with its corner at x,y in the
// window.
//
// A grid is only stretched into the window below and to the right of
// its own corner. One that reached the far edge whatever its corner
// would be measured for more window than it has, and its texture made
// that much too big.
func (r *Renderer) MeasureAt(g *grid.Grid, x, y int, geo *Geometry) {
	geo.Layout(g, r.atlas.Metrics())
	geo.Fill(r.winW-x, r.winH-y)
}

// Draw paints the dirty rows of g onto dst.
//
// It does not clear the grid's damage. The caller does that, once
// everything drawing from that grid has been drawn: clearing here would
// let the first of several layers on one grid take the row damage for
// all of them, and leave the rest showing stale text.
//
// Callers relying on clean rows being skipped must also call
// ebiten.SetScreenClearedEveryFrame(false); otherwise the rows this
// skips are blank rather than showing the previous frame.
//
// The cursor is always drawn. A layer goes through draw instead, which
// takes the half of the blink the compositor settled for that frame.
func (r *Renderer) Draw(dst *ebiten.Image, g *grid.Grid, geo *Geometry) {
	r.draw(dst, g, geo, true)
}

// draw paints the dirty rows of g onto dst, leaving the cursor out when
// showCursor is false, which is the off half of a blink.
func (r *Renderer) draw(dst *ebiten.Image, g *grid.Grid, geo *Geometry, showCursor bool) {
	cols, rows := g.Size()
	cur := g.Cursor()
	curVisible := showCursor && cur.Visible && cur.X >= 0 && cur.X < cols && cur.Y >= 0 && cur.Y < rows
	// A cursor on the continuation half of a double-width character
	// belongs on its lead cell, which is where the glyph actually is.
	// Left alone, a block cursor would paint over half the character in
	// the foreground colour and hide it.
	if curVisible && cur.X > 0 && g.At(cur.X, cur.Y).Width == 0 {
		cur.X--
	}

	r.reset()
	r.stats = Stats{CellsTotal: cols * rows}
	for y := 0; y < rows; y++ {
		if g.RowDirty(y) {
			r.stats.RowsDrawn++
		}
	}

	// Two passes, not one interleaved pass. Backgrounds must all land
	// before any glyph does, and a batch that fills up mid-pass is
	// flushed immediately — so interleaving would let a later row's
	// background paint over an earlier row's descenders.
	for y := 0; y < rows; y++ {
		if !g.RowDirty(y) {
			continue
		}
		g.BGRuns(y, func(x0, x1 int, c color.RGBA) {
			if c.A == 0 {
				return
			}
			b := bgRect(geo, x0, x1, y)
			r.push(dst, &r.bg, b.X, b.Y, b.W, b.H, 0, 0, 1, 1, c)
		})
		r.pushArt(dst, g, y, geo)
		r.pushRules(dst, g, y, geo)
		if curVisible && cur.Y == y {
			r.pushCursor(dst, g, cur, geo)
		}
	}
	r.flush(dst, &r.bg)

	for y := 0; y < rows; y++ {
		if !g.RowDirty(y) {
			continue
		}
		for x := 0; x < cols; x++ {
			c := g.At(x, y)
			// Width 0 is the column a double-width character spills
			// into; its glyph was already drawn by the lead cell. A
			// blank still has to be drawn when it carries a combining
			// mark, which is how a mark on a space appears at all.
			if c.Width == 0 {
				continue
			}
			if (c.Rune == ' ' || c.Rune == 0) && len(c.Comb) == 0 {
				continue
			}
			if c.Attr&grid.AttrHidden != 0 {
				continue
			}
			style := glyph.Regular
			if c.Attr&grid.AttrBold != 0 {
				style |= glyph.Bold
			}
			if c.Attr&grid.AttrItalic != 0 {
				style |= glyph.Italic
			}
			fg := g.FGOf(x, y)
			// A block cursor inverts the cell it sits on, so the glyph
			// has to come back out in the background colour.
			if curVisible && cur.Style == grid.CursorBlock && cur.X == x && cur.Y == y {
				fg = g.BGOf(x, y)
			}
			if c.Attr&grid.AttrDim != 0 {
				fg = dimmed(fg, g.BGOf(x, y))
			}
			r.pushGlyph(dst, x, y, c.Rune, style, fg, geo)
			for _, cb := range c.Comb {
				r.pushGlyph(dst, x, y, cb, style, fg, geo)
			}
		}
	}
	// Glyph batches flush in atlas-page order, not push order. A base
	// glyph and its combining marks are rasterised together and so
	// almost always share a page, but a page boundary falling between
	// them would draw the mark underneath the base. Rare enough to
	// accept, and the alternative is a draw call per cell.
	for i := range r.fg {
		r.flush(dst, &r.fg[i])
	}
}

// pushArt queues the quads for one row's art.
//
// Drawn in the background pass, because it is a set of plain rectangles
// like a background is, and because a glyph on the same cell should land
// over it rather than under it.
func (r *Renderer) pushArt(dst *ebiten.Image, g *grid.Grid, y int, geo *Geometry) {
	cols, _ := g.Size()
	for x := 0; x < cols; x++ {
		c := g.At(x, y)
		if c.Art.Kind == grid.ArtNone {
			continue
		}
		col, ok := artColour(g, x, y, c)
		if !ok {
			continue
		}
		for _, b := range artBars(c.Art, x, y, artCols(g, x, y), geo) {
			r.push(dst, &r.bg, b.X, b.Y, b.W, b.H, 0, 0, 1, 1, col)
		}
	}
}

// artBars is where a piece of art's rectangles go inside the cells it
// has been given.
func artBars(art grid.Art, x, y, cols int, geo *Geometry) []bar {
	switch art.Kind {
	case grid.ArtGraph:
		return graphBars(art, x, y, geo)
	case grid.ArtIcon:
		return iconBars(art, x, y, cols, geo)
	}
	return nil
}

// artCols is how many cells a piece of art may draw across: its own, and
// the one after it while nothing else is drawn there.
func artCols(g *grid.Grid, x, y int) int {
	cols, _ := g.Size()
	if x+1 >= cols {
		return 1
	}
	switch next := g.At(x+1, y); {
	case next.Art.Kind != grid.ArtNone:
		return 1
	case next.Rune != 0 && next.Rune != ' ':
		return 1
	case len(next.Comb) > 0:
		return 1
	case next.Attr&(grid.AttrUnderline|grid.AttrStrike) != 0:
		return 1
	}
	return 2
}

// artColour is what a cell's art is drawn in, and whether to draw it at
// all.
//
// The same rules a glyph on that cell would follow: hidden is not drawn,
// reverse video has already been resolved into FGOf, and dim is mixed
// towards the background.
func artColour(g *grid.Grid, x, y int, c grid.Cell) (color.RGBA, bool) {
	if c.Attr&grid.AttrHidden != 0 {
		return color.RGBA{}, false
	}
	col := g.FGOf(x, y)
	if c.Attr&grid.AttrDim != 0 {
		col = dimmed(col, g.BGOf(x, y))
	}
	return col, col.A != 0
}

// bar is one rectangle of a graph, in pixels.
type bar struct{ X, Y, W, H float32 }

// graphBars is where a graph's bars go inside one cell.
//
// They share the cell's width, counted from its left edge each time so
// that they tile it exactly: every bar is a whole number of pixels wide
// and no column is drawn twice or left out. Quads are not antialiased,
// so a bar narrower than a pixel would only be drawn when a pixel centre
// happened to fall inside it -- and the same seconds would vanish every
// time, which is the one thing a graph of a run must not do.
//
// A cell too narrow for every bar shows the newest that fit. Dropping
// the oldest is the honest loss: the graph is read from the right.
//
// A bar of nothing still stands one pixel high, because a gap and a zero
// read the same otherwise and the point of the graph is the shape.
func graphBars(art grid.Art, x, y int, geo *Geometry) []bar {
	cellW, ascent := geo.CellW(), geo.Ascent()
	// The x-height, so the graph sits on the baseline and stands about
	// as tall as the letters beside it.
	top := float32(geo.CellY(y)) + float32(ascent)*0.35
	foot := float32(geo.CellY(y) + ascent)
	tall := foot - top
	// Only the seconds really measured, and only as many as the cell has
	// whole pixels for.
	bars := min(art.Bars(), cellW)
	if tall <= 0 || bars <= 0 {
		return nil
	}
	// The newest ones, which are the last of them.
	first := grid.ArtGraphBars - bars
	left := geo.CellX(x)
	out := make([]bar, 0, bars)
	for i := 0; i < bars; i++ {
		x0 := left + i*cellW/bars
		x1 := left + (i+1)*cellW/bars
		h := tall * float32(art.Bar(first+i)) / grid.ArtGraphMax
		if h < 1 {
			h = 1
		}
		out = append(out, bar{X: float32(x0), Y: foot - h, W: float32(x1 - x0), H: h})
	}
	return out
}

// pushGlyph queues one glyph quad at cell x,y.
func (r *Renderer) pushGlyph(
	dst *ebiten.Image, x, y int, ch rune,
	style glyph.Style, fg color.RGBA, geo *Geometry,
) {
	gl := r.atlas.Get(ch, style)
	if gl.Empty {
		return
	}
	// Rasterising a new glyph can add an atlas page, so the per-page
	// batches are grown here rather than up front.
	r.growPages()
	sz := gl.Rect.Size()
	left, top := geo.CellX(x)+gl.Offset.X, geo.CellY(y)+gl.Offset.Y
	width, height := sz.X, sz.Y

	r.push(dst, &r.fg[gl.Page],
		float32(left), float32(top),
		float32(width), float32(height),
		float32(gl.Rect.Min.X), float32(gl.Rect.Min.Y),
		float32(gl.Rect.Max.X), float32(gl.Rect.Max.Y),
		fg)
	r.pushArms(dst, x, y, ch, gl, left, top, width, height, fg, geo)
}

// pushArms carries a line-drawing character's arms across whatever the
// grid leaves between its cell and the ones beside it. Left inside its
// own cell a line would stop at the gap, and a rule would break where a
// menu crosses the sidebar's margin.
//
// One pixel of the glyph's own edge repeated across the gap, rather than
// the glyph stretched to cover it: stretching moves every stroke in it,
// so a corner drawn that way sits beside the edge it should meet.
func (r *Renderer) pushArms(
	dst *ebiten.Image, x, y int, ch rune, gl glyph.Glyph,
	left, top, width, height int, fg color.RGBA, geo *Geometry,
) {
	armLeft, armRight, armUp, armDown, ok := glyph.Arms(ch)
	if !ok {
		return
	}
	b, s := &r.fg[gl.Page], gl.Rect
	if width == geo.CellW() {
		before, after := geo.ColGap(x)
		if armLeft && before > 0 {
			r.push(dst, b, float32(left-before), float32(top), float32(before), float32(height),
				float32(s.Min.X), float32(s.Min.Y), float32(s.Min.X+1), float32(s.Max.Y), fg)
		}
		if armRight && after > 0 {
			r.push(dst, b, float32(left+width), float32(top), float32(after), float32(height),
				float32(s.Max.X-1), float32(s.Min.Y), float32(s.Max.X), float32(s.Max.Y), fg)
		}
	}
	if height == geo.CellH() {
		before, after := geo.RowGap(y)
		if armUp && before > 0 {
			r.push(dst, b, float32(left), float32(top-before), float32(width), float32(before),
				float32(s.Min.X), float32(s.Min.Y), float32(s.Max.X), float32(s.Min.Y+1), fg)
		}
		if armDown && after > 0 {
			r.push(dst, b, float32(left), float32(top+height), float32(width), float32(after),
				float32(s.Min.X), float32(s.Max.Y-1), float32(s.Max.X), float32(s.Max.Y), fg)
		}
	}
}

// pushRules queues the underline and strikethrough rules for a row,
// merging adjacent cells that share an attribute and colour into one
// quad. They go in the background pass so the glyph lands on top, which
// is what keeps an underline from cutting through a descender.
func (r *Renderer) pushRules(dst *ebiten.Image, g *grid.Grid, y int, geo *Geometry) {
	cols, _ := g.Size()
	cellH, ascent := geo.CellH(), geo.Ascent()
	thick := float32(max(cellH/lineFraction, 1))

	for _, rule := range []struct {
		attr grid.Attr
		top  float32
	}{
		// Just below the baseline for the underline, and through the
		// middle of the x-height for the strikethrough.
		{grid.AttrUnderline, float32(min(ascent+1, cellH-int(thick)))},
		{grid.AttrStrike, float32(ascent) * 0.65},
	} {
		runStart := -1
		var runColor color.RGBA
		flush := func(end int) {
			if runStart < 0 {
				return
			}
			b := ruleRect(geo, runStart, end, y, rule.top, thick)
			r.push(dst, &r.bg, b.X, b.Y, b.W, b.H, 0, 0, 1, 1, runColor)
			runStart = -1
		}
		for x := 0; x < cols; x++ {
			c := g.At(x, y)
			col := g.FGOf(x, y)
			if c.Attr&grid.AttrDim != 0 {
				col = dimmed(col, g.BGOf(x, y))
			}
			on := c.Attr&rule.attr != 0 && c.Attr&grid.AttrHidden == 0 && col.A != 0
			switch {
			case !on:
				flush(x)
			case runStart < 0:
				runStart, runColor = x, col
			case col != runColor:
				flush(x)
				runStart, runColor = x, col
			}
		}
		flush(cols)
	}
}

// dimmed mixes a cell's foreground towards its background, which is what
// SGR 2 asks for.
func dimmed(fg, bg color.RGBA) color.RGBA {
	out := grid.Blend(fg, bg, dimPart, dimWhole)
	// The foreground's own alpha, because a cell with nothing behind it
	// is drawn all the same and a mixed alpha would fade the glyph out
	// rather than dim it.
	out.A = fg.A
	return out
}

// pushCursor queues the cursor's own quad. A block cursor fills the
// cell; underline and bar draw a sliver. All three go in the background
// pass, so the glyph lands on top of them.
func (r *Renderer) pushCursor(
	dst *ebiten.Image, g *grid.Grid, cur grid.Cursor, geo *Geometry,
) {
	col := g.FGOf(cur.X, cur.Y)
	if col.A == 0 {
		return
	}
	b := cursorRect(g, cur, geo)
	r.push(dst, &r.bg, b.X, b.Y, b.W, b.H, 0, 0, 1, 1, col)
}

// bgRect is where a run of cells' background goes: the outer box both
// ways, so the padding around a cell takes the colour of the cell it
// pads and no seam shows.
func bgRect(geo *Geometry, x0, x1, y int) bar {
	at, width := geo.ColBox(x0, x1)
	top, height := geo.RowBox(y, y+1)
	return bar{X: float32(at), Y: float32(top), W: float32(width), H: float32(height)}
}

// ruleRect is where an underline or a strikethrough goes under a run of
// cells, top being how far under the top of the cell it sits.
//
// Across the cells, not the room around them: padding is the window's
// furniture rather than part of the text, and a rule reaching into it
// is a line sticking out past what it underlines.
func ruleRect(geo *Geometry, x0, x1, y int, top, thick float32) bar {
	at, width := geo.CellsX(x0, x1)
	return bar{
		X: float32(at), Y: float32(geo.CellY(y)) + top,
		W: float32(width), H: thick,
	}
}

// cursorRect is where the cursor's own quad goes.
//
// A block inverts the whole cell, so it takes the outer box and covers
// the padding with it: anything less would leave a notch of ordinary
// background beside an inverted cell. A bar and an underline belong to
// the character, so they are measured from the cell. Drawn from the
// outer box, a bar in the window's first column would float half a
// character out in the margin, away from the text it marks.
func cursorRect(g *grid.Grid, cur grid.Cursor, geo *Geometry) bar {
	// A cursor over the lead half of a double-width character covers
	// both columns, matching where the glyph actually is.
	wide := 1
	if g.At(cur.X, cur.Y).Width == 2 {
		wide = 2
	}
	left, width := geo.ColBox(cur.X, cur.X+wide)
	top, height := geo.RowBox(cur.Y, cur.Y+1)
	if cur.Style != grid.CursorBlock {
		left, width = geo.CellsX(cur.X, cur.X+wide)
		top, height = geo.CellY(cur.Y), geo.CellH()
	}
	x, y := float32(left), float32(top)
	w, h := float32(width), float32(height)
	thick := float32(max(geo.CellH()/barFraction, 1))

	switch cur.Style {
	case grid.CursorUnderline:
		y += h - thick
		h = thick
	case grid.CursorBar:
		w = thick
	}
	return bar{X: x, Y: y, W: w, H: h}
}

func (r *Renderer) reset() {
	r.bg.verts, r.bg.idx = r.bg.verts[:0], r.bg.idx[:0]
	// A rebuilt atlas — a font size change — replaces its pages, so a
	// batch still holding the old texture would draw the old glyphs at
	// the new coordinates.
	if n := r.atlas.Pages(); len(r.fg) > n {
		r.fg = r.fg[:n]
	}
	for i := range r.fg {
		r.fg[i].src = r.atlas.Page(i)
		r.fg[i].verts, r.fg[i].idx = r.fg[i].verts[:0], r.fg[i].idx[:0]
	}
}

// growPages makes sure there is a batch per atlas page.
func (r *Renderer) growPages() {
	for len(r.fg) < r.atlas.Pages() {
		r.fg = append(r.fg, batch{src: r.atlas.Page(len(r.fg))})
	}
}

func (r *Renderer) flush(dst *ebiten.Image, b *batch) {
	if len(b.idx) == 0 {
		return
	}
	dst.DrawTriangles(b.verts, b.idx, b.src, &r.opts)
	r.stats.DrawCalls++
	r.stats.Quads += len(b.idx) / 6
	b.verts, b.idx = b.verts[:0], b.idx[:0]
}

// push appends one textured, tinted rectangle, flushing first if the
// batch has no room left within the uint16 index space.
//
// Vertex colours are premultiplied to match ColorScaleModePremultipliedAlpha,
// and glyph masks are themselves premultiplied white, so tinting is a
// plain multiply with no per-glyph shader work.
func (r *Renderer) push(
	dst *ebiten.Image, b *batch,
	dx, dy, dw, dh float32,
	sx0, sy0, sx1, sy1 float32,
	c color.RGBA,
) {
	if len(b.verts)+4 > maxBatchVerts {
		r.flush(dst, b)
	}
	base := uint16(len(b.verts))
	a := float32(c.A) / 255
	cr := float32(c.R) / 255 * a
	cg := float32(c.G) / 255 * a
	cb := float32(c.B) / 255 * a

	b.verts = append(b.verts,
		ebiten.Vertex{DstX: dx, DstY: dy, SrcX: sx0, SrcY: sy0,
			ColorR: cr, ColorG: cg, ColorB: cb, ColorA: a},
		ebiten.Vertex{DstX: dx + dw, DstY: dy, SrcX: sx1, SrcY: sy0,
			ColorR: cr, ColorG: cg, ColorB: cb, ColorA: a},
		ebiten.Vertex{DstX: dx, DstY: dy + dh, SrcX: sx0, SrcY: sy1,
			ColorR: cr, ColorG: cg, ColorB: cb, ColorA: a},
		ebiten.Vertex{DstX: dx + dw, DstY: dy + dh, SrcX: sx1, SrcY: sy1,
			ColorR: cr, ColorG: cg, ColorB: cb, ColorA: a},
	)
	b.idx = append(b.idx,
		base, base+1, base+2,
		base+1, base+2, base+3,
	)
}
