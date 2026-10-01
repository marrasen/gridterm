package app

import (
	"errors"
	"path/filepath"
	"slices"
	"strings"

	"github.com/marrasen/kakel/conf"
	"github.com/marrasen/kakel/machines"

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
func (a *app) openFileManager(m machines.ID, path string) error {
	if a.files == nil {
		return errors.New("the file manager can't open here")
	}
	if m != machines.Local {
		// Servers' files come to the file manager next; until then a
		// file pane shows them.
		return a.filesOn(m, path)
	}
	a.notePlaces()
	w, err := a.files.Open(filemanager.Options{
		FS: filemanager.LocalFS(), Dir: path, Name: "Files", PrefsPath: fileManagerPrefs(),
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
		places = append(places, filemanager.Place{Name: h.Name, Kind: "drive", Group: "Servers", Note: note, FS: serverFS + h.ID})
	}
	if old := a.serverPlaces.Load(); old != nil && slices.Equal(*old, places) {
		return
	}
	a.serverPlaces.Store(&places)
	if a.files != nil && len(a.fileWins) > 0 {
		a.files.Refresh()
	}
}

// visitPlace opens a place on another file system than the window's: a
// server's files. It runs on the window's goroutine.
func (a *app) visitPlace(_ *filemanager.Window, fs, path string) {
	id, ok := strings.CutPrefix(fs, serverFS)
	if !ok {
		return
	}
	a.events <- func() {
		if err := a.filesOn(machines.ID(id), path); err != nil {
			a.failed("Couldn't open the files", err.Error())
		}
	}
}

// closeFileManager closes every file manager window, as kakel ends.
func (a *app) closeFileManager() {
	for _, w := range a.fileWins {
		w.Close()
	}
}
