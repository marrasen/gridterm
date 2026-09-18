package main

import (
	"errors"
	"fmt"
	"image/color"
	"io"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/input"
	"github.com/marrasen/gridterm/internal/sshtest"
	"github.com/marrasen/gridterm/jobs"
	"github.com/marrasen/gridterm/meter"
	"github.com/marrasen/gridterm/remote"
	"github.com/marrasen/gridterm/ui"
	"github.com/marrasen/gridterm/ui/files"
	"github.com/marrasen/gridterm/ui/term"
	"github.com/marrasen/gridterm/vfs"
)

// openFilesFromThePlus opens a file pane on a machine the way a user
// does: the plus on its row, and the line that browses.
func openFilesFromThePlus(t *testing.T, a *testApp, host string) *files.Pane {
	t.Helper()
	was := make(map[*files.Pane]bool)
	if a.files != nil {
		for _, p := range a.files.view.Panes() {
			was[p] = true
		}
	}
	chooseMenuItem(t, clickPlus(t, a, host), "conn.files")
	var opened *files.Pane
	waitFor(t, a, "a file pane on "+host, func() bool {
		if a.files == nil {
			return false
		}
		for _, p := range a.files.view.Panes() {
			if !was[p] {
				opened = p
				return true
			}
		}
		return false
	})
	return opened
}

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
	for range 2 {
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

	f := awaitModal(t, a, "a dialog whose title starts with Replace", byTitlePrefix[*ui.Form]("Replace"))
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
	f := awaitModal(t, a, "a dialog whose title starts with Replace", byTitlePrefix[*ui.Form]("Replace"))

	// Escape, which is how a dialog goes away without an answer.
	sendKey(t, a, press(input.KeyEscape, 0))
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

	f := awaitModal(t, a, "a dialog whose title starts with Delete", byTitlePrefix[*ui.Form]("Delete"))
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

	openFilesFromThePlus(t, a, conns.Local)
	openFilesFromThePlus(t, a, host)
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
	f := awaitModal(t, a, "a dialog whose title starts with New directory", byTitlePrefix[*ui.Form]("New directory"))

	typeIntoField(t, a, f, "Name", "in/out")
	pressButton(t, a, f, "Make it")
	if a.root.Modal() != f {
		t.Fatal("the dialog closed on a name that is a path")
	}
	if f.Error() == nil {
		t.Fatal("nothing said why it was refused")
	}

	retypeField(t, a, f, "Name", "made")
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

	var last *files.Pane
	for _, on := range []string{conns.Local, host, conns.Local, host} {
		last = openFilesFromThePlus(t, a, on)
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
	if a.files.view.Here() != last {
		t.Fatal("the keys are not on the pane that was just opened")
	}
	checkTree(t, a)
}

// Closing one pane's row takes that pane and leaves the rest.
func TestClosingOneFilePaneLeavesTheOthers(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)

	for range 3 {
		openFilesFromThePlus(t, a, conns.Local)
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

	openFilesFromThePlus(t, a, conns.Local)
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

	openFilesFromThePlus(t, a, conns.Local)
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
		openFilesFromThePlus(t, a, on)
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

// A file pane can be sent anywhere by typing the path, which is the
// only way to reach another drive: going up from one leads nowhere.
func TestGoToSendsAFilePaneAnywhere(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	openFilesFromThePlus(t, a, conns.Local)
	p, ok := ui.FocusedLeaf(a.root.Widget()).(*files.Pane)
	if !ok {
		t.Fatal("the keys are not on a file pane")
	}
	where := t.TempDir()

	if err := a.openGoTo(); err != nil {
		t.Fatalf("go to: %v", err)
	}
	f := awaitModal[*ui.Form](t, a, "a dialog", nil)
	typeIntoField(t, a, f, "Path", where)
	pressButton(t, a, f, "Go")
	a.pump.run()

	waitFor(t, a, "the pane to go there", func() bool { return p.At() == where })

	// And the places the filesystem starts from are offered, so a drive
	// is one key away rather than a letter to remember.
	if err := a.openGoTo(); err != nil {
		t.Fatalf("go to again: %v", err)
	}
	f = awaitModal[*ui.Form](t, a, "a dialog", nil)
	if len(f.Field("Path").Options) < 2 {
		t.Errorf("it offers %v, want where it is now and the roots", f.Field("Path").Options)
	}
}

// A path that is not there keeps the go-to dialog open and says why in
// it, so the typo can be fixed and tried again in the same dialog.
func TestGoToStaysOpenWhenThePathIsNotThere(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	openFilesFromThePlus(t, a, conns.Local)
	p, ok := ui.FocusedLeaf(a.root.Widget()).(*files.Pane)
	if !ok {
		t.Fatal("the keys are not on a file pane")
	}
	// The pane reads through the pump, so where it lands is not known
	// until it has.
	waitFor(t, a, "the pane to land somewhere", func() bool { return p.At() != "" })
	was := p.At()

	if err := a.openGoTo(); err != nil {
		t.Fatalf("go to: %v", err)
	}
	f := awaitModal(t, a, "the go-to dialog", byTitle[*ui.Form]("Go to"))
	nowhere := filepath.Join(t.TempDir(), "nowhere-at-all")
	retypeField(t, a, f, "Path", nowhere)
	pressButton(t, a, f, "Go")

	waitFor(t, a, "the dialog to say why", func() bool { return f.ErrorText() != "" })
	// The same dialog, and the only one: a second dialog on top is what
	// the user had to dismiss before they could try again.
	if got := a.root.Modal(); got != f {
		t.Fatalf("the dialog on top is %T, want the go-to dialog", got)
	}
	if n := len(a.modals); n != 1 {
		t.Errorf("%d dialogs are open, want only the go-to dialog", n)
	}
	// With what was typed still in it.
	if got := f.Field("Path").Text(); got != nowhere {
		t.Errorf("the field holds %q, want what was typed", got)
	}
	if p.At() != was {
		t.Errorf("the pane moved to %q, want it to stay in %q", p.At(), was)
	}

	// And a path that is there closes it.
	retypeField(t, a, f, "Path", was)
	pressButton(t, a, f, "Go")
	waitFor(t, a, "the dialog to go", func() bool { return a.root.Modal() == nil })
}

// Cancelling the go-to dialog while the read is still out does not lose
// the reason. It arrives the way every other one does.
func TestGoToCancelledWhileTheReadIsOutStillSaysWhy(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	openFilesFromThePlus(t, a, conns.Local)
	p, ok := ui.FocusedLeaf(a.root.Widget()).(*files.Pane)
	if !ok {
		t.Fatal("the keys are not on a file pane")
	}
	waitFor(t, a, "the pane to land somewhere", func() bool { return p.At() != "" })

	// Held rather than answered, so Cancel comes first.
	var held func([]vfs.Entry, error)
	p.Read = func(_ vfs.FS, _ string, then func([]vfs.Entry, error)) { held = then }

	if err := a.openGoTo(); err != nil {
		t.Fatalf("go to: %v", err)
	}
	f := awaitModal(t, a, "the go-to dialog", byTitle[*ui.Form]("Go to"))
	retypeField(t, a, f, "Path", filepath.Join(t.TempDir(), "nowhere-at-all"))
	pressButton(t, a, f, "Go")
	pressButton(t, a, f, "Cancel")

	want := errors.New("the machine went away")
	held(nil, want)

	n := awaitModal[*ui.Notice](t, a, "a notice", nil)
	if n.Title != "Could not read a directory" {
		t.Errorf("the dialog is titled %q", n.Title)
	}
	if !strings.Contains(n.Message(), want.Error()) {
		t.Errorf("the dialog says\n%s\nwant it to hold %q", n.Message(), want)
	}
}

// paneDrawn reads a pane back off a grid of its own, one string per row.
func paneDrawn(p *files.Pane, cols, rows int) []string {
	g := grid.New(cols, rows, color.RGBA{}, color.RGBA{})
	p.Layout(ui.Size{Cols: cols, Rows: rows})
	p.Draw(g.View())
	out := make([]string, 0, rows)
	for y := range rows {
		var b strings.Builder
		for x := range cols {
			if c := g.At(x, y); c.Width != 0 {
				b.WriteRune(c.Rune)
			}
		}
		out = append(out, strings.TrimRight(b.String(), " "))
	}
	return out
}

// noNoticeOpens runs the pump for a moment and fails if a dialog turns
// up while it does.
func noNoticeOpens(t *testing.T, a *testApp, what string) {
	t.Helper()
	for range 100 {
		a.pump.run()
		if got := a.root.Modal(); got != nil {
			t.Fatalf("%s opened %T", what, got)
		}
		time.Sleep(time.Millisecond)
	}
}

// dismissNotice puts the dialog on top away with Escape, the way the
// user does.
func dismissNotice(t *testing.T, a *testApp) {
	t.Helper()
	if _, err := a.root.HandleKey(press(input.KeyEscape, 0)); err != nil {
		t.Fatalf("Escape: %v", err)
	}
	if got := a.root.Modal(); got != nil {
		t.Fatalf("Escape left %T on the stack", got)
	}
}

// A directory that cannot be read leaves the listing that worked on
// screen and says so on one short row. The reason is a dialog of its
// own, shown at once in the pane the user is working in: trimming it
// into the row was losing the part that said what to do.
func TestAFilePaneWithTheKeysShowsWhyAReadFailed(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)

	b, left, _ := onlyBrowser(t, a)
	putFile(t, left, "one.txt", "the body")
	p := b.view.Panes()[0]
	p.Open(left)
	waitFor(t, a, "the listing", func() bool { return !p.Busy() })

	bad := refusedPath(t)
	p.Open(bad)
	waitFor(t, a, "the read to fail", func() bool { return !p.Busy() && p.Err() != nil })

	// The names that worked are still there to act on.
	got := p.Entries()
	if len(got) != 1 || got[0].Name != "one.txt" {
		t.Fatalf("the pane shows %v, want what it had", got)
	}
	// The row says what happened rather than a fragment of why, and
	// offers the rest.
	rows := paneDrawn(p, 40, 10)
	if !strings.Contains(rows[2], "could not be read") {
		t.Fatalf("the pane does not say the read failed:\n%s", strings.Join(rows, "\n"))
	}
	if !strings.Contains(rows[2], "click to see why") {
		t.Errorf("the row does not offer the reason:\n%s", rows[2])
	}
	if strings.Contains(strings.Join(rows, "\n"), p.Err().Error()) {
		t.Error("the whole reason is on the row, so it is being trimmed again")
	}

	// The user asked for that directory, so the reason arrives without
	// their having to ask for it again.
	n := awaitModal[*ui.Notice](t, a, "a notice", nil)
	if n.Title != "Could not read a directory" {
		t.Errorf("the dialog is titled %q", n.Title)
	}
	if !strings.HasPrefix(n.Message(), bad+"\n") {
		t.Errorf("the dialog does not start with the directory:\n%s", n.Message())
	}
	if !strings.Contains(n.Message(), p.Err().Error()) {
		t.Errorf("the dialog says\n%s\nwant it to hold the whole of\n%s",
			n.Message(), p.Err().Error())
	}
	if !n.Failure {
		t.Error("the dialog is not marked as a failure, so its title is not red")
	}

	// And the row brings it back once it has been read and put away.
	dismissNotice(t, a)
	if _, err := a.root.HandleMouse(input.MouseEvent{
		Kind: input.MousePress, Button: input.MouseLeft, Col: 4, Row: 2,
	}); err != nil {
		t.Fatalf("clicking the row: %v", err)
	}
	if again := awaitModal[*ui.Notice](t, a, "a notice", nil); again.Message() != n.Message() {
		t.Errorf("the row brought back\n%s\nwant\n%s", again.Message(), n.Message())
	}
}

// A pane nobody is looking at waits for the keys before it says why: a
// dialog on top of what somebody is doing in the other pane is the
// window getting in their way. It says so once, not on every visit.
func TestAFilePaneWithoutTheKeysWaitsToSayWhyAReadFailed(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)

	b, _, _ := onlyBrowser(t, a)
	other := b.view.Panes()[1]
	if other.Focused() {
		t.Fatal("the second pane has the keys, so there is no waiting to test")
	}

	other.Open(refusedPath(t))
	waitFor(t, a, "the read to fail", func() bool { return !other.Busy() && other.Err() != nil })
	noNoticeOpens(t, a, "a failure in the pane without the keys")

	// Tab moves the keys to it, and that is when it says why.
	if _, err := a.root.HandleKey(press(input.KeyTab, 0)); err != nil {
		t.Fatalf("Tab: %v", err)
	}
	if !other.Focused() {
		t.Fatal("Tab did not move the keys to the other pane")
	}
	awaitModal[*ui.Notice](t, a, "a notice", nil)
	dismissNotice(t, a)

	// Leaving and coming back is not another failure.
	for range 2 {
		if _, err := a.root.HandleKey(press(input.KeyTab, 0)); err != nil {
			t.Fatalf("Tab: %v", err)
		}
	}
	if !other.Focused() {
		t.Fatal("two more Tabs did not come back to the pane that failed")
	}
	noNoticeOpens(t, a, "coming back to a pane whose reason has been read")
}

