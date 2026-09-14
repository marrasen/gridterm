// Package vfs is a filesystem a browser pane works on.
//
// One interface with two implementations: this machine, and a machine on
// the far end of an SSH connection. A pane holds one of them and does
// not know which it has, which is what lets a copy run between any two
// of them.
//
// Every failure is returned. Nothing here reports a partial listing, an
// empty directory where a read failed, or a file that is shorter than
// what was asked for: a browser that showed any of those would be
// telling the user something untrue about their own disk.
package vfs

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"strings"
	"time"
)

// Entry is one name in a directory.
type Entry struct {
	// Name is the name in its directory, with no path in front of it.
	Name string

	// Size is the length in bytes. It means nothing for a directory.
	Size int64

	// Mode is the kind and the permissions.
	Mode fs.FileMode

	// Mod is when it last changed.
	Mod time.Time

	// Link is what a symbolic link points at, and is empty for
	// everything else. What it points at is not followed: a browser
	// shows the link, and the user decides.
	Link string
}

// IsDir reports whether the entry is a directory.
func (e Entry) IsDir() bool { return e.Mode.IsDir() }

// IsLink reports whether the entry is a symbolic link.
func (e Entry) IsLink() bool { return e.Mode&fs.ModeSymlink != 0 }

// FS is a filesystem a pane can browse.
//
// An implementation must be safe to use from several goroutines: a
// background copy reads through the same FS the pane is listing with.
//
// Every method returns the failure it met. A ReadDir that could not read
// the whole directory returns no entries and the error, because a short
// listing shown as a whole one is worse than no listing at all.
type FS interface {
	// Name is what the panel calls this filesystem: "Local", or the
	// machine a connection reaches.
	Name() string

	// Sep is the separator between the parts of a path on it. A Windows
	// pane and a POSIX one sit side by side, so neither can assume.
	Sep() byte

	// Home is where a pane starts.
	Home() (string, error)

	// ReadDir lists a directory. The entries are in no particular order;
	// sorting is the pane's business.
	ReadDir(path string) ([]Entry, error)

	// Stat reads one name. It does not follow a symbolic link: a browser
	// shows the link.
	Stat(path string) (Entry, error)

	// Open reads a file.
	Open(path string) (io.ReadCloser, error)

	// Create makes a file, replacing one that is there.
	Create(path string, mode fs.FileMode) (io.WriteCloser, error)

	// Mkdir makes one directory. Its parent has to exist.
	Mkdir(path string, mode fs.FileMode) error

	// Remove takes away one file or one empty directory.
	Remove(path string) error

	// Rename moves a name to another, on the same filesystem.
	Rename(from, to string) error

	// Chmod sets the permissions.
	Chmod(path string, mode fs.FileMode) error

	// Close lets go of whatever the filesystem is holding. A local one
	// holds nothing; a remote one holds a channel on a connection.
	Close() error
}

// ErrNotSupported is returned by an operation a filesystem cannot do.
var ErrNotSupported = errors.New("vfs: that cannot be done on this filesystem")

// Join puts the parts of a path together with the filesystem's own
// separator.
//
// Nothing is cleaned away: ".." is a name the far end resolves, and
// resolving it here would mean guessing what the far end does with a
// symbolic link.
func Join(f FS, parts ...string) string {
	sep := string(f.Sep())
	var out string
	for _, part := range parts {
		part = strings.Trim(part, sep)
		switch {
		case part == "":
			continue
		case out == "":
			out = part
		default:
			out += sep + part
		}
	}
	// A POSIX path that started at the root keeps its leading separator.
	if len(parts) > 0 && strings.HasPrefix(parts[0], sep) {
		out = sep + out
	}
	return out
}

// Dir returns the directory a path is in, or the path itself when it is
// already the top of the filesystem.
func Dir(f FS, path string) string {
	sep := f.Sep()
	trimmed := strings.TrimRight(path, string(sep))
	if trimmed == "" {
		// The root itself, which is its own parent.
		return path
	}
	at := strings.LastIndexByte(trimmed, sep)
	switch {
	case at < 0:
		// A bare name, with nowhere above it that this knows about.
		return trimmed
	case at == 0:
		// Straight under a POSIX root.
		return string(sep)
	}
	return trimmed[:at]
}

// Base returns the last part of a path.
func Base(f FS, path string) string {
	sep := f.Sep()
	trimmed := strings.TrimRight(path, string(sep))
	if at := strings.LastIndexByte(trimmed, sep); at >= 0 {
		return trimmed[at+1:]
	}
	return trimmed
}

// IsTop reports whether a path has nothing above it: the root of a
// POSIX filesystem, or a drive on this one.
func IsTop(f FS, path string) bool {
	sep := string(f.Sep())
	return strings.TrimRight(Dir(f, path), sep) == strings.TrimRight(path, sep)
}

// entryOf builds an Entry from what a directory listing gives, which is
// the same shape on both filesystems.
func entryOf(name string, info fs.FileInfo, link string) Entry {
	return Entry{
		Name: name,
		Size: info.Size(),
		Mode: info.Mode(),
		Mod:  info.ModTime(),
		Link: link,
	}
}

// joinClose puts a failure together with a failure to tidy up after it.
// Both matter: the first says what went wrong, and the second says a
// file was left open on the far end.
func joinClose(err, closeErr error) error {
	if closeErr == nil {
		return err
	}
	return errors.Join(err, closeErr)
}

// wrap says which filesystem a failure happened on, so an error from a
// two-pane copy names the end that could not do it.
func wrap(f FS, what, path string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %s %s: %w", f.Name(), what, path, err)
}
