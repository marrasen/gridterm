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

	"github.com/marrasen/gridterm/shellsetup"
	"github.com/marrasen/gridterm/ui/term"
	"github.com/marrasen/gridterm/vfs"
)

// pastedDir is where a picture is written on a machine that has no
// clipboard this window can reach, under the home directory there.
const pastedDir = "gridterm-pasted"

// pasteImage writes the picture on the clipboard to a file and types its
// path into the pane in front.
//
// A program reading a terminal cannot be handed a picture, so what it is
// handed is somewhere to find one. Claude Code and the rest read the
// path and open the file.
// pastePicture hands the picture on the clipboard to the pane, by
// whichever route reaches the program running in it.
//
// The clipboard wherever there is one this window can reach, because a
// program that takes a pasted picture reads the clipboard of the machine
// it runs on. A file only where there is not.
func (a *app) pastePicture(pane *term.Terminal) error {
	img, end, err := a.pictureFor(pane)
	if err != nil {
		return err
	}
	on := a.about(end.host)
	switch {
	case end.far.window == nil && on.kind == hostHere:
		if a.shellWouldQuoteIt(pane) {
			return a.pasteImage(pane)
		}
		// The picture is already on this machine's clipboard and the
		// program is running on this machine, so there is nothing to
		// move. Pressing paste is the whole of it: a program that wants
		// a picture goes and reads the clipboard itself, which is how
		// Claude Code and the rest take one.
		pane.PressPaste()
		return nil
	case end.far.window == nil && on.window != nil:
		// A gridterm over there, so the picture goes on that machine's
		// own clipboard and the program reads it the way it reads one
		// pasted by somebody sitting at it.
		//
		// Only for a program running on that window's own machine. One
		// running on a machine that window reached would read that
		// machine's clipboard, which this picture never went near.
		return a.sendPictureTo(on, pane, img)
	}
	// Nothing over there to hand a clipboard to, so it goes as a file.
	return a.pasteImage(pane)
}

// pasteImage writes the picture on the clipboard to a file on whatever
// machine the pane is running on, and types the path.
//
// It is a command of its own, for when a name is what is wanted: one to
// hand to a program at a prompt, rather than a picture for something
// that reads the clipboard itself.
func (a *app) pasteImage(pane *term.Terminal) error {
	img, end, err := a.pictureFor(pane)
	if err != nil {
		return err
	}
	if end.far.window == nil {
		on := a.about(end.host)
		if on.kind == hostHere {
			path, err := writePastedImage(img, a.now)
			if err != nil {
				return err
			}
			pane.Paste(path)
			return nil
		}
		if on.window == nil && on.machine == nil {
			return fmt.Errorf("nothing is connected to %s", on.name)
		}
	}
	return a.writePictureOn(end, pane, img)
}

// shellWouldQuoteIt reports whether pressing ctrl+V at this pane would
// land in a POSIX shell's line editor rather than in a program.
//
// readline reads ctrl+V as quoted-insert: it takes the next character
// literally. So pressing it at a bash or zsh prompt leaves the shell
// quoting the beginning of whatever is pasted next, and that paste shows
// its bracketed-paste markers as text rather than being obeyed. The
// shell stays that way until something clears it, and nothing on screen
// says why.
//
// A program the shell started is a different matter, and is the case
// PressPaste is for: it reads the clipboard itself and takes the picture
// properly. So the shell is asked whether it is the one reading -- the
// OSC 133 marks shell setup already puts there, which is on by default.
//
// So ctrl+V is kept for the case it is good for -- a program is running
// and the shell says so -- and the picture goes as a file otherwise.
// A shell that sends no marks lands on the file too: not knowing is not
// a reason to send a key that breaks a shell silently, and a path is
// something every program here can already use, which is what a pane on
// a machine at the far end is handed anyway.
//
// The shell decides this and not the machine: a WSL pane on Windows is a
// POSIX shell too, and reads ctrl+V the same way.
func (a *app) shellWouldQuoteIt(pane *term.Terminal) bool {
	return shellsetup.RouteFor(a.localArgv(pane)) == shellsetup.Posix && !pane.RunningAProgram()
}

// pictureFor is the picture on the clipboard and the machine the pane's
// program is running on, which is what says how to hand it over.
func (a *app) pictureFor(pane *term.Terminal) (image.Image, jobEnd, error) {
	img, have, err := a.clipboardPicture()
	if err != nil {
		return nil, jobEnd{}, err
	}
	if !have {
		return nil, jobEnd{}, fmt.Errorf("there is no picture on the clipboard")
	}
	return img, a.paneEnd(pane), nil
}

// writePictureOn writes a picture on a machine reached by SSH and types
// the path into the pane.
//
// Opening the filesystem here rather than on the goroutine below is what
// opening a file manager on that machine already does: it is a moment
// after a key was pressed, which is when the user expects one. The
// writing is the part that can take a while, and that goes elsewhere.
func (a *app) writePictureOn(end jobEnd, pane *term.Terminal, img image.Image) error {
	raw, err := asPNG(img)
	if err != nil {
		return err
	}
	fs, err := a.openEnd(end)
	if err != nil {
		return err
	}
	at, host := a.clock(), endName(end)
	sent := a.sendingPicture(host)
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
			sent()
			if err != nil {
				a.reportError("Could not paste a picture onto "+host, err)
				return
			}
			pane.Paste(path)
		})
	}()
	return nil
}

// sendingPicture says a picture is on its way to a machine, and gives
// back what to call once it has arrived or failed.
//
// Both on the goroutine that draws: the count is read there, to build
// the bar.
func (a *app) sendingPicture(to string) func() {
	a.sending++
	a.sendingTo = to
	a.markDirty()
	return func() {
		a.sending--
		if a.sending <= 0 {
			a.sending, a.sendingTo = 0, ""
		}
		a.markDirty()
	}
}

// putPictureOn writes a picture into a directory of its own under the
// home directory of whoever the connection logs in as.
//
// Under home rather than a temporary directory, because where that is
// depends on the machine and this has only a path separator to go on.
// A directory of its own so the files are together and can be cleared
// out in one go.
func putPictureOn(fs vfs.FS, raw []byte, at time.Time) (string, error) {
	dir, err := pastedDirOn(fs)
	if err != nil {
		return "", err
	}
	path := dir + string(fs.Sep()) + at.Format("20060102-150405.000") + ".png"
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

// endName is what to call the machine a piece of file work is going to.
//
// The machine over there when the work is on one a window reached, and
// the window's own name otherwise: a path on a machine that window is
// connected to is nothing to do with the window's own disk.
func endName(end jobEnd) string {
	if end.far.window != nil {
		return end.far.host
	}
	return end.host
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
	sent := a.sendingPicture(name)
	go func() {
		err := win.SendPicture(raw)
		a.pump.post(func() {
			sent()
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

// paste puts whatever is on the clipboard into a pane.
//
// Text when there is text, and the picture when there is no text and
// there is a picture. A clipboard holding both is text: that is what
// copying from a browser leaves, and the words are what was meant far
// more often than the picture. edit.pasteImage asks for the other one.
func (a *app) paste(pane *term.Terminal) error {
	if a.clipboardText() {
		pane.Paste(a.pasteText())
		return nil
	}
	if _, have, err := a.clipboardPicture(); err != nil || !have {
		// No picture either, so this is an empty clipboard and pasting
		// nothing is what was asked for. A clipboard that would not be
		// read is worth saying.
		if err != nil {
			return err
		}
		return nil
	}
	return a.pastePicture(pane)
}
