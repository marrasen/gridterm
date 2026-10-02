package app

import (
	"context"
	"errors"
	"fmt"
	"image"
	"io"
	"io/fs"
	"path"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/marrasen/kakel/internal/winattrs"
	"github.com/marrasen/kakel/vfs"

	"github.com/marrasen/gunim/filemanager"
)

// fmFS is a machine's files, as kakel reaches them, shown in the file
// manager: a server's over SFTP, or a machine's a window reaches. The
// file manager's own LocalFS shows this computer's.
//
// What SFTP lacks the file manager does without: there is no trash, so
// a delete asks and deletes for good, and free space shows only where
// the server answers statvfs.
//
// The files it reads are the machine's as long as they are open: once
// the connection ends they are gone, and every call fails, until the
// machine's files open again and the app hands them over with set.
type fmFS struct {
	id   string
	cur  atomic.Pointer[vfs.FS]
	name atomic.Pointer[string]
	// home is guarded by mu, as it goes with the files cur holds.
	mu   sync.Mutex
	home string
	// learned, when set, runs once the home folder is read, on the
	// goroutine that read it.
	learned func()
}

// newFMFS is f in the file manager, named id.
func newFMFS(id string, f vfs.FS) *fmFS {
	m := &fmFS{id: id}
	m.set(f)
	return m
}

// set hands over the machine's files, or nil once they are gone. Files
// opened again may be another account's, so the home folder is read
// again.
func (m *fmFS) set(f vfs.FS) {
	if f == nil {
		m.cur.Store(nil)
		return
	}
	name := f.Name()
	m.name.Store(&name)
	m.mu.Lock()
	defer m.mu.Unlock()
	if old := m.cur.Load(); old == nil || *old != f {
		m.home = ""
	}
	m.cur.Store(&f)
}

// live reports whether the machine's files are open.
func (m *fmFS) live() bool { return m.cur.Load() != nil }

// homeDir is the machine's home folder, once read, or "".
//
// The home folder is kept once read, as the window asks for it on its
// own goroutine, and a server may be slow to say.
func (m *fmFS) homeDir() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.home
}

// errNotConnected is what a call fails with once the connection ended.
type errNotConnected struct{ name string }

func (e errNotConnected) Error() string {
	return "the connection to " + e.name + " ended. Open its files again to connect again"
}

// files is the machine's files, or an error once they are gone.
func (m *fmFS) files() (vfs.FS, error) {
	if f := m.cur.Load(); f != nil {
		return *f, nil
	}
	name := m.id
	if n := m.name.Load(); n != nil {
		name = *n
	}
	return nil, errNotConnected{name}
}

// The extra calls kakel's SFTP file system has, which the file manager
// uses where it finds them.
type (
	follower interface {
		Follow(path string) (fs.FileInfo, error)
	}
	readlinker interface {
		Readlink(path string) (string, error)
		RealPath(path string) (string, error)
	}
	timer interface {
		Chtimes(path string, mod time.Time) error
	}
	spacer interface {
		Space(path string) (free, total uint64, err error)
	}
	newCreator interface {
		CreateNew(path string, mode fs.FileMode) (io.WriteCloser, error)
	}
)

// ID implements [filemanager.FS].
func (m *fmFS) ID() string { return m.id }

// Paths implements [filemanager.FS]: a server's paths are written with
// slashes, whatever the server runs, and a Windows machine's are shown
// as Windows writes them. A Windows machine is told by its home folder,
// which SFTP writes as /C:/Users/….
func (m *fmFS) Paths() filemanager.PathStyle {
	home := m.homeDir()
	if home == "" {
		home, _ = m.Home()
	}
	if onADrive(home) {
		return filemanager.DrivePaths
	}
	return filemanager.SlashPaths
}

