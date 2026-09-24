package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/internal/sshtest"
	"github.com/marrasen/gridterm/jobs"
	"github.com/marrasen/gridterm/remote"
	"github.com/marrasen/gridterm/ui/files"
	"github.com/marrasen/gridterm/vfs"
)

// Step 2 of the plan: everything else a file pane does goes through the
// same wrapper, so each of them opens the machine again. Each has its
// own failure path in the browser, so each is asked for here the way
// the user asks for it.

// aPaneOnADroppedMachine is a file pane pointed at a directory the test
// owns, on a machine whose connection has gone.
func aPaneOnADroppedMachine(t *testing.T) (a *testApp, s *sshtest.Server, dir string, pane *files.Pane) {
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
	return a, s, dir, pane
}

// Making a directory after the connection went opens the machine again
// and makes it.
func TestMakingADirectoryAfterTheDropOpensTheMachineAgain(t *testing.T) {
	a, _, dir, pane := aPaneOnADroppedMachine(t)

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
	a, s, dir, pane := aPaneOnADroppedMachine(t)
	putFile(t, dir, "was.txt", "hello")
	sessions := s.SFTPs()

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
	// One session between the two asks, not one each: the filesystem
	// keeps what it opened for the first.
	if got := s.SFTPs() - sessions; got != 1 {
		t.Errorf("the rename opened %d sessions on the machine, want the one", got)
	}
}

