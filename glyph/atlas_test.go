package glyph

import (
	"testing"

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