// onADrive reports whether p, an SFTP path, is on a Windows drive:
// /C:, or under it.
func onADrive(p string) bool {
	return len(p) >= 3 && p[0] == '/' && p[2] == ':' && (len(p) == 3 || p[3] == '/') &&
		(p[1] >= 'A' && p[1] <= 'Z' || p[1] >= 'a' && p[1] <= 'z')
}

// SameFile implements [filemanager.SameFiler] for a Windows machine,
// whose names ignore case: two names alike but for case, of the same
// size, time and kind, are one item, as a rename that changes only the
// case is. SFTP says nothing that tells items apart for sure, so on any
// other machine two names are two items.
func (m *fmFS) SameFile(a, b fs.FileInfo) bool {
	return m.Paths() == filemanager.DrivePaths && strings.EqualFold(a.Name(), b.Name()) &&
		a.Size() == b.Size() && a.ModTime().Equal(b.ModTime()) && a.Mode() == b.Mode()
}

// TrueCase implements [filemanager.TrueCaser]: each name of p as the
// machine spells it, read from the folder it is in, for a path typed
// in another case on a machine whose names ignore case. A name not
// found stays as typed, and so does the rest after it.
func (m *fmFS) TrueCase(p string) (string, error) {
	f, err := m.files()
	if err != nil {
		return "", err
	}
	names := strings.Split(strings.Trim(p, "/"), "/")
	at := "/"
	for i, name := range names {
		if name == "" {
			continue
		}
		if i == 0 && onADrive("/"+name) {
			// A drive is written as Windows writes it, in capitals.
			at = "/" + strings.ToUpper(name)
			continue
		}
		entries, err := f.ReadDir(at)
		if err != nil {
			return "", err
		}
		found := ""
		for _, e := range entries {
			if e.Name == name {
				found = name
				break
			}
			if found == "" && strings.EqualFold(e.Name, name) {
				found = e.Name
			}
		}
		if found == "" {
			return path.Join(append([]string{at}, names[i:]...)...), nil
		}
		at = path.Join(at, found)
	}
	return at, nil
}

// Home implements [filemanager.FS].
func (m *fmFS) Home() (string, error) {
	f, err := m.files()
	if err != nil {
		return "", err
	}
	if home := m.homeDir(); home != "" {
		return home, nil
	}
	home, err := f.Home()
	if err != nil {
		return "", err
	}
	m.mu.Lock()
	cur := m.cur.Load()
	same := cur != nil && *cur == f
	if same {
		m.home = home
	}
	m.mu.Unlock()
	if same && m.learned != nil {
		m.learned()
	}
	return home, nil
}

// ReadDir implements [filemanager.FS]. kakel's file systems read a
// folder whole, so a ctx that ends lets the read finish unheard.
func (m *fmFS) ReadDir(ctx context.Context, dir string) ([]fs.DirEntry, error) {
	f, err := m.files()
	if err != nil {
		return nil, err
	}
	type read struct {
		entries []vfs.Entry
		err     error
	}
	got := make(chan read, 1)
	go func() {
		entries, err := f.ReadDir(dir)
		got <- read{entries, err}
	}()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case r := <-got:
		if r.err != nil {
			return nil, r.err
		}
		out := make([]fs.DirEntry, len(r.entries))
		for i, e := range r.entries {
			out[i] = fmEntry{e}
		}
		return out, nil
	}
}

// Stat implements [filemanager.FS], following a link at the end of path.
func (m *fmFS) Stat(path string) (fs.FileInfo, error) {
	f, err := m.files()
	if err != nil {
		return nil, err
	}
	if fl, ok := f.(follower); ok {
		return fl.Follow(path)
	}
	return m.Lstat(path)
}

// Lstat implements [filemanager.FS].
func (m *fmFS) Lstat(path string) (fs.FileInfo, error) {
	f, err := m.files()
	if err != nil {
		return nil, err
	}
	e, err := f.Stat(path)
	if err != nil {
		return nil, err
	}
	return fmInfo{e}, nil
}

