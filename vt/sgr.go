package vt

import (
	"image/color"

	"github.com/marrasen/gridterm/grid"
)

// applySGR updates the pen from one SGR sequence.
//
// Parameters arrive as a slice of sub-parameter lists because 38 and 48
// accept colours in two spellings: the colon form `38:2::r:g:b`, where
// the parts are sub-parameters of one parameter, and the older semicolon
// form `38;2;r;g;b`, where they are separate parameters. Both are in the
// wild, so both are handled.
func (t *Terminal) applySGR(params [][]uint16) {
	if len(params) == 0 {
		t.scr.SetPen(t.defaultPen())
		return
	}
	pen := t.scr.Pen()
	// The screen's own palette rather than a copy of it: it is a
	// kilobyte, and a coloured listing sends one of these per word.
	pal := &t.scr.palette

	for i := 0; i < len(params); i++ {
		sub := params[i]
		if len(sub) == 0 {
			continue
		}
		n := int(sub[0])

		switch n {
		case 38, 48, 58:
			var col color.RGBA
			var ok bool
			if len(sub) > 1 {
				col, ok = colorFromSub(sub[1:], pal)
			} else {
				var used int
				col, ok, used = colorFromParams(params[i+1:], pal)
				i += used
			}
			if !ok {
				continue
			}
			switch n {
			case 38:
				pen.FG = col
			case 48:
				pen.BG = col
			}
			// 58 sets the underline colour, which the renderer does not
			// draw yet; parsing it keeps the rest of the sequence from
			// being misread as attributes.
			continue
		}

		switch {
		case n == 4:
			// The colon form carries a style: 4:0 is off, 4:1 single,
			// 4:3 curly. Only on and off are drawn, but 4:0 must not be
			// read as plain "underline on".
			if len(sub) > 1 && sub[1] == 0 {
				pen.Attr &^= grid.AttrUnderline
			} else {
				pen.Attr |= grid.AttrUnderline
			}
		case n == 21:
			// Double underline. Drawn as a single one for now, but it is
			// not "bold off", which is what ECMA-48 says and almost no
			// terminal implements.
			pen.Attr |= grid.AttrUnderline
		case n == 0:
			pen = t.defaultPen()
		case n == 1:
			pen.Attr |= grid.AttrBold
		case n == 2:
			pen.Attr |= grid.AttrDim
		case n == 3:
			pen.Attr |= grid.AttrItalic
		case n == 5 || n == 6:
			pen.Attr |= grid.AttrBlink
		case n == 7:
			pen.Attr |= grid.AttrReverse
		case n == 8:
			pen.Attr |= grid.AttrHidden
		case n == 9:
			pen.Attr |= grid.AttrStrike
		case n == 22:
			pen.Attr &^= grid.AttrBold | grid.AttrDim
		case n == 23:
			pen.Attr &^= grid.AttrItalic
		case n == 24:
			pen.Attr &^= grid.AttrUnderline
		case n == 25:
			pen.Attr &^= grid.AttrBlink
		case n == 27:
			pen.Attr &^= grid.AttrReverse
		case n == 28:
			pen.Attr &^= grid.AttrHidden
		case n == 29:
			pen.Attr &^= grid.AttrStrike
		case n >= 30 && n <= 37:
			pen.FG = pal.index(n - 30)
		case n == 39:
			pen.FG = pal.FG
		case n >= 40 && n <= 47:
			pen.BG = pal.index(n - 40)
		case n == 49:
			pen.BG = pal.BG
		case n >= 90 && n <= 97:
			pen.FG = pal.index(n - 90 + 8)
		case n >= 100 && n <= 107:
			pen.BG = pal.index(n - 100 + 8)
		}
	}
	t.scr.SetPen(pen)
}

// defaultPen is SGR 0: default colours, no attributes.
func (t *Terminal) defaultPen() grid.Cell {
	pal := &t.scr.palette
	return grid.Cell{Rune: ' ', FG: pal.FG, BG: pal.BG, Width: 1}
}

// colorFromSub reads a colour from the sub-parameters of 38/48, the
// colon form. For 2 (direct colour) an optional colour-space id may come
// first, which is why r/g/b are taken from the end rather than a fixed
// offset.
func colorFromSub(sub []uint16, pal *Palette) (color.RGBA, bool) {
	if len(sub) == 0 {
		return color.RGBA{}, false
	}
	switch sub[0] {
	case 5:
		if len(sub) < 2 {
			return color.RGBA{}, false
		}
		return pal.index(int(sub[1])), true
	case 2:
		if len(sub) < 4 {
			return color.RGBA{}, false
		}
		rgb := sub[len(sub)-3:]
		return color.RGBA{clamp8(rgb[0]), clamp8(rgb[1]), clamp8(rgb[2]), 0xff}, true
	}
	return color.RGBA{}, false
}

// colorFromParams reads a colour from following parameters, the
// semicolon form. It returns how many parameters it consumed so the
// caller can skip them.
func colorFromParams(rest [][]uint16, pal *Palette) (col color.RGBA, ok bool, used int) {
	at := func(i int) (uint16, bool) {
		if i >= len(rest) || len(rest[i]) == 0 {
			return 0, false
		}
		return rest[i][0], true
	}
	kind, ok := at(0)
	if !ok {
		return color.RGBA{}, false, 0
	}
	switch kind {
	case 5:
		n, ok := at(1)
		if !ok {
			return color.RGBA{}, false, 1
		}
		return pal.index(int(n)), true, 2
	case 2:
		r, rok := at(1)
		g, gok := at(2)
		b, bok := at(3)
		if !rok || !gok || !bok {
			// Consume whatever of the run is present. Leaving the tail
			// behind would let "38;2;1" apply the 1 as bold.
			used := 1
			for _, ok := range []bool{rok, gok, bok} {
				if !ok {
					break
				}
				used++
			}
			return color.RGBA{}, false, used
		}
		return color.RGBA{clamp8(r), clamp8(g), clamp8(b), 0xff}, true, 4
	}
	return color.RGBA{}, false, 1
}

// clamp8 narrows a parameter to a colour channel. Parameters are 16-bit
// and come from the far end of a pipe, so values above 255 are possible
// and must not wrap.
func clamp8(v uint16) uint8 {
	if v > 255 {
		return 255
	}
	return uint8(v)
}