// Dragging the divider between two file panes gives one of them more
// room. It starts at the press on the window's own root, the way the
// user does it.
func TestDraggingAFileBrowserDivider(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	b, _, _ := onlyBrowser(t, a)
	panes := b.view.Panes()

	area, shown := a.root.AreaOf(panes[0])
	if !shown {
		t.Fatal("the first file pane is not on screen")
	}
	right, shown := a.root.AreaOf(panes[1])
	if !shown {
		t.Fatal("the second file pane is not on screen")
	}
	// The divider is the column the first pane ends in.
	at, row := area.X+area.Cols, area.Y+1
	wasLeft, wasRight := area.Cols, right.Cols

	for _, ev := range []input.MouseEvent{
		{Kind: input.MousePress, Button: input.MouseLeft, Col: at, Row: row},
		{Kind: input.MouseMove, Button: input.MouseLeft, Col: at - 10, Row: row},
		{Kind: input.MouseRelease, Button: input.MouseLeft, Col: at - 10, Row: row},
	} {
		if _, err := a.root.HandleMouse(ev); err != nil {
			t.Fatalf("the drag failed: %v", err)
		}
	}

	got, _ := a.root.AreaOf(panes[0])
	after, _ := a.root.AreaOf(panes[1])
	if got.Cols != wasLeft-10 {
		t.Errorf("the first pane is %d columns wide, want %d", got.Cols, wasLeft-10)
	}
	if after.Cols != wasRight+10 {
		t.Errorf("the second pane is %d columns wide, want %d", after.Cols, wasRight+10)
	}
	// The panes and the rule between them still fill the browser.
	if at := after.X + after.Cols; at != right.X+right.Cols {
		t.Errorf("the second pane now ends at column %d, want the right edge at %d",
			at, right.X+right.Cols)
	}
}

