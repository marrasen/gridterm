package themes

import (
	"fmt"
	"image/color"
	"strings"
)

// Frame is the window's own furniture written down rather than worked
// out: the menu bar, the sidebar and the dialogs.
//
// A theme that leaves it out has all of that derived from its two ends,
// which is right for a theme whose ground and text are a step apart. It
// is not right for one that wants a dark ground under light furniture,
// because a colour blended from the ground towards the text can only
// land between them.
type Frame struct {
	// FG and BG are what the furniture is written in and sits on.
	FG string `json:"fg"`
	BG string `json:"bg"`

	// Border is the rule round a dialog: "single" or "double". Empty
	// means single.
	Border string `json:"border,omitempty"`

	// ButtonFG and ButtonBG are a button, and ActiveFG and ActiveBG the
	// one Enter would press. Empty means derived from FG and BG.
	ButtonFG string `json:"buttonFG,omitempty"`
	ButtonBG string `json:"buttonBG,omitempty"`
	ActiveFG string `json:"activeFG,omitempty"`
	ActiveBG string `json:"activeBG,omitempty"`
}

// Look is a Frame with its colours read, and what a window draws its
// furniture from. The zero Look means the theme said nothing, and every
// colour is derived as before.
//
// A theme that wrote its frame down gets flat dialogs: an opaque box
// with square corners and no frosted glass. Glass behind an opaque box
// is paid for and never seen, and a theme that names the colours a
// dialog is drawn in has said it wants that box.
type Look struct {
	Set bool

	FG, BG color.RGBA

	// Double picks the double-line box characters for a dialog's rule.
	Double bool

	ButtonFG, ButtonBG color.RGBA
	ActiveFG, ActiveBG color.RGBA
}

// Look reads the theme's frame block. A theme with no block gets the
// zero Look, and the window derives its furniture as it always has.
func (t Theme) Look() (Look, error) {
	if t.Frame == nil {
		return Look{}, nil
	}
	f := *t.Frame
	if strings.TrimSpace(f.FG) == "" || strings.TrimSpace(f.BG) == "" {
		return Look{}, fmt.Errorf(
			"%s: frame: a frame block names fg and bg, the two colours the window's own "+
				"menu bar, sidebar and dialogs are drawn in", t.title())
	}
	l := Look{Set: true}
	read := func(name, raw string, into *color.RGBA) error {
		c, err := ParseColour(raw)
		if err != nil {
			return fmt.Errorf("%s: frame: %s: %w", t.title(), name, err)
		}
		*into = c
		return nil
	}
	if err := read("fg", f.FG, &l.FG); err != nil {
		return Look{}, err
	}
	if err := read("bg", f.BG, &l.BG); err != nil {
		return Look{}, err
	}
	switch strings.ToLower(strings.TrimSpace(f.Border)) {
	case "", "single":
	case "double":
		l.Double = true
	default:
		return Look{}, fmt.Errorf(
			"%s: frame: %q is not a border. Write \"single\" or \"double\"", t.title(), f.Border)
	}
	// A button falls back to the frame turned round, which is what marks
	// one out from the dialog it sits on.
	l.ButtonFG, l.ButtonBG = l.BG, l.FG
	l.ActiveFG, l.ActiveBG = l.BG, l.FG
	for _, c := range []struct {
		name string
		raw  string
		into *color.RGBA
	}{
		{"buttonFG", f.ButtonFG, &l.ButtonFG},
		{"buttonBG", f.ButtonBG, &l.ButtonBG},
		{"activeFG", f.ActiveFG, &l.ActiveFG},
		{"activeBG", f.ActiveBG, &l.ActiveBG},
	} {
		if c.raw == "" {
			continue
		}
		if err := read(c.name, c.raw, c.into); err != nil {
			return Look{}, err
		}
	}
	return l, nil
}
