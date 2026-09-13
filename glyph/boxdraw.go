package glyph

import "image"

// Box-drawing and block-element characters are drawn in code rather than
// taken from a font.
//
// A font's box glyphs are designed for that font's own advance width and
// line height, so in a terminal's cell box the strokes stop short of the
// edges and adjacent cells do not join up. Every table and framed TUI
// then renders as a field of disconnected ticks. Drawing them to the
// exact cell size is the only way to make the lines meet.

// stroke weights used by the line-drawing characters.
const (
	wNone uint8 = iota
	wLight
	wHeavy
	wDouble
)

// boxLine describes one line-drawing character as the weight of its four
// arms, in the order up, right, down, left.
type boxLine struct {
	up, right, down, left uint8
	// dashes, when non-zero, splits the arms into that many segments.
	dashes int
}

// boxLines maps the line-drawing characters this package draws. The
// remainder of the block falls through to the font.
var boxLines = map[rune]boxLine{
	// Horizontal and vertical, plain and dashed.
	0x2500: {right: wLight, left: wLight},
	0x2501: {right: wHeavy, left: wHeavy},
	0x2502: {up: wLight, down: wLight},
	0x2503: {up: wHeavy, down: wHeavy},
	0x2504: {right: wLight, left: wLight, dashes: 3},
	0x2505: {right: wHeavy, left: wHeavy, dashes: 3},
	0x2506: {up: wLight, down: wLight, dashes: 3},
	0x2507: {up: wHeavy, down: wHeavy, dashes: 3},
	0x2508: {right: wLight, left: wLight, dashes: 4},
	0x2509: {right: wHeavy, left: wHeavy, dashes: 4},
	0x250a: {up: wLight, down: wLight, dashes: 4},
	0x250b: {up: wHeavy, down: wHeavy, dashes: 4},
	0x254c: {right: wLight, left: wLight, dashes: 2},
	0x254d: {right: wHeavy, left: wHeavy, dashes: 2},
	0x254e: {up: wLight, down: wLight, dashes: 2},
	0x254f: {up: wHeavy, down: wHeavy, dashes: 2},

	// Corners.
	0x250c: {down: wLight, right: wLight},
	0x250d: {down: wLight, right: wHeavy},
	0x250e: {down: wHeavy, right: wLight},
	0x250f: {down: wHeavy, right: wHeavy},
	0x2510: {down: wLight, left: wLight},
	0x2511: {down: wLight, left: wHeavy},
	0x2512: {down: wHeavy, left: wLight},
	0x2513: {down: wHeavy, left: wHeavy},
	0x2514: {up: wLight, right: wLight},
	0x2515: {up: wLight, right: wHeavy},
	0x2516: {up: wHeavy, right: wLight},
	0x2517: {up: wHeavy, right: wHeavy},
	0x2518: {up: wLight, left: wLight},
	0x2519: {up: wLight, left: wHeavy},
	0x251a: {up: wHeavy, left: wLight},
	0x251b: {up: wHeavy, left: wHeavy},

	// Tees, left.
	0x251c: {up: wLight, down: wLight, right: wLight},
	0x251d: {up: wLight, down: wLight, right: wHeavy},
	0x251e: {up: wHeavy, down: wLight, right: wLight},
	0x251f: {up: wLight, down: wHeavy, right: wLight},
	0x2520: {up: wHeavy, down: wHeavy, right: wLight},
	0x2521: {up: wHeavy, down: wLight, right: wHeavy},
	0x2522: {up: wLight, down: wHeavy, right: wHeavy},
	0x2523: {up: wHeavy, down: wHeavy, right: wHeavy},

	// Tees, right.
	0x2524: {up: wLight, down: wLight, left: wLight},
	0x2525: {up: wLight, down: wLight, left: wHeavy},
	0x2526: {up: wHeavy, down: wLight, left: wLight},
	0x2527: {up: wLight, down: wHeavy, left: wLight},
	0x2528: {up: wHeavy, down: wHeavy, left: wLight},
	0x2529: {up: wHeavy, down: wLight, left: wHeavy},
	0x252a: {up: wLight, down: wHeavy, left: wHeavy},
	0x252b: {up: wHeavy, down: wHeavy, left: wHeavy},

	// Tees, down.
	0x252c: {left: wLight, right: wLight, down: wLight},
	0x252d: {left: wHeavy, right: wLight, down: wLight},
	0x252e: {left: wLight, right: wHeavy, down: wLight},
	0x252f: {left: wHeavy, right: wHeavy, down: wLight},
	0x2530: {left: wLight, right: wLight, down: wHeavy},
	0x2531: {left: wHeavy, right: wLight, down: wHeavy},
	0x2532: {left: wLight, right: wHeavy, down: wHeavy},
	0x2533: {left: wHeavy, right: wHeavy, down: wHeavy},

	// Tees, up.
	0x2534: {left: wLight, right: wLight, up: wLight},
	0x2535: {left: wHeavy, right: wLight, up: wLight},
	0x2536: {left: wLight, right: wHeavy, up: wLight},
	0x2537: {left: wHeavy, right: wHeavy, up: wLight},
	0x2538: {left: wLight, right: wLight, up: wHeavy},
	0x2539: {left: wHeavy, right: wLight, up: wHeavy},
	0x253a: {left: wLight, right: wHeavy, up: wHeavy},
	0x253b: {left: wHeavy, right: wHeavy, up: wHeavy},

	// Crosses.
	0x253c: {up: wLight, right: wLight, down: wLight, left: wLight},
	0x253d: {up: wLight, right: wLight, down: wLight, left: wHeavy},
	0x253e: {up: wLight, right: wHeavy, down: wLight, left: wLight},
	0x253f: {up: wLight, right: wHeavy, down: wLight, left: wHeavy},
	0x2540: {up: wHeavy, right: wLight, down: wLight, left: wLight},
	0x2541: {up: wLight, right: wLight, down: wHeavy, left: wLight},
	0x2542: {up: wHeavy, right: wLight, down: wHeavy, left: wLight},
	0x2543: {up: wHeavy, right: wLight, down: wLight, left: wHeavy},
	0x2544: {up: wHeavy, right: wHeavy, down: wLight, left: wLight},
	0x2545: {up: wLight, right: wLight, down: wHeavy, left: wHeavy},
	0x2546: {up: wLight, right: wHeavy, down: wHeavy, left: wLight},
	0x2547: {up: wHeavy, right: wHeavy, down: wLight, left: wHeavy},
	0x2548: {up: wLight, right: wHeavy, down: wHeavy, left: wHeavy},
	0x2549: {up: wHeavy, right: wLight, down: wHeavy, left: wHeavy},
	0x254a: {up: wHeavy, right: wHeavy, down: wHeavy, left: wLight},
	0x254b: {up: wHeavy, right: wHeavy, down: wHeavy, left: wHeavy},

	// Double lines.
	0x2550: {right: wDouble, left: wDouble},
	0x2551: {up: wDouble, down: wDouble},
	0x2552: {down: wLight, right: wDouble},
	0x2553: {down: wDouble, right: wLight},
	0x2554: {down: wDouble, right: wDouble},
	0x2555: {down: wLight, left: wDouble},
	0x2556: {down: wDouble, left: wLight},
	0x2557: {down: wDouble, left: wDouble},
	0x2558: {up: wLight, right: wDouble},
	0x2559: {up: wDouble, right: wLight},
	0x255a: {up: wDouble, right: wDouble},
	0x255b: {up: wLight, left: wDouble},
	0x255c: {up: wDouble, left: wLight},
	0x255d: {up: wDouble, left: wDouble},
	0x255e: {up: wLight, down: wLight, right: wDouble},
	0x255f: {up: wDouble, down: wDouble, right: wLight},
	0x2560: {up: wDouble, down: wDouble, right: wDouble},
	0x2561: {up: wLight, down: wLight, left: wDouble},
	0x2562: {up: wDouble, down: wDouble, left: wLight},
	0x2563: {up: wDouble, down: wDouble, left: wDouble},
	0x2564: {left: wDouble, right: wDouble, down: wLight},
	0x2565: {left: wLight, right: wLight, down: wDouble},
	0x2566: {left: wDouble, right: wDouble, down: wDouble},
	0x2567: {left: wDouble, right: wDouble, up: wLight},
	0x2568: {left: wLight, right: wLight, up: wDouble},
	0x2569: {left: wDouble, right: wDouble, up: wDouble},
	0x256a: {left: wDouble, right: wDouble, up: wLight, down: wLight},
	0x256b: {left: wLight, right: wLight, up: wDouble, down: wDouble},
	0x256c: {left: wDouble, right: wDouble, up: wDouble, down: wDouble},

	// Half lines and mixed-weight stubs.
	0x2574: {left: wLight},
	0x2575: {up: wLight},
	0x2576: {right: wLight},
	0x2577: {down: wLight},
	0x2578: {left: wHeavy},
	0x2579: {up: wHeavy},
	0x257a: {right: wHeavy},
	0x257b: {down: wHeavy},
	0x257c: {left: wLight, right: wHeavy},
	0x257d: {up: wLight, down: wHeavy},
	0x257e: {left: wHeavy, right: wLight},
	0x257f: {up: wHeavy, down: wLight},
}

