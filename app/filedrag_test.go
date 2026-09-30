package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/gunimtest"
	"github.com/marrasen/kakel/screen"
)

// twoFolders is a program with two file panes on this computer, p1 on
// from, holding a.txt and dir/, and p2 on into.
func twoFolders(t *testing.T) (a *app, from, into string) {
	t.Helper()
	from, into = t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(from, "a.txt"), []byte("a"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(from, "dir"), 0o700); err != nil {
		t.Fatal(err)
	}
	a = newApp(gunimtest.New(t, geom.Sz(400, 300), nil).Client(), screen.NewShells())
	a.ctx = t.Context()
	a.addPane(Pane{ID: "p1", Kind: KindFiles}, nil, Placement{})
	a.addPane(Pane{ID: "p2", Kind: KindFiles}, nil, Placement{})
	a.setBrowser("p1", Browser{Path: from, Seq: 1})
	a.setBrowser("p2", Browser{Path: into, Seq: 1})
	return a, from, into
}

// Rows dropped on another folder of the same disk move there; with Copy
// they copy.
func TestRowsDroppedOnAPaneMoveOrCopy(t *testing.T) {
	a, from, into := twoFolders(t)
	a.handle(DropOnFiles{Pane: "p2", Into: into, Drag: FileDrag{Pane: "p1", At: from, Names: []string{"a.txt"}, Sep: "/"}})
	waitFor(t, a, "the move", func() bool { return len(a.st.Jobs) == 1 && a.st.Jobs[0].Done })
	if _, err := os.Stat(filepath.Join(into, "a.txt")); err != nil {
		t.Fatal("the file did not arrive")
	}
	if _, err := os.Stat(filepath.Join(from, "a.txt")); err == nil {
		t.Fatal("moved, the file is still where it was")
	}
	a.handle(DropOnFiles{Pane: "p1", Into: from, Drag: FileDrag{Pane: "p2", At: into, Names: []string{"a.txt"}, Sep: "/"}, Copy: true})
	waitFor(t, a, "the copy", func() bool { return len(a.st.Jobs) == 2 && a.st.Jobs[1].Done })
	for _, d := range []string{from, into} {
		if _, err := os.Stat(filepath.Join(d, "a.txt")); err != nil {
			t.Fatalf("copied, the file is missing from %s", d)
		}
	}
}

// A folder dropped into itself is refused, and files from another
// program are copied in.
func TestAFolderIntoItselfIsRefusedAndOtherProgramsFilesCopy(t *testing.T) {
	a, from, into := twoFolders(t)
	err := a.dropOnFiles(DropOnFiles{Pane: "p1", Into: filepath.Join(from, "dir"), Drag: FileDrag{Pane: "p1", At: from, Names: []string{"dir"}, Sep: "/"}})
	if err == nil {
		t.Fatal("a folder went into itself")
	}
	a.handle(DropOnFiles{Pane: "p2", Into: into, Paths: []string{filepath.Join(from, "a.txt")}, Copy: true})
	waitFor(t, a, "the copy", func() bool { return len(a.st.Jobs) == 1 && a.st.Jobs[0].Done })
	if _, err := os.Stat(filepath.Join(into, "a.txt")); err != nil {
		t.Fatal("the file from another program did not arrive")
	}
	if _, err := os.Stat(filepath.Join(from, "a.txt")); err != nil {
		t.Fatal("the file from another program left its folder")
	}
}

// Files on this computer leave kakel as their paths; files elsewhere
// do not.
func TestOnlyLocalFilesDragOut(t *testing.T) {
	paths, err := FileDrag{At: "/srv/x", Names: []string{"a", "b"}, Sep: "/", Local: true}.ExportFiles()
	if err != nil || len(paths) != 2 || paths[1] != "/srv/x/b" {
		t.Fatalf("exported %v, %v", paths, err)
	}
	if _, err := (FileDrag{At: "/srv", Names: []string{"a"}, Sep: "/", Machine: "srv"}).ExportFiles(); err == nil {
		t.Fatal("a file on a server was dragged out")
	}
}
