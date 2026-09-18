package glyph

import "image"

// slantRise is how far the top of a glyph leans right, as a fraction of
// its height. About eleven degrees, which is the slope a type designer
// draws an oblique at.
const slantRise = 0.2

// emboldenBy is how many pixels a smeared glyph is widened by. One is
// enough at the sizes a terminal is read at, and two closes the counters
// of an "e" and an "a".
const emboldenBy = 1

// missing returns the parts of a style the face standing in for it does
// not have, which is what has to be faked.
func missing(want, have Style) Style { return want &^ have }

// embolden smears a mask to the right, which is how a family with no
// bold face of its own gets one. It is what a printer driver did before
// fonts came in four weights, and it reads as bold at a terminal's size.
//
// The mask grows by emboldenBy, and its place in the cell does not move:
// a glyph thickens to the right rather than shifting.
func embolden(src *image.RGBA) *image.RGBA {
	w, h := src.Rect.Dx(), src.Rect.Dy()
	out := image.NewRGBA(image.Rect(0, 0, w+emboldenBy, h))
	for y := range h {
		from := src.Pix[y*src.Stride : y*src.Stride+w*4]
		row := out.Pix[y*out.Stride:]
		for at := 0; at <= emboldenBy; at++ {
			copyOver(row[at*4:], from)
		}
	}
	return out
}

// slant shears a mask about its baseline, leaning what is above the
// baseline to the right and what hangs below it to the left, which is
// how a family with no italic face gets one.
//
// baseline is the row the glyph sits on, counted from the top of the
// mask. It returns the new mask and how far the mask's left edge moved,
// which the caller adds to the glyph's offset.
func slant(src *image.RGBA, baseline int) (*image.RGBA, int) {
	w, h := src.Rect.Dx(), src.Rect.Dy()
	// How far each end travels. The top row leans furthest right, the
	// bottom row furthest left, and the baseline does not move.
	right := shiftAt(0, baseline)
	left := shiftAt(h-1, baseline)
	grewLeft := max(-left, 0)
	out := image.NewRGBA(image.Rect(0, 0, w+grewLeft+max(right, 0), h))
	for y := range h {
		at := shiftAt(y, baseline) + grewLeft
		if at < 0 {
			at = 0
		}
		from := src.Pix[y*src.Stride : y*src.Stride+w*4]
		copyOver(out.Pix[y*out.Stride+at*4:], from)
	}
	return out, -grewLeft
}

// shiftAt is how far the row at y moves, in pixels, for a shear about
// the baseline.
func shiftAt(y, baseline int) int {
	rise := float64(baseline-y) * slantRise
	if rise < 0 {
		return -int(-rise + 0.5)
	}
	return int(rise + 0.5)
}

// copyOver writes a row of coverage over another, keeping whichever
// pixel covers more. The mask is white with its coverage in every
// channel, so taking the larger byte is taking the larger coverage.
func copyOver(dst, src []byte) {
	n := min(len(dst), len(src))
	for i := range n {
		if src[i] > dst[i] {
			dst[i] = src[i]
		}
	}
}