// aSavedMachine is a window with one machine in the server list, pointed
// at a test server and nothing connected to it.
func aSavedMachine(t *testing.T) (*testApp, *sshtest.Server) {
	t.Helper()
	s := sshtest.New(t)
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	withMenubar(t, a)
	pinServers(t, a, s)
	saveHost(t, a, "margit", s, "")
	a.refreshServers()
	return a, s
}

// filePaneOn returns the file pane the window has on a machine, and nil
// when it has none.
func filePaneOn(a *testApp, host string) *files.Pane {
	if a.files == nil {
		return nil
	}
	for _, p := range a.files.view.Panes() {
		if p.FS().Name() == host {
			return p
		}
	}
	return nil
}

// openAt sends a file pane to a directory on the machine it is on and
// waits for the listing.
func openAt(t *testing.T, a *testApp, p *files.Pane, dir string) {
	t.Helper()
	// It opens on the user's own directory by itself, on a goroutine of
	// its own, so that has to land before the test sends it anywhere
	// else.
	waitFor(t, a, "the pane to open somewhere", func() bool { return p.At() != "" })
	p.Open(dir)
	waitFor(t, a, "the listing of "+dir, func() bool { return p.At() == dir && !p.Busy() })
}

// rowKindsUnder is the kind of every sidebar row under a machine.
func rowKindsUnder(a *testApp, host string) []conns.Kind {
	var kinds []conns.Kind
	for _, group := range a.registry.Groups(time.Now()) {
		if group.Host != host {
			continue
		}
		for _, row := range group.Rows {
			kinds = append(kinds, row.Kind)
		}
	}
	return kinds
}

// "Files" on a machine nothing is connected to connects to it first and
// opens the pane on the connection it made, the way "Terminal" does.
//
// The connection is made for files alone: no shell is started on it, and
// the pane that watched it being made goes once there is a file pane to
// look at instead.
func TestFilesOnASavedMachineConnectsFirst(t *testing.T) {
	a, s := aSavedMachine(t)
	dials := dialCounter(a)
	dir := filepath.ToSlash(t.TempDir())
	putFile(t, dir, "one.txt", "the body")

	menu := clickPlus(t, a, "margit")
	if !offers(menu, "conn.files") {
		t.Fatalf("the plus offers %v", menuCommands(menu))
	}
	chooseMenuItem(t, menu, "conn.files")

	// A pane holds the place while the connection is made. Read now
	// rather than waited for: the command runs where it is chosen, so
	// the row is there as soon as the line is, and waiting for a row
	// that goes again when the pane closes is a race.
	connecting := rowSaying(t, a, "connecting")
	if connecting.Host != "margit" || connecting.Kind != conns.Files {
		t.Errorf("the pane watching the connection is %v on %q, want files on margit",
			connecting.Kind, connecting.Host)
	}

	var pane *files.Pane
	waitFor(t, a, "a file pane on margit", func() bool {
		pane = filePaneOn(a, "margit")
		return pane != nil
	})

	// One login, and nothing riding on it.
	if n := s.Conns(); n != 1 {
		t.Errorf("the server saw %d logins, want the one the pane reads over", n)
	}
	if *dials != 1 {
		t.Errorf("%d machines were dialled, want the one", *dials)
	}
	m := a.machines.named("margit")
	if m == nil {
		t.Fatalf("the window holds no connection to margit: %v", a.machines.names())
	}
	if on := a.machines.panesOn(m); len(on) != 0 {
		t.Errorf("%d shells were opened on a connection asked for by Files", len(on))
	}

	// The pane reads the machine's files.
	openAt(t, a, pane, dir)
	got := pane.Entries()
	if len(got) != 1 || got[0].Name != "one.txt" {
		t.Errorf("the pane shows %v, want the file the server has", got)
	}

	// The pane that was connecting has gone, and the sidebar shows the
	// file pane's row under the machine and no terminal.
	for _, e := range a.panes {
		if e.Host == "margit" {
			t.Errorf("the pane that was connecting is still there, saying %q", e.Label)
		}
	}
	rows := 0
	for _, kind := range rowKindsUnder(a, "margit") {
		if kind == conns.Terminal {
			t.Errorf("the sidebar shows a terminal under margit: %v", panelText(a, time.Now()))
		}
		if kind == conns.Files {
			rows++
		}
	}
	if rows != 1 {
		t.Errorf("the sidebar shows %d file rows under margit, want the one", rows)
	}
	checkTree(t, a)

	// The account of the connection is where every other one is.
	chooseMenuItem(t, clickPlus(t, a, "margit"), "conn.log")
	n := awaitModal(t, a, "the account", byTitle[*ui.Notice]("How margit was reached"))
	if !strings.Contains(n.Message(), "connected to margit") {
		t.Errorf("the account is %q", n.Message())
	}
}

// A terminal on a machine connected for files alone rides on the
// connection that is already there.
func TestATerminalRidesOnAConnectionMadeForFiles(t *testing.T) {
	a, s := aSavedMachine(t)
	dials := dialCounter(a)

	chooseMenuItem(t, clickPlus(t, a, "margit"), "conn.files")
	waitFor(t, a, "a file pane on margit", func() bool { return filePaneOn(a, "margit") != nil })

	clickTerminalLine(t, a, "margit")
	m := a.machines.named("margit")
	waitFor(t, a, "a shell on margit", func() bool {
		return m != nil && len(a.machines.panesOn(m)) == 1
	})
	if n := s.Conns(); n != 1 {
		t.Errorf("the server saw %d logins, want the one both ride on", n)
	}
	if *dials != 1 {
		t.Errorf("%d machines were dialled, want the one", *dials)
	}
}

