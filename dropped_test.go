package main

import (
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/internal/sshtest"
	"github.com/marrasen/gridterm/jobs"
	"github.com/marrasen/gridterm/pasted"
	"github.com/marrasen/gridterm/ui"
	"github.com/marrasen/gridterm/vfs"
)

// aDroppedFile writes a file of n bytes and returns its path.
func aDroppedFile(t *testing.T, name string, n int) string {
	t.Helper()
	at := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(at, []byte(strings.Repeat("x", n)), 0o600); err != nil {
		t.Fatalf("write it: %v", err)
	}
	return at
}

// What is typed for a set of paths is one line, and a path holding a
// space is quoted: what reads it is a shell or a program taking a word.
func TestWhatIsTypedForDroppedPaths(t *testing.T) {
	for what, tc := range map[string]struct {
		paths []string
		want  string
	}{
		"one":              {[]string{`C:\tmp\a.png`}, `C:\tmp\a.png`},
		"two":              {[]string{`/tmp/a`, `/tmp/b`}, `/tmp/a /tmp/b`},
		"one with a space": {[]string{`C:\my files\a.png`}, `"C:\my files\a.png"`},
		"none":             {nil, ""},
	} {
		if got := pasted.Typed(tc.paths); got != tc.want {
			t.Errorf("%s: it types %q, want %q", what, got, tc.want)
		}
	}
}

// A file dropped on a pane on this machine is typed straight away.
// There is nothing to copy: the program is running on the machine the
// file is already on.
func TestAFileDroppedOnAPaneHereIsTypedAtOnce(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withPanel(t, a)
	pane := onlyPaneOn(t, a)
	at := aDroppedFile(t, "notes.txt", 16)
	jobs := len(a.jobs)

	if err := a.dropOnPane(pane, []string{at}); err != nil {
		t.Fatalf("drop it: %v", err)
	}

	waitFor(t, a, "the path to reach the shell", func() bool {
		return strings.Contains(a.shells[0].sentText(), at)
	})
	if got := len(a.jobs); got != jobs {
		t.Errorf("it started %d jobs to copy a file that is already here", got-jobs)
	}
}

// homedFS is a filesystem whose home is a directory the test owns, so a
// test never writes into whoever is running it's own home.
type homedFS struct {
	vfs.FS
	home string
}

func (f homedFS) Home() (string, error) { return f.home, nil }
func (f homedFS) Name() string          { return "over there" }

// Dropped files go in a directory of their own under the home directory
// of the machine they land on, made if it is not there yet.
func TestDroppedFilesGoUnderHome(t *testing.T) {
	home := t.TempDir()
	fs := homedFS{FS: vfs.NewLocal(), home: home}

	dir, err := pasted.DirOn(fs)
	if err != nil {
		t.Fatalf("work out where: %v", err)
	}

	if want := filepath.Join(home, pasted.DirName); dir != want {
		t.Errorf("it puts them in %q, want %q", dir, want)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Errorf("the directory was not made: %v", err)
	}
	// Asked again, it takes the one that is there rather than failing.
	if again, err := pasted.DirOn(fs); err != nil || again != dir {
		t.Errorf("asked again it gave %q, %v", again, err)
	}
}

// A file dropped on a pane on another machine is copied there, with a
// row on the sidebar saying how far it has got and a cross that stops
// it, and its path is typed once it has arrived.
func TestAFileDroppedOnAPaneElsewhereIsCopiedThere(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	pane := onlyPaneOn(t, a)
	at := aDroppedFile(t, "notes.txt", 4096)
	into := t.TempDir()

	a.uploadOne(vfs.NewLocal(), jobEnd{host: conns.Local}, pane, at, into, nil)

	row := theJobRow(t, a)
	if row.Close == nil {
		t.Error("the row has no way to stop the copy")
	}
	waitFor(t, a, "the path to reach the shell", func() bool {
		return strings.Contains(a.shells[0].sentText(), pasted.DirName) ||
			strings.Contains(a.shells[0].sentText(), "notes.txt")
	})

	// The file is there, and its path is what was typed.
	want := filepath.Join(into, "notes.txt")
	if _, err := os.Stat(want); err != nil {
		t.Errorf("the file was not copied: %v", err)
	}
	if got := a.shells[0].sentText(); !strings.Contains(got, "notes.txt") {
		t.Errorf("it typed %q, want the path of the file it copied", got)
	}
}