// drawnRune reports whether r is drawn in code rather than looked up in
// a font.
func drawnRune(r rune) bool {
	if _, ok := boxLines[r]; ok {
		return true
	}
	return r >= 0x2580 && r <= 0x2595
}

// drawCellGlyph renders r into a w by h premultiplied-white mask and
// reports whether it drew anything.
func drawCellGlyph(r rune, w, h int, pix []byte) bool {
	if w <= 0 || h <= 0 {
		return false
	}
	m := &mask{pix: pix, w: w, h: h}
	if bl, ok := boxLines[r]; ok {
		drawBoxLine(m, bl)
		return true
	}
	if r >= 0x2580 && r <= 0x2595 {
		return drawBlock(m, r)
	}
	return false
}

// mask is a w by h premultiplied-white coverage buffer, 4 bytes a pixel.
type mask struct {
	pix  []byte
	w, h int
}

// fill sets every pixel of the rectangle to a coverage level.
func (m *mask) fill(x0, y0, x1, y1 int, alpha uint8) {
	x0, y0 = max(x0, 0), max(y0, 0)
	x1, y1 = min(x1, m.w), min(y1, m.h)
	for y := y0; y < y1; y++ {
		row := m.pix[(y*m.w+x0)*4 : (y*m.w+x1)*4]
		for i := 0; i < len(row); i += 4 {
			row[i], row[i+1], row[i+2], row[i+3] = alpha, alpha, alpha, alpha
		}
	}
}

