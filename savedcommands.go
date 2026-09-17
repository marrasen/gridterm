package main

import (
	"errors"
	"strings"

	"github.com/marrasen/gridterm/settings"
)

// mostSavedCommands is how many commands are kept. Saving one past that
// drops the one saved longest ago.
const mostSavedCommands = 50

// savedCommands are the command lines the user asked to keep, offered by
// the dialog that runs a command.
type savedCommands struct {
	// remembered is where they are kept between runs. Nothing is saved
	// while it is nil.
	remembered *settings.Settings
}

// newSavedCommands builds a list that remembers nothing until it is
// given the settings.
func newSavedCommands() *savedCommands { return &savedCommands{} }

// remember gives the list the settings it reads and writes.
func (c *savedCommands) remember(set *settings.Settings) { c.remembered = set }

// all is every saved command, newest first.
func (c *savedCommands) all() []settings.SavedCommand {
	if c.remembered == nil {
		return nil
	}
	return c.remembered.Commands()
}

// lines are the saved command lines, newest first, for a field to offer.
func (c *savedCommands) lines() []string {
	saved := c.all()
	out := make([]string, 0, len(saved))
	for _, cmd := range saved {
		out = append(out, cmd.Line)
	}
	return out
}

// find is the saved command with this line, and whether there is one.
func (c *savedCommands) find(line string) (settings.SavedCommand, bool) {
	for _, cmd := range c.all() {
		if cmd.Line == line {
			return cmd, true
		}
	}
	return settings.SavedCommand{}, false
}

// keep saves a command at the front of the list.
func (c *savedCommands) keep(cmd settings.SavedCommand) error {
	if c.remembered == nil {
		return errNothingToSaveInto
	}
	return c.remembered.KeepCommand(cmd, mostSavedCommands)
}

// forget drops a saved command.
func (c *savedCommands) forget(line string) error {
	if c.remembered == nil {
		return errNothingToSaveInto
	}
	return c.remembered.DropCommand(line)
}

// errNothingToSaveInto is what a list with no settings behind it answers
// rather than reporting a save that did not happen.
var errNothingToSaveInto = errors.New("this window has no settings to keep a command in")

// commandLine is a typed command with the spacing tidied, which is what
// a saved one is matched by.
//
// The same split the Run button makes, so what is saved is what was run.
func commandLine(text string) string { return strings.Join(strings.Fields(text), " ") }
