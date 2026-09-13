// Package render draws a grid.Grid onto an ebiten image using a glyph
// atlas. Everything is batched: cell backgrounds in one pass, then the
// text in one pass per atlas page.
package render

import (
	"image/color"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/marcus/gridterm/glyph"
	"github.com/marcus/gridterm/grid"
)

// maxBatchVerts caps how many vertices go into one DrawTriangles call.
// ebiten indexes vertices with uint16, so 65536 is the hard ceiling; a
// quad is 4 vertices, so the cap must be a multiple of 4. At 16383 quads
// per batch this only splits on grids past roughly 200x80, but getting
// it wrong corrupts geometry rather than failing loudly.
const maxBatchVerts = 65532

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

// CellSize returns the pixel size of one cell.
func (r *Renderer) CellSize() (w, h int) {
	m := r.atlas.Metrics()
	return m.CellW, m.CellH
}

// GridSizeFor returns how many columns and rows fit in a window of the
// given pixel size.
func (r *Renderer) GridSizeFor(pxW, pxH int) (cols, rows int) {
	m := r.atlas.Metrics()
	return max(pxW/m.CellW, 1), max(pxH/m.CellH, 1)
}

// Draw paints the dirty rows of g onto dst and marks the grid clean.
//
// Callers relying on clean rows being skipped must also call
// ebiten.SetScreenClearedEveryFrame(false); otherwise the rows this
// skips are blank rather than showing the previous frame.
func (r *Renderer) Draw(dst *ebiten.Image, g *grid.Grid) {
	m := r.atlas.Metrics()
	cols, rows := g.Size()

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
		top := float32(y * m.CellH)
		g.BGRuns(y, func(x0, x1 int, c color.RGBA) {
			if c.A == 0 {
				return
			}
			r.push(dst, &r.bg,
				float32(x0*m.CellW), top,
				float32((x1-x0)*m.CellW), float32(m.CellH),
				0, 0, 1, 1, c)
		})
	}
	r.flush(dst, &r.bg)

	for y := 0; y < rows; y++ {
		if !g.RowDirty(y) {
			continue
		}
		top := float32(y * m.CellH)
		for x := 0; x < cols; x++ {
			c := g.At(x, y)
			if c.Rune == ' ' || c.Rune == 0 {
				continue
			}
			style := glyph.Regular
			if c.Attr&grid.AttrBold != 0 {
				style = glyph.Bold
			}
			gl := r.atlas.Get(c.Rune, style)
			if gl.Empty {
				continue
			}
			// Rasterising a new glyph can add an atlas page, so the
			// per-page batches are grown here rather than up front.
			r.growPages()
			sz := gl.Rect.Size()
			r.push(dst, &r.fg[gl.Page],
				float32(x*m.CellW+gl.Offset.X), top+float32(gl.Offset.Y),
				float32(sz.X), float32(sz.Y),
				float32(gl.Rect.Min.X), float32(gl.Rect.Min.Y),
				float32(gl.Rect.Max.X), float32(gl.Rect.Max.Y),
				g.FGOf(x, y))
		}
	}
	for i := range r.fg {
		r.flush(dst, &r.fg[i])
	}

	g.ClearDirty()
}

func (r *Renderer) reset() {
	r.bg.verts, r.bg.idx = r.bg.verts[:0], r.bg.idx[:0]
	for i := range r.fg {
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
