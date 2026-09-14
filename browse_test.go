package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/input"
	"github.com/marrasen/gridterm/internal/sshtest"
	"github.com/marrasen/gridterm/ui"
)

// putFile writes a file for a browser test to find.
func putFile(t *testing.T, at, name, body string) {
	t.Helper()
	path := filepath.Join(at, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

// onlyBrowser returns the window's one browser, with both panes pointed
// at directories the test owns.
func onlyBrowser(t *testing.T, a *testApp) (*browser, string, string) {
	t.Helper()
	if err := a.openFilesHere(); err != nil {
		t.Fatalf("openFilesHere: %v", err)
	}
	if len(a.browsers) != 1 {
		t.Fatalf("the window holds %d browsers", len(a.browsers))
	}
	var b *browser
	for _, have := range a.browsers {
		b = have
	}
	left, right := t.TempDir(), t.TempDir()
	leftPane, rightPane := b.view.Panes()
	leftPane.Open(left)
	rightPane.Open(right)
	waitFor(t, a, "both panes to be read", func() bool {
		return !leftPane.Busy() && !rightPane.Busy()
	})
	return b, left, right
}

// A browser opens in a tab, with a row on the panel for each side.
func TestOpenABrowser(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)

	b, left, _ := onlyBrowser(t, a)
	checkTree(t, a)

	// Two rows, both under this machine, both saying where they are.
	var rows []*conns.Entry
	for _, group := range a.registry.Groups(time.Now()) {
		for _, row := range group.Rows {
			if row.Kind == conns.Files {
				rows = append(rows, row.Entry)
			}
		}
	}
	if len(rows) != 2 {
		t.Fatalf("the panel shows %d browser rows, want one per side", len(rows))
	}
	a.refreshPanel(time.Now())
	if rows[0].Label != left {
		t.Errorf("the first row says %q, want where it is", rows[0].Label)
	}
	// And the row can put the pane in front of the user.
	if rows[0].Reveal == nil {
		t.Fatal("a browser row cannot be revealed")
	}
	rows[0].Reveal()
	if !b.view.HasFocus() {
		t.Error("revealing a browser row did not put the keys in it")
	}
}

// Closing the pane takes the browser and its rows away.
func TestClosingABrowserPane(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)

	b, _, _ := onlyBrowser(t, a)
	if err := a.closePane(b.view); err != nil {
		t.Fatalf("closePane: %v", err)
	}
	if len(a.browsers) != 0 {
		t.Fatalf("the window still holds %d browsers", len(a.browsers))
	}
	for _, group := range a.registry.Groups(time.Now()) {
		for _, row := range group.Rows {
			if row.Kind == conns.Files {
				t.Fatal("a browser row was left on the panel")
			}
		}
	}
	checkTree(t, a)
}

// F5 copies what is picked out to the other pane, in the background,
// with a row on the panel saying how it is going.
func TestCopyingBetweenPanes(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)

	b, left, right := onlyBrowser(t, a)
	putFile(t, left, "one.txt", "the body")
	leftPane, _ := b.view.Panes()
	leftPane.Reload()
	waitFor(t, a, "the listing", func() bool { return !leftPane.Busy() })

	// Down onto the file, then copy.
	tap(t, b.view, input.KeyDown)
	tap(t, b.view, input.KeyF5)

	if len(a.jobs) != 1 {
		t.Fatalf("the window holds %d jobs, want the copy", len(a.jobs))
	}
	waitFor(t, a, "the copy to finish", func() bool {
		a.refreshJobs()
		return len(a.jobs) == 0
	})

	got, err := os.ReadFile(filepath.Join(right, "one.txt"))
	if err != nil {
		t.Fatalf("read the copy: %v", err)
	}
	if string(got) != "the body" {
		t.Fatalf("the copy holds %q", got)
	}
}

// A name that is already there asks, and the answer is obeyed.
func TestCopyingAsksBeforeReplacing(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)

	b, left, right := onlyBrowser(t, a)
	putFile(t, left, "one.txt", "the new one")
	putFile(t, right, "one.txt", "the old one")
	leftPane, _ := b.view.Panes()
	leftPane.Reload()
	waitFor(t, a, "the listing", func() bool { return !leftPane.Busy() })

	tap(t, b.view, input.KeyDown)
	tap(t, b.view, input.KeyF5)

	f := waitForDialogPrefix(t, a, "Replace")
	if !strings.Contains(strings.Join(f.Lines, " "), "file") {
		t.Errorf("the question says %q, want what is there", f.Lines)
	}
	pressButton(t, a, f, "Skip")

	waitFor(t, a, "the copy to finish", func() bool {
		a.refreshJobs()
		return len(a.jobs) == 0
	})
	got, err := os.ReadFile(filepath.Join(right, "one.txt"))
	if err != nil {
		t.Fatalf("read it back: %v", err)
	}
	if string(got) != "the old one" {
		t.Fatalf("skipping wrote %q anyway", got)
	}
}