// checker fills a rectangle with a dither pattern, for the shade blocks.
// A real dither reads better at terminal sizes than flat translucency,
// which the renderer would have to blend against an unknown background.
func (m *mask) checker(x0, y0, x1, y1, period, keep int) {
	x0, y0 = max(x0, 0), max(y0, 0)
	x1, y1 = min(x1, m.w), min(y1, m.h)
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			if (x+y*2)%period < keep {
				i := (y*m.w + x) * 4
				m.pix[i], m.pix[i+1], m.pix[i+2], m.pix[i+3] = 255, 255, 255, 255
			}
		}
	}
}

// thickness returns the stroke width in pixels for a weight.
func thickness(weight uint8, cell int) int {
	light := max(cell/14, 1)
	switch weight {
	case wHeavy:
		return max(light*2, 2)
	case wDouble:
		return light
	default:
		return light
	}
}

// drawBoxLine strokes one line-drawing character. Every arm runs from
// the cell's centre to its edge, so the same character in the next cell
// continues the line exactly.
func drawBoxLine(m *mask, bl boxLine) {
	// Arms meet at the centre of the cell. Rounding down keeps a
	// one-pixel stroke on the same side of centre in every cell, which
	// is what stops a long rule from looking ragged.
	cx, cy := m.w/2, m.h/2

	arm := func(weight uint8, dir int, vertical bool) {
		if weight == wNone {
			return
		}
		t := thickness(weight, m.h)
		if weight == wDouble {
			// Two parallel strokes with a gap of one stroke width.
			off := t
			m.stroke(cx-off, cy-off, dir, vertical, t, bl.dashes)
			m.stroke(cx+off, cy+off, dir, vertical, t, bl.dashes)
			return
		}
		m.stroke(cx, cy, dir, vertical, t, bl.dashes)
	}

	arm(bl.up, -1, true)
	arm(bl.down, +1, true)
	arm(bl.left, -1, false)
	arm(bl.right, +1, false)
}

