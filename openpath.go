package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/ui/files"
	"github.com/marrasen/gridterm/vfs"
)

// pathFinder answers whether text a pane printed names something on
// this machine, for a pane running here.
//
// Only for a pane on this machine. A pane on a machine at the far end
// prints that machine's paths, and looking for them on this disk
// would find the wrong file or, worse, the right-looking one.
func (a *app) pathFinder(host string) func(text, dir string) (string, bool, bool) {
	if host != conns.Local {
		return nil
	}
	return func(text, dir string) (string, bool, bool) {
		return findOnDisk(text, dir)
	}
}

// findOnDisk resolves text a pane printed against the directory the
// shell said it was in, and reports what it found.
//
// The disk is the check. Anything in the output could look like a
// path, and what makes this safe rather than a guess is that a run of
// characters naming nothing is simply not a link.
func findOnDisk(text, dir string) (at string, isDir, ok bool) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", false, false
	}
	// A path with a line break or a null in it is not one, whatever
	// the disk would say about it.
	if strings.ContainsAny(text, "\r\n\x00") {
		return "", false, false
	}
	for _, try := range pathsToTry(text, dir) {
		info, err := os.Lstat(try)
		if err != nil {
			continue
		}
		// A symbolic link is followed only as far as saying what it
		// points at: opening the link itself is what the user asked
		// for, and a link to a directory opens the browser.
		if info.Mode()&fs.ModeSymlink != 0 {
			if target, err := os.Stat(try); err == nil {
				return try, target.IsDir(), true
			}
		}
		return try, info.IsDir(), true
	}
	return "", false, false
}

// pathsToTry is where a piece of text might name something: as
// written, and under the directory the shell is in.
func pathsToTry(text, dir string) []string {
	if filepath.IsAbs(text) || looksAbsolute(text) {
		return []string{text}
	}
	if dir == "" {
		// Nowhere to resolve a relative name against. Not tried
		// against gridterm's own directory, which is not where the
		// user is and would open the wrong file.
		return nil
	}
	return []string{filepath.Join(dir, text)}
}

// looksAbsolute catches the paths filepath.IsAbs does not on the
// machine it is compiled for: a POSIX path read on Windows, which a
// pane running WSL or a remote shell prints.
func looksAbsolute(text string) bool {
	return strings.HasPrefix(text, "/") || strings.HasPrefix(text, `\\`)
}

// pathOpener opens what pathFinder found: a directory in the file
// browser and a file in the viewer, at the line the output named.
func (a *app) pathOpener(host string) func(at string, isDir bool, line int) {
	if host != conns.Local {
		return nil
	}
	return func(at string, isDir bool, line int) {
		if err := a.openOnDisk(at, isDir, line); err != nil {
			a.reportError("Could not open "+at, err)
		}
	}
}

// openOnDisk puts a path on screen: a directory in the browser, and a
// file in the viewer at the line it was named at.
func (a *app) openOnDisk(at string, isDir bool, line int) error {
	if isDir {
		return a.openFilesAt(conns.Local, at)
	}
	f := vfs.NewLocal()
	e, err := f.Stat(at)
	if err != nil {
		return err
	}
	if err := a.openReader(f, conns.Local, at, filepath.Base(at), false, e.Size); err != nil {
		return err
	}
	if line > 0 {
		a.goToLineWhenRead(a.readerOn(at), line)
	}
	return nil
}

// readerOn is the viewer just opened on a path.
func (a *app) readerOn(at string) *files.Reader {
	for r, held := range a.readers {
		if held.at == at {
			return r
		}
	}
	return nil
}

// goToLineWhenRead moves a viewer to a line once its file has
// arrived, because the line is not there to go to until it has.
func (a *app) goToLineWhenRead(r *files.Reader, line int) {
	if r == nil {
		return
	}
	var wait func()
	tries := 0
	wait = func() {
		switch {
		case r.Lines() > 0:
			r.GoToLine(line)
		case r.Busy() && tries < mostLineTries:
			tries++
			a.pump.post(wait)
		}
	}
	a.pump.post(wait)
}

// mostLineTries bounds the waiting for a file to arrive before going
// to a line in it, so a read that never comes back does not leave
// something asking after it for ever.
const mostLineTries = 10000
