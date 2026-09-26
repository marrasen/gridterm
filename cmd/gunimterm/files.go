package main

import (
	"cmp"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/marrasen/gridterm/ui/files"
	"github.com/marrasen/gridterm/vfs"
)

// File panes and readers, on the program's side: listing folders and
// reading files happen on goroutines of their own, and what they find
// is published for the window to show.

// Browser is what a file pane shows: a folder and what is in it.
type Browser struct {
	Path    string
	Entries []vfs.Entry
	// Land names the entry the cursor goes to once the folder shows,
	// as the folder just left, going up.
	Land string
	// Err says why the folder could not be read.
	Err string
	// Seq counts the listings, so the window knows a new one.
	Seq int
}

// Reader is what a reader pane shows: a file's lines.
type Reader struct {
	Path  string
	Lines []string
	// Cut says the file was longer than a reader holds.
	Cut bool
	Err string
	// Follow says the reader follows the file as it grows, and Seq
	// counts its reads.
	Follow bool
	Seq    int
	// Line is the line to show first, counted from 1, and 0 for the top.
	Line int
	// Find opens the reader with its find bar open, as the scrollback
	// is opened, to be searched.
	Find bool
}

// Intents for files.
type (
	// OpenFiles opens a file pane where the focused pane is, at home.
	OpenFiles struct{}
	// Browse shows path in a file pane, with the cursor on land.
	Browse struct{ Pane, Path, Land string }
	// ReadFile opens path in a reader beside the file pane, following
	// it as it grows with Follow.
	ReadFile struct {
		Pane, Path string
		Follow     bool
	}
	// EnterEntry goes into a folder of a file pane, or opens a file in
	// a reader.
	EnterEntry struct{ Pane, Name string }
	// GoUp shows the folder above a file pane's, with the cursor on the
	// one it came from.
	GoUp struct{ Pane string }
	// GoTo shows a folder typed as a path, ~ standing for home.
	GoTo struct{ Pane, Path string }
	// ViewFile reads the file named in a file pane's folder, following
	// it as it grows with Follow.
	ViewFile struct {
		Pane, Name string
		Follow     bool
	}
)

// viewFile reads a file of a file pane's folder.
func (a *app) viewFile(in ViewFile) {
	b, ok := a.st.Browsers[in.Pane]
	f := a.fsFor(a.machineOf(in.Pane))
	if !ok || f == nil {
		return
	}
	a.readFile(ReadFile{Pane: in.Pane, Path: vfs.Join(f, b.Path, in.Name), Follow: in.Follow})
}

// goTo shows the folder typed.
func (a *app) goTo(in GoTo) {
	f := a.fsFor(a.machineOf(in.Pane))
	if f == nil {
		return
	}
	path := strings.TrimSpace(in.Path)
	if rest, ok := strings.CutPrefix(path, "~"); ok && (rest == "" || rest[0] == '/' || rest[0] == f.Sep()) {
		home, err := f.Home()
		if err != nil {
			a.notify("Couldn't find home", err.Error(), "")
			return
		}
		path = home + rest
	}
	a.browse(Browse{Pane: in.Pane, Path: path})
}

// enter goes into the folder named name, or reads the file.
func (a *app) enter(in EnterEntry) {
	b, ok := a.st.Browsers[in.Pane]
	f := a.fsFor(a.machineOf(in.Pane))
	if !ok || f == nil {
		return
	}
	path := vfs.Join(f, b.Path, in.Name)
	for _, e := range b.Entries {
		if e.Name == in.Name && !e.IsDir() {
			a.readFile(ReadFile{Pane: in.Pane, Path: path})
			return
		}
	}
	a.browse(Browse{Pane: in.Pane, Path: path})
}

// goUp shows the folder above a file pane's.
func (a *app) goUp(in GoUp) {
	b, ok := a.st.Browsers[in.Pane]
	f := a.fsFor(a.machineOf(in.Pane))
	if !ok || f == nil || vfs.IsTop(f, b.Path) {
		return
	}
	a.browse(Browse{Pane: in.Pane, Path: vfs.Dir(f, b.Path), Land: vfs.Base(f, b.Path)})
}

// Pane kinds.
const (
	kindTerminal = ""
	kindFiles    = "files"
	kindReader   = "reader"
)

// fsFor returns the filesystem of machine, "" for this computer, or
// nil for a server whose files are not open yet.
func (a *app) fsFor(machine string) vfs.FS {
	if machine == "" {
		if a.local == nil {
			a.local = vfs.NewLocal()
		}
		return a.local
	}
	return a.remoteFS[machine]
}

// openFiles opens a file pane at home on the focused pane's machine.
func (a *app) openFiles() error { return a.filesOn(a.machineOf(a.st.Focus), "") }

// filesOn opens a file pane on machine, at path, or at home when path
// is empty. A server's files open over its connection first, with
// SFTP, once for all its file panes.
func (a *app) filesOn(machine, path string) error {
	if f := a.fsFor(machine); f != nil {
		return a.openFilesOn(machine, f, path)
	}
	if _, ok := a.windows[machine]; ok {
		a.windowFiles(machine, path)
		return nil
	}
	conn, ok := a.conns[machine]
	if !ok {
		return fmt.Errorf("this window is not connected to %s", machine)
	}
	a.st.Status = "Opening the files on " + machine + "…"
	go func() {
		files, err := conn.Files(a.ctx)
		a.events <- func() {
			a.st.Status = ""
			if err != nil {
				a.notify("Couldn't open the files on "+machine, err.Error(), "")
				return
			}
			f := vfs.NewSFTP(machine, conn, files.Client(), files.Close)
			a.remoteFS[machine] = f
			if err := a.openFilesOn(machine, f, path); err != nil {
				a.notify("Couldn't open the files on "+machine, err.Error(), "")
			}
		}
	}()
	return nil
}

