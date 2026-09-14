package render

import (
	"fmt"
	"image"
	"image/color"
	"math"

	"github.com/hajimehoshi/ebiten/v2"
)

// blurTaps is how far the blur reaches from a pixel, in taps each side.
// The kernel below is nine wide, so four.
const blurTaps = 4

// Frost asks for a frosted-glass background behind part of a layer.
//
// What is already on screen under Rect is blurred, lifted, tinted and
// rounded off, and the layer's own text is then drawn over it. It is a
// property of the pixels, not of the grid, so nothing in the widget
// toolkit knows about it.
//
// The zero Frost draws nothing: Rect is empty.
type Frost struct {
	// Rect is the part of the layer to frost, in pixels from the layer's
	// own top-left corner. A dialog is usually much smaller than the
	// layer it is drawn on, so this is not the whole layer.
	Rect image.Rectangle

	// Radius is how far the blur reaches, in pixels.
	Radius float32

	// Corner rounds off the corners, in pixels.
	Corner float32

	// Tint is painted over the blurred background, its alpha saying how
	// much of it to use.
	Tint color.RGBA

	// Saturation lifts the colour of what shows through, because a blur
	// pulls everything towards grey. 1 leaves it alone.
	Saturation float32

	// Grain is how much noise is mixed in, 0 to 1. A large flat panel
	// without it bands into visible steps.
	Grain float32

	// Edge lights the rim, 0 to 1. It is what reads as an edge without a
	// drawn border.
	Edge float32
}

// blurSource is one pass of a separable Gaussian blur.
//
// Two passes of nine taps cost far less than one pass of eighty-one and
// look the same, which is the whole reason to separate them.
const blurSource = `//kage:unit pixels

package main

// Direction is (1,0) for the horizontal pass and (0,1) for the vertical.
var Direction vec2

// Step is how far apart the taps sit, in pixels.
var Step float

// tap reads one sample, held inside the source region. Left to run off
// the edge it would read nothing and darken the rim.
func tap(p vec2) vec4 {
	origin := imageSrc0Origin()
	size := imageSrc0Size()
	return imageSrc0At(clamp(p, origin, origin+size-vec2(1)))
}

func Fragment(dstPos vec4, srcPos vec2, color vec4) vec4 {
	// The binomial row for n=8, normalised. Close enough to a Gaussian
	// at this width and it costs no arithmetic at runtime.
	d := Direction * Step
	sum := tap(srcPos) * 0.2270270270
	sum += (tap(srcPos+d) + tap(srcPos-d)) * 0.1945945946
	sum += (tap(srcPos+d*2.0) + tap(srcPos-d*2.0)) * 0.1216216216
	sum += (tap(srcPos+d*3.0) + tap(srcPos-d*3.0)) * 0.0540540541
	sum += (tap(srcPos+d*4.0) + tap(srcPos-d*4.0)) * 0.0162162162
	return sum
}
`

// frostSource turns the blurred backdrop into frosted glass: a rounded
// panel, its colour lifted and tinted, with noise through it and a lit
// rim.
const frostSource = `//kage:unit pixels

package main

// Origin and Size are the panel in destination pixels. The destination
// is the whole window, so its own size says nothing about the panel.
var Origin vec2
var Size vec2

var Corner float
var Tint vec4
var Saturation float
var Grain float
var Edge float

// roundedBox returns the signed distance from p to a box of the given
// half-size with rounded corners, negative inside it.
func roundedBox(p vec2, half vec2, r float) float {
	d := abs(p) - half + vec2(r)
	return length(max(d, vec2(0))) + min(max(d.x, d.y), 0.0) - r
}

func Fragment(dstPos vec4, srcPos vec2, color vec4) vec4 {
	half := Size * 0.5
	dist := roundedBox(dstPos.xy-Origin-half, half, Corner)

	// Softened over the last pixel, or the corners come out as stairs.
	alpha := 1.0 - smoothstep(-1.0, 0.0, dist)
	if alpha <= 0.0 {
		return vec4(0)
	}

	rgb := imageSrc0At(srcPos).rgb

	// A blur averages colour away, so it is put back before anything
	// else is done to it.
	grey := dot(rgb, vec3(0.299, 0.587, 0.114))
	rgb = mix(vec3(grey), rgb, Saturation)
	rgb = mix(rgb, Tint.rgb, Tint.a)

	// Enough noise to break up a flat panel without being seen as noise.
	n := fract(sin(dot(dstPos.xy, vec2(12.9898, 78.233))) * 43758.5453)
	rgb += vec3((n - 0.5) * Grain)

	// The rim catches the light, which is what says "edge" without a
	// border drawn round it.
	rgb += vec3(Edge * smoothstep(-2.0, 0.0, dist))

	rgb = clamp(rgb, vec3(0), vec3(1))
	// Premultiplied, which is what ebiten blends.
	return vec4(rgb*alpha, alpha)
}
`

// shaders holds the compiled programs, built on first use because a
// compositor with nothing frosted should not pay for them.
type shaders struct {
	blur  *ebiten.Shader
	frost *ebiten.Shader
}

// compile builds both programs. It reports the first failure rather than
// leaving a nil shader for the draw path to find.
func (s *shaders) compile() error {
	if s.blur != nil && s.frost != nil {
		return nil
	}
	blur, err := ebiten.NewShader([]byte(blurSource))
	if err != nil {
		return fmt.Errorf("compile the blur shader: %w", err)
	}
	frost, err := ebiten.NewShader([]byte(frostSource))
	if err != nil {
		blur.Deallocate()
		return fmt.Errorf("compile the frost shader: %w", err)
	}
	s.blur, s.frost = blur, frost
	return nil
}

