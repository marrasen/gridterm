package main

import (
	"cmp"
	"slices"
	"strings"

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
}

// Intents for files.
type (
	// OpenFiles opens a file pane where the focused pane is, at home.
	OpenFiles struct{}
	// Browse shows path in a file pane, with the cursor on land.
	Browse struct{ Pane, Path, Land string }
	// ReadFile opens path in a reader beside the file pane.
	ReadFile struct{ Pane, Path string }
	// EnterEntry goes into a folder of a file pane, or opens a file in
	// a reader.
	EnterEntry struct{ Pane, Name string }
	// GoUp shows the folder above a file pane's, with the cursor on the
	// one it came from.
	GoUp struct{ Pane string }
)

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

// fsFor returns the filesystem of machine, "" for this computer.
func (a *app) fsFor(machine string) vfs.FS {
	if machine == "" {
		if a.local == nil {
			a.local = vfs.NewLocal()
		}
		return a.local
	}
	return nil
}

// openFiles opens a file pane at home on the focused pane's machine.
func (a *app) openFiles() error {
	machine := a.machineOf(a.st.Focus)
	f := a.fsFor(machine)
	if f == nil {
		machine, f = "", a.fsFor("")
	}
	home, err := f.Home()
	if err != nil {
		return err
	}
	a.next++
	id := "p" + itoa(a.next)
	a.addPane(Pane{ID: id, Title: vfs.Base(f, home), Machine: machine, Kind: kindFiles}, nil, placement{})
	a.browse(Browse{Pane: id, Path: home})
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

// readFile opens a file in a reader beside its file pane.
func (a *app) readFile(in ReadFile) {
	machine := a.machineOf(in.Pane)
	f := a.fsFor(machine)
	if f == nil {
		return
	}
	a.next++
	id := "p" + itoa(a.next)
	a.addPane(Pane{ID: id, Title: vfs.Base(f, in.Path), Machine: machine, Kind: kindReader}, nil, placement{beside: in.Pane})
	go func() {
		lines, cut, err := files.ReadFile(f, in.Path)
		r := Reader{Path: in.Path, Lines: lines, Cut: cut}
		if err != nil {
			r.Err = err.Error()
		}
		a.events <- func() {
			m := make(map[string]Reader, len(a.st.Readers)+1)
			for k, v := range a.st.Readers {
				m[k] = v
			}
			m[id] = r
			a.st.Readers = m
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