// "Files" on a machine already being connected to asks about the one on
// its way, and waiting for it opens the pane when it lands.
func TestFilesWaitsForAMachineOnItsWay(t *testing.T) {
	a, s := aSavedMachine(t)
	dials := dialCounter(a)

	// A command runs where it is chosen, so nothing the first dial posts
	// can land between these two clicks: the second request meets a
	// machine on its way every time.
	clickTerminalLine(t, a, "margit")
	if a.about("margit").dialling == nil {
		t.Fatal("the first request is not on its way")
	}
	chooseMenuItem(t, clickPlus(t, a, "margit"), "conn.files")

	f := awaitModal(t, a, "the question about the one on its way",
		byTitlePrefix[*ui.Form]("Already connecting to"))
	pressButton(t, a, f, "Wait for it")

	waitFor(t, a, "a file pane on margit", func() bool { return filePaneOn(a, "margit") != nil })
	if n := s.Conns(); n != 1 {
		t.Errorf("the server saw %d logins, want the one that was waited for", n)
	}
	if *dials != 1 {
		t.Errorf("%d machines were dialled, want the one that was waited for", *dials)
	}
}

// A connection asked for by "Files" that cannot be made keeps its pane,
// with the account of what happened still in it, and opens no file pane.
func TestFilesOnAMachineThatWillNotAnswerKeepsItsAccount(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	withMenubar(t, a)
	// Nothing to dial with but the test's own key, and a port nothing
	// answers on.
	pinServers(t, a)
	if err := a.book.Put(remote.Host{
		Name: "margit", Address: "127.0.0.1", Port: 1, User: "tester",
	}, ""); err != nil {
		t.Fatalf("save the machine: %v", err)
	}
	a.refreshServers()

	chooseMenuItem(t, clickPlus(t, a, "margit"), "conn.files")
	pane := newestPane(t, a)
	waitFor(t, a, "the pane to say the connection was not made", func() bool {
		return strings.Contains(paneText(pane), "The connection was not made")
	})
	if got := paneText(pane); !strings.Contains(got, "connecting to") {
		t.Errorf("the account went with the failure: %q", got)
	}
	if a.files != nil {
		t.Fatalf("a file manager opened on a machine that never answered: %v", filesRows(a))
	}
}

// A machine that is already connected opens its pane on the connection
// it has, without dialling again.
func TestFilesOnAConnectedMachineDialsNothing(t *testing.T) {
	s := sshtest.New(t)
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	host := connectedTo(t, a, s)
	dials := dialCounter(a)

	openFilesFromThePlus(t, a, host)
	if n := s.Conns(); n != 1 {
		t.Errorf("the server saw %d logins, want the one that was already open", n)
	}
	if *dials != 0 {
		t.Errorf("%d machines were dialled for a pane on a machine already connected", *dials)
	}
}

// rowSaying is the sidebar row whose label says this, now rather than
// when it turns up.
func rowSaying(t *testing.T, a *testApp, label string) *conns.Entry {
	t.Helper()
	for _, group := range a.registry.Groups(time.Now()) {
		for _, row := range group.Rows {
			if row.Label == label {
				return row.Entry
			}
		}
	}
	t.Fatalf("no row says %q: %v", label, panelText(a, time.Now()))
	return nil
}

// closeRow closes a pane the way the x on its sidebar row does.
func closeRow(t *testing.T, a *testApp, pane *term.Terminal) {
	t.Helper()
	e := a.panes[pane]
	if e == nil || e.Close == nil {
		t.Fatal("the pane has no row to close")
	}
	if err := e.Close(); err != nil {
		t.Fatalf("closing the row: %v", err)
	}
}

// The pane that watches the connection can be the last one in the
// window, so the file pane has to be in before it goes: the window quits
// with its last pane.
func TestTheWindowStaysWhenTheConnectingPaneWasItsLast(t *testing.T) {
	a, _ := aSavedMachine(t)

	chooseMenuItem(t, clickPlus(t, a, "margit"), "conn.files")
	// Nothing else left in the window while it connects: the pane it
	// started with goes, the way closing its row does.
	closeRow(t, a, localPane(t, a))

	waitFor(t, a, "a file pane on margit", func() bool { return filePaneOn(a, "margit") != nil })
	if a.quit.Load() {
		t.Fatal("the window quit when the pane that was connecting closed")
	}
	if len(a.panes) != 0 {
		t.Errorf("%d terminals are left, want only the file manager", len(a.panes))
	}
	checkTree(t, a)
}

// A machine that answers but will not open its files stays connected,
// and the account says what could not be opened rather than saying the
// connection was never made.
func TestFilesOnAMachineThatRefusesThemKeepsTheConnection(t *testing.T) {
	a, s := aSavedMachine(t)
	s.RefuseSFTP()

	chooseMenuItem(t, clickPlus(t, a, "margit"), "conn.files")
	pane := newestPane(t, a)
	waitFor(t, a, "the pane to say the files could not be opened", func() bool {
		return strings.Contains(paneText(pane), "could not be opened")
	})

	got := paneText(pane)
	if strings.Contains(got, "The connection was not made") {
		t.Errorf("the account says the dial failed on a machine that answered: %q", got)
	}
	if !strings.Contains(got, "margit is connected") {
		t.Errorf("the account does not say the machine is still connected: %q", got)
	}
	if a.machines.named("margit") == nil {
		t.Errorf("the connection went with the files: %v", a.machines.names())
	}
	if e := a.panes[pane]; e == nil || e.Label != "no files" {
		t.Errorf("the row says %q, want it to say the files did not open", e.Label)
	}
	if a.files != nil {
		t.Errorf("a file manager opened on a machine that refused SFTP: %v", filesRows(a))
	}
	// And the whole account is where every other one is.
	chooseMenuItem(t, clickPlus(t, a, "margit"), "conn.log")
	n := awaitModal(t, a, "the account", byTitle[*ui.Notice]("How margit was reached"))
	if !strings.Contains(n.Message(), "connected to margit") {
		t.Errorf("the account is %q", n.Message())
	}
}

// "Files" first and "Terminal" while it is still on its way: the shell
// opens on the connection the files asked for, once it lands.
func TestATerminalWaitsForAConnectionAskedForByFiles(t *testing.T) {
	a, s := aSavedMachine(t)
	dials := dialCounter(a)

	chooseMenuItem(t, clickPlus(t, a, "margit"), "conn.files")
	if a.about("margit").dialling == nil {
		t.Fatal("the files request is not on its way")
	}
	clickTerminalLine(t, a, "margit")

	f := awaitModal(t, a, "the question about the one on its way",
		byTitlePrefix[*ui.Form]("Already connecting to"))
	pressButton(t, a, f, "Wait for it")

	waitFor(t, a, "a shell on margit", func() bool {
		m := a.machines.named("margit")
		return m != nil && len(a.machines.panesOn(m)) == 1
	})
	if filePaneOn(a, "margit") == nil {
		t.Error("the file pane the connection was asked for is not there")
	}
	if n := s.Conns(); n != 1 {
		t.Errorf("the server saw %d logins, want the one both ride on", n)
	}
	if *dials != 1 {
		t.Errorf("%d machines were dialled, want the one", *dials)
	}
}

