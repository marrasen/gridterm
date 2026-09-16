package render

import (
	"image"
	"image/color"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/marrasen/gridterm/grid"
)

// Layer is one grid drawn at a position on screen, backed by a texture
// that is only repainted when the grid changes.
//
// Layers are what make things overlap. A menu or a dialog is its own
// layer above the one it covers, so the grid underneath never has to
// know it is hidden, and uncovering it costs a blit rather than a
// repaint.
//
// A layer belongs to one compositor. Two compositors sharing a layer
// would each think the other's repaint was their own.
type Layer struct {
	Grid *grid.Grid

	// X and Y place the layer's top-left corner on screen, in pixels.
	X, Y int

	// Hidden skips the layer, keeping its texture for when it comes
	// back.
	Hidden bool

	// Transparent clears every row before repainting it, so the layer
	// below shows through wherever a cell has no background.
	//
	// A row holding a cell that is not fully opaque is cleared whether or
	// not this is set, because otherwise whatever the texture held before
	// would show through. Setting it skips that per-row check.
	Transparent bool

	// Frost puts frosted glass behind part of the layer, taking what is
	// already on screen as its backdrop. Nil for an ordinary layer.
	Frost *Frost

	// Scale shrinks or blows up the texture as it is blitted, for a grid
	// drawn at a size the room it goes in cannot hold. 0 and 1 both mean
	// none.
	//
	// The texture keeps the grid's own pixel size, so the text is
	// rasterised once at full size and the scaling costs one blit.
	Scale float64

	// Geom is where the layer's grid lands in pixels, for a layer whose
	// owner works that out rather than leaving it to the compositor.
	// Nil for an ordinary layer, which is measured against the window.
	//
	// It is for a region: a hole in the window whose columns are the
	// window's own, down to the odd pixels the window had over.
	// Measured on its own a region would put those somewhere else, and
	// its text would sit a pixel or two off the text beside it.
	//
	// The owner keeps it up to date before the frame is drawn.
	Geom *Geometry

	tex *ebiten.Image

	// painted records that the texture matches its grid. It cannot be
	// read off the grid's own damage flags: those are shared by every
	// layer showing that grid, so one layer's repaint would look like
	// every layer's. Stated this way round, a layer starts life needing
	// paint.
	painted bool

	// full records that the texture is behind by an unknown amount, so
	// the grid's row damage describes someone else's frame rather than
	// the gap this texture has to close. Being behind and knowing what
	// changed are different states, and drawing the second when it is
	// really the first leaves rows blank for good.
	//
	// Repainting in full re-dirties the shared grid, so a sibling drawn
	// later in the same frame draws rows it had already done. That costs
	// one extra repaint on the frame a pane appears, not every frame.
	full bool

	// lastGrid and lastTransparent spot exported fields being changed,
	// which no damage flag reports.
	lastGrid        *grid.Grid
	lastTransparent bool

	// lastGeom spots a carried geometry moving the grid inside a
	// texture that stayed the same size. Nothing else would notice: the
	// compositor does not measure a layer that brought its own, and a
	// geometry can change with no cell of the grid touched.
	lastGeom [4]int
}

// Size returns the layer's pixel size, which is its grid measured by
// the given geometry. The geometry must have been laid out for that
// grid.
func (l *Layer) Size(geo *Geometry) (w, h int) { return geo.Width(), geo.Height() }

// ensure allocates the layer's texture, or replaces one that no longer
// matches the grid. It reports whether the texture changed size, which
// uncovers whatever the old one was hiding.
func (l *Layer) ensure(geo *Geometry) bool {
	w, h := l.Size(geo)
	if l.tex != nil {
		if b := l.tex.Bounds(); b.Dx() == w && b.Dy() == h {
			return false
		}
		l.tex.Deallocate()
	}
	l.tex = ebiten.NewImage(w, h)
	l.invalidate()
	return true
}

// invalidate marks the texture behind by an unknown amount.
func (l *Layer) invalidate() { l.painted, l.full = false, true }

