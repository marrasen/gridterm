package grid

import (
	"image/color"
	"math"
)

// Blend mixes two colours, at/of the way from the first to the second.
// Every channel is mixed, alpha included.
//
// An of of zero or less is the first colour: a gradient over no distance
// has only one end.
func Blend(from, to color.RGBA, at, of int) color.RGBA {
	if of <= 0 {
		return from
	}
	part := func(a, b uint8) uint8 { return uint8(int(a) + floorDiv((int(b)-int(a))*at, of)) }
	return color.RGBA{
		R: part(from.R, to.R),
		G: part(from.G, to.G),
		B: part(from.B, to.B),
		A: part(from.A, to.A),
	}
}

// floorDiv divides rounding down, where Go's own division rounds towards
// zero. A blend towards a darker colour has a negative numerator, and
// rounding the two ways apart moves such a cell by one.
func floorDiv(n, d int) int {
	q := n / d
	if n%d != 0 && (n < 0) != (d < 0) {
		q--
	}
	return q
}

// Contrast is the WCAG contrast ratio between two colours: 1 for a
// colour against itself, and 21 for black against white.
//
// Text is readable from about 4.5, and a change of ground reads from
// about 1.5.
func Contrast(a, b color.RGBA) float64 {
	high, low := luminance(a), luminance(b)
	if high < low {
		high, low = low, high
	}
	return (high + 0.05) / (low + 0.05)
}

// luminance is the WCAG relative luminance of an sRGB colour, from 0 for
// black to 1 for white.
func luminance(c color.RGBA) float64 {
	channel := func(v uint8) float64 {
		s := float64(v) / 255
		if s <= 0.03928 {
			return s / 12.92
		}
		return math.Pow((s+0.055)/1.055, 2.4)
	}
	return 0.2126*channel(c.R) + 0.7152*channel(c.G) + 0.0722*channel(c.B)
}
