package main

import (
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/marrasen/gridterm/jobs"
	"github.com/marrasen/gridterm/pasted"
	shellfind "github.com/marrasen/gridterm/shells"
	"github.com/marrasen/gridterm/vfs"
)

// Files dropped on a terminal from another program go where the program
// in it can open them, as gridterm puts them. A program reading a
// terminal cannot be handed a file, so the file is put where that
// program is already looking: the folder the shell last said it was in.
// A shell that has said none leaves nowhere to put it, and then the path
// is typed instead, after a copy for a file that has to reach another
// machine. Each copy is a job, with a card saying how far it has got.

// DropFiles puts files dropped on the window into a terminal pane: the
// one they were dropped on, or the focused one when Pane is empty.
type DropFiles struct {
	Pane  string
	Paths []string
}

// dropFiles hands dropped files to the machine a pane runs on.
func (a *app) dropFiles(in DropFiles) error {
	id := in.Pane
	if id == "" {
		id = a.st.Focus
	}
	t := a.terminal(id)
	switch {
	case len(in.Paths) == 0:
		return nil
	case t == nil:
		return errors.New("dropped files go into a terminal, and none has the focus")
	case a.farHost[id] != "":
		return fmt.Errorf("this pane runs on %s, which %s reached, and this window has no way to put a file there", a.farHost[id], a.machineOf(id))
	}
	machine := a.machineOf(id)
	if dir, ok := a.droppedInto(id); ok {
		return a.copyDropped(machine, in.Paths, dir)
	}
	if machine == "" {
		// Already on the machine the program runs on, so the path is
		// the whole of it.
		paths := make([]string, 0, len(in.Paths))
		for _, p := range in.Paths {
			paths = append(paths, a.pathForPane(id, p))
		}
		t.Paste(pasted.Typed(paths))
		return nil
	}
	return a.uploadDropped(id, machine, in.Paths)
}

// droppedInto is the folder a file dropped on a pane goes in, and
// whether the shell in it has said where it is, through OSC 7.
func (a *app) droppedInto(id string) (string, bool) {
	dir, _ := a.terminal(id).Dir()
	if dir == "" {
		return "", false
	}
	if a.machineOf(id) != "" {
		// The machine at the far end names its own paths.
		return dir, true
	}
	if distro := a.distroOf(id); distro != "" {
		// A pane in WSL says a path inside the distribution, which this
		// machine reaches on a share of its own.
		at := shellfind.WindowsPath(distro, dir)
		return at, at != ""
	}
	return dir, true
}

// copyDropped copies dropped files into the folder the shell in a pane
// said it was in, and says so once they are there. Nothing is typed:
// the files are where the program is already looking.
func (a *app) copyDropped(machine string, paths []string, dir string) error {
	return a.withFiles(machine, func(to vfs.FS) {
		var started []*jobs.Job
		var already []string
		for _, path := range paths {
			if machine == "" && sameDir(filepath.Dir(path), dir) {
				// Already there: copying it would copy a file onto
				// itself.
				already = append(already, filepath.Base(path))
				continue
			}
			op := jobs.Op{Kind: jobs.Copy, From: a.fsFor(""), At: filepath.Dir(path), Names: []string{filepath.Base(path)}, To: to, Into: dir}
			started = append(started, a.followOn(op, "Copying "+filepath.Base(path)+" to "+vfs.Base(to, dir), "", machine))
		}
		if len(already) > 0 {
			a.notify(arrived(already, dir, machine), "Already there.", "")
		}
		if len(started) > 0 {
			a.sayWhenArrived(started, paths, dir, machine)
		}
	})
}

// sameDir reports whether two paths name one folder on this machine.
func sameDir(a, b string) bool {
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}

// sayWhenArrived waits for a drop's copies and says what landed, in one
// notice for the drop: a handful of files dragged in together is one
// thing the user did.
func (a *app) sayWhenArrived(started []*jobs.Job, paths []string, dir, machine string) {
	go func() {
		failed := 0
		for _, j := range started {
			<-j.Done()
			if j.Progress().Err != nil {
				failed++
			}
		}
		a.events <- func() {
			if failed == len(started) {
				// Every card already says what went wrong, and a copy
				// stopped on purpose is no failure to report.
				return
			}
			names := make([]string, 0, len(paths))
			for _, p := range paths {
				names = append(names, filepath.Base(p))
			}
			body := ""
			if failed > 0 {
				body = strconv.Itoa(failed) + " failed. The Jobs pane says why."
			}
			a.notify(arrived(names, dir, machine), body, "")
		}
	}()
}

// arrived says what landed and where.
func arrived(names []string, dir, machine string) string {
	what := "Copied " + strconv.Itoa(len(names)) + " files"
	if len(names) == 1 {
		what = "Copied " + names[0]
	}
	where := "this computer"
	if machine != "" {
		where = machine
	}
	return fmt.Sprintf("%s to %s on %s", what, dir, where)
}

// uploadDropped copies files into the folder for pasted files on the
// machine a pane runs on, and types each path once it is there. One
// job for each, so each has a card of its own and a cross that stops
// that one.
func (a *app) uploadDropped(id, machine string, paths []string) error {
	t := a.terminal(id)
	return a.withFiles(machine, func(to vfs.FS) {
		// Found off the program's goroutine, since it asks the machine.
		go func() {
			dir, err := pasted.DirOn(to)
			a.events <- func() {
				if err != nil {
					a.notify("Couldn't copy the files to "+machine, err.Error(), "")
					return
				}
				for _, path := range paths {
					name := filepath.Base(path)
					op := jobs.Op{Kind: jobs.Copy, From: a.fsFor(""), At: filepath.Dir(path), Names: []string{name}, To: to, Into: dir}
					j := a.followOn(op, "Copying "+name+" to "+machine, "", machine)
					at := strings.TrimSuffix(dir, string(to.Sep())) + string(to.Sep()) + name
					go func() {
						<-j.Done()
						a.events <- func() {
							switch {
							case j.Progress().Err != nil:
								// The card says what went wrong.
							case a.terminal(id) == t:
								t.Paste(pasted.Typed([]string{at}))
							default:
								// The pane closed while the file was on
								// its way; the notice's Copy is the way
								// left to the path.
								a.notify("File copied", at+" on "+machine+".", at)
							}
						}
					}()
				}
			}
		}()
	})
}
