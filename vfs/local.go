package vfs

import (
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// Local is the filesystem gridterm is running on.
//
// It holds nothing and costs nothing to make, so a pane that shows this
// machine makes its own rather than sharing one.
type Local struct{}

// NewLocal returns the filesystem of this machine.
func NewLocal() *Local { return &Local{} }

// Name is what the panel calls it.
func (l *Local) Name() string { return "Local" }

// Sep is the separator between the parts of a path here: a backslash on
// Windows, a slash everywhere else.
func (l *Local) Sep() byte { return filepath.Separator }

// Home is the user's own directory.
func (l *Local) Home() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", wrap(l, "find the home directory for", "this machine", err)
	}
	return home, nil
}

// ReadDir lists a directory.
//
// A name that cannot be read is the whole listing's failure. A browser
// showing a directory with a file quietly missing from it is worse than
// one that says it could not read the directory.
func (l *Local) ReadDir(path string) ([]Entry, error) {
	names, err := os.ReadDir(path)
	if err != nil {
		return nil, wrap(l, "read the directory", path, err)
	}
	out := make([]Entry, 0, len(names))
	for _, name := range names {
		info, err := name.Info()
		if err != nil {
			return nil, wrap(l, "read", filepath.Join(path, name.Name()), err)
		}
		link := ""
		if info.Mode()&fs.ModeSymlink != 0 {
			// A link that cannot be read is shown as a link to nowhere
			// rather than failing the listing: the name is really there,
			// and where it points is the part that is broken.
			link, _ = os.Readlink(filepath.Join(path, name.Name()))
		}
		out = append(out, entryOf(name.Name(), info, link))
	}
	return out, nil
}

// Stat reads one name, without following a symbolic link.
func (l *Local) Stat(path string) (Entry, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return Entry{}, wrap(l, "read", path, err)
	}
	link := ""
	if info.Mode()&fs.ModeSymlink != 0 {
		link, _ = os.Readlink(path)
	}
	return entryOf(filepath.Base(path), info, link), nil
}

// Open reads a file.
func (l *Local) Open(path string) (io.ReadCloser, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, wrap(l, "open", path, err)
	}
	return f, nil
}

// Create makes a file, replacing one that is there.
func (l *Local) Create(path string, mode fs.FileMode) (io.WriteCloser, error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode.Perm())
	if err != nil {
		return nil, wrap(l, "create", path, err)
	}
	return f, nil
}

// Mkdir makes one directory.
func (l *Local) Mkdir(path string, mode fs.FileMode) error {
	return wrap(l, "make the directory", path, os.Mkdir(path, mode.Perm()))
}

// Remove takes away one file or one empty directory.
func (l *Local) Remove(path string) error {
	return wrap(l, "remove", path, os.Remove(path))
}

// Rename moves a name to another.
func (l *Local) Rename(from, to string) error {
	if err := os.Rename(from, to); err != nil {
		return wrap(l, "rename", from+" to "+to, err)
	}
	return nil
}

// Chmod sets the permissions.
func (l *Local) Chmod(path string, mode fs.FileMode) error {
	return wrap(l, "set the permissions on", path, os.Chmod(path, mode.Perm()))
}

// Close does nothing: this filesystem holds nothing.
func (l *Local) Close() error { return nil }
