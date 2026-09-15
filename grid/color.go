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
	part := func(a, b uint8) uint8 { return uint8(int(a) + (int(b)-int(a))*at/of) }
	return color.RGBA{
		R: part(from.R, to.R),
		G: part(from.G, to.G),
		B: part(from.B, to.B),
		A: part(from.A, to.A),
	}
}
