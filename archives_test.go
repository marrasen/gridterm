package main

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/ui/files"
	"github.com/marrasen/gridterm/vfs"
)

// aBundle writes an archive holding a couple of files, and answers
// the directory it is in and the archive's path.
func aBundle(t *testing.T) (dir, at string) {
	t.Helper()
	dir = t.TempDir()
	at = filepath.Join(dir, "bundle.zip")
	var b bytes.Buffer
	w := zip.NewWriter(&b)
	for name, text := range map[string]string{
		"readme.md":   "# the bundle\n",
		"src/main.go": "package main\n",
	} {
		f, err := w.Create(name)
		if err != nil {
			t.Fatalf("put %s in the archive: %v", name, err)
		}
		if _, err := io.WriteString(f, text); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close the archive: %v", err)
	}
	if err := os.WriteFile(at, b.Bytes(), 0o600); err != nil {
		t.Fatalf("write the archive: %v", err)
	}
	return dir, at
}

// browserAt opens the file browser at a directory and answers its
// pane.
func browserAt(t *testing.T, a *testApp, dir string) *files.Pane {
	t.Helper()
	if err := a.openFilesAt(conns.Local, dir); err != nil {
		t.Fatalf("open the browser: %v", err)
	}
	pane := a.files.view.Here()
	if pane == nil {
		t.Fatal("the browser has no pane")
	}
	// The listing as well as the directory: a pane is moved before the
	// read comes back, so a test that waited only for the path would
	// read an empty listing.
	waitFor(t, a, "the browser to read the directory", func() bool {
		return strings.EqualFold(pane.At(), dir) && len(pane.Entries()) > 0
	})
	return pane
}

// namesOn is what a browser pane is showing.
func namesOn(p *files.Pane) []string {
	var out []string
	for _, e := range p.Entries() {
		out = append(out, e.Name)
	}
	return out
}

// The browser walks into an archive the way it walks into a
// directory, and the archive's own name is a row that can be entered.
func TestTheBrowserWalksIntoAnArchive(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	withDialogs(t, a)
	a.commands()
	dir, at := aBundle(t)
	pane := browserAt(t, a, dir)

	var isDir bool
	for _, e := range pane.Entries() {
		if e.Name == "bundle.zip" {
			isDir = e.IsDir()
		}
	}
	if !isDir {
		t.Fatalf("the archive is not offered as a directory, among %v", namesOn(pane))
	}

	pane.Open(at)

	waitFor(t, a, "the browser to reach the archive", func() bool {
		return strings.EqualFold(pane.At(), at)
	})
	got := namesOn(pane)
	if !contains(got, "readme.md") || !contains(got, "src") {
		t.Errorf("the archive shows %v", got)
	}
}

// And back out of it, which is walking up a path.
func TestTheBrowserWalksBackOutOfAnArchive(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	withDialogs(t, a)
	a.commands()
	dir, at := aBundle(t)
	pane := browserAt(t, a, dir)
	pane.Open(vfs.Join(pane.FS(), at, "src"))
	waitFor(t, a, "the browser to reach the directory in the archive", func() bool {
		return contains(namesOn(pane), "main.go")
	})

	pane.Up()
	waitFor(t, a, "the browser to reach the top of the archive", func() bool {
		return strings.EqualFold(pane.At(), at)
	})
	pane.Up()

	waitFor(t, a, "the browser to reach the directory the archive is in", func() bool {
		return strings.EqualFold(pane.At(), dir)
	})
	if got := namesOn(pane); !contains(got, "bundle.zip") {
		t.Errorf("coming out of the archive showed %v", got)
	}
}

// A file inside an archive opens in the viewer, which is what makes
// the archive worth walking into.
func TestAFileInsideAnArchiveOpensInTheViewer(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	withDialogs(t, a)
	a.commands()
	dir, at := aBundle(t)
	pane := browserAt(t, a, dir)
	pane.Open(at)
	waitFor(t, a, "the browser to reach the archive", func() bool {
		return contains(namesOn(pane), "readme.md")
	})

	inside := vfs.Join(pane.FS(), at, "readme.md")
	if err := a.openReader(pane.FS(), conns.Local, inside, "readme.md", false, 0); err != nil {
		t.Fatalf("open it: %v", err)
	}

	r := onlyReader(t, a)
	waitFor(t, a, "the viewer to read the file out of the archive", func() bool {
		return r.Lines() > 0
	})
	if got := r.Name(); got != "readme.md" {
		t.Errorf("the viewer is on %q", got)
	}
}

// Writing into an archive is refused, and the window says why rather
// than failing quietly.
func TestWritingIntoAnArchiveIsRefused(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	withDialogs(t, a)
	a.commands()
	_, at := aBundle(t)
	f, err := a.filesystem(conns.Local)
	if err != nil {
		t.Fatalf("open the filesystem: %v", err)
	}
	defer f.Close()

	err = f.Mkdir(vfs.Join(f, at, "new"), 0o700)

	if err == nil {
		t.Fatal("a directory was made inside an archive")
	}
	if !strings.Contains(err.Error(), "read only") {
		t.Errorf("it said %q, which does not say why", err)
	}
}

// contains reports whether a list holds a name.
func contains(got []string, want string) bool {
	for _, at := range got {
		if at == want {
			return true
		}
	}
	return false
}
