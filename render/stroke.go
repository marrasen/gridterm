package render

import (
	"image"
	"image/color"
	"math"

	"github.com/hajimehoshi/ebiten/v2"
)

// Stroke is a rule drawn round a box on a layer, in pixels.
//
// It is a shape rather than a character, so it is drawn per pixel rather
// than out of cells: the corners are round, the width is whatever is
// asked for, and neither is held to the grid the layer is measured in.
//
// The zero Stroke draws nothing: Rect is empty.
type Stroke struct {
	// Rect is the box the rule runs round, in pixels from the layer's
	// own top-left corner.
	Rect image.Rectangle

	// Corner rounds the box off, in pixels.
	Corner float32

	// Width is how thick the rule is, in pixels. It straddles the box's
	// own edge, half inside and half out.
	Width float32

	// Colour is what the rule is drawn in. Its alpha is coverage, and
	// its components are read as written rather than premultiplied.
	Colour color.RGBA
}

// Empty reports whether a stroke draws nothing.
//
// The width is read as "not above zero", so a NaN counts as nothing
// rather than reaching the shader and costing a draw call on every
// frame for ever, because a NaN never compares equal to itself.
func (s *Stroke) Empty() bool {
	return s == nil || s.Rect.Empty() || s.Colour.A == 0 ||
		!(s.Width > 0) || math.IsNaN(float64(s.Corner))
}

// strokeSource paints a rule along the edge of a rounded box.
//
// The distance field is the one the frosted panel uses, so a rule round
// a dialog and the dialog's own corner are the same shape.
const strokeSource = `//kage:unit pixels

package main

// Origin and Size are the box in destination pixels, Half its half-size.
var Origin vec2
var Size vec2

var Corner float
var Width float
var Colour vec4

// roundedBox returns the signed distance from p to a box of the given
// half-size with rounded corners, negative inside it.
func roundedBox(p vec2, half vec2, r float) float {
	d := abs(p) - half + vec2(r)
	return length(max(d, vec2(0))) + min(max(d.x, d.y), 0.0) - r
}

func Fragment(dstPos vec4, srcPos vec2, color vec4) vec4 {
	half := Size * 0.5
	dist := roundedBox(dstPos.xy-Origin-half, half, Corner)

	// The rule straddles the edge, so the band is the distance from it.
	// Softened over the last pixel, or the corners come out as stairs.
	edge := abs(dist) - Width*0.5
	alpha := 1.0 - smoothstep(-0.5, 0.5, edge)
	if alpha <= 0.0 {
		return vec4(0)
	}
	a := Colour.a * alpha
	return vec4(Colour.rgb*a, a)
}
`

// drawStroke paints one layer's rule over what is already on screen.
func (c *Compositor) drawStroke(screen *ebiten.Image, l *Layer, s *Stroke) {
	if c.shaderFailed {
		return
	}
	box := s.Rect.Add(image.Pt(l.X, l.Y))
	if err := c.shaders.compile(); err != nil {
		// Once, not once a frame: a shader that will not compile will
		// not compile on the next frame either. What the rule marks is
		// still drawn, just unmarked.
		c.shaderFailed = true
		c.onError(err)
		return
	}

	// The band reaches half the width either side of the edge, and the
	// softening another half pixel, so the quad is grown to hold it.
	grow := int(s.Width/2) + 2
	quad := box.Inset(-grow).Intersect(screen.Bounds())
	if quad.Empty() {
		return
	}
	op := &ebiten.DrawRectShaderOptions{}
	op.GeoM.Translate(float64(quad.Min.X), float64(quad.Min.Y))
	op.Uniforms = strokeUniforms(box, s)
	screen.DrawRectShader(quad.Dx(), quad.Dy(), c.shaders.stroke, op)
	c.stats.Strokes++
}

// strokeUniforms are the shader's inputs for one rule round a box given
// in screen pixels.
func strokeUniforms(box image.Rectangle, s *Stroke) map[string]any {
	return map[string]any{
		"Origin": []float32{float32(box.Min.X), float32(box.Min.Y)},
		"Size":   []float32{float32(box.Dx()), float32(box.Dy())},
		// Held to the box, because the rounded-box distance field is
		// only a distance while the radius fits inside it.
		"Corner": frostCorner(s.Corner, box),
		"Width":  s.Width,
		"Colour": []float32{
			float32(s.Colour.R) / 0xff, float32(s.Colour.G) / 0xff,
			float32(s.Colour.B) / 0xff, float32(s.Colour.A) / 0xff,
		},
	}
}