// repaint draws the grid into the layer's texture.
//
// geo must have been laid out for the layer's grid.
func (l *Layer) repaint(r *Renderer, geo *Geometry) {
	if l.full {
		l.Grid.MarkAllDirty()
	}
	_, rows := l.Grid.Size()
	width := geo.Width()
	for y := 0; y < rows; y++ {
		if !l.Grid.RowDirty(y) || !l.clears(y) {
			continue
		}
		// The outer box, so the padding around a row is wiped with it
		// rather than keeping whatever the row before left there.
		at, height := geo.RowBox(y, y+1)
		strip := image.Rect(0, at, width, at+height)
		l.tex.SubImage(strip).(*ebiten.Image).Clear()
	}
	r.Draw(l.tex, l.Grid, geo)
	l.painted, l.full = true, false
}

// clears reports whether row y has to be wiped before it is drawn.
//
// A background that is not fully opaque cannot cover what the texture
// held before: a clear one paints no quad at all, and a partly clear one
// blends. That is the point for a transparent layer and a bug for any
// other, so the row itself decides rather than the caller.
func (l *Layer) clears(y int) bool {
	if l.Transparent {
		return true
	}
	seeThrough := false
	l.Grid.BGRuns(y, func(_, _ int, c color.RGBA) {
		if c.A != 0xff {
			seeThrough = true
		}
	})
	return seeThrough
}

// CompositorStats reports what the last composited frame cost.
//
// The embedded Stats covers repainting layer textures. It does not count
// the blits, which are a full-screen textured quad each and are usually
// the larger cost; those are counted separately.
type CompositorStats struct {
	Stats // summed over the layers repainted this frame

	Layers    int  // layers in the stack
	Repainted int  // layers whose texture was repainted
	Blits     int  // textures drawn to the screen
	Frosted   int  // frosted panels drawn behind a layer
	Cleared   bool // the screen was wiped before blitting
	Skipped   bool // nothing changed, so the frame was left alone
}

// Compositor draws a stack of layers, bottom first.
//
// A frame where nothing changed is skipped entirely, which is what keeps
// an idle window free. Callers relying on that must also call
// ebiten.SetScreenClearedEveryFrame(false), or a skipped frame is blank
// rather than showing the one before it.
//
// Nothing is erased between frames, so the bottom layer should cover the
// screen. When the geometry changes — a layer moves, hides, resizes or
// leaves — the screen is wiped first, because otherwise the pixels it
// vacated would stay.
type Compositor struct {
	r      *Renderer
	layers []*Layer
	stats  CompositorStats

	// last is the stack as it was drawn, so a layer moving, appearing or
	// changing order repaints the screen even when no grid is dirty.
	last []placement

	// lastScreen catches ebiten handing back a fresh offscreen image,
	// which it does on any window resize and which arrives blank. The
	// identity is what changes; the bounds are checked too in case a
	// screen is ever resized in place.
	lastScreen     *ebiten.Image
	lastScreenRect image.Rectangle

	// lastAtlas notices the glyphs being re-rasterised under a layer
	// whose pixel size happens not to change with them.
	lastAtlas uint64

	// geo measures whichever layer is being sized or repainted. Reused
	// so a frame does not allocate.
	geo Geometry

	// OnError reports a failure Draw cannot hand back, because the game
	// loop calls it and there is nowhere to return one. A nil OnError
	// drops them.
	OnError func(error)

	// shaders and scratch are the frosted-glass machinery, built on
	// first use so a window with nothing frosted never pays for them.
	shaders      shaders
	scratch      scratch
	shaderFailed bool
}

// placement is the part of a layer's state that changes what the screen
// looks like without dirtying any grid.
type placement struct {
	l      *Layer
	x, y   int
	hidden bool

	// scale is how big the texture is blitted, because a layer drawn at
	// a different size covers different pixels with no cell of its grid
	// touched.
	scale float64

	// frost is where the glass is, because moving it changes the screen
	// without dirtying any grid. Nothing in the program does that today:
	// a dialog that moves has redrawn its cells. It is here so that a
	// dialog which animates its panel, or sizes it from something other
	// than its own text, cannot silently freeze.
	frost image.Rectangle
}

// NewCompositor returns a compositor drawing with the given renderer.
func NewCompositor(r *Renderer) *Compositor { return &Compositor{r: r} }

