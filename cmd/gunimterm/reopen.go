package main

import (
	"errors"

	"github.com/marrasen/gunim"

	"github.com/marrasen/gridterm/vfs"
)

// A file pane whose connection dropped stays, and opens its machine's
// files again on the next thing asked of it, connecting again first
// when the connection itself has gone, as gridterm's file panes do.

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
	if pane == "" || a.kindOfPane(pane) != kindFiles || a.fsFor(a.filesKey(pane)) != nil {
		return false
	}
	key := a.filesKey(pane)
	then := func(vfs.FS) { a.handle(in) }
	machine := a.machineOf(pane)
	if _, ok := a.conns[machine]; ok {
		if err := a.withFiles(key, then); err != nil {
			a.notify("Couldn't open the files on "+placeName(key), err.Error(), "")
		}
		return true
	}
	if _, ok := a.windows[machine]; ok {
		if err := a.withFiles(key, then); err != nil {
			a.notify("Couldn't open the files on "+placeName(key), err.Error(), "")
		}
		return true
	}
	if err := a.dialAgain(machine, func(err error) {
		if err != nil {
			return
		}
		if err := a.withFiles(key, then); err != nil {
			a.notify("Couldn't open the files on "+placeName(key), err.Error(), "")
		}
	}); err != nil {
		a.notify("Couldn't open the files on "+placeName(key), err.Error(), "")
	}
	return true
}

// dialAgain connects once more to a server whose connection has gone:
// the saved one by that name, or the target the name is. then hears how
// it went. A window is connected to again by the user.
func (a *app) dialAgain(machine string, then func(error)) error {
	in := ConnectTo{Target: machine}
	if a.book != nil {
		if h, saved := a.book.Lookup(machine); saved {
			if h.Window {
				return errors.New("the window " + machine + " has gone. Connect to it again first")
			}
			in = ConnectTo{Saved: machine}
		}
	}
	return a.connectThen(in, then)
}
