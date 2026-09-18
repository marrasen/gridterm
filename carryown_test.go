package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marrasen/gridterm/conf"
	"github.com/marrasen/gridterm/settings"
	"github.com/marrasen/gridterm/ui"
)

// A copy that is offered files of its own is given the ones it is
// reading now, so it starts with what the user has rather than with
// nothing.
func TestCarryingOwnFilesCopiesWhatTheWindowIsReading(t *testing.T) {
	withHome(t)
	a := newTestApp(t, 80, 24)
	dir, err := settings.Dir()
	if err != nil {
		t.Fatalf("where the files go: %v", err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("make it: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, settings.File),
		[]byte(`{"version":1}`), 0o600); err != nil {
		t.Fatalf("write the settings: %v", err)
	}

	said, err := a.carryOwnFiles()
	if err != nil {
		t.Fatalf("carry its own files: %v", err)
	}

	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("where this is: %v", err)
	}
	beside := conf.Beside(exe)
	t.Cleanup(func() { _ = os.RemoveAll(beside) })
	raw, err := os.ReadFile(filepath.Join(beside, settings.File))
	if err != nil {
		t.Fatalf("the settings were not copied: %v", err)
	}
	if string(raw) != `{"version":1}` {
		t.Errorf("the copy holds %q, want what the window was reading", raw)
	}
	if !strings.Contains(said, beside) {
		t.Errorf("it said %q, want it to name where the files went", said)
	}
	if !strings.Contains(said, "again") {
		t.Errorf("it said %q, want it to say the window has to be started again", said)
	}
}

// A file that is not there yet is not a failure. The window writes each
// of them the first time it has something to say, so a fresh copy has
// almost none.
func TestCarryingOwnFilesWithNothingWrittenYet(t *testing.T) {
	withHome(t)
	a := newTestApp(t, 80, 24)

	if _, err := a.carryOwnFiles(); err != nil {
		t.Fatalf("carry its own files with nothing to copy: %v", err)
	}

	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("where this is: %v", err)
	}
	beside := conf.Beside(exe)
	t.Cleanup(func() { _ = os.RemoveAll(beside) })
	if _, err := os.Stat(beside); err != nil {
		t.Errorf("the directory was not made: %v", err)
	}
}

// Nothing is written over. A directory that already holds files is one
// somebody set up, and copying into it would take what is there away
// without asking.
func TestCarryingOwnFilesRefusesToWriteOverOne(t *testing.T) {
	withHome(t)
	a := newTestApp(t, 80, 24)
	dir, err := settings.Dir()
	if err != nil {
		t.Fatalf("where the files go: %v", err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("make it: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, settings.File),
		[]byte(`{"version":1}`), 0o600); err != nil {
		t.Fatalf("write the settings: %v", err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("where this is: %v", err)
	}
	beside := conf.Beside(exe)
	t.Cleanup(func() { _ = os.RemoveAll(beside) })
	if err := os.MkdirAll(beside, 0o700); err != nil {
		t.Fatalf("make the directory beside: %v", err)
	}
	if err := os.WriteFile(filepath.Join(beside, settings.File),
		[]byte("mine"), 0o600); err != nil {
		t.Fatalf("write one there already: %v", err)
	}

	if _, err := a.carryOwnFiles(); err == nil {
		t.Fatal("it wrote over a file that was already there")
	}

	raw, err := os.ReadFile(filepath.Join(beside, settings.File))
	if err != nil {
		t.Fatalf("read it back: %v", err)
	}
	if string(raw) != "mine" {
		t.Errorf("the file now holds %q, want the %q it held", raw, "mine")
	}
}

// The notice offers the button while this copy is not carrying its own
// files, because that is when there is something to do.
func TestTheFilesNoticeOffersToDoIt(t *testing.T) {
	withHome(t)
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)

	if err := a.showWhereFiles(); err != nil {
		t.Fatalf("show where the files are: %v", err)
	}
	a.pump.run()

	n, up := a.root.Modal().(*ui.Notice)
	if !up {
		t.Fatalf("it opened %T, want the notice", a.root.Modal())
	}
	if n.Action.Title != carryOwnTitle {
		t.Errorf("the notice offers %q, want %q", n.Action.Title, carryOwnTitle)
	}
	if n.Action.Do == nil {
		t.Error("the button does nothing")
	}
}

// It stops offering once the directory is there.
//
// Where this window reads from was decided when it opened and does not
// move, so it goes on saying it is not carrying its own files until it
// is started again. That is no reason to offer to make a directory that
// now exists, and pressing the button again would only fail on the
// files already in it.
func TestTheFilesNoticeStopsOfferingOnceTheDirectoryIsThere(t *testing.T) {
	withHome(t)
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("where this is: %v", err)
	}
	beside := conf.Beside(exe)
	t.Cleanup(func() { _ = os.RemoveAll(beside) })
	if err := os.MkdirAll(beside, 0o700); err != nil {
		t.Fatalf("make the directory beside: %v", err)
	}

	if err := a.showWhereFiles(); err != nil {
		t.Fatalf("show where the files are: %v", err)
	}
	a.pump.run()

	n, up := a.root.Modal().(*ui.Notice)
	if !up {
		t.Fatalf("it opened %T, want the notice", a.root.Modal())
	}
	if n.Action.Title != "" {
		t.Errorf("it still offers %q", n.Action.Title)
	}
}
