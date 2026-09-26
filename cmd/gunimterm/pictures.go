package main

import (
	"errors"
	"image"
	"time"

	"github.com/marrasen/gridterm/clip"
	"github.com/marrasen/gridterm/pasted"
	"github.com/marrasen/gridterm/session"
	shellfind "github.com/marrasen/gridterm/shells"
	"github.com/marrasen/gridterm/shellsetup"
	"github.com/marrasen/gridterm/vfs"
)

// A picture on the clipboard is handed to the program in a terminal as
// gridterm hands it: by pressing paste, for a program here that reads
// the clipboard itself; by putting it on the clipboard of a window this
// one is connected to, and pressing paste there; or as a file, whose
// path is typed, where there is no clipboard to hand it to.

// Intents for pictures on the clipboard.
type (
	// PasteImage writes the picture on the clipboard to a file on the
	// machine a terminal pane runs on, and types the path. An empty
	// Pane is the focused one.
	PasteImage struct{ Pane string }
	// PastePicture hands the picture on the clipboard to the program in
	// a terminal pane, for a paste that found no text.
	PastePicture struct{ Pane string }
	// NoTextToPaste says a middle click found no text to paste, which
	// is said when the clipboard holds a picture: that paste takes text.
	NoTextToPaste struct{}
)

// readPicture reads the picture on the clipboard. It is read off the
// program's goroutine, since the program holding it may be slow to hand
// it over. A test puts a picture of its own in its place.
var readPicture = clip.Image

// takePicture puts a picture another window pasted on this machine's
// clipboard, for the program in the pane it was pasted into. A test
// puts a function of its own in its place.
var takePicture = clip.SetPNG

// pastePicture hands the picture on the clipboard to the program in a
// pane: as a file when asFile, and otherwise by whichever route reaches
// that program.
func (a *app) pastePicture(id string, asFile bool) error {
	if id == "" {
		id = a.st.Focus
	}
	if a.terminal(id) == nil {
		return errors.New("a picture is pasted into a terminal, and this pane is none")
	}
	read := readPicture
	go func() {
		img, have, err := read()
		a.events <- func() {
			switch {
			case err != nil:
				a.notify("Couldn't paste the picture", err.Error(), "")
			case !have && asFile:
				a.notify("There is no picture on the clipboard", "Copy one first, then paste it as a file.", "")
			case !have:
				// An empty clipboard, and pasting nothing is what was
				// asked for.
			default:
				if err := a.handPicture(id, img, asFile); err != nil {
					a.notify("Couldn't paste the picture", err.Error(), "")
				}
			}
		}
	}()
	return nil
}

// handPicture hands a picture read off the clipboard to a pane.
func (a *app) handPicture(id string, img image.Image, asFile bool) error {
	t := a.terminal(id)
	if t == nil {
		// Closed while the clipboard was read.
		return nil
	}
	machine := a.machineOf(id)
	if machine == "" {
		if asFile || a.shellWouldQuoteIt(id) {
			path, err := pasted.WriteHere(img, nil)
			if err != nil {
				return err
			}
			t.Paste(a.pathForPane(id, path))
			return nil
		}
		// The picture is on this machine's clipboard already, and the
		// program runs here: pressing paste is the whole of it.
		t.PressPaste()
		return nil
	}
	raw, err := pasted.PNG(img)
	if err != nil {
		return err
	}
	if w, ok := a.windows[machine]; ok && !asFile && a.farHost[id] == "" {
		// Onto that window's clipboard, then paste pressed, once it has
		// landed: pressing first would paste what was there before.
		go func() {
			err := w.win.SendPicture(raw)
			a.events <- func() {
				switch {
				case err != nil:
					a.notify("Couldn't paste the picture into "+machine, err.Error(), "")
				case a.terminal(id) == t:
					t.PressPaste()
				}
			}
		}()
		return nil
	}
	// A file, on the machine the program runs on: a server, a window
	// asked for a file, or a machine a window reached, which has no
	// clipboard this window can hand the picture to.
	key := a.filesKey(id)
	return a.withFiles(key, func(f vfs.FS) {
		at := time.Now()
		go func() {
			path, err := pasted.WriteOn(f, raw, at)
			a.events <- func() {
				switch {
				case err != nil:
					a.notify("Couldn't paste the picture to "+placeName(key), err.Error(), "")
				case a.terminal(id) == t:
					t.Paste(path)
				default:
					a.notify("Picture saved", path+" on "+placeName(key)+".", path)
				}
			}
		}()
	})
}

// shellWouldQuoteIt reports whether pressing paste, Ctrl+V, in a pane
// would reach a POSIX shell's line editor rather than a program.
//
// readline reads Ctrl+V as quoted-insert, and the shell then shows the
// next paste's markers as text. So a shell at its prompt is handed a
// file instead, as in gridterm, and so is a shell that never says
// whether a program is running.
func (a *app) shellWouldQuoteIt(id string) bool {
	t := a.terminal(id)
	return t != nil && shellsetup.RouteFor(a.localArgv(id)) == shellsetup.Posix && !t.RunningAProgram()
}

// localArgv is what a local pane runs: what it was started with, or
// the user's usual shell when it was started with nothing named.
func (a *app) localArgv(id string) []string {
	if argv := a.argvs[id]; len(argv) > 0 {
		return argv
	}
	argv, err := session.DefaultShell()
	if err != nil {
		return nil
	}
	return argv
}

// pathForPane is the path the program in a local pane opens a file of
// this machine's at: inside WSL, this machine's drives are under /mnt.
func (a *app) pathForPane(id, path string) string {
	if a.distroOf(id) != "" {
		if unix := shellfind.UnixPath(path); unix != "" {
			return unix
		}
	}
	return path
}

// distroOf is the WSL distribution a local pane runs, and empty for a
// pane running anything else.
func (a *app) distroOf(id string) string {
	sh, ok := shellfind.Running(a.found, a.localArgv(id))
	if !ok {
		return ""
	}
	return sh.Distro
}

// noTextToPaste says why a middle click pasted nothing, when there is
// something to say: an empty clipboard says nothing.
func (a *app) noTextToPaste() {
	if img, have, err := readPicture(); err == nil && have && img != nil {
		a.notify("Could not paste", "The clipboard holds a picture rather than text, and this takes text.", "")
	}
}