// Add puts a layer on top of the stack, ignoring one already in it.
//
// The compositor then owns the damage flags on that layer's grid, which
// is why a layer belongs to one compositor and one only.
func (c *Compositor) Add(l *Layer) {
	for _, have := range c.layers {
		if have == l {
			return
		}
	}
	c.layers = append(c.layers, l)
}

// Remove takes a layer out of the stack and frees its texture.
func (c *Compositor) Remove(l *Layer) {
	for i, have := range c.layers {
		if have != l {
			continue
		}
		copy(c.layers[i:], c.layers[i+1:])
		// Clear the slot the shift vacated, or a slice a caller already
		// took keeps a duplicate, and the compositor keeps the removed
		// layer alive.
		c.layers[len(c.layers)-1] = nil
		c.layers = c.layers[:len(c.layers)-1]
		if l.tex != nil {
			l.tex.Deallocate()
			l.tex = nil
		}
		l.invalidate()
		return
	}
}

// Layers returns the stack, bottom first. Reordering the slice reorders
// the stack, but Add and Remove invalidate it, so do not hold it across
// either.
func (c *Compositor) Layers() []*Layer { return c.layers }

// Stats returns the cost of the most recent Draw.
func (c *Compositor) Stats() CompositorStats { return c.stats }

// Draw repaints the layers that changed and blits the stack onto screen.
//
// It takes ownership of the damage flags on every grid in the stack,
// clearing them once each layer showing that grid has been drawn.
// Nothing else may rely on those flags surviving a frame.
func (c *Compositor) Draw(screen *ebiten.Image) {
	c.stats = CompositorStats{Layers: len(c.layers)}

	// A rebuilt atlas re-rasterises every glyph. The cell box is not the
	// signal for that: two font sizes can share one.
	if gen := c.r.Generation(); gen != c.lastAtlas {
		c.lastAtlas = gen
		c.markAllStale()
	}

	// Sizing first: allocating a texture marks its layer stale, so this
	// has to settle before anything asks whether the frame is idle.
	resized := false
	for _, l := range c.layers {
		if l.Grid == nil {
			// Nothing to show. Give the texture back and uncover what it
			// was hiding.
			if l.tex != nil {
				l.tex.Deallocate()
				l.tex, l.lastGrid = nil, nil
				l.invalidate()
				resized = true
			}
			continue
		}
		if l.Grid != l.lastGrid || l.Transparent != l.lastTransparent {
			l.lastGrid, l.lastTransparent = l.Grid, l.Transparent
			l.invalidate()
		}
		geo := c.measure(l)
		if sig := geo.Signature(); sig != l.lastGeom {
			l.lastGeom = sig
			l.invalidate()
			// The pixels the grid moved off are still on screen.
			resized = true
		}
		if l.ensure(geo) {
			resized = true
		}
		if l.Grid.AnyDirty() {
			l.painted = false
		}
	}

	moved := !placementsMatch(c.last, c.layers)
	// A resized window arrives as a fresh, blank offscreen image, so the
	// stack has to be put back even though nothing else changed.
	rescreened := screen != c.lastScreen || screen.Bounds() != c.lastScreenRect
	// resized is in its own right: a layer that lost its grid has no
	// texture left to be stale, but the pixels it held are still there.
	if !c.anyVisibleStale() && !moved && !rescreened && !resized {
		c.stats.Skipped = true
		return
	}
	was := len(c.last)
	c.last = appendPlacements(c.last[:0], c.layers)
	if n := len(c.last); n < was {
		// Reusing the array leaves removed layers in the slots past the
		// end, which keeps them and their grids alive.
		clear(c.last[n:was])
	}
	c.lastScreen, c.lastScreenRect = screen, screen.Bounds()

	for _, l := range c.layers {
		if l.Hidden || l.Grid == nil || l.painted {
			continue
		}
		l.repaint(c.r, c.measure(l))
		c.stats.Stats.add(c.r.Stats())
		c.stats.Repainted++
	}
	// Damage is cleared once every layer has had it, not by whichever
	// drew first, so several layers can share one grid and each still
	// repaint only the rows that changed.
	for _, l := range c.layers {
		if l.Grid == nil {
			continue
		}
		// A layer still behind when the record of which rows moved is
		// thrown away has to repaint the lot when it comes back.
		if !l.painted {
			l.full = true
		}
		l.Grid.ClearDirty()
	}

	// Blitting alone cannot erase, so anything that uncovers pixels has
	// to wipe the screen first.
	//
	// Frosted glass forces it too. The panel takes what is on screen as
	// its backdrop, so drawing over a screen that still held the last
	// frame's panel would blur the panel into itself, a little more on
	// every frame.
	if moved || resized || rescreened || c.anyFrosted() {
		screen.Clear()
		c.stats.Cleared = true
	}
	// Every visible layer is blitted, not just the repainted ones: a
	// layer below changing shows through the ones above it.
	for _, l := range c.layers {
		if l.Hidden || l.Grid == nil || l.tex == nil {
			continue
		}
		// The glass first: it reads the layers already blitted under it,
		// and this layer's own text then goes on top of it.
		if l.Frost != nil && !l.Frost.Rect.Empty() {
			c.drawFrost(screen, l)
		}
		screen.DrawImage(l.tex, blitOp(l))
		c.stats.Blits++
	}

}

