package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/internal/sshtest"
	"github.com/marrasen/gridterm/jobs"
	"github.com/marrasen/gridterm/settings"
	"github.com/marrasen/gridterm/vfs"
)

// What a file pane and a piece of file work are on is the id of the
// saved server, not its name. A name can be given up in a rename and
// given to another machine; the id stays with the server it was given
// to, so nothing that goes by it can be led to a machine it was never on.

// A pane on a server that has left the list reads nothing, even with the
// machine that first had its name still connected.
//
// This is the worst of what a trail of old names did: db renamed to db2,
// another server saved as db and a pane opened on it, that one dropped
// and taken off the list -- and the trail said db was db2, so the pane
// listed and wrote the first machine's files.
func TestAPaneOnARemovedServerDoesNotReachTheOneThatGaveUpItsName(t *testing.T) {
	first, second := sshtest.New(t), sshtest.New(t)
	a := newTestApp(t, 100, 30)
	withDialogs(t, a)
	withPanel(t, a)
	pinServers(t, a, first, second)

	saveHost(t, a, "db", first, "")
	if err := a.connectSaved("db"); err != nil {
		t.Fatalf("connectSaved the first: %v", err)
	}
	waitFor(t, a, "the first machine to answer", func() bool {
		return a.machines.named("db") != nil
	})
	renameSaved(t, a, "db", "db2")

	saveHost(t, a, "db", second, "")
	if err := a.connectSaved("db"); err != nil {
		t.Fatalf("connectSaved the second: %v", err)
	}
	waitFor(t, a, "the second machine to answer", func() bool {
		return a.machines.named("db") != nil
	})
	if openFilesFromThePlus(t, a, "db") == nil {
		t.Fatal("no file pane opened")
	}
	f := reopenersOn(t, a, "db")[0]

	second.CloseClients()
	waitFor(t, a, "the window to see it go", func() bool {
		a.reapExited()
		return a.machines.named("db") == nil
	})
	settleAndClearNotices(t, a)
	if err := a.book.Remove("db"); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := f.ReadDir("/")
		done <- err
	}()
	waitFor(t, a, "the read to come back", func() bool { return len(done) > 0 })
	if err := <-done; err == nil {
		t.Error("a pane on a server that has left the list read something")
	}
	if got := f.Host(); got != "db" {
		t.Errorf("the pane was filed under %q, a machine it was never on", got)
	}
}

// A pane reading through a connection left under the old name follows
// the server once that connection goes.
//
// An edit that renames a server and points it somewhere else leaves the
// connection where it is, because it is to the old address, and the pane
// reads through it. When it drops, the pane is on the server the user
// edited, under the name that server has now.
func TestAPaneFollowsItsServerOnceTheOldConnectionGoes(t *testing.T) {
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
	if a.machines.named("one") == nil {
		t.Fatalf("the connection to the old address moved: %v", a.machines.names())
	}
	if got := f.Host(); got != "one" {
		t.Fatalf("the pane left the connection it reads through for %q", got)
	}

	here.CloseClients()
	waitFor(t, a, "the window to see it go", func() bool {
		a.reapExited()
		return a.machines.named("one") == nil
	})
	settleAndClearNotices(t, a)

	done := make(chan error, 1)
	go func() {
		_, err := f.ReadDir("/")
		done <- err
	}()
	waitFor(t, a, "the read to come back", func() bool { return len(done) > 0 })
	if err := <-done; err != nil {
		t.Fatalf("reading once the old connection went: %v", err)
	}
	if got := f.Host(); got != "two" {
		t.Errorf("the pane is filed under %q, want the name its server has now", got)
	}
	if got := a.machines.named("two"); got == nil || got.at.cfg.Port != port {
		t.Errorf("the pane did not reach its server where it is now: %v", a.machines.names())
	}
}

