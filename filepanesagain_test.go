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
	addr, port := s.Host()
	stuck := &dialling{
		names:  []string{host},
		cancel: func() {},
		route:  []step{{name: host, cfg: serverConfig(t, s)}},
	}
	holdTheNames(t, a, stuck)
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

// A machine renamed while nothing is connected to it is followed.
//
// A file pane outlives its connection, so this is the likeliest moment
// to rename a machine: the user sees the pane, sees the row greyed, and
// tidies up the server list. Left under the old name, the pane's next
// click dials the same box under a name the list has stopped using.
func TestARenameWhileTheMachineIsDroppedIsFollowed(t *testing.T) {
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
	pane := openFilesFromThePlus(t, a, "one")
	if pane == nil {
		t.Fatal("no file pane opened")
	}
	held := reopenersOn(t, a, "one")
	if len(held) != 1 {
		t.Fatalf("%d filesystems hold one", len(held))
	}
	f := held[0]

	s.CloseClients()
	waitFor(t, a, "the window to see the machine go", func() bool {
		a.reapExited()
		return a.machines.named("one") == nil
	})
	settleAndClearNotices(t, a)

	renameSaved(t, a, "one", "two")
	if got := f.Host(); got != "two" {
		t.Errorf("the pane's filesystem is filed under %q, want the name it has now", got)
	}

	// And a read opens it under that name, rather than a second group
	// for the same box under the name the list has given up.
	done := make(chan error, 1)
	go func() {
		_, err := f.ReadDir("/")
		done <- err
	}()
	waitFor(t, a, "the read to come back", func() bool { return len(done) > 0 })
	if err := <-done; err != nil {
		t.Fatalf("reading after the rename: %v", err)
	}
	if got := a.machines.names(); len(got) != 1 || got[0] != "two" {
		t.Errorf("the window holds %v, want the one machine under its new name", got)
	}
}

