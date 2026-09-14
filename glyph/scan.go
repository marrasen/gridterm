package glyph

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/image/font"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
)

// scanPPEM is the size monospace is tested at. Any size does, since the
// question is whether the advances match each other, not what they are;
// a large one keeps rounding from making two different widths look equal.
const scanPPEM = fixed.Int26_6(64 << 6)

// probes are the characters a terminal font has to have, and the ones
// whose advances must match for it to be monospace. Narrow, wide and
// punctuation, because a proportional font can still give two letters
// the same width by chance.
var probes = []rune{'i', 'M', 'W', 'm', '.', 'l', '0'}

// Source names one font inside a file. Most files hold a single font, at
// index 0; a TrueType collection holds several, which is how macOS ships
// a family's four styles.
type Source struct {
	Path  string
	Index int
}

// Family is an installed font family with a file for each style it has.
// Regular is always present; the others may not be.
type Family struct {
	Name string

	// Src holds where each style comes from. A zero Source means the
	// family has no file for that style, and the atlas borrows another.
	Src [numStyles]Source
}

// Has reports whether the family has a file of its own for a style.
func (f Family) Has(s Style) bool {
	return s < numStyles && f.Src[s].Path != ""
}

// Styles returns the styles the family has files for, regular first.
func (f Family) Styles() []Style {
	var out []Style
	for s := Style(0); s < numStyles; s++ {
		if f.Has(s) {
			out = append(out, s)
		}
	}
	return out
}

// Load reads the family's files. A style the family does not have is
// left nil, which makes the atlas borrow the nearest style it does have.
func (f Family) Load() (Fonts, error) {
	var fonts Fonts
	into := [numStyles]*[]byte{&fonts.Regular, &fonts.Bold, &fonts.Italic, &fonts.BoldItalic}
	// A file is read once however many styles come from it. Four styles
	// in one collection is the ordinary case on macOS, and a collection
	// runs to tens of megabytes.
	read := make(map[string][]byte, numStyles)
	for s := Style(0); s < numStyles; s++ {
		if !f.Has(s) {
			continue
		}
		path := f.Src[s].Path
		b, seen := read[path]
		if !seen {
			var err error
			if b, err = os.ReadFile(path); err != nil {
				return Fonts{}, fmt.Errorf("read %s: %w", path, err)
			}
			read[path] = b
		}
		*into[s] = b
		fonts.Index[s] = f.Src[s].Index
	}
	if fonts.Regular == nil {
		return Fonts{}, fmt.Errorf("font family %q has no regular style", f.Name)
	}
	return fonts, nil
}

// Monospaced returns the installed monospace families, by name.
//
// It reads every font file the system has, so it takes long enough to be
// worth doing off whatever goroutine is drawing.
//
// A file that will not parse is skipped and not reported: a broken font
// among hundreds is not a reason to offer the user none of them. A file
// or directory that cannot be read is a different thing and is reported,
// because otherwise an unreadable font directory looks exactly like a
// machine with no fonts on it. The families found come back either way,
// so a caller can decide what to do about the failures.
func Monospaced() ([]Family, error) {
	return monospacedIn(fontDirs())
}

// monospacedIn is Monospaced over given directories, so a test can point
// it at a directory it controls.
func monospacedIn(dirs []string) ([]Family, error) {
	// Keyed by the lowercased family name, so two files disagreeing about
	// capitalisation do not become two families.
	families := make(map[string]*Family)
	paths, failed := fontFilesIn(dirs)
	for _, path := range paths {
		faces, err := facesIn(path)
		if err != nil {
			failed = append(failed, err)
			continue
		}
		for _, face := range faces {
			addFace(families, face)
		}
	}

	out := make([]Family, 0, len(families))
	for _, f := range families {
		// A family with no regular style cannot be the primary font: the
		// atlas has nothing for the other three to borrow from.
		if f.Has(Regular) {
			out = append(out, *f)
		}
	}
	sortFamilies(out)
	return out, errors.Join(failed...)
}

// sortFamilies puts families in the order a menu shows them: by name,
// ignoring case, so "apple" and "Banana" do not come out the other way
// round from how they read.
func sortFamilies(fams []Family) {
	sort.Slice(fams, func(i, j int) bool {
		if a, b := strings.ToLower(fams[i].Name), strings.ToLower(fams[j].Name); a != b {
			return a < b
		}
		return fams[i].Name < fams[j].Name
	})
}

// faceInfo is one font inside one file, as the scan sees it.
type faceInfo struct {
	family string
	style  Style
	src    Source
}

