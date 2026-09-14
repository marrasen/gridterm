package glyph

import (
	"testing"

	"golang.org/x/image/math/fixed"

	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/gofont/gomonobold"
	"golang.org/x/image/font/gofont/gomonobolditalic"
	"golang.org/x/image/font/gofont/gomonoitalic"
)

// TestStyleFaces checks that each style gets its own face when the bytes
// are there, and borrows the right one when they are not.
func TestStyleFaces(t *testing.T) {
	full := Fonts{
		Regular:    gomono.TTF,
		Bold:       gomonobold.TTF,
		Italic:     gomonoitalic.TTF,
		BoldItalic: gomonobolditalic.TTF,
	}
	faces, err := buildFaces(full, 15, 96)
	if err != nil {
		t.Fatalf("buildFaces: %v", err)
	}
	for s := Style(0); s < numStyles; s++ {
		for o := s + 1; o < numStyles; o++ {
			if faces[s] == faces[o] {
				t.Errorf("styles %d and %d share a face", s, o)
			}
		}
	}

	for _, tc := range []struct {
		name    string
		fonts   Fonts
		borrows map[Style]Style
	}{
		{
			name:    "only regular",
			fonts:   Fonts{Regular: gomono.TTF},
			borrows: map[Style]Style{Bold: Regular, Italic: Regular, BoldItalic: Regular},
		},
		{
			name:    "no italic",
			fonts:   Fonts{Regular: gomono.TTF, Bold: gomonobold.TTF},
			borrows: map[Style]Style{Italic: Regular, BoldItalic: Bold},
		},
		{
			name:    "no bold italic",
			fonts:   Fonts{Regular: gomono.TTF, Bold: gomonobold.TTF, Italic: gomonoitalic.TTF},
			borrows: map[Style]Style{BoldItalic: Italic},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			faces, err := buildFaces(tc.fonts, 15, 96)
			if err != nil {
				t.Fatalf("buildFaces: %v", err)
			}
			for style, want := range tc.borrows {
				if faces[style] != faces[want] {
					t.Errorf("style %d did not borrow style %d", style, want)
				}
			}
		})
	}
}

// TestNewAtlasNeedsRegular checks that a Fonts with no regular weight is
// refused rather than building an atlas that cannot draw.
func TestNewAtlasNeedsRegular(t *testing.T) {
	if _, err := NewAtlas(Fonts{Bold: gomonobold.TTF}, 15, 96); err == nil {
		t.Error("NewAtlas accepted a Fonts with no regular weight")
	}
}

// TestStyleBits checks the bitfield the renderer relies on to turn the
// bold and italic attributes into a style.
func TestStyleBits(t *testing.T) {
	if got := Regular | Bold | Italic; got != BoldItalic {
		t.Errorf("Bold|Italic = %d, want %d", got, BoldItalic)
	}
}

// Every glyph sits on one baseline.
//
// The mask is placed at whole pixels, so the pen has to be at whole
// pixels too. Drawing at the outline's own corner instead leaves the
// baseline a fraction of a pixel from where the mask lands, and the
// fraction comes from the glyph's own height: a lower-case "i" reaches
// higher than an "n", so the two would be drawn at different heights on
// the same line.
func TestEveryGlyphSitsOnOneBaseline(t *testing.T) {
	const ascent = 14
	// Bounds with every shape of fractional part: a tall letter, a short
	// one, one that dips below the line, and ones landing exactly on a
	// pixel.
	for _, bounds := range []fixed.Rectangle26_6{
		{Min: fixed.Point26_6{X: 64, Y: -700}, Max: fixed.Point26_6{X: 500, Y: 0}},
		{Min: fixed.Point26_6{X: 37, Y: -643}, Max: fixed.Point26_6{X: 491, Y: 3}},
		{Min: fixed.Point26_6{X: 1, Y: -449}, Max: fixed.Point26_6{X: 470, Y: 1}},
		{Min: fixed.Point26_6{X: -12, Y: -512}, Max: fixed.Point26_6{X: 448, Y: 191}},
		{Min: fixed.Point26_6{X: 63, Y: -65}, Max: fixed.Point26_6{X: 127, Y: 63}},
	} {
		size, pen, offset := glyphBox(bounds, ascent)

		// The pen is on a whole pixel, in both directions. A fractional
		// one is the whole bug.
		if pen.X&63 != 0 || pen.Y&63 != 0 {
			t.Fatalf("%v: the pen is at %v, which is not a whole pixel", bounds, pen)
		}
		// And where the pen ends up on screen is the same for every
		// glyph: the cell's left edge, on the cell's baseline.
		if got := offset.Y + int(pen.Y>>6); got != ascent {
			t.Fatalf("%v: the baseline lands at row %d, want %d", bounds, got, ascent)
		}
		if got := offset.X + int(pen.X>>6); got != 0 {
			t.Fatalf("%v: the pen lands at column %d, want the cell's left edge", bounds, got)
		}

		// And the mask is big enough for the ink drawn into it, with the
		// pen where it is.
		inkLeft := int(bounds.Min.X>>6) + int(pen.X>>6)
		inkRight := int((bounds.Max.X+63)>>6) + int(pen.X>>6)
		inkTop := int(bounds.Min.Y>>6) + int(pen.Y>>6)
		inkBottom := int((bounds.Max.Y+63)>>6) + int(pen.Y>>6)
		if inkLeft < 0 || inkTop < 0 || inkRight > size.X || inkBottom > size.Y {
			t.Fatalf("%v: the ink spans %d..%d by %d..%d in a %v mask",
				bounds, inkLeft, inkRight, inkTop, inkBottom, size)
		}
	}
}

// Two glyphs of different heights are drawn the same distance above the
// baseline as their outlines say, rather than each rounded on its own.
func TestGlyphsKeepTheirHeightsApart(t *testing.T) {
	const ascent = 14
	// An "i" reaching a little higher than an "n", by less than a pixel.
	tall := fixed.Rectangle26_6{Min: fixed.Point26_6{Y: -645}, Max: fixed.Point26_6{X: 400}}
	short := fixed.Rectangle26_6{Min: fixed.Point26_6{Y: -449}, Max: fixed.Point26_6{X: 400}}

	tallSize, tallPen, tallAt := glyphBox(tall, ascent)
	shortSize, shortPen, shortAt := glyphBox(short, ascent)

	// Both masks end at the baseline, because both outlines do.
	if got, want := tallAt.Y+tallSize.Y, shortAt.Y+shortSize.Y; got != want {
		t.Fatalf("the two feet are at %d and %d, want them on one line", got, want)
	}
	// And both pens are on it.
	if tallAt.Y+int(tallPen.Y>>6) != shortAt.Y+int(shortPen.Y>>6) {
		t.Fatal("the two baselines are not the same row")
	}
}
