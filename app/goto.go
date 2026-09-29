package app

import (
	"github.com/marrasen/kakel/vfs"
)

// Go To completes a folder's name as it is typed: the rest of the name
// shows faintly after the caret, and Tab or Right takes it. Only folders
// are offered, and where several match, the part they share.

// ListFolders asks for the folders in Dir of a file pane's files, for
// Go To to complete from.
type ListFolders struct{ Pane, Dir string }

// Listed is the folders in a folder, for completing.
type Listed struct {
	Dir     string
	Folders []string
}

// listFolders reads the folders in a folder for a file pane, off the
// program's goroutine.
func (a *app) listFolders(in ListFolders) {
	f := a.filesOf(in.Pane)
	if f == nil {
		return
	}
	go func() {
		entries, err := f.ReadDir(in.Dir)
		a.events <- func() {
			if err != nil {
				// Nothing to complete from; the typing goes on.
				return
			}
			var folders []string
			for _, e := range entries {
				if e.IsDir() || e.IsLink() {
					folders = append(folders, e.Name)
				}
			}
			b := a.st.Browsers[in.Pane]
			b.Listed = Listed{Dir: in.Dir, Folders: folders}
			a.setBrowser(in.Pane, b)
		}
	}()
}

// sepOf is a filesystem's separator, as the window is told it.
func sepOf(f vfs.FS) string { return string(f.Sep()) }