// openFilesOn opens a file pane on a machine whose files are open, at
// path, or at home when path is empty.
func (a *app) openFilesOn(machine string, f vfs.FS, path string) error {
	if path == "" {
		home, err := f.Home()
		if err != nil {
			return err
		}
		path = home
	}
	a.next++
	id := "p" + itoa(a.next)
	a.addPane(Pane{ID: id, Title: vfs.Base(f, path), Machine: machine, Kind: kindFiles}, nil, placement{})
	a.browse(Browse{Pane: id, Path: path})
	return nil
}

// browse lists a folder for a file pane, in the background.
func (a *app) browse(in Browse) {
	f := a.fsFor(a.machineOf(in.Pane))
	if f == nil {
		return
	}
	go func() {
		entries, err := f.ReadDir(in.Path)
		a.events <- func() {
			b := a.st.Browsers[in.Pane]
			if err != nil {
				b.Err = err.Error()
				a.setBrowser(in.Pane, b)
				return
			}
			order(entries)
			a.setBrowser(in.Pane, Browser{Path: in.Path, Entries: entries, Land: in.Land, Seq: b.Seq + 1})
			a.retitleAs(in.Pane, vfs.Base(f, in.Path))
		}
	}()
}

// setBrowser replaces a file pane's state. The map is copied, so the
// state the window holds is never changed under it.
func (a *app) setBrowser(id string, b Browser) {
	m := make(map[string]Browser, len(a.st.Browsers)+1)
	for k, v := range a.st.Browsers {
		m[k] = v
	}
	m[id] = b
	a.st.Browsers = m
}

// readFile opens a file in a reader beside its file pane, and, to
// follow it, reads it again each time it changes.
func (a *app) readFile(in ReadFile) {
	machine := a.machineOf(in.Pane)
	f := a.fsFor(machine)
	if f == nil {
		return
	}
	a.readOn(machine, f, in.Path, in.Follow, 0, placement{beside: in.Pane})
}

// readOn opens path on a machine's files in a reader, at line when it
// is past zero, placed at at.
func (a *app) readOn(machine string, f vfs.FS, path string, follow bool, line int, at placement) {
	in := ReadFile{Path: path, Follow: follow}
	a.next++
	id := "p" + itoa(a.next)
	title := vfs.Base(f, in.Path)
	if in.Follow {
		title += " (following)"
	}
	a.addPane(Pane{ID: id, Title: title, Machine: machine, Kind: kindReader}, nil, at)
	go func() {
		var last vfs.Entry
		seq := 0
		for {
			lines, cut, err := files.ReadFile(f, in.Path)
			seq++
			r := Reader{Path: in.Path, Lines: lines, Cut: cut, Follow: in.Follow, Seq: seq, Line: line}
			if err != nil {
				r.Err = err.Error()
			}
			open := make(chan bool, 1)
			a.events <- func() {
				if !a.has(id) {
					open <- false
					return
				}
				m := make(map[string]Reader, len(a.st.Readers)+1)
				for k, v := range a.st.Readers {
					m[k] = v
				}
				m[id] = r
				a.st.Readers = m
				open <- true
			}
			if !<-open || !in.Follow {
				return
			}
			// Following: wait for the file to change, as gridterm does,
			// by its size and its time.
			for {
				select {
				case <-a.ctx.Done():
					return
				case <-time.After(time.Second):
				}
				e, err := f.Stat(in.Path)
				if err == nil && (e.Size != last.Size || !e.Mod.Equal(last.Mod)) {
					changed := last.Mod != (time.Time{})
					last = e
					if changed {
						break
					}
				}
				gone := make(chan bool, 1)
				a.events <- func() { gone <- !a.has(id) }
				if <-gone {
					return
				}
			}
		}
	}()
}

// order sorts a folder's entries: folders first, then by name, as a
// person reads them, whatever their case.
func order(entries []vfs.Entry) {
	slices.SortFunc(entries, func(a, b vfs.Entry) int {
		if a.IsDir() != b.IsDir() {
			if a.IsDir() {
				return -1
			}
			return 1
		}
		if n := cmp.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)); n != 0 {
			return n
		}
		return cmp.Compare(a.Name, b.Name)
	})
}

// retitleAs names a pane that has no name of the user's.
func (a *app) retitleAs(id, title string) {
	for i := range a.st.Panes {
		if p := &a.st.Panes[i]; p.ID == id && !p.Named {
			p.Title = title
		}
	}
}

// showScrollback opens what a terminal pane has kept in a reader
// beside it, at the end, with the find bar open.
func (a *app) showScrollback(pane string) error {
	t := a.terminal(pane)
	if t == nil {
		return errors.New("the pane in front is not a terminal, so it has no scrollback")
	}
	lines := strings.Split(strings.TrimRight(t.AllText(), "\n "), "\n")
	a.next++
	id := "p" + itoa(a.next)
	title := "Scrollback of " + a.titleOf(pane)
	a.addPane(Pane{ID: id, Title: title, Machine: a.machineOf(pane), Kind: kindReader}, nil, placement{beside: pane})
	m := maps.Clone(a.st.Readers)
	if m == nil {
		m = map[string]Reader{}
	}
	m[id] = Reader{Path: title, Lines: lines, Seq: 1, Line: max(1, len(lines)), Find: true}
	a.st.Readers = m
	return nil
}
