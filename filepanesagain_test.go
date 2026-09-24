package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/marrasen/gridterm/jobs"
	"github.com/marrasen/gridterm/ui/files"
	"github.com/marrasen/gridterm/vfs"
)

// Step 2 of the plan: everything else a file pane does goes through the
// same wrapper, so each of them opens the machine again. Each has its
// own failure path in the browser, so each is asked for here the way
// the user asks for it.

// aPaneOnADroppedMachine is a file pane pointed at a directory the test
// owns, on a machine whose connection has gone.
func aPaneOnADroppedMachine(t *testing.T) (a *testApp, dir string, pane *files.Pane) {
	t.Helper()
	a, s, host := aConnectedWindow(t, 100, 30)
	pane = openFilesFromThePlus(t, a, host)
	if pane == nil {
		t.Fatal("no file pane opened")
	}
	dir = t.TempDir()
	openAt(t, a, pane, filepath.ToSlash(dir))

	s.CloseClients()
	waitFor(t, a, "the window to see the machine go", func() bool {
		a.reapExited()
		return a.machines.named(host) == nil
	})
	settleAndClearNotices(t, a)
	return a, dir, pane
}

// Making a directory after the connection went opens the machine again
// and makes it.
func TestMakingADirectoryAfterTheDropOpensTheMachineAgain(t *testing.T) {
	a, dir, pane := aPaneOnADroppedMachine(t)

	a.makeDirectory(pane, "made")
	waitFor(t, a, "the directory to be made", func() bool {
		_, err := os.Stat(filepath.Join(dir, "made"))
		return err == nil
	})
	if up := a.root.Modal(); up != nil {
		t.Errorf("a dialog complained about it: %T", up)
	}
}

// Renaming after the connection went opens the machine again. It asks
// twice -- once to see whether the name is taken, once to rename -- so
// it is the operation that would show a reconnect twice if the
// filesystem did not keep what it opened.
func TestRenamingAfterTheDropOpensTheMachineAgain(t *testing.T) {
	a, dir, pane := aPaneOnADroppedMachine(t)
	putFile(t, dir, "was.txt", "hello")

	a.renameTo(pane, "was.txt", "now.txt")
	waitFor(t, a, "the rename to happen", func() bool {
		_, err := os.Stat(filepath.Join(dir, "now.txt"))
		return err == nil
	})
	if up := a.root.Modal(); up != nil {
		t.Errorf("a dialog complained about it: %T", up)
	}
	if _, err := os.Stat(filepath.Join(dir, "was.txt")); err == nil {
		t.Error("the old name is still there")
	}
	// One connection between the two asks, not one each.
	if got := len(reopenersOn(t, a, a.hostOf(pane.FS()))); got != 1 {
		t.Errorf("%d filesystems hold the machine after the rename", got)
	}
}

// Reading a file after the connection went opens the machine again and
// shows it.
func TestReadingAFileAfterTheDropOpensTheMachineAgain(t *testing.T) {
	a, dir, pane := aPaneOnADroppedMachine(t)
	putFile(t, dir, "note.txt", "what it says\n")
	var found bool
	for _, e := range pane.Entries() {
		if e.Name == "note.txt" {
			found = true
		}
	}
	if !found {
		// The pane listed the directory before the file was put there.
		pane.Reload()
		waitFor(t, a, "the pane to list the file", func() bool {
			for _, e := range pane.Entries() {
				if e.Name == "note.txt" {
					return true
				}
			}
			return false
		})
	}
	var e vfs.Entry
	for _, got := range pane.Entries() {
		if got.Name == "note.txt" {
			e = got
		}
	}

	if err := a.readFileFrom(pane, e, false); err != nil {
		t.Fatalf("open the file: %v", err)
	}
	r := onlyReader(t, a)
	waitFor(t, a, "the file to be read", func() bool { return r.Lines() > 0 })
	if err := r.Err(); err != nil {
		t.Errorf("reading it after the drop: %v", err)
	}
	if up := a.root.Modal(); up != nil {
		t.Errorf("a dialog complained about it: %T", up)
	}
}

// Deleting after the connection went opens the machine again, and the
// work is filed under that machine rather than under this one.
func TestDeletingAfterTheDropOpensTheMachineAgain(t *testing.T) {
	a, dir, pane := aPaneOnADroppedMachine(t)
	putFile(t, dir, "gone.txt", "take me")
	host := a.hostOf(pane.FS())

	a.startJob(jobs.Delete, files.Work{
		From: pane, At: filepath.ToSlash(dir), Names: []string{"gone.txt"},
	})
	waitFor(t, a, "the file to go", func() bool {
		a.refreshJobs()
		_, err := os.Stat(filepath.Join(dir, "gone.txt"))
		return err != nil
	})
	if up := a.root.Modal(); up != nil {
		t.Errorf("a dialog complained about it: %T", up)
	}
	// Filed under the machine it happened on. Reading which machine off
	// what is connected would have said Local.
	var filed []string
	for e := range a.jobs {
		filed = append(filed, e.Host)
	}
	for _, at := range filed {
		if at != host {
			t.Errorf("the work is filed under %q, want %q", at, host)
		}
	}
}

// A reader outlives the browser pane it was opened from, so it opens
// the machine again itself.
//
// The browser letting go of a filesystem a reader still holds does not
// close it, and must not leave it unable to open the machine either:
// the reader is still on screen, and asking it to read again is exactly
// what the user does.
func TestAReaderWhoseBrowserPaneWentStillOpensTheMachine(t *testing.T) {
	a, s, host := aConnectedWindow(t, 100, 30)
	pane := openFilesFromThePlus(t, a, host)
	if pane == nil {
		t.Fatal("no file pane opened")
	}
	dir := t.TempDir()
	putFile(t, dir, "note.txt", "what it says\n")
	openAt(t, a, pane, filepath.ToSlash(dir))
	var e vfs.Entry
	for _, got := range pane.Entries() {
		if got.Name == "note.txt" {
			e = got
		}
	}
	if err := a.readFileFrom(pane, e, false); err != nil {
		t.Fatalf("open the file: %v", err)
	}
	open := onlyReader(t, a)
	waitFor(t, a, "the file to be read", func() bool { return open.Lines() > 0 })

	// The browser pane goes, the reader stays, and then the machine
	// drops.
	if err := a.closePane(pane); err != nil {
		t.Fatalf("close the browser pane: %v", err)
	}
	s.CloseClients()
	waitFor(t, a, "the window to see the machine go", func() bool {
		a.reapExited()
		return a.machines.named(host) == nil
	})
	settleAndClearNotices(t, a)

	// Reading again opens the machine, the way the browser's read does.
	putFile(t, dir, "note.txt", "one\ntwo\nthree\n")
	open.Open()
	waitFor(t, a, "the reader to read again", func() bool { return open.Lines() == 3 })
	if err := open.Err(); err != nil {
		t.Errorf("reading again after the drop: %v", err)
	}
	if up := a.root.Modal(); up != nil {
		t.Errorf("a dialog complained about it: %T", up)
	}
}