// blitOp is how a layer's texture goes on screen: scaled if it asked to
// be, then moved to its place.
//
// Linear filtering while it is scaled, because text shrunk by anything
// but a whole number is a mess when the nearest pixel is taken.
func blitOp(l *Layer) *ebiten.DrawImageOptions {
	op := &ebiten.DrawImageOptions{}
	if s := l.Scale; s > 0 && s != 1 {
		op.GeoM.Scale(s, s)
		op.Filter = ebiten.FilterLinear
	}
	op.GeoM.Translate(float64(l.X), float64(l.Y))
	return op
}

// measure returns where a layer's grid lands in pixels, working it out
// unless the layer came with it.
func (c *Compositor) measure(l *Layer) *Geometry {
	if l.Geom != nil {
		return l.Geom
	}
	c.r.MeasureAt(l.Grid, l.X, l.Y, &c.geo)
	return &c.geo
}

// onError reports a failure the compositor cannot hand back, because
// Draw is called by the game loop and has nowhere to return one.
func (c *Compositor) onError(err error) {
	if c.OnError != nil {
		c.OnError(err)
	}
}

// anyVisibleStale reports whether a layer the viewer can see needs
// repainting. A hidden layer stays stale until it comes back.
func (c *Compositor) anyVisibleStale() bool {
	for _, l := range c.layers {
		if !l.Hidden && l.Grid != nil && !l.painted {
			return true
		}
	}
	return false
}

func (c *Compositor) markAllStale() {
	for _, l := range c.layers {
		l.invalidate()
	}
}

// placementOf records where one layer currently sits. The grid is not
// part of it: swapping one is caught by Layer.lastGrid, which has to
// mark the layer behind as well.
func placementOf(l *Layer) placement {
	p := placement{l: l, x: l.X, y: l.Y, hidden: l.Hidden, scale: l.Scale}
	if l.Frost != nil {
		p.frost = l.Frost.Rect
	}
	return p
}

// appendPlacements records where the layers currently sit, reusing dst's
// storage so a frame does not allocate.
func appendPlacements(dst []placement, layers []*Layer) []placement {
	for _, l := range layers {
		dst = append(dst, placementOf(l))
	}
	return dst
}

// placementsMatch reports whether the stack would draw in the same place
// as it did last frame. It compares against the live layers rather than
// a fresh snapshot, so an idle frame allocates nothing.
func placementsMatch(last []placement, layers []*Layer) bool {
	if len(last) != len(layers) {
		return false
	}
	for i, l := range layers {
		if last[i] != placementOf(l) {
			return false
		}
	}
	return true
}

// add accumulates one layer's cost into a frame total.
func (s *Stats) add(o Stats) {
	s.RowsDrawn += o.RowsDrawn
	s.Quads += o.Quads
	s.DrawCalls += o.DrawCalls
	s.CellsTotal += o.CellsTotal
}
