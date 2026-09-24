package vfs

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"strings"
	"sync"
)

// MostArchiveBytes is the largest archive this will open.
//
// The whole of it is held while it is being read: a zip is read from
// its end and the filesystem interface hands out a stream, so there is
// nothing to seek in. A larger one is refused with a message rather
// than pulled across a connection a megabyte at a time.
const MostArchiveBytes = 64 << 20

// archiveExts are the names that open as a directory. Only zip: tar is
// a stream with no index, so listing one means reading all of it, and
// everything else needs a library.
var archiveExts = []string{".zip", ".jar", ".whl", ".xpi", ".crx", ".vsix"}

// ErrInArchive is why a write inside an archive fails. Reading one is
// answering questions about a file that is already there; writing one
// means building the container again.
var ErrInArchive = errors.New("an archive is read only here")

// archives is a filesystem with the archives on it opened as
// directories.
//
// A path inside one is answered the way any other path is, so nothing
// above has to know: a pane walks into an archive and back out of it
// with the keys it already has, and the viewer opens a file inside one
// because it opens a file through a filesystem.
//
// It holds the last archive it read, because a pane in one asks about
// it over and over: once for the listing, once a row for the details,
// and again for every file opened out of it.
type archives struct {
	FS

	mu sync.Mutex

	// at is the archive held open, and held what was read from it. The
	// bytes are kept as well, because a zip.Reader reads from them.
	at    string
	held  *zip.Reader
	bytes []byte
}

// WithArchives returns the filesystem with its archives opened as
// directories.
//
// The wrapper is per pane rather than shared: two panes in two
// archives would otherwise take turns throwing each other's out.
func WithArchives(under FS) FS {
	if under == nil {
		return nil
	}
	return &archives{FS: under}
}

// IsArchive reports whether a name opens as a directory.
func IsArchive(name string) bool {
	lower := strings.ToLower(name)
	for _, ext := range archiveExts {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}

// split cuts a path at the archive in it: what is on the real
// filesystem, and what is inside the archive.
//
// The inner path is in the archive's own spelling, which is slashes
// whatever the machine outside uses. It is empty for the archive
// itself, which is the directory at its top.
func (a *archives) split(at string) (outer, inner string, in bool) {
	sep := string(a.Sep())
	parts := strings.Split(at, sep)
	for i, part := range parts {
		if !IsArchive(part) {
			continue
		}
		return strings.Join(parts[:i+1], sep),
			strings.Trim(strings.Join(parts[i+1:], "/"), "/"), true
	}
	return at, "", false
}

// open reads an archive and keeps it, or answers the one it is already
// holding.
func (a *archives) open(at string) (*zip.Reader, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.at == at && a.held != nil {
		return a.held, nil
	}
	e, err := a.FS.Stat(at)
	if err != nil {
		return nil, err
	}
	if e.Size > MostArchiveBytes {
		return nil, fmt.Errorf("%s is %d bytes, and the most an archive may be to open it here is %d",
			at, e.Size, MostArchiveBytes)
	}
	f, err := a.FS.Open(at)
	if err != nil {
		return nil, err
	}
	raw, err := io.ReadAll(io.LimitReader(f, MostArchiveBytes+1))
	if err != nil {
		return nil, errors.Join(fmt.Errorf("read the archive %s: %w", at, err), f.Close())
	}
	if err := f.Close(); err != nil {
		return nil, fmt.Errorf("read the archive %s: %w", at, err)
	}
	if int64(len(raw)) > MostArchiveBytes {
		return nil, fmt.Errorf("%s grew past %d bytes while it was being read", at, MostArchiveBytes)
	}
	got, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return nil, fmt.Errorf("read the archive %s: %w", at, err)
	}
	a.at, a.held, a.bytes = at, got, raw
	return got, nil
}

// Stat answers about an archive the way it answers about a directory,
// and about what is inside one from the archive itself.
func (a *archives) Stat(at string) (Entry, error) {
	outer, inner, in := a.split(at)
	if !in {
		return a.FS.Stat(at)
	}
	if inner == "" {
		// The archive itself, which is the directory at its top.
		e, err := a.FS.Stat(outer)
		if err != nil {
			return Entry{}, err
		}
		e.Mode |= fs.ModeDir
		return e, nil
	}
	got, err := a.open(outer)
	if err != nil {
		return Entry{}, err
	}
	return statIn(got, at, inner)
}

// statIn finds one name inside an archive.
func statIn(got *zip.Reader, at, inner string) (Entry, error) {
	for _, f := range got.File {
		name := strings.Trim(f.Name, "/")
		switch {
		case name == inner:
			return archiveEntry(path.Base(name), f), nil
		case strings.HasPrefix(name, inner+"/"):
			// A directory nothing in the archive names on its own,
			// which is how most of them are written.
			return Entry{Name: path.Base(inner), Mode: fs.ModeDir | 0o555}, nil
		}
	}
	return Entry{}, fmt.Errorf("%s: there is nothing by that name in the archive", at)
}

// archiveEntry is one thing in an archive, as a listing shows it.
func archiveEntry(name string, f *zip.File) Entry {
	mode := f.Mode()
	if strings.HasSuffix(f.Name, "/") {
		mode |= fs.ModeDir
	}
	return Entry{
		Name: name,
		Size: int64(f.UncompressedSize64),
		Mode: mode,
		Mod:  f.Modified,
	}
}

// ReadDir lists what is in a directory, inside an archive or out.
func (a *archives) ReadDir(at string) ([]Entry, error) {
	outer, inner, in := a.split(at)
	if !in {
		return a.markArchives(at)
	}
	got, err := a.open(outer)
	if err != nil {
		return nil, err
	}
	return listIn(got, inner), nil
}

