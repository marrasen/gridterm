package vfs

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// aZip writes an archive holding these names, with the name as its own
// contents so a read can be checked.
func aZip(t *testing.T, at string, names ...string) {
	t.Helper()
	var b bytes.Buffer
	w := zip.NewWriter(&b)
	for _, name := range names {
		f, err := w.Create(name)
		if err != nil {
			t.Fatalf("put %s in the archive: %v", name, err)
		}
		if strings.HasSuffix(name, "/") {
			continue
		}
		if _, err := io.WriteString(f, "in "+name); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close the archive: %v", err)
	}
	if err := os.WriteFile(at, b.Bytes(), 0o600); err != nil {
		t.Fatalf("write the archive: %v", err)
	}
}

// withAZip is a filesystem holding one archive, and where it is.
func withAZip(t *testing.T, names ...string) (FS, string) {
	t.Helper()
	dir := t.TempDir()
	at := filepath.Join(dir, "bundle.zip")
	aZip(t, at, names...)
	f := WithArchives(NewLocal())
	t.Cleanup(func() { _ = f.Close() })
	return f, at
}

// named is the names of a listing, sorted, so a test reads what is
// there rather than what order it came in.
func named(got []Entry) []string {
	out := make([]string, 0, len(got))
	for _, e := range got {
		out = append(out, e.Name)
	}
	slices.Sort(out)
	return out
}

// An archive is listed as a directory, and its top holds what is at
// the top of it.
func TestAnArchiveIsListedLikeADirectory(t *testing.T) {
	f, at := withAZip(t, "readme.md", "src/main.go", "src/deep/x.txt")

	got, err := f.ReadDir(at)
	if err != nil {
		t.Fatalf("list the archive: %v", err)
	}

	if want := []string{"readme.md", "src"}; !slices.Equal(named(got), want) {
		t.Errorf("the top of the archive holds %v, want %v", named(got), want)
	}
	for _, e := range got {
		if e.Name == "src" && !e.IsDir() {
			t.Error("a directory inside the archive is not one")
		}
	}
}

// A directory nothing in the archive wrote down is still a directory.
// Most zips only name the files.
func TestADirectoryTheArchiveOnlyImpliesIsOne(t *testing.T) {
	f, at := withAZip(t, "src/deep/x.txt")

	e, err := f.Stat(Join(f, at, "src"))
	if err != nil {
		t.Fatalf("stat it: %v", err)
	}

	if !e.IsDir() {
		t.Error("a directory the archive only implies came back as a file")
	}
}

// A directory deeper in lists what is under it and nothing else.
func TestADirectoryInsideAnArchiveListsItsOwn(t *testing.T) {
	f, at := withAZip(t, "readme.md", "src/main.go", "src/util.go", "src/deep/x.txt")

	got, err := f.ReadDir(Join(f, at, "src"))
	if err != nil {
		t.Fatalf("list it: %v", err)
	}

	if want := []string{"deep", "main.go", "util.go"}; !slices.Equal(named(got), want) {
		t.Errorf("src holds %v, want %v", named(got), want)
	}
}

// A file inside an archive is read, which is what makes the viewer
// work inside one.
func TestAFileInsideAnArchiveIsRead(t *testing.T) {
	f, at := withAZip(t, "src/main.go")

	r, err := f.Open(Join(f, at, "src", "main.go"))
	if err != nil {
		t.Fatalf("open it: %v", err)
	}
	defer r.Close()
	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read it: %v", err)
	}

	if string(got) != "in src/main.go" {
		t.Errorf("it read %q", got)
	}
}

// The archive itself is still a file to read, so a copy of it copies
// the archive rather than walking into it.
func TestTheArchiveItselfIsStillAFileToRead(t *testing.T) {
	f, at := withAZip(t, "readme.md")

	r, err := f.Open(at)
	if err != nil {
		t.Fatalf("open it: %v", err)
	}
	defer r.Close()
	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read it: %v", err)
	}

	if len(got) < 4 || string(got[:2]) != "PK" {
		t.Errorf("reading the archive gave %d bytes starting %q", len(got), got[:min(len(got), 4)])
	}
}

