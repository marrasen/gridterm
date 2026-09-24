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
	"time"
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

	// was is the archive as it stood when it was read, to tell it has
	// changed since, and checked when that was last asked.
	was     Entry
	checked time.Time
}

// archiveRecheck is how long the archive held open is trusted before the
// file is asked again whether it has changed.
var archiveRecheck = time.Second

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
//
// A name is only an archive when there is a file by it. A directory
// with an archive's name is a directory, walked and written like any
// other, and a name nothing has yet is taken for an archive, so a path
// under it is not written into one by mistake.
func (a *archives) split(at string) (outer, inner string, in bool) {
	sep := string(a.Sep())
	parts := strings.Split(at, sep)
	for i, part := range parts {
		if !IsArchive(part) {
			continue
		}
		outer = strings.Join(parts[:i+1], sep)
		if !a.mayBeArchive(outer) {
			continue
		}
		return outer, strings.Trim(strings.Join(parts[i+1:], "/"), "/"), true
	}
	return at, "", false
}

// mayBeArchive reports whether a path with an archive's name is to be
// read as one: a plain file, or nothing yet, so a path under a name
// nothing has is not written into an archive by mistake.
//
// A directory is a directory whatever it is called. A link is what it
// points at: one to a directory is walked like the directory, and one
// to a jar is the jar. A name that cannot be asked about
// at all is not taken for an archive, so what goes wrong with it is said
// by the filesystem under this one rather than blamed on an archive.
// The archive held open is known to be one without asking.
func (a *archives) mayBeArchive(at string) bool {
	a.mu.Lock()
	known := a.at == at && a.held != nil && time.Since(a.checked) < archiveRecheck
	a.mu.Unlock()
	if known {
		return true
	}
	e, err := a.FS.Stat(at)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return true
	case err != nil:
		return false
	}
	if e.IsLink() {
		// Only the name itself being free makes it one to be. A link
		// that leads nowhere is a link, and it is left to say so.
		if e, err = a.follow(at, e); err != nil {
			return false
		}
	}
	return !e.IsDir()
}

// mostLinkHops is how many links are followed to find what a name is,
// before a loop of them is given up on.
const mostLinkHops = 8

// followed is what a path is once any links it names are followed: a
// link to a jar is the jar, and one to a directory the directory. The
// filesystems under this one answer about the link itself.
func (a *archives) followed(at string) (Entry, error) {
	e, err := a.FS.Stat(at)
	if err != nil {
		return e, err
	}
	return a.follow(at, e)
}

// follow is followed for a name already asked about once.
func (a *archives) follow(at string, e Entry) (Entry, error) {
	for range mostLinkHops {
		if !e.IsLink() {
			return e, nil
		}
		target := e.Link
		if !isAbsOn(a, target) {
			target = Join(a, Dir(a, at), target)
		}
		at = target
		var err error
		if e, err = a.FS.Stat(at); err != nil {
			return e, err
		}
	}
	return Entry{}, fmt.Errorf("%s: more than %d links, one after another", at, mostLinkHops)
}

// isAbsOn reports whether a path starts at the top of a filesystem: at
// its separator, or at a drive and the top of it -- "C:\x" or "C:/x".
// The drive is taken whatever the separator: a Windows machine served
// over SFTP says where a link points in its own spelling. "a:b.jar" is
// a name.
func isAbsOn(f FS, p string) bool {
	if strings.HasPrefix(p, string(f.Sep())) {
		return true
	}
	return len(p) >= 3 && isDrive(p[:2]) && (p[2] == '\\' || p[2] == '/')
}

// letGo drops the archive held open when a write is about to change the
// file at a path, so what is read next is what is there then rather
// than what was there a moment ago.
func (a *archives) letGo(at string) {
	a.mu.Lock()
	if a.at == at {
		a.at, a.held, a.bytes = "", nil, nil
	}
	a.mu.Unlock()
}

// open reads an archive and keeps it, or answers the one it is already
// holding.
func (a *archives) open(at string) (*zip.Reader, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	// The one held is answered only while the file is still the one it
	// was read from. Anything can change it: a copy from another pane, a
	// move, a program outside gridterm. A zip read before and shown after
	// would list what is no longer there. Asked at most once a
	// archiveRecheck, because a copy out of a zip on a machine far away
	// opens it once for every file, and each question is a round trip.
	if a.at == at && a.held != nil && time.Since(a.checked) < archiveRecheck {
		return a.held, nil
	}
	// What the name leads to, so a jar reached through a link is sized
	// and watched for changes as the jar rather than as the link.
	e, err := a.followed(at)
	if err != nil {
		return nil, err
	}
	if a.at == at && a.held != nil && e.Size == a.was.Size && e.Mod.Equal(a.was.Mod) {
		a.checked = time.Now()
		return a.held, nil
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
	a.at, a.held, a.bytes, a.was, a.checked = at, got, raw, e, time.Now()
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
		if !e.IsDir() {
			// A file, shown as the directory at its top. A real
			// directory with an archive's name is left as it is.
			e.Mode |= fs.ModeDir
			e.Archive = true
		}
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
			out[i].Archive = true
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
// is a different thing from reading it. The archive itself is a file
// like any other, and a name with an archive's ending that is not a
// file yet can be made into anything: a link to a jar, a directory.

func (a *archives) Create(at string, mode fs.FileMode) (io.WriteCloser, error) {
	if _, inner, in := a.split(at); in && inner != "" {
		return nil, inArchive("write", at)
	}
	a.letGo(at)
	return a.FS.Create(at, mode)
}

func (a *archives) Mkdir(at string, mode fs.FileMode) error {
	if _, inner, in := a.split(at); in && inner != "" {
		return inArchive("make a directory in", at)
	}
	a.letGo(at)
	return a.FS.Mkdir(at, mode)
}

func (a *archives) Symlink(target, at string) error {
	if _, inner, in := a.split(at); in && inner != "" {
		return inArchive("make a link in", at)
	}
	a.letGo(at)
	return a.FS.Symlink(target, at)
}

func (a *archives) Remove(at string) error {
	if _, inner, in := a.split(at); in && inner != "" {
		return inArchive("remove something from", at)
	}
	a.letGo(at)
	return a.FS.Remove(at)
}

func (a *archives) Rename(from, to string) error {
	for _, at := range []string{from, to} {
		if _, inner, in := a.split(at); in && inner != "" {
			return inArchive("move something in", at)
		}
	}
	a.letGo(from)
	a.letGo(to)
	return a.FS.Rename(from, to)
}

func (a *archives) Chmod(at string, mode fs.FileMode) error {
	if _, inner, in := a.split(at); in && inner != "" {
		return inArchive("change the permissions of something in", at)
	}
	a.letGo(at)
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
