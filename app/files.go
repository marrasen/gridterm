package app

import (
	"cmp"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/marrasen/kakel/conf"
	"github.com/marrasen/kakel/machines"

	"github.com/pkg/sftp"

	"github.com/marrasen/kakel/ui/files"
	uiterm "github.com/marrasen/kakel/ui/term"
	"github.com/marrasen/kakel/vfs"
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
	// WentTo is the last Go To answered, and GoToErr why it could not
	// go there, empty when it went.
	WentTo  int
	GoToErr string
	// Seq counts the listings, so the window knows a new one, and Top
	// says Path is the top of its filesystem.
	Seq int
	Top bool
	// Roots are where the filesystem starts, such as each drive, Sep its
	// separator, and Listed the folders last listed for Go To.
	Roots  []string
	Sep    string
	Listed Listed
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
	// is opened, to be searched, and FindAgain counts the times it is
	// asked to open it again after.
	Find      bool
	FindAgain int
	// Of is the terminal pane whose scrollback the reader shows, which
	// reading again reads again; SaveAs is where a save is offered.
	Of, SaveAs string
	// Saves counts the reader's saves that have finished, and SaveErr
	// is why the last one failed, empty when it worked.
	Saves   int
	SaveErr string
	// Expect is how large the file was listed as, for the reader to say
	// how far a read has got of it, and 0 when nobody said.
	Expect int64
	// Gone says why a scrollback's reader has nothing left to read
	// again, its pane having closed.
	Gone string
	// Text says a file named as a picture is read as lines: it was not
	// one.
	Text bool
}

// Intents for readers.
type (
	// ReadAgain reads a reader pane's file again: as lines with Text,
	// for a file named as a picture that is not one.
	ReadAgain struct {
		Pane string
		Text bool
	}
	// FollowFile has a reader pane follow its file, reading it again as
	// it changes, or stop.
	FollowFile struct {
		Pane string
		On   bool
	}
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
	Browse struct {
		Pane, Path, Land string
		// GoTo is the Go To it answers, and 0 for none.
		GoTo int
	}
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
	GoTo struct {
		Pane, Path string
		// Ask numbers it, for the pane to hear how it went.
		Ask int
	}
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
	f := a.filesOf(in.Pane)
	if !ok || f == nil {
		return
	}
	a.readFile(ReadFile{Pane: in.Pane, Path: vfs.Join(f, b.Path, in.Name), Follow: in.Follow})
}

// goTo shows the folder typed.
func (a *app) goTo(in GoTo) {
	f := a.filesOf(in.Pane)
	if f == nil {
		return
	}
	path := strings.TrimSpace(in.Path)
	if rest, ok := strings.CutPrefix(path, "~"); ok && (rest == "" || rest[0] == '/' || rest[0] == f.Sep()) {
		home, err := f.Home()
		if err != nil {
			b := a.st.Browsers[in.Pane]
			b.WentTo, b.GoToErr = in.Ask, "couldn't find home: "+err.Error()
			a.setBrowser(in.Pane, b)
			return
		}
		path = home + rest
	}
	a.browse(Browse{Pane: in.Pane, Path: path, GoTo: in.Ask})
}

// enter goes into the folder named name, or reads the file.
func (a *app) enter(in EnterEntry) {
	b, ok := a.st.Browsers[in.Pane]
	f := a.filesOf(in.Pane)
	if !ok || f == nil {
		return
	}
	path := vfs.Join(f, b.Path, in.Name)
	for _, e := range b.Entries {
		if e.Name == in.Name && !e.IsDir() && !vfs.IsArchive(e.Name) {
			a.readFile(ReadFile{Pane: in.Pane, Path: path})
			return
		}
	}
	a.browse(Browse{Pane: in.Pane, Path: path})
}

// goUp shows the folder above a file pane's.
func (a *app) goUp(in GoUp) {
	b, ok := a.st.Browsers[in.Pane]
	f := a.filesOf(in.Pane)
	if !ok || f == nil || vfs.IsTop(f, b.Path) {
		return
	}
	a.browse(Browse{Pane: in.Pane, Path: vfs.Dir(f, b.Path), Land: vfs.Base(f, b.Path)})
}

