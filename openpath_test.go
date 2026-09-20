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

// A pane on another machine is never answered from this disk. It
// prints that machine's paths, and a name that happens to be on this
// one would open the wrong file.
func TestAPaneOnAnotherMachineIsNeverAnsweredFromThisDisk(t *testing.T) {
	a := newTestApp(t, 80, 24)
	dir := t.TempDir()
	at := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(at, []byte("x\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	find := a.pathFinder("margit")
	if find == nil {
		t.Fatal("a pane on margit looks for no paths at all")
	}
	if got, _, ok := find(at, dir); ok {
		t.Errorf("a pane on margit was told about %q, which is a file on this machine", got)
	}
	if a.pathFinder(conns.Local) == nil {
		t.Error("a pane on this machine looks for no paths")
	}
	if a.pathOpener("margit") == nil {
		t.Error("a pane on margit opens nothing")
	}
}

// What a machine at the far end already said is what the pointer is
// answered with, because asking again is a round trip.
func TestWhatAMachineAlreadySaidIsUsed(t *testing.T) {
	a := newTestApp(t, 80, 24)
	a.far.known["margit\x00/srv/app/main.go"] = farPath{
		at: "/srv/app/main.go", size: 120, found: true,
	}

	at, isDir, ok := a.findFar("margit", "main.go", "/srv/app")

	if !ok || isDir || at != "/srv/app/main.go" {
		t.Errorf("it came back as %q dir=%v ok=%v", at, isDir, ok)
	}
}

// A run of text is resolved against the directory the shell named, in
// the way that directory is written: the machine at the far end may
// not use this one's separator.
func TestAPathAtTheFarEndIsJoinedTheWayItIsWritten(t *testing.T) {
	for _, tc := range []struct {
		text, dir, want string
		ok              bool
	}{
		{"main.go", "/srv/app", "/srv/app/main.go", true},
		{"src/main.go", "/srv/app/", "/srv/app/src/main.go", true},
		{"/etc/hosts", "", "/etc/hosts", true},
		{`main.go`, `C:\app`, `C:\app\main.go`, true},
		{`C:\app\main.go`, "", `C:\app\main.go`, true},
		{`\\server\share\f`, "", `\\server\share\f`, true},
		// Nowhere to resolve it against.
		{"main.go", "", "", false},
		{"", "/srv", "", false},
		{"a\nb", "/srv", "", false},
	} {
		got, ok := farPathToTry(tc.text, tc.dir)
		if ok != tc.ok || got != tc.want {
			t.Errorf("%q in %q gave %q ok=%v, want %q ok=%v",
				tc.text, tc.dir, got, ok, tc.want, tc.ok)
		}
	}
}

// A machine nothing is connected to is not asked, and the question is
// not left to go out again on every frame.
func TestAMachineNothingIsConnectedToIsNotAsked(t *testing.T) {
	a := newTestApp(t, 80, 24)

	if _, _, ok := a.findFar("margit", "/etc/hosts", ""); ok {
		t.Error("a machine nothing is connected to answered")
	}

	if _, written := a.far.known["margit\x00/etc/hosts"]; !written {
		t.Error("the question was not written off, so it goes out again every frame")
	}
	if len(a.far.asking) != 0 {
		t.Error("a question went out to a machine nothing is connected to")
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
