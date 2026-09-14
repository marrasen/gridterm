package glyph

import (
	"encoding/binary"
	"path/filepath"
	"slices"
	"testing"

	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/gofont/gomonobold"
	"golang.org/x/image/font/sfnt"
)

// A TrueType collection holds several fonts in one file, sharing tables
// between them. It is how macOS ships a family's four styles, and it is
// the only reason Source.Index and Fonts.Index exist. There is no such
// file among the bundled fonts, so these tests build one.

// buildCollection packs whole fonts into a collection.
//
// Each font's table directory holds offsets from the start of the file,
// so moving a font into a collection means adding where it now sits to
// every one of them. Checksums are left alone: nothing reads them.
func buildCollection(t *testing.T, fonts ...[]byte) []byte {
	t.Helper()
	const headerSize = 12 // "ttcf", two version halves, the font count

	out := make([]byte, headerSize+4*len(fonts))
	copy(out, "ttcf")
	binary.BigEndian.PutUint16(out[4:], 1) // major version
	binary.BigEndian.PutUint16(out[6:], 0) // minor version
	binary.BigEndian.PutUint32(out[8:], uint32(len(fonts)))

	offsets := make([]uint32, len(fonts))
	for i, font := range fonts {
		// Tables are read as four-byte words, so each font starts on one.
		for len(out)%4 != 0 {
			out = append(out, 0)
		}
		offsets[i] = uint32(len(out))
		out = append(out, shiftTables(t, font, offsets[i])...)
	}
	for i, at := range offsets {
		binary.BigEndian.PutUint32(out[headerSize+4*i:], at)
	}
	return out
}

// shiftTables returns a copy of a font with every table offset moved by
// base.
func shiftTables(t *testing.T, font []byte, base uint32) []byte {
	t.Helper()
	b := slices.Clone(font)
	if len(b) < 12 {
		t.Fatalf("font is %d bytes, too short to hold a table directory", len(b))
	}
	tables := int(binary.BigEndian.Uint16(b[4:]))
	for i := 0; i < tables; i++ {
		at := 12 + 16*i + 8 // past the tag and the checksum
		if at+4 > len(b) {
			t.Fatalf("table %d runs past the end of the font", i)
		}
		binary.BigEndian.PutUint32(b[at:], binary.BigEndian.Uint32(b[at:])+base)
	}
	return b
}

// TestBuiltCollectionParses checks the test's own scaffolding before
// anything is concluded from it.
func TestBuiltCollectionParses(t *testing.T) {
	ttc := buildCollection(t, gomono.TTF, gomonobold.TTF)

	coll, err := sfnt.ParseCollection(ttc)
	if err != nil {
		t.Fatalf("parse the built collection: %v", err)
	}
	if got := coll.NumFonts(); got != 2 {
		t.Fatalf("%d fonts in the collection, want 2", got)
	}
	var buf sfnt.Buffer
	for i, want := range []string{"Regular", "Bold"} {
		f, err := coll.Font(i)
		if err != nil {
			t.Fatalf("font %d: %v", i, err)
		}
		got, err := f.Name(&buf, sfnt.NameIDSubfamily)
		if err != nil {
			t.Fatalf("font %d subfamily: %v", i, err)
		}
		if got != want {
			t.Errorf("font %d is %q, want %q", i, got, want)
		}
	}
}

// TestScanReadsEveryFontInACollection checks that a family whose styles
// share one file is found, with each style pointing at its own font
// inside that file.
func TestScanReadsEveryFontInACollection(t *testing.T) {
	dir := fontDir(t, map[string][]byte{
		"gomono.ttc": buildCollection(t, gomono.TTF, gomonobold.TTF),
	})

	fams := mustScan(t, []string{dir})

	if len(fams) != 1 {
		t.Fatalf("found %d families, want one: %+v", len(fams), fams)
	}
	f := fams[0]
	if f.Name != "Go Mono" {
		t.Errorf("family = %q, want %q", f.Name, "Go Mono")
	}
	if !f.Has(Regular) || !f.Has(Bold) {
		t.Fatalf("styles = %v, want regular and bold", f.Styles())
	}
	if f.Src[Regular].Path != f.Src[Bold].Path {
		t.Error("the two styles came from different files, but they share one")
	}
	if got := f.Src[Regular].Index; got != 0 {
		t.Errorf("regular is font %d of the collection, want 0", got)
	}
	if got := f.Src[Bold].Index; got != 1 {
		t.Errorf("bold is font %d of the collection, want 1", got)
	}
}

// TestCollectionIndexReachesTheFace is the end of that chain: the index
// has to survive Load and reach the face the atlas builds. Ignored
// anywhere along the way, every style would come out as the first font
// in the file and the whole window would be drawn in one weight.
func TestCollectionIndexReachesTheFace(t *testing.T) {
	dir := fontDir(t, map[string][]byte{
		"gomono.ttc": buildCollection(t, gomono.TTF, gomonobold.TTF),
	})
	fams := mustScan(t, []string{dir})
	if len(fams) != 1 {
		t.Fatalf("found %d families, want one", len(fams))
	}

	fonts, err := fams[0].Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if fonts.Index[Regular] != 0 || fonts.Index[Bold] != 1 {
		t.Fatalf("indices = %v, want regular at 0 and bold at 1", fonts.Index)
	}

	faces, err := buildFaces(fonts, 24, 96)
	if err != nil {
		t.Fatalf("build faces: %v", err)
	}
	// The two styles must actually differ. Bold ink is wider than
	// regular ink at the same size, even in a monospace family, because
	// only the advance is fixed.
	regular, _, ok := faces[Regular].GlyphBounds('M')
	if !ok {
		t.Fatal("the regular face has no M")
	}
	bold, _, ok := faces[Bold].GlyphBounds('M')
	if !ok {
		t.Fatal("the bold face has no M")
	}
	if regular == bold {
		t.Error("bold and regular draw the same glyph: the collection index was ignored")
	}
}

// TestCollectionIsReadOnce checks that a family sharing one file reads
// it once rather than once per style. A collection runs to tens of
// megabytes, and four styles is the ordinary case.
func TestCollectionIsReadOnce(t *testing.T) {
	ttc := buildCollection(t, gomono.TTF, gomonobold.TTF)
	dir := fontDir(t, map[string][]byte{"gomono.ttc": ttc})
	fams := mustScan(t, []string{dir})
	if len(fams) != 1 {
		t.Fatalf("found %d families, want one", len(fams))
	}

	fonts, err := fams[0].Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	// The same bytes, not two copies of them.
	if &fonts.Regular[0] != &fonts.Bold[0] {
		t.Error("the file was read twice for one family")
	}
	if got := filepath.Base(fams[0].Src[Regular].Path); got != "gomono.ttc" {
		t.Errorf("regular came from %q, want the collection", got)
	}
}
