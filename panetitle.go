package main

import (
	"errors"

	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/settings"
)

// paneTitles is whether each pane shows a line naming it, kept between
// runs.
type paneTitles struct {
	// remembered is where the answer is kept. Nothing is saved while it
	// is nil.
	remembered *settings.Settings
}

// newPaneTitles builds a switch that remembers nothing until it is given
// the settings.
func newPaneTitles() *paneTitles { return &paneTitles{} }

// remember gives the switch the settings it reads and writes.
func (p *paneTitles) remember(set *settings.Settings) { p.remembered = set }

// on reports whether the lines are showing.
func (p *paneTitles) on() bool {
	return p.remembered != nil && p.remembered.PaneTitles()
}

// set turns the lines on or off and writes the answer down.
func (p *paneTitles) set(on bool) error {
	if p.remembered == nil {
		return errNoSettingsForTitles
	}
	return p.remembered.PutPaneTitles(on)
}

// errNoSettingsForTitles is what a switch with no settings behind it
// answers.
var errNoSettingsForTitles = errors.New(
	"this window has no settings to keep the line above a pane in")

// togglePaneTitles turns the line above each pane on or off.
func (a *app) togglePaneTitles() error {
	if err := a.paneTitles.set(!a.paneTitles.on()); err != nil {
		return err
	}
	a.refreshCaptions()
	a.markDirty()
	return nil
}

// refreshCaptions puts the line above each pane in step with what the
// pane is and where it runs.
func (a *app) refreshCaptions() {
	on := a.paneTitles.on()
	for pane, e := range a.panes {
		if !on {
			pane.SetCaption("")
			continue
		}
		pane.SetCaption(paneCaption(e))
	}
}

// paneCaption is what a pane's line says: the machine it is on and what
// the program running in it calls itself.
func paneCaption(e *conns.Entry) string {
	where := groupName(e.Host)
	if e.Label == "" {
		return where
	}
	return where + ": " + e.Label
}
