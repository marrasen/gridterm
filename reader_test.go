package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

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

	if err := a.openReader(vfs.NewLocal(), conns.Local, path, name, false); err != nil {
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
	held := a.readers[r]
	if held == nil {
		t.Fatal("the reader has no row on the sidebar")
	}
	if held.row.Kind != conns.Reader {
		t.Errorf("the row is a %v, want a reader", held.row.Kind)
	}
	if held.row.Label != name {
		t.Errorf("the row is called %q, want the file's name", held.row.Label)
	}
}

// Closing a reader takes its row off the sidebar and its pane out of the
// window.
func TestClosingAReaderTakesItsRowAway(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	name, path := aReadableFile(t, "one")
	if err := a.openReader(vfs.NewLocal(), conns.Local, path, name, false); err != nil {
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
	if err := a.openReader(vfs.NewLocal(), "kettle", path, name, false); err != nil {
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

	if err := a.openReader(vfs.NewLocal(), conns.Local, gone, "gone.txt", false); err != nil {
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

// A reader following a file picks up what is written to it, and stays at
// the end as it grows.
//
// The file is longer than the pane on purpose. A file that fits has
// nowhere to scroll, so its reader is at the end whatever following did.
func TestAFollowingReaderPicksUpWhatIsWritten(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	was := logOf(60)
	name, path := aReadableFile(t, was...)
	if err := a.openReader(vfs.NewLocal(), conns.Local, path, name, false); err != nil {
		t.Fatalf("open a reader: %v", err)
	}
	r := onlyReader(t, a)
	waitUntil(t, "the first read", func() bool {
		a.pump.run()
		return r.Lines() == len(was)
	})
	r.Follow(true)
	wasTop := r.Top()
	if wasTop == 0 {
		t.Fatal("the file is not longer than the pane, so this proves nothing")
	}

	// The file grows, the way a log does.
	grown := logOf(90)
	if err := os.WriteFile(path, []byte(strings.Join(grown, "\n")), 0o600); err != nil {
		t.Fatalf("write the file: %v", err)
	}
	at := time.Now()
	waitUntil(t, "the reader to pick up the new lines", func() bool {
		at = at.Add(followEvery)
		a.followReaders(at)
		a.pump.run()
		return r.Lines() == len(grown)
	})

	if got := r.Top(); got <= wasTop {
		t.Errorf("the reader is on line %d, where it was before the file grew", got+1)
	}
	if !r.AtEnd() {
		t.Error("the reader followed the file and did not stay at its end")
	}
}

// logOf is a file of n lines, each saying which it is.
func logOf(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = "line " + strconv.Itoa(i+1)
	}
	return out
}

// A file that has not changed is not read again, or a reader following a
// quiet log would read it sixty times a second.
func TestAFileThatHasNotChangedIsNotReadAgain(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	name, path := aReadableFile(t, "one", "two")
	if err := a.openReader(vfs.NewLocal(), conns.Local, path, name, false); err != nil {
		t.Fatalf("open a reader: %v", err)
	}
	r := onlyReader(t, a)
	waitUntil(t, "the first read", func() bool {
		a.pump.run()
		return r.Lines() == 2
	})
	r.Follow(true)

	// Asked about twice, so the second answer is compared with the
	// first rather than with nothing.
	at := time.Now()
	reads := 0
	for i := 0; i < 4; i++ {
		at = at.Add(followEvery)
		a.followReaders(at)
		waitUntil(t, "the question to come back", func() bool {
			a.pump.run()
			return !a.readers[r].checking
		})
		if r.Busy() {
			reads++
			waitUntil(t, "that read", func() bool {
				a.pump.run()
				return !r.Busy()
			})
		}
	}

	if reads > 1 {
		t.Errorf("a file nobody wrote to was read %d times over", reads)
	}
}

// A reader takes the keys when it is focused, and says so on its bar.
func TestAReaderTakesTheKeys(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	name, path := aReadableFile(t, "one", "two")
	if err := a.openReader(vfs.NewLocal(), conns.Local, path, name, false); err != nil {
		t.Fatalf("open a reader: %v", err)
	}
	r := onlyReader(t, a)

	a.focus(r)

	if !r.Focused() {
		t.Error("the window focused the reader and it does not know")
	}
	if got := ui.FocusedLeaf(a.root.Widget()); got != ui.Widget(r) {
		t.Errorf("the keys are on %T, want the reader", got)
	}
}

// With a reader in front, the window acts on the machine the file is on.
func TestAReaderInFrontIsOnItsOwnMachine(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	name, path := aReadableFile(t, "one")
	if err := a.openReader(vfs.NewLocal(), "kettle", path, name, false); err != nil {
		t.Fatalf("open a reader: %v", err)
	}
	a.focus(onlyReader(t, a))

	if got := a.currentHost(); got != "kettle" {
		t.Errorf("the window is on %q, want the machine the file is on", got)
	}
}

// A reader's row is named the way the sidebar names a kind, not
// "Unknown".
func TestAReadersKindHasAName(t *testing.T) {
	if got := conns.Reader.String(); got == "Unknown" || got == "" {
		t.Errorf("a reader's kind is called %q", got)
	}
}

// Closing the browser pane a reader was opened from leaves the reader
// able to read: the connection it reads through is still its to use.
func TestClosingTheBrowserLeavesAReaderAbleToRead(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	name, path := aReadableFile(t, "one", "two")
	f := vfs.NewLocal()
	if err := a.openReader(f, conns.Local, path, name, false); err != nil {
		t.Fatalf("open a reader: %v", err)
	}
	r := onlyReader(t, a)
	waitUntil(t, "the first read", func() bool {
		a.pump.run()
		return r.Lines() == 2
	})

	// The browser lets the filesystem go while the reader still has it.
	if err := a.browserLetGoFS(f); err != nil {
		t.Fatalf("let it go: %v", err)
	}

	if a.fsHeld[f] == 0 {
		t.Error("the reader's hold on the filesystem was not counted")
	}
	if !a.fsGone[f] {
		t.Error("the browser letting go was not remembered")
	}
	// And the reader can still read through it.
	r.Open()
	waitUntil(t, "the reread", func() bool {
		a.pump.run()
		return !r.Busy()
	})
	if err := r.Err(); err != nil {
		t.Errorf("the reread failed after the browser let go: %v", err)
	}

	// Closing the reader is what finally lets it go.
	if err := a.closePane(r); err != nil {
		t.Fatalf("close the reader: %v", err)
	}
	if a.fsHeld[f] != 0 || a.fsGone[f] {
		t.Error("the filesystem was not let go of when the last reader closed")
	}
}
