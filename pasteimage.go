package main

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"time"

	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/ui/term"
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
	}
	return fmt.Errorf(
		"this pane is running on %s, and a picture cannot be pasted there yet", on.name)
}

// sendPictureTo puts a picture on the clipboard of a window this one has
// taken over, and then presses paste in the pane.
//
// The sending is network work, so it happens on a goroutine of its own
// and comes back to the goroutine that draws to say what happened. The
// paste is only pressed once the picture has landed: pressing it first
// would paste whatever was on that clipboard before.
func (a *app) sendPictureTo(on hostFacts, pane *term.Terminal, img image.Image) error {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return fmt.Errorf("read the picture: %w", err)
	}
	raw, win, name := buf.Bytes(), on.window.win, on.name
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
