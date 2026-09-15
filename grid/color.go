package grid

import "image/color"

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
