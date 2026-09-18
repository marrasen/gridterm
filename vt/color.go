package vt

import (
	"image/color"

	"github.com/marrasen/gridterm/grid"
)

// Palette is the colour scheme a terminal resolves SGR colours against.
//
// Colours are resolved to RGBA as cells are written rather than stored
// as indices, which keeps the grid a pure display structure. Changing
// the scheme moves what is already on a screen by matching each colour
// against the old scheme.
type Palette struct {
	// ANSI holds the 256-colour palette: 0-7 normal, 8-15 bright,
	// 16-231 the 6x6x6 cube, 232-255 greyscale.
	ANSI [256]color.RGBA

	FG     color.RGBA // default foreground
	BG     color.RGBA // default background
	Cursor color.RGBA

	// Selection is painted behind selected text.
	Selection color.RGBA
}

// DefaultPalette returns a dark scheme with the usual xterm 256-colour
// layout for everything above index 15.
func DefaultPalette() Palette {
	p := Palette{
		FG:        color.RGBA{0xc8, 0xd0, 0xda, 0xff},
		BG:        color.RGBA{0x14, 0x17, 0x1c, 0xff},
		Cursor:    color.RGBA{0xc8, 0xd0, 0xda, 0xff},
		Selection: color.RGBA{0x33, 0x3f, 0x52, 0xff},
	}

	base := [16]color.RGBA{
		{0x1c, 0x20, 0x26, 0xff}, // black
		{0xe0, 0x6c, 0x75, 0xff}, // red
		{0x8f, 0xd4, 0x6a, 0xff}, // green
		{0xe6, 0xb4, 0x50, 0xff}, // yellow
		{0x61, 0xaf, 0xef, 0xff}, // blue
		{0xc6, 0x78, 0xdd, 0xff}, // magenta
		{0x56, 0xb6, 0xc2, 0xff}, // cyan
		{0xab, 0xb2, 0xbf, 0xff}, // white
		{0x5c, 0x63, 0x70, 0xff}, // bright black
		{0xff, 0x8b, 0x94, 0xff},
		{0xa9, 0xe8, 0x8a, 0xff},
		{0xff, 0xd0, 0x74, 0xff},
		{0x84, 0xc5, 0xff, 0xff},
		{0xdb, 0x9a, 0xf0, 0xff},
		{0x76, 0xd4, 0xdf, 0xff},
		{0xff, 0xff, 0xff, 0xff},
	}
	copy(p.ANSI[:16], base[:])

	p.FillUpper()
	return p
}

// index returns palette entry n, or the default foreground if n is out
// of range. SGR parameters come from the far end of a pipe and cannot be
// trusted to be in bounds.
func (p *Palette) index(n int) color.RGBA {
	if n < 0 || n >= len(p.ANSI) {
		return p.FG
	}
	return p.ANSI[n]
}

// Surface is a ground just off the window's own, for a field, a button
// or a panel that has to read as sitting on the window rather than in
// it.
//
// Derived from the two ends of the scheme rather than taken from a
// numbered colour: ANSI[0] is black, which is a shade off the ground in
// a dark scheme and the same as the text in a light one.
func (p *Palette) Surface() color.RGBA { return grid.Blend(p.BG, p.FG, 1, 6) }

// FillUpper fills palette entries 16 to 255 with the xterm layout: a
// 6x6x6 cube and 24 greys. A theme names the first sixteen and leaves
// these, which every terminal agrees on.
func (p *Palette) FillUpper() {
	// The level steps are xterm's, which are not evenly spaced -- 0 then
	// 95 then 40 apart.
	levels := [6]uint8{0, 0x5f, 0x87, 0xaf, 0xd7, 0xff}
	i := 16
	for r := 0; r < 6; r++ {
		for g := 0; g < 6; g++ {
			for b := 0; b < 6; b++ {
				p.ANSI[i] = color.RGBA{levels[r], levels[g], levels[b], 0xff}
				i++
			}
		}
	}
	// 232-255: 24 greys from 0x08 to 0xee in steps of 10.
	for j := 0; j < 24; j++ {
		v := uint8(8 + j*10)
		p.ANSI[232+j] = color.RGBA{v, v, v, 0xff}
	}
}