// The row of a file dropped on a pane on a server is under that server,
// where the file is going. It used to be under this machine, because
// that is where the file was read.
func TestADroppedFilesRowIsUnderTheMachineItGoesTo(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	pane := onlyPaneOn(t, a)
	at := aDroppedFile(t, "notes.txt", 4096)

	a.uploadOne(vfs.NewLocal(), jobEnd{host: "db"}, pane, at, t.TempDir(), nil)

	if got := theJobRow(t, a).Host; got != "db" {
		t.Errorf("the copy's row is under %q, want db, where the file is going", groupName(got))
	}
}

// Which machine a piece of work is filed under: the one it writes to,
// unless that is this one.
func TestWorkIsFiledUnderTheMachineItWritesTo(t *testing.T) {
	here, db, web := jobEnd{host: conns.Local}, jobEnd{host: "db"}, jobEnd{host: "web"}
	to := vfs.NewLocal()
	for _, c := range []struct {
		what     string
		op       jobs.Op
		from, to jobEnd
		want     string
	}{
		{"a copy up to a server", jobs.Op{Kind: jobs.Copy, To: to}, here, db, "db"},
		{"a copy down from a server", jobs.Op{Kind: jobs.Copy, To: to}, db, here, "db"},
		{"a copy between two servers", jobs.Op{Kind: jobs.Copy, To: to}, db, web, "web"},
		{"a move up to a server", jobs.Op{Kind: jobs.Move, To: to}, here, db, "db"},
		{"a delete on a server", jobs.Op{Kind: jobs.Delete}, db, web, "db"},
		{"a copy on this machine", jobs.Op{Kind: jobs.Copy, To: to}, here, here, conns.Local},
	} {
		if got := jobFiledUnder(c.op, c.from, c.to).host; got != c.want {
			t.Errorf("%s is filed under %q, want %q", c.what, groupName(got), groupName(c.want))
		}
	}
}

// A copy that was stopped types no path: there is nothing at the other
// end to name.
func TestAStoppedCopyTypesNoPath(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	pane := onlyPaneOn(t, a)
	at := aDroppedFile(t, "big.bin", 1<<20)
	into := t.TempDir()
	held := &stalledFS{FS: vfs.NewLocal(), hold: make(chan struct{})}

	a.uploadOne(held, jobEnd{host: conns.Local}, pane, at, into, nil)
	row := theJobRow(t, a)
	if err := row.Close(); err != nil {
		t.Fatalf("stop it: %v", err)
	}
	close(held.hold)

	waitFor(t, a, "the job to stop", func() bool { return len(a.jobs) == 0 })
	for range 20 {
		a.pump.run()
		time.Sleep(time.Millisecond)
	}
	if got := a.shells[0].sentText(); strings.Contains(got, "big.bin") {
		t.Errorf("it typed %q after the copy was stopped", got)
	}
}

// stalledFS holds a write until the test lets it go, so a copy can be
// stopped part way through.
type stalledFS struct {
	vfs.FS
	hold chan struct{}
}

func (f *stalledFS) Create(path string, mode fs.FileMode) (io.WriteCloser, error) {
	<-f.hold
	return f.FS.Create(path, mode)
}

// A drop lands on the pane under the pointer, and on the pane in front
// when it was dropped on something that is not a pane.
func TestAPaneIsFoundUnderThePointer(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withPanel(t, a)
	pane := onlyPaneOn(t, a)
	a.relayout()

	area, ok := a.paneArea(pane)
	if !ok {
		t.Fatal("the pane has no room in the window")
	}
	left, width := a.geo.ColBox(area.X, area.X+area.Cols)
	top, height := a.geo.RowBox(area.Y, area.Y+area.Rows)

	if got := a.paneAt(left+width/2, top+height/2); got != pane {
		t.Errorf("the middle of the pane gave %v, want the pane", got)
	}
	// The sidebar is not a pane.
	side, ok := a.dock.ChildArea(a.side)
	if !ok {
		t.Fatal("the sidebar has no room")
	}
	sideLeft, sideWidth := a.geo.ColBox(side.X, side.X+side.Cols)
	if got := a.paneAt(sideLeft+sideWidth/2, top+height/2); got != nil {
		t.Errorf("a drop on the sidebar gave %v, want no pane", got)
	}
}