// A file pane on a window taken over reads the files of the machine that
// window is on.
//
// The whole path in one test: the plus opens the pane, windowFiles opens
// an SFTP session over the connection to the other window, and the pane
// lists a directory and reads a file in it.
func TestAFilePaneOnAWindowReadsItsFiles(t *testing.T) {
	_, client, addr := twoWindows(t)

	at := t.TempDir()
	putFile(t, at, "one.txt", "the body")
	// The same directory as the other window's filesystem spells it: one
	// root with the drives under it, so a Windows path hangs off "/" and
	// a POSIX one is already there.
	dir := path.Join("/", filepath.ToSlash(at))

	pane := openFilesFromThePlus(t, client, addr)
	openAt(t, client, pane, dir)

	got := pane.Entries()
	if len(got) != 1 || got[0].Name != "one.txt" {
		t.Fatalf("the pane shows %v, want the file the other window has", got)
	}

	// And the file itself comes over the same session, which is the only
	// thing that proves the filesystem is really on that window.
	f, err := pane.FS().Open(dir + "/one.txt")
	if err != nil {
		t.Fatalf("open a file on the window: %v", err)
	}
	defer func() { _ = f.Close() }()
	body, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("read a file on the window: %v", err)
	}
	if string(body) != "the body" {
		t.Errorf("the file reads %q, want what the other window has in it", body)
	}
}

// openFilesFromTheFarPlus opens a file pane on a machine a window taken
// over is connected to, the way a user does: the plus on that machine's
// heading, and the line that browses.
func openFilesFromTheFarPlus(t *testing.T, client, host *testApp, addr, machine string) *files.Pane {
	t.Helper()
	was := make(map[*files.Pane]bool)
	if client.files != nil {
		for _, p := range client.files.view.Panes() {
			was[p] = true
		}
	}
	menu := clickPlusFar(t, client, addr, machine)
	chooseMenuItemOver(t, host, menu, "conn.files")
	var opened *files.Pane
	waitFor(t, client, "a file pane on "+machine, func() bool {
		if client.files == nil {
			return false
		}
		for _, p := range client.files.view.Panes() {
			if !was[p] {
				opened = p
				return true
			}
		}
		return false
	}, host)
	return opened
}

// rowAt is where the sidebar drew a key, and -1 when it drew nothing for
// it.
func rowAt(a *testApp, key any) int {
	for i, row := range a.panel.Rows() {
		if row.Key == key {
			return i
		}
	}
	return -1
}

// The plus on a machine of a window taken over offers files and nothing
// else.
//
// That machine is reached through the window, which holds the shells and
// the tunnels on it. A terminal, a command or a tunnel there would be
// this window reaching past the only connection it has.
func TestThePlusOnAMachineOverThereOffersFilesAlone(t *testing.T) {
	_, client, addr := aWindowConnectedToMargit(t)

	menu := clickPlusFar(t, client, addr, "margit")

	if got := menuCommands(menu); len(got) != 1 || got[0] != "conn.files" {
		t.Fatalf("the plus offers %v, want files alone", got)
	}
	// And it says so in the word the rest of the sidebar uses, rather
	// than in whatever the command is called.
	if got := menu.Items()[0].Title; got != "Files" {
		t.Errorf("the line reads %q, want Files", got)
	}
}

// Choosing it opens a pane that reads that machine through the window.
//
// The whole path in one test: the plus opens the pane, the window over
// there relays the bytes to the machine it is connected to, and the pane
// lists a directory on that machine.
func TestFilesOnAMachineOverThereReadItThroughTheWindow(t *testing.T) {
	host, client, addr := aWindowConnectedToMargit(t)
	held := windowAt(t, client, addr)

	dir := t.TempDir()
	putFile(t, dir, "over-there.txt", "the body")

	pane := openFilesFromTheFarPlus(t, client, host, addr, "margit")

	// The machine it reads, through the window carrying the bytes: every
	// question the pane asks is about margit.
	if got, want := pane.FS().Name(), "margit through "+held.name; got != want {
		t.Errorf("the pane says it is on %q, want %q", got, want)
	}
	// And it is filed under the window, so it goes when the window does.
	if got := client.hostOf(pane.FS()); got != held.name {
		t.Errorf("the pane is filed under %q, want the window %q", got, held.name)
	}
	// And margit on that window as its place, so it is neither the
	// window's own disk nor a machine of this one's.
	want := any(remoteHostKey{window: held, host: "margit"})
	if got := pane.FS().Place(); got != want {
		t.Errorf("the pane's place is %v, want margit on that window", got)
	}
	if pane.FS().Place() == any(held.win) {
		t.Error("the pane counts as the window's own disk")
	}

	// What is on margit's disk, read through the window over there.
	var entries []vfs.Entry
	offWindow(t, client, "read the directory on margit", func() error {
		var err error
		entries, err = pane.FS().ReadDir(overThere(dir))
		return err
	})
	found := false
	for _, e := range entries {
		if e.Name == "over-there.txt" {
			found = true
		}
	}
	if !found {
		t.Errorf("the file was not there: %v", entries)
	}

	// Its row goes under margit: after the heading, and before the
	// screens over there nobody here is watching.
	client.refreshPanel(panelNow)
	heading := rowAt(client, remoteHostKey{window: held, host: "margit"})
	mine := rowAt(client, client.files.rows[pane])
	screen := rowAt(client, remoteRowOn(t, client, addr, "margit"))
	if heading < 0 || mine < 0 || screen < 0 {
		t.Fatalf("heading %d, the pane %d, a screen over there %d: %v",
			heading, mine, screen, panelText(client, panelNow))
	}
	if !(heading < mine && mine < screen) {
		t.Errorf("the rows are drawn heading %d, pane %d, screen %d: %v",
			heading, mine, screen, panelText(client, panelNow))
	}

	// And the heading is drawn the way a machine's is: a step in from
	// the window's own heading, with its rows beside it, and a state dot
	// saying the window over there is connected to it.
	//
	// The dot's colour is this window's own for the state the window over
	// there published, so a heading marked "closed" in grey would not pass
	// for one that is connected.
	said := ""
	for _, open := range held.win.Opens() {
		if open.Host == "margit" && open.Kind == conns.Server.String() {
			said = open.State
		}
	}
	if said == "" {
		t.Fatal("the window over there published no connection to margit")
	}
	wantMark := client.stateFG(stateNamed(said), panelNow)
	rows := client.panel.Rows()
	if got := rows[heading]; got.Depth != 2 || got.Mark != dot || got.MarkFG != wantMark {
		t.Errorf("margit's heading is drawn at depth %d with mark %q in %v, "+
			"want a machine heading with a %s dot in %v",
			got.Depth, got.Mark, got.MarkFG, said, wantMark)
	}
	if got := rows[mine]; got.Depth != 2 {
		t.Errorf("the pane's row is drawn at depth %d, want beside the heading", got.Depth)
	}
}