// stroke draws one arm from (cx,cy) to the cell edge in direction dir.
func (m *mask) stroke(cx, cy, dir int, vertical bool, t, dashes int) {
	half := t / 2
	if vertical {
		y0, y1 := cy, m.h
		if dir < 0 {
			y0, y1 = 0, cy+t-half
		}
		m.dashed(cx-half, y0, cx-half+t, y1, dashes, true)
		return
	}
	x0, x1 := cx, m.w
	if dir < 0 {
		x0, x1 = 0, cx+t-half
	}
	m.dashed(x0, cy-half, x1, cy-half+t, dashes, false)
}

// dashed fills a rectangle, optionally broken into n segments along its
// long axis.
func (m *mask) dashed(x0, y0, x1, y1, n int, vertical bool) {
	if n <= 1 {
		m.fill(x0, y0, x1, y1, 255)
		return
	}
	// Each segment is on for two thirds of its slot, which is roughly
	// how the dashed characters look in a typical font.
	if vertical {
		span := y1 - y0
		for i := 0; i < n; i++ {
			a := y0 + span*i/n
			b := y0 + (span*i/n + (span/n)*2/3)
			m.fill(x0, a, x1, max(b, a+1), 255)
		}
		return
	}
	span := x1 - x0
	for i := 0; i < n; i++ {
		a := x0 + span*i/n
		b := x0 + (span*i/n + (span/n)*2/3)
		m.fill(max(a, x0), y0, max(b, a+1), y1, 255)
	}
}

// drawBlock renders the block elements and shades, U+2580 to U+2595.
func drawBlock(m *mask, r rune) bool {
	switch {
	case r == 0x2580: // upper half
		m.fill(0, 0, m.w, m.h/2, 255)
	case r >= 0x2581 && r <= 0x2587: // lower eighths, 1/8 up to 7/8
		n := int(r-0x2581) + 1
		m.fill(0, m.h-m.h*n/8, m.w, m.h, 255)
	case r == 0x2588: // full block
		m.fill(0, 0, m.w, m.h, 255)
	case r >= 0x2589 && r <= 0x258f: // left eighths, 7/8 down to 1/8
		n := 8 - (int(r-0x2589) + 1)
		m.fill(0, 0, m.w*n/8, m.h, 255)
	case r == 0x2590: // right half
		m.fill(m.w/2, 0, m.w, m.h, 255)
	case r == 0x2591: // light shade
		m.checker(0, 0, m.w, m.h, 4, 1)
	case r == 0x2592: // medium shade
		m.checker(0, 0, m.w, m.h, 2, 1)
	case r == 0x2593: // dark shade
		m.checker(0, 0, m.w, m.h, 4, 3)
	case r == 0x2594: // upper eighth
		m.fill(0, 0, m.w, max(m.h/8, 1), 255)
	case r == 0x2595: // right eighth
		m.fill(m.w-max(m.w/8, 1), 0, m.w, m.h, 255)
	default:
		return false
	}
	return true
}

// cellRect is the full cell box, which every drawn glyph fills.
func cellRect(m Metrics) image.Rectangle {
	return image.Rect(0, 0, m.CellW, m.CellH)
}