// A listing outside an archive marks the archives in it, so a pane
// offers to walk into one.
func TestAListingMarksTheArchivesInIt(t *testing.T) {
	f, at := withAZip(t, "readme.md")
	dir := filepath.Dir(at)
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("x"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	got, err := f.ReadDir(dir)
	if err != nil {
		t.Fatalf("list the directory: %v", err)
	}

	for _, e := range got {
		switch e.Name {
		case "bundle.zip":
			if !e.IsDir() {
				t.Error("the archive is not offered as a directory")
			}
		case "notes.txt":
			if e.IsDir() {
				t.Error("an ordinary file was marked as a directory")
			}
		}
	}
}

// Walking out of an archive is walking up a path, which is what the
// pane already does.
func TestWalkingOutOfAnArchiveIsWalkingUp(t *testing.T) {
	f, at := withAZip(t, "src/deep/x.txt")
	deep := Join(f, at, "src", "deep")

	up := Dir(f, deep)
	out := Dir(f, Dir(f, up))

	if want := Join(f, at, "src"); up != want {
		t.Errorf("above %s is %s, want %s", deep, up, want)
	}
	if want := filepath.Dir(at); out != want {
		t.Errorf("two above the archive is %s, want %s", out, want)
	}
	if _, err := f.ReadDir(out); err != nil {
		t.Errorf("the directory the archive is in would not list: %v", err)
	}
}

// Every way of writing is refused inside an archive, and says so.
func TestWritingInsideAnArchiveIsRefused(t *testing.T) {
	f, at := withAZip(t, "readme.md")
	inner := Join(f, at, "readme.md")

	_, err := f.Create(Join(f, at, "new.txt"), 0o600)
	tried := map[string]error{
		"create":  err,
		"mkdir":   f.Mkdir(Join(f, at, "d"), 0o700),
		"symlink": f.Symlink("x", Join(f, at, "l")),
		"remove":  f.Remove(inner),
		"rename":  f.Rename(inner, Join(f, at, "other.md")),
		"chmod":   f.Chmod(inner, 0o600),
	}

	for what, err := range tried {
		if err == nil {
			t.Errorf("%s inside an archive was allowed", what)
			continue
		}
		if !errors.Is(err, ErrInArchive) {
			t.Errorf("%s said %q, which does not say why", what, err)
		}
	}
}

// Writing outside one is handed on, so wrapping a filesystem does not
// make it read only.
func TestWritingOutsideAnArchiveStillWorks(t *testing.T) {
	f, at := withAZip(t, "readme.md")
	beside := filepath.Join(filepath.Dir(at), "beside.txt")

	w, err := f.Create(beside, 0o600)
	if err != nil {
		t.Fatalf("create it: %v", err)
	}
	if _, err := io.WriteString(w, "hello"); err != nil {
		t.Fatalf("write it: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close it: %v", err)
	}

	if got, err := os.ReadFile(beside); err != nil || string(got) != "hello" {
		t.Errorf("it read %q, %v", got, err)
	}
}

// The archive is removed and renamed as the file it is: taking one
// away is not taking something out of it.
func TestTheArchiveItselfIsStillAFileToMove(t *testing.T) {
	f, at := withAZip(t, "readme.md")
	to := filepath.Join(filepath.Dir(at), "moved.zip")

	if err := f.Rename(at, to); err != nil {
		t.Fatalf("rename the archive: %v", err)
	}
	if _, err := os.Stat(to); err != nil {
		t.Fatalf("it did not move: %v", err)
	}

	if err := f.Remove(to); err != nil {
		t.Errorf("remove the archive: %v", err)
	}
}

// A name that is not in the archive says so rather than reading empty.
func TestANameThatIsNotInTheArchiveSaysSo(t *testing.T) {
	f, at := withAZip(t, "readme.md")

	if _, err := f.Open(Join(f, at, "nothing.txt")); err == nil {
		t.Error("a name that is not there opened")
	}
	if _, err := f.Stat(Join(f, at, "nothing.txt")); err == nil {
		t.Error("a name that is not there was answered about")
	}
}

// An archive too big to hold is refused with a message rather than
// pulled across a connection.
func TestAnArchiveTooBigIsRefused(t *testing.T) {
	dir := t.TempDir()
	at := filepath.Join(dir, "huge.zip")
	if err := os.WriteFile(at, make([]byte, 16), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	f := &archives{FS: &bigly{FS: NewLocal(), size: MostArchiveBytes + 1}}

	_, err := f.ReadDir(Join(f, at, "x"))

	if err == nil {
		t.Fatal("an archive past the cap was opened")
	}
	if !strings.Contains(err.Error(), "most an archive may be") {
		t.Errorf("it said %q", err)
	}
}

// bigly is a filesystem that says every file is one size, for the test
// about an archive too big to hold.
type bigly struct {
	FS
	size int64
}

func (b *bigly) Stat(at string) (Entry, error) {
	e, err := b.FS.Stat(at)
	e.Size = b.size
	return e, err
}

// Something that is not an archive is not opened as one, whatever it
// is called.
func TestSomethingThatIsNotAnArchiveIsNotOne(t *testing.T) {
	dir := t.TempDir()
	at := filepath.Join(dir, "not-really.zip")
	if err := os.WriteFile(at, []byte("this is not a zip"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	f := WithArchives(NewLocal())
	defer f.Close()

	_, err := f.ReadDir(at)

	if err == nil {
		t.Fatal("a file that is not an archive listed as one")
	}
	if !strings.Contains(err.Error(), "not-really.zip") {
		t.Errorf("it said %q, which does not name the file", err)
	}
}

// Which names open as a directory.
func TestWhichNamesOpenAsADirectory(t *testing.T) {
	for _, tc := range []struct {
		name string
		want bool
	}{
		{"bundle.zip", true},
		{"BUNDLE.ZIP", true},
		{"app.jar", true},
		{"thing.vsix", true},
		{"notes.txt", false},
		{"zip", false},
		{"archive.tar.gz", false},
	} {
		if got := IsArchive(tc.name); got != tc.want {
			t.Errorf("%q came out as %v", tc.name, got)
		}
	}
}

// The archive is read once however many questions are asked of it: a
// pane in one asks about it a row at a time.
func TestTheArchiveIsReadOnce(t *testing.T) {
	f, at := withAZip(t, "readme.md", "src/main.go")
	counted := &counting{FS: NewLocal()}
	wrapped := &archives{FS: counted}

	for range 5 {
		if _, err := wrapped.ReadDir(at); err != nil {
			t.Fatalf("list it: %v", err)
		}
	}

	if counted.opens != 1 {
		t.Errorf("the archive was read %d times", counted.opens)
	}
	_ = f
}

// counting is a filesystem that says how many times a file was opened.
type counting struct {
	FS
	opens int
}

func (c *counting) Open(at string) (io.ReadCloser, error) {
	c.opens++
	return c.FS.Open(at)
}

// A wrapper carries the methods that are not on the interface. The
// window tells a filesystem its machine has been renamed by asking for
// the method by type, and one it could not ask would leave a pane
// under the name its machine had when the pane opened.
func TestTheWrapperCarriesARename(t *testing.T) {
	under := &renameable{FS: NewLocal()}
	f := WithArchives(under)

	got, ok := f.(interface{ Renamed(string) })
	if !ok {
		t.Fatal("the wrapper cannot be told its machine was renamed")
	}
	got.Renamed("office")

	if under.now != "office" {
		t.Errorf("the filesystem under it was told %q", under.now)
	}
}

// And a filesystem with no such method is not a failure: this machine
// is never renamed.
func TestAWrapperOverSomethingThatCannotBeRenamed(t *testing.T) {
	f := WithArchives(NewLocal())

	got, ok := f.(interface{ Renamed(string) })
	if !ok {
		t.Fatal("the wrapper lost the method")
	}
	got.Renamed("office")
}

// renameable is a filesystem that can be told its machine is called
// something else.
type renameable struct {
	FS
	now string
}

func (r *renameable) Renamed(now string) { r.now = now }

// An archive changed since it was read is read again. The wrapper holds
// the last one it read, and a copy from another pane or a program
// outside gridterm can change the file under it.
func TestAnArchiveChangedSinceItWasReadIsReadAgain(t *testing.T) {
	// Asked every time here, rather than once a second.
	was := archiveRecheck
	archiveRecheck = 0
	t.Cleanup(func() { archiveRecheck = was })
	f, at := withAZip(t, "one.txt")
	if got, err := f.ReadDir(at); err != nil || !slices.Equal(named(got), []string{"one.txt"}) {
		t.Fatalf("first read: %v, %v", named(got), err)
	}

	aZip(t, at, "two.txt", "three.txt")

	got, err := f.ReadDir(at)
	if err != nil {
		t.Fatalf("second read: %v", err)
	}
	if want := []string{"three.txt", "two.txt"}; !slices.Equal(named(got), want) {
		t.Errorf("the changed archive lists %v, want %v", named(got), want)
	}
}

// A real directory with an archive's name is a directory, not an
// archive: nothing says to read it as a file.
func TestADirectoryNamedLikeAnArchiveIsNotOne(t *testing.T) {
	dir := t.TempDir()
	at := filepath.Join(dir, "made.zip")
	if err := os.Mkdir(at, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	f := WithArchives(NewLocal())

	e, err := f.Stat(at)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if e.Archive || !e.Stored().IsDir() {
		t.Errorf("a directory named made.zip reads as an archive: %+v", e)
	}
}

// A directory with an archive's name is a directory: made, walked,
// written into and removed like any other. Only a file is an archive.
func TestADirectoryWithAnArchivesNameIsADirectory(t *testing.T) {
	dir := t.TempDir()
	f := WithArchives(NewLocal())
	at := filepath.Join(dir, "made.zip")

	if err := f.Mkdir(at, 0o755); err != nil {
		t.Fatalf("making a directory named made.zip: %v", err)
	}
	w, err := f.Create(filepath.Join(at, "one.txt"), 0o644)
	if err != nil {
		t.Fatalf("writing into it: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	got, err := f.ReadDir(at)
	if err != nil || !slices.Equal(named(got), []string{"one.txt"}) {
		t.Fatalf("listing it: %v, %v", named(got), err)
	}
	if err := f.Remove(filepath.Join(at, "one.txt")); err != nil {
		t.Fatalf("removing what is in it: %v", err)
	}
	if err := f.Remove(at); err != nil {
		t.Errorf("removing it: %v", err)
	}
}

// A link with an archive's name is made, the way a jar is linked to by
// a name without its version.
func TestALinkWithAnArchivesNameIsMade(t *testing.T) {
	f, at := withAZip(t, "one.txt")
	link := filepath.Join(filepath.Dir(at), "latest.zip")

	if err := f.Symlink(at, link); err != nil {
		t.Fatalf("linking latest.zip: %v", err)
	}
}

// Nothing is written inside an archive: that is building the container
// again, which is not what reading one is.
func TestNothingIsWrittenInsideAnArchive(t *testing.T) {
	f, at := withAZip(t, "one.txt")

	if _, err := f.Create(filepath.Join(at, "two.txt"), 0o644); !errors.Is(err, ErrInArchive) {
		t.Errorf("writing inside the archive: %v", err)
	}
	if err := f.Mkdir(filepath.Join(at, "sub"), 0o755); !errors.Is(err, ErrInArchive) {
		t.Errorf("making a directory inside the archive: %v", err)
	}
}

// statCounting counts the questions asked of a filesystem.
type statCounting struct {
	FS
	stats int
}

func (c *statCounting) Stat(at string) (Entry, error) {
	c.stats++
	return c.FS.Stat(at)
}

// The archive held open is asked whether it changed at most once a
// second, not once for every file read out of it: over a connection each
// question is a round trip, and a copy out of a jar opens it for every
// file in it.
func TestAHeldArchiveIsNotAskedAboutForEveryRead(t *testing.T) {
	_, at := withAZip(t, "one.txt", "two.txt", "three.txt")
	c := &statCounting{FS: NewLocal()}
	f := WithArchives(c)

	for range 10 {
		if _, err := f.ReadDir(at); err != nil {
			t.Fatalf("list: %v", err)
		}
		r, err := f.Open(filepath.Join(at, "one.txt"))
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		_ = r.Close()
	}
	if c.stats > 3 {
		t.Errorf("the archive was asked about %d times for twenty reads", c.stats)
	}
}

// A zip replaced through the same pane is read afresh straight away: a
// write to the file lets go of the one held, however recently it was
// asked about.
func TestAZipReplacedThroughThePaneIsReadAgainAtOnce(t *testing.T) {
	f, at := withAZip(t, "old.txt")
	if _, err := f.ReadDir(at); err != nil {
		t.Fatalf("first read: %v", err)
	}
	fresh := filepath.Join(filepath.Dir(at), "fresh.zip")
	aZip(t, fresh, "new.txt")

	if err := f.Rename(fresh, at); err != nil {
		t.Fatalf("rename: %v", err)
	}
	got, err := f.ReadDir(at)
	if err != nil || !slices.Equal(named(got), []string{"new.txt"}) {
		t.Errorf("the replaced zip lists %v, %v", named(got), err)
	}

	// And one removed and made again as a directory is a directory.
	if err := f.Remove(at); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if err := f.Mkdir(at, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	w, err := f.Create(filepath.Join(at, "one.txt"), 0o644)
	if err != nil {
		t.Fatalf("writing into the directory that replaced the zip: %v", err)
	}
	_ = w.Close()
}

// A link with an archive's name that points at a directory is walked
// like the directory.
func TestALinkToADirectoryWithAnArchivesNameIsWalked(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "real"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "real", "one.class"), nil, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.Symlink("real", filepath.Join(dir, "cur.jar")); err != nil {
		t.Skipf("no links here: %v", err)
	}
	f := WithArchives(NewLocal())

	got, err := f.ReadDir(filepath.Join(dir, "cur.jar"))
	if err != nil || !slices.Equal(named(got), []string{"one.class"}) {
		t.Errorf("the link lists %v, %v", named(got), err)
	}
}

// A zip changed by something else is noticed once the second it is
// trusted for has passed.
func TestAZipChangedElsewhereIsNoticedAfterASecond(t *testing.T) {
	was := archiveRecheck
	archiveRecheck = 20 * time.Millisecond
	t.Cleanup(func() { archiveRecheck = was })
	f, at := withAZip(t, "one.txt")
	if _, err := f.ReadDir(at); err != nil {
		t.Fatalf("first read: %v", err)
	}

	aZip(t, at, "two.txt", "three.txt")
	time.Sleep(3 * archiveRecheck)

	got, err := f.ReadDir(at)
	if err != nil || !slices.Equal(named(got), []string{"three.txt", "two.txt"}) {
		t.Errorf("the changed zip lists %v, %v", named(got), err)
	}
}

// A link to a zip is the zip: a path typed into Go to through one, the
// way /usr/share/java links a jar under a name without its version, is
// walked into.
func TestALinkToAZipIsTheZip(t *testing.T) {
	f, at := withAZip(t, "one.txt")
	link := filepath.Join(filepath.Dir(at), "latest.zip")
	if err := os.Symlink(filepath.Base(at), link); err != nil {
		t.Skipf("no links here: %v", err)
	}

	got, err := f.ReadDir(link)
	if err != nil || !slices.Equal(named(got), []string{"one.txt"}) {
		t.Fatalf("the link lists %v, %v", named(got), err)
	}
	r, err := f.Open(filepath.Join(link, "one.txt"))
	if err != nil {
		t.Fatalf("open through the link: %v", err)
	}
	_ = r.Close()
}

// failingStat answers every question about one name with a failure that
// is not "it is not there", the way a dropped connection does.
type failingStat struct {
	FS
	name string
}

func (f failingStat) Stat(at string) (Entry, error) {
	if filepath.Base(at) == f.name {
		return Entry{}, errors.New("the connection went")
	}
	return f.FS.Stat(at)
}

// A name that cannot be asked about is not taken for an archive, so a
// write under it fails for its own reason rather than for being "inside
// an archive".
func TestAWriteThatFailsIsNotBlamedOnAnArchive(t *testing.T) {
	dir := t.TempDir()
	f := WithArchives(failingStat{FS: NewLocal(), name: "ext.xpi"})

	_, err := f.Create(filepath.Join(dir, "ext.xpi", "a.txt"), 0o644)
	if err == nil {
		t.Fatal("writing under a name nothing has worked")
	}
	if errors.Is(err, ErrInArchive) {
		t.Errorf("the failure was blamed on an archive: %v", err)
	}
}

// A link whose target has a colon in its name is a relative path on a
// filesystem that does not write drives, and is followed from where the
// link is.
func TestALinkToANameWithAColonIsFollowedFromTheLink(t *testing.T) {
	if os.PathSeparator == '\\' {
		t.Skip("a name cannot hold a colon here")
	}
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	aZip(t, filepath.Join(sub, "a:b.jar"), "one.txt")
	if err := os.Symlink("a:b.jar", filepath.Join(sub, "l.jar")); err != nil {
		t.Skipf("no links here: %v", err)
	}
	f := WithArchives(NewLocal())

	got, err := f.ReadDir(filepath.Join(sub, "l.jar"))
	if err != nil || !slices.Equal(named(got), []string{"one.txt"}) {
		t.Errorf("the link lists %v, %v", named(got), err)
	}
}

// A link that leads nowhere is a link, not an archive to be: a write
// under it says what the filesystem says rather than blaming an archive.
func TestABrokenLinkIsNotAnArchive(t *testing.T) {
	dir := t.TempDir()
	link := filepath.Join(dir, "gone.jar")
	if err := os.Symlink("nowhere.jar", link); err != nil {
		t.Skipf("no links here: %v", err)
	}
	f := WithArchives(NewLocal())

	e, err := f.Stat(link)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if e.IsDir() {
		t.Error("a broken link is shown as a directory to walk into")
	}
	if _, err := f.Create(filepath.Join(link, "x"), 0o644); errors.Is(err, ErrInArchive) {
		t.Errorf("a write under a broken link was blamed on an archive: %v", err)
	}
}

// sepOf is a filesystem that writes paths with the separator given.
type sepOf struct {
	FS
	sep byte
}

func (s sepOf) Sep() byte { return s.sep }

// Where a link points is taken as starting at the top when it starts
// at the separator, or at a drive and the top of it in either spelling:
// a Windows machine served over SFTP says "C:\x" although its paths go
// with "/". A colon in a name does not make it a drive.
func TestWhereALinkPointsIsReadTheWayTheMachineWritesIt(t *testing.T) {
	for _, c := range []struct {
		sep  byte
		path string
		abs  bool
	}{
		{'/', "/x/y.jar", true},
		{'/', `C:\x\y.jar`, true},
		{'/', "C:/x/y.jar", true},
		{'/', "a:b.jar", false},
		{'/', "../y.jar", false},
		{'/', `..\y.jar`, false},
		{'\\', `C:\x\y.jar`, true},
		{'\\', `\\server\share\y.jar`, true},
		{'\\', `..\y.jar`, false},
	} {
		if got := isAbsOn(sepOf{FS: NewLocal(), sep: c.sep}, c.path); got != c.abs {
			t.Errorf("%q with %q between names: absolute %v, want %v", c.path, c.sep, got, c.abs)
		}
	}
}

// Where a link points, as the filesystem it is on is asked about it: a
// relative target beside the link, and a drive on a machine served over
// SFTP in the spelling its server takes for a drive.
func TestALinksTargetIsAskedForWhereItIs(t *testing.T) {
	slash := sepOf{FS: NewLocal(), sep: '/'}
	back := sepOf{FS: NewLocal(), sep: '\\'}
	for _, c := range []struct {
		f              FS
		at, to, wanted string
	}{
		{slash, "/d/l.jar", "x.jar", "/d/x.jar"},
		{slash, "/d/l.jar", "/e/x.jar", "/e/x.jar"},
		{slash, "/C:/d/l.jar", `C:\e\x.jar`, "/C:/e/x.jar"},
		{slash, "/C:/d/l.jar", "C:/e/x.jar", "/C:/e/x.jar"},
		{slash, "/", "/", "/"},
		{back, `C:\d\l.jar`, `C:\e\x.jar`, `C:\e\x.jar`},
		{back, `C:\d\l.jar`, "x.jar", `C:\d\x.jar`},
	} {
		if got := linkTarget(c.f, c.at, c.to); got != c.wanted {
			t.Errorf("%s -> %s is asked for as %q, want %q", c.at, c.to, got, c.wanted)
		}
	}
}

// A chain of links as long as the most followed is followed to its end;
// one longer is given up on.
func TestAChainOfLinksIsFollowedAsFarAsItMay(t *testing.T) {
	dir := t.TempDir()
	aZip(t, filepath.Join(dir, "real.zip"), "one.txt")
	prev := "real.zip"
	for i := range mostLinkHops + 1 {
		name := fmt.Sprintf("l%d.zip", i)
		if err := os.Symlink(prev, filepath.Join(dir, name)); err != nil {
			t.Skipf("no links here: %v", err)
		}
		prev = name
	}
	f := WithArchives(NewLocal())

	longest := filepath.Join(dir, fmt.Sprintf("l%d.zip", mostLinkHops-1))
	if got, err := f.ReadDir(longest); err != nil || !slices.Equal(named(got), []string{"one.txt"}) {
		t.Errorf("%d links to the zip list %v, %v", mostLinkHops, named(got), err)
	}
	if e, err := f.(*archives).followed(filepath.Join(dir, fmt.Sprintf("l%d.zip", mostLinkHops))); err == nil {
		t.Errorf("%d links were followed to %+v", mostLinkHops+1, e)
	}
}
