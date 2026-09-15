package glyph

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/font/sfnt"
)

// A bundled monospace font covers Latin, light box-drawing and the block
// elements, which is enough for most terminal output. It does not cover
// CJK, heavy box-drawing, braille or combining marks, so those come from
// whatever the system has.
//
// Fallbacks are found by filename rather than by querying a font
// database: fontconfig is not available on Windows, and pulling in a
// full font-matching library to find four well-known files would be out
// of proportion.

// fallbackNames lists candidate font files in preference order,
// lowercased. The first one found that has a given rune wins.
func fallbackNames() []string {
	switch runtime.GOOS {
	case "windows":
		return []string{
			"consola.ttf",      // Consolas: wide coverage, ships with Windows
			"cascadiacode.ttf", // Cascadia Code, if the user has it
			"msgothic.ttc",     // CJK
			"seguisym.ttf",     // symbols, braille, heavy box-drawing
			"arial.ttf",        // last resort, very broad
		}
	case "darwin":
		return []string{
			"menlo.ttc",
			"sfnsmono.ttf",
			"applesymbols.ttf",
			"pingfang.ttc", // CJK
			"arialunicode.ttf",
		}
	default:
		return []string{
			"dejavusansmono.ttf",
			"notosansmono-regular.ttf",
			"unifont.ttf", // enormous coverage, ugly but complete
			"notosansmono[wdth,wght].ttf",
			"notosanscjk-regular.ttc",
			"notosansjp-regular.otf",
			"freemono.ttf",
		}
	}
}

// fontDirs returns where to look for the files above.
func fontDirs() []string {
	home, _ := os.UserHomeDir()
	switch runtime.GOOS {
	case "windows":
		var dirs []string
		if w := os.Getenv("windir"); w != "" {
			dirs = append(dirs, filepath.Join(w, "Fonts"))
		}
		if l := os.Getenv("localappdata"); l != "" {
			dirs = append(dirs, filepath.Join(l, "Microsoft", "Windows", "Fonts"))
		}
		return dirs
	case "darwin":
		dirs := []string{"/System/Library/Fonts", "/Library/Fonts"}
		if home != "" {
			dirs = append(dirs, filepath.Join(home, "Library", "Fonts"))
		}
		return dirs
	default:
		dirs := []string{
			"/usr/share/fonts",
			"/usr/local/share/fonts",
		}
		if home != "" {
			dirs = append(dirs,
				filepath.Join(home, ".local", "share", "fonts"),
				filepath.Join(home, ".fonts"))
		}
		return dirs
	}
}

// findFallbackFiles walks the font directories once and returns the
// paths of the wanted files, in preference order, along with whatever
// went wrong on the way.
//
// It walks through fontFilesIn, which is the walk the font scan does and
// the one that keeps what it could not read. What was missed is handed
// back rather than dropped, under the decision recorded on the window's
// reportFontScan.
//
// The walk is bounded: font trees are shallow and a few thousand entries
// at worst, and it happens once, lazily, on the first rune the primary
// font cannot draw.
func findFallbackFiles() ([]string, []error) {
	return findFallbackFilesIn(fontDirs())
}

// findFallbackFilesIn is findFallbackFiles over given directories, so a
// test can point it at a directory it controls.
func findFallbackFilesIn(dirs []string) ([]string, []error) {
	want := fallbackNames()
	found := make(map[string]string, len(want))

	paths, failed := fontFilesIn(dirs)
	for _, path := range paths {
		name := strings.ToLower(filepath.Base(path))
		for _, w := range want {
			if name == w {
				if _, seen := found[w]; !seen {
					found[w] = path
				}
			}
		}
	}

	out := make([]string, 0, len(found))
	for _, w := range want {
		if p, ok := found[w]; ok {
			out = append(out, p)
		}
	}
	return out, failed
}

// loadFace parses a font file and returns a face at the given size.
// TrueType collections hold several fonts in one file; the first is
// taken, which for the files listed above is the regular weight.
//
// read says the bytes were in hand, so the caller can tell a disk that
// failed from a file that is not a face this program can use. Only the
// first is worth telling anybody about; see facesIn.
func loadFace(path string, sizePt, dpi float64) (face font.Face, read bool, err error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, false, err
	}
	var f *sfnt.Font
	if strings.HasSuffix(strings.ToLower(path), ".ttc") ||
		strings.HasSuffix(strings.ToLower(path), ".otc") {
		coll, err := sfnt.ParseCollection(b)
		if err != nil {
			return nil, true, err
		}
		if f, err = coll.Font(0); err != nil {
			return nil, true, err
		}
	} else if f, err = sfnt.Parse(b); err != nil {
		return nil, true, err
	}
	face, err = opentype.NewFace(f, &opentype.FaceOptions{
		Size:    sizePt,
		DPI:     dpi,
		Hinting: font.HintingFull,
	})
	return face, true, err
}