// Reading a file after the connection went opens the machine again and
// shows it.
func TestReadingAFileAfterTheDropOpensTheMachineAgain(t *testing.T) {
	a, _, dir, pane := aPaneOnADroppedMachine(t)
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
	a, _, dir, pane := aPaneOnADroppedMachine(t)
	putFile(t, dir, "gone.txt", "take me")
	host := a.hostOf(pane.FS())

	a.startJob(jobs.Delete, files.Work{
		From: pane, At: filepath.ToSlash(dir), Names: []string{"gone.txt"},
	})
	// The row, taken while the work is still on the window's list.
	row := theJobRow(t, a)
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
	if row.Host != host {
		t.Errorf("the work is filed under %q, want %q", row.Host, host)
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

// Step 3: a copy repeated after the connection went opens the machine
// again by itself.

// A saved copy run again after the machine it reads went connects to it
// and copies.
func TestASavedCopyRunAgainOpensTheMachine(t *testing.T) {
	a, s, host := aConnectedWindow(t, 100, 30)
	there := openFilesFromThePlus(t, a, host)
	here := openFilesFromThePlus(t, a, conns.Local)
	if there == nil || here == nil {
		t.Fatal("no file panes opened")
	}
	from, into := t.TempDir(), t.TempDir()
	putFile(t, from, "one.txt", "the body")
	openAt(t, a, there, filepath.ToSlash(from))
	openAt(t, a, here, into)

	op := jobs.Op{
		Kind: jobs.Copy, At: filepath.ToSlash(from), Into: into,
		Names: []string{"one.txt"},
	}
	fromEnd, toEnd := a.endOf(there), a.endOf(here)

	s.CloseClients()
	waitFor(t, a, "the window to see the machine go", func() bool {
		a.reapExited()
		return a.machines.named(host) == nil
	})
	settleAndClearNotices(t, a)

	a.repeatSavedCopy(op, fromEnd, toEnd)
	waitFor(t, a, "the copy to start", func() bool { return len(a.jobs) == 1 })
	// The row, taken while the work is still on the window's list.
	row := theJobRow(t, a)
	waitFor(t, a, "the copy to happen", func() bool {
		a.refreshJobs()
		_, err := os.Stat(filepath.Join(into, "one.txt"))
		return err == nil
	})
	if a.machines.named(host) == nil {
		t.Error("the machine was not opened again")
	}
	if up := a.root.Modal(); up != nil {
		t.Errorf("a dialog complained about it: %T", up)
	}
	// The row is filed under the machine, not under this one.
	if row.Host != host {
		t.Errorf("the work is filed under %q, want %q", row.Host, host)
	}
}

// A copy between two machines that both went opens both of them.
func TestACopyBetweenTwoGoneMachinesOpensBoth(t *testing.T) {
	near, far := sshtest.New(t), sshtest.New(t)
	a := newTestApp(t, 100, 30)
	withDialogs(t, a)
	withPanel(t, a)
	pinServers(t, a, near, far)
	saveHost(t, a, "one", near, "")
	saveHost(t, a, "two", far, "")
	for _, name := range []string{"one", "two"} {
		if err := a.connectSaved(name); err != nil {
			t.Fatalf("connectSaved %s: %v", name, err)
		}
	}
	waitFor(t, a, "both machines to answer", func() bool {
		return a.machines.named("one") != nil && a.machines.named("two") != nil
	})
	source := openFilesFromThePlus(t, a, "one")
	sink := openFilesFromThePlus(t, a, "two")
	if source == nil || sink == nil {
		t.Fatal("no file panes opened")
	}
	from, into := t.TempDir(), t.TempDir()
	putFile(t, from, "one.txt", "the body")
	openAt(t, a, source, filepath.ToSlash(from))
	openAt(t, a, sink, filepath.ToSlash(into))

	op := jobs.Op{
		Kind: jobs.Copy, At: filepath.ToSlash(from), Into: filepath.ToSlash(into),
		Names: []string{"one.txt"},
	}
	fromEnd, toEnd := a.endOf(source), a.endOf(sink)

	near.CloseClients()
	far.CloseClients()
	waitFor(t, a, "the window to see both machines go", func() bool {
		a.reapExited()
		return a.machines.named("one") == nil && a.machines.named("two") == nil
	})
	settleAndClearNotices(t, a)

	a.repeatSavedCopy(op, fromEnd, toEnd)
	waitFor(t, a, "the copy to happen", func() bool {
		a.refreshJobs()
		_, err := os.Stat(filepath.Join(into, "one.txt"))
		return err == nil
	})
	if a.machines.named("one") == nil || a.machines.named("two") == nil {
		t.Errorf("only %v were opened again", a.machines.names())
	}
	if up := a.root.Modal(); up != nil {
		t.Errorf("a dialog complained about it: %T", up)
	}
}

// A repeat after the machine was renamed goes to the machine, not to a
// second login under the name the work remembers.
//
// Work keeps the name its machine had when it started, and the name is
// all it has to go on. Dialling under the old one puts a second group
// on the sidebar for a machine already there.
func TestARepeatAfterARenameGoesToTheOneMachine(t *testing.T) {
	s := sshtest.New(t)
	a := newTestApp(t, 100, 30)
	withDialogs(t, a)
	withPanel(t, a)
	pinServers(t, a, s)
	saveHost(t, a, "one", s, "")
	if err := a.connectSaved("one"); err != nil {
		t.Fatalf("connectSaved: %v", err)
	}
	waitFor(t, a, "the machine to answer", func() bool {
		return a.machines.named("one") != nil
	})
	there := openFilesFromThePlus(t, a, "one")
	here := openFilesFromThePlus(t, a, conns.Local)
	if there == nil || here == nil {
		t.Fatal("no file panes opened")
	}
	from, into := t.TempDir(), t.TempDir()
	putFile(t, from, "one.txt", "the body")
	openAt(t, a, there, filepath.ToSlash(from))
	openAt(t, a, here, into)

	// The ends as the work took them, before the rename.
	op := jobs.Op{
		Kind: jobs.Copy, At: filepath.ToSlash(from), Into: into,
		Names: []string{"one.txt"},
	}
	fromEnd, toEnd := a.endOf(there), a.endOf(here)
	renameSaved(t, a, "one", "two")
	if a.machines.named("two") == nil {
		t.Fatalf("the connection did not follow the rename: %v", a.machines.names())
	}

	a.repeatSavedCopy(op, fromEnd, toEnd)
	waitFor(t, a, "the copy to happen", func() bool {
		a.refreshJobs()
		_, err := os.Stat(filepath.Join(into, "one.txt"))
		return err == nil
	})
	if got := a.machines.names(); len(got) != 1 || got[0] != "two" {
		t.Errorf("the window holds %v, want the one machine under its new name", got)
	}
	if up := a.root.Modal(); up != nil {
		t.Errorf("a dialog complained about it: %T", up)
	}
}

// Pressing Repeat twice while the machine is being opened again starts
// one copy, not two.
//
// It used to be instant, so a second press was a second copy the user
// had asked for. A repeat that has to open a machine takes as long as a
// connection does, and the press in that time is the user wondering
// whether the first one registered.
func TestPressingRepeatTwiceWhileReconnectingStartsOneCopy(t *testing.T) {
	a, s, host := aConnectedWindow(t, 100, 30)
	there := openFilesFromThePlus(t, a, host)
	here := openFilesFromThePlus(t, a, conns.Local)
	if there == nil || here == nil {
		t.Fatal("no file panes opened")
	}
	from, into := t.TempDir(), t.TempDir()
	putFile(t, from, "one.txt", "the body")
	a.focus(there)
	openAt(t, a, there, filepath.ToSlash(from))
	openAt(t, a, here, into)

	copyTheFirstFile(t, a, a.files.view)
	e := theJobRow(t, a)
	waitFor(t, a, "the copy to finish", func() bool {
		a.refreshJobs()
		return len(a.jobs) == 0
	})
	openTheRow(t, a, e)
	d := theJobPane(t, a)

	s.CloseClients()
	waitFor(t, a, "the window to see the machine go", func() bool {
		a.reapExited()
		return a.machines.named(host) == nil
	})
	settleAndClearNotices(t, a)

	// What the first copy made goes, so the copy done again is the one
	// that puts it back.
	if err := os.Remove(filepath.Join(into, "one.txt")); err != nil {
		t.Fatalf("clear what the first copy made: %v", err)
	}
	rows := copyRows(a)

	// Two presses before the window has had a frame to answer the
	// first, which is what a machine being opened again leaves room for.
	pressChoice(t, d, btnRepeat)
	pressChoice(t, d, btnRepeat)
	waitFor(t, a, "the copy to be done again", func() bool {
		a.refreshJobs()
		_, err := os.Stat(filepath.Join(into, "one.txt"))
		return err == nil
	})
	settleAndClearNotices(t, a)
	if up := a.root.Modal(); up != nil {
		t.Errorf("a second copy asked about the file the first wrote: %T", up)
	}
	if got := copyRows(a) - rows; got != 1 {
		t.Errorf("the two presses put %d copies on the sidebar, want the one", got)
	}
}

// copyRows is how many copies the sidebar is showing, finished or not.
func copyRows(a *testApp) int {
	n := 0
	for _, group := range a.registry.Groups(time.Now()) {
		for _, row := range group.Rows {
			if row.Entry != nil && row.Entry.Kind == conns.Copy {
				n++
			}
		}
	}
	return n
}

// A name given to another machine since the rename stands for that
// machine, so work that remembers the name is left pointing at it.
//
// The rename trail says what a machine used to be called. It must not
// outlive the name: a user who renames one server and saves another
// under the name it gave up has said which machine that name means.
func TestARepeatGoesToTheMachineThatHasTheNameNow(t *testing.T) {
	first, second := sshtest.New(t), sshtest.New(t)
	a := newTestApp(t, 100, 30)
	withDialogs(t, a)
	withPanel(t, a)
	pinServers(t, a, first, second)
	saveHost(t, a, "one", first, "")
	if err := a.connectSaved("one"); err != nil {
		t.Fatalf("connectSaved: %v", err)
	}
	waitFor(t, a, "the machine to answer", func() bool {
		return a.machines.named("one") != nil
	})
	there := openFilesFromThePlus(t, a, "one")
	here := openFilesFromThePlus(t, a, conns.Local)
	if there == nil || here == nil {
		t.Fatal("no file panes opened")
	}
	from, into := t.TempDir(), t.TempDir()
	putFile(t, from, "one.txt", "the body")
	openAt(t, a, there, filepath.ToSlash(from))
	openAt(t, a, here, into)
	fromEnd, toEnd := a.endOf(there), a.endOf(here)
	op := jobs.Op{
		Kind: jobs.Copy, At: filepath.ToSlash(from), Into: into,
		Names: []string{"one.txt"},
	}

	// The machine is renamed, and the name it gave up is saved for
	// another machine.
	renameSaved(t, a, "one", "two")
	saveHost(t, a, "one", second, "")

	a.repeatSavedCopy(op, fromEnd, toEnd)
	waitFor(t, a, "the copy to happen", func() bool {
		a.refreshJobs()
		_, err := os.Stat(filepath.Join(into, "one.txt"))
		return err == nil
	})
	// It went to the machine the name stands for now.
	m := a.machines.named("one")
	if m == nil {
		t.Fatalf("the window holds %v, want the machine called one", a.machines.names())
	}
	if _, port := second.Host(); m.at.cfg.Port != port {
		t.Errorf("one is connected to port %d, want the machine saved under that name (%d)",
			m.at.cfg.Port, port)
	}
}

// A machine renamed only by its letters' case is still followed.
//
// The server list does not tell two names apart by case, so the machine
// finds itself under the name it gave up. Left there, a repeat would
// say nothing is connected to Prod while prod sat connected.
func TestARepeatFollowsARenameThatOnlyChangesTheCase(t *testing.T) {
	s := sshtest.New(t)
	a := newTestApp(t, 100, 30)
	withDialogs(t, a)
	withPanel(t, a)
	pinServers(t, a, s)
	saveHost(t, a, "Prod", s, "")
	if err := a.connectSaved("Prod"); err != nil {
		t.Fatalf("connectSaved: %v", err)
	}
	waitFor(t, a, "the machine to answer", func() bool {
		return a.machines.named("Prod") != nil
	})
	there := openFilesFromThePlus(t, a, "Prod")
	here := openFilesFromThePlus(t, a, conns.Local)
	if there == nil || here == nil {
		t.Fatal("no file panes opened")
	}
	from, into := t.TempDir(), t.TempDir()
	putFile(t, from, "one.txt", "the body")
	openAt(t, a, there, filepath.ToSlash(from))
	openAt(t, a, here, into)
	op := jobs.Op{
		Kind: jobs.Copy, At: filepath.ToSlash(from), Into: into,
		Names: []string{"one.txt"},
	}
	fromEnd, toEnd := a.endOf(there), a.endOf(here)

	renameSaved(t, a, "Prod", "prod")
	if a.machines.named("prod") == nil {
		t.Fatalf("the connection did not follow the rename: %v", a.machines.names())
	}

	a.repeatSavedCopy(op, fromEnd, toEnd)
	waitFor(t, a, "the copy to happen", func() bool {
		a.refreshJobs()
		_, err := os.Stat(filepath.Join(into, "one.txt"))
		return err == nil
	})
	if up := a.root.Modal(); up != nil {
		t.Errorf("a dialog complained about it: %T", up)
	}
	if got := a.machines.names(); len(got) != 1 || got[0] != "prod" {
		t.Errorf("the window holds %v, want the one machine", got)
	}
}

// A machine renamed while it is being connected to is followed too.
//
// The dial lands under the new name, so a file pane or a finished copy
// left under the old one asks for a machine nothing is connected to and
// dials the same box a second time under a name the window has stopped
// using.
func TestARenameCaughtMidDialIsFollowed(t *testing.T) {
	a, s, host := aConnectedWindow(t, 100, 30)
	pane := openFilesFromThePlus(t, a, host)
	if pane == nil {
		t.Fatal("no file pane opened")
	}
	held := reopenersOn(t, a, host)
	if len(held) != 1 {
		t.Fatalf("%d filesystems hold %s", len(held), host)
	}
	f := held[0]

	s.CloseClients()
	waitFor(t, a, "the window to see the machine go", func() bool {
		a.reapExited()
		return a.machines.named(host) == nil
	})
	settleAndClearNotices(t, a)

	// A connection to it on its way, and the user renames it while that
	// is happening.
	stuck := &dialling{names: []string{host}, cancel: func() {}}
	holdTheNames(t, a, stuck)
	addr, port := s.Host()
	a.renamedMachine(host, remote.Host{
		Name: "renamed", Address: addr, Port: port, User: "tester",
	})

	if got := f.Host(); got != "renamed" {
		t.Errorf("the pane's filesystem is filed under %q, want the name it has now", got)
	}
	if got := a.renamed[host]; got != "renamed" {
		t.Errorf("the work on it would still look for %q", host)
	}
	if a.machines.connecting("renamed") == nil {
		t.Errorf("the dial is still held under the old name: %v", a.machines.reaching())
	}
}
