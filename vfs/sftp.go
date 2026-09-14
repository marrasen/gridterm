package vfs

import (
	"io"
	"io/fs"

	"github.com/pkg/sftp"
)

// SFTP is the filesystem of a machine on the far end of a connection.
//
// Paths are POSIX whatever this machine runs, because they are read over
// there. A Windows pane and one of these sit side by side and neither
// may assume the other's separator.
type SFTP struct {
	name   string
	client *sftp.Client

	// close is what lets go of the session, which is the connection's
	// business rather than this one's.
	close func() error
}

// NewSFTP wraps an SFTP session as a filesystem. name is what the panel
// calls the machine, and close ends the session.
func NewSFTP(name string, client *sftp.Client, close func() error) *SFTP {
	return &SFTP{name: name, client: client, close: close}
}

// Name is what the panel calls the machine.
func (s *SFTP) Name() string { return s.name }

// Sep is the separator between the parts of a path over there, which
// SFTP defines as a slash whatever the machine runs.
func (s *SFTP) Sep() byte { return '/' }

// Home is the directory the session starts in, which is the user's own.
func (s *SFTP) Home() (string, error) {
	home, err := s.client.Getwd()
	if err != nil {
		return "", wrap(s, "find the home directory on", s.name, err)
	}
	return home, nil
}

// ReadDir lists a directory.
//
// A name that cannot be read is the whole listing's failure, the same as
// on this machine: a listing with something quietly missing from it is
// worse than one that says it could not be read.
func (s *SFTP) ReadDir(path string) ([]Entry, error) {
	infos, err := s.client.ReadDir(path)
	if err != nil {
		return nil, wrap(s, "read the directory", path, err)
	}
	out := make([]Entry, 0, len(infos))
	for _, info := range infos {
		link := ""
		if info.Mode()&fs.ModeSymlink != 0 {
			// A link that cannot be read is shown as a link to nowhere
			// rather than failing the listing: the name is really there,
			// and where it points is the part that is broken.
			link, _ = s.client.ReadLink(Join(s, path, info.Name()))
		}
		out = append(out, entryOf(info.Name(), info, link))
	}
	return out, nil
}

// Stat reads one name, without following a symbolic link.
func (s *SFTP) Stat(path string) (Entry, error) {
	info, err := s.client.Lstat(path)
	if err != nil {
		return Entry{}, wrap(s, "read", path, err)
	}
	link := ""
	if info.Mode()&fs.ModeSymlink != 0 {
		link, _ = s.client.ReadLink(path)
	}
	return entryOf(Base(s, path), info, link), nil
}

// Open reads a file.
func (s *SFTP) Open(path string) (io.ReadCloser, error) {
	f, err := s.client.Open(path)
	if err != nil {
		return nil, wrap(s, "open", path, err)
	}
	return f, nil
}

// Create makes a file, replacing one that is there.
//
// The mode is set after it is made: SFTP's own create does not carry
// one, and a file left with whatever the far end's umask gave it is not
// the file that was asked for.
func (s *SFTP) Create(path string, mode fs.FileMode) (io.WriteCloser, error) {
	f, err := s.client.Create(path)
	if err != nil {
		return nil, wrap(s, "create", path, err)
	}
	if err := s.client.Chmod(path, mode.Perm()); err != nil {
		return nil, wrap(s, "set the permissions on", path, joinClose(err, f.Close()))
	}
	return f, nil
}

// Mkdir makes one directory.
func (s *SFTP) Mkdir(path string, mode fs.FileMode) error {
	if err := s.client.Mkdir(path); err != nil {
		return wrap(s, "make the directory", path, err)
	}
	// The same as Create: the request carries no mode, so it is set
	// afterwards rather than left to the far end's umask.
	return wrap(s, "set the permissions on", path, s.client.Chmod(path, mode.Perm()))
}

// Remove takes away one file or one empty directory.
func (s *SFTP) Remove(path string) error {
	return wrap(s, "remove", path, s.client.Remove(path))
}

// Rename moves a name to another.
func (s *SFTP) Rename(from, to string) error {
	if err := s.client.Rename(from, to); err != nil {
		return wrap(s, "rename", from+" to "+to, err)
	}
	return nil
}

// Chmod sets the permissions.
func (s *SFTP) Chmod(path string, mode fs.FileMode) error {
	return wrap(s, "set the permissions on", path, s.client.Chmod(path, mode.Perm()))
}

// Close ends the session.
func (s *SFTP) Close() error {
	if s.close == nil {
		return nil
	}
	return s.close()
}
