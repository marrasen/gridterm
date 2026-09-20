package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marrasen/gridterm/input"
	"github.com/marrasen/gridterm/ui"
)

// ctrlPressOn presses the left button with ctrl held on a cell of a
// widget, which is what follows a link.
func ctrlPressOn(t *testing.T, a *testApp, w ui.Widget, col, row int) bool {
	t.Helper()
	area, ok := a.paneArea(w)
	if !ok {
		t.Fatalf("%T is not on screen", w)
	}
	took, err := a.routeMouse(input.MouseEvent{
		Kind: input.MousePress, Button: input.MouseLeft,
		Col: area.X + col, Row: area.Y + row, Mods: input.ModCtrl,
	})
	if err != nil {
		t.Fatalf("the press failed: %v", err)
	}
	return took
}

// A whole path a pane printed opens when it is clicked.
func TestClickingAWholePathOpensIt(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	withDialogs(t, a)
	a.commands()
	pane := onlyPaneOn(t, a)
	at := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(at, []byte("one\ntwo\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	a.shells[0].out <- []byte("see " + at + " for it")
	waitFor(t, a, "the pane to say it", func() bool {
		return strings.Contains(paneText(pane), "notes.txt for it")
	})
	a.relayout()
	if got := pane.LinkAt(4, 0); got != at {
		t.Fatalf("the pane says the path under the pointer is %q, want %q", got, at)
	}

	if !ctrlPressOn(t, a, pane, 4, 0) {
		t.Fatal("the press was not taken")
	}

	r := onlyReader(t, a)
	if r.Name() != "notes.txt" {
		t.Errorf("the viewer is on %q", r.Name())
	}
}

// A path relative to where the shell is opens when the shell has said
// where that is. A compiler names a file this way.
func TestClickingARelativePathOpensIt(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	withDialogs(t, a)
	a.commands()
	pane := onlyPaneOn(t, a)
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "vt"), 0o700); err != nil {
		t.Fatalf("make the folder: %v", err)
	}
	at := filepath.Join(dir, "vt", "image.go")
	if err := os.WriteFile(at, []byte("package vt\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	// OSC 7 is the shell saying where it is, which is the only way a
	// relative name can be resolved.
	a.shells[0].out <- []byte("\x1b]7;file://localhost/" + filepath.ToSlash(dir) + "\x07")
	a.shells[0].out <- []byte("vt/image.go:42:8: undefined: x")
	waitFor(t, a, "the pane to say it", func() bool {
		return strings.Contains(paneText(pane), "undefined: x")
	})
	a.relayout()
	if got := pane.LinkAt(0, 0); got != at {
		t.Fatalf("the pane says the path under the pointer is %q, want %q", got, at)
	}

	if !ctrlPressOn(t, a, pane, 0, 0) {
		t.Fatal("the press was not taken")
	}

	r := onlyReader(t, a)
	if r.Name() != "image.go" {
		t.Errorf("the viewer is on %q", r.Name())
	}
}

// Without the shell saying where it is, a relative name resolves
// against nothing and is not a link.
func TestARelativePathWithNoWorkingDirectoryIsNotALink(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	withDialogs(t, a)
	a.commands()
	pane := onlyPaneOn(t, a)

	a.shells[0].out <- []byte("openpath.go:42:8: undefined: x")
	waitFor(t, a, "the pane to say it", func() bool {
		return strings.Contains(paneText(pane), "undefined: x")
	})
	a.relayout()

	if got := pane.LinkAt(0, 0); got != "" {
		t.Errorf("a relative name resolved to %q with nowhere to resolve it", got)
	}
}
