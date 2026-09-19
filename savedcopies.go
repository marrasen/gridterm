package main

import (
	"errors"

	"github.com/marrasen/gridterm/settings"
)

// mostSavedCopies is how many copies are kept. Saving one past that
// drops the one saved longest ago.
const mostSavedCopies = 50

// savedCopies are the file copies the user asked to keep, offered by the
// dialog that runs one again.
type savedCopies struct {
	// remembered is where they are kept between runs. Nothing is saved
	// while it is nil.
	remembered *settings.Settings
}

// newSavedCopies builds a list that remembers nothing until it is given
// the settings.
func newSavedCopies() *savedCopies { return &savedCopies{} }

// remember gives the list the settings it reads and writes.
func (c *savedCopies) remember(set *settings.Settings) { c.remembered = set }

// all is every saved copy, newest first.
func (c *savedCopies) all() []settings.SavedCopy {
	if c.remembered == nil {
		return nil
	}
	return c.remembered.Copies()
}

// has reports whether a copy is already kept.
func (c *savedCopies) has(want settings.SavedCopy) bool {
	for _, have := range c.all() {
		if have.Same(want) {
			return true
		}
	}
	return false
}

// keep saves a copy at the front of the list.
func (c *savedCopies) keep(saved settings.SavedCopy) error {
	if c.remembered == nil {
		return errNothingToKeepACopyIn
	}
	return c.remembered.KeepCopy(saved, mostSavedCopies)
}

// forget drops a saved copy.
func (c *savedCopies) forget(saved settings.SavedCopy) error {
	if c.remembered == nil {
		return errNothingToKeepACopyIn
	}
	return c.remembered.DropCopy(saved)
}

// errNothingToKeepACopyIn is what a list with no settings behind it
// answers rather than reporting a save that did not happen.
var errNothingToKeepACopyIn = errors.New("this window has no settings to keep a copy in")
