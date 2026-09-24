package vfs

import (
	"errors"
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

// localPlace is this machine. Every Local stands for it, so two panes
// here are one place although they are two values.
type localPlace struct{}

// Place is this machine.
func (l *Local) Place() any { return localPlace{} }

// Roots are the places a path here can start: one on a POSIX machine,
// and one per drive on Windows.
func (l *Local) Roots() []string { return localRoots() }

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
		at := filepath.Join(path, name.Name())
		info, err := name.Info()
		if errors.Is(err, fs.ErrNotExist) {
			// It went between the directory being read and this name
			// being asked about, which is ordinary in a directory
			// something else is writing to. It is not in the listing
			// because it is not there.
			continue
		}
		if err != nil {
			return nil, wrap(l, "read", at, err)
		}
		link := ""
		if info.Mode()&fs.ModeSymlink != 0 {
			// Where it points is part of what the listing says. A link
			// whose target cannot be read is not a link to nowhere: a
			// dangling one reads back perfectly well, so a failure here
			// means something else, and a blank shown as an answer would
			// be a partial listing passed off as a whole one.
			if link, err = os.Readlink(at); err != nil {
				if errors.Is(err, fs.ErrNotExist) {
					continue
				}
				return nil, wrap(l, "read the link", at, err)
			}
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
		if link, err = os.Readlink(path); err != nil {
			return Entry{}, wrap(l, "read the link", path, err)
		}
	}
	return entryOf(Base(l, path), info, link), nil
}

// Open reads a file.
//
// A directory is refused here rather than at the first read. A copy that
// opened one would already have emptied the file it was copying to
// before finding out.
func (l *Local) Open(path string) (io.ReadCloser, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, wrap(l, "open", path, err)
	}
	info, err := f.Stat()
	if err != nil {
		return nil, wrap(l, "read", path, errors.Join(err, f.Close()))
	}
	if info.IsDir() {
		return nil, wrap(l, "open", path,
			errors.Join(errIsDir, f.Close()))
	}
	return f, nil
}

// Append opens a file that is there, to write after what is in it.
func (l *Local) Append(path string) (io.WriteCloser, error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		return nil, wrap(l, "open", path, err)
	}
	return f, nil
}

// CreateNew makes a file that is not there, and fails when there is
// one.
func (l *Local) CreateNew(path string, mode fs.FileMode) (io.WriteCloser, error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode.Perm())
	if err != nil {
		return nil, wrap(l, "create", path, err)
	}
	// Set rather than left to the umask, the way Create sets it.
	if err := os.Chmod(path, mode.Perm()); err != nil {
		return nil, wrap(l, "set the permissions on", path,
			errors.Join(err, f.Close(), os.Remove(path)))
	}
	return f, nil
}

// Create makes a file, replacing one that is there.
//
// The mode is set rather than left to the umask, so a file made here and
// the same file made on a machine at the far end come out the same. One
// that is already there keeps the mode it has.
func (l *Local) Create(path string, mode fs.FileMode) (io.WriteCloser, error) {
	// Only "it is not there" makes the file new. Any other failure
	// leaves it unknown whether the file has a mode of its own to keep,
	// and guessing it has none overwrites the permissions on it.
	_, err := os.Lstat(path)
	there := err == nil
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, wrap(l, "read", path, err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode.Perm())
	if err != nil {
		return nil, wrap(l, "create", path, err)
	}
	if there {
		return f, nil
	}
	if err := os.Chmod(path, mode.Perm()); err != nil {
		// The file is half made: it has been emptied and nothing has
		// been written to it. Taking it away is better than leaving one
		// that nobody asked for with a mode nobody chose.
		return nil, wrap(l, "set the permissions on", path,
			errors.Join(err, f.Close(), os.Remove(path)))
	}
	return f, nil
}

// Mkdir makes one directory, with the mode it was asked for rather than
// whatever the umask allows.
func (l *Local) Mkdir(path string, mode fs.FileMode) error {
	if err := os.Mkdir(path, mode.Perm()); err != nil {
		return wrap(l, "make the directory", path, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return wrap(l, "read", path, errors.Join(err, os.Remove(path)))
	}
	if info.Mode().Perm() == mode.Perm() {
		// Already what was asked for, which is the usual case.
		return nil
	}
	if err := os.Chmod(path, mode.Perm()); err != nil {
		// Made but not as asked. It goes, so that trying again is not
		// refused for being there already.
		return wrap(l, "set the permissions on", path,
			errors.Join(err, os.Remove(path)))
	}
	return nil
}

// Symlink makes a symbolic link pointing at target.
func (l *Local) Symlink(target, path string) error {
	if err := os.Symlink(target, path); err != nil {
		return wrap(l, "link", path+" to "+target, err)
	}
	return nil
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
