package main

import (
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

// pastedTries is how many names are tried before giving up. A name holds
// the millisecond, so reaching this means something else is writing into
// the same directory as fast as it can.
const pastedTries = 100

// pasteImage writes the picture on the clipboard to a file and types its
// path into the pane in front.
//
// A program reading a terminal cannot be handed a picture, so what it is
// handed is somewhere to find one. Claude Code and the rest read the
// path and open the file.
func (a *app) pasteImage(pane *term.Terminal) error {
	if e, ok := a.panes[pane]; ok && e != nil && e.Host != conns.Local {
		// The file would land on this machine and the path would mean
		// nothing on the other one. Better said than typed.
		return fmt.Errorf(
			"this pane is running on %s, and a picture pasted here would be written on this machine", e.Host)
	}
	read := a.readClipImage
	if read == nil {
		read = clipboardImage
	}
	img, have, err := read()
	if err != nil {
		return err
	}
	if !have {
		return fmt.Errorf("there is no picture on the clipboard")
	}
	path, err := writePastedImage(img, a.now)
	if err != nil {
		return err
	}
	pane.Paste(path)
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
	// Named for the moment it was pasted, and numbered when that name is
	// taken: two pastes in one millisecond, or a second window pasting,
	// must not have one quietly stand in for the other.
	stamp := at().Format("20060102-150405.000")
	var path string
	var f *os.File
	for n := 1; ; n++ {
		name := stamp + ".png"
		if n > 1 {
			name = fmt.Sprintf("%s-%d.png", stamp, n)
		}
		path = filepath.Join(dir, name)
		var err error
		f, err = os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err == nil {
			break
		}
		if !os.IsExist(err) || n >= pastedTries {
			return "", fmt.Errorf("write the picture: %w", err)
		}
	}
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
