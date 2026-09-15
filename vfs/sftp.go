package vfs

import (
	"errors"
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

// Renamed says the machine is called something else now.
//
// The name is what the window matches a pane against to find which
// machine it is on, so one that kept the old name would be left behind
// when its connection closed.
func (s *SFTP) Renamed(name string) { s.name = name }

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
		at := Join(s, path, info.Name())
		link := ""
		if info.Mode()&fs.ModeSymlink != 0 {
			// Where it points is part of what the listing says. A link
			// whose target cannot be read is not a link to nowhere: a
			// dangling one reads back perfectly well, so a failure here
			// means something else -- often the session going away part
			// way through -- and a blank shown as an answer would be a
			// partial listing passed off as a whole one.
			var err error
			if link, err = s.client.ReadLink(at); err != nil {
				if errors.Is(err, fs.ErrNotExist) {
					// It went between the directory being read and this
					// name being asked about.
					continue
				}
				return nil, wrap(s, "read the link", at, err)
			}
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
		if link, err = s.client.ReadLink(path); err != nil {
			return Entry{}, wrap(s, "read the link", path, err)
		}
	}
	return entryOf(Base(s, path), info, link), nil
}

// Open reads a file.
//
// A directory is refused here rather than at the first read. A copy that
// opened one would already have emptied the file it was copying to
// before finding out.
func (s *SFTP) Open(path string) (io.ReadCloser, error) {
	f, err := s.client.Open(path)
	if err != nil {
		return nil, wrap(s, "open", path, err)
	}
	info, err := f.Stat()
	if err != nil {
		return nil, wrap(s, "read", path, errors.Join(err, f.Close()))
	}
	if info.IsDir() {
		return nil, wrap(s, "open", path, errors.Join(errIsDir, f.Close()))
	}
	return f, nil
}

// Create makes a file, replacing one that is there.
//
// The mode is set after it is made: SFTP's own create does not carry
// one, and a file left with whatever the far end's umask gave it is not
// the file that was asked for. One that is already there keeps the mode
// it has, which is what happens on this machine too.
func (s *SFTP) Create(path string, mode fs.FileMode) (io.WriteCloser, error) {
	_, known := s.client.Lstat(path)
	f, err := s.client.Create(path)
	if err != nil {
		return nil, wrap(s, "create", path, err)
	}
	if known == nil {
		return f, nil
	}
	if err := s.client.Chmod(path, mode.Perm()); err != nil {
		// The file is half made: it has been emptied and nothing has
		// been written to it. Taking it away is better than leaving one
		// that nobody asked for with a mode nobody chose.
		return nil, wrap(s, "set the permissions on", path,
			errors.Join(err, f.Close(), s.client.Remove(path)))
	}
	return f, nil
}

// Mkdir makes one directory, with the mode it was asked for rather than
// whatever the far end's umask allows.
func (s *SFTP) Mkdir(path string, mode fs.FileMode) error {
	if err := s.client.Mkdir(path); err != nil {
		// SFTP version 3 has no status for "it is already there": every
		// refusal arrives as the same failure. Asking says which it was,
		// so a caller can tell a name already taken from a directory it
		// may not write in.
		if _, there := s.client.Lstat(path); there == nil {
			err = errors.Join(err, fs.ErrExist)
		}
		return wrap(s, "make the directory", path, err)
	}
	// The request carries no mode, so what it got is read back and set
	// only when it is not what was asked for. Usually it is, and then
	// there is nothing to send and nothing that can fail.
	info, err := s.client.Stat(path)
	if err != nil {
		return wrap(s, "read", path, errors.Join(err, s.client.Remove(path)))
	}
	if info.Mode().Perm() == mode.Perm() {
		return nil
	}
	if err := s.client.Chmod(path, mode.Perm()); err != nil {
		// Made but not as asked. It goes, so that trying again is not
		// refused for being there already.
		return wrap(s, "set the permissions on", path,
			errors.Join(err, s.client.Remove(path)))
	}
	return nil
}

// Symlink makes a symbolic link pointing at target.
func (s *SFTP) Symlink(target, path string) error {
	if err := s.client.Symlink(target, path); err != nil {
		return wrap(s, "link", path+" to "+target, err)
	}
	return nil
}

// Remove takes away one file or one empty directory.
func (s *SFTP) Remove(path string) error {
	return wrap(s, "remove", path, s.client.Remove(path))
}

// Rename moves a name to another, replacing what is there.
//
// SFTP's own rename refuses a name that is already taken, while renaming
// on this machine replaces it. OpenSSH's posix-rename extension is what
// makes the two agree, so it is used wherever the far end has it.
func (s *SFTP) Rename(from, to string) error {
	var err error
	if _, ok := s.client.HasExtension("posix-rename@openssh.com"); ok {
		err = s.client.PosixRename(from, to)
	} else {
		// A machine without it cannot replace a name in one step. Doing
		// it in two -- remove, then rename -- would lose the file that
		// was there if the rename then failed, so the refusal stands and
		// the caller is told.
		err = s.client.Rename(from, to)
	}
	if err != nil {
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
