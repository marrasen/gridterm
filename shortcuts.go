package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/marrasen/gridterm/conf"
	"github.com/marrasen/gridterm/keys"
)

// keysCommand writes a starting shortcuts file, and keysTitle names the
// line that does it.
const (
	keysCommand = "keys.start"
	keysTitle   = "Write a starting keyboard shortcuts file"
)

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
	if a.root.Accelerators == nil || a.root.Commands == nil {
		return errors.New("this window has no keys to change")
	}
	// Checked before any of it is applied, so a file naming a command
	// this gridterm does not have changes nothing.
	var unknown []string
	for _, c := range changes {
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
			a.root.Accelerators.Unbind(c.Chord)
			continue
		}
		if err := a.root.Accelerators.Bind(c.Chord, c.Command); err != nil {
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
	a.showNotice("Wrote "+at, "It holds every shortcut this window has now.\n\n"+
		"The file says what to change, not what the whole window does. Add a line\n"+
		"to put a command on another chord. To move it, set the old chord to \""+
		keys.Nothing+"\"\nas well, or the command runs on both. Delete a line and "+
		"that chord goes back\nto what gridterm comes with.\n\n"+
		"Every chord in the file runs before the pane sees it, so a chord a program\n"+
		"in the pane needs stops reaching it.\n\n"+
		"Start gridterm again to use the file.", false)
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