// Pane kinds.
const (
	KindTerminal = ""
	KindFiles    = "files"
	KindReader   = "reader"
)

// fsFor returns the filesystem of machine, "" for this computer, or
// nil for a server whose files are not open yet.
func (a *app) fsFor(machine machines.ID) vfs.FS {
	if machine == "" {
		if a.local == nil {
			a.local = vfs.NewLocal()
		}
		return a.local
	}
	return a.machines.Get(machine).Files
}

// filesOf is what a file pane reads: its machine's files, with the
// archives on them opened as folders. One wrapper for each pane, since
// each holds the archive it is in, and two panes in two archives would
// take turns throwing each other's out.
func (a *app) filesOf(pane string) vfs.FS {
	under := a.fsFor(a.filesKey(pane))
	if under == nil {
		return nil
	}
	if w, ok := a.paneFiles[pane]; ok && w.under == under {
		return w.over
	}
	w := wrappedFiles{under: under, over: vfs.WithArchives(under)}
	a.paneFiles[pane] = w
	return w.over
}

// wrappedFiles is a pane's view of its machine's files.
type wrappedFiles struct{ under, over vfs.FS }

// openFiles opens a file pane at home on the focused pane's machine.
func (a *app) openFiles() error { return a.filesOn(a.filesKey(a.st.Focus), "") }

// filesOn opens a file pane on machine, at path, or when path is empty
// at the one folder saved for the machine, or else at home.
func (a *app) filesOn(machine machines.ID, path string) error {
	return a.withFiles(machine, func(f vfs.FS) {
		if err := a.openFilesOn(machine, f, path); err != nil {
			a.failed("Couldn't open the files on "+a.machines.Name(machine), err.Error())
		}
	})
}

// KeptFarSep joins, in the kept form of a machine beyond a window, the
// window as it is kept and the window's name for the machine: how
// saved commands, tunnels and copies name where they run.
const KeptFarSep = "\x00"

// farFiles names the files on a machine a window reached, as a
// filesystem tells whose files it holds.
type farFiles struct {
	w    *machines.Window
	host string
}

// filesKey is the name the files a pane's program sees are kept under:
// its machine's, or for a pane attached from a window, running on a
// machine that window reached, that machine's through the window.
func (a *app) filesKey(id string) machines.ID {
	if host := a.farHost[id]; host != "" {
		return machines.FarID(a.machineOf(id), host)
	}
	return a.machineOf(id)
}

// paneOn files p under the machine a files key names: a window, for a
// machine that window reached, with the machine noted.
func (a *app) paneOn(key machines.ID, p Pane) Pane {
	if window, host, far := key.Far(); far {
		a.farHost[p.ID] = host
		p.Machine, p.On = window, host
		return p
	}
	p.Machine = key
	return p
}

// withFiles runs then with machine's files, on the program's goroutine,
// opening them first when they are not open: over a server's
// connection, or a window's, with SFTP, once for all its file panes.
func (a *app) withFiles(machine machines.ID, then func(vfs.FS)) error {
	return a.withFilesOr(machine, then, func() {})
}

