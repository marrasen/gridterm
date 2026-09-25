package main

import (
	"image/color"
	"math"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/theme"
	"github.com/marrasen/gunim/widget"

	"github.com/marrasen/gridterm/settings"
	"github.com/marrasen/gridterm/themes"
	"github.com/marrasen/gridterm/vt"
)

// gridterm's themes, turned into gunim's. A theme is a terminal palette
// with a ground and a text colour at its two ends; the window's own
// surfaces are worked out from those, the ground a step towards the
// text, as gridterm works them out, or taken from the frame a theme
// writes down. Switching themes fades every colour across, the
// terminals' included.

// termBackground is a terminal's ground, where its cells leave it clear.
var termBackground = theme.Color("gunimterm.background", color.NRGBA{R: 0x14, G: 0x17, B: 0x1c, A: 0xff})

// themed is a gridterm theme ready for the window: gunim's theme, and
// the palette the terminals draw with.
type themed struct {
	name  string
	theme theme.Theme
	// content is what the panes on stage wear: the terminal's own text
	// and ground, which a theme's frame may not share, as Turbo's grey
	// frame round its blue ground does not.
	content theme.Theme
	palette vt.Palette
}

// luminance is how bright c looks, from 0 to 1.
func luminance(c color.NRGBA) float64 {
	lin := func(v uint8) float64 {
		x := float64(v) / 255
		if x <= 0.03928 {
			return x / 12.92
		}
		return math.Pow((x+0.055)/1.055, 2.4)
	}
	return 0.2126*lin(c.R) + 0.7152*lin(c.G) + 0.0722*lin(c.B)
}