// A rename that changes the address as well is a different machine
// under that name, so nothing follows it.
//
// The connection has always been left where it is for this. The panes,
// the work and a dial still on its way follow the same rule: the name
// now stands for somewhere else, and a pane reading the old machine is
// not reading that one.
func TestARenameThatChangesTheAddressIsNotFollowed(t *testing.T) {
	here, elsewhere := sshtest.New(t), sshtest.New(t)
	a := newTestApp(t, 100, 30)
	withDialogs(t, a)
	withPanel(t, a)
	pinServers(t, a, here, elsewhere)
	saveHost(t, a, "one", here, "")
	if err := a.connectSaved("one"); err != nil {
		t.Fatalf("connectSaved: %v", err)
	}
	waitFor(t, a, "the machine to answer", func() bool {
		return a.machines.named("one") != nil
	})
	if openFilesFromThePlus(t, a, "one") == nil {
		t.Fatal("no file pane opened")
	}
	f := reopenersOn(t, a, "one")[0]

	here.CloseClients()
	waitFor(t, a, "the window to see the machine go", func() bool {
		a.reapExited()
		return a.machines.named("one") == nil
	})
	settleAndClearNotices(t, a)

	// The entry keeps its name and is pointed somewhere else.
	addr, port := elsewhere.Host()
	h, ok := a.book.Lookup("one")
	if !ok {
		t.Fatal("one is not saved")
	}
	h.Name, h.Address, h.Port = "two", addr, port
	if err := a.book.Put(h, "one"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	a.renamedMachine("one", h)

	if got := f.Host(); got != "one" {
		t.Errorf("the pane's filesystem is filed under %q, want the machine it is reading", got)
	}
	if now, followed := a.renamed["one"]; followed {
		t.Errorf("work on one would be sent to %q, which is somewhere else", now)
	}
}

// A dial renamed once is still checked against the address the second
// time.
//
// A dial carries the route it is on, and that is what says whether a
// rename is the same machine. Left spelling the name the route started
// with, the second rename finds nothing to check against and the dial
// follows a name to an address that is somewhere else -- taking the
// panes and the finished work with it.
func TestADialRenamedTwiceIsCheckedBothTimes(t *testing.T) {
	here, elsewhere := sshtest.New(t), sshtest.New(t)
	a := newTestApp(t, 100, 30)
	withDialogs(t, a)
	withPanel(t, a)
	pinServers(t, a, here, elsewhere)
	saveHost(t, a, "one", here, "")

	stuck := &dialling{
		names:  []string{"one"},
		cancel: func() {},
		route:  []step{{name: "one", cfg: serverConfig(t, here)}},
	}
	holdTheNames(t, a, stuck)

	// The first rename keeps the address, so the dial follows it.
	renameSaved(t, a, "one", "two")
	if a.machines.connecting("two") != stuck {
		t.Fatalf("the dial did not follow the first rename: %v", a.machines.reaching())
	}

	// A second rename that keeps the address is followed too, which is
	// what the dial needs its route spelled the current way for.
	renameSaved(t, a, "two", "twice")
	if a.machines.connecting("twice") != stuck {
		t.Fatalf("the dial did not follow the second rename: %v", a.machines.reaching())
	}

	// The next one points the entry somewhere else. The dial is on its
	// way to the first machine and stays where it is.
	addr, port := elsewhere.Host()
	h, ok := a.book.Lookup("twice")
	if !ok {
		t.Fatal("twice is not saved")
	}
	h.Name, h.Address, h.Port = "three", addr, port
	if err := a.book.Put(h, "twice"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	a.renamedMachine("twice", h)

	if a.machines.connecting("three") != nil {
		t.Error("the dial followed a rename onto another machine's address")
	}
	if a.machines.connecting("twice") != stuck {
		t.Errorf("the dial is no longer held under the name it was reaching: %v", a.machines.reaching())
	}
	if now, followed := a.renamed["twice"]; followed {
		t.Errorf("work on twice would be sent to %q, which is somewhere else", now)
	}
}

// A machine that moved and was reconnected to is renamed by where it is
// now, not by where it was when the pane opened.
//
// What the filesystem kept is the only record of where a machine is
// when no list has a route to it, so it has to be the machine it last
// reached. Left as it was when the pane opened, a rename after the
// address changed would be read as a rename onto somewhere else and
// nothing would follow it.
func TestAMachineThatMovedIsRenamedByWhereItIsNow(t *testing.T) {
	was, now := sshtest.New(t), sshtest.New(t)
	a := newTestApp(t, 100, 30)
	withDialogs(t, a)
	withPanel(t, a)
	pinServers(t, a, was, now)
	saveHost(t, a, "one", was, "")
	if err := a.connectSaved("one"); err != nil {
		t.Fatalf("connectSaved: %v", err)
	}
	waitFor(t, a, "the machine to answer", func() bool {
		return a.machines.named("one") != nil
	})
	if openFilesFromThePlus(t, a, "one") == nil {
		t.Fatal("no file pane opened")
	}
	f := reopenersOn(t, a, "one")[0]

	was.CloseClients()
	waitFor(t, a, "the window to see it go", func() bool {
		a.reapExited()
		return a.machines.named("one") == nil
	})
	settleAndClearNotices(t, a)

	// The server moved, and the entry is pointed at where it is now.
	// The name is untouched, so nothing tells the pane anything.
	addr, port := now.Host()
	h, ok := a.book.Lookup("one")
	if !ok {
		t.Fatal("one is not saved")
	}
	h.Address, h.Port = addr, port
	if err := a.book.Put(h, "one"); err != nil {
		t.Fatalf("Put: %v", err)
	}

	// A read takes the pane to where the machine is now.
	done := make(chan error, 1)
	go func() {
		_, err := f.ReadDir("/")
		done <- err
	}()
	waitFor(t, a, "the read to come back", func() bool { return len(done) > 0 })
	if err := <-done; err != nil {
		t.Fatalf("reading after the machine moved: %v", err)
	}
	if got := a.machines.named("one"); got == nil || got.at.cfg.Port != port {
		t.Fatalf("the pane did not reach the machine where it is now: %v", a.machines.names())
	}

	// It goes again, and is renamed. That is the same machine, so the
	// pane and the work follow it.
	now.CloseClients()
	waitFor(t, a, "the window to see it go again", func() bool {
		a.reapExited()
		return a.machines.named("one") == nil
	})
	settleAndClearNotices(t, a)

	renameSaved(t, a, "one", "two")
	if got := f.Host(); got != "two" {
		t.Errorf("the pane's filesystem is filed under %q, want the name it has now", got)
	}
}

// A filesystem keeps the machine it was opened on, not whatever wears
// its old name by the time the connection lands.
//
// A name given up in a rename can be saved for somewhere else while the
// dial is still running. Looking the machine up again by the name the
// read asked with would file another machine's address in the pane, and
// a repeat with no list entry to go on would open that one.
func TestAReconnectKeepsTheMachineItOpened(t *testing.T) {
	mine, other := sshtest.New(t), sshtest.New(t)
	a := newTestApp(t, 100, 30)
	withDialogs(t, a)
	withPanel(t, a)
	pinServers(t, a, mine, other)
	saveHost(t, a, "one", mine, "")
	if err := a.connectSaved("one"); err != nil {
		t.Fatalf("connectSaved: %v", err)
	}
	waitFor(t, a, "the machine to answer", func() bool {
		return a.machines.named("one") != nil
	})
	// A filesystem on it, held the way a job's is: no pane, so nothing
	// reads through it except this test.
	opened, err := a.filesystem("one")
	if err != nil {
		t.Fatalf("open a filesystem on one: %v", err)
	}
	f, is := opened.(*reopening)
	if !is {
		t.Fatalf("the filesystem is a %T, not one that holds the machine", opened)
	}

	// The machine is set aside rather than closed, so it can answer the
	// dial further down while still being the machine it always was.
	was := a.machines.named("one")
	delete(a.machines.held, "one")
	f.Lost()

	// A connection to it on its way, and the read queues behind that.
	stuck := &dialling{
		names:  []string{"one"},
		cancel: func() {},
		route:  []step{{name: "one", cfg: serverConfig(t, mine)}},
	}
	holdTheNames(t, a, stuck)
	done := make(chan error, 1)
	go func() {
		_, err := f.ReadDir("/")
		done <- err
	}()
	waitFor(t, a, "the read to queue behind it", func() bool {
		return len(stuck.answering) > 0
	})

	// While it runs: the machine is renamed, and the name it gave up is
	// saved for a different machine and connected.
	renameSaved(t, a, "one", "two")
	if got := f.Host(); got != "two" {
		t.Fatalf("the filesystem is filed under %q after the rename", got)
	}
	saveHost(t, a, "one", other, "")
	if err := a.connectSaved("one"); err != nil {
		t.Fatalf("connectSaved the other machine: %v", err)
	}
	waitFor(t, a, "the other machine to answer", func() bool {
		return a.machines.named("one") != nil
	})

	// Then the connection lands, under the name the machine has now.
	was.at.name = "two"
	a.machines.take(was)
	a.machines.settle(stuck, true)

	waitFor(t, a, "the read to come back", func() bool { return len(done) > 0 })
	if err := <-done; err != nil {
		t.Fatalf("reading after the old name was given to another machine: %v", err)
	}

	// What it kept is its own machine, not the one wearing its old name.
	_, port := mine.Host()
	if got := f.step().cfg.Port; got != port {
		t.Errorf("the filesystem kept port %d, want its own machine's %d", got, port)
	}
	if got := f.step().name; got != "two" {
		t.Errorf("the step it kept is named %q, want the name the machine has now", got)
	}
}
