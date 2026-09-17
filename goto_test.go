package main

import (
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"

	"github.com/marrasen/gridterm/ui"
	"github.com/marrasen/gridterm/vfs"
)

// dirEntry is a directory a completion could land on.
func dirEntry(name string) vfs.Entry { return vfs.Entry{Name: name, Mode: fs.ModeDir} }

// fileEntry is a file, which a directory name never completes to.
func fileEntry(name string) vfs.Entry { return vfs.Entry{Name: name} }

// splitLeaf divides a typed path into the directory to read and the part
// of a name that has been typed.
func TestSplitLeafDividesATypedPath(t *testing.T) {
	for _, c := range []struct {
		text      string
		sep       byte
		dir, leaf string
		ok        bool
	}{
		{"/var/lo", '/', "/var", "lo", true},
		{"/va", '/', "/", "va", true},
		// A separator just typed: the directory itself, with no name
		// begun yet.
		{"/var/", '/', "/var/", "", true},
		{"/", '/', "/", "", true},
		{"var", '/', "", "", false},
		{`C:\Users\mar`, '\\', `C:\Users`, "mar", true},
	} {
		dir, leaf, ok := splitLeaf(fsWithSep{c.sep}, c.text)
		if ok != c.ok || dir != c.dir || leaf != c.leaf {
			t.Errorf("%q splits to %q %q %v, want %q %q %v",
				c.text, dir, leaf, ok, c.dir, c.leaf, c.ok)
		}
	}
}

// The rest offered is what every directory that matches agrees on, so a
// guess is never one of several.
func TestTheRestOfferedIsWhatTheyAllAgreeOn(t *testing.T) {
	entries := []vfs.Entry{
		dirEntry("log"), dirEntry("lock"), dirEntry("locker"), dirEntry("lib"),
		dirEntry("tmp"), fileEntry("locate"),
	}
	for _, c := range []struct{ leaf, want string }{
		// lock and locker agree on the k; locate is a file and log is
		// out of the running by then.
		{"loc", "k"},
		// log and lock agree on nothing at all after lo.
		{"lo", ""},
		// A name that already names a directory is left alone: the user
		// has a path that works, and End is pressed out of habit.
		{"lock", ""},
		{"li", "b"},
		{"t", "mp"},
		// Nothing matches, and a name with nothing left to add.
		{"z", ""},
		{"lib", ""},
	} {
		if got := restOf(fsWithSep{'/'}, entries, c.leaf); got != c.want {
			t.Errorf("after %q it offers %q, want %q", c.leaf, got, c.want)
		}
	}
}

// A file is never offered: Go to reads a directory.
func TestAFileIsNeverOffered(t *testing.T) {
	if got := restOf(fsWithSep{'/'}, []vfs.Entry{fileEntry("passwd")}, "pass"); got != "" {
		t.Errorf("it offers %q for a file", got)
	}
}

// Typing in the Go to dialog offers the rest of a directory's name.
func TestGoToOffersTheRestOfADirectory(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "workspace"), 0o700); err != nil {
		t.Fatalf("make a directory: %v", err)
	}
	p := aFilePaneAt(t, a, dir)

	a.focus(p)
	if err := a.openGoTo(); err != nil {
		t.Fatalf("go to: %v", err)
	}
	f := awaitModal(t, a, "the Go to dialog", byTitle[*ui.Form]("Go to"))
	where := f.Field("Path")
	retypeField(t, a, f, "Path", filepath.Join(dir, "works"))

	waitFor(t, a, "the rest of the name to be offered", func() bool {
		return where.Ghost != ""
	})
	if where.Ghost != "pace" {
		t.Errorf("it offers %q, want the rest of workspace", where.Ghost)
	}
	if !strings.HasSuffix(where.Text(), "works") {
		t.Errorf("the field says %q, and the rest is not part of it", where.Text())
	}
}

// An answer that lands after the text has moved on is dropped: it
// completes something the user is no longer typing.
func TestAnAnswerForTextThatHasMovedOnIsDropped(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	dir := t.TempDir()
	for _, name := range []string{"workspace", "attic"} {
		if err := os.Mkdir(filepath.Join(dir, name), 0o700); err != nil {
			t.Fatalf("make a directory: %v", err)
		}
	}
	held := &heldReads{let: make(chan struct{}), FS: vfs.NewLocal()}
	p := aFilePaneAt(t, a, dir)
	a.focus(p)

	f := a.newForm("Go to")
	where := f.AddField("Path", a.newField("", 0))
	a.completePath(where, held)

	// Typed once and left in flight, then typed on to something with a
	// completion of its own.
	where.SetText(filepath.Join(dir, "works"))
	waitFor(t, a, "the first read to be asked for", func() bool { return held.asked() > 0 })
	where.SetText(filepath.Join(dir, "att"))
	close(held.let)

	waitFor(t, a, "the second read to answer", func() bool { return where.Ghost != "" })
	if where.Ghost != "ic" {
		t.Errorf("it offers %q, want the rest of attic", where.Ghost)
	}
	// One read, not two: the second keystroke landed in the directory
	// the first was already reading.
	if got := held.asked(); got != 1 {
		t.Errorf("it read the directory %d times while one read was out", got)
	}
}

