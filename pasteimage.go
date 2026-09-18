package main

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/ui/term"
	"github.com/marrasen/gridterm/vfs"
)

// pastedDir is where a picture taken off the clipboard is written, under
// the system's temporary directory.
const pastedDir = "gridterm-pasted"

// pasteImage writes the picture on the clipboard to a file and types its
// path into the pane in front.
//
// A program reading a terminal cannot be handed a picture, so what it is
// handed is somewhere to find one. Claude Code and the rest read the
// path and open the file.
func (a *app) pasteImage(pane *term.Terminal) error {
	img, have, err := a.clipboardPicture()
	if err != nil {
		return err
	}
	if !have {
		return fmt.Errorf("there is no picture on the clipboard")
	}
	host := conns.Local
	if e, ok := a.panes[pane]; ok && e != nil {
		host = e.Host
	}
	on := a.about(host)
	switch {
	case on.kind == hostHere:
		path, err := writePastedImage(img, a.now)
		if err != nil {
			return err
		}
		pane.Paste(path)
		return nil
	case on.window != nil:
		// A gridterm over there, so the picture goes on that machine's
		// own clipboard and the program reads it the way it reads one
		// pasted by somebody sitting at it.
		return a.sendPictureTo(on, pane, img)
	case on.machine != nil:
		// A machine reached by SSH. There is no gridterm over there to
		// hand a clipboard to, so the picture is written on it and the
		// path typed names a file that machine can open.
		return a.writePictureOn(on, pane, img)
	}
	return fmt.Errorf("nothing is connected to %s", on.name)
}

// writePictureOn writes a picture on a machine reached by SSH and types
// the path into the pane.
//
// Opening the filesystem here rather than on the goroutine below is what
// opening a file manager on that machine already does: it is a moment
// after a key was pressed, which is when the user expects one. The
// writing is the part that can take a while, and that goes elsewhere.
func (a *app) writePictureOn(on hostFacts, pane *term.Terminal, img image.Image) error {
	raw, err := asPNG(img)
	if err != nil {
		return err
	}
	fs, err := a.filesystem(on.name)
	if err != nil {
		return err
	}
	at, host := a.clock(), on.name
	go func() {
		defer func() {
			if err := fs.Close(); err != nil {
				a.pump.post(func() {
					a.logError(fmt.Errorf("let go of the files on %s: %w", host, err))
				})
			}
		}()
		path, err := putPictureOn(fs, raw, at)
		a.pump.post(func() {
			if err != nil {
				a.reportError("Could not paste a picture onto "+host, err)
				return
			}
			pane.Paste(path)
		})
	}()
	return nil
}

// putPictureOn writes a picture into a directory of its own under the
// home directory of whoever the connection logs in as.
//
// Under home rather than a temporary directory, because where that is
// depends on the machine and this has only a path separator to go on.
// A directory of its own so the files are together and can be cleared
// out in one go.
func putPictureOn(fs vfs.FS, raw []byte, at time.Time) (string, error) {
	home, err := fs.Home()
	if err != nil {
		return "", fmt.Errorf("find somewhere to put the picture: %w", err)
	}
	sep := string(fs.Sep())
	dir := strings.TrimSuffix(home, sep) + sep + pastedDir
	if _, err := fs.Stat(dir); err != nil {
		if err := fs.Mkdir(dir, 0o700); err != nil {
			return "", fmt.Errorf("make somewhere to put the picture: %w", err)
		}
	}
	path := dir + sep + at.Format("20060102-150405.000") + ".png"
	w, err := fs.Create(path, 0o600)
	if err != nil {
		return "", fmt.Errorf("write the picture: %w", err)
	}
	if _, err := w.Write(raw); err != nil {
		return "", fmt.Errorf("write the picture: %w", errors.Join(err, w.Close()))
	}
	if err := w.Close(); err != nil {
		return "", fmt.Errorf("write the picture: %w", err)
	}
	return path, nil
}

// asPNG is a picture as the bytes that go over a connection.
func asPNG(img image.Image) ([]byte, error) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, fmt.Errorf("read the picture: %w", err)
	}
	return buf.Bytes(), nil
}

// sendPictureTo puts a picture on the clipboard of a window this one has
// taken over, and then presses paste in the pane.
//
// The sending is network work, so it happens on a goroutine of its own
// and comes back to the goroutine that draws to say what happened. The
// paste is only pressed once the picture has landed: pressing it first
// would paste whatever was on that clipboard before.
func (a *app) sendPictureTo(on hostFacts, pane *term.Terminal, img image.Image) error {
	raw, err := asPNG(img)
	if err != nil {
		return err
	}
	win, name := on.window.win, on.name
	go func() {
		err := win.SendPicture(raw)
		a.pump.post(func() {
			if err != nil {
				a.reportError("Could not paste a picture into "+name, err)
				return
			}
			pane.PressPaste()
		})
	}()
	return nil
}

// writePastedImage puts a picture in a file of its own and returns the
// path. now is the window's clock, so a test does not depend on the
// wall clock.
func writePastedImage(img image.Image, now func() time.Time) (string, error) {
	at := time.Now
	if now != nil {
		at = now
	}
	dir := filepath.Join(os.TempDir(), pastedDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("make somewhere to put the picture: %w", err)
	}
	// Named for the moment it was pasted, with the rest left to
	// os.CreateTemp: it settles two pastes in one millisecond, and a
	// second window pasting into the same directory, without this having
	// to think about either.
	f, err := os.CreateTemp(dir, at().Format("20060102-150405")+"-*.png")
	if err != nil {
		return "", fmt.Errorf("write the picture: %w", err)
	}
	path := f.Name()
	if err := png.Encode(f, img); err != nil {
		// Closed on the way out, and the half-written file taken away:
		// a path typed into a shell has to name a picture that opens.
		return "", fmt.Errorf("write the picture: %w",
			errors.Join(err, f.Close(), os.Remove(path)))
	}
	if err := f.Close(); err != nil {
		return "", fmt.Errorf("write the picture: %w", errors.Join(err, os.Remove(path)))
	}
	return path, nil
}

// takeSentPicture puts a picture a client pasted on this machine's
// clipboard, so a program running here can be handed it.
//
// The bytes are decoded before anything is put on the clipboard: a
// client that sent something that is not a picture must not empty the
// clipboard of whoever is sitting here.
func takeSentPicture(raw []byte) error {
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("that is not a picture this window can read: %w", err)
	}
	return setClipboardImage(img)
}
