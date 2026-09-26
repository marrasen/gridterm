package main

import (
	"cmp"
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/pkg/sftp"

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
	// Name is the file's name, for telling its kind; Pic is it, read as
	// a picture; SoFar is how much of it a read has got through.
	Name  string
	Pic   *files.Pic
	SoFar int64
	// Find opens the reader with its find bar open, as the scrollback
	// is opened, to be searched.
	Find bool
	// Saves counts the reader's saves that have finished, and SaveErr
	// is why the last one failed, empty when it worked.
	Saves   int
	SaveErr string
}

// Intents for readers.
type (
	// ReadAgain reads a reader pane's file again.
	ReadAgain struct{ Pane string }
	// SaveLines writes lines to a file at Path on this machine, for
	// the reader in Pane, which is told how it went.
	SaveLines struct {
		Pane, Path string
		Lines      []string
	}
)

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
	f := a.fsFor(a.filesKey(in.Pane))
	if !ok || f == nil {
		return
	}
	a.readFile(ReadFile{Pane: in.Pane, Path: vfs.Join(f, b.Path, in.Name), Follow: in.Follow})
}

// goTo shows the folder typed.
func (a *app) goTo(in GoTo) {
	f := a.fsFor(a.filesKey(in.Pane))
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
	f := a.fsFor(a.filesKey(in.Pane))
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
	f := a.fsFor(a.filesKey(in.Pane))
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
func (a *app) openFiles() error { return a.filesOn(a.filesKey(a.st.Focus), "") }

// filesOn opens a file pane on machine, at path, or at home when path
// is empty.
func (a *app) filesOn(machine, path string) error {
	return a.withFiles(machine, func(f vfs.FS) {
		if err := a.openFilesOn(machine, f, path); err != nil {
			a.notify("Couldn't open the files on "+placeName(machine), err.Error(), "")
		}
	})
}

// farSep joins a window and a machine it reached, in the name the
// files on that machine are kept under.
const farSep = "\x00"

// farFiles names the files on a machine a window reached, as a
// filesystem tells whose files it holds.
type farFiles struct {
	w    *remoteWin
	host string
}

// filesKey is the name the files a pane's program sees are kept under:
// its machine's, or for a pane attached from a window, running on a
// machine that window reached, that machine's through the window.
func (a *app) filesKey(id string) string {
	if host := a.farHost[id]; host != "" {
		return a.machineOf(id) + farSep + host
	}
	return a.machineOf(id)
}

// paneOn files p under the machine a files key names: a window, for a
// machine that window reached, with the machine noted.
func (a *app) paneOn(key string, p Pane) Pane {
	if window, host, far := strings.Cut(key, farSep); far {
		a.farHost[p.ID] = host
		p.Machine, p.On = window, host
		return p
	}
	p.Machine = key
	return p
}

// placeName is what a files key is called where the user reads it.
func placeName(key string) string {
	if window, host, far := strings.Cut(key, farSep); far {
		return host + " through " + window
	}
	if key == "" {
		return "this computer"
	}
	return key
}

// withFiles runs then with machine's files, on the program's goroutine,
// opening them first when they are not open: over a server's
// connection, or a window's, with SFTP, once for all its file panes.
func (a *app) withFiles(machine string, then func(vfs.FS)) error {
	if f := a.fsFor(machine); f != nil {
		then(f)
		return nil
	}
	open := func() (vfs.FS, error) {
		return nil, fmt.Errorf("this window is not connected to %s", placeName(machine))
	}
	if window, host, far := strings.Cut(machine, farSep); far && a.windows[window] != nil {
		w := a.windows[window]
		open = func() (vfs.FS, error) {
			files, err := w.win.FilesOn(host)
			if err != nil {
				return nil, err
			}
			client, err := sftp.NewClientPipe(files, files)
			if err != nil {
				return nil, errors.Join(err, files.Close())
			}
			return vfs.NewSFTP(host, farFiles{w, host}, client, func() error { return errors.Join(client.Close(), files.Close()) }), nil
		}
	} else if w, ok := a.windows[machine]; ok {
		open = func() (vfs.FS, error) {
			files, err := w.win.Files()
			if err != nil {
				return nil, err
			}
			client, err := sftp.NewClientPipe(files, files)
			if err != nil {
				return nil, errors.Join(err, files.Close())
			}
			return vfs.NewSFTP(machine, w, client, func() error { return errors.Join(client.Close(), files.Close()) }), nil
		}
	} else if conn, ok := a.conns[machine]; ok {
		open = func() (vfs.FS, error) {
			files, err := conn.Files(a.ctx)
			if err != nil {
				return nil, err
			}
			return vfs.NewSFTP(machine, conn, files.Client(), files.Close), nil
		}
	} else {
		_, err := open()
		return err
	}
	a.st.Status = "Opening the files on " + placeName(machine) + "…"
	go func() {
		f, err := open()
		a.events <- func() {
			a.st.Status = ""
			if err != nil {
				a.notify("Couldn't open the files on "+placeName(machine), err.Error(), "")
				return
			}
			if have := a.fsFor(machine); have != nil {
				_ = f.Close()
				f = have
			} else {
				a.remoteFS[machine] = f
			}
			then(f)
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
	a.addPane(a.paneOn(machine, Pane{ID: id, Title: vfs.Base(f, path), Kind: kindFiles}), nil, placement{})
	a.browse(Browse{Pane: id, Path: path})
	return nil
}

// browse lists a folder for a file pane, in the background.
func (a *app) browse(in Browse) {
	f := a.fsFor(a.filesKey(in.Pane))
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
	machine := a.filesKey(in.Pane)
	f := a.fsFor(machine)
	if f == nil {
		return
	}
	a.readOn(machine, f, in.Path, in.Follow, 0, placement{beside: in.Pane})
}

// readOn opens path on a machine's files in a reader, at line when it
// is past zero, placed at at. A picture is read as a picture. A file
// followed is read again each time it changes.
func (a *app) readOn(machine string, f vfs.FS, path string, follow bool, line int, at placement) {
	a.next++
	id := "p" + itoa(a.next)
	title := vfs.Base(f, path)
	if follow {
		title += " (following)"
	}
	a.addPane(a.paneOn(machine, Pane{ID: id, Title: title, Kind: kindReader}), nil, at)
	a.reads[id] = readSpec{f: f, path: path, name: vfs.Base(f, path), follow: follow, line: line}
	a.readOnce(id)
	if follow {
		go a.followFile(id, f, path)
	}
}

// readSpec is what a reader pane reads, to read it again.
type readSpec struct {
	f      vfs.FS
	path   string
	name   string
	follow bool
	line   int
	seq    int
}

// mostPictureSide bounds a picture read, as gridterm bounds it.
const mostPictureSide = 4096

// readOnce reads a reader pane's file in the background, and publishes
// it: its lines, or its picture, with how far the read has got as it
// goes.
func (a *app) readOnce(id string) {
	spec, ok := a.reads[id]
	if !ok {
		return
	}
	var last time.Time
	watch := func(read int64) {
		if now := time.Now(); now.Sub(last) >= 100*time.Millisecond {
			last = now
			a.events <- func() {
				if r, ok := a.st.Readers[id]; ok {
					r.SoFar = read
					a.setReader(id, r)
				}
			}
		}
	}
	go func() {
		r := Reader{Path: spec.path, Name: spec.name, Follow: spec.follow, Line: spec.line}
		var err error
		if files.IsPicture(spec.name) {
			var pic files.Pic
			pic, err = files.ReadPictureWatched(spec.f, spec.path, mostPictureSide, watch)
			r.Pic = &pic
		} else {
			r.Lines, r.Cut, err = files.ReadFileWatched(spec.f, spec.path, watch)
		}
		if err != nil {
			r.Err = err.Error()
		}
		a.events <- func() {
			if !a.has(id) {
				return
			}
			spec := a.reads[id]
			spec.seq++
			a.reads[id] = spec
			r.Seq = spec.seq
			// A save that finished is still counted, for the reader to
			// hear how it went.
			was := a.st.Readers[id]
			r.Saves, r.SaveErr = was.Saves, was.SaveErr
			a.setReader(id, r)
		}
	}()
}

// setReader publishes a reader pane's state.
func (a *app) setReader(id string, r Reader) {
	m := make(map[string]Reader, len(a.st.Readers)+1)
	maps.Copy(m, a.st.Readers)
	m[id] = r
	a.st.Readers = m
}

// followFile reads a followed file again each time it changes, by its
// size and its time, as gridterm does, until its pane closes.
func (a *app) followFile(id string, f vfs.FS, path string) {
	var last vfs.Entry
	for {
		select {
		case <-a.ctx.Done():
			return
		case <-time.After(time.Second):
		}
		gone := make(chan bool, 1)
		a.events <- func() { gone <- !a.has(id) }
		if <-gone {
			return
		}
		e, err := f.Stat(path)
		if err != nil || (e.Size == last.Size && e.Mod.Equal(last.Mod)) {
			continue
		}
		first := last.Mod.IsZero()
		last = e
		if !first {
			a.events <- func() { a.readOnce(id) }
		}
	}
}

// saveLines writes what a reader shows to a file on this machine.
func (a *app) saveLines(in SaveLines) {
	at, err := expandHome(in.Path)
	if err == nil {
		err = os.WriteFile(at, []byte(strings.Join(in.Lines, "\n")+"\n"), 0o600)
	}
	r, ok := a.st.Readers[in.Pane]
	if !ok {
		// The pane closed while its save was out; a notice says how
		// it went instead.
		if err != nil {
			a.notify("Couldn't save", err.Error(), "")
		}
		return
	}
	r.Saves++
	r.SaveErr = ""
	if err != nil {
		r.SaveErr = err.Error()
	}
	a.setReader(in.Pane, r)
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
	a.addPane(a.paneOn(a.filesKey(pane), Pane{ID: id, Title: title, Kind: kindReader}), nil, placement{beside: pane})
	m := maps.Clone(a.st.Readers)
	if m == nil {
		m = map[string]Reader{}
	}
	m[id] = Reader{Path: title, Lines: lines, Seq: 1, Line: max(1, len(lines)), Find: true}
	a.st.Readers = m
	return nil
}
