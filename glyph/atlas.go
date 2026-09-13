// Package glyph rasterises monospace glyphs once and packs them into
// GPU texture pages, so drawing a screenful of text is a handful of
// batched triangle draws rather than one texture upload per character.
package glyph

import (
	"fmt"
	"image"
	"image/color"

	"github.com/hajimehoshi/ebiten/v2"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
)

// pageSize is the edge length in pixels of each atlas page. One page
// holds a few thousand cell-sized glyphs, so a normal working set fits
// in one or two pages and a whole screen of text batches into one or two
// DrawTriangles calls.
const pageSize = 1024

// padding keeps one transparent pixel between packed glyphs so sampling
// at a quad edge cannot pick up a neighbour's coverage.
const padding = 1

// Style selects a variant of the same typeface. Kept deliberately small:
// the atlas key must stay cheap to hash.
type Style uint8

const (
	Regular Style = iota
	Bold
	numStyles
)

// Glyph records where a rasterised glyph lives and how to place its quad
// relative to the cell origin (the top-left of the cell box).
type Glyph struct {
	Page   int             // index into the atlas pages
	Rect   image.Rectangle // pixel region within that page
	Offset image.Point     // Rect's top-left, relative to the cell origin
	Empty  bool            // no pixels (space, unmapped rune); draw nothing
}

type key struct {
	r     rune
	style Style
}

// Metrics describes the fixed cell box every glyph is drawn into.
type Metrics struct {
	CellW  int // advance width of the monospace face, in pixels
	CellH  int // line height, in pixels
	Ascent int // baseline offset from the top of the cell, in pixels
}

// Atlas rasterises and caches glyphs for one font size.
//
// An Atlas is not safe for concurrent use: ebiten drives drawing from a
// single goroutine and the atlas sits on that path.
type Atlas struct {
	faces   [numStyles]font.Face
	metrics Metrics

	// sizePt and dpi are kept so fallback faces can be built at the same
	// size as the primary one.
	sizePt, dpi float64

	// fallbacks are system fonts consulted for runes the primary font
	// has no glyph for: CJK, heavy box-drawing, braille. They are found
	// and loaded lazily, on the first miss, because walking the font
	// directories is not worth doing for a session that never needs one.
	fallbacks     []font.Face
	fallbackPaths []string
	fallbackDone  bool

	pages []*ebiten.Image
	cache map[key]Glyph

	// Shelf-packing cursor into the last page.
	shelfX, shelfY, shelfH int

	// scratch backs the RGBA mask a glyph is rasterised into before
	// upload, so steady-state drawing does not allocate per glyph.
	scratch []byte
}

// NewAtlas builds an atlas from TrueType/OpenType bytes for the regular
// and bold weights at the given size. Pass nil for boldTTF to reuse the
// regular face for bold text.
func NewAtlas(regularTTF, boldTTF []byte, sizePt, dpi float64) (*Atlas, error) {
	a := &Atlas{cache: make(map[key]Glyph, 512), sizePt: sizePt, dpi: dpi}

	mkFace := func(b []byte) (font.Face, error) {
		f, err := sfnt.Parse(b)
		if err != nil {
			return nil, fmt.Errorf("parse font: %w", err)
		}
		return opentype.NewFace(f, &opentype.FaceOptions{
			Size: sizePt,
			DPI:  dpi,
			// Full hinting keeps stems on the pixel grid, which is what
			// makes small monospace text crisp rather than smeared.
			Hinting: font.HintingFull,
		})
	}

	var err error
	if a.faces[Regular], err = mkFace(regularTTF); err != nil {
		return nil, err
	}
	if boldTTF == nil {
		a.faces[Bold] = a.faces[Regular]
	} else if a.faces[Bold], err = mkFace(boldTTF); err != nil {
		return nil, err
	}

	// Derive the cell box from the regular face. A monospace face gives
	// every rune the same advance, so measuring 'M' is enough.
	adv, ok := a.faces[Regular].GlyphAdvance('M')
	if !ok {
		return nil, fmt.Errorf("font has no 'M' glyph; is it monospace?")
	}
	fm := a.faces[Regular].Metrics()
	a.metrics = Metrics{
		CellW:  ceil26_6(adv),
		CellH:  ceil26_6(fm.Ascent + fm.Descent),
		Ascent: ceil26_6(fm.Ascent),
	}
	if a.metrics.CellW <= 0 || a.metrics.CellH <= 0 {
		return nil, fmt.Errorf("degenerate cell box %dx%d",
			a.metrics.CellW, a.metrics.CellH)
	}

	a.newPage()
	return a, nil
}

// Metrics returns the cell box this atlas rasterises into.
func (a *Atlas) Metrics() Metrics { return a.metrics }

// Page returns the texture for page i, for use as a DrawTriangles source.
func (a *Atlas) Page(i int) *ebiten.Image { return a.pages[i] }

// Pages returns how many texture pages are currently allocated.
func (a *Atlas) Pages() int { return len(a.pages) }

// Cached returns how many distinct glyphs have been rasterised.
func (a *Atlas) Cached() int { return len(a.cache) }

// Get returns the glyph for r in the given style, rasterising and
// packing it on first use.
func (a *Atlas) Get(r rune, style Style) Glyph {
	if style >= numStyles {
		style = Regular
	}
	k := key{r: r, style: style}
	if g, ok := a.cache[k]; ok {
		return g
	}
	g := a.rasterise(r, style)
	a.cache[k] = g
	return g
}

