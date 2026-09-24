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

	// Place is which machine the filesystem reads, as a value equal for
	// two filesystems on one machine and for nothing else. It is what
	// Same asks, and it has to be comparable.
	//
	// It is not the name. A name is a label the window can change, and
	// two machines can end up called the same thing.
	Place() any

	// Roots are the places a path on it can start: one on a POSIX
	// machine, one per drive on Windows. A pane at the top of the
	// filesystem has these to go to and nowhere else.
	//
	// What the machine says it has, not what answers: a drive is listed
	// whether or not there is a disk in it, because "there is a drive
	// here and it will not answer" is something the user is entitled to
	// find out by going to it. An empty list means the machine would not
	// say, and a pane then has only where it already is.
	//
	// It is asked on the goroutine that draws, so it does not go to the
	// disk.
	Roots() []string

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
	//
	// The mode is what a new file gets, whatever the machine's umask
	// would have said. A file that is already there keeps its own: a
	// copy over an existing file changes what is in it, not who may
	// read it.
	Create(path string, mode fs.FileMode) (io.WriteCloser, error)

	// Mkdir makes one directory. Its parent has to exist, and the mode
	// is what it gets whatever the machine's umask would have said.
	Mkdir(path string, mode fs.FileMode) error

	// Symlink makes a symbolic link at path pointing at target. The
	// target is not checked and need not exist: that is what a link is.
	Symlink(target, path string) error

	// Remove takes away one file or one empty directory.
	Remove(path string) error

	// Rename moves a name to another, on the same filesystem.
	Rename(from, to string) error

	// Chmod sets the permissions. Only the nine permission bits are
	// used: setuid, setgid and the sticky bit are left alone, because a
	// copy between two machines must not quietly hand out rights on the
	// far one.
	Chmod(path string, mode fs.FileMode) error

	// Close lets go of whatever the filesystem is holding. A local one
	// holds nothing; a remote one holds a channel on a connection.
	Close() error
}

// Appender is a filesystem that can add to a file without ever
// emptying it: at the end of one that is there, or by making one that
// is not.
//
// Not part of FS: a pane never needs it. It is for a file whose old
// contents must survive a write that fails half way, or a second writer
// that got there first, which a file replaced whole does not promise.
type Appender interface {
	// Append opens a file that is there, to write after what is in it.
	Append(path string) (io.WriteCloser, error)

	// CreateNew makes a file that is not there, with the mode given,
	// and fails when there is one.
	CreateNew(path string, mode fs.FileMode) (io.WriteCloser, error)
}

// errIsDir says an operation was given a directory where it needed a
// file. It is not exported: a caller tells one from the other with Stat,
// and this is only what the failure says.
var errIsDir = errors.New("it is a directory")

// Same reports whether two filesystems are the same place, so a move
// between them can be a rename rather than a copy and a delete.
//
// Two sessions to one machine are the same place even though they are
// two connections, and two values standing for this machine are the same
// place even though they are two values. Comparing the values themselves
// answers neither, and comparing the names answers wrongly: a name is
// only what the window calls a machine, so a move that read two machines
// as one because they were called the same thing would put the file on
// the machine it came from.
func Same(a, b FS) bool {
	if a == nil || b == nil {
		return false
	}
	return a.Place() == b.Place()
}

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
		if part == "" {
			continue
		}
		if out == "" {
			// The first part that says anything decides where the path
			// starts. Its leading separators are kept, so a POSIX root
			// and a Windows share both survive.
			out = trimTail(part, sep)
			continue
		}
		next := strings.Trim(part, sep)
		if next == "" {
			continue
		}
		if strings.HasSuffix(out, sep) {
			out += next
		} else {
			out += sep + next
		}
	}
	return out
}

// trimTail takes the separators off the end of a path, unless that would
// leave nothing at all -- the root -- or a bare drive letter, which on
// Windows names the current directory on that drive rather than its top.
func trimTail(path, sep string) string {
	trimmed := strings.TrimRight(path, sep)
	if trimmed == "" || isDrive(trimmed) {
		return path
	}
	return trimmed
}

// isDrive reports whether a path is a bare Windows drive letter.
func isDrive(path string) bool {
	if len(path) != 2 || path[1] != ':' {
		return false
	}
	c := path[0]
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

// Dir returns the directory a path is in, or the path itself when it is
// already the top of the filesystem.
func Dir(f FS, path string) string {
	sep := f.Sep()
	trimmed := strings.TrimRight(path, string(sep))
	if trimmed == "" || isDrive(trimmed) {
		// The root, or the top of a drive: its own parent.
		return path
	}
	at := strings.LastIndexByte(trimmed, sep)
	if at < 0 {
		// A bare name, with nowhere above it that this knows about.
		return trimmed
	}
	head := strings.TrimRight(trimmed[:at], string(sep))
	switch {
	case head == "":
		// Straight under a POSIX root.
		return string(sep)
	case isDrive(head):
		// Straight under a drive. The separator stays: "C:" on its own
		// means the current directory on that drive.
		return head + string(sep)
	}
	return head
}

// Base returns the last part of a path. The root is its own name,
// because a pane sitting there has to have something to show.
func Base(f FS, path string) string {
	sep := f.Sep()
	trimmed := strings.TrimRight(path, string(sep))
	if trimmed == "" {
		return path
	}
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

// wrap says which filesystem a failure happened on, so an error from a
// two-pane copy names the end that could not do it.
func wrap(f FS, what, path string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %s %s: %w", f.Name(), what, path, err)
}
