// Package pasted writes pictures and dropped files where the program in
// a pane can open them: a file of its own, whose path is typed into the
// pane.
//
// A program reading a terminal cannot be handed a picture, so what it
// is handed is somewhere to find one. Claude Code and the rest read the
// path and open the file.
package pasted

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

	"github.com/marrasen/gridterm/vfs"
)

// DirName is the directory pictures and dropped files are written in:
// under the temporary directory on this machine, and under the home
// directory on another.
const DirName = "gridterm-pasted"

// PNG is a picture as the bytes that go over a connection.
func PNG(img image.Image) ([]byte, error) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, fmt.Errorf("read the picture: %w", err)
	}
	return buf.Bytes(), nil
}

// WriteHere puts a picture in a file of its own on this machine and
// returns the path. now is the window's clock, so a test does not
// depend on the wall clock; nil is the wall clock.
func WriteHere(img image.Image, now func() time.Time) (string, error) {
	at := time.Now
	if now != nil {
		at = now
	}
	dir := filepath.Join(os.TempDir(), DirName)
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

// WriteOn writes a picture, as PNG bytes, into a directory of its own
// under the home directory of whoever fs logs in as, and returns the
// path.
//
// Under home rather than a temporary directory, because where that is
// depends on the machine and this has only a path separator to go on.
// A directory of its own so the files are together and can be cleared
// out in one go.
func WriteOn(fs vfs.FS, raw []byte, at time.Time) (string, error) {
	dir, err := DirOn(fs)
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

// DirOn is the directory pasted pictures and dropped files go in on a
// machine, made if it is not there yet.
func DirOn(fs vfs.FS) (string, error) {
	home, err := fs.Home()
	if err != nil {
		return "", fmt.Errorf("find somewhere to put it: %w", err)
	}
	sep := string(fs.Sep())
	dir := strings.TrimSuffix(home, sep) + sep + DirName
	if _, err := fs.Stat(dir); err != nil {
		if err := fs.Mkdir(dir, 0o700); err != nil {
			return "", fmt.Errorf("make somewhere to put it: %w", err)
		}
	}
	return dir, nil
}

// Typed is what is typed into a pane for a set of paths: one line's
// worth, with a space between them.
//
// A path holding a space is quoted, because what reads it is a shell or
// a program taking a word.
func Typed(paths []string) string {
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		if strings.ContainsAny(path, " \t") {
			path = `"` + path + `"`
		}
		out = append(out, path)
	}
	return strings.Join(out, " ")
}