// Open implements [filemanager.FS]. A server's file can seek; one that
// can't fails a seek, and is read from the start.
func (m *fmFS) Open(path string) (io.ReadSeekCloser, error) {
	f, err := m.files()
	if err != nil {
		return nil, err
	}
	r, err := f.Open(path)
	if err != nil {
		return nil, err
	}
	if rs, ok := r.(io.ReadSeekCloser); ok {
		return rs, nil
	}
	return noSeek{r}, nil
}

// Create implements [filemanager.FS]: a file new at path, readable only
// by the user until a Chmod says otherwise. One already there is
// refused, as the file manager asks before replacing.
func (m *fmFS) Create(path string) (io.WriteCloser, error) {
	f, err := m.files()
	if err != nil {
		return nil, err
	}
	if n, ok := f.(newCreator); ok {
		return n.CreateNew(path, 0o600)
	}
	if err := m.absent("create", f, path); err != nil {
		return nil, err
	}
	return f.Create(path, 0o600)
}

// Mkdir implements [filemanager.FS].
func (m *fmFS) Mkdir(path string, perm fs.FileMode) error {
	f, err := m.files()
	if err != nil {
		return err
	}
	if err := m.absent("mkdir", f, path); err != nil {
		return err
	}
	return f.Mkdir(path, perm)
}

// absent fails with fs.ErrExist when something is at path, and with
// what went wrong when that can't be told.
func (m *fmFS) absent(op string, f vfs.FS, path string) error {
	_, err := f.Stat(path)
	switch {
	case err == nil:
		return &fs.PathError{Op: op, Path: path, Err: fs.ErrExist}
	case errors.Is(err, fs.ErrNotExist):
		return nil
	}
	return err
}

// Rename implements [filemanager.FS], replacing a file at to.
//
// A server without OpenSSH's posix-rename refuses a name that is taken,
// so the file there is first set aside, and put back when the rename
// fails. A rename that fails with SFTP's plain failure while from is
// still there fails as one across volumes, which the window then does
// by copying: a server says no more of a rename from one mount to
// another, though the same failure may mean something else, and then
// the copy or the delete after it fails and says so.
func (m *fmFS) Rename(from, to string) error {
	f, err := m.files()
	if err != nil {
		return err
	}
	err = f.Rename(from, to)
	if err == nil {
		return nil
	}
	if _, serr := f.Stat(from); serr != nil {
		return err
	}
	if e, terr := f.Stat(to); terr == nil && !e.IsDir() {
		// Short, as a name near the longest a folder takes still fits.
		aside := path.Join(path.Dir(to), "~"+strconv.FormatInt(time.Now().UnixNano()%(1<<40), 36)+".old")
		if f.Rename(to, aside) != nil {
			return crossed(err)
		}
		if err = f.Rename(from, to); err != nil {
			if rerr := f.Rename(aside, to); rerr != nil {
				return fmt.Errorf("%w; what was at %s is now at %s: %w", err, to, aside, rerr)
			}
			return crossed(err)
		}
		if rerr := f.Remove(aside); rerr != nil {
			return fmt.Errorf("replaced %s, and what was there is left at %s: %w", to, aside, rerr)
		}
		return nil
	}
	return crossed(err)
}

// crossed is err from a rename, as one across volumes when SFTP's plain
// failure says nothing else.
func crossed(err error) error {
	if vfs.Failed(err) {
		return fmt.Errorf("%w: %w", filemanager.ErrCrossDevice, err)
	}
	return err
}

// Volume implements [filemanager.VolumeNamer]: a Windows machine's
// drive, /C: in /C:/Users, or / on a machine with one tree.
func (m *fmFS) Volume(path string) (string, error) {
	if len(path) >= 3 && path[0] == '/' && path[2] == ':' && (len(path) == 3 || path[3] == '/') {
		return strings.ToUpper(path[:3]), nil
	}
	return "/", nil
}

