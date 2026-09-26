package main

import (
	"errors"
	"slices"
	"strings"

	"github.com/marrasen/gunim/theme"

	"github.com/marrasen/gridterm/internal/build"
	"github.com/marrasen/gridterm/internal/update"
	"github.com/marrasen/gridterm/keys"
	"github.com/marrasen/gridterm/settings"
	"github.com/marrasen/gridterm/themes"
	"github.com/marrasen/gridterm/ui"
)

// The window's own files and commands, as gridterm has them: the
// shortcuts file, the themes file, checking for a newer release, and
// the list of every command.

// Intents for the window's files and commands.
type (
	// ReloadShortcuts reads the shortcuts file again.
	ReloadShortcuts struct{}
	// WriteShortcuts writes a shortcuts file holding Bindings, every
	// shortcut the window has now, for the user to start from.
	WriteShortcuts struct{ Bindings []ui.Binding }
	// ReloadThemes reads the themes file again.
	ReloadThemes struct{}
	// WriteThemeFile writes a themes file holding a copy of the theme
	// the window is drawn in, for the user to start from.
	WriteThemeFile struct{}
	// CheckUpdates asks whether a newer gridterm is out.
	CheckUpdates struct{}
	// ShowHelp opens the list of every command and its shortcut.
	ShowHelp struct{}
)

// kindHelp is the pane listing every command.
const kindHelp = "help"

// latestRelease asks for the newest release. A variable, so a test
// reaches no network.
var latestRelease = update.Latest

// loadShortcuts reads the user's shortcuts file, when there is one, for
// the window to take on.
func (a *app) loadShortcuts(said bool) error {
	dir, err := settings.Dir()
	if err != nil {
		return err
	}
	changes, err := keys.Load(keys.Path(dir))
	if err != nil {
		return err
	}
	a.st.Shortcuts = changes
	a.st.ShortcutsRead++
	if said {
		a.notify("Shortcuts read again", keys.Path(dir), "")
	}
	return nil
}

// writeShortcuts writes the starting shortcuts file.
func (a *app) writeShortcuts(have []ui.Binding) error {
	dir, err := settings.Dir()
	if err != nil {
		return err
	}
	at := keys.Path(dir)
	if err := keys.WriteStart(at, have); err != nil {
		return err
	}
	a.notify("Shortcuts file created", at+" holds every shortcut now. Edit it, then choose Options → Read Again → Shortcuts.", "")
	return nil
}

// writeThemeFile writes the starting themes file, a copy of the theme
// the window is drawn in.
func (a *app) writeThemeFile() error {
	dir, err := settings.Dir()
	if err != nil {
		return err
	}
	i := slices.IndexFunc(a.themes, func(t themed) bool { return t.name == a.st.Theme })
	if i < 0 {
		return errors.New("the window is drawn in a theme that is not in the list")
	}
	at := themes.Path(dir)
	if err := themes.WriteStart(at, a.themes[i].source); err != nil {
		return err
	}
	a.notify("Theme file created", at+" holds a copy of "+a.st.Theme+". Edit it, then choose Options → Read Again → Themes.", "")
	return nil
}

// reloadThemes reads the themes again, and draws the window in the one
// it is drawn in when it is still there.
func (a *app) reloadThemes() {
	all, err := loadThemesSaying()
	if err != nil {
		a.notify("Couldn't read all the themes", err.Error(), "")
	}
	a.themes = all
	if a.registerThemes != nil {
		a.registerThemes(a.themes)
	}
	a.st.Themes = nil
	contents := map[string]theme.Theme{}
	for _, t := range a.themes {
		a.st.Themes = append(a.st.Themes, t.name)
		contents[t.name] = t.content
	}
	a.st.Contents = contents
	name := a.st.Theme
	if !slices.Contains(a.st.Themes, name) && len(a.themes) > 0 {
		name = a.themes[0].name
	}
	a.pickTheme(name)
	a.notify("Themes read again", strings.Join(a.st.Themes, ", "), "")
}

// checkUpdates asks, in the background, whether a newer gridterm is
// out, and says what it found.
func (a *app) checkUpdates() {
	a.st.Status = "Looking for a newer gridterm…"
	go func() {
		newest, err := latestRelease(a.ctx)
		a.events <- func() {
			a.st.Status = ""
			if err != nil {
				a.notify("Couldn't check for a newer gridterm", err.Error(), "")
				return
			}
			have := build.Version()
			switch update.Against(have, newest.Version) {
			case update.Current:
				a.notify("gridterm is up to date", newest.Version+" is the newest release.", "")
			case update.Ahead:
				a.notify("This gridterm is newer than any release", "The newest release is "+newest.Version+".", "")
			default:
				page := newest.Page
				go func() {
					ans, err := a.ask(a.ctx, Ask{Title: "A newer gridterm is out", Text: newest.Version + " is the newest release; this one is " + have + ".\n\n" + page, Yes: "Open the Page", No: "Close"})
					if err == nil && ans.Yes {
						a.events <- func() {
							if err := openInBrowser(page); err != nil {
								a.notify("Couldn't open the page", err.Error(), "")
							}
						}
					}
				}()
			}
		}
	}()
}

// showHelp opens the list of every command, or goes to it.
func (a *app) showHelp() {
	for _, p := range a.st.Panes {
		if p.Kind == kindHelp {
			a.st.Focus = p.ID
			return
		}
	}
	a.next++
	a.addPane(Pane{ID: "p" + itoa(a.next), Title: "Shortcuts and Commands", Kind: kindHelp}, nil, placement{})
}