// scratch is a pair of offscreen images the blur bounces between.
type scratch struct {
	a, b *ebiten.Image
	w, h int
}

// ensure sizes the pair, reallocating when more room is needed.
//
// It grows and never shrinks. A dialog changes size as it is typed into,
// and throwing the images away each time would allocate once a keystroke.
func (s *scratch) ensure(w, h int) {
	if w <= s.w && h <= s.h && s.a != nil {
		return
	}
	s.w, s.h = max(w, s.w), max(h, s.h)
	if s.a != nil {
		s.a.Deallocate()
		s.b.Deallocate()
	}
	s.a = ebiten.NewImage(max(s.w, 1), max(s.h, 1))
	s.b = ebiten.NewImage(max(s.w, 1), max(s.h, 1))
}

// drawFrost paints the frosted panel for one layer onto the screen.
//
// The screen already holds every layer below this one, because layers
// are blitted from the bottom up, so it is the backdrop. That is also
// why the screen is cleared and rebuilt whenever anything is frosted:
// reading a screen that still held the last frame's panel would blur the
// panel into itself, a little more on every frame.
func (c *Compositor) drawFrost(screen *ebiten.Image, l *Layer) {
	f := l.Frost
	panel := f.Rect.Add(image.Pt(l.X, l.Y)).Intersect(screen.Bounds())
	if panel.Empty() {
		return
	}
	if err := c.shaders.compile(); err != nil {
		// Once, not once a frame: a shader that will not compile will not
		// compile on the next frame either. The dialog still draws, just
		// without the glass behind it.
		if !c.shaderFailed {
			c.shaderFailed = true
			c.onError(err)
		}
		return
	}

	// Sampled wider than the panel so the blur has real pixels to reach
	// into, rather than fading the rim into nothing.
	pad := int(math.Ceil(float64(f.Radius))) * blurTaps
	src := panel.Inset(-pad).Intersect(screen.Bounds())
	c.scratch.ensure(src.Dx(), src.Dy())

	// The backdrop, moved to the scratch image's own origin.
	into := c.scratch.a.SubImage(image.Rect(0, 0, src.Dx(), src.Dy())).(*ebiten.Image)
	into.Clear()
	op := &ebiten.DrawImageOptions{}
	op.GeoM.Translate(-float64(src.Min.X), -float64(src.Min.Y))
	into.DrawImage(screen.SubImage(src).(*ebiten.Image), op)

	step := max(float64(f.Radius)/blurTaps, 1)
	c.blurPass(c.scratch.b, into, [2]float32{1, 0}, step, src)
	blurred := c.scratch.b.SubImage(image.Rect(0, 0, src.Dx(), src.Dy())).(*ebiten.Image)
	c.blurPass(c.scratch.a, blurred, [2]float32{0, 1}, step, src)

	// The part of the scratch that lines up with the panel.
	inner := panel.Sub(src.Min)
	backdrop := c.scratch.a.SubImage(inner).(*ebiten.Image)

	sop := &ebiten.DrawRectShaderOptions{}
	sop.GeoM.Translate(float64(panel.Min.X), float64(panel.Min.Y))
	sop.Images[0] = backdrop
	sop.Uniforms = map[string]any{
		"Origin":     []float32{float32(panel.Min.X), float32(panel.Min.Y)},
		"Size":       []float32{float32(panel.Dx()), float32(panel.Dy())},
		"Corner":     f.Corner,
		"Tint":       rgbaToFloats(f.Tint),
		"Saturation": f.Saturation,
		"Grain":      f.Grain,
		"Edge":       f.Edge,
	}
	screen.DrawRectShader(panel.Dx(), panel.Dy(), c.shaders.frost, sop)
	c.stats.Frosted++
}

// blurPass runs one direction of the blur from src into dst, both in the
// scratch pair's coordinates.
func (c *Compositor) blurPass(dst, src *ebiten.Image, dir [2]float32, step float64, area image.Rectangle) {
	into := dst.SubImage(image.Rect(0, 0, area.Dx(), area.Dy())).(*ebiten.Image)
	into.Clear()
	op := &ebiten.DrawRectShaderOptions{}
	op.Images[0] = src
	op.Uniforms = map[string]any{
		"Direction": []float32{dir[0], dir[1]},
		"Step":      float32(step),
	}
	into.DrawRectShader(area.Dx(), area.Dy(), c.shaders.blur, op)
}

// rgbaToFloats turns a colour into the 0-to-1 vector a shader wants.
func rgbaToFloats(c color.RGBA) []float32 {
	return []float32{
		float32(c.R) / 255,
		float32(c.G) / 255,
		float32(c.B) / 255,
		float32(c.A) / 255,
	}
}

// anyFrosted reports whether a layer the viewer can see asks for glass.
//
// A frosted layer reads the screen as its backdrop, so the screen has to
// be rebuilt from the bottom rather than drawn over.
func (c *Compositor) anyFrosted() bool {
	for _, l := range c.layers {
		if !l.Hidden && l.Grid != nil && l.Frost != nil && !l.Frost.Rect.Empty() {
			return true
		}
	}
	return false
}
