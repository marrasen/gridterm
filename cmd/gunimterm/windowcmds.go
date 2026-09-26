package main

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/marrasen/gunim/theme"

	"github.com/marrasen/gridterm/conf"
	"github.com/marrasen/gridterm/internal/build"
	"github.com/marrasen/gridterm/internal/update"
	"github.com/marrasen/gridterm/keys"
	"github.com/marrasen/gridterm/remote"
	"github.com/marrasen/gridterm/serve"
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
	// MakePortable makes the folder beside the program and copies the
	// files it reads now into it, for a copy that carries its own.
	MakePortable struct{}
	// ShowHelp opens the list of every command and its shortcut.
	ShowHelp struct{}
)

// kindHelp is the pane listing every command.
const kindHelp = "help"

// latestRelease asks for the newest release. A variable, so a test
// reaches no network.
var latestRelease = update.Latest

// thisVersion is what this build calls itself. A variable, so a test
// can be a release: the tests run from a working tree, where every
// build is a development build.
var thisVersion = build.Version

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
	a.st.ShortcutsAgain = said
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
	if a.checking {
		// One question is already out, and it has one answer however
		// many times the button is pressed while it is on its way.
		return
	}
	a.checking = true
	a.st.Status = "Checking for updates…"
	go func() {
		newest, err := latestRelease(a.ctx)
		a.events <- func() {
			a.checking = false
			a.st.Status = ""
			if err != nil {
				a.notify("Could not check for updates", err.Error(), "")
				return
			}
			have := thisVersion()
			switch update.Against(have, newest.Version) {
			case update.Behind:
				a.offerRelease("Update available", have, newest)
			case update.Current:
				a.notify(newest.Version+" is the newest release", "", "")
			case update.Ahead:
				a.notify("This build is later than the newest release, "+newest.Version, "", "")
			default:
				// A build from a working tree, which has no order against
				// a release: it may hold work no release has. Both
				// versions are named and the choice is the reader's.
				a.offerRelease("Newest release", have, newest)
			}
		}
	}()
}

// offerRelease names both versions and the page the newer one is on.
// Enter closes it: opening a browser is a thing to choose.
func (a *app) offerRelease(title, have string, newest update.Release) {
	page := newest.Page
	go func() {
		ans, err := a.ask(a.ctx, Ask{Title: title, Text: newest.Version + " is the newest release; this build is " + have + ".\n\n" + page,
			Yes: "Open the Page", No: "Close", Careful: true})
		if err == nil && ans.Yes {
			a.events <- func() {
				if err := openInBrowser(page); err != nil {
					a.notify("Could not open the download page", err.Error(), "")
				}
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

// ownFiles are the files a copy carrying its own takes with it, beside
// the serving key.
var ownFiles = []string{
	settings.File, remote.BookFile, themes.File, keys.File,
	serve.AuthFile, knownWindowsFile,
}

// makePortable copies the files the window reads now into the folder
// beside the program, and lists what it did. The window reads them
// once it is started again: where the files are is decided as it
// opens.
func (a *app) makePortable() {
	said, err := func() ([]string, error) {
		exe, err := os.Executable()
		if err != nil {
			return nil, fmt.Errorf("where this copy of %s is: %w", programName, err)
		}
		dir, err := conf.Dir()
		if err != nil {
			return nil, err
		}
		key, err := serve.HostKeyPath()
		if err != nil {
			return nil, err
		}
		return conf.CarryOwn(exe, dir, ownFiles, key)
	}()
	if err != nil {
		a.notify("Could not make portable", err.Error(), "")
		return
	}
	said = append(said, "", "Restart "+programName+" to use them.")
	go func() {
		_, _ = a.ask(a.ctx, Ask{Title: "Made Portable", Text: strings.Join(said, "\n"), Plain: true})
	}()
}
