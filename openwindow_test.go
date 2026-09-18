package main

import (
	"os"
	"strings"
	"testing"

	"golang.org/x/image/font/gofont/gomono"

	"github.com/marrasen/gridterm/glyph"
	"github.com/marrasen/gridterm/input"
	"github.com/marrasen/gridterm/keys"
	"github.com/marrasen/gridterm/session"
	"github.com/marrasen/gridterm/settings"
	"github.com/marrasen/gridterm/themes"
	"github.com/marrasen/gridterm/ui"
)

// aRealWindow opens a window the way main does, with the files it reads
// in a directory the test owns and no shell of its own.
//
// It is the only test that goes through openWindow. Everything else
// builds the window by hand, which is what let a feature be left
// unwired with nothing noticing.
func aRealWindow(t *testing.T) *app {
	t.Helper()
	withHome(t)
	return openWindowIn(t)
}

// A window opens with everything it reads wired into it.
//
// Each of these was a line in main that no test walked, and cutting any
// of them left the whole feature gone with the suite still green.
func TestOpeningAWindowReadsTheColourSchemes(t *testing.T) {
	withHome(t)
	// A scheme of the user's own, which is the only way to tell the file
	// was read: the built-in ones are there before it is opened.
	dir, err := settings.Dir()
	if err != nil {
		t.Fatalf("where the files go: %v", err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("make it: %v", err)
	}
	body := `{"version":1,"themes":[{"name":"Mine","fg":"#fff","bg":"#000",` +
		`"ansi":["#000","#100","#200","#300","#400","#500","#600","#700",` +
		`"#800","#900","#a00","#b00","#c00","#d00","#e00","#f00"]}]}`
	if err := os.WriteFile(themes.Path(dir), []byte(body), 0o600); err != nil {
		t.Fatalf("write the schemes: %v", err)
	}

	a := openWindowIn(t)

	if _, have := themes.Named(a.theme.all(), "Mine"); !have {
		t.Errorf("the window offers %v, want the scheme the file holds",
			themes.Names(a.theme.all()))
	}
	if a.theme.showing == "" {
		t.Error("the window is drawn in no scheme at all")
	}
}

// The keyboard shortcuts file is read, so a shortcut the user moved is
// where they put it.
func TestOpeningAWindowReadsTheShortcutsFile(t *testing.T) {
	withHome(t)
	dir, err := settings.Dir()
	if err != nil {
		t.Fatalf("where the files go: %v", err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("make it: %v", err)
	}
	body := `{"version":1,"keys":{"ctrl+shift+O":"palette.open"}}`
	if err := os.WriteFile(keys.Path(dir), []byte(body), 0o600); err != nil {
		t.Fatalf("write the shortcuts: %v", err)
	}

	a := openWindowIn(t)

	moved := ui.Chord{Key: input.KeyO, Mods: input.ModCtrl | input.ModShift}
	if got, on := a.root.Accelerators.Lookup(moved); !on || got != "palette.open" {
		t.Errorf("ctrl+shift+O runs %q (%v), want the command the file put there", got, on)
	}
}

// A window that was serving offers the port again, which is a dialog on
// the first frame rather than anything the window holds.
func TestOpeningAWindowOffersToServeAgain(t *testing.T) {
	withHome(t)
	set := settingsInHome(t)
	if err := set.PutServeOn(true); err != nil {
		t.Fatalf("remember that it served: %v", err)
	}

	a := openWindowIn(t)
	a.pump.run()

	f, is := a.root.Modal().(*ui.Form)
	if !is {
		t.Fatalf("the window opened %T, want the offer", a.root.Modal())
	}
	if got := f.Title; got != serveAgainTitle {
		t.Errorf("it opened %q", got)
	}
}

// The window can run everything the menus name, and the shortcuts the
// keymap holds reach a command that is registered.
func TestOpeningAWindowRegistersEveryCommandItsKeysName(t *testing.T) {
	a := aRealWindow(t)

	if a.root.Commands == nil || a.root.Accelerators == nil {
		t.Fatal("the window has no commands or no keys")
	}
	for _, b := range a.root.Accelerators.Bindings() {
		if generatedCommand(b.ID) {
			continue
		}
		if _, have := a.root.Commands.Lookup(b.ID); !have {
			t.Errorf("%s runs %q, which is not a command this window has", b.Chord, b.ID)
		}
	}
	// And the switcher, which is reached by a key and by a menu line.
	if _, have := a.root.Commands.Lookup(switcherCommand); !have {
		t.Errorf("the window cannot %q", switcherCommand)
	}
	if _, on := a.root.Accelerators.ChordFor(switcherCommand); !on {
		t.Errorf("%q has no key", switcherCommand)
	}
}

// The tree is built and the window opens on a pane.
func TestOpeningAWindowBuildsTheTree(t *testing.T) {
	a := aRealWindow(t)

	for _, part := range []struct {
		what string
		got  any
	}{
		{"the sidebar", a.panel},
		{"the sidebar's rows", a.side},
		{"the stage", a.stage},
		{"the dock", a.dock},
		{"the menu bar", a.bar},
		{"the widget tree", a.root.Widget()},
	} {
		if part.got == nil {
			t.Errorf("the window has no %s", part.what)
		}
	}
	if len(a.panes) != 1 {
		t.Errorf("the window opened with %d panes, want the one", len(a.panes))
	}
}

// And a frame runs, which is what says the whole of it holds together.
func TestARealWindowRunsAFrame(t *testing.T) {
	a := aRealWindow(t)

	if err := a.Update(); err != nil {
		t.Fatalf("a frame: %v", err)
	}
}

// openWindowIn opens a window in the home a test has already set up,
// with a shell that is a pair of pipes rather than a program.
func openWindowIn(t *testing.T) *app {
	t.Helper()
	atlas, err := glyph.NewAtlas(glyph.Fonts{Regular: gomono.TTF}, 12, 96)
	if err != nil {
		t.Fatalf("an atlas: %v", err)
	}
	a, err := openWindow(wanted{
		atlas:    atlas,
		fontSize: 12,
		newShell: func([]string, string, int, int) (session.Session, error) {
			return newPipeSession(), nil
		},
	})
	if err != nil {
		t.Fatalf("open the window: %v", err)
	}
	t.Cleanup(func() { _ = a.shutDown(nil) })
	return a
}

// settingsInHome is the settings file of the home a test set up.
func settingsInHome(t *testing.T) *settings.Settings {
	t.Helper()
	path, err := settings.Path()
	if err != nil {
		t.Fatalf("where the settings go: %v", err)
	}
	if err := os.MkdirAll(strings.TrimSuffix(path, settings.File), 0o700); err != nil {
		t.Fatalf("make the directory: %v", err)
	}
	set, err := settings.Load(path)
	if err != nil {
		t.Fatalf("read the settings: %v", err)
	}
	return set
}
