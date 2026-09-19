package main

import (
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/vfs"
)

// aDroppedFile writes a file of n bytes and returns its path.
func aDroppedFile(t *testing.T, name string, n int) string {
	t.Helper()
	at := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(at, []byte(strings.Repeat("x", n)), 0o600); err != nil {
		t.Fatalf("write it: %v", err)
	}
	return at
}

// What is typed for a set of paths is one line, and a path holding a
// space is quoted: what reads it is a shell or a program taking a word.
func TestWhatIsTypedForDroppedPaths(t *testing.T) {
	for what, tc := range map[string]struct {
		paths []string
		want  string
	}{
		"one":              {[]string{`C:\tmp\a.png`}, `C:\tmp\a.png`},
		"two":              {[]string{`/tmp/a`, `/tmp/b`}, `/tmp/a /tmp/b`},
		"one with a space": {[]string{`C:\my files\a.png`}, `"C:\my files\a.png"`},
		"none":             {nil, ""},
	} {
		if got := typedPaths(tc.paths); got != tc.want {
			t.Errorf("%s: it types %q, want %q", what, got, tc.want)
		}
	}
}

// A file dropped on a pane on this machine is typed straight away.
// There is nothing to copy: the program is running on the machine the
// file is already on.
func TestAFileDroppedOnAPaneHereIsTypedAtOnce(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withPanel(t, a)
	pane := onlyPaneOn(t, a)
	at := aDroppedFile(t, "notes.txt", 16)
	jobs := len(a.jobs)

	if err := a.dropOnPane(pane, []string{at}); err != nil {
		t.Fatalf("drop it: %v", err)
	}

	waitFor(t, a, "the path to reach the shell", func() bool {
		return strings.Contains(a.shells[0].sentText(), at)
	})
	if got := len(a.jobs); got != jobs {
		t.Errorf("it started %d jobs to copy a file that is already here", got-jobs)
	}
}

// homedFS is a filesystem whose home is a directory the test owns, so a
// test never writes into whoever is running it's own home.
type homedFS struct {
	vfs.FS
	home string
}

func (f homedFS) Home() (string, error) { return f.home, nil }
func (f homedFS) Name() string          { return "over there" }

// Dropped files go in a directory of their own under the home directory
// of the machine they land on, made if it is not there yet.
func TestDroppedFilesGoUnderHome(t *testing.T) {
	home := t.TempDir()
	fs := homedFS{FS: vfs.NewLocal(), home: home}

	dir, err := pastedDirOn(fs)
	if err != nil {
		t.Fatalf("work out where: %v", err)
	}

	if want := filepath.Join(home, pastedDir); dir != want {
		t.Errorf("it puts them in %q, want %q", dir, want)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Errorf("the directory was not made: %v", err)
	}
	// Asked again, it takes the one that is there rather than failing.
	if again, err := pastedDirOn(fs); err != nil || again != dir {
		t.Errorf("asked again it gave %q, %v", again, err)
	}
}

// A file dropped on a pane on another machine is copied there, with a
// row on the sidebar saying how far it has got and a cross that stops
// it, and its path is typed once it has arrived.
func TestAFileDroppedOnAPaneElsewhereIsCopiedThere(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	pane := onlyPaneOn(t, a)
	at := aDroppedFile(t, "notes.txt", 4096)
	into := t.TempDir()

	a.uploadOne(vfs.NewLocal(), a.about(conns.Local), pane, at, into, nil)

	row := theJobRow(t, a)
	if row.Close == nil {
		t.Error("the row has no way to stop the copy")
	}
	waitFor(t, a, "the path to reach the shell", func() bool {
		return strings.Contains(a.shells[0].sentText(), pastedDir) ||
			strings.Contains(a.shells[0].sentText(), "notes.txt")
	})

	// The file is there, and its path is what was typed.
	want := filepath.Join(into, "notes.txt")
	if _, err := os.Stat(want); err != nil {
		t.Errorf("the file was not copied: %v", err)
	}
	if got := a.shells[0].sentText(); !strings.Contains(got, "notes.txt") {
		t.Errorf("it typed %q, want the path of the file it copied", got)
	}
}

// A copy that was stopped types no path: there is nothing at the other
// end to name.
func TestAStoppedCopyTypesNoPath(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	pane := onlyPaneOn(t, a)
	at := aDroppedFile(t, "big.bin", 1<<20)
	into := t.TempDir()
	held := &stalledFS{FS: vfs.NewLocal(), hold: make(chan struct{})}

	a.uploadOne(held, a.about(conns.Local), pane, at, into, nil)
	row := theJobRow(t, a)
	if err := row.Close(); err != nil {
		t.Fatalf("stop it: %v", err)
	}
	close(held.hold)

	waitFor(t, a, "the job to stop", func() bool { return len(a.jobs) == 0 })
	for range 20 {
		a.pump.run()
		time.Sleep(time.Millisecond)
	}
	if got := a.shells[0].sentText(); strings.Contains(got, "big.bin") {
		t.Errorf("it typed %q after the copy was stopped", got)
	}
}

// stalledFS holds a write until the test lets it go, so a copy can be
// stopped part way through.
type stalledFS struct {
	vfs.FS
	hold chan struct{}
}

func (f *stalledFS) Create(path string, mode fs.FileMode) (io.WriteCloser, error) {
	<-f.hold
	return f.FS.Create(path, mode)
}

// A drop lands on the pane under the pointer, and on the pane in front
// when it was dropped on something that is not a pane.
func TestAPaneIsFoundUnderThePointer(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withPanel(t, a)
	pane := onlyPaneOn(t, a)
	a.relayout()

	area, ok := a.paneArea(pane)
	if !ok {
		t.Fatal("the pane has no room in the window")
	}
	left, width := a.geo.ColBox(area.X, area.X+area.Cols)
	top, height := a.geo.RowBox(area.Y, area.Y+area.Rows)

	if got := a.paneAt(left+width/2, top+height/2); got != pane {
		t.Errorf("the middle of the pane gave %v, want the pane", got)
	}
	// The sidebar is not a pane.
	side, ok := a.dock.ChildArea(a.side)
	if !ok {
		t.Fatal("the sidebar has no room")
	}
	sideLeft, sideWidth := a.geo.ColBox(side.X, side.X+side.Cols)
	if got := a.paneAt(sideLeft+sideWidth/2, top+height/2); got != nil {
		t.Errorf("a drop on the sidebar gave %v, want no pane", got)
	}
}