// contrast is how far apart two colours read, from 1 to 21.
func contrast(a, b color.NRGBA) float64 {
	la, lb := luminance(a), luminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

// standout returns the one of cs that stands out most on bg.
func standout(bg color.NRGBA, cs ...color.NRGBA) color.NRGBA {
	best := cs[0]
	for _, c := range cs[1:] {
		if contrast(c, bg) > contrast(best, bg) {
			best = c
		}
	}
	return best
}

// loadThemes returns the themes on offer, ready: gridterm's own and the
// user's, from the themes file gridterm reads, or gridterm's own alone
// when there is no file to read.
func loadThemes() []themed {
	all := themes.Built()
	if dir, err := settings.Dir(); err == nil {
		if read, err := themes.Load(themes.Path(dir)); err == nil && len(read) > 0 {
			all = read
		}
	}
	var out []themed
	for _, t := range all {
		if th, err := themeOf(t); err == nil {
			out = append(out, th)
		}
	}
	return out
}

func nrgba(c color.RGBA) color.NRGBA { return color.NRGBA{R: c.R, G: c.G, B: c.B, A: 0xff} }

// mix goes from a towards b by pct percent.
func mix(a, b color.NRGBA, pct int) color.NRGBA {
	m := func(x, y uint8) uint8 { return uint8((int(x)*(100-pct) + int(y)*pct) / 100) }
	return color.NRGBA{R: m(a.R, b.R), G: m(a.G, b.G), B: m(a.B, b.B), A: 0xff}
}

func alpha(c color.NRGBA, a uint8) color.NRGBA { c.A = a; return c }

// themeOf turns a gridterm theme into gunim's.
func themeOf(t themes.Theme) (themed, error) {
	pal, err := t.Palette()
	if err != nil {
		return themed{}, err
	}
	look, err := t.Look()
	if err != nil {
		return themed{}, err
	}
	bg, fg := nrgba(pal.BG), nrgba(pal.FG)
	frameBG, frameFG := bg, fg
	sideBG, sideFG := bg, fg
	if look.Set {
		frameBG, frameFG = nrgba(look.BG), nrgba(look.FG)
		sideBG, sideFG = nrgba(look.SidebarBG), nrgba(look.SidebarFG)
	}
	accent := nrgba(pal.ANSI[12])
	buttonBG, buttonFG := mix(frameBG, frameFG, 14), frameFG
	primaryBG := mix(nrgba(pal.ANSI[4]), frameBG, 20)
	if look.Set {
		buttonBG, buttonFG = nrgba(look.ButtonBG), nrgba(look.ButtonFG)
		primaryBG = nrgba(look.ActiveBG)
	}
	surface := mix(frameBG, frameFG, 7)
	rule := mix(frameBG, frameFG, 20)
	dim := mix(frameFG, frameBG, 45)
	th := theme.Make(t.Name,
		theme.Set(widget.Background, bg),
		theme.Set(widget.Ink, frameFG),
		theme.Set(widget.Accent, accent),
		theme.Set(widget.Selection, alpha(nrgba(pal.Selection), 0xa0)),
		theme.Set(widget.CardFill, surface),
		theme.Set(widget.DialogFill, surface),
		theme.Set(widget.DialogBorder, rule),
		theme.Set(widget.DialogProblem, standout(surface, nrgba(pal.ANSI[1]), nrgba(pal.ANSI[9]))),
		theme.Set(widget.MenuFill, surface),
		theme.Set(widget.MenuBorder, rule),
		theme.Set(widget.MenuHot, alpha(accent, 0x48)),
		theme.Set(widget.MenuHint, dim),
		theme.Set(widget.FieldFill, mix(bg, frameBG, 50)),
		theme.Set(widget.FieldBorder, rule),
		theme.Set(widget.Placeholder, dim),
		theme.Set(widget.ButtonFill, buttonBG),
		theme.Set(widget.ButtonHover, mix(buttonBG, buttonFG, 12)),
		theme.Set(widget.ButtonPrimaryFill, primaryBG),
		theme.Set(widget.ButtonPrimaryHover, mix(primaryBG, color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}, 12)),
		theme.Set(widget.TooltipFill, alpha(frameFG, 0xf4)),
		theme.Set(widget.TooltipInk, frameBG),
		theme.Set(widget.MenubarFill, mix(frameBG, frameFG, 4)),
		theme.Set(widget.SplitLine, mix(bg, fg, 12)),
		theme.Set(widget.TableHeader, dim),
		theme.Set(widget.TableCursor, alpha(accent, 0x40)),
		theme.Set(widget.TableStrong, accent),
		theme.Set(widget.PaletteHint, dim),
		theme.Set(widget.PaletteMark, alpha(accent, 0x50)),
		theme.Set(sidebarFill, mix(sideBG, sideFG, 4)),
		theme.Set(rowActive, alpha(accent, 0x40)),
		theme.Set(rowHover, alpha(sideFG, 0x14)),
		theme.Set(faint, mix(sideFG, sideBG, 45)),
		theme.Set(termBackground, bg),
	)
	// On stage, the terminal's own colours, and an accent that reads on
	// its ground.
	strong := standout(bg, accent, nrgba(pal.ANSI[14]), nrgba(pal.ANSI[11]))
	content := theme.Make(t.Name+" content",
		theme.Set(widget.Background, bg),
		theme.Set(widget.Ink, fg),
		theme.Set(widget.Accent, strong),
		theme.Set(widget.TableHeader, mix(fg, bg, 45)),
		theme.Set(widget.TableCursor, alpha(strong, 0x40)),
		theme.Set(widget.TableStrong, strong),
		theme.Set(widget.MenuBorder, mix(bg, fg, 20)),
		theme.Set(widget.FieldFill, mix(bg, fg, 6)),
		theme.Set(widget.FieldBorder, mix(bg, fg, 20)),
		theme.Set(widget.Placeholder, mix(fg, bg, 45)),
		theme.Set(widget.MenuFill, mix(bg, fg, 8)),
		theme.Set(faint, mix(fg, bg, 45)),
	)
	return themed{name: t.Name, theme: th, content: content, palette: pal}, nil
}

// registerThemes names gridterm's themes to the window, for the program
// to switch between.
func registerThemes(w *gunim.Window, all []themed) {
	for _, t := range all {
		w.RegisterTheme(t.theme)
	}
}