// addFace files a face under its family, keeping the first file found
// for each style. Several files can claim the same style -- an installed
// copy and one in the user's own font directory -- and the first found
// is as good an answer as any, so long as it is always the same one.
func addFace(families map[string]*Family, face faceInfo) {
	key := strings.ToLower(face.family)
	f, ok := families[key]
	if !ok {
		f = &Family{Name: face.family}
		families[key] = f
	}
	if f.Src[face.style].Path == "" {
		f.Src[face.style] = face.src
	}
}

// fontFilesIn returns every font file under the given directories, in a
// stable order so two scans of the same system agree.
//
// An unreadable directory is skipped. Font trees are shallow and a few
// thousand entries at worst.
func fontFilesIn(dirs []string) (paths []string, failed []error) {
	seen := make(map[string]bool)
	for _, dir := range dirs {
		err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				// A font directory the system simply does not have is
				// ordinary; one it has and will not open is not.
				if !os.IsNotExist(err) {
					failed = append(failed, err)
				}
				return nil
			}
			if d.IsDir() || !isFontFile(d.Name()) {
				return nil
			}
			if !seen[path] {
				seen[path] = true
				paths = append(paths, path)
			}
			return nil
		})
		if err != nil {
			failed = append(failed, err)
		}
	}
	sort.Strings(paths)
	return paths, failed
}

// isFontFile reports whether a filename looks like something sfnt can
// parse.
func isFontFile(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".ttf", ".otf", ".ttc", ".otc", ".dfont":
		return true
	}
	return false
}

// facesIn returns the monospace faces in one file, which is usually one
// and is several for a collection.
//
// A file that will not parse yields nothing and no error. A file that
// will not open yields the error: that is the disk failing rather than
// the font being unusable.
func facesIn(path string) ([]faceInfo, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	coll, err := sfnt.ParseCollection(b)
	if err != nil {
		return nil, nil
	}
	var out []faceInfo
	var buf sfnt.Buffer
	for i := 0; i < coll.NumFonts(); i++ {
		f, err := coll.Font(i)
		if err != nil {
			continue
		}
		face, ok := describe(f, &buf, Source{Path: path, Index: i})
		if ok {
			out = append(out, face)
		}
	}
	return out, nil
}

// describe reads a font's family and style and reports whether it is a
// monospace face worth offering.
func describe(f *sfnt.Font, buf *sfnt.Buffer, src Source) (faceInfo, bool) {
	family, err := f.Name(buf, sfnt.NameIDFamily)
	if err != nil || family == "" {
		return faceInfo{}, false
	}
	// A font with no subfamily record at all is the regular one, which
	// is the same answer an empty subfamily gets. Rejecting the face
	// instead would make "no name" and "an empty name" mean different
	// things for no reason.
	sub, _ := f.Name(buf, sfnt.NameIDSubfamily)
	style, ok := styleOf(sub)
	if !ok {
		return faceInfo{}, false
	}
	if !isMonospace(f, buf) {
		return faceInfo{}, false
	}
	return faceInfo{family: strings.TrimSpace(family), style: style, src: src}, true
}

// styleOf reads a subfamily name as one of the four styles, rejecting
// anything else.
//
// Name id 1 is the family name a font uses to keep its family down to
// these four, so a face named "Light" or "SemiBold" here belongs to a
// family of its own and is not a style of this one. Rejecting it keeps
// the four slots meaning what they say.
func styleOf(subfamily string) (Style, bool) {
	switch normaliseStyle(subfamily) {
	case "regular", "book", "normal", "roman", "":
		return Regular, true
	case "bold":
		return Bold, true
	case "italic", "oblique":
		return Italic, true
	case "bolditalic", "boldoblique", "italicbold", "obliquebold":
		return BoldItalic, true
	}
	return 0, false
}

// normaliseStyle lowercases a subfamily and drops what only separates
// words, so "Bold Italic" and "BoldItalic" read the same.
func normaliseStyle(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch r {
		case ' ', '-', '_', '.':
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// isMonospace reports whether every probe character is present and they
// all advance by the same width.
//
// Asked of the font rather than read from the post table's fixed-pitch
// flag, which plenty of fonts set wrongly in both directions.
func isMonospace(f *sfnt.Font, buf *sfnt.Buffer) bool {
	want := fixed.Int26_6(-1)
	for _, r := range probes {
		gi, err := f.GlyphIndex(buf, r)
		if err != nil || gi == 0 {
			// A font without the Latin alphabet is no use as the font a
			// terminal is read in, whatever its advances do.
			return false
		}
		adv, err := f.GlyphAdvance(buf, gi, scanPPEM, font.HintingNone)
		if err != nil || adv <= 0 {
			return false
		}
		if want < 0 {
			want = adv
			continue
		}
		if adv != want {
			return false
		}
	}
	return want > 0
}