// The path lands in the pane the file was dropped on, whatever the user
// has moved on to while it was copying.
//
// A copy of a large file takes a while, and working in another pane
// meanwhile is the ordinary thing to do. A path typed into whatever
// happens to have the keys when it finishes would land in the wrong
// place.
func TestThePathLandsInThePaneItWasDroppedOn(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	dropped := onlyPaneOn(t, a)
	if err := a.openPane(); err != nil {
		t.Fatalf("a second pane: %v", err)
	}
	elsewhere := newestPane(t, a)
	if dropped == elsewhere {
		t.Fatal("the second pane is the first")
	}
	at := aDroppedFile(t, "notes.txt", 4096)
	into := t.TempDir()

	a.uploadOne(vfs.NewLocal(), jobEnd{host: conns.Local}, dropped, at, into, nil)
	// And the user goes to work somewhere else while it copies.
	a.focus(elsewhere)

	waitFor(t, a, "the path to reach a shell", func() bool {
		for _, sh := range a.shells {
			if strings.Contains(sh.sentText(), "notes.txt") {
				return true
			}
		}
		return false
	})

	// The shells are made in the order the panes were, so the first is
	// the pane the file was dropped on and the second is the one the
	// user moved to.
	if got := a.shells[0].sentText(); !strings.Contains(got, "notes.txt") {
		t.Errorf("the pane it was dropped on was sent %q", got)
	}
	if got := a.shells[1].sentText(); strings.Contains(got, "notes.txt") {
		t.Errorf("the pane the user moved to was sent %q", got)
	}
}

// A file that arrives after its pane has closed says where it went.
// Typing the path into a pane nobody can see would lose it.
func TestAFileThatArrivesAfterItsPaneClosedSaysWhere(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	if err := a.openPane(); err != nil {
		t.Fatalf("a second pane: %v", err)
	}
	dropped := newestPane(t, a)
	at := aDroppedFile(t, "notes.txt", 4096)
	into := t.TempDir()
	held := &stalledFS{FS: vfs.NewLocal(), hold: make(chan struct{})}

	a.uploadOne(held, jobEnd{host: conns.Local}, dropped, at, into, nil)
	if err := a.closePane(dropped); err != nil {
		t.Fatalf("close the pane: %v", err)
	}
	close(held.hold)

	n := awaitModal[*ui.Notice](t, a, "where the file went", nil)
	if !strings.Contains(n.Message(), "notes.txt") {
		t.Errorf("it says %q, want it to name the file", n.Message())
	}
}

// A file dropped on a pane running on a machine behind another window
// goes to that machine, not to the window.
//
// Marcus dropped a file on a pane served from one gridterm (Nyli) that
// was itself a shell on a Linux machine that gridterm had reached over
// SSH (Picard). The file went onto Nyli's disk and Nyli's path was typed
// into Picard, where there was no such file.
func TestAFileGoesToTheMachineBehindAWindow(t *testing.T) {
	host, client, addr := twoWindows(t)
	held := windowAt(t, client, addr)

	// The window over there connects to a machine of its own, and opens
	// a shell on it.
	s := sshtest.New(t)
	pinServers(t, host, s)
	saveHost(t, host, "picard", s, "")
	host.refreshServers()
	clickTerminalLine(t, host, "picard")
	waitFor(t, host, "the machine to answer", func() bool {
		return host.machines.named("picard") != nil
	}, client)

	// This window sees it under that machine's name, and works in it.
	var row remoteKey
	waitFor(t, client, "the shell on picard to be published", func() bool {
		host.refreshPanel(panelNow)
		client.refreshPanel(panelNow)
		for _, open := range held.win.Opens() {
			if open.Host == "picard" && open.HasScreen() {
				row = remoteKey{window: held, id: open.ID}
				return true
			}
		}
		return false
	}, host)
	attachFromTheSidebar(t, client, row)
	pane := newestPane(t, client)

	end := client.paneEnd(pane)

	if end.far.window != held {
		t.Errorf("the file would go to %v, want it through the window it is drawn from", end.far.window)
	}
	if end.far.host != "picard" {
		t.Errorf("the file would go to %q, want picard: the shell runs there", end.far.host)
	}
}

// A pane on the window's own machine still goes to that window.
func TestAFileGoesToTheWindowWhenTheShellIsOnIt(t *testing.T) {
	host, client, addr := twoWindows(t)
	held := windowAt(t, client, addr)
	pane, _ := thePaneDrawnFrom(t, client, host, held)

	end := client.paneEnd(pane)

	if end.far.window != nil {
		t.Errorf("the file would go through %v to %q, want the window's own files",
			end.far.window, end.far.host)
	}
	if end.host != held.name {
		t.Errorf("the file would go to %q, want %q", end.host, held.name)
	}
}
