package main

import (
	"image/color"

	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/ui"
)

// The window's own furniture: the menu bar, the sidebar, a dialog and a
// menu. A theme that wrote its frame down says what these are. Every
// other theme has them derived from its two ends, which is what the
// window always did.

// frameFG is what the furniture is written in.
func (a *app) frameFG() color.RGBA {
	if a.look.Set {
		return a.look.FG
	}
	return a.colours.FG
}

// panelBG is the ground a dialog or a menu paints itself on. Nothing at
// all unless the theme asked for a flat one, so the frosted glass behind
// it shows through.
func (a *app) panelBG() color.RGBA {
	if a.look.Set {
		return a.look.BG
	}
	return color.RGBA{}
}

// panelRule is the kind of rule drawn round a dialog or a menu.
func (a *app) panelRule() ui.Border {
	if a.look.Double {
		return ui.BorderDouble
	}
	return ui.BorderSingle
}

// panelBorderFG is what that rule is drawn in.
func (a *app) panelBorderFG() color.RGBA {
	if a.look.Set {
		return a.look.FG
	}
	return a.colours.ANSI[8]
}

// panelShadow is what a dialog or a menu lays over the cells below and
// to the right of it. A flat theme casts a solid one, because a box with
// no glass in it has no light to let through either.
func (a *app) panelShadow() color.RGBA {
	if a.look.Set {
		return color.RGBA{A: 0xff}
	}
	return shadow
}

// panelDimFG is a label, a hint or a note on a dialog: the furniture's
// own text faded towards its ground.
func (a *app) panelDimFG() color.RGBA {
	if a.look.Set {
		return grid.Blend(a.look.FG, a.look.BG, 1, 3)
	}
	return a.colours.ANSI[8]
}

// panelFieldBG is the ground a field or a resting button sits on, a step
// off the dialog's own so it reads as something to type in.
func (a *app) panelFieldBG() color.RGBA {
	if a.look.Set {
		return grid.Blend(a.look.BG, a.look.FG, 1, 6)
	}
	return a.colours.Surface()
}

// buttonFG and buttonBG are a button, and activeFG and activeBG the one
// Enter would press.
func (a *app) buttonFG() color.RGBA {
	if a.look.Set {
		return a.look.ButtonFG
	}
	return a.colours.FG
}

func (a *app) buttonBG() color.RGBA {
	if a.look.Set {
		return a.look.ButtonBG
	}
	return a.colours.Surface()
}

func (a *app) activeFG() color.RGBA {
	if a.look.Set {
		return a.look.ActiveFG
	}
	return a.colours.BG
}

func (a *app) activeBG() color.RGBA {
	if a.look.Set {
		return a.look.ActiveBG
	}
	return a.colours.FG
}

// frameDimFG is a note beside a row on the window's frame: the frame's
// own text faded towards its ground.
func (a *app) frameDimFG() color.RGBA {
	if a.look.Set {
		return grid.Blend(a.look.FG, a.look.BG, 1, 3)
	}
	return a.colours.ANSI[8]
}

// onFrame lifts a colour from the window's palette until it can be read
// on the frame's ground, by moving it towards the frame's own text.
//
// The numbered colours are picked to read on the window's ground. A
// theme that wrote its frame down has a second ground the window draws
// on, and a colour that stood out on the first can be lost on it.
func (a *app) onFrame(c color.RGBA) color.RGBA {
	if !a.look.Set {
		return c
	}
	const least = 3.0
	for at := range onFrameSteps {
		got := grid.Blend(c, a.look.FG, at, onFrameSteps)
		if grid.Contrast(got, a.look.BG) >= least {
			return got
		}
	}
	return a.look.FG
}

// onFrameSteps is how finely a colour is moved towards the frame's text:
// enough to keep what is left of the hue, few enough to stop quickly.
const onFrameSteps = 8

// headingFG names a machine on the sidebar, in the cyan a machine is
// said in elsewhere.
func (a *app) headingFG() color.RGBA { return a.onFrame(a.colours.ANSI[6]) }

// fillBG is how far a copy has got, drawn on the sidebar's own ground
// and a step further along the line that ground is shaded on.
func (a *app) fillBG() color.RGBA {
	if a.look.Set {
		return grid.Blend(a.look.BG, a.look.FG, 1, 4)
	}
	return grid.Blend(a.colours.BG, a.colours.ANSI[4], 1, 3)
}

// currentBG marks the row for whatever is in front, lifted off the
// sidebar's own ground rather than the window's selection colour.
func (a *app) currentBG() color.RGBA {
	if a.look.Set {
		return grid.Blend(a.look.BG, a.look.FG, 1, 6)
	}
	return grid.Blend(a.colours.BG, a.colours.FG, 1, 6)
}

// disabledFG is a menu line naming a command nothing registered. Panes
// register commands as they open, so a menu written once can hold lines
// that are not always there.
func (a *app) disabledFG() color.RGBA {
	if a.look.Set {
		return grid.Blend(a.look.FG, a.look.BG, 2, 3)
	}
	return a.colours.Selection
}
