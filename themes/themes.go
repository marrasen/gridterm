// Package themes holds the colour themes a window can be drawn in, the
// ones built in and the ones the user has written down.
package themes

import (
	"encoding/json"
	"errors"
	"fmt"
	"image/color"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/marrasen/gridterm/internal/newfile"
	"github.com/marrasen/gridterm/vt"
)

// FileVersion is the version this package writes and reads.
const FileVersion = 1

// Theme is a colour theme under a name.
//
// The first sixteen colours are the theme's own. The rest of the 256
// are the layout every terminal agrees on, filled in from those.
type Theme struct {
	Name string `json:"name"`

	FG        string `json:"fg"`
	BG        string `json:"bg"`
	Selection string `json:"selection,omitempty"`

	// ANSI are the sixteen named colours, black first and bright white
	// last.
	ANSI []string `json:"ansi"`

	// Frame is the window's own furniture written down rather than
	// worked out from the two ends above. Nil for a theme that leaves it
	// to the window.
	Frame *Frame `json:"frame,omitempty"`

	// Font names the typeface the window is drawn in while this theme is
	// on, by family name. Empty leaves the font alone, which is what a
	// theme that is only about colour does.
	//
	// A name this machine has no font for is not an error: the window
	// keeps the typeface it was already drawn in.
	Font string `json:"font,omitempty"`
}

// stored is the shape of the file.
type stored struct {
	Version int     `json:"version"`
	Themes  []Theme `json:"themes"`
}

// Palette turns a theme into the colours a window draws with.
func (t Theme) Palette() (vt.Palette, error) {
	if len(t.ANSI) != 16 {
		return vt.Palette{}, fmt.Errorf(
			"%s names %d colours, and a theme names the sixteen from black to bright white",
			t.title(), len(t.ANSI))
	}
	var p vt.Palette
	read := func(name, raw string, into *color.RGBA) error {
		c, err := ParseColour(raw)
		if err != nil {
			return fmt.Errorf("%s: %s: %w", t.title(), name, err)
		}
		*into = c
		return nil
	}
	if err := errors.Join(read("fg", t.FG, &p.FG), read("bg", t.BG, &p.BG)); err != nil {
		return vt.Palette{}, err
	}
	// A selection falls back to a ground just off the window's own.
	// It cannot fall back to the foreground: selected text keeps its own
	// colour and is drawn on this, so the two would be one.
	p.Selection = p.Surface()
	if t.Selection != "" {
		if err := read("selection", t.Selection, &p.Selection); err != nil {
			return vt.Palette{}, err
		}
	}
	for i, raw := range t.ANSI {
		c, err := ParseColour(raw)
		if err != nil {
			return vt.Palette{}, fmt.Errorf("%s: colour %d: %w", t.title(), i, err)
		}
		p.ANSI[i] = c
	}
	p.FillUpper()
	return p, nil
}

// title names a theme in a message, for one with no name of its own.
func (t Theme) title() string {
	if strings.TrimSpace(t.Name) == "" {
		return "a theme with no name"
	}
	return t.Name
}

// ParseColour reads a colour written the way a stylesheet writes one:
// #rgb or #rrggbb, with or without the hash.
func ParseColour(raw string) (color.RGBA, error) {
	s := strings.TrimPrefix(strings.TrimSpace(raw), "#")
	switch len(s) {
	case 3:
		// Each digit stands for both of its pair, so #abc is #aabbcc.
		s = string([]byte{s[0], s[0], s[1], s[1], s[2], s[2]})
	case 6:
	default:
		return color.RGBA{}, fmt.Errorf(
			"%q is not a colour. Write one as #rrggbb, or #rgb for short", raw)
	}
	n, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		return color.RGBA{}, fmt.Errorf(
			"%q is not a colour. Write one as #rrggbb, or #rgb for short", raw)
	}
	return color.RGBA{R: uint8(n >> 16), G: uint8(n >> 8), B: uint8(n), A: 0xff}, nil
}

// Colour writes a colour back the way a file holds one.
func Colour(c color.RGBA) string { return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B) }

