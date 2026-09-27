package glyph

import (
	"fmt"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/font/sfnt"
)

// buildFaces makes a face of each style that has bytes, at sizePt and
// dpi, to prove the bytes a family loads are fonts a window can draw
// with. A style with no bytes is left nil.
func buildFaces(fonts Fonts, sizePt, dpi float64) ([numStyles]font.Face, [numStyles]Style, error) {
	var faces [numStyles]font.Face
	var none [numStyles]Style
	if fonts.Regular == nil {
		return faces, none, fmt.Errorf("no regular font")
	}
	srcs := [numStyles][]byte{
		Regular: fonts.Regular, Bold: fonts.Bold,
		Italic: fonts.Italic, BoldItalic: fonts.BoldItalic,
	}
	for s := range numStyles {
		if srcs[s] == nil {
			continue
		}
		coll, err := sfnt.ParseCollection(srcs[s])
		if err != nil {
			return faces, none, fmt.Errorf("parse font: %w", err)
		}
		i := fonts.Index[s]
		if i < 0 || i >= coll.NumFonts() {
			return faces, none, fmt.Errorf("font %d of a file holding %d", i, coll.NumFonts())
		}
		f, err := coll.Font(i)
		if err != nil {
			return faces, none, fmt.Errorf("parse font: %w", err)
		}
		if faces[s], err = opentype.NewFace(f, &opentype.FaceOptions{Size: sizePt, DPI: dpi}); err != nil {
			return faces, none, err
		}
	}
	return faces, none, nil
}
