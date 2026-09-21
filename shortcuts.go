package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/marrasen/gridterm/conf"
	"github.com/marrasen/gridterm/keys"
	"github.com/marrasen/gridterm/ui"
)

// keysCommand writes a starting shortcuts file, and keysTitle names
// the line that does it. keysReloadCommand reads the file again.
//
// Named shortcuts rather than keys for the same reason the ids are: a
// key is an SSH key here.
const (
	keysCommand       = "shortcuts.write"
	keysTitle         = "New Shortcuts File"
	keysReloadCommand = "shortcuts.reload"
	keysReloadTitle   = "Reload Shortcuts"
)

// reloadShortcuts reads the shortcuts file again and applies it.
//
// It starts from the keymap gridterm comes with rather than from the
// one on screen. Laying the changes on top of a keymap they have
// already changed would not give back the built-in chord of a line the
// user has since deleted.
//
// A file that cannot be used leaves the window on the shortcuts it had,
// not on the built-in ones: a reload that went wrong should take
// nothing away.
func (a *app) reloadShortcuts() error {
	if a.root.Accelerators == nil || a.root.Commands == nil {
		return errors.New("this window has no keys to change")
	}
	dir, err := a.shortcutDir()
	if err != nil {
		return err
	}
	changes, err := keys.Load(keys.Path(dir))
	if err != nil {
		return err
	}
	// Built to one side and only then taken on, so a file that cannot
	// be used leaves the window on the keys it had. Taken on rather
	// than swapped in, because the menu bar was handed this keymap when
	// it was built and keeps it: a new one would leave every menu
	// printing the chords of a keymap nothing runs.
	next := defaultShortcuts()
	if err := a.applyShortcutsTo(next, changes); err != nil {
		return err
	}
	a.root.Accelerators.Become(next)
	a.markDirty()
	// A line along the bottom rather than a dialog. It worked and there
	// is nothing to read: a dialog for it would cost a keypress to
	// dismiss, and the keypress is the whole of what it offers.
	a.say("Shortcuts reloaded")
	return nil
}

// loadShortcuts applies the shortcuts file to the keys the window starts
// with.
//
// It returns an error only when the file could not be read off the disk,
// which stops the window opening. A file read whole and found wrong is
// the user's to fix, so the window opens on the shortcuts gridterm comes
// with and a notice says which line is wrong.
func (a *app) loadShortcuts() error {
	say := func(title string, err error) {
		a.pump.post(func() { a.reportError(title, err) })
	}
	dir, err := a.shortcutDir()
	if err != nil {
		return err
	}
	changes, err := keys.Load(keys.Path(dir))
	if err != nil {
		if errors.Is(err, keys.ErrDisk) {
			return err
		}
		say("The keyboard shortcuts could not be read", err)
		return nil
	}
	if err := a.applyShortcuts(changes); err != nil {
		say("The keyboard shortcuts could not be read", err)
	}
	return nil
}

// applyShortcuts binds what the file says, and changes nothing when any
// line names a command the window does not have.
func (a *app) applyShortcuts(changes []keys.Change) error {
	if a.root.Accelerators == nil {
		return errors.New("this window has no keys to change")
	}
	return a.applyShortcutsTo(a.root.Accelerators, changes)
}

// applyShortcutsTo is applyShortcuts onto a keymap named, for a reload
// that builds one to one side before taking it on.
func (a *app) applyShortcutsTo(km *ui.Keymap, changes []keys.Change) error {
	if km == nil || a.root.Commands == nil {
		return errors.New("this window has no keys to change")
	}
	// Checked before any of it is applied, so a file naming a command
	// this gridterm does not have changes nothing.
	var unknown []string
	for i, c := range changes {
		// An id that has been renamed is followed to its new name, so a
		// file written before the rename goes on working. Done before
		// the check below, so a renamed id is not reported as unknown.
		if to, moved := keys.Renamed[c.Command]; moved {
			changes[i].Command = to
			c.Command = to
		}
		// A command built from the server list, the font scan or the shell
		// scan is registered after this runs, so its name is taken as
		// written rather than checked.
		if c.Command == "" || generatedCommand(c.Command) {
			continue
		}
		if _, have := a.root.Commands.Lookup(c.Command); !have {
			unknown = append(unknown, fmt.Sprintf("%s runs %q", c.Written, c.Command))
		}
	}
	if len(unknown) > 0 {
		return fmt.Errorf(
			"This gridterm has no command by these names:\n\n%s\n\n"+
				"None of the file was used, so this window has the shortcuts gridterm\n"+
				"comes with. %q on the Help menu lists what every command is called.",
			strings.Join(unknown, "\n"), helpTitle)
	}
	for _, c := range changes {
		if c.Command == "" {
			km.Unbind(c.Chord)
			continue
		}
		if err := km.Bind(c.Chord, c.Command); err != nil {
			return err
		}
	}
	return nil
}

// writeShortcutStart puts every shortcut the window has now into the
// shortcuts file, for a user with nowhere to start from.
func (a *app) writeShortcutStart() error {
	dir, err := a.shortcutDir()
	if err != nil {
		return err
	}
	if a.root.Accelerators == nil {
		return errors.New("this window has no keys to write down")
	}
	at := keys.Path(dir)
	if err := keys.WriteStart(at, a.root.Accelerators.Bindings()); err != nil {
		return err
	}
	// How overrides work is in the file itself, as a comment block at the
	// top: that is where somebody editing it is looking when they need
	// it, and a dialog they dismissed is a paragraph they cannot get back.
	n := a.newNotice("Shortcuts file created", at+"\n\n"+
		"Contains every current shortcut. Edit it, then\n"+
		"choose Options › Reload › Shortcuts.")
	// A path is not prose, and the dialog would re-wrap one at a space.
	n.Preformatted = true
	a.presentNotice(n)
	return nil
}

// shortcutDir is the directory the shortcuts file lives in. Empty means
// the one gridterm keeps its files in.
func (a *app) shortcutDir() (string, error) {
	if a.keysDir != "" {
		return a.keysDir, nil
	}
	return conf.Dir()
}