// heldReads is a filesystem whose first listing does not answer until a
// test lets it.
type heldReads struct {
	vfs.FS
	let  chan struct{}
	mu   sync.Mutex
	n    int
	once bool
}

func (h *heldReads) ReadDir(path string) ([]vfs.Entry, error) {
	h.mu.Lock()
	h.n++
	first := !h.once
	h.once = true
	h.mu.Unlock()
	if first {
		<-h.let
	}
	return h.FS.ReadDir(path)
}

// asked is how many listings have been started.
func (h *heldReads) asked() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.n
}

// A directory is read once however much is typed in it, because every
// read over a connection is a round trip.
func TestTypingOnInOneDirectoryReadsItOnce(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "workspace"), 0o700); err != nil {
		t.Fatalf("make a directory: %v", err)
	}
	counted := &heldReads{let: closed(), FS: vfs.NewLocal()}

	f := a.newForm("Go to")
	where := f.AddField("Path", a.newField("", 0))
	a.completePath(where, counted)

	for _, text := range []string{"w", "wo", "wor", "work"} {
		where.SetText(filepath.Join(dir, text))
		waitFor(t, a, "the rest of the name to be offered", func() bool {
			return where.Ghost != ""
		})
	}

	if got := counted.asked(); got != 1 {
		t.Errorf("it read the directory %d times, want once", got)
	}
}

// closed is a channel nothing waits on.
func closed() chan struct{} {
	c := make(chan struct{})
	close(c)
	return c
}

// fsWithSep is a filesystem that answers nothing but which separator it
// uses, for the path splitting that is all it is asked about.
type fsWithSep struct{ sep byte }

func (f fsWithSep) Sep() byte { return f.sep }

func (f fsWithSep) Name() string                        { return "test" }
func (f fsWithSep) Place() any                          { return f }
func (f fsWithSep) Roots() []string                     { return nil }
func (f fsWithSep) Home() (string, error)               { return "", nil }
func (f fsWithSep) ReadDir(string) ([]vfs.Entry, error) { return nil, nil }
func (f fsWithSep) Stat(string) (vfs.Entry, error)      { return vfs.Entry{}, nil }
func (f fsWithSep) Open(string) (io.ReadCloser, error)  { return nil, nil }
func (f fsWithSep) Symlink(string, string) error        { return nil }
func (f fsWithSep) Remove(string) error                 { return nil }
func (f fsWithSep) Rename(string, string) error         { return nil }
func (f fsWithSep) Chmod(string, fs.FileMode) error     { return nil }
func (f fsWithSep) Mkdir(string, fs.FileMode) error     { return nil }
func (f fsWithSep) Close() error                        { return nil }

func (f fsWithSep) Create(string, fs.FileMode) (io.WriteCloser, error) { return nil, nil }

// The rest offered is cut at a character boundary, so half of one is
// never drawn and never taken.
func TestTheRestOfferedIsCutAtACharacter(t *testing.T) {
	// Two names that differ inside the second character: the bytes they
	// share end in the middle of it.
	entries := []vfs.Entry{dirEntry("\u4e00\u6708"), dirEntry("\u4e01\u6708")}

	got := restOf(fsWithSep{'/'}, entries, "")

	if got != "" {
		t.Errorf("it offers %q, want nothing: the two share no whole character", got)
	}
	if !utf8.ValidString(got) {
		t.Errorf("it offers %q, which is not whole characters", got)
	}
}

// A name that differs only in case is offered on a machine whose
// filesystem does not count case, and not on one that does.
func TestCaseCountsWhereTheFilesystemCountsIt(t *testing.T) {
	entries := []vfs.Entry{dirEntry("Marcus")}

	if got := restOf(fsWithSep{'\\'}, entries, "marc"); got != "us" {
		t.Errorf("Windows offers %q for %q, want the rest of the name", got, "marc")
	}
	if got := restOf(fsWithSep{'/'}, entries, "marc"); got != "" {
		t.Errorf("a machine that counts case offers %q", got)
	}
}

// A link to a directory is offered: most of /bin, /lib and /sbin are
// links on a modern machine, and Go to is for reaching a directory
// however it is spelt.
func TestALinkIsOffered(t *testing.T) {
	entries := []vfs.Entry{{Name: "bin", Mode: fs.ModeSymlink}}

	if got := restOf(fsWithSep{'/'}, entries, "bi"); got != "n" {
		t.Errorf("it offers %q for a link to a directory", got)
	}
}
