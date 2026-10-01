// Package install puts kakel where a user's programs go, for that user
// alone and with no administrator: the program, a shortcut to start it
// with, an entry the system lists to take it away again, and, when
// asked, a shortcut on the desktop and a start with the computer, into
// the tray.
//
// On Windows that is %LOCALAPPDATA%\Programs\kakel, a Start menu
// shortcut, and an entry under Installed apps. On Linux it is
// ~/.local/bin/kakel and a desktop file. Elsewhere it is not done yet.
package install

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Name is what kakel is installed as.
const Name = "kakel"

// ErrUnsupported says installing is not done on this system yet.
var ErrUnsupported = errors.New("installing kakel is not done on this system yet")

// Options are the choices made as kakel is installed.
type Options struct {
	// Desktop puts a shortcut on the desktop, and Autostart starts
	// kakel with the computer, into the tray.
	Desktop   bool
	Autostart bool
	// Version is the version installed, for the system's list.
	Version string
}

// Installed reports whether exe is the installed copy.
func Installed(exe string) bool {
	want, err := Exe()
	if err != nil {
		return false
	}
	return samePath(exe, want)
}

// samePath reports whether two paths name one file, letter case aside
// where the system ignores it, and through links: the program's own path
// comes with them resolved.
func samePath(a, b string) bool {
	a, b = resolve(a), resolve(b)
	if caseless {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// resolve is path cleaned, with its links resolved where it exists.
func resolve(path string) string {
	if real, err := filepath.EvalSymlinks(path); err == nil {
		return real
	}
	return filepath.Clean(path)
}

// Install copies the program at from to where it is installed, and
// makes its shortcuts and its entry. It returns the installed program.
// The copy is written beside the one it replaces and then moved over
// it, so a copy installed already is replaced whole or not at all.
func Install(from string, o Options) (string, error) {
	to, err := Exe()
	if err != nil {
		return "", err
	}
	if !samePath(from, to) {
		if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
			return "", err
		}
		if err := copyFile(from, to); err != nil {
			return "", fmt.Errorf("copy kakel to %s: %w", to, err)
		}
	}
	if err := register(to, o); err != nil {
		return to, err
	}
	if err := SetAutostart(o.Autostart); err != nil {
		return to, err
	}
	return to, nil
}

// copyFile writes from's bytes to to, through a file beside to that is
// moved into place. A program running from to on Windows is moved aside
// first, as it cannot be written over.
func copyFile(from, to string) error {
	src, err := os.Open(from)
	if err != nil {
		return err
	}
	defer func() { _ = src.Close() }()
	part := to + ".new"
	dst, err := os.OpenFile(part, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(dst, src); err != nil {
		_ = dst.Close()
		_ = os.Remove(part)
		return err
	}
	if err := dst.Close(); err != nil {
		_ = os.Remove(part)
		return err
	}
	if err := Replace(part, to); err != nil {
		_ = os.Remove(part)
		return err
	}
	return nil
}

// Replace puts the program at part in place of the one at exe. On
// Windows a running program cannot be written over, but can be moved:
// it is moved aside to exe.old, which the next start takes away. The
// old program is moved back when the new one can't be put in place, so
// exe is never left missing.
func Replace(part, exe string) error {
	old := ""
	if movesAside {
		if _, err := os.Stat(exe); err == nil {
			old = exe + ".old"
			_ = os.Remove(old)
			if err := os.Rename(exe, old); err != nil {
				return fmt.Errorf("move the old kakel aside: %w", err)
			}
		}
	}
	if err := os.Rename(part, exe); err != nil {
		if old != "" {
			_ = os.Rename(old, exe)
		}
		return fmt.Errorf("put the new kakel in place: %w", err)
	}
	return nil
}

// CleanOld takes away the copy a replace moved aside, once the program
// that was running from it has gone.
func CleanOld(exe string) { _ = os.Remove(exe + ".old") }