// Built are the themes that come with gridterm, the one a window opens
// on first.
func Built() []Theme {
	return []Theme{
		{
			Name: "Dark", FG: "#c8d0da", BG: "#14171c",
			Selection: "#333f52",
			ANSI: []string{
				"#1c2026", "#e06c75", "#8fd46a", "#e6b450",
				"#61afef", "#c678dd", "#56b6c2", "#abb2bf",
				"#5c6370", "#ff8b94", "#a9e88a", "#ffd074",
				"#84c5ff", "#db9af0", "#76d4df", "#ffffff",
			},
		},
		{
			Name: "Paper", FG: "#26292e", BG: "#fbfbf7",
			Selection: "#cdd8e8",
			ANSI: []string{
				"#2b3038", "#b2273a", "#3f7d20", "#9a6700",
				"#1f5fbf", "#8b3ec4", "#0f7686", "#57606a",
				"#6e7781", "#d1243c", "#4f9c28", "#b97d00",
				"#2b7bd6", "#a052e0", "#1596a8", "#24292f",
			},
		},
		{
			// The Borland IDE: bright yellow on the blue that DOS wrote
			// it on, with the grey and cyan of that era round it.
			//
			// The lower eight are the era's hues lifted off the PC's own
			// levels. A blue ground carries almost no brightness, so the
			// dark half of that palette cannot be read on it, and the
			// window writes its own labels and notes in those colours.
			Name: "Turbo", FG: "#ffff55", BG: "#0000aa",
			Selection: "#007b7b",
			// The IBM VGA character set, which comes with gridterm, so
			// the theme reads as a DOS program rather than as DOS
			// colours in a modern typeface.
			Font: "PxPlus IBM VGA8",
			// The furniture written down rather than shaded off the blue:
			// black on the light grey a DOS dialog sat on, a double rule
			// round it, and green buttons the way Turbo Pascal drew them.
			// The one Enter presses is the one written in white, which is
			// how the original marked the button it would press.
			Frame: &Frame{
				FG: "#000000", BG: "#aaaaaa", Border: "double",
				ButtonFG: "#000000", ButtonBG: "#00aa00",
				ActiveFG: "#ffffff", ActiveBG: "#00aa00",
				// The sidebar a darker grey than the menu bar, so the
				// list of what is open reads as a panel beside the
				// window rather than as more of the bar above it. The
				// row in front is written in white: the ground that
				// marks it moves towards the black the rest is in.
				SidebarBG: "#808080", CurrentFG: "#ffffff",
			},
			ANSI: []string{
				"#000000", "#ec6464", "#55cc55", "#e0a030",
				"#6f8fff", "#d070d0", "#40c8c8", "#aaaaaa",
				"#8a8a8a", "#ff5555", "#55ff55", "#ffff55",
				"#5555ff", "#ff55ff", "#55ffff", "#ffffff",
			},
		},
		{
			Name: "Contrast", FG: "#ffffff", BG: "#000000",
			Selection: "#0000c0",
			ANSI: []string{
				"#000000", "#ff5f5f", "#5fff5f", "#ffff5f",
				"#5f9fff", "#ff5fff", "#5fffff", "#e0e0e0",
				"#808080", "#ff8787", "#87ff87", "#ffff87",
				"#87c7ff", "#ff87ff", "#87ffff", "#ffffff",
			},
		},
	}
}

// File is what the user's own themes are kept in, in the directory conf
// gives gridterm.
const File = "themes.json"

// Path is where the file of the user's own themes lives, in a
// directory.
func Path(dir string) string { return filepath.Join(dir, File) }

// Load reads the themes from a file, and returns the built-in ones with
// those after them.
//
// A file that is not there is not a failure: it is what the first run
// looks like. Anything else is, because a theme file half read would
// offer a theme with colours nobody chose.
func Load(path string) ([]Theme, error) {
	all := Built()
	raw, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return all, nil
	case err != nil:
		return all, fmt.Errorf("themes: read %s: %w", path, err)
	}
	var file stored
	if err := json.Unmarshal(raw, &file); err != nil {
		return all, fmt.Errorf("themes: read %s: %w", path, err)
	}
	if file.Version != FileVersion {
		return all, fmt.Errorf(
			"themes: %s says version %d, and this gridterm reads version %d",
			path, file.Version, FileVersion)
	}
	// Built up beside the list rather than into it, so a file that fails
	// half way offers none of itself.
	mine := make([]Theme, 0, len(file.Themes))
	for i, t := range file.Themes {
		if strings.TrimSpace(t.Name) == "" {
			return all, fmt.Errorf("themes: %s: theme %d has no name", path, i+1)
		}
		// Read now rather than when it is picked, so a colour nobody can
		// read is said when the file is, and not at the moment the user
		// chooses the theme.
		if _, err := t.Palette(); err != nil {
			return all, fmt.Errorf("themes: %s: %w", path, err)
		}
		if _, err := t.Look(); err != nil {
			return all, fmt.Errorf("themes: %s: %w", path, err)
		}
		taken := slices.ContainsFunc(all, func(have Theme) bool {
			return strings.EqualFold(have.Name, t.Name)
		}) || slices.ContainsFunc(mine, func(have Theme) bool {
			return strings.EqualFold(have.Name, t.Name)
		})
		if taken {
			return all, fmt.Errorf("themes: %s: there are two themes called %q", path, t.Name)
		}
		mine = append(mine, t)
	}
	return append(all, mine...), nil
}

// Named is the theme with a name, and whether there is one. Case does
// not matter: a name is what the user types or picks.
func Named(all []Theme, name string) (Theme, bool) {
	for _, t := range all {
		if strings.EqualFold(t.Name, name) {
			return t, true
		}
	}
	return Theme{}, false
}

// Names are the themes' names, in the order they are offered.
func Names(all []Theme) []string {
	out := make([]string, 0, len(all))
	for _, t := range all {
		out = append(out, t.Name)
	}
	return out
}

// WriteStart writes a themes file holding a copy of one theme, for a
// user with nowhere to start from.
//
// It refuses a file that is already there: what is in one is the user's,
// and sixteen colours cannot be got back.
func WriteStart(path string, from Theme) error {
	from.Name = from.Name + " of my own"
	file := stored{Version: FileVersion, Themes: []Theme{from}}
	raw, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return fmt.Errorf("themes: write %s: %w", path, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("themes: write %s: %w", path, err)
	}
	// Whole or not at all: a starting file cut short by a write that
	// failed would be a file the next attempt refuses to write over.
	err = newfile.Write(path, append(raw, '\n'), 0o600)
	if errors.Is(err, fs.ErrExist) {
		return fmt.Errorf(
			"%s is already there. Edit it, or move it aside and take this again", path)
	}
	if err != nil {
		return fmt.Errorf("themes: write %s: %w", path, err)
	}
	return nil
}