// Letting go of the window takes the pane with it.
//
// The pane reads through that window's connection. One left behind would
// be a pane whose every read fails, on a machine this window cannot
// reach by itself.
func TestLettingGoOfTheWindowClosesAPaneOnAMachineOverThere(t *testing.T) {
	host, client, addr := aWindowConnectedToMargit(t)

	pane := openFilesFromTheFarPlus(t, client, host, addr, "margit")
	if client.files == nil || len(client.files.view.Panes()) != 1 {
		t.Fatalf("the manager holds %v, want the one pane", filesRows(client))
	}

	// The user's own path: the plus on the window's heading, and the
	// line that lets go of it.
	chooseMenuItem(t, clickPlus(t, client, addr), "conn.disconnect")

	// The pane says goodbye to its file session and lets go either way,
	// with nothing to read: letting go of a window is not something that
	// can half work.
	if n, up := client.root.Modal().(*ui.Notice); up {
		t.Errorf("letting go reported %q: %s", n.Title, n.Message())
	}

	if client.files != nil {
		for _, p := range client.files.view.Panes() {
			if p == pane {
				t.Fatal("the pane on margit outlived the window it read through")
			}
		}
		t.Errorf("the manager is still open: %v", filesRows(client))
	}
	// And nothing of it is left on the sidebar: no heading for margit,
	// and no row for a pane on it.
	for _, line := range panelText(client, panelNow) {
		if strings.Contains(line, "margit") {
			t.Errorf("the sidebar still says %q: %v", line, panelText(client, panelNow))
		}
	}
}

// And letting go of a window whose goodbye came back late says nothing to
// the user either.
//
// filesGrace is one round trip to that window, spent on the goroutine
// that draws, so a slow link or a pause in the garbage collector runs it
// out with nothing wrong. There is nothing on a notice about it for the
// user to act on, and the pane has gone whichever way it went, so it is
// logged.
func TestLettingGoOfAWindowThatWentQuietSaysNothingToTheUser(t *testing.T) {
	client, addr, relay := aRelayedWindow(t)

	// The link goes quiet for longer than the grace and then comes back,
	// which is the slow round trip the grace really catches.
	relay.stop()
	time.AfterFunc(2*filesGrace, relay.resume)

	chooseMenuItem(t, clickPlus(t, client, addr), "conn.disconnect")

	if n, up := client.root.Modal().(*ui.Notice); up {
		t.Errorf("letting go reported %q: %s", n.Title, n.Message())
	}
	if !client.logged.holds(errFilesGraceExpired.Error()) {
		t.Errorf("nothing was logged about the goodbye going unanswered: %v", client.logged.all())
	}
	// And the window has gone, which is what letting go is for.
	if client.windows.named(addr) != nil {
		t.Error("the window is still held")
	}
}

// And letting go of a window whose link is dead in both directions comes
// back too.
//
// Closing the channel only sends a message. A window that is still on
// the network and carrying nothing never answers it, so the SFTP
// client's own close stays parked on a read that nothing will wake.
// Waited for, that read is the goroutine that draws: the one action left
// to a user with a wedged window would be the one that wedges the window
// for good.
func TestLettingGoOfAWindowWhoseLinkIsDeadComesBack(t *testing.T) {
	client, addr, relay := aRelayedWindow(t)

	// Nothing crosses in either direction from here on, and it never
	// starts again: the window over there has dropped off the network.
	relay.stop()

	// The user's own path, run on a goroutine of its own so a wait that
	// never ends is a failure here rather than the whole package timing
	// out. Nothing else touches this window meanwhile.
	menu := clickPlus(t, client, addr)
	selectMenuItem(t, menu, "conn.disconnect")
	done := make(chan error, 1)
	go func() {
		_, err := menu.HandleKey(input.Event{Kind: input.KeyPress, Key: input.KeyEnter})
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("letting go of the window: %v", err)
		}
	case <-time.After(20 * filesGrace):
		t.Fatal("letting go of a window whose link is dead never came back")
	}

	// It says nothing to the user: the pane has gone either way, and
	// there is nothing on a notice about it to act on.
	if n, up := client.root.Modal().(*ui.Notice); up {
		t.Errorf("letting go reported %q: %s", n.Title, n.Message())
	}
	if !client.logged.holds(errFilesCloseAbandoned.Error()) {
		t.Errorf("nothing was logged about the close being left behind: %v", client.logged.all())
	}
	// And the window has gone, which is what letting go is for, and what
	// ends the close that was left behind.
	if client.windows.named(addr) != nil {
		t.Error("the window is still held")
	}
}

// Closing the focused pane with the close-pane key says nothing to the
// user when its file session only ran the grace out.
//
// Ctrl+Shift+W and the File menu both come here, and it is the commonest
// way a file pane goes. The grace is one round trip on the goroutine that
// draws, which a slow link runs out with nothing wrong, so a notice about
// it is a notice the user can do nothing with.
func TestClosingTheFocusedPaneSaysNothingAboutTheGrace(t *testing.T) {
	client, _, relay := aRelayedWindow(t)
	pane := onlyFilePane(t, client)

	// The link goes quiet and comes back long after both bounds, so the
	// goodbye goes unanswered however late the close starts. Timed to come
	// back inside them, a close that started a moment late would find the
	// link working and log nothing.
	relay.stop()
	time.AfterFunc(8*filesGrace, relay.resume)

	client.focus(pane)
	if err := client.closeFocused(); err != nil {
		t.Fatalf("closing the focused pane reported %v", err)
	}
	if n, up := client.root.Modal().(*ui.Notice); up {
		t.Errorf("closing the pane reported %q: %s", n.Title, n.Message())
	}
	if !client.logged.holds(errFilesGraceExpired.Error()) {
		t.Errorf("nothing was logged about the goodbye going unanswered: %v", client.logged.all())
	}
	// And the pane has gone, which is what the key is for.
	if client.files != nil {
		t.Errorf("the pane is still open: %v", filesRows(client))
	}
}