// Remove implements [filemanager.FS].
func (m *fmFS) Remove(path string) error {
	f, err := m.files()
	if err != nil {
		return err
	}
	return f.Remove(path)
}

// Chmod implements [filemanager.Stamper].
func (m *fmFS) Chmod(path string, mode fs.FileMode) error {
	f, err := m.files()
	if err != nil {
		return err
	}
	return f.Chmod(path, mode)
}

// Chtimes implements [filemanager.Stamper], where the file system can.
func (m *fmFS) Chtimes(path string, mod time.Time) error {
	f, err := m.files()
	if err != nil {
		return err
	}
	if t, ok := f.(timer); ok {
		return t.Chtimes(path, mod)
	}
	return nil
}

// Symlink implements [filemanager.Linker].
func (m *fmFS) Symlink(target, path string) error {
	f, err := m.files()
	if err != nil {
		return err
	}
	return f.Symlink(target, path)
}

// Readlink implements [filemanager.Linker].
func (m *fmFS) Readlink(path string) (string, error) {
	f, err := m.files()
	if err != nil {
		return "", err
	}
	if r, ok := f.(readlinker); ok {
		return r.Readlink(path)
	}
	e, err := f.Stat(path)
	if err != nil {
		return "", err
	}
	if e.Link == "" {
		return "", fmt.Errorf("%s is not a link", path)
	}
	return e.Link, nil
}

// EvalSymlinks implements [filemanager.Linker].
func (m *fmFS) EvalSymlinks(path string) (string, error) {
	f, err := m.files()
	if err != nil {
		return "", err
	}
	if r, ok := f.(readlinker); ok {
		return r.RealPath(path)
	}
	return path, nil
}

// Space implements [filemanager.SpaceReporter], where the server says.
func (m *fmFS) Space(path string) (free, total uint64, err error) {
	f, err := m.files()
	if err != nil {
		return 0, 0, err
	}
	if s, ok := f.(spacer); ok {
		return s.Space(path)
	}
	return 0, 0, errors.ErrUnsupported
}

// OnlineOnly implements [filemanager.OnlineReporter]: a file a kakel
// window on Windows says a cloud provider keeps online only. Such a file
// is read only when the user asks, as reading it downloads it.
func (m *fmFS) OnlineOnly(info fs.FileInfo) bool {
	if info.IsDir() {
		return false
	}
	attrs := uint32(0)
	if i, ok := info.(fmInfo); ok {
		attrs = i.e.Attrs
	} else {
		attrs = vfs.WinAttrs(info)
	}
	return winattrs.OnlineOnly(attrs)
}

// SystemThumb implements [filemanager.OnlineReporter]: the thumbnails
// Windows keeps don't come over SFTP.
func (m *fmFS) SystemThumb(string, int) (image.Image, error) { return nil, nil }

// fmEntry is an item of a folder, as the file manager reads it.
type fmEntry struct{ e vfs.Entry }

func (d fmEntry) Name() string               { return d.e.Name }
func (d fmEntry) IsDir() bool                { return d.e.IsDir() }
func (d fmEntry) Type() fs.FileMode          { return d.e.Mode.Type() }
func (d fmEntry) Info() (fs.FileInfo, error) { return fmInfo(d), nil }

// fmInfo is what the file manager reads of an item.
type fmInfo struct{ e vfs.Entry }

func (i fmInfo) Name() string       { return i.e.Name }
func (i fmInfo) Size() int64        { return i.e.Size }
func (i fmInfo) Mode() fs.FileMode  { return i.e.Mode }
func (i fmInfo) ModTime() time.Time { return i.e.Mod }
func (i fmInfo) IsDir() bool        { return i.e.IsDir() }
func (i fmInfo) Sys() any           { return nil }

// noSeek is a file read from the start only.
type noSeek struct{ io.ReadCloser }

func (noSeek) Seek(int64, int) (int64, error) {
	return 0, errors.New("this file can only be read from the start")
}
