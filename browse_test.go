package main

import (
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/input"
	"github.com/marrasen/gridterm/internal/sshtest"
	"github.com/marrasen/gridterm/remote"
	"github.com/marrasen/gridterm/ui"
	"github.com/marrasen/gridterm/ui/files"
	"github.com/marrasen/gridterm/ui/term"
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

	for i := 0; i < 3; i++ {
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

// paneDrawn reads a pane back off a grid of its own, one string per row.
func paneDrawn(p *files.Pane, cols, rows int) []string {
	g := grid.New(cols, rows, color.RGBA{}, color.RGBA{})
	p.Layout(ui.Size{Cols: cols, Rows: rows})
	p.Draw(g.View())
	out := make([]string, 0, rows)
	for y := 0; y < rows; y++ {
		var b strings.Builder
		for x := 0; x < cols; x++ {
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
	for i := 0; i < 100; i++ {
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
	for i := 0; i < 2; i++ {
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

// The machine -ssh put the panes on is reached through nothing this
// window holds, so "Files" on it says so rather than dialling, even when
// the server list holds the same name.
func TestFilesOnTheMachineThePanesRunOnDialsNothing(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	withMenubar(t, a)
	pinServers(t, a)
	dials := dialCounter(a)
	a.localHost = "tester@box"
	saveHostNamed(t, a, "tester@box", "box.example")
	a.refreshServers()

	chooseMenuItem(t, clickPlus(t, a, "tester@box"), "conn.files")

	n := awaitModal[*ui.Notice](t, a, "the reason it cannot", nil)
	if !strings.Contains(n.Message(), "did not open that connection") {
		t.Errorf("it said %q", n.Message())
	}
	if *dials != 0 {
		t.Errorf("%d machines were dialled for the machine the panes already run on", *dials)
	}
	if a.files != nil {
		t.Errorf("a file manager opened: %v", filesRows(a))
	}
}
