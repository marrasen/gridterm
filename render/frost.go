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
	//
	// Its components are read as they are written rather than as
	// alpha-premultiplied, which is what color.RGBA usually means. The
	// alpha here is a mixing weight, not coverage: the colour is what it
	// says, and the alpha says how much of it to lay on.
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

	// Shadow is what the panel casts on what is behind it, its alpha
	// saying how dark. One with no alpha casts none.
	//
	// Read as it is written rather than alpha-premultiplied, the way
	// Tint is.
	Shadow color.RGBA

	// Drop is how far the shadow falls, in pixels, right and down.
	Drop [2]float32

	// Spread is how far its edge is softened over, in pixels. Zero
	// leaves a hard edge, which still follows the rounded corners.
	Spread float32
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

// shadowSource draws the shadow a panel casts: the same rounded box,
// moved and softened. It is a pass of its own, where the frost has no
// backdrop to sample.
const shadowSource = `//kage:unit pixels

package main

// Origin and Size are the panel in destination pixels, the same two the
// frost is given.
var Origin vec2
var Size vec2

var Corner float

// Drop is how far the shadow falls and Spread how far its edge is
// softened over.
var Drop vec2
var Spread float

// Colour is what it is drawn in, its alpha saying how dark.
var Colour vec4

// roundedBox returns the signed distance from p to a box of the given
// half-size with rounded corners, negative inside it.
func roundedBox(p vec2, half vec2, r float) float {
	d := abs(p) - half + vec2(r)
	return length(max(d, vec2(0))) + min(max(d.x, d.y), 0.0) - r
}

func Fragment(dstPos vec4, srcPos vec2, color vec4) vec4 {
	half := Size * 0.5
	dist := roundedBox(dstPos.xy-Origin-Drop-half, half, Corner)

	// Softened over the spread, and over the last pixel at the least, or
	// the corners come out as stairs.
	soft := max(Spread, 1.0)
	alpha := Colour.a * (1.0 - smoothstep(-soft, soft, dist))
	if alpha <= 0.0 {
		return vec4(0)
	}
	// Premultiplied, which is what ebiten blends.
	return vec4(Colour.rgb*alpha, alpha)
}
`

// shaderArgs is one shader's uniforms and the options they are handed
// over in, kept between frames and written through.
//
// Ebiten takes uniforms as a map of any and copies them out while the
// draw call runs, so the same map can be handed over every frame. Built
// afresh each time, the map and the boxing of each value in it cost an
// allocation a frame for as long as a dialog is open.
type shaderArgs struct {
	op *ebiten.DrawRectShaderOptions

	// at is each uniform's own slice, which is what the map holds. A
	// scalar is a slice of one: ebiten takes a slice wherever the count
	// matches.
	at map[string][]float32
}

// newShaderArgs builds the options and one slice per uniform, of the
// widths given.
func newShaderArgs(widths map[string]int) shaderArgs {
	a := shaderArgs{
		op: &ebiten.DrawRectShaderOptions{Uniforms: map[string]any{}},
		at: make(map[string][]float32, len(widths)),
	}
	for name, w := range widths {
		v := make([]float32, w)
		a.at[name] = v
		a.op.Uniforms[name] = v
	}
	return a
}

// set writes a uniform's values.
func (a shaderArgs) set(name string, v ...float32) { copy(a.at[name], v) }

// at is where the shader draws, which is the one option that is not a
// uniform. The geometry is reset first, because it is kept between
// frames along with everything else.
func (a shaderArgs) placeAt(x, y int) {
	a.op.GeoM.Reset()
	a.op.GeoM.Translate(float64(x), float64(y))
}

// shaders holds the compiled programs, built on first use because a
// compositor with no glass and no rules should not pay for them.
type shaders struct {
	blur   *ebiten.Shader
	frost  *ebiten.Shader
	stroke *ebiten.Shader
	shadow *ebiten.Shader
}

