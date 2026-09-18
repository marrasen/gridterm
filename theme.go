package main

import (
	"errors"
	"fmt"

	"github.com/marrasen/gridterm/settings"
	"github.com/marrasen/gridterm/themes"
	"github.com/marrasen/gridterm/ui"
)

// themePick is the colour theme the window is drawn in: the themes it
// can offer, and which one is chosen.
type themePick struct {
	// have are the themes built in, with the user's own after them.
	have []themes.Theme

	// showing is the theme the window is drawn in now, which is not
	// always the one written down: a choice that could not be saved, or
	// a remembered name that has gone from the file, leave the two
	// apart.
	showing string

	// remembered is where the choice is kept between runs. Nothing is
	// saved while it is nil.
	remembered *settings.Settings

	// dir is the directory the themes file lives in. Empty means the one
	// the settings are in.
	dir string
}

// newThemePick builds a picker holding the themes gridterm comes with.
func newThemePick() *themePick { return &themePick{have: themes.Built()} }

// remember gives the picker the settings it reads and writes.
func (p *themePick) remember(set *settings.Settings) { p.remembered = set }

// at points the picker at the directory the themes file lives in.
func (p *themePick) at(dir string) { p.dir = dir }

// where is the directory the themes file lives in.
func (p *themePick) where() (string, error) {
	if p.dir != "" {
		return p.dir, nil
	}
	return settings.Dir()
}

// take keeps the themes read from a file, over the built-in ones.
func (p *themePick) take(all []themes.Theme) { p.have = all }

// all are the themes on offer.
func (p *themePick) all() []themes.Theme { return p.have }

// chosen is the theme the window is drawn in, and whether one was
// picked.
func (p *themePick) chosen() (string, bool) {
	if p.remembered == nil {
		return "", false
	}
	return p.remembered.Theme()
}

// choose writes down which theme was picked, for the next run.
func (p *themePick) choose(name string) error {
	if p.remembered == nil {
		return errNoSettingsForTheme
	}
	return p.remembered.PutTheme(name)
}

// errNoSettingsForTheme is what a picker with no settings behind it
// answers.
var errNoSettingsForTheme = errors.New("this window has no settings to keep a theme in")

// themeTitle names the dialog that picks a theme.
const themeTitle = "Colour theme"

// startTheme is the theme the window opens on: the one remembered, and
// the first built in when nothing was picked or what was picked has
// since gone from the file.
func (a *app) startTheme() themes.Theme {
	all := a.theme.all()
	if name, picked := a.theme.chosen(); picked {
		if t, ok := themes.Named(all, name); ok {
			return t
		}
		// Said rather than swapped quietly: a window that came up in
		// another theme with no word about it reads as gridterm having
		// forgotten.
		a.logError(fmt.Errorf("the theme %q is not in the list any more, so this window is %q",
			name, all[0].Name))
	}
	return all[0]
}

// useTheme draws the window in a theme, from this frame on.
func (a *app) useTheme(t themes.Theme) error {
	pal, err := t.Palette()
	if err != nil {
		return err
	}
	look, err := t.Look()
	if err != nil {
		return err
	}
	a.colours, a.look = pal, look
	a.theme.showing = t.Name
	a.restyle()
	return nil
}

