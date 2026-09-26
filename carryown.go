package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/marrasen/gridterm/conf"
	"github.com/marrasen/gridterm/keys"
	"github.com/marrasen/gridterm/remote"
	"github.com/marrasen/gridterm/serve"
	"github.com/marrasen/gridterm/settings"
	"github.com/marrasen/gridterm/themes"
)

// carryOwnTitle is what the button on the files notice says, and
// carriedOwnTitle heads the notice that says it is done.
//
// Two strings rather than one: a button says what pressing it will do
// and a title says what has happened, and one word cannot be both.
const (
	carryOwnTitle   = "Make Portable"
	carriedOwnTitle = "Made Portable"
)

// ownFiles are the files a copy carrying its own takes with it, beside
// the serving key.
var ownFiles = []string{
	settings.File, remote.BookFile, themes.File, keys.File,
	serve.AuthFile, knownWindowsFile,
}

// carryOwnFiles makes the directory beside this copy of gridterm and
// copies into it everything the window is reading now, so the copy
// starts with what the user has rather than with nothing.
//
// It reports what it did, for the notice that follows.
func (a *app) carryOwnFiles() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("where this copy of gridterm is: %w", err)
	}
	from, err := conf.Dir()
	if err != nil {
		return "", err
	}
	key, err := serve.HostKeyPath()
	if err != nil {
		return "", err
	}
	done, err := conf.CarryOwn(exe, from, ownFiles, key)
	if err != nil {
		return "", err
	}
	done = append(done, "", "Restart gridterm to use them.")
	return strings.Join(done, "\n"), nil
}
