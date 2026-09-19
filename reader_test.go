package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/ui"
	"github.com/marrasen/gridterm/ui/files"
	"github.com/marrasen/gridterm/vfs"
)

// aReadableFile writes a file on this machine and returns its name and
// its path.
func aReadableFile(t *testing.T, lines ...string) (name, path string) {
	t.Helper()
	name = "notes.txt"
	path = filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o600); err != nil {
		t.Fatalf("write the file: %v", err)
	}
	return name, path
}

// Opening a file from the browser puts a reader in the window, with a
// row on the sidebar under the machine the file is on.
func TestOpeningAFileFromTheBrowserPutsAReaderInTheWindow(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	name, path := aReadableFile(t, "one", "two", "three")

	if err := a.openReader(vfs.NewLocal(), conns.Local, path, name); err != nil {
		t.Fatalf("open a reader: %v", err)
	}

	r := onlyReader(t, a)
	waitUntil(t, "the file to be read", func() bool {
		a.pump.run()
		return r.Lines() > 0
	})
	if got := r.Lines(); got != 3 {
		t.Errorf("the reader holds %d lines, want the three in the file", got)
	}
	row := a.readers[r]
	if row == nil {
		t.Fatal("the reader has no row on the sidebar")
	}
	if row.Kind != conns.Reader {
		t.Errorf("the row is a %v, want a reader", row.Kind)
	}
	if row.Label != name {
		t.Errorf("the row is called %q, want the file's name", row.Label)
	}
}

// Closing a reader takes its row off the sidebar and its pane out of the
// window.
func TestClosingAReaderTakesItsRowAway(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	name, path := aReadableFile(t, "one")
	if err := a.openReader(vfs.NewLocal(), conns.Local, path, name); err != nil {
		t.Fatalf("open a reader: %v", err)
	}
	r := onlyReader(t, a)
	before := a.registry.Len()

	if err := a.closePane(r); err != nil {
		t.Fatalf("close it: %v", err)
	}

	if len(a.readers) != 0 {
		t.Errorf("the window still holds %d readers", len(a.readers))
	}
	if got := a.registry.Len(); got != before-1 {
		t.Errorf("the sidebar holds %d rows, want one fewer than the %d it had", got, before)
	}
	if inTree(a, r) {
		t.Error("the reader is still in the tree")
	}
}

// A reader is a pane: the window names it, files it under the machine
// the file is on, and lists it among the panes to switch between.
func TestAReaderIsAPane(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	name, path := aReadableFile(t, "one")
	if err := a.openReader(vfs.NewLocal(), "kettle", path, name); err != nil {
		t.Fatalf("open a reader: %v", err)
	}
	r := onlyReader(t, a)

	if got := a.paneName(r); !strings.Contains(got, name) {
		t.Errorf("the window calls it %q, want the file's name in it", got)
	}
	if got := a.entryOf(r); got == nil {
		t.Error("the window finds no row for it")
	}
	if !a.isPane(r) {
		t.Error("a reader is not a pane, so nothing can close it")
	}
}

// A file that will not read leaves the reader saying why, on the pane
// and on its row.
func TestAFileThatWillNotReadSaysWhyOnItsRow(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	gone := filepath.Join(t.TempDir(), "gone.txt")

	if err := a.openReader(vfs.NewLocal(), conns.Local, gone, "gone.txt"); err != nil {
		t.Fatalf("open a reader: %v", err)
	}

	r := onlyReader(t, a)
	waitUntil(t, "the read to fail", func() bool {
		a.pump.run()
		return r.Err() != nil
	})
	if got := a.readerNote(r); got == "" {
		t.Error("the row says nothing about a file that would not read")
	}
}

// onlyReader is the window's one reader.
func onlyReader(t *testing.T, a *testApp) *files.Reader {
	t.Helper()
	if len(a.readers) != 1 {
		t.Fatalf("the window holds %d readers, want the one", len(a.readers))
	}
	for r := range a.readers {
		return r
	}
	return nil
}

// inTree reports whether a widget is still in the window's tree.
func inTree(a *testApp, w ui.Widget) bool {
	for _, leaf := range ui.Leaves(a.root.Widget()) {
		if leaf == w {
			return true
		}
	}
	return false
}
