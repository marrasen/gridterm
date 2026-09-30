package app

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/marrasen/kakel/jobs"
	"github.com/marrasen/kakel/machines"
	"github.com/marrasen/kakel/vfs"
	"github.com/marrasen/kakel/words"
)

// Files dragged out of a file pane land in another file pane, in any of
// kakel's windows, or outside kakel in another program; files dragged
// in from another program land in a file pane. Between two folders of
// one volume a drop moves, and anywhere else it copies, as a file
// manager does; Ctrl copies and Shift moves whatever the two are. Each
// drop is a job, with a card saying how far it has got.

// FileDrag is what rows dragged out of a file pane carry.
type FileDrag struct {
	// Pane is the file pane they came from, in Window, and Machine the
	// machine its files are on, by the pane's key for its files.
	Pane    string
	Window  int
	Machine machines.ID
	// At is the folder they are in, and Names and Dirs what they are.
	At    string
	Names []string
	Dirs  []bool
	// Sep is the separator of the machine's paths, and Local says they
	// are files on this computer, outside an archive, which another
	// program can be given. Volume is the volume At is on, and Archive
	// says it is inside an archive, where nothing can be taken away.
	Sep     string
	Local   bool
	Volume  string
	Archive bool
}

// ExportFiles implements gunim's FileExporter: files on this computer
// leave kakel as their paths, for Explorer or another program to take.
// Files on another machine stay inside kakel.
func (d FileDrag) ExportFiles() ([]string, error) {
	if !d.Local {
		return nil, errors.New("only files on this computer can be dragged out of kakel")
	}
	out := make([]string, 0, len(d.Names))
	for _, n := range d.Names {
		out = append(out, strings.TrimSuffix(d.At, d.Sep)+d.Sep+n)
	}
	return out, nil
}

// DropOnFiles puts what was dropped on a file pane in folder Into there:
// rows dragged from a file pane, Drag, or files from another program,
// Paths. Copy copies them, and otherwise they move.
type DropOnFiles struct {
	Pane  string
	Into  string
	Drag  FileDrag
	Paths []string
	Copy  bool
}

// dropOnFiles starts the jobs for a drop on a file pane.
func (a *app) dropOnFiles(in DropOnFiles) error {
	to, _, ok := a.folderOf(in.Pane)
	if !ok || in.Into == "" {
		return nil
	}
	toKey := a.filesKey(in.Pane)
	if len(in.Paths) > 0 {
		// From another program: files on this computer, copied, a job
		// for each folder they came from.
		byDir := map[string][]string{}
		var dirs []string
		for _, p := range in.Paths {
			d := filepath.Dir(p)
			if _, ok := byDir[d]; !ok {
				dirs = append(dirs, d)
			}
			byDir[d] = append(byDir[d], filepath.Base(p))
		}
		for _, d := range dirs {
			if toKey == "" && sameDir(d, in.Into) {
				continue
			}
			names := byDir[d]
			op := jobs.Op{Kind: jobs.Copy, From: a.fsFor(""), At: d, Names: names, To: to, Into: in.Into}
			a.followOn(op, fmt.Sprintf("Copying %s to %s", countNames(names), vfs.Base(to, in.Into)), "", toKey)
		}
		return nil
	}
	d := in.Drag
	if len(d.Names) == 0 {
		return nil
	}
	if !a.has(d.Pane) || a.filesKey(d.Pane) != d.Machine {
		return errors.New("the pane they were dragged from has closed or moved to another machine")
	}
	from := a.filesOf(d.Pane)
	if from == nil {
		return errors.New("the files they were dragged from are no longer reachable")
	}
	if vfs.Same(from, to) && samePathOn(from, d.At, in.Into) {
		// Dropped where they are: nothing to do.
		return nil
	}
	if vfs.Same(from, to) && slices.ContainsFunc(d.Names, func(n string) bool { return underDir(from, vfs.Join(from, d.At, n), in.Into) }) {
		return errors.New("a folder cannot go inside itself")
	}
	kind, verb := jobs.Move, "Moving"
	if in.Copy {
		kind, verb = jobs.Copy, "Copying"
	}
	if c := a.clip; kind == jobs.Move && c != nil && c.kind == jobs.Move && c.machine == d.Machine && samePathOn(from, c.at, d.At) {
		// What was cut and moved here has gone somewhere else now; the
		// rest is still waiting to be pasted.
		left := slices.DeleteFunc(slices.Clone(c.names), func(n string) bool { return slices.Contains(d.Names, n) })
		if len(left) < len(c.names) {
			c.names = left
			if len(left) == 0 {
				a.clip = nil
				a.say("clip", "")
			}
		}
	}
	op := jobs.Op{Kind: kind, From: from, At: d.At, Names: slices.Clone(d.Names), To: to, Into: in.Into}
	a.followOn(op, fmt.Sprintf("%s %s to %s", verb, countNames(d.Names), vfs.Base(to, in.Into)), d.Machine, toKey)
	return nil
}

// countNames says how many names there are: the one by its name, more as a
// number of items.
func countNames(names []string) string {
	if len(names) == 1 {
		return names[0]
	}
	return words.Count(len(names), "item")
}

// underDir reports whether path at is folder dir or under it, on f.
func underDir(f vfs.FS, dir, at string) bool {
	sep := string(f.Sep())
	dir = strings.TrimSuffix(dir, sep)
	return samePathOn(f, at, dir) || len(at) > len(dir) && samePathOn(f, at[:len(dir)], dir) && strings.HasPrefix(at[len(dir):], sep)
}

// samePathOn reports whether two paths of f are one: letter case aside
// where paths are Windows'.
func samePathOn(f vfs.FS, x, y string) bool {
	sep := string(f.Sep())
	x, y = strings.TrimSuffix(x, sep), strings.TrimSuffix(y, sep)
	if sep == "\\" {
		return strings.EqualFold(x, y)
	}
	return x == y
}