func (a *Atlas) rasterise(r rune, style Style) Glyph {
	// Box-drawing and block characters are drawn to the exact cell size
	// rather than taken from a font, so adjacent cells join up.
	if drawnRune(r) {
		return a.rasteriseDrawn(r)
	}
	face := a.faces[style]

	bounds, _, ok := face.GlyphBounds(r)
	if !ok {
		// The primary font has no glyph. Ask the system fonts before
		// giving up, so CJK and the heavier box-drawing characters draw
		// instead of vanishing.
		if fb := a.fallbackFor(r); fb != nil {
			face = fb
			bounds, _, ok = face.GlyphBounds(r)
		}
	}
	if !ok {
		// Genuinely unmapped. Cache the miss so we do not retry it every
		// frame.
		return Glyph{Empty: true}
	}

	// Convert the 26.6 fixed-point glyph box to whole pixels, rounding
	// outwards so no coverage is clipped.
	minX, minY := floor26_6(bounds.Min.X), floor26_6(bounds.Min.Y)
	maxX, maxY := ceil26_6(bounds.Max.X), ceil26_6(bounds.Max.Y)
	w, h := maxX-minX, maxY-minY
	if w <= 0 || h <= 0 {
		return Glyph{Empty: true} // space and friends
	}

	// Rasterise a white mask: colour is applied per-vertex at draw time,
	// so one cached glyph serves every foreground colour.
	need := 4 * w * h
	if cap(a.scratch) < need {
		a.scratch = make([]byte, need)
	}
	buf := a.scratch[:need]
	clear(buf)
	mask := &image.RGBA{Pix: buf, Stride: 4 * w, Rect: image.Rect(0, 0, w, h)}

	d := font.Drawer{
		Dst:  mask,
		Src:  image.NewUniform(color.White),
		Face: face,
		// Place the pen so the glyph's own bounding box lands at the
		// mask origin.
		Dot: fixed.Point26_6{X: -bounds.Min.X, Y: -bounds.Min.Y},
	}
	d.DrawString(string(r))

	page, rect := a.alloc(w, h)
	a.pages[page].SubImage(rect).(*ebiten.Image).WritePixels(mask.Pix)

	return Glyph{
		Page: page,
		Rect: rect,
		// GlyphBounds is relative to the baseline with Y growing down, so
		// the mask's top-left sits Ascent+minY below the cell origin.
		Offset: image.Pt(minX, a.metrics.Ascent+minY),
	}
}

// rasteriseDrawn renders a procedurally drawn glyph, which always fills
// the whole cell and so needs no glyph metrics.
func (a *Atlas) rasteriseDrawn(r rune) Glyph {
	rect := cellRect(a.metrics)
	w, h := rect.Dx(), rect.Dy()
	need := 4 * w * h
	if cap(a.scratch) < need {
		a.scratch = make([]byte, need)
	}
	buf := a.scratch[:need]
	clear(buf)
	if !drawCellGlyph(r, w, h, buf) {
		return Glyph{Empty: true}
	}
	page, dst := a.alloc(w, h)
	a.pages[page].SubImage(dst).(*ebiten.Image).WritePixels(buf)
	return Glyph{Page: page, Rect: dst}
}

// alloc reserves a w by h region, starting a new shelf or a new page
// when the current one is full. It returns the page index and the region.
func (a *Atlas) alloc(w, h int) (int, image.Rectangle) {
	// A glyph larger than a whole page cannot be packed. Clamp rather
	// than panic: an oversized glyph renders cropped, which is a visual
	// bug, not a crash.
	w, h = min(w, pageSize), min(h, pageSize)

	if a.shelfX+w+padding > pageSize {
		a.shelfX = 0
		a.shelfY += a.shelfH + padding
		a.shelfH = 0
	}
	if a.shelfY+h+padding > pageSize {
		a.newPage()
	}
	r := image.Rect(a.shelfX, a.shelfY, a.shelfX+w, a.shelfY+h)
	a.shelfX += w + padding
	a.shelfH = max(a.shelfH, h)
	return len(a.pages) - 1, r
}

func (a *Atlas) newPage() {
	a.pages = append(a.pages, ebiten.NewImage(pageSize, pageSize))
	a.shelfX, a.shelfY, a.shelfH = 0, 0, 0
}

// fallbackFor returns the first system font that can draw r, loading
// candidates on demand. It returns nil when none can.
func (a *Atlas) fallbackFor(r rune) font.Face {
	for _, f := range a.fallbacks {
		if _, _, ok := f.GlyphBounds(r); ok {
			return f
		}
	}
	for !a.fallbackDone {
		f := a.loadNextFallback()
		if f == nil {
			break
		}
		if _, _, ok := f.GlyphBounds(r); ok {
			return f
		}
	}
	return nil
}

// loadNextFallback loads one more candidate font, or returns nil when
// the list is exhausted.
func (a *Atlas) loadNextFallback() font.Face {
	if a.fallbackPaths == nil && !a.fallbackDone {
		a.fallbackPaths = findFallbackFiles()
		if len(a.fallbackPaths) == 0 {
			a.fallbackDone = true
			return nil
		}
	}
	for len(a.fallbackPaths) > 0 {
		path := a.fallbackPaths[0]
		a.fallbackPaths = a.fallbackPaths[1:]
		f, err := loadFace(path, a.sizePt, a.dpi)
		if err != nil {
			// A font that will not parse is not worth reporting; there
			// are other candidates and the glyph may not be needed.
			continue
		}
		a.fallbacks = append(a.fallbacks, f)
		if len(a.fallbackPaths) == 0 {
			a.fallbackDone = true
		}
		return f
	}
	a.fallbackDone = true
	return nil
}

func ceil26_6(v fixed.Int26_6) int  { return int((v + 63) >> 6) }
func floor26_6(v fixed.Int26_6) int { return int(v >> 6) }
