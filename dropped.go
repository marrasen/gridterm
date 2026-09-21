package main

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/jobs"
	"github.com/marrasen/gridterm/ui"
	"github.com/marrasen/gridterm/ui/term"
	"github.com/marrasen/gridterm/vfs"
)

// takeDroppedFiles puts files dropped on the window into the pane they
// were dropped on, and types their paths.
//
// A program reading a terminal cannot be handed a file, so the file is
// put where that program is already looking: the directory the shell
// said it was in. A shell that has not said leaves nowhere to put it,
// and then the path is typed instead.
//
// Either way a file that has to reach another machine is copied first,
// with a row on the sidebar saying how far it has got and a cross that
// stops it.
func (a *app) takeDroppedFiles() {
	paths := ebiten.DroppedFilePaths()
	if len(paths) == 0 {
		return
	}
	px, py := ebiten.CursorPosition()
	pane := a.paneAt(px, py)
	if pane == nil {
		// Dropped somewhere that is not a pane: the sidebar, a dialog,
		// the menu bar. The pane in front is what the user is working
		// in, and is where a file they just dragged in is wanted.
		pane = a.focusedTerminal()
	}
	if pane == nil {
		a.reportError("Could not take that file", errors.New(
			"there is no pane open to put it in"))
		return
	}
	if err := a.dropOnPane(pane, paths); err != nil {
		a.reportError("Could not take that file", err)
	}
}

// paneAt is the pane under a pixel, and nil when what is there is not a
// pane.
func (a *app) paneAt(px, py int) *term.Terminal {
	if a.root.Widget() == nil || a.pointerGone {
		return nil
	}
	if a.root.Modal() != nil {
		// A dialog covers the window, so nothing under it is being
		// pointed at.
		return nil
	}
	col, row := a.cellAt(px, py)
	cols, rows := a.g.Size()
	w, _, ok := ui.LeafAt(a.root.Widget(), ui.Rect{Cols: cols, Rows: rows}, col, row)
	if !ok {
		return nil
	}
	pane, _ := w.(*term.Terminal)
	return pane
}

// dropOnPane hands files to the machine a pane is running on.
//
// Into the directory the shell said it was in, when it has said: the
// file lands where the program is already looking and nothing is
// typed. Failing that the path is typed, and a file that is on the
// wrong machine is copied first.
func (a *app) dropOnPane(pane *term.Terminal, paths []string) error {
	end := a.paneEnd(pane)
	if dir, ok := a.droppedInto(pane, end); ok {
		return a.copyDropped(end, paths, dir)
	}
	if end.far.window == nil && a.about(end.host).kind == hostHere {
		// Already on the machine the program runs on, so there is
		// nothing to copy and the path is the whole of it.
		pane.Paste(typedPaths(a.pathsForPane(pane, paths)))
		return nil
	}
	return a.uploadDropped(end, pane, paths)
}

// paneEnd is the machine a pane's program is running on, as a piece of
// file work names one.
//
// Not the machine the pane's row is filed under. A pane drawn from a
// window taken over may be running on a machine that window reached
// rather than on the window's own, and then the files that matter are
// that machine's. A file put on the window instead is a file the program
// cannot open, and the path typed after it names nothing.
func (a *app) paneEnd(pane *term.Terminal) jobEnd {
	end := jobEnd{host: conns.Local}
	if e := a.panes[pane]; e != nil {
		end.host = e.Host
	}
	what, drawn := a.windows.watching(pane)
	if !drawn || what.window == nil {
		return end
	}
	open, still := a.openOver(what)
	if !still || isTheirOwn(open.Host) {
		// On that window's own machine, which its own files are.
		return end
	}
	end.far = remoteHostKey{window: what.window, host: open.Host}
	return end
}

// uploadDropped copies files onto the machine a pane is running on and
// types their paths once they are there.
//
// One job for each, so a file that is still going has a row of its own
// saying how far it has got, and a cross that stops that one rather than
// all of them. A file dropped in a batch is usually a file the user
// wants named on its own line anyway.
func (a *app) uploadDropped(end jobEnd, pane *term.Terminal, paths []string) error {
	fs, err := a.openEnd(end)
	if err != nil {
		return err
	}
	dir, err := pastedDirOn(fs)
	if err != nil {
		return errors.Join(err, fs.Close())
	}
	// The filesystem is the last job's to close, so the ones before it
	// keep it open. A job that finished would otherwise take the
	// connection out from under the ones still copying.
	for i, path := range paths {
		var owned []vfs.FS
		if i == len(paths)-1 {
			owned = []vfs.FS{fs}
		}
		a.uploadOne(fs, end, pane, path, dir, owned)
	}
	return nil
}

// uploadOne copies one file and types its path when it is there.
func (a *app) uploadOne(fs vfs.FS, end jobEnd, pane *term.Terminal,
	path, dir string, owned []vfs.FS) {

	name := filepath.Base(path)
	op := jobs.Op{
		Kind: jobs.Copy,
		From: vfs.NewLocal(), At: filepath.Dir(path), Names: []string{name},
		To: fs, Into: dir,
	}
	j := a.runJob(op, jobEnd{host: conns.Local}, end, owned)
	if j == nil {
		return
	}
	at := strings.TrimSuffix(dir, string(fs.Sep())) + string(fs.Sep()) + name
	// Waiting for a job cannot happen on the goroutine that draws, and
	// what to do with the answer belongs to it.
	a.closes.inBackground(func() error {
		<-j.Done()
		a.pump.post(func() {
			if err := j.Progress().Err; err != nil {
				// The row already says what went wrong, and a job the
				// user stopped is not a failure to report.
				return
			}
			if _, live := a.panes[pane]; !live {
				// The pane it was dropped on has been closed while the
				// file was on its way. Typing into it would put the path
				// where nobody can read it, and the file is there.
				a.showNotice("The file arrived after its pane closed",
					at+" is on "+groupName(endName(end)), false)
				return
			}
			// The pane it was dropped on, whatever the user has moved on
			// to: a path typed into whatever happens to have the keys
			// when a copy finishes would land in the wrong place.
			pane.Paste(typedPaths([]string{at}))
		})
		return nil
	})
}

// pastedDirOn is the directory dropped files go in on a machine, made if
// it is not there yet.
func pastedDirOn(fs vfs.FS) (string, error) {
	home, err := fs.Home()
	if err != nil {
		return "", fmt.Errorf("find somewhere to put it: %w", err)
	}
	sep := string(fs.Sep())
	dir := strings.TrimSuffix(home, sep) + sep + pastedDir
	if _, err := fs.Stat(dir); err != nil {
		if err := fs.Mkdir(dir, 0o700); err != nil {
			return "", fmt.Errorf("make somewhere to put it: %w", err)
		}
	}
	return dir, nil
}

// typedPaths is what is typed into the pane for a set of paths: one
// line's worth, with a space between them.
//
// A path holding a space is quoted, because what reads it is a shell or
// a program taking a word.
func typedPaths(paths []string) string {
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		if strings.ContainsAny(path, " \t") {
			path = `"` + path + `"`
		}
		out = append(out, path)
	}
	return strings.Join(out, " ")
}