// withFilesOr is withFiles, running failed when the files could not be
// opened after it returned.
func (a *app) withFilesOr(machine machines.ID, then func(vfs.FS), failed func()) error {
	if c := a.machines.Get(machine).Conn; c != nil {
		// Its files came over the connection as it was saved then.
		if err := a.machines.SavedOtherwise(machine); err != nil {
			return err
		}
	}
	if f := a.fsFor(machine); f != nil {
		then(f)
		return nil
	}
	open := a.filesOpener(machine)
	if open == nil {
		if _, _, far := machine.Far(); !far && machine != machines.Local {
			// Not connected: connected to first.
			return a.dialAgain(machine, func(err error) {
				if err != nil {
					failed()
					return
				}
				if err := a.withFilesOr(machine, then, failed); err != nil {
					failed()
					a.failed("Couldn't open the files on "+a.machines.Name(machine), err.Error())
				}
			})
		}
		return fmt.Errorf("this window is not connected to %s", a.machines.Name(machine))
	}
	opening := "files " + string(machine)
	a.say(opening, "Opening the files on "+a.machines.Name(machine)+"…")
	a.starting++
	go func() {
		f, err := open()
		a.events <- func() {
			a.starting--
			a.say(opening, "")
			if err != nil {
				failed()
				a.failed("Couldn't open the files on "+a.machines.Name(machine), err.Error())
				return
			}
			then(a.keepFiles(machine, f))
		}
	}()
	return nil
}

// filesOpener returns what opens a machine's files with SFTP over the
// connection this window has to it, a server's or a window's, or nil
// when it has none. What it returns runs on a goroutine of its own.
func (a *app) filesOpener(machine machines.ID) func() (vfs.FS, error) {
	// Named as the user knows it, in what goes wrong with its files.
	called := a.machines.Name(machine)
	if window, host, far := machine.Far(); far && a.machines.Get(window).Window != nil {
		w := a.machines.Get(window).Window
		return func() (vfs.FS, error) {
			files, err := w.Serve.FilesOn(host)
			if err != nil {
				return nil, fmt.Errorf("the files on %s: %w", called, err)
			}
			client, err := sftp.NewClientPipe(files, files)
			if err != nil {
				return nil, errors.Join(err, files.Close())
			}
			return vfs.NewSFTP(called, farFiles{w, host}, client, func() error { return errors.Join(client.Close(), files.Close()) }), nil
		}
	}
	if w := a.machines.Get(machine).Window; w != nil {
		return func() (vfs.FS, error) {
			files, err := w.Serve.Files()
			if err != nil {
				return nil, err
			}
			client, err := sftp.NewClientPipe(files, files)
			if err != nil {
				return nil, errors.Join(err, files.Close())
			}
			return vfs.NewSFTP(called, w, client, func() error { return errors.Join(client.Close(), files.Close()) }), nil
		}
	}
	if conn, ok, err := a.connOf(machine); ok {
		if err != nil {
			return func() (vfs.FS, error) { return nil, err }
		}
		return func() (vfs.FS, error) {
			files, err := conn.Files(a.ctx)
			if err != nil {
				return nil, err
			}
			return vfs.NewSFTP(called, conn, files.Client(), files.Close), nil
		}
	}
	return nil
}

// keepFiles keeps f, a machine's files just opened, for its file panes
// and its links, and returns what is kept: the files opened meanwhile,
// when something else opened them first.
func (a *app) keepFiles(machine machines.ID, f vfs.FS) vfs.FS {
	if have := a.fsFor(machine); have != nil {
		_ = f.Close()
		return have
	}
	a.machines.At(machine).Files = f
	return f
}

// openFilesOn opens a file pane on a machine whose files are open, at
// path, or when path is empty at the one folder saved for the machine,
// or else at home.
func (a *app) openFilesOn(machine machines.ID, f vfs.FS, path string) error {
	if saved := a.savedFolders(machine); path == "" && len(saved) == 1 {
		// One folder saved for the machine is where its files open,
		// however they are asked for: it is the one the user wants.
		path = saved[0]
	}
	if path == "" {
		home, err := f.Home()
		if err != nil {
			return err
		}
		path = home
	}
	path = vfs.Spelled(f, path)
	a.next++
	id := "p" + itoa(a.next)
	// Beside the file pane in front: two side by side is the way to
	// copy between them.
	at := Placement{}
	if a.kindOfPane(a.st.Focus) == KindFiles {
		at.Beside = a.st.Focus
	}
	a.addPane(a.paneOn(machine, Pane{ID: id, Title: vfs.Base(f, path), Kind: KindFiles}), nil, at)
	a.browse(Browse{Pane: id, Path: path})
	return nil
}

