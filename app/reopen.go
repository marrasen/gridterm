package app

import (
	"errors"

	"github.com/marrasen/kakel/machines"

	"github.com/marrasen/gunim"

	"github.com/marrasen/kakel/vfs"
)

// A file pane whose connection dropped stays, and opens its machine's
// files again on the next thing asked of it, connecting again first
// when the connection itself has gone.

// filePaneOf is the file pane an intent works in, and empty for one
// that works in none.
func filePaneOf(in gunim.Intent) string {
	switch in := in.(type) {
	case Browse:
		return in.Pane
	case EnterEntry:
		return in.Pane
	case GoUp:
		return in.Pane
	case GoTo:
		return in.Pane
	case ViewFile:
		return in.Pane
	case ClipFiles:
		return in.Pane
	case PasteFiles:
		return in.Pane
	case DeleteFiles:
		return in.Pane
	case RenameFile:
		return in.Pane
	case MakeFolder:
		return in.Pane
	}
	return ""
}

// needsFiles reports whether in works in a file pane whose files are
// not open, and if so opens them and carries it out once they are.
func (a *app) needsFiles(in gunim.Intent) bool {
	pane := filePaneOf(in)
	if pane == "" || a.kindOfPane(pane) != KindFiles || a.fsFor(a.filesKey(pane)) != nil {
		return false
	}
	key := a.filesKey(pane)
	then := func(vfs.FS) { a.handle(in) }
	machine := a.machineOf(pane)
	if a.machines.Get(machine).Conn != nil {
		if err := a.withFiles(key, then); err != nil {
			a.failed("Couldn't open the files on "+a.machines.Name(key), err.Error())
		}
		return true
	}
	if a.machines.Get(machine).Window != nil {
		if err := a.withFiles(key, then); err != nil {
			a.failed("Couldn't open the files on "+a.machines.Name(key), err.Error())
		}
		return true
	}
	if err := a.dialAgain(machine, func(err error) {
		if err != nil {
			return
		}
		if err := a.withFiles(key, then); err != nil {
			a.failed("Couldn't open the files on "+a.machines.Name(key), err.Error())
		}
	}); err != nil {
		a.failed("Couldn't open the files on "+a.machines.Name(key), err.Error())
	}
	return true
}

// dialAgain connects once more to a server whose connection has gone,
// by its ID: a saved one, or a quick connection's address, as the same
// quick connection. then hears how it went. A window is connected to
// again by the user, and a server removed from the list is not.
func (a *app) dialAgain(machine machines.ID, then func(error)) error {
	return a.dialAgainHow(machine, false, then)
}

// dialAgainHow is dialAgain, quiet for a file manager window.
func (a *app) dialAgainHow(machine machines.ID, quiet bool, then func(error)) error {
	if target, window, ok := a.machines.Quick(machine); ok {
		if window {
			return a.reachWindow(ConnectWindow{Addr: target, ID: machine}, quiet, then)
		}
		return a.connectThen(ConnectTo{Target: target, As: machine, Quiet: quiet}, then)
	}
	if _, saved := a.machines.Saved(machine); !saved {
		return errors.New(a.machines.Name(machine) + " is not in the server list any more, so there is nothing to connect to")
	}
	// A saved window too: connectThen connects to it as one.
	return a.connectThen(ConnectTo{Server: machine, Quiet: quiet}, then)
}