// The cross on a pane's row says nothing about the grace either.
func TestTheCrossOnAFilePaneSaysNothingAboutTheGrace(t *testing.T) {
	client, _, relay := aRelayedWindow(t)
	pane := onlyFilePane(t, client)
	row := client.files.rows[pane]
	if row == nil || row.Close == nil {
		t.Fatal("the pane has no row to close from")
	}

	// Back long after both bounds, so the goodbye goes unanswered however
	// late the close starts.
	relay.stop()
	time.AfterFunc(8*filesGrace, relay.resume)

	if err := row.Close(); err != nil {
		t.Fatalf("the cross on the row reported %v", err)
	}
	if n, up := client.root.Modal().(*ui.Notice); up {
		t.Errorf("the cross reported %q: %s", n.Title, n.Message())
	}
	if !client.logged.holds(errFilesGraceExpired.Error()) {
		t.Errorf("nothing was logged about the goodbye going unanswered: %v", client.logged.all())
	}
	if client.files != nil {
		t.Errorf("the pane is still open: %v", filesRows(client))
	}
}

// A filesystem let go of on a goroutine of its own says nothing about the
// grace when the frame picks its failure up.
//
// A pane a job was reading through is closed elsewhere, so what that
// close reported reaches the user a frame or more later. It is the same
// grace either way.
func TestACloseInTheBackgroundSaysNothingAboutTheGrace(t *testing.T) {
	client, _, relay := aRelayedWindow(t)
	pane := onlyFilePane(t, client)

	// A job of the pane's that has already finished, which is what sends
	// the close off this goroutine: the window waits for the job before
	// it lets go of what the job was reading.
	finishedJobOn(t, client, pane.FS())

	relay.stop()
	time.AfterFunc(2*filesGrace, relay.resume)
	if err := client.closePane(pane); err != nil {
		t.Fatalf("closing the pane reported %v", err)
	}

	// The frames go on coming, and one of them picks the close up.
	waitFor(t, client, "the background close to be picked up", func() bool {
		client.reportClosed()
		return client.logged.holds(errFilesGraceExpired.Error())
	})
	if n, up := client.root.Modal().(*ui.Notice); up {
		t.Errorf("letting go in the background reported %q: %s", n.Title, n.Message())
	}
}

// Quitting with a machine that has stopped answering exits cleanly.
//
// Closing the connection closes the file session riding on it, and a link
// that is dead both ways never answers, so that close is abandoned. Left
// in the join the window hands to log.Fatal, it would turn an ordinary
// quit into a failure and an exit code of 1.
func TestQuittingWithAWedgedMachineDoesNotFail(t *testing.T) {
	a, relay := aRelayedMachine(t)
	if err := a.openFilesOn("margit"); err != nil {
		t.Fatalf("open a file pane on margit: %v", err)
	}
	waitFor(t, a, "a file pane on margit", func() bool { return a.files != nil })

	// margit drops off the network, and the user quits.
	relay.stop()

	// On a goroutine of its own, so a shutdown that never comes back is a
	// failure here rather than the whole package timing out.
	done := make(chan error, 1)
	go func() { done <- a.shutDown(nil) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("quitting reported %v, want the abandoned close logged and a clean exit", err)
		}
	case <-time.After(waitBudget):
		t.Fatal("quitting with a machine that had stopped answering never came back")
	}
	if !a.logged.holds(remote.ErrCloseAbandoned.Error()) {
		t.Errorf("nothing was logged about the close being left behind: %v", a.logged.all())
	}
}

// And a real failure on the way out still fails.
func TestQuittingStillFailsOnRealTrouble(t *testing.T) {
	a, _ := aRelayedMachine(t)
	boom := errors.New("the game loop fell over")

	if err := a.shutDown(boom); !errors.Is(err, boom) {
		t.Errorf("quitting reported %v, want the failure the window ended with", err)
	}
}

// A window taken over that has stopped answering is refused the next file
// pane once too many closes have been left behind on it.
//
// An abandoned close holds a goroutine and the file session's error
// stream until this window hangs up on that one, which letting go of the
// window does and closing a pane does not. Without a bound, panes opened
// and closed on a wedged window pile them up.
func TestAWindowWithTooManyAbandonedFileClosesIsRefusedTheNext(t *testing.T) {
	client, addr, relay := aRelayedWindow(t)
	held := windowAt(t, client, addr)
	// aRelayedWindow opens the first, so this is as many as the window
	// will leave closes behind for.
	for i := 1; i < mostAbandonedCloses; i++ {
		openFilesFromThePlus(t, client, addr)
	}
	panes := append([]*files.Pane(nil), client.files.view.Panes()...)
	if len(panes) != mostAbandonedCloses {
		t.Fatalf("the window holds %d file panes, want %d", len(panes), mostAbandonedCloses)
	}

	// The window over there drops off the network for good, and the panes
	// are closed one at a time: each goodbye goes unanswered, and so does
	// the close that follows it.
	relay.stop()
	for _, pane := range panes {
		// The way the cross on the row closes one: an unanswered goodbye
		// is logged rather than handed back.
		if err := client.graceLogged(client.closePane(pane)); err != nil {
			t.Fatalf("closing a file pane: %v", err)
		}
	}
	waitFor(t, client, "every close to be counted against the window", func() bool {
		return client.windows.abandonedCloses(held.win) == mostAbandonedCloses
	})

	// The next one is turned away before anything is opened, by name and
	// by count.
	_, err := client.windowFiles(addr)
	if err == nil {
		t.Fatal("a file pane was opened on a window with every close left behind on it")
	}
	want := addr + " has stopped answering; four file sessions to it are still waiting to end"
	if err.Error() != want {
		t.Errorf("it said %q, want %q", err, want)
	}

	// And letting go of the window ends every one of them, so the count
	// goes with it.
	if err := client.dropWindow(addr); err != nil {
		t.Fatalf("let go of the window: %v", err)
	}
	if got := client.windows.abandonedCloses(held.win); got != 0 {
		t.Errorf("%d closes are still counted against a window that has been let go of", got)
	}
}

// A failure carrying a grace and real trouble together is still shown.
//
// Only the grace is quiet. A close that ran its bound out and also failed
// for a reason of its own is a failure the user can act on, and the words
// around it say what was being closed.
func TestAGraceAlongsideRealTroubleIsStillShown(t *testing.T) {
	boom := errors.New("the session would not close")

	t.Run("joined", func(t *testing.T) {
		expired, rest := splitGraceExpired(errors.Join(errFilesGraceExpired, boom))
		if !errors.Is(expired, errFilesGraceExpired) {
			t.Errorf("the grace was not taken out to be logged: %v", expired)
		}
		if !errors.Is(rest, boom) {
			t.Errorf("the real failure was hidden: %v", rest)
		}
	})

	// The way a close in the background reports it: the join is wrapped in
	// the words saying what was being closed.
	t.Run("wrapped", func(t *testing.T) {
		err := fmt.Errorf("could not close margit: %w", errors.Join(errFilesGraceExpired, boom))
		expired, rest := splitGraceExpired(err)
		if !errors.Is(rest, boom) {
			t.Errorf("the real failure was hidden: expired %v, shown %v", expired, rest)
		}
		if rest != nil && !strings.Contains(rest.Error(), "could not close margit") {
			t.Errorf("what is shown lost the words saying what was closed: %v", rest)
		}
		// And nothing was taken out to be logged: the whole thing is
		// shown, so logging a copy of it would say it twice.
		if expired != nil {
			t.Errorf("it logged %v as well as showing it", expired)
		}
	})

	// And a wrap carrying nothing but the grace is still quiet.
	t.Run("wrapped grace alone", func(t *testing.T) {
		err := fmt.Errorf("could not close margit: %w", errors.Join(errFilesGraceExpired, nil))
		expired, rest := splitGraceExpired(err)
		if rest != nil {
			t.Errorf("a close that only ran the grace out was shown: %v", rest)
		}
		if !errors.Is(expired, errFilesGraceExpired) {
			t.Errorf("the grace was not logged: %v", expired)
		}
	})
}