// savedFolders are the folders saved for machine: a saved server's, or
// for a machine a window reached, the ones that window saved for it.
func (a *app) savedFolders(machine machines.ID) []string {
	if window, key, far := machine.Far(); far {
		if w := a.machines.Get(window).Window; w != nil {
			return w.Folders[key]
		}
		return nil
	}
	if h, ok := a.machines.Saved(machine); ok {
		return h.Folders
	}
	return nil
}

// browse lists a folder for a file pane, in the background.
func (a *app) browse(in Browse) {
	f := a.filesOf(in.Pane)
	if f == nil {
		return
	}
	in.Path = vfs.Spelled(f, in.Path)
	a.listing[in.Pane]++
	a.listingAt[in.Pane] = in
	asked := a.listing[in.Pane]
	go func() {
		entries, err := f.ReadDir(in.Path)
		a.events <- func() {
			if a.listing[in.Pane] != asked {
				// A later listing was asked for: this one is old news.
				return
			}
			delete(a.listingAt, in.Pane)
			b := a.st.Browsers[in.Pane]
			if err != nil {
				b.Err = err.Error()
				if in.GoTo != 0 {
					b.WentTo, b.GoToErr = in.GoTo, err.Error()
				}
				a.setBrowser(in.Pane, b)
				return
			}
			order(entries)
			a.setBrowser(in.Pane, Browser{Path: in.Path, Entries: entries, Land: in.Land, Seq: b.Seq + 1, Top: vfs.IsTop(f, in.Path),
				Roots: f.Roots(), Sep: sepOf(f), Listed: b.Listed, WentTo: max(b.WentTo, in.GoTo)})
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
	// The pane's view, which reads inside an archive too.
	f := a.filesOf(in.Pane)
	if f == nil {
		return
	}
	id := a.readOn(machine, f, in.Path, in.Follow, 0, Placement{Beside: in.Pane})
	// The size the listing says, for the reader to say how far it has
	// got of it.
	if b, ok := a.st.Browsers[in.Pane]; ok {
		for _, e := range b.Entries {
			if vfs.Join(f, b.Path, e.Name) == in.Path {
				r := a.st.Readers[id]
				r.Expect = e.Size
				a.setReader(id, r)
			}
		}
	}
}

// readOn opens path on a machine's files in a reader, at line when it
// is past zero, placed at at, and returns its pane. A picture is read
// as a picture. A file followed is read again each time it changes.
func (a *app) readOn(machine machines.ID, f vfs.FS, path string, follow bool, line int, at Placement) string {
	path = vfs.Spelled(f, path)
	a.next++
	id := "p" + itoa(a.next)
	name := vfs.Base(f, path)
	a.addPane(a.paneOn(machine, Pane{ID: id, Title: name, Kind: KindReader}), nil, at)
	under := a.fsFor(machine)
	window := a.machines.Get(machine).Window != nil
	_, _, far := machine.Far()
	window = window || far
	a.reads[id] = readSpec{f: f, under: under, machine: machine, window: window, archives: f != under, path: path, name: name, line: line}
	// There before the first read, so how far that read has got shows.
	a.setReader(id, Reader{Path: path, Name: name, Line: line})
	a.readOnce(id)
	if follow {
		a.followReader(id, true)
	}
	return id
}

// readSpec is what a reader pane reads, to read it again.
type readSpec struct {
	f    vfs.FS
	path string
	name string
	line int
	seq  int
	// machine is where the file is, and under the files there that f
	// reads through, with archives opened as folders when archives is
	// set. A connection that went takes those files with it, and a read
	// after goes through the files opened once it is back.
	machine  machines.ID
	under    vfs.FS
	archives bool
	// window says machine is another kakel window, or a machine it
	// reaches, which is connected to again from the sidebar, never by a
	// read: its name may be an address, and not one to sign in at.
	window bool
	// text reads a file named as a picture as lines, asked for once it
	// was not one.
	text bool
	// reading says a read is out, and again that another was asked for
	// meanwhile, made once it lands: reads never overlap, so an older
	// one cannot land last and put older text back.
	reading, again bool
}

// mostPictureSide bounds a picture read.
const mostPictureSide = 4096

// readOnce reads a reader pane's file in the background, and publishes
// it: its lines, or its picture, with how far the read has got as it
// goes. A server whose connection went is connected to again first.
func (a *app) readOnce(id string) {
	a.readerFiles(id, true, func(f vfs.FS) { a.readWith(id, f) })
}

// readerFiles hands then the files reader id reads through: the ones
// it was opened with, or those of its machine opened since, when the
// connection those came over went. With dial it connects again to a
// machine it is not connected to; without, it says it is not and gives
// up.
func (a *app) readerFiles(id string, dial bool, then func(vfs.FS)) {
	spec, ok := a.reads[id]
	if !ok {
		return
	}
	if spec.machine == "" || (spec.under != nil && a.fsFor(spec.machine) == spec.under) {
		then(spec.f)
		return
	}
	lost := func() {
		a.readerSays(id, "The connection to "+a.machines.Name(spec.machine)+" went. Ctrl+R connects again.")
	}
	if spec.window && a.fsFor(spec.machine) == nil {
		a.readerSays(id, "The connection to "+a.machines.Name(spec.machine)+" went. Connect to it again, then Ctrl+R.")
		return
	}
	if !dial && a.fsFor(spec.machine) == nil {
		lost()
		return
	}
	err := a.withFilesOr(spec.machine, func(under vfs.FS) {
		spec, ok := a.reads[id]
		if !ok {
			return
		}
		spec.under, spec.f = under, under
		if spec.archives {
			spec.f = vfs.WithArchives(under)
		}
		a.reads[id] = spec
		then(spec.f)
	}, lost)
	if err != nil {
		lost()
	}
}

// readWith reads reader id's file through f.
func (a *app) readWith(id string, f vfs.FS) {
	spec, ok := a.reads[id]
	if !ok {
		return
	}
	if spec.reading {
		spec.again = true
		a.reads[id] = spec
		return
	}
	spec.reading = true
	a.reads[id] = spec
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
		r := Reader{Path: spec.path, Name: spec.name, Line: spec.line, Text: spec.text}
		var err error
		if files.IsPicture(spec.name) && !spec.text {
			var pic files.Pic
			pic, err = files.ReadPictureWatched(f, spec.path, mostPictureSide, watch)
			r.Pic = &pic
		} else {
			r.Lines, r.Cut, err = files.ReadFileWatched(f, spec.path, watch)
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
			again := spec.again
			spec.reading, spec.again = false, false
			a.reads[id] = spec
			if again {
				defer a.readOnce(id)
			}
			r.Seq = spec.seq
			// A save that finished is still counted, for the reader to
			// hear how it went, and what was said of the file stays.
			was := a.st.Readers[id]
			r.Saves, r.SaveErr, r.SaveAs = was.Saves, was.SaveErr, was.SaveAs
			r.Follow, r.Expect = was.Follow, was.Expect
			a.setReader(id, r)
		}
	}()
}

// readerSays shows why in reader id in place of its file, counted as a
// read of its own, so the read after it is one the reader has not seen.
func (a *app) readerSays(id, why string) {
	spec, ok := a.reads[id]
	r, shown := a.st.Readers[id]
	if !ok || !shown || r.Err == why {
		// Said already: once is enough, and a read on its way is not
		// answered with it.
		return
	}
	spec.seq++
	a.reads[id] = spec
	r.Err, r.Seq = why, spec.seq
	a.setReader(id, r)
}

// setReader publishes a reader pane's state.
func (a *app) setReader(id string, r Reader) {
	m := make(map[string]Reader, len(a.st.Readers)+1)
	maps.Copy(m, a.st.Readers)
	m[id] = r
	a.st.Readers = m
}

// followEvery is how often a followed file is looked at: as often as
// the old app looked, so a log written to shows its lines at once.
const followEvery = 300 * time.Millisecond

// scrollbackFollowEvery is how often a followed scrollback is looked at.
const scrollbackFollowEvery = time.Second

// followTitle is what a reader's title says while it follows.
const followTitle = " (following)"

// followReader has reader id follow what it shows, or stop: its file,
// read again each time it changes, or its terminal's scrollback.
func (a *app) followReader(id string, on bool) {
	r, ok := a.st.Readers[id]
	if !ok {
		return
	}
	if on && !a.followable(id) {
		// Nothing to read again: typed history, or a scrollback whose
		// pane has gone. The reader is told it does not follow.
		r.Follow = false
		a.setReader(id, r)
		return
	}
	was := r.Follow
	r.Follow = on
	a.setReader(id, r)
	for i := range a.st.Panes {
		if p := &a.st.Panes[i]; p.ID == id && !p.Named {
			p.Title = strings.TrimSuffix(p.Title, followTitle)
			if on {
				p.Title += followTitle
			}
		}
	}
	if on && !was && r.Seq > 0 {
		// What it shows may be from long ago: read again now, and
		// followed from there.
		a.readOnce(id)
	}
	if on && !a.following[id] {
		a.following[id] = true
		go a.followLoop(id)
	}
}

// followable reports whether reader id has something to follow: a
// file, or the scrollback of a pane still open.
func (a *app) followable(id string) bool {
	if _, ok := a.reads[id]; ok {
		return true
	}
	r := a.st.Readers[id]
	return r.Of != "" && r.Gone == "" && a.has(r.Of)
}

// followStep is what a follow loop does next.
type followStep struct {
	stop bool
	// f and path are the file to look at, or scroll says the reader
	// follows a scrollback, which was brought up to date.
	f      vfs.FS
	path   string
	scroll bool
}

// nextFollow says what reader id's follow loop does next, on the
// program's goroutine. A loop told to stop has stopped.
func (a *app) nextFollow(id string) followStep {
	r, ok := a.st.Readers[id]
	if !a.has(id) || !ok || !r.Follow || !a.followable(id) {
		delete(a.following, id)
		return followStep{stop: true}
	}
	if r.Of != "" {
		if t := a.terminal(r.Of); t != nil {
			if lines := scrollbackText(t); !slices.Equal(lines, r.Lines) {
				r.Lines = lines
				r.Seq++
				a.setReader(id, r)
			}
		}
		return followStep{scroll: true}
	}
	var step followStep
	a.readerFiles(id, false, func(f vfs.FS) {
		step.f, step.path = f, a.reads[id].path
	})
	return step
}

// followLoop reads reader id again each time what it follows changes,
// looked at every followEvery, until it stops following or closes. A
// file that cannot be looked at says so in the pane, and is read again
// once it can be.
func (a *app) followLoop(id string) {
	var last vfs.Entry
	failed := false
	every := followEvery
	for looked := false; ; looked = true {
		// The first look at once, so a change straight after following
		// was turned on is not taken as how the file stands.
		if looked {
			select {
			case <-a.ctx.Done():
				return
			case <-time.After(every):
			}
		}
		step := make(chan followStep, 1)
		var s followStep
		select {
		case a.events <- func() { step <- a.nextFollow(id) }:
		case <-a.ctx.Done():
			return
		}
		select {
		case s = <-step:
		case <-a.ctx.Done():
			return
		}
		switch {
		case s.stop:
			return
		case s.scroll:
			// A scrollback is compared whole on the window's goroutine,
			// so it is looked at less often.
			every = scrollbackFollowEvery
			continue
		case s.f == nil:
			// Not reachable now, and the pane says so already.
			failed = true
			continue
		}
		e, err := s.f.Stat(s.path)
		if err != nil {
			failed = true
			a.events <- func() {
				if r, ok := a.st.Readers[id]; ok && r.Follow {
					a.readerSays(id, "Couldn't look at the file: "+err.Error())
				}
			}
			continue
		}
		first := last.Mod.IsZero() && !failed
		changed := e.Size != last.Size || !e.Mod.Equal(last.Mod)
		last = e
		if failed || (changed && !first) {
			failed = false
			a.events <- func() { a.readOnce(id) }
		}
	}
}

// saveLines writes what a reader shows to a file on this machine.
func (a *app) saveLines(in SaveLines) {
	at, err := conf.ExpandHome(in.Path)
	if err == nil {
		err = createNew(at, func(w io.Writer) error {
			_, err := io.WriteString(w, strings.Join(in.Lines, "\n")+"\n")
			return err
		})
	}
	taken := errors.Is(err, fs.ErrExist)
	if taken {
		// Why first: a narrow pane cuts the end off.
		err = fmt.Errorf("already there, give it another name: %s", at)
	}
	r, ok := a.st.Readers[in.Pane]
	if !ok {
		// The pane closed while its save was out; a notice says how
		// it went instead.
		if err != nil {
			a.failed("Couldn't save", err.Error())
		}
		return
	}
	r.Saves++
	r.SaveErr = ""
	if err != nil {
		r.SaveErr = err.Error()
	}
	if taken {
		// Offered next time, so saving again is saving somewhere new.
		r.SaveAs = freeName(in.Path)
	}
	a.setReader(in.Pane, r)
}

// createNew writes a file that is not there yet, readable by the user
// alone. A file already there is left alone, and the error is one
// errors.Is finds fs.ErrExist in: a name that comes filled in is one
// Enter from losing that file. The file is on the disk before it
// returns, and one that could not be written whole is taken away.
func createNew(at string, write func(io.Writer) error) error {
	f, err := os.OpenFile(at, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if err := write(f); err != nil {
		return errors.Join(err, f.Close(), os.Remove(at))
	}
	if err := f.Sync(); err != nil {
		return errors.Join(err, f.Close(), os.Remove(at))
	}
	if err := f.Close(); err != nil {
		return errors.Join(err, os.Remove(at))
	}
	return nil
}

// freeName is a name like typed, "~/notes.txt", that nothing has yet:
// "~/notes 2.txt", then 3 and on. It is typed back when none is found.
func freeName(typed string) string {
	ext := filepath.Ext(typed)
	stem := strings.TrimSuffix(typed, ext)
	for n := 2; n < 1000; n++ {
		try := fmt.Sprintf("%s %d%s", stem, n, ext)
		at, err := conf.ExpandHome(try)
		if err != nil {
			return typed
		}
		if _, err := os.Lstat(at); errors.Is(err, fs.ErrNotExist) {
			return try
		}
	}
	return typed
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
	// One viewer per pane: a second would show the same
	// text, and the first is where the user left it. Its find opens
	// again, as the command asked for a search.
	for id, r := range a.st.Readers {
		if r.Of == pane && slices.ContainsFunc(a.st.Panes, func(p Pane) bool { return p.ID == id }) {
			r.FindAgain++
			a.setReader(id, r)
			a.bringHere(id)
			return nil
		}
	}
	lines := scrollbackText(t)
	a.next++
	id := "p" + itoa(a.next)
	title := "Scrollback of " + a.titleOf(pane)
	a.addPane(a.paneOn(a.filesKey(pane), Pane{ID: id, Title: title, Kind: KindReader}), nil, Placement{Beside: pane})
	m := maps.Clone(a.st.Readers)
	if m == nil {
		m = map[string]Reader{}
	}
	// Saved on this machine, where the user is looking, under a name
	// every filesystem takes.
	m[id] = Reader{Path: title, Lines: lines, Seq: 1, Line: max(1, len(lines)), Find: true,
		Of: pane, SaveAs: filepath.Join("~", files.SafeName(a.titleOf(pane)+" scrollback")+".txt")}
	a.st.Readers = m
	return nil
}

// scrollbackText is a terminal's screen and what has scrolled off it,
// as lines of plain text.
func scrollbackText(t *uiterm.Terminal) []string {
	return strings.Split(strings.TrimRight(t.AllText(), "\n "), "\n")
}