// Work follows a server that moved and was then renamed, the first time
// it is done again.
//
// The end of the work was taken before either change. Going by what the
// end kept -- a name and an address -- got this wrong: the name was
// given up and the address was old.
func TestWorkFollowsAMachineThatMovedAndWasRenamed(t *testing.T) {
	was, now := sshtest.New(t), sshtest.New(t)
	a := newTestApp(t, 100, 30)
	withDialogs(t, a)
	withPanel(t, a)
	pinServers(t, a, was, now)
	saveHost(t, a, "db", was, "")
	if err := a.connectSaved("db"); err != nil {
		t.Fatalf("connectSaved: %v", err)
	}
	waitFor(t, a, "the machine to answer", func() bool {
		return a.machines.named("db") != nil
	})
	there := openFilesFromThePlus(t, a, "db")
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

	// The server moves. The user points the entry at where it is now,
	// keeping the name, and connects again.
	was.CloseClients()
	waitFor(t, a, "the window to see it go", func() bool {
		a.reapExited()
		return a.machines.named("db") == nil
	})
	settleAndClearNotices(t, a)
	addr, port := now.Host()
	h, ok := a.book.Lookup("db")
	if !ok {
		t.Fatal("db is not saved")
	}
	h.Address, h.Port = addr, port
	if err := a.book.Put(h, "db"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := a.connectSaved("db"); err != nil {
		t.Fatalf("connectSaved where it is now: %v", err)
	}
	waitFor(t, a, "the machine to answer where it is now", func() bool {
		return a.machines.named("db") != nil
	})

	// Then it is renamed.
	renameSaved(t, a, "db", "db2")
	if a.machines.named("db2") == nil {
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
	if got := a.machines.names(); len(got) != 1 || got[0] != "db2" {
		t.Errorf("the window holds %v, want the one machine under its new name", got)
	}
}

// A copy saved before servers had ids is given them the first time it
// runs, and follows a rename after that.
//
// Before the ids, the names are all there is, and the first run is the
// last time they can be trusted to mean what they meant when the copy
// was saved.
func TestAnOldSavedCopyIsGivenIDsAndFollowsARenameAfter(t *testing.T) {
	first, second := sshtest.New(t), sshtest.New(t)
	a, _ := aCopyWindow(t)
	withPanel(t, a)
	pinServers(t, a, first, second)
	saveHost(t, a, "one", first, "")

	from, into := t.TempDir(), t.TempDir()
	putFile(t, from, "one.txt", "the body")
	old := settings.SavedCopy{
		From: "one", At: filepath.ToSlash(from), Into: into, Names: []string{"one.txt"},
	}
	if err := a.copies.keep(old); err != nil {
		t.Fatalf("keep: %v", err)
	}

	runIt := func() {
		t.Helper()
		kept := a.copies.all()
		if len(kept) != 1 {
			t.Fatalf("%d copies kept, want the one", len(kept))
		}
		_ = os.Remove(filepath.Join(into, "one.txt"))
		if err := a.runSavedCopy(kept[0]); err != nil {
			t.Fatalf("run it: %v", err)
		}
		waitFor(t, a, "the copy to happen", func() bool {
			a.refreshJobs()
			_, err := os.Stat(filepath.Join(into, "one.txt"))
			return err == nil
		})
	}
	runIt()
	saved, _ := a.book.Lookup("one")
	if got := a.copies.all()[0].FromID; got != saved.ID {
		t.Fatalf("the copy kept the id %q, want one's (%q)", got, saved.ID)
	}

	// Renamed, and the name it gave up saved for another machine.
	renameSaved(t, a, "one", "two")
	saveHost(t, a, "one", second, "")
	runIt()
	if a.machines.named("one") != nil {
		t.Errorf("the copy went to the server saved since as one: %v", a.machines.names())
	}
	if a.machines.named("two") == nil {
		t.Errorf("the copy did not go to the server it was saved on: %v", a.machines.names())
	}
}

// Work on a server is not done through a connection to another one that
// happens to hold its name.
//
// An edit that renames a server and points it somewhere else leaves the
// connection under the old name. A server saved since under that name is
// another server, and the connection is not to it.
func TestWorkIsNotDoneThroughAnotherServersConnection(t *testing.T) {
	here, elsewhere := sshtest.New(t), sshtest.New(t)
	a := newTestApp(t, 100, 30)
	withDialogs(t, a)
	withPanel(t, a)
	pinServers(t, a, here, elsewhere)
	saveHost(t, a, "db", here, "")
	if err := a.connectSaved("db"); err != nil {
		t.Fatalf("connectSaved: %v", err)
	}
	waitFor(t, a, "the machine to answer", func() bool {
		return a.machines.named("db") != nil
	})

	// Renamed and pointed somewhere else: the connection stays as db.
	addr, port := elsewhere.Host()
	h, _ := a.book.Lookup("db")
	h.Name, h.Address, h.Port = "db2", addr, port
	if err := a.book.Put(h, "db"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	a.renamedMachine("db", h)
	if a.machines.named("db") == nil {
		t.Fatalf("the connection moved: %v", a.machines.names())
	}

	// Another server saved as db, and work on it.
	saveHost(t, a, "db", elsewhere, "")
	other, _ := a.book.Lookup("db")
	end := jobEnd{host: "db", at: step{name: "db", id: other.ID}}

	var opened vfs.FS
	answered := false
	a.openEndAgain(end, func(f vfs.FS, err error) {
		opened, answered = f, true
		if err == nil {
			t.Error("work on one server was opened through another's connection")
		}
	})
	if !answered {
		t.Fatal("the end was not answered straight away")
	}
	if opened != nil {
		_ = opened.Close()
	}
}