// finishedJobOn leaves a job that has already stopped on the window's
// list, against a filesystem.
//
// A filesystem with a job on it is let go of on a goroutine of its own,
// which is the path this is for. The job has finished, so that goroutine
// has nothing to wait for.
func finishedJobOn(t *testing.T, a *testApp, f vfs.FS) {
	t.Helper()
	count := meter.New()
	e := &conns.Entry{Host: conns.Local, Kind: conns.Files, Meter: count}
	j := a.queue.Start(a.ctx, jobs.Op{
		Kind: jobs.Delete, From: f, At: "/", Names: []string{"no-such-file"},
	}, jobs.Options{Count: count})
	a.jobs[e] = j
	a.registry.Add(e)
	waitFor(t, a, "the job to finish", func() bool {
		select {
		case <-j.Done():
			return true
		default:
			return false
		}
	})
}

// The question before a delete names the machine the files are really
// on.
//
// A pane's own name is what every question about it is written with: the
// delete, the new directory, the go-to hint and every failure a read
// reports. One calling itself by the window's name asked about taking
// files off a machine they were not on.
func TestDeletingOnAMachineOverThereNamesThatMachine(t *testing.T) {
	host, client, addr := aWindowConnectedToMargit(t)

	dir := t.TempDir()
	putFile(t, dir, "one.txt", "the body")

	pane := openFilesFromTheFarPlus(t, client, host, addr, "margit")
	openAt(t, client, pane, overThere(dir))

	tap(t, client.files.view, input.KeyDown)
	tap(t, client.files.view, input.KeyF8)

	f := awaitModal(t, client, "the delete question", byTitlePrefix[*ui.Form]("Delete"))
	said := strings.Join(f.Lines, " ")
	if !strings.Contains(said, "margit") {
		t.Errorf("it asks %q, want the machine the files are on", said)
	}
	pressButton(t, client, f, "Keep them")
}

// A window renamed while a pane on one of its machines is open leaves
// that pane where it is, under the new name.
//
// The name is frozen into the filesystem when the pane opens, and the
// window finds a pane's machine by matching it. A pane left under the
// old name would be drawn under a heading nothing is held at.
func TestRenamingTheWindowMovesAPaneOnAMachineOverThere(t *testing.T) {
	host, client, addr, keyFile := aServingWindowConnectedToMargit(t)
	held := windowAt(t, client, addr)

	pane := openFilesFromTheFarPlus(t, client, host, addr, "margit")

	saveWindowFromTheDialog(t, client, "office", addr, keyFile)

	if got := client.windows.names(); !slices.Equal(got, []string{"office"}) {
		t.Fatalf("it is holding %v, want the one window under office", got)
	}
	if held != client.windows.named("office") {
		t.Fatal("the window was taken over again rather than renamed")
	}
	// The pane still reads margit, and says so under the new name.
	if got, want := pane.FS().Name(), "margit through office"; got != want {
		t.Errorf("the pane says it is on %q, want %q", got, want)
	}
	row := client.files.rows[pane]
	if row == nil {
		t.Fatal("the pane has no row on the sidebar")
	}
	if row.Host != "office" {
		t.Errorf("the pane's row is filed under %q, want office", row.Host)
	}

	// And it is drawn under margit's heading, which is under the
	// window's new name.
	client.refreshPanel(panelNow)
	window := rowAt(client, hostKey("office"))
	heading := rowAt(client, remoteHostKey{window: held, host: "margit"})
	mine := rowAt(client, row)
	if window < 0 || heading < 0 || mine < 0 {
		t.Fatalf("window %d, margit %d, the pane %d: %v",
			window, heading, mine, panelText(client, panelNow))
	}
	if !(window < heading && heading < mine) {
		t.Errorf("the rows are drawn office %d, margit %d, pane %d: %v",
			window, heading, mine, panelText(client, panelNow))
	}
}

// A machine the window over there is no longer connected to says so, in
// that window's own words.
//
// The rows are a snapshot old, so a heading can name a machine the
// window over there has since let go of. This window cannot tell: it has
// no connection of its own to try.
func TestFilesOnAMachineTheWindowHasLetGoOfSaysSo(t *testing.T) {
	host, client, addr := aWindowConnectedToMargit(t)

	// The menu goes up while the heading is still there, and the machine
	// over there is let go of before the line is chosen.
	menu := clickPlusFar(t, client, addr, "margit")
	if err := host.dropMachine("margit"); err != nil {
		t.Fatalf("let go of margit: %v", err)
	}

	chooseMenuItemOver(t, host, menu, "conn.files")

	// The failure is shown the way every command's is, with the window
	// over there quoted in it. The whole sentence: the machine is named,
	// and so is the fact that this window was reading through another one.
	n := awaitModal[*ui.Notice](t, client, "the failure", nil)
	if !strings.Contains(n.Message(), "the window you are reading through is not connected to margit") {
		t.Errorf("it said %q, want the window's own words about margit", n.Message())
	}
	if client.files != nil {
		t.Errorf("a file manager opened anyway: %v", filesRows(client))
	}
}

// The go-to dialog on a pane over there names the machine the pane reads,
// through the window it reads it through.
//
// The hint is the pane's own name, which is the name every question about
// the pane is written with. One saying the window would be asking for a
// directory on the wrong machine.
func TestTheGoToHintOnAMachineOverThereNamesThatMachine(t *testing.T) {
	host, client, addr := aWindowConnectedToMargit(t)
	held := windowAt(t, client, addr)

	pane := openFilesFromTheFarPlus(t, client, host, addr, "margit")
	client.focus(pane)

	// The chord, so the command is reached the way the user reaches it.
	sendKey(t, client, press(input.KeyG, input.ModCtrl|input.ModShift))
	f := awaitModal(t, client, "the go-to dialog", byTitle[*ui.Form]("Go to"))

	want := "a directory on margit through " + held.name
	if got := f.Field("Path").Placeholder; got != want {
		t.Errorf("the dialog asks for %q, want %q", got, want)
	}
	pressButton(t, client, f, "Cancel")
}