// markArchives is an ordinary listing with the archives in it marked as
// directories, so a pane offers to walk into one.
func (a *archives) markArchives(at string) ([]Entry, error) {
	out, err := a.FS.ReadDir(at)
	if err != nil {
		return nil, err
	}
	for i, e := range out {
		if !e.IsDir() && e.Link == "" && IsArchive(e.Name) {
			out[i].Mode |= fs.ModeDir
		}
	}
	return out, nil
}

// listIn is what one directory inside an archive holds.
//
// A zip is a flat list of names, and most of them do not write the
// directories down. So the names are cut at the next separator past
// the directory being listed, and what that gives is the listing.
func listIn(got *zip.Reader, inner string) []Entry {
	seen := map[string]bool{}
	var out []Entry
	for _, f := range got.File {
		name := strings.Trim(f.Name, "/")
		rest, under := below(name, inner)
		if !under || rest == "" {
			continue
		}
		if dir, _, deeper := strings.Cut(rest, "/"); deeper {
			// Something further down: what this listing shows is the
			// directory it is in.
			if !seen[dir] {
				seen[dir] = true
				out = append(out, Entry{Name: dir, Mode: fs.ModeDir | 0o555, Mod: f.Modified})
			}
			continue
		}
		if seen[rest] {
			continue
		}
		seen[rest] = true
		out = append(out, archiveEntry(rest, f))
	}
	return out
}

// below is what is left of a name under a directory, and whether it is
// under it at all.
func below(name, inner string) (string, bool) {
	if inner == "" {
		return name, true
	}
	rest, cut := strings.CutPrefix(name, inner+"/")
	return rest, cut
}

// Open reads a file, inside an archive or out.
func (a *archives) Open(at string) (io.ReadCloser, error) {
	outer, inner, in := a.split(at)
	if !in || inner == "" {
		// Outside an archive, or the archive itself: the bytes of a
		// zip are what a copy of it wants.
		return a.FS.Open(at)
	}
	got, err := a.open(outer)
	if err != nil {
		return nil, err
	}
	for _, f := range got.File {
		if strings.Trim(f.Name, "/") == inner {
			return f.Open()
		}
	}
	return nil, fmt.Errorf("%s: there is nothing by that name in the archive", at)
}

// Roots are the machine's own. An archive is somewhere on it rather
// than a place a path starts.
func (a *archives) Roots() []string { return a.FS.Roots() }

// Close lets go of the archive being held as well as the filesystem
// under it.
func (a *archives) Close() error {
	a.mu.Lock()
	a.at, a.held, a.bytes = "", nil, nil
	a.mu.Unlock()
	return a.FS.Close()
}

// Everything that writes is refused inside an archive and handed on
// outside one. Writing one means building the container again, which
// is a different thing from reading it.

func (a *archives) Create(at string, mode fs.FileMode) (io.WriteCloser, error) {
	if _, _, in := a.split(at); in {
		return nil, inArchive("write", at)
	}
	return a.FS.Create(at, mode)
}

// Append adds to a file outside any archive, on a filesystem that can.
func (a *archives) Append(at string) (io.WriteCloser, error) {
	if _, _, in := a.split(at); in {
		return nil, inArchive("write", at)
	}
	add, ok := a.FS.(Appender)
	if !ok {
		return nil, fmt.Errorf("%s cannot add to a file in place", a.Name())
	}
	return add.Append(at)
}

// CreateNew makes a file outside any archive, on a filesystem that can.
func (a *archives) CreateNew(at string, mode fs.FileMode) (io.WriteCloser, error) {
	if _, _, in := a.split(at); in {
		return nil, inArchive("write", at)
	}
	add, ok := a.FS.(Appender)
	if !ok {
		return nil, fmt.Errorf("%s cannot add to a file in place", a.Name())
	}
	return add.CreateNew(at, mode)
}

func (a *archives) Mkdir(at string, mode fs.FileMode) error {
	if _, _, in := a.split(at); in {
		return inArchive("make a directory in", at)
	}
	return a.FS.Mkdir(at, mode)
}

func (a *archives) Symlink(target, at string) error {
	if _, _, in := a.split(at); in {
		return inArchive("make a link in", at)
	}
	return a.FS.Symlink(target, at)
}

func (a *archives) Remove(at string) error {
	if _, inner, in := a.split(at); in && inner != "" {
		return inArchive("remove something from", at)
	}
	return a.FS.Remove(at)
}

func (a *archives) Rename(from, to string) error {
	for _, at := range []string{from, to} {
		if _, inner, in := a.split(at); in && inner != "" {
			return inArchive("move something in", at)
		}
	}
	return a.FS.Rename(from, to)
}

func (a *archives) Chmod(at string, mode fs.FileMode) error {
	if _, inner, in := a.split(at); in && inner != "" {
		return inArchive("change the permissions of something in", at)
	}
	return a.FS.Chmod(at, mode)
}

// inArchive says what could not be done and why.
func inArchive(what, at string) error {
	return fmt.Errorf("could not %s %s: %w", what, at, ErrInArchive)
}

// Renamed passes a new name down to the filesystem under this one.
//
// Renamed is not on FS: it is a thing a filesystem reading a machine
// may be told, and the window asks for it by type. A wrapper has to
// carry the methods that are not on the interface as well, or a pane
// inside an archive would keep the name its machine had when the pane
// opened.
func (a *archives) Renamed(now string) {
	if under, ok := a.FS.(interface{ Renamed(string) }); ok {
		under.Renamed(now)
	}
}
