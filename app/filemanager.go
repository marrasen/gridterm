package app

import (
	"errors"
	"path/filepath"
	"slices"
	"strings"

	"github.com/marrasen/kakel/conf"
	"github.com/marrasen/kakel/machines"
	"github.com/marrasen/kakel/vfs"

	"github.com/marrasen/gunim/filemanager"
)

// The file manager: windows of their own, in the style of gunim's Files,
// outside kakel's tabs. Its places are the machines, this computer's
// folders under This computer and each saved server under Servers. A
// file pane is the other way to work with files; where files open is
// the user's last choice between the two.

// FileWindows opens the file manager's windows and tells them their
// places changed: gunim's filemanager.Hub.
type FileWindows interface {
	Open(o filemanager.Options) (*filemanager.Window, error)
	Refresh()
}

// Intents for the file manager.
type (
	// OpenFilesOn opens the files on Machine, at Path or at home, where
	// the user last chose: a file manager window or a file pane.
	OpenFilesOn struct {
		Machine machines.ID
		Path    string
	}
	// OpenFileManager opens a file manager window on Machine, at Path or
	// at home, and keeps that as where files open.
	OpenFileManager struct {
		Machine machines.ID
		Path    string
	}
	// FilesInPane opens a file pane on Machine, at Path or at home, and
	// keeps that as where files open.
	FilesInPane struct {
		Machine machines.ID
		Path    string
	}
)

// serverFS is how a server is named as a file system in the file
// manager's places: this prefix and its ID.
const serverFS = "kakel:"

// gone marks a server place whose files the file manager had and lost,
// so a window showing them sees the place as elsewhere, and a click on
// it connects again.
const gone = "\x00gone"

// openFilesWhere opens files where the user last chose.
func (a *app) openFilesWhere(m machines.ID, path string) error {
	if a.files != nil && a.settings != nil && a.settings.FilesInWindow() {
		return a.openFileManager(m, path)
	}
	return a.filesOn(m, path)
}

// keepFilesIn keeps where files open, as the user just chose.
func (a *app) keepFilesIn(window bool) {
	if a.settings == nil || a.settings.FilesInWindow() == window {
		return
	}
	if err := a.settings.PutFilesInWindow(window); err != nil {
		a.failed("Couldn't keep where files open", err.Error())
	}
}

// openFileManager opens a file manager window on m, at path or at home.
// A server is connected to first, and its files opened.
func (a *app) openFileManager(m machines.ID, path string) error {
	if a.files == nil {
		return errors.New("the file manager can't open here")
	}
	if m == machines.Local {
		return a.openFileWindow(filemanager.LocalFS(), path)
	}
	return a.withFiles(m, func(f vfs.FS) {
		if err := a.openFileWindow(a.fmFor(m, f), path); err != nil {
			a.failed("Couldn't open the files on "+a.machines.Name(m), err.Error())
		}
	})
}

// openFileWindow opens a file manager window on fsys, at path or at home.
func (a *app) openFileWindow(fsys filemanager.FS, path string) error {
	a.notePlaces()
	w, err := a.files.Open(filemanager.Options{
		FS: fsys, Dir: path, Name: "Files", PrefsPath: fileManagerPrefs(),
		Places: a.fileManagerPlaces, Visit: a.visitPlace,
	})
	if err != nil {
		return err
	}
	a.fileWins = append(a.fileWins, w)
	go func() {
		<-w.Done()
		a.events <- func() { a.fileWins = slices.DeleteFunc(a.fileWins, func(o *filemanager.Window) bool { return o == w }) }
	}()
	return nil
}

// fmFor is m's files, open as f, as the file manager reads them: the
// same for every window, so one that lost them has them again.
func (a *app) fmFor(m machines.ID, f vfs.FS) *fmFS {
	fm := a.fmFiles[m]
	if fm == nil {
		if a.fmFiles == nil {
			a.fmFiles = map[machines.ID]*fmFS{}
		}
		fm = newFMFS(serverFS+string(m), f)
		// Its place goes to its home once that is known.
		fm.learned = func() { go func() { a.events <- a.notePlaces }() }
		a.fmFiles[m] = fm
	} else {
		fm.set(f)
	}
	if fm.homeDir() == "" {
		go func() { _, _ = fm.Home() }()
	}
	return fm
}

// fmBack hands the file manager m's files, opened again, so a window
// that lost them has them again.
func (a *app) fmBack(m machines.ID, f vfs.FS) {
	if fm := a.fmFiles[m]; fm != nil {
		fm.set(f)
	}
}

// fmGone tells the file manager m's files are gone, as its connection
// ended.
func (a *app) fmGone(m machines.ID) {
	if fm := a.fmFiles[m]; fm != nil {
		fm.set(nil)
	}
}

// fileManagerPrefs is where the file manager keeps its settings: beside
// kakel's own, or "" for its default when kakel's place can't be found.
func fileManagerPrefs() string {
	dir, err := conf.Dir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "files.json")
}

// fileManagerPlaces are the file manager's places: this computer's
// folders, then the servers, as last noted. It runs off the program's
// goroutine.
func (a *app) fileManagerPlaces() ([]filemanager.Place, error) {
	out, err := filemanager.LocalPlaces()
	for i := range out {
		out[i].Group = "This computer"
	}
	if servers := a.serverPlaces.Load(); servers != nil {
		out = append(out, *servers...)
	}
	return out, err
}

// notePlaces notes the servers as the file manager lists them, and has
// its windows read their places again when that changed: a server
// saved, renamed, connected or disconnected.
func (a *app) notePlaces() {
	var places []filemanager.Place
	for _, h := range a.st.Saved {
		if h.Window {
			continue
		}
		m := machines.ID(h.ID)
		note := "Not connected"
		switch {
		case slices.Contains(a.st.Dialing, m):
			note = "Connecting…"
		case slices.Contains(a.st.Connected, m):
			note = "Connected"
		}
		p := filemanager.Place{Name: h.Name, Kind: "drive", Group: "Servers", Note: note, FS: serverFS + h.ID}
		if fm := a.fmFiles[m]; fm != nil {
			if fm.live() {
				p.Path = fm.homeDir()
			} else {
				p.FS += gone
			}
		}
		places = append(places, p)
	}
	if old := a.serverPlaces.Load(); old != nil && slices.Equal(*old, places) {
		return
	}
	a.serverPlaces.Store(&places)
	if a.files != nil && len(a.fileWins) > 0 {
		a.files.Refresh()
	}
}

// visitPlace turns window w to a place on another file system than the
// one it shows: this computer's, or a server's, connected to first. It
// runs on a goroutine of its own.
func (a *app) visitPlace(w *filemanager.Window, fs, path string) {
	if fs == "" {
		w.Show(filemanager.LocalFS(), path)
		return
	}
	id, ok := strings.CutPrefix(fs, serverFS)
	if !ok {
		return
	}
	m := machines.ID(strings.TrimSuffix(id, gone))
	a.events <- func() {
		if err := a.withFiles(m, func(f vfs.FS) { w.Show(a.fmFor(m, f), path) }); err != nil {
			a.failed("Couldn't open the files on "+a.machines.Name(m), err.Error())
		}
	}
}

// closeFileManager closes every file manager window, as kakel ends.
func (a *app) closeFileManager() {
	for _, w := range a.fileWins {
		w.Close()
	}
}
