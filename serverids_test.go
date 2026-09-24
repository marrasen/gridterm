package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/internal/sshtest"
	"github.com/marrasen/gridterm/jobs"
	"github.com/marrasen/gridterm/settings"
	"github.com/marrasen/gridterm/ui"
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
	err := <-done
	if err == nil {
		t.Error("a pane on a server that has left the list read something")
	} else if !strings.Contains(err.Error(), "removed") {
		t.Errorf("the read says %q, want it to say the server was removed", err)
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

// A copy saved before servers had ids is given them when a window
// opens, and follows a rename after that.
//
// Before the ids, the names are all there is, and the sooner they are
// turned into ids the likelier they still mean what they meant when the
// copy was saved.
func TestAnOldSavedCopyIsGivenIDsAndFollowsARenameAfter(t *testing.T) {
	first, second := sshtest.New(t), sshtest.New(t)
	a, path := aCopyWindow(t)
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
	// The settings read again, the way a window opening reads them.
	if _, err := withSettings(t, a, path); err != nil {
		t.Fatalf("settings: %v", err)
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
	saved, _ := a.book.Lookup("one")
	if got := a.copies.all()[0].FromID; got != saved.ID {
		t.Fatalf("the copy was given the id %q, want one's (%q)", got, saved.ID)
	}
	runIt()

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

// A pane that follows its server to a new name leaves a pane on another
// server under the old name where it is.
//
// The old name stands for two machines here: the pane left under it
// when its server was renamed and pointed somewhere else, and a pane on
// a server saved since under that name. Moving by the name took both.
func TestFollowingARenameLeavesTheOtherServersPaneAlone(t *testing.T) {
	here, elsewhere, other := sshtest.New(t), sshtest.New(t), sshtest.New(t)
	a := newTestApp(t, 100, 30)
	withDialogs(t, a)
	withPanel(t, a)
	pinServers(t, a, here, elsewhere, other)
	saveHost(t, a, "db", here, "")
	if err := a.connectSaved("db"); err != nil {
		t.Fatalf("connectSaved: %v", err)
	}
	waitFor(t, a, "the machine to answer", func() bool {
		return a.machines.named("db") != nil
	})
	first := openFilesFromThePlus(t, a, "db")
	if first == nil {
		t.Fatal("no file pane opened")
	}
	mine := first.FS().(*reopening)

	// A copy done from it, finished, and its row filed under db.
	from, into := t.TempDir(), t.TempDir()
	putFile(t, from, "one.txt", "the body")
	a.runJob(jobs.Op{
		Kind: jobs.Copy, From: mine, At: filepath.ToSlash(from),
		Names: []string{"one.txt"}, To: vfs.NewLocal(), Into: into,
	}, a.endOf(first), jobEnd{host: conns.Local}, nil)
	jobRow := theJobRow(t, a)
	waitFor(t, a, "the copy to finish", func() bool {
		a.refreshJobs()
		return len(a.jobs) == 0
	})

	// Renamed and pointed somewhere else: the connection, and the pane
	// reading through it, stay as db. Then that connection goes.
	addr, port := elsewhere.Host()
	h, _ := a.book.Lookup("db")
	h.Name, h.Address, h.Port = "db2", addr, port
	if err := a.book.Put(h, "db"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	a.renamedMachine("db", h)
	here.CloseClients()
	waitFor(t, a, "the window to see it go", func() bool {
		a.reapExited()
		return a.machines.named("db") == nil
	})
	settleAndClearNotices(t, a)

	// Another server saved as db, a pane on it, and that goes too.
	saveHost(t, a, "db", other, "")
	if err := a.connectSaved("db"); err != nil {
		t.Fatalf("connectSaved the other: %v", err)
	}
	waitFor(t, a, "the other machine to answer", func() bool {
		return a.machines.named("db") != nil
	})
	second := openFilesFromThePlus(t, a, "db")
	if second == nil || second == first {
		t.Fatal("no second file pane opened")
	}
	theirs := second.FS().(*reopening)
	other.CloseClients()
	waitFor(t, a, "the window to see the other go", func() bool {
		a.reapExited()
		return a.machines.named("db") == nil
	})
	settleAndClearNotices(t, a)

	// The first pane is read, and follows its server to db2.
	done := make(chan error, 1)
	go func() {
		_, err := mine.ReadDir("/")
		done <- err
	}()
	waitFor(t, a, "the read to come back", func() bool { return len(done) > 0 })
	if err := <-done; err != nil {
		t.Fatalf("reading the first pane: %v", err)
	}
	if got := mine.Host(); got != "db2" {
		t.Errorf("the first pane is filed under %q, want db2", got)
	}

	// The other server's pane is still db, by every name it has.
	if got := theirs.Host(); got != "db" {
		t.Errorf("the other server's filesystem is filed under %q, want db", got)
	}
	if got := theirs.Name(); got != "db" {
		t.Errorf("the other server's pane is titled %q, want db", got)
	}
	if row := a.files.rows[second]; row == nil || row.Host != "db" {
		t.Errorf("the other server's row is under %v, want db", row)
	}
	// And the finished copy went with its server.
	if jobRow.Host != "db2" {
		t.Errorf("the copy done from the first pane is under %q, want db2", jobRow.Host)
	}
}

// A filesystem closed but not yet off the window's list is not counted
// as another machine under its name.
//
// Closing takes it off the list a frame later. Counted until then, a
// pane that has gone would make a rename move only part of what it
// should.
func TestAClosedFilesystemDoesNotShareAName(t *testing.T) {
	a := newTestApp(t, 80, 24)
	gone := newReopening(a.app, "db", step{name: "db", id: "theirs"}, nil)
	a.keepReopening(gone)
	if !a.sharesTheName("db", "mine") {
		t.Fatal("a filesystem on another server under the name is not counted")
	}
	_ = gone.Close()
	if a.sharesTheName("db", "mine") {
		t.Error("a closed filesystem is still counted")
	}
}

// A saved command runs on the server it was saved on, under the name
// that server has now, and not on a server saved since under its old
// name. Its palette row says the name it has now.
func TestASavedCommandFollowsItsServer(t *testing.T) {
	first, second := sshtest.New(t), sshtest.New(t)
	a, _ := aCopyWindow(t)
	withPanel(t, a)
	pinServers(t, a, first, second)
	saveHost(t, a, "one", first, "")
	one, _ := a.book.Lookup("one")
	cmd := settings.SavedCommand{Line: "true", Host: "one", HostID: one.ID}
	if err := a.saved.keep(cmd); err != nil {
		t.Fatalf("keep: %v", err)
	}

	renameSaved(t, a, "one", "two")
	saveHost(t, a, "one", second, "")
	a.refreshServers()
	if !hasCommandTitled(a, "Run true on two") {
		t.Error("the palette does not offer the command on two")
	}

	if err := a.runSaved(a.saved.all()[0]); err != nil {
		t.Fatalf("runSaved: %v", err)
	}
	waitFor(t, a, "the machine to answer", func() bool {
		return a.machines.named("two") != nil || a.machines.named("one") != nil
	})
	if a.machines.named("one") != nil {
		t.Errorf("the command went to the server saved since as one: %v", a.machines.names())
	}
	if _, port := first.Host(); a.machines.named("two") == nil ||
		a.machines.named("two").at.cfg.Port != port {
		t.Errorf("the command did not go to the server it was saved on: %v", a.machines.names())
	}
}

// A saved tunnel is offered on its server after a rename, and one whose
// server was removed says so rather than opening anywhere.
func TestASavedTunnelFollowsItsServer(t *testing.T) {
	a, _ := aCopyWindow(t)
	saveHostNamed(t, a, "one", "one.example")
	one, _ := a.book.Lookup("one")
	kept := settings.SavedTunnel{
		Host: "one", HostID: one.ID, Kind: "local", Listen: "127.0.0.1:8080", Target: "localhost:80",
	}
	if err := a.savedTuns.keep(kept); err != nil {
		t.Fatalf("keep: %v", err)
	}

	renameSaved(t, a, "one", "two")
	saveHostNamed(t, a, "one", "elsewhere.example")
	on := func(host string) func(settings.SavedTunnel) bool {
		return func(saved settings.SavedTunnel) bool {
			return a.sameServer(saved.Host, saved.HostID, host)
		}
	}
	if got := a.savedTuns.listenOn(on("two")); len(got) != 2 || got[1] != "127.0.0.1:8080" {
		t.Errorf("the dialog on two offers %v, want the tunnel saved on it", got)
	}
	if got := a.savedTuns.listenOn(on("one")); len(got) != 0 {
		t.Errorf("the dialog on the new one offers %v, want nothing", got)
	}
	if got := a.savedTunnelTitle(a.savedTuns.all()[0]); !strings.HasSuffix(got, "via two") {
		t.Errorf("the palette calls it %q, want it over two", got)
	}

	if err := a.book.Remove("two"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	err := a.openSavedTunnel(a.savedTuns.all()[0])
	if err == nil || !strings.Contains(err.Error(), "removed") {
		t.Errorf("opening it says %v, want it to say the server was removed", err)
	}
}

// Commands and tunnels saved before servers had ids are given them when
// a window opens, from the names they were saved with.
func TestOldCommandsAndTunnelsAreGivenIDs(t *testing.T) {
	a, path := aCopyWindow(t)
	saveHostNamed(t, a, "one", "one.example")
	if err := a.saved.keep(settings.SavedCommand{Line: "uptime", Host: "one"}); err != nil {
		t.Fatalf("keep the command: %v", err)
	}
	if err := a.savedTuns.keep(settings.SavedTunnel{
		Host: "one", Kind: "local", Listen: "8080", Target: "localhost:80",
	}); err != nil {
		t.Fatalf("keep the tunnel: %v", err)
	}
	if _, err := withSettings(t, a, path); err != nil {
		t.Fatalf("settings: %v", err)
	}
	one, _ := a.book.Lookup("one")
	if got := a.saved.all()[0].HostID; got != one.ID {
		t.Errorf("the command was given the id %q, want %q", got, one.ID)
	}
	if got := a.savedTuns.all()[0].HostID; got != one.ID {
		t.Errorf("the tunnel was given the id %q, want %q", got, one.ID)
	}
}

// hasCommandTitled reports whether the palette offers a command with
// this title.
func hasCommandTitled(a *testApp, title string) bool {
	for _, cmd := range a.root.Commands.All() {
		if cmd.Title == title {
			return true
		}
	}
	return false
}

// The tunnel dialog on a renamed server offers the tunnels kept on it
// under its old name.
func TestTheTunnelDialogOffersWhatWasKeptBeforeARename(t *testing.T) {
	s := sshtest.New(t)
	a, _ := aCopyWindow(t)
	withPanel(t, a)
	pinServers(t, a, s)
	saveHost(t, a, "one", s, "")
	if err := a.connectSaved("one"); err != nil {
		t.Fatalf("connectSaved: %v", err)
	}
	waitFor(t, a, "the machine to answer", func() bool {
		return a.machines.named("one") != nil
	})
	one, _ := a.book.Lookup("one")
	if err := a.savedTuns.keep(settings.SavedTunnel{
		Host: "one", HostID: one.ID, Kind: "local",
		Listen: "127.0.0.1:8080", Target: "localhost:80",
	}); err != nil {
		t.Fatalf("keep: %v", err)
	}
	renameSaved(t, a, "one", "two")

	if err := a.openTunnelHere(); err != nil {
		t.Fatalf("openTunnelHere: %v", err)
	}
	f := awaitModal(t, a, "the tunnel dialog", byTitle[*ui.Form](dlgTunnelVia+"two"))
	if got := f.Field(fldListenOn).Options; !slices.Contains(got, "127.0.0.1:8080") {
		t.Errorf("the dialog offers %v, want the tunnel kept on one", got)
	}
}

// Clearing finished work lets go of what the window kept about where
// each row was filed, the way the cross on one row does.
func TestClearingFinishedWorkForgetsItsEnds(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	from, into := t.TempDir(), t.TempDir()
	putFile(t, from, "one.txt", "the body")
	a.runJob(jobs.Op{
		Kind: jobs.Copy, From: vfs.NewLocal(), At: from,
		Names: []string{"one.txt"}, To: vfs.NewLocal(), Into: into,
	}, jobEnd{host: conns.Local}, jobEnd{host: conns.Local}, nil)
	waitFor(t, a, "the copy to finish", func() bool {
		a.refreshJobs()
		return len(a.jobs) == 0
	})
	if len(a.jobFrom) != 1 {
		t.Fatalf("the window keeps %d ends, want the one row's", len(a.jobFrom))
	}
	if err := a.clearFinished(); err != nil {
		t.Fatalf("clearFinished: %v", err)
	}
	if len(a.jobFrom) != 0 {
		t.Errorf("the window still keeps %d ends after the rows went", len(a.jobFrom))
	}
}