// A question that goes away without being answered stops the job rather
// than leaving it waiting for an answer that cannot come.
func TestAQuestionThatIsDismissedStopsTheJob(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)

	b, left, right := onlyBrowser(t, a)
	putFile(t, left, "one.txt", "the new one")
	putFile(t, right, "one.txt", "the old one")
	leftPane, _ := b.view.Panes()
	leftPane.Reload()
	waitFor(t, a, "the listing", func() bool { return !leftPane.Busy() })

	tap(t, b.view, input.KeyDown)
	tap(t, b.view, input.KeyF5)
	f := waitForDialogPrefix(t, a, "Replace")

	// Escape, which is how a dialog goes away without an answer.
	a.root.HandleKey(press(input.KeyEscape, 0))
	a.pump.run()
	if a.root.Modal() == f {
		t.Fatal("the dialog is still there")
	}

	waitFor(t, a, "the job to stop", func() bool {
		a.refreshJobs()
		return len(a.jobs) == 0
	})
	got, err := os.ReadFile(filepath.Join(right, "one.txt"))
	if err != nil {
		t.Fatalf("read it back: %v", err)
	}
	if string(got) != "the old one" {
		t.Fatalf("it wrote %q on the way out", got)
	}
}

// Deleting asks first, because nothing puts it back.
func TestDeletingAsksFirst(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)

	b, left, _ := onlyBrowser(t, a)
	putFile(t, left, "one.txt", "the body")
	leftPane, _ := b.view.Panes()
	leftPane.Reload()
	waitFor(t, a, "the listing", func() bool { return !leftPane.Busy() })

	tap(t, b.view, input.KeyDown)
	tap(t, b.view, input.KeyF8)

	f := waitForDialogPrefix(t, a, "Delete")
	if len(a.jobs) != 0 {
		t.Fatal("it started deleting before the question was answered")
	}
	pressButton(t, a, f, "Keep them")
	a.pump.run()
	if len(a.jobs) != 0 {
		t.Fatalf("%d jobs after saying no", len(a.jobs))
	}
	if _, err := os.Stat(filepath.Join(left, "one.txt")); err != nil {
		t.Fatalf("it went anyway: %v", err)
	}
}

// A browser on a machine reached over a connection is one pane here and
// one there.
func TestABrowserOnAnotherMachine(t *testing.T) {
	s := sshtest.New(t)
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	host := connectedTo(t, a, s)

	if err := a.openBrowser(conns.Local, host); err != nil {
		t.Fatalf("openBrowser: %v", err)
	}
	var b *browser
	for _, have := range a.browsers {
		b = have
	}
	left, right := b.view.Panes()
	if left.FS().Name() != "Local" {
		t.Errorf("the left pane is on %q", left.FS().Name())
	}
	if right.FS().Name() != host {
		t.Errorf("the right pane is on %q", right.FS().Name())
	}
	// One row under each machine.
	var hosts []string
	for _, group := range a.registry.Groups(time.Now()) {
		for _, row := range group.Rows {
			if row.Kind == conns.Files {
				hosts = append(hosts, group.Host)
			}
		}
	}
	if len(hosts) != 2 || !has(hosts, conns.Local) || !has(hosts, host) {
		t.Fatalf("the browser is shown under %v, want one row on each machine", hosts)
	}

	// And closing the machine takes the browser with it.
	if err := a.dropMachine(host); err != nil {
		t.Fatalf("dropMachine: %v", err)
	}
	if len(a.browsers) != 0 {
		t.Fatalf("the browser outlived the connection it was half on")
	}
}

// A browser cannot be opened on a machine nothing is connected to.
func TestABrowserNeedsAConnection(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)

	if err := a.openBrowser(conns.Local, "nowhere"); err == nil {
		t.Fatal("a browser opened on a machine nothing is connected to")
	}
	if len(a.browsers) != 0 {
		t.Fatalf("%d browsers were left behind", len(a.browsers))
	}
}

// Making a directory asks for a name and refuses one that is a path.
func TestMakingADirectory(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)

	b, left, _ := onlyBrowser(t, a)
	tap(t, b.view, input.KeyF7)
	f := waitForDialogPrefix(t, a, "New directory")

	f.Fields()[0].SetText("in/out")
	pressButton(t, a, f, "Make it")
	if a.root.Modal() != f {
		t.Fatal("the dialog closed on a name that is a path")
	}
	if f.Error() == nil {
		t.Fatal("nothing said why it was refused")
	}

	f.Fields()[0].SetText("made")
	pressButton(t, a, f, "Make it")
	waitFor(t, a, "the directory to be made", func() bool {
		_, err := os.Stat(filepath.Join(left, "made"))
		return err == nil
	})
}

// tap sends one key straight to a widget, without going through the
// window: a browser inside a tab is reached through a tree the test does
// not have to walk.
func tap(t *testing.T, w ui.KeyHandler, key input.Key) {
	t.Helper()
	if _, err := w.HandleKey(press(key, 0)); err != nil {
		t.Fatalf("key %v: %v", key, err)
	}
}