// compile builds the programs. It reports the first failure rather than
// leaving a nil shader for the draw path to find.
func (s *shaders) compile() error {
	if s.blur != nil && s.frost != nil && s.stroke != nil && s.shadow != nil {
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
	stroke, err := ebiten.NewShader([]byte(strokeSource))
	if err != nil {
		blur.Deallocate()
		frost.Deallocate()
		return fmt.Errorf("compile the stroke shader: %w", err)
	}
	drop, err := ebiten.NewShader([]byte(shadowSource))
	if err != nil {
		blur.Deallocate()
		frost.Deallocate()
		stroke.Deallocate()
		return fmt.Errorf("compile the shadow shader: %w", err)
	}
	s.blur, s.frost, s.stroke, s.shadow = blur, frost, stroke, drop
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
	if c.shaderFailed {
		return
	}
	f := l.Frost
	panel := f.Rect.Add(image.Pt(l.X, l.Y)).Intersect(screen.Bounds())
	if panel.Empty() {
		return
	}
	if err := c.shaders.compile(); err != nil {
		// Once, not once a frame: a shader that will not compile will not
		// compile on the next frame either. The dialog still draws, just
		// without the glass behind it.
		c.shaderFailed = true
		c.onError(err)
		return
	}

	src, inner, step := frostRegion(panel, f.Radius, screen.Bounds())
	c.scratch.ensure(src.Dx(), src.Dy())

	// The backdrop, moved so the wanted region sits at the scratch's own
	// origin.
	//
	// The whole canvas goes in and the scratch's bounds do the cropping.
	// Passing a sub-image of the canvas instead draws nothing at all on
	// the Direct3D backend, with no error, which is a long way to chase
	// a black panel.
	c.scratch.a.Clear()
	op := &ebiten.DrawImageOptions{}
	op.GeoM.Translate(-float64(src.Min.X), -float64(src.Min.Y))
	c.scratch.a.DrawImage(screen, op)
	into := c.scratch.a.SubImage(image.Rect(0, 0, src.Dx(), src.Dy())).(*ebiten.Image)

	if step > 0 {
		c.blurPass(c.scratch.b, into, 1, 0, step, src)
		blurred := c.scratch.b.SubImage(image.Rect(0, 0, src.Dx(), src.Dy())).(*ebiten.Image)
		c.blurPass(c.scratch.a, blurred, 0, 1, step, src)
	}

	// After the backdrop was taken off the screen and before the panel
	// goes on, so the glass neither picks up its own shadow nor covers
	// it.
	c.drawPanelShadow(screen, f, panel)

	// The part of the scratch that lines up with the panel.
	backdrop := c.scratch.a.SubImage(inner).(*ebiten.Image)

	a := c.frostArgs()
	a.placeAt(panel.Min.X, panel.Min.Y)
	a.op.Images[0] = backdrop
	a.set("Origin", float32(panel.Min.X), float32(panel.Min.Y))
	a.set("Size", float32(panel.Dx()), float32(panel.Dy()))
	a.set("Corner", frostCorner(f.Corner, panel))
	a.set("Tint", rgbaToFloats(f.Tint)...)
	a.set("Saturation", f.Saturation)
	a.set("Grain", f.Grain)
	a.set("Edge", f.Edge)
	screen.DrawRectShader(panel.Dx(), panel.Dy(), c.shaders.frost, a.op)
	c.stats.Frosted++
}

// drawPanelShadow lays the panel's shadow on the screen, rounded in
// pixels the same way the panel's own corners are.
func (c *Compositor) drawPanelShadow(screen *ebiten.Image, f *Frost, panel image.Rectangle) {
	if f.Shadow.A == 0 {
		return
	}
	box := shadowBox(panel, f.Drop, f.Spread).Intersect(screen.Bounds())
	if box.Empty() {
		return
	}
	a := c.shadowArgs()
	a.placeAt(box.Min.X, box.Min.Y)
	a.set("Origin", float32(panel.Min.X), float32(panel.Min.Y))
	a.set("Size", float32(panel.Dx()), float32(panel.Dy()))
	a.set("Corner", frostCorner(f.Corner, panel))
	a.set("Drop", f.Drop[0], f.Drop[1])
	a.set("Spread", f.Spread)
	a.set("Colour", rgbaToFloats(f.Shadow)...)
	screen.DrawRectShader(box.Dx(), box.Dy(), c.shaders.shadow, a.op)
	c.stats.Shadowed++
}

// shadowBox is where a panel's shadow can reach: the panel moved by the
// drop and grown by the spread, and the panel itself, because a shadow
// that falls up or left still starts behind it.
func shadowBox(panel image.Rectangle, drop [2]float32, spread float32) image.Rectangle {
	reach := int(math.Ceil(float64(max(spread, 1))))
	moved := panel.Add(image.Pt(int(math.Round(float64(drop[0]))), int(math.Round(float64(drop[1])))))
	return moved.Inset(-reach).Union(panel)
}

// frostRegion works out the three rectangles a panel needs: src is the
// region of the screen to blur, inner is where the panel sits inside
// that region once it has been moved to the scratch image's origin, and
// step is how far apart the blur taps sit. A step of zero means no blur
// was asked for.
//
// The region is wider than the panel so that the blur has real pixels to
// reach into. Left to sample its own edge, the panel would fade out at
// the rim instead of showing what is beside it. At the edge of the
// screen there is nothing to widen into, and the taps clamp instead --
// which is why inner is not simply the padding: it is however much of
// the padding there was room for.
func frostRegion(panel image.Rectangle, radius float32, screen image.Rectangle) (src, inner image.Rectangle, step float64) {
	if radius <= 0 {
		return panel, panel.Sub(panel.Min), 0
	}
	// The taps are spread over the radius, but never closer than a pixel
	// apart, so a small radius still reaches a whole number of pixels.
	step = max(float64(radius)/blurTaps, 1)
	pad := int(math.Ceil(step * blurTaps))
	src = panel.Inset(-pad).Intersect(screen)
	return src, panel.Sub(src.Min), step
}

// frostCorner holds a corner radius to what a box can take, for the
// glass and for a rule alike. The rounded-box distance field is only a
// distance while the radius fits inside the box; past that it draws as
// a lens.
func frostCorner(corner float32, box image.Rectangle) float32 {
	limit := float32(min(box.Dx(), box.Dy())) / 2
	return min(max(corner, 0), limit)
}

// blurPass runs one direction of the blur from src into dst, both in the
// scratch pair's coordinates.
func (c *Compositor) blurPass(dst, src *ebiten.Image, dx, dy float32, step float64, area image.Rectangle) {
	into := dst.SubImage(image.Rect(0, 0, area.Dx(), area.Dy())).(*ebiten.Image)
	into.Clear()
	a := c.blurArgs()
	a.placeAt(0, 0)
	a.op.Images[0] = src
	a.set("Direction", dx, dy)
	a.set("Step", float32(step))
	into.DrawRectShader(area.Dx(), area.Dy(), c.shaders.blur, a.op)
}

// blurArgs, frostArgs and shadowArgs are each shader's uniforms, built
// on first use and written through after that.
func (c *Compositor) blurArgs() shaderArgs {
	if c.blur.op == nil {
		c.blur = newShaderArgs(map[string]int{"Direction": 2, "Step": 1})
	}
	return c.blur
}

func (c *Compositor) frostArgs() shaderArgs {
	if c.frost.op == nil {
		c.frost = newShaderArgs(map[string]int{
			"Origin": 2, "Size": 2, "Corner": 1, "Tint": 4,
			"Saturation": 1, "Grain": 1, "Edge": 1,
		})
	}
	return c.frost
}

func (c *Compositor) shadowArgs() shaderArgs {
	if c.shadow.op == nil {
		c.shadow = newShaderArgs(map[string]int{
			"Origin": 2, "Size": 2, "Corner": 1, "Drop": 2, "Spread": 1, "Colour": 4,
		})
	}
	return c.shadow
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
