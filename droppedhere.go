package main

import (
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/jobs"
	"github.com/marrasen/gridterm/shells"
	"github.com/marrasen/gridterm/ui/term"
	"github.com/marrasen/gridterm/vfs"
)

// droppedInto is the directory a file dropped on a pane goes in, and
// whether the shell in that pane has said where it is.
//
// The shell says so through OSC 7, which gridterm sets it up to send.
// Without it there is nowhere to put the file but a directory of
// gridterm's choosing, and the path has to be typed instead.
func (a *app) droppedInto(pane *term.Terminal, end jobEnd) (string, bool) {
	dir, _ := pane.Dir()
	if dir == "" {
		return "", false
	}
	if end.far.window != nil || a.about(end.host).kind != hostHere {
		// The machine at the far end names its own paths, and the
		// filesystem opened for it reads them.
		return dir, true
	}
	if sh, ok := a.shellPick.running(a.localArgv(pane)); ok && sh.Distro != "" {
		// A pane in WSL says a path inside the distribution, which this
		// machine reaches on a share of its own.
		at := shells.WindowsPath(sh.Distro, dir)
		return at, at != ""
	}
	return dir, true
}

// pathForPane is the path the program in a pane opens a file of this
// machine's at.
//
// A pane in WSL reads the distribution's filesystem, where this
// machine's drives are mounted under /mnt, so a Windows path typed into
// one names nothing. Anything else takes the path as it stands.
func (a *app) pathForPane(pane *term.Terminal, win string) string {
	sh, ok := a.shellPick.running(a.localArgv(pane))
	if !ok || sh.Distro == "" {
		return win
	}
	if unix := shells.UnixPath(win); unix != "" {
		return unix
	}
	return win
}

// pathsForPane is pathForPane over a list, leaving the one it was given
// alone.
func (a *app) pathsForPane(pane *term.Terminal, wins []string) []string {
	out := make([]string, 0, len(wins))
	for _, win := range wins {
		out = append(out, a.pathForPane(pane, win))
	}
	return out
}

// copyDropped copies dropped files into the directory the shell in a
// pane said it was in, and says so once they are there, through tell.
//
// Nothing is typed. The file is where the program is already looking,
// so a path after it would only be in the way.
func (a *app) copyDropped(end jobEnd, paths []string, dir string) error {
	fs, err := a.openEnd(end)
	if err != nil {
		return err
	}
	end = endReached(end, fs)
	var started []*jobs.Job
	var already []string
	for _, path := range paths {
		if sameDir(filepath.Dir(path), dir) {
			// Already there. Copying it would be copying a file onto
			// itself, which leaves nothing behind.
			already = append(already, filepath.Base(path))
			continue
		}
		op := jobs.Op{
			Kind: jobs.Copy,
			From: vfs.NewLocal(), At: filepath.Dir(path),
			Names: []string{filepath.Base(path)},
			To:    fs, Into: dir,
		}
		if j := a.runJob(op, jobEnd{host: conns.Local}, end, nil); j != nil {
			started = append(started, j)
		}
	}
	if len(already) > 0 {
		a.tell(arrived(already, dir, endName(end)), "Already exists.")
	}
	if len(started) == 0 {
		return fs.Close()
	}
	a.sayWhenArrived(started, fs, paths, dir, endName(end))
	return nil
}

// sameDir reports whether two paths name one directory on this machine.
func sameDir(a, b string) bool {
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}

// sayWhenArrived waits for a drop's copies and says what landed.
//
// One message for the drop rather than one per file: a handful of files
// dragged in together is one thing the user did.
func (a *app) sayWhenArrived(started []*jobs.Job, fs vfs.FS, paths []string,
	dir, where string) {

	a.closes.inBackground(func() error {
		var failed int
		for _, j := range started {
			<-j.Done()
			if j.Progress().Err != nil {
				failed++
			}
		}
		a.pump.post(func() {
			if failed == len(started) {
				// Every row already says what went wrong, and a copy
				// the user stopped is not a failure to report.
				return
			}
			names := make([]string, 0, len(paths))
			for _, path := range paths {
				names = append(names, filepath.Base(path))
			}
			body := ""
			if failed > 0 {
				body = strconv.Itoa(failed) + " failed. Details are on their rows."
			}
			a.tell(arrived(names, dir, where), body)
		})
		return errors.Join(fs.Close())
	})
}

// arrived is the line at the top of the message: what landed and where.
func arrived(names []string, dir, where string) string {
	what := "Copied " + strconv.Itoa(len(names)) + " files"
	if len(names) == 1 {
		what = "Copied " + names[0]
	}
	return fmt.Sprintf("%s to %s on %s", what, dir, groupName(where))
}
