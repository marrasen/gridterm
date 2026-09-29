// Package look is how kakel looks: its themes, made from terminal
// themes, and the colours of the window's own parts that a theme sets.
package look

import (
	"errors"
	"fmt"
	"image/color"
	"math"
	"strings"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/theme"
	"github.com/marrasen/gunim/widget"

	"github.com/marrasen/kakel/grid"
	"github.com/marrasen/kakel/settings"
	"github.com/marrasen/kakel/themes"
	"github.com/marrasen/kakel/vt"
)

// kakel's themes, turned into gunim's. A theme is a terminal palette
// with a ground and a text colour at its two ends; the window's own
// surfaces are worked out from those, the ground a step towards the
// text, or taken from the frame a theme writes down. Switching themes
// fades every colour across, the terminals' included.

// TermBackground is a terminal's ground, where its cells leave it clear.
// The window's own colours, which a theme sets.
var (
	SidebarFill = theme.Color("kakel.sidebar", color.NRGBA{R: 0x1b, G: 0x1e, B: 0x26, A: 0xff})
	RowActive   = theme.Color("kakel.row.active", color.NRGBA{R: 0x5e, G: 0x9c, B: 0xff, A: 0x40})
	RowHover    = theme.Color("kakel.row.hover", color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0x12})
	Faint       = theme.Color("kakel.faint", color.NRGBA{R: 0x8a, G: 0x93, B: 0xa6, A: 0xff})
	// RowActiveInk is the words of the sidebar's row for whatever is in
	// front: the theme's currentFG, where it wrote a frame down.
	RowActiveInk = theme.Foreground("kakel.row.active.ink", color.NRGBA{R: 0xe6, G: 0xe9, B: 0xef, A: 0xff})
)

var TermBackground = theme.Color("kakel.background", color.NRGBA{R: 0x14, G: 0x17, B: 0x1c, A: 0xff})

