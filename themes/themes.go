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

	"github.com/marrasen/kakel/internal/newfile"
	"github.com/marrasen/kakel/vt"
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

	// Echo is how the rings kakel sends past its window's edges look
	// under this theme. Nil takes them from the palette.
	Echo *Echo `json:",omitempty"`

	// Shape is how round and how roomy the window's furniture is. Nil
	// leaves it as kakel draws it.
	Shape *Shape `json:"shape,omitempty"`

	// Motion is how things move: "still", with almost no animation;
	// "calm", as kakel moves them, which empty means too; or "lively",
	// with a bounce.
	Motion string `json:"motion,omitempty"`
}

// Motions a theme can ask for.
const (
	MotionStill  = "still"
	MotionCalm   = "calm"
	MotionLively = "lively"
)

// Shape is how round and how roomy the window's furniture is, each a
// scale on how kakel draws it, where 1 is as it is. Unset is 1.
type Shape struct {
	// Corners scales every rounded corner: 0 squares them all, 2 makes
	// them twice as round.
	Corners *float64 `json:"corners,omitempty"`
	// Room scales the room in and around things: the padding in
	// buttons, fields, menus and dialogs, how tall they and the rows
	// of lists are, and the gaps between them. 0.75 is tight, 1.4 roomy.
	Room *float64 `json:"room,omitempty"`
	// Text is the size of the window's own words, in logical pixels.
	// The terminals keep the font size set for them. Unset is 14.
	Text *float64 `json:"text,omitempty"`
}

// Echo is the look of the rings kakel sends out past its window's
// edges: a colour for each tone, and how strong they are. An empty
// colour is taken from the palette: Problem from bright red, Done from
// bright green, Call from bright yellow, and Wait from the text, dimmed.
type Echo struct {
	// Problem is for a failure, such as a connection dropped. Done is
	// for work finished, such as a copy. Call is for the bell rung in a
	// pane out of sight. Wait is the faint ring while connecting.
	Problem string `json:",omitempty"`
	Done    string `json:",omitempty"`
	Call    string `json:",omitempty"`
	Wait    string `json:",omitempty"`
	// Strength scales every ring: 0 turns the echo off, 1 draws it as
	// it comes, and 2 twice as strong. Unset is 1.
	Strength *float64 `json:",omitempty"`
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

// Built are the themes that come with kakel, the one a window opens
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
			// A green phosphor screen: green on black, square corners,
			// everything close together, and buttons that cast a black
			// shadow, as a text screen's did.
			//
			// The colours keep their hues, only leaning green, so a
			// failure a program writes in red still reads as one; the
			// bright blue, which the window takes its accent from, is a
			// green of its own.
			Name: "Phosphor", FG: "#33ff66", BG: "#050a05",
			Selection: "#0f4a1f",
			Frame: &Frame{
				FG: "#33ff66", BG: "#081208",
				// Buttons written in the green, on a dark green, and the
				// one Enter presses lit solid: the window writes a
				// plain button in the frame's own colour.
				ButtonFG: "#33ff66", ButtonBG: "#0c2a14",
				ActiveFG: "#050a05", ActiveBG: "#33ff66",
				SidebarBG: "#040804", CurrentFG: "#d6ffe0",
				ButtonShadow: "#000000",
			},
			Shape: &Shape{Corners: ptr(0), Room: ptr(0.78), Text: ptr(13)},
			ANSI: []string{
				"#0a140a", "#e0605a", "#33cc55", "#c8d65a",
				"#3fbf80", "#b87ad0", "#3fc8b0", "#9fdfae",
				"#2e5a38", "#ff7a70", "#33ff66", "#ecff7a",
				"#7dffa8", "#d69cf0", "#6fffe0", "#e8ffee",
			},
		},
		{
			// Soft and round: pastel pink on a warm white, corners twice
			// as round as usual, room to breathe everywhere, and things
			// that bounce as they move.
			Name: "Marshmallow", FG: "#4a4058", BG: "#fff7fb",
			Selection: "#f5d6ec",
			Frame: &Frame{
				FG: "#4a4058", BG: "#fdeef6",
				ButtonFG: "#4a4058", ButtonBG: "#f6dcea",
				ActiveFG: "#ffffff", ActiveBG: "#e27fb8",
				SidebarBG: "#f8e6f1", CurrentFG: "#b84f8f",
			},
			Shape:  &Shape{Corners: ptr(2.2), Room: ptr(1.35), Text: ptr(15)},
			Motion: MotionLively,
			ANSI: []string{
				"#5a5068", "#e0607e", "#4f9e6a", "#c98a2e",
				"#6a7fd8", "#b061c9", "#3f9ea8", "#8a8098",
				"#a89cb8", "#f07896", "#6abf86", "#e0a64a",
				"#e27fb8", "#c77ee0", "#5ab8c2", "#2e2838",
			},
		},
		{
			// Electronic paper: near black on a warm grey, colours kept
			// but faded, next to no rounding, and next to no motion, as
			// a page that is redrawn rather than one that moves.
			Name: "Ink", FG: "#1a1a1a", BG: "#ebe9e4",
			Selection: "#c9c6be",
			Frame: &Frame{
				FG: "#1a1a1a", BG: "#e2dfd8",
				ButtonFG: "#1a1a1a", ButtonBG: "#d2cec5",
				ActiveFG: "#ebe9e4", ActiveBG: "#1a1a1a",
				SidebarBG: "#dcd8cf", CurrentFG: "#000000",
			},
			Shape:  &Shape{Corners: ptr(0.3), Room: ptr(1.1)},
			Motion: MotionStill,
			ANSI: []string{
				"#1a1a1a", "#8c2f2f", "#3f6b3f", "#7a6420",
				"#2f4f7a", "#6a3f7a", "#2f6a6a", "#5a5752",
				"#7a766e", "#a33c3c", "#4f8050", "#8f7628",
				"#1a1a1a", "#7f4f90", "#3a7f7f", "#000000",
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

// ptr is v, for a Shape's settings, which are unset when nil.
func ptr(v float64) *float64 { return &v }

// File is what the user's own themes are kept in, in the directory conf
// gives kakel.
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
			"themes: %s says version %d, and this kakel reads version %d",
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
		// newfile says which file already.
		return fmt.Errorf("themes: %w", err)
	}
	return nil
}
