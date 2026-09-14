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
	"github.com/marrasen/gridterm/ui/files"
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

// onlyBrowser returns the window's file manager with two panes in it,
// pointed at directories the test owns.
func onlyBrowser(t *testing.T, a *testApp) (*browser, string, string) {
	t.Helper()
	for i := 0; i < 2; i++ {
		if err := a.openFilesHere(); err != nil {
			t.Fatalf("openFilesHere: %v", err)
		}
	}
	b := a.files
	if b == nil {
		t.Fatal("the window has no file manager")
	}
	panes := b.view.Panes()
	if len(panes) != 2 {
		t.Fatalf("the manager holds %d panes, want 2", len(panes))
	}
	leftPane, rightPane := panes[0], panes[1]
	// It opens on the user's own directory by itself, on a goroutine of
	// its own, so that has to land before the test sends it anywhere
	// else.
	waitFor(t, a, "the browser to open somewhere", func() bool {
		return leftPane.At() != "" && rightPane.At() != "" &&
			!leftPane.Busy() && !rightPane.Busy()
	})

	// The keys go on the first pane: a copy goes from the pane with the
	// keys to the next one, and a test that acts on the first wants
	// them there.
	a.focus(leftPane)

	left, right := t.TempDir(), t.TempDir()
	leftPane.Open(left)
	rightPane.Open(right)
	waitFor(t, a, "both panes to be read", func() bool {
		return leftPane.At() == left && rightPane.At() == right &&
			!leftPane.Busy() && !rightPane.Busy()
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

// Closing the file manager takes it and its rows away.
func TestClosingABrowserPane(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)

	b, _, _ := onlyBrowser(t, a)
	if err := a.closePane(b.view); err != nil {
		t.Fatalf("closePane: %v", err)
	}
	if a.files != nil {
		t.Fatal("the window still holds a file manager")
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
	leftPane := b.view.Panes()[0]
	leftPane.Reload()
	waitFor(t, a, "the listing", func() bool { return !leftPane.Busy() })

	// Down onto the file, then copy.
	tap(t, b.view, input.KeyDown)
	// Copy picks the names out; pasting in the next pane is what starts
	// the job.
	tap(t, b.view, input.KeyF5)
	tap(t, b.view, input.KeyTab)
	tap(t, b.view, input.KeyF7)

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
	leftPane := b.view.Panes()[0]
	leftPane.Reload()
	waitFor(t, a, "the listing", func() bool { return !leftPane.Busy() })

	tap(t, b.view, input.KeyDown)
	// Copy picks the names out; pasting in the next pane is what starts
	// the job.
	tap(t, b.view, input.KeyF5)
	tap(t, b.view, input.KeyTab)
	tap(t, b.view, input.KeyF7)

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
	leftPane := b.view.Panes()[0]
	leftPane.Reload()
	waitFor(t, a, "the listing", func() bool { return !leftPane.Busy() })

	tap(t, b.view, input.KeyDown)
	// Copy picks the names out; pasting in the next pane is what starts
	// the job.
	tap(t, b.view, input.KeyF5)
	tap(t, b.view, input.KeyTab)
	tap(t, b.view, input.KeyF7)
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
	leftPane := b.view.Panes()[0]
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

	if err := a.openFilesOn(conns.Local); err != nil {
		t.Fatalf("a pane here: %v", err)
	}
	if err := a.openFilesOn(host); err != nil {
		t.Fatalf("a pane on %s: %v", host, err)
	}
	b := a.files
	if b == nil {
		t.Fatal("the window has no file manager")
	}
	panes := b.view.Panes()
	if len(panes) != 2 {
		t.Fatalf("the manager holds %d panes, want 2", len(panes))
	}
	left, right := panes[0], panes[1]
	if left.FS().Name() != "Local" {
		t.Errorf("the first pane is on %q", left.FS().Name())
	}
	if right.FS().Name() != host {
		t.Errorf("the second pane is on %q", right.FS().Name())
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

	// And closing the machine takes its pane with it, leaving the one
	// on this machine alone.
	if err := a.dropMachine(host); err != nil {
		t.Fatalf("dropMachine: %v", err)
	}
	if a.files == nil {
		t.Fatal("the whole manager went with one machine")
	}
	if got := a.files.view.Panes(); len(got) != 1 || got[0] != left {
		t.Fatalf("the manager holds %d panes, want the one on this machine", len(got))
	}
	var still []string
	for _, group := range a.registry.Groups(time.Now()) {
		for _, row := range group.Rows {
			if row.Kind == conns.Files {
				still = append(still, group.Host)
			}
		}
	}
	if len(still) != 1 || still[0] != conns.Local {
		t.Fatalf("the sidebar shows file rows under %v, want only this machine", still)
	}
}

// A browser cannot be opened on a machine nothing is connected to.
func TestABrowserNeedsAConnection(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)

	if err := a.openFilesOn("nowhere"); err == nil {
		t.Fatal("a pane opened on a machine nothing is connected to")
	}
	if a.files != nil {
		t.Fatal("a file manager was left behind")
	}
}

// Making a directory asks for a name and refuses one that is a path.
func TestMakingADirectory(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)

	b, left, _ := onlyBrowser(t, a)
	tap(t, b.view, input.KeyF9)
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

// filesRows returns which machine each file row on the sidebar is under.
func filesRows(a *testApp) []string {
	var hosts []string
	for _, group := range a.registry.Groups(time.Now()) {
		for _, row := range group.Rows {
			if row.Kind == conns.Files {
				hosts = append(hosts, group.Host)
			}
		}
	}
	return hosts
}

// The file manager takes as many panes as the user asks for, on as many
// machines, each with a row of its own under the machine it is on.
func TestTheFileManagerTakesAsManyPanesAsAsked(t *testing.T) {
	s := sshtest.New(t)
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	host := connectedTo(t, a, s)

	for _, on := range []string{conns.Local, host, conns.Local, host} {
		if err := a.openFilesOn(on); err != nil {
			t.Fatalf("a pane on %q: %v", on, err)
		}
	}
	if got := len(a.files.view.Panes()); got != 4 {
		t.Fatalf("the manager holds %d panes, want 4", got)
	}
	// One manager, not four: they are side by side in the one tab.
	var managers int
	for _, leaf := range ui.Leaves(a.root.Widget()) {
		if _, ok := leaf.(*files.Pane); ok {
			managers++
		}
	}
	if managers != 4 {
		t.Fatalf("the tree holds %d file panes", managers)
	}
	got := filesRows(a)
	if len(got) != 4 {
		t.Fatalf("the sidebar shows %v, want a row for each pane", got)
	}
	var here, there int
	for _, on := range got {
		if on == conns.Local {
			here++
		}
		if on == host {
			there++
		}
	}
	if here != 2 || there != 2 {
		t.Fatalf("the rows sit under %v, want two on each machine", got)
	}

	// The pane just opened has the keys, which is what opening one is
	// for.
	if a.files.view.Here() != a.files.view.Panes()[3] {
		t.Fatal("the keys are not on the pane that was just opened")
	}
	checkTree(t, a)
}

// Closing one pane's row takes that pane and leaves the rest.
func TestClosingOneFilePaneLeavesTheOthers(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)

	for i := 0; i < 3; i++ {
		if err := a.openFilesOn(conns.Local); err != nil {
			t.Fatalf("a pane: %v", err)
		}
	}
	panes := a.files.view.Panes()
	row := a.files.rows[panes[1]]
	if row == nil || row.Close == nil {
		t.Fatal("the middle pane has no row to close")
	}
	if err := row.Close(); err != nil {
		t.Fatalf("closing the row: %v", err)
	}

	left := a.files.view.Panes()
	if len(left) != 2 || left[0] != panes[0] || left[1] != panes[2] {
		t.Fatalf("the manager holds %d panes, want the other two", len(left))
	}
	if a.files.rows[panes[1]] != nil {
		t.Fatal("the closed pane still has a row")
	}
	if got := filesRows(a); len(got) != 2 {
		t.Fatalf("the sidebar shows %v, want two rows", got)
	}
	checkTree(t, a)
}

// The last pane takes the manager with it, and the window carries on.
func TestTheLastFilePaneTakesTheManager(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)

	if err := a.openFilesOn(conns.Local); err != nil {
		t.Fatalf("a pane: %v", err)
	}
	manager := a.files.view
	p := a.files.view.Panes()[0]
	if err := a.closePane(p); err != nil {
		t.Fatalf("closePane: %v", err)
	}
	if a.files != nil {
		t.Fatal("the manager outlived its last pane")
	}
	if len(filesRows(a)) != 0 {
		t.Fatal("a file row was left on the sidebar")
	}
	for _, leaf := range ui.Leaves(a.root.Widget()) {
		if leaf == ui.Widget(manager) {
			t.Fatal("the manager is still in the tree with no panes in it")
		}
	}
	if a.quit.Load() {
		t.Fatal("the window closed, and there is still a terminal open")
	}
	checkTree(t, a)
}

// The one pane of a manager just opened has the keys and knows it.
//
// The manager goes into the tree empty and is given the keys there, and
// the pane arrives after. A pane never told draws no selected row, so
// the arrows move something nobody can see.
func TestTheFirstFilePaneHasTheKeys(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)

	if err := a.openFilesOn(conns.Local); err != nil {
		t.Fatalf("a pane: %v", err)
	}
	p := a.files.view.Panes()[0]
	if a.files.view.Here() != p {
		t.Fatal("the manager says the keys are somewhere else")
	}
	if !p.Focused() {
		t.Fatal("the only pane does not know it has the keys")
	}
	checkTree(t, a)
}

// A machine dropping takes its pane and leaves the keys where the user
// had them.
func TestAMachineGoingLeavesTheKeysWhereTheyWere(t *testing.T) {
	s := sshtest.New(t)
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	host := connectedTo(t, a, s)

	// Here, there, here: the keys end on the last one.
	for _, on := range []string{conns.Local, host, conns.Local} {
		if err := a.openFilesOn(on); err != nil {
			t.Fatalf("a pane on %q: %v", on, err)
		}
	}
	was := a.files.view.Here()

	if err := a.dropMachine(host); err != nil {
		t.Fatalf("dropMachine: %v", err)
	}
	if a.files == nil {
		t.Fatal("the whole manager went with one machine")
	}
	if got := a.files.view.Here(); got != was {
		t.Fatalf("the keys moved to %q, want the pane the user was in", got.At())
	}
	var focused int
	for _, p := range a.files.view.Panes() {
		if p.Focused() {
			focused++
		}
	}
	if focused != 1 {
		t.Fatalf("%d panes believe they have the keys", focused)
	}
	checkTree(t, a)
}
