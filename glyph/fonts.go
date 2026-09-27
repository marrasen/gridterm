// Package glyph finds the monospaced font families on this machine, and
// holds the font files a family's four styles are drawn from.
package glyph

import (
	"os"
	"path/filepath"
	"runtime"
)

// Style selects a variant of the same typeface.
//
// The values are a bitfield, Bold bit 0 and Italic bit 1, so a caller
// holding the two attributes separately combines them with a plain OR.
type Style uint8

const (
	Regular    Style = 0
	Bold       Style = 1
	Italic     Style = 2
	BoldItalic Style = Bold | Italic
	numStyles  Style = 4
)

// Fonts holds the TrueType or OpenType bytes for each style. Regular is
// required; a nil entry borrows the nearest style that is present.
type Fonts struct {
	Regular, Bold, Italic, BoldItalic []byte

	// Index picks which font inside a collection file each style is.
	// Zero for an ordinary font file, which holds one font. A family
	// whose four styles share one .ttc, as macOS ships them, needs it.
	Index [numStyles]int
}

// fontDirs are the directories this platform installs fonts in, the
// system's and the user's.
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
