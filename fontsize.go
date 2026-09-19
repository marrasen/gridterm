package main

import (
	"github.com/marrasen/gridterm/settings"
)

// fontPick is where the size the text is drawn at is kept between runs.
//
// The size itself lives on the window, because every frame reads it.
// This is only what remembers it.
type fontPick struct {
	// remembered is where the size is kept between runs. Nothing is
	// saved while it is nil, which is what a window with no settings
	// file behind it has.
	remembered *settings.Settings

	// fixed says a size was named on the command line. A size asked for
	// there is the size for that run, whatever the last run ended on.
	fixed bool
}

// newFontPick builds the picker. fixed says a size was named on the
// command line.
func newFontPick(fixed bool) *fontPick { return &fontPick{fixed: fixed} }

// remember gives the picker the settings it reads and writes.
func (p *fontPick) remember(set *settings.Settings) { p.remembered = set }

// chosen is the size the text was last drawn at, and whether there is
// one to open at.
func (p *fontPick) chosen() (float64, bool) {
	if p.remembered == nil || p.fixed {
		return 0, false
	}
	return p.remembered.FontSize()
}

// choose writes the size down, for the next run.
//
// A window with nowhere to write it does nothing and says nothing: there
// is no settings file behind it, which is not a failure to save. A save
// that was tried and failed is handed back.
func (p *fontPick) choose(pt float64) error {
	if p.remembered == nil {
		return nil
	}
	return p.remembered.PutFontSize(pt)
}

// useStartFontSize draws the window at the size the text was last drawn
// at.
//
// The atlas and the size itself, and no resize: the window has no grid
// yet, and how big it opens is worked out from the atlas once this has
// run. A size that the atlas will not take leaves the window at the one
// it opened with, and says so.
func (a *app) useStartFontSize() {
	pt, ok := a.font.chosen()
	if !ok || pt == a.fontSize {
		return
	}
	pt = min(max(pt, minFontSize), maxFontSize)
	if err := a.atlas.SetSize(pt); err != nil {
		a.logError(err)
		return
	}
	a.fontSize = pt
}
