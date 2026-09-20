package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marrasen/gridterm/shells"
	"github.com/marrasen/gridterm/ui"
)

// saysWhereItIs makes a pane report a working directory the way a
// shell set up by gridterm does.
func saysWhereItIs(t *testing.T, a *testApp, pane paneLike, dir string) {
	t.Helper()
	a.shells[0].out <- []byte("\x1b]9;9;" + dir + "\x07")
	waitFor(t, a, "the pane to say where it is", func() bool {
		at, _ := pane.Dir()
		return at == dir
	})
}

// paneLike is the bit of a pane these tests ask about.
type paneLike interface{ Dir() (string, string) }

// A file dropped on a pane whose shell has said where it is goes
// there, and nothing is typed.
func TestADroppedFileGoesWhereTheShellIs(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	withDialogs(t, a)
	a.commands()
	pane := onlyPaneOn(t, a)
	into := t.TempDir()
	saysWhereItIs(t, a, pane, into)

	from := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(from, []byte("one\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	was := a.shells[0].sentText()

	if err := a.dropOnPane(pane, []string{from}); err != nil {
		t.Fatalf("drop it: %v", err)
	}
	waitFor(t, a, "the file to arrive", func() bool {
		_, err := os.Stat(filepath.Join(into, "notes.txt"))
		return err == nil
	})

	if got := a.shells[0].sentText(); got != was {
		t.Errorf("%q was typed into the pane", strings.TrimPrefix(got, was))
	}
}

// And the window says so, because a file that arrives silently is a
// file the user cannot tell arrived.
func TestTheWindowSaysADroppedFileArrived(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	withDialogs(t, a)
	a.commands()
	pane := onlyPaneOn(t, a)
	into := t.TempDir()
	saysWhereItIs(t, a, pane, into)
	from := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(from, []byte("one\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	if err := a.dropOnPane(pane, []string{from}); err != nil {
		t.Fatalf("drop it: %v", err)
	}

	waitFor(t, a, "the window to say the file arrived", func() bool {
		n, up := a.root.Modal().(*ui.Notice)
		return up && strings.Contains(n.Title, "notes.txt")
	})
}

// A shell that has not said where it is leaves nowhere to put the
// file, so the path is typed the way it always was.
func TestWithNoWorkingDirectoryThePathIsTyped(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	withDialogs(t, a)
	a.commands()
	pane := onlyPaneOn(t, a)
	from := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(from, []byte("one\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	if err := a.dropOnPane(pane, []string{from}); err != nil {
		t.Fatalf("drop it: %v", err)
	}

	waitFor(t, a, "the path to reach the shell", func() bool {
		return strings.Contains(a.shells[0].sentText(), "notes.txt")
	})
}

// A file already in that directory is left where it is. Copying it
// onto itself would leave nothing behind.
func TestAFileAlreadyThereIsLeftAlone(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	withDialogs(t, a)
	a.commands()
	pane := onlyPaneOn(t, a)
	into := t.TempDir()
	saysWhereItIs(t, a, pane, into)
	at := filepath.Join(into, "notes.txt")
	if err := os.WriteFile(at, []byte("one\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	if err := a.dropOnPane(pane, []string{at}); err != nil {
		t.Fatalf("drop it: %v", err)
	}
	a.pump.run()

	got, err := os.ReadFile(at)
	if err != nil {
		t.Fatalf("read it back: %v", err)
	}
	if string(got) != "one\n" {
		t.Errorf("the file reads %q, want it untouched", got)
	}
}

// A pane in WSL says a path inside the distribution, and this machine
// reaches those on a share of its own.
func TestAPaneInWSLPutsTheFileOnTheShare(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	withDialogs(t, a)
	a.commands()
	// The shells this machine has, found the way the window finds them:
	// which shell a pane runs is read back off that list.
	a.registerShells(testShells())
	sh, ok := shells.Lookup(testShells(), "wsl:Ubuntu")
	if !ok {
		t.Fatal("no WSL test shell")
	}
	if err := a.openPaneOn(sh); err != nil {
		t.Fatalf("open a pane on WSL: %v", err)
	}
	pane := newestPane(t, a)
	last := a.shells[len(a.shells)-1]
	last.out <- []byte("\x1b]7;file://ubuntu/home/marcus\x07")
	waitFor(t, a, "the pane to say where it is", func() bool {
		at, _ := pane.Dir()
		return at == "/home/marcus"
	})

	into, ok := a.droppedInto(pane, a.paneEnd(pane))

	if !ok {
		t.Fatal("a file dropped on a WSL pane has nowhere to go")
	}
	if want := `\\wsl.localhost\Ubuntu\home\marcus`; into != want {
		t.Errorf("it would go to %q, want %q", into, want)
	}
}