// Themed is a kakel theme ready for the window: gunim's theme, and
// the palette the terminals draw with.
type Themed struct {
	Name  string
	Theme theme.Theme
	// Content is what the panes on stage wear: the terminal's own text
	// and ground, which a theme's frame may not share, as Turbo's grey
	// frame round its blue ground does not.
	Content theme.Theme
	Palette vt.Palette
	// Source is the kakel theme it came from, for writing a copy.
	Source themes.Theme
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

// Load returns the themes on offer, ready: kakel's own and the
// user's, from the themes file kakel reads, or kakel's own alone
// when there is no file to read.
func Load() []Themed {
	all, _ := LoadSaying()
	return all
}

// LoadSaying is Load, with what went wrong on the way: a
// themes file that could not be found or read, which leaves the ones
// built in, and each theme in it that would not draw, which is left
// out. Said rather than dropped: the reason is what tells the user to
// go and fix the file.
func LoadSaying() ([]Themed, error) {
	all := themes.Built()
	var trouble []error
	dir, err := settings.Dir()
	if err != nil {
		trouble = append(trouble, fmt.Errorf("could not find the themes: %w", err))
	} else {
		read, err := themes.Load(themes.Path(dir))
		switch {
		case err != nil:
			trouble = append(trouble, fmt.Errorf("could not read the themes: %w", err))
		case len(read) > 0:
			all = read
		}
	}
	var out []Themed
	for _, t := range all {
		th, err := Of(t)
		if err != nil {
			trouble = append(trouble, fmt.Errorf("the theme %q: %w", t.Name, err))
			continue
		}
		out = append(out, th)
	}
	if len(out) == 0 {
		// Nothing drew: the built-in ones, which always do.
		for _, t := range themes.Built() {
			if th, err := Of(t); err == nil {
				out = append(out, th)
			}
		}
	}
	return out, errors.Join(trouble...)
}

func nrgba(c color.RGBA) color.NRGBA { return color.NRGBA{R: c.R, G: c.G, B: c.B, A: 0xff} }

// mix goes from a towards b by pct percent.
func mix(a, b color.NRGBA, pct int) color.NRGBA {
	m := func(x, y uint8) uint8 { return uint8((int(x)*(100-pct) + int(y)*pct) / 100) }
	return color.NRGBA{R: m(a.R, b.R), G: m(a.G, b.G), B: m(a.B, b.B), A: 0xff}
}

func alpha(c color.NRGBA, a uint8) color.NRGBA { c.A = a; return c }

// echoOf is the echo's look under t: the colours its block names, the
// rest from the palette, and its strength.
func echoOf(t themes.Theme, pal vt.Palette) ([]theme.Entry, error) {
	var e themes.Echo
	if t.Echo != nil {
		e = *t.Echo
	}
	fg, bg := nrgba(pal.FG), nrgba(pal.BG)
	tones := []struct {
		name  string
		raw   string
		def   color.NRGBA
		token theme.Token[color.NRGBA]
	}{
		{"Problem", e.Problem, nrgba(pal.ANSI[9]), widget.EchoProblem},
		{"Done", e.Done, nrgba(pal.ANSI[10]), widget.EchoDone},
		{"Call", e.Call, nrgba(pal.ANSI[11]), widget.EchoCall},
		{"Wait", e.Wait, mix(fg, bg, 35), widget.EchoWait},
	}
	out := make([]theme.Entry, 0, len(tones)+1)
	for _, tone := range tones {
		c := tone.def
		if strings.TrimSpace(tone.raw) != "" {
			read, err := themes.ParseColour(tone.raw)
			if err != nil {
				return nil, fmt.Errorf("%s: Echo: %s: %w", t.Name, tone.name, err)
			}
			c = nrgba(read)
		}
		out = append(out, theme.Set(tone.token, c))
	}
	if e.Strength != nil {
		if *e.Strength < 0 {
			return nil, fmt.Errorf("%s: Echo: Strength is %v, and it runs from 0, which turns the echo off, upwards", t.Name, *e.Strength)
		}
		out = append(out, theme.Set(widget.EchoStrength, float32(*e.Strength)))
	}
	return out, nil
}

// Of turns a kakel theme into gunim's.
func Of(t themes.Theme) (Themed, error) {
	pal, err := t.Palette()
	if err != nil {
		return Themed{}, err
	}
	look, err := t.Look()
	if err != nil {
		return Themed{}, err
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
	primaryInk := color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	borderLines := float32(1)
	var buttonShadow color.NRGBA
	activeInk := sideFG
	if look.Set {
		buttonBG, buttonFG = nrgba(look.ButtonBG), nrgba(look.ButtonFG)
		primaryBG, primaryInk = nrgba(look.ActiveBG), nrgba(look.ActiveFG)
		// Drawn as a text screen's boxes: a black shadow under each
		// button, as the old app cast, and a double rule if asked for.
		buttonShadow = color.NRGBA{A: 0xff}
		if look.Double {
			borderLines = 2
		}
		activeInk = nrgba(look.CurrentFG)
	}
	surface := mix(frameBG, frameFG, 7)
	rule := mix(frameBG, frameFG, 20)
	dim := mix(frameFG, frameBG, 45)
	echo, err := echoOf(t, pal)
	if err != nil {
		return Themed{}, err
	}
	th := theme.Make(t.Name, append(echo,
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
		theme.Set(widget.ButtonPrimaryInk, primaryInk),
		theme.Set(widget.ButtonShadow, buttonShadow),
		theme.Set(widget.DialogBorderLines, borderLines),
		theme.Set(widget.ButtonPrimaryHover, mix(primaryBG, color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}, 12)),
		theme.Set(widget.TooltipFill, alpha(frameFG, 0xf4)),
		theme.Set(widget.TooltipInk, frameBG),
		theme.Set(widget.MenubarFill, mix(frameBG, frameFG, 4)),
		theme.Set(widget.SplitLine, mix(bg, fg, 12)),
		theme.Set(widget.ProgressTrack, mix(surface, frameFG, 14)),
		theme.Set(widget.TableHeader, dim),
		theme.Set(widget.TableCursor, alpha(accent, 0x40)),
		theme.Set(widget.TableStrong, accent),
		theme.Set(widget.PaletteHint, dim),
		theme.Set(widget.PaletteMark, alpha(accent, 0x50)),
		theme.Set(SidebarFill, mix(sideBG, sideFG, 4)),
		theme.Set(RowActive, alpha(accent, 0x40)),
		theme.Set(RowActiveInk, activeInk),
		theme.Set(RowHover, alpha(sideFG, 0x14)),
		theme.Set(Faint, mix(sideFG, sideBG, 45)),
		theme.Set(TermBackground, bg),
	)...)
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
		theme.Set(widget.ProgressTrack, mix(bg, fg, 14)),
		theme.Set(widget.Placeholder, mix(fg, bg, 45)),
		theme.Set(widget.MenuFill, mix(bg, fg, 8)),
		theme.Set(Faint, mix(fg, bg, 45)),
	)
	return Themed{Name: t.Name, Theme: th, Content: content, Palette: pal, Source: t}, nil
}

// Register names kakel's themes to the window, for the program
// to switch between.
func Register(w *gunim.Window, all []Themed) {
	for _, t := range all {
		w.RegisterTheme(t.Theme)
	}
}

// Marks are the colours of the rings, from the theme's terminal
// colours: an agent's, and another window's.
type Marks struct{ Agent, Watched color.NRGBA }

// MarksOf are the rings' colours in a palette.
func MarksOf(p vt.Palette) Marks {
	// Lifted towards whichever of black and white the ground is not.
	black, white := color.RGBA{A: 0xff}, color.RGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	far := black
	if grid.Contrast(white, p.BG) >= grid.Contrast(black, p.BG) {
		far = white
	}
	nrgba := func(c color.RGBA) color.NRGBA { return color.NRGBA{R: c.R, G: c.G, B: c.B, A: 0xff} }
	return Marks{Agent: nrgba(p.ANSI[6]), Watched: nrgba(grid.Blend(p.ANSI[9], far, 2, 5))}
}