// restyle gives every long-lived widget the window's colours again.
//
// A dialog, a menu and the list a walk shows are built when they are
// wanted, so they take the theme without being told. These are the ones
// that outlive it.
func (a *app) restyle() {
	if a.g != nil {
		a.g.DefaultFG, a.g.DefaultBG = a.colours.FG, a.colours.BG
		a.g.SelectionBG = a.colours.Selection
		a.g.MarkAllDirty()
	}
	if a.sideRegion != nil {
		a.sideRegion.g.DefaultFG, a.sideRegion.g.DefaultBG = a.colours.FG, a.colours.BG
		a.sideRegion.g.MarkAllDirty()
	}
	if a.panel != nil {
		a.panel.Style = a.panelStyle()
	}
	if a.bar != nil {
		a.bar.Style = a.menubarStyle()
		a.bar.MenuStyle = a.menuStyle()
		// The chips are built only when what they say changes, and a
		// theme is not part of that. Forgotten here so the next frame
		// builds them again.
		a.statusWas = statusKey{}
		a.updateStatus()
	}
	if a.side != nil {
		a.side.FG = a.headingFG()
		a.side.BG = a.panelStyle().BGEnd
	}
	if a.dock != nil {
		a.dock.DividerBG = a.colours.BG
	}
	// Every split already made. A window where only the newest divider
	// had the theme would have two kinds of divider in it.
	for _, w := range ui.Leaves(a.root.Widget()) {
		for at := ui.ParentOf(a.root.Widget(), w); at != nil; at = ui.ParentOf(a.root.Widget(), at) {
			if s, is := at.(*ui.Split); is {
				s.DividerFG, s.DividerBG = a.colours.FG, a.colours.BG
			}
		}
	}
	for _, s := range a.scaled {
		s.g.DefaultFG, s.g.DefaultBG = a.colours.FG, a.colours.BG
	}
	if a.files != nil {
		a.files.view.Style = a.paneStyle()
		for _, p := range a.files.view.Panes() {
			p.Style = a.paneStyle()
		}
	}
	for pane := range a.panes {
		pane.SetPalette(a.colours)
	}
	a.markDirty()
}

// openThemePick offers the colour themes and draws the window in the
// one chosen.
func (a *app) openThemePick() error {
	var hide func()
	c := ui.NewChooser(themeTitle, func() {
		if hide != nil {
			hide()
		}
	})
	c.Style = a.chooserStyle()
	was := a.theme.showing
	for _, t := range a.theme.all() {
		note := ""
		if t.Name == was {
			note = "showing"
		}
		c.Add(t.Name, note, func() error { return a.takeTheme(t) })
	}
	hide = a.showModal(c, nil)
	if a.root.Modal() != ui.Widget(c) {
		return errors.New("there is no room to ask")
	}
	a.markDirty()
	return nil
}

// takeTheme draws the window in a theme and writes the choice down.
func (a *app) takeTheme(t themes.Theme) error {
	if err := a.useTheme(t); err != nil {
		return err
	}
	// Written down once it has been drawn: a theme whose colours will
	// not read is not one to come back to on the next run.
	return a.theme.choose(t.Name)
}

// loadThemes reads the user's own themes and puts them after the ones
// gridterm comes with.
//
// A file that cannot be read leaves the built-in ones and says so: a
// window with nowhere to start would be worse than one whose own theme
// is missing, and the reason is what tells the user to go and fix it.
func (a *app) loadThemes() {
	dir, err := a.theme.where()
	if err != nil {
		a.pump.post(func() { a.reportError("The colour themes could not be found", err) })
		return
	}
	all, err := themes.Load(themes.Path(dir))
	a.theme.take(all)
	if err != nil {
		a.pump.post(func() { a.reportError("The colour themes could not be read", err) })
	}
}

// reloadThemes reads the themes file again and draws the window in
// whatever it says now, so a theme can be written with the window open.
func (a *app) reloadThemes() error {
	a.loadThemes()
	showing := a.theme.showing
	if t, ok := themes.Named(a.theme.all(), showing); ok {
		return a.useTheme(t)
	}
	return a.useTheme(a.startTheme())
}

// writeThemeStart puts the theme the window is drawn in into the themes
// file, for a user with nowhere to start from.
//
// It refuses a file that is already there. What is in one is the user's,
// and sixteen colours cannot be got back.
func (a *app) writeThemeStart() error {
	dir, err := a.theme.where()
	if err != nil {
		return err
	}
	at := themes.Path(dir)
	showing, ok := themes.Named(a.theme.all(), a.theme.showing)
	if !ok {
		return fmt.Errorf("this window is drawn in %q, which is not in the list", a.theme.showing)
	}
	if err := themes.WriteStart(at, showing); err != nil {
		return err
	}
	a.showNotice("Wrote "+at, "It holds a copy of "+showing.Name+" under another name.\n\n"+
		"Edit the colours in it and take \"Reload colour themes\" to see them.", false)
	return nil
}
