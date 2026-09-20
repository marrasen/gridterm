package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marrasen/gridterm/conns"
)

// A path a pane printed is found on the disk, and what it is is read
// off the disk rather than guessed from the name.
func TestAPathIsFoundOnTheDisk(t *testing.T) {
	dir := t.TempDir()
	under := filepath.Join(dir, "a-folder")
	if err := os.Mkdir(under, 0o700); err != nil {
		t.Fatalf("make the folder: %v", err)
	}
	file := filepath.Join(dir, "a-file.txt")
	if err := os.WriteFile(file, []byte("x\n"), 0o600); err != nil {
		t.Fatalf("write the file: %v", err)
	}

	at, isDir, ok := findOnDisk(file, "")
	if !ok || isDir || at != file {
		t.Errorf("the file came back as %q dir=%v ok=%v", at, isDir, ok)
	}

	at, isDir, ok = findOnDisk(under, "")
	if !ok || !isDir || at != under {
		t.Errorf("the folder came back as %q dir=%v ok=%v", at, isDir, ok)
	}
}

// A name that is on nothing is not a link. The disk is the check,
// which is what makes finding a path safe where finding an address
// is a guess.
func TestANameThatIsOnNothingIsNotAPath(t *testing.T) {
	dir := t.TempDir()

	for _, text := range []string{
		filepath.Join(dir, "not-there.txt"),
		"src/main.go",
		"just-a-word",
		"",
		"   ",
	} {
		if at, _, ok := findOnDisk(text, dir); ok {
			t.Errorf("%q was taken as %q", text, at)
		}
	}
}

// A relative name is resolved against the directory the shell said
// it was in, which is what makes "src/main.go" in a build's output
// mean something.
func TestARelativeNameIsResolvedAgainstTheShellsDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "src"), 0o700); err != nil {
		t.Fatalf("make the folder: %v", err)
	}
	at := filepath.Join(dir, "src", "main.go")
	if err := os.WriteFile(at, []byte("package main\n"), 0o600); err != nil {
		t.Fatalf("write the file: %v", err)
	}

	got, isDir, ok := findOnDisk("src/main.go", dir)

	if !ok {
		t.Fatal("the relative name was not found")
	}
	if isDir {
		t.Error("it came back as a folder")
	}
	if got != at {
		t.Errorf("it found %q, want %q", got, at)
	}
}

// With no directory to resolve against, a relative name is nothing.
// Trying it against gridterm's own directory would open a file the
// user is nowhere near.
func TestARelativeNameWithNoDirectoryIsNothing(t *testing.T) {
	if at, _, ok := findOnDisk("openpath.go", ""); ok {
		t.Errorf("a relative name was resolved to %q with nowhere to resolve it", at)
	}
}

// A path with a line break in it is not one, whatever the disk says.
func TestAPathWithALineBreakIsRefused(t *testing.T) {
	dir := t.TempDir()
	at := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(at, []byte("x"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	if _, _, ok := findOnDisk(at+"\nmore", dir); ok {
		t.Error("a name with a line break was taken as a path")
	}
}

// A pane on another machine prints that machine's paths, and looking
// for them here would find the wrong file.
func TestAPaneOnAnotherMachineLooksForNoPaths(t *testing.T) {
	a := newTestApp(t, 80, 24)

	if a.pathFinder("margit") != nil {
		t.Error("a pane on margit would look for paths on this disk")
	}
	if a.pathOpener("margit") != nil {
		t.Error("a pane on margit would open paths on this machine")
	}
	if a.pathFinder(conns.Local) == nil {
		t.Error("a pane on this machine looks for no paths")
	}
}

// A folder opens the file browser.
func TestAFolderOpensTheBrowser(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	withDialogs(t, a)
	a.commands()
	dir := t.TempDir()

	if err := a.openOnDisk(dir, true, 0); err != nil {
		t.Fatalf("open it: %v", err)
	}

	if a.files == nil {
		t.Fatal("no file browser opened")
	}
	pane := a.files.view.Here()
	if pane == nil {
		t.Fatal("the browser has no pane")
	}
	waitFor(t, a, "the browser to reach the folder", func() bool {
		return strings.EqualFold(pane.At(), dir)
	})
	if len(a.readers) != 0 {
		t.Errorf("%d viewers opened for a folder", len(a.readers))
	}
}

// A file opens the viewer.
func TestAFileOpensTheViewer(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	withDialogs(t, a)
	a.commands()
	at := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(at, []byte("one\ntwo\nthree\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	if err := a.openOnDisk(at, false, 0); err != nil {
		t.Fatalf("open it: %v", err)
	}

	r := onlyReader(t, a)
	if r.Name() != "notes.txt" {
		t.Errorf("the viewer is on %q", r.Name())
	}
	if a.files != nil {
		t.Error("a file browser opened for a file")
	}
}

// A file named with a line goes to that line, which is what a build
// error naming one is for.
func TestAFileNamedWithALineGoesToIt(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	withDialogs(t, a)
	a.commands()
	at := filepath.Join(t.TempDir(), "notes.txt")
	lines := make([]string, 400)
	for i := range lines {
		lines[i] = "a line"
	}
	if err := os.WriteFile(at, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	if err := a.openOnDisk(at, false, 300); err != nil {
		t.Fatalf("open it: %v", err)
	}

	r := onlyReader(t, a)
	waitFor(t, a, "the viewer to reach the line", func() bool {
		return r.Lines() > 0 && r.Top() > 0
	})
	if r.Top() == 0 {
		t.Error("the viewer stayed at the top rather than going to the line")
	}
}
