package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marrasen/kakel/screen"

	"github.com/marrasen/kakel/machines"

	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/gunimtest"

	"github.com/marrasen/kakel/jobs"
	"github.com/marrasen/kakel/remote"
	"github.com/marrasen/kakel/settings"
	"github.com/marrasen/kakel/vfs"
)

func TestACopyShowsOnTheJobsPaneUntilCleared(t *testing.T) {
	from, into := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(from, "a.txt"), make([]byte, 1<<20), 0o600); err != nil {
		t.Fatal(err)
	}
	w := gunimtest.New(t, geom.Sz(400, 300), nil)
	a := newApp(w.Client(), screen.NewShells())
	a.ctx = t.Context()
	local := vfs.NewLocal()
	a.follow(jobs.Op{Kind: jobs.Copy, From: local, At: from, Names: []string{"a.txt"}, To: local, Into: into}, "Copying 1 item to x")
	if len(a.st.Jobs) != 1 || a.st.Jobs[0].Title != "Copying 1 item to x" {
		t.Fatalf("started, the jobs are %+v", a.st.Jobs)
	}
	waitFor(t, a, "the copy to finish", func() bool { return len(a.st.Jobs) == 1 && a.st.Jobs[0].Done })
	j := a.st.Jobs[0]
	if j.Failed || j.Share != 1 || !strings.HasPrefix(j.Detail, "1 file done") {
		t.Fatalf("finished, the job reads %+v", j)
	}
	if _, err := os.Stat(filepath.Join(into, "a.txt")); err != nil {
		t.Fatal(err)
	}
	a.handle(ClearJobs{})
	if len(a.st.Jobs) != 0 {
		t.Fatalf("cleared, the jobs are %+v", a.st.Jobs)
	}
}

func TestAJobSaysHowItEnded(t *testing.T) {
	start := time.Now()
	for _, c := range []struct {
		p      jobs.Progress
		detail string
		failed bool
		share  float32
	}{
		{jobs.Progress{}, "Counting…", false, -1},
		{jobs.Progress{Files: 4, FilesDone: 1, Current: "b.txt"}, "1 of 4 files", false, 0.25},
		{jobs.Progress{Files: 4, FilesDone: 1, Started: start.Add(-3 * time.Second)}, "1 of 4 files · going 3 s", false, 0.25},
		{jobs.Progress{Done: true, FilesDone: 2, Err: context.Canceled}, "It was cancelled. · 2 files done", false, -1},
		{jobs.Progress{Done: true, FilesDone: 1, Err: jobs.ErrStopped}, "It was stopped. · 1 file done", false, -1},
		{jobs.Progress{Done: true, Err: errors.Join(jobs.ErrStopped, errors.New("busy"))},
			"It was stopped, but what was half written could not be taken away: busy", true, -1},
		{jobs.Progress{Done: true, Err: errors.New("disk full")}, "It failed: disk full", true, -1},
		{jobs.Progress{Done: true, FilesDone: 3, Skipped: 1, Started: start, Ended: start.Add(2 * time.Second)}, "3 files done, 1 left as they were · in 2s", false, 1},
	} {
		got := jobRow(&running{id: "j1", title: "t"}, c.p)
		if got.Detail != c.detail || got.Failed != c.failed || got.Share != c.share {
			t.Errorf("%+v reads %q, failed %v, share %v; want %q, %v, %v", c.p, got.Detail, got.Failed, got.Share, c.detail, c.failed, c.share)
		}
	}
}

func TestAFinishedCopyIsRepeatedAndSaved(t *testing.T) {
	from, into := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(from, "a.txt"), []byte("one"), 0o600); err != nil {
		t.Fatal(err)
	}
	w := gunimtest.New(t, geom.Sz(400, 300), nil)
	a := newApp(w.Client(), screen.NewShells())
	a.ctx = t.Context()
	set, err := settings.Load(filepath.Join(t.TempDir(), "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	a.settings = set
	local := a.fsFor("")
	a.followOn(jobs.Op{Kind: jobs.Copy, From: local, At: from, Names: []string{"a.txt"}, To: local, Into: into}, "Copying 1 item to x", "", "")
	waitFor(t, a, "the copy", func() bool { return a.st.Jobs[0].Done })
	if j := a.st.Jobs[0]; !j.Repeatable || j.Saved {
		t.Fatalf("finished, the copy reads %+v", j)
	}
	// Changed since, and copied again, it is the same again.
	if err := os.WriteFile(filepath.Join(from, "a.txt"), []byte("two"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(into, "a.txt")); err != nil {
		t.Fatal(err)
	}
	a.handle(RepeatJob{ID: a.st.Jobs[0].ID})
	waitFor(t, a, "the repeat", func() bool { return len(a.st.Jobs) == 2 && a.st.Jobs[1].Done })
	if got, _ := os.ReadFile(filepath.Join(into, "a.txt")); string(got) != "two" {
		t.Fatalf("repeated, the copy holds %q", got)
	}
	a.handle(SaveCopy{ID: a.st.Jobs[0].ID, On: true})
	if len(a.st.SavedCopies) != 1 || !a.st.Jobs[0].Saved {
		t.Fatalf("saved, the list is %+v", a.st.SavedCopies)
	}
	// Gone from where it went, so the copy asks about nothing.
	if err := os.Remove(filepath.Join(into, "a.txt")); err != nil {
		t.Fatal(err)
	}
	a.handle(RunSavedCopy{Saved: a.st.SavedCopies[0]})
	waitFor(t, a, "the saved copy", func() bool { return len(a.st.Jobs) == 3 && a.st.Jobs[2].Done })
	a.handle(ForgetCopy{Saved: a.st.SavedCopies[0]})
	if len(a.st.SavedCopies) != 0 {
		t.Fatalf("forgotten, the list is %+v", a.st.SavedCopies)
	}
}

func TestStopOnAReplaceQuestionStopsTheCopy(t *testing.T) {
	from, into := t.TempDir(), t.TempDir()
	for _, dir := range []string{from, into} {
		if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte(dir), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	w := gunimtest.New(t, geom.Sz(400, 300), nil)
	a := newApp(w.Client(), screen.NewShells())
	a.ctx = t.Context()
	local := vfs.NewLocal()
	a.follow(jobs.Op{Kind: jobs.Copy, From: local, At: from, Names: []string{"a.txt"}, To: local, Into: into}, "Copying 1 item to x")
	waitFor(t, a, "the question", func() bool { return len(a.st.Asks) == 1 })
	a.handle(AskAnswered{ID: a.st.Asks[0].ID})
	waitFor(t, a, "the copy to end", func() bool { return len(a.st.Jobs) == 1 && a.st.Jobs[0].Done })
	if j := a.st.Jobs[0]; j.Failed || j.Detail != "It was stopped." {
		t.Fatalf("stopped, the job reads %+v", j)
	}
	if body, _ := os.ReadFile(filepath.Join(into, "a.txt")); string(body) != into {
		t.Fatalf("stopped, the file there holds %q", body)
	}
}

func TestWorkKeptFromBeforeFindsItsServerByID(t *testing.T) {
	w := gunimtest.New(t, geom.Sz(400, 300), nil)
	a := newApp(w.Client(), screen.NewShells())
	book, err := remote.LoadBook(filepath.Join(t.TempDir(), "servers.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := book.Put(remote.Host{Name: "desk", Address: "desk.example"}, ""); err != nil {
		t.Fatal(err)
	}
	a.book = book
	desk, _ := book.Lookup("desk")
	// By its ID, whatever it was called when the work was kept.
	if got, err := a.machineNow("old name", desk.ID); got != machines.ID(desk.ID) || err != nil {
		t.Fatalf("kept on desk as old name, it runs on %q, %v", got, err)
	}
	// Removed from the list since: refused.
	if _, err := a.machineNow("gone", "no-such-id"); err == nil || !strings.Contains(err.Error(), "removed") {
		t.Fatalf("kept on a removed server, it said %v", err)
	}
	// Kept with no ID: this computer, or a quick connection to the
	// address, the same one each time.
	if got, err := a.machineNow("", ""); got != "" || err != nil {
		t.Fatalf("kept on this computer, it runs on %q, %v", got, err)
	}
	first, _ := a.machineNow("me@typed.example", "")
	again, _ := a.machineNow("me@typed.example", "")
	if !strings.HasPrefix(string(first), "quick-") || again != first || a.machines.Name(first) != "me@typed.example" {
		t.Fatalf("kept on a typed address, it runs on %q then %q, called %q", first, again, a.machines.Name(first))
	}
}

// A finished copy is done again on the machines it ran between, unless
// a saved server at either end, or the saved window it went through,
// was removed from the list since.
func TestRepeatRefusesAServerRemovedSince(t *testing.T) {
	w := gunimtest.New(t, geom.Sz(400, 300), nil)
	a := newApp(w.Client(), screen.NewShells())
	book, err := remote.LoadBook(filepath.Join(t.TempDir(), "servers.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range []remote.Host{{Name: "desk", Address: "desk.example"}, {Name: "box", Address: "box.example", Window: true}} {
		if err := book.Put(h, ""); err != nil {
			t.Fatal(err)
		}
	}
	a.book = book
	desk, _ := book.Lookup("desk")
	box, _ := book.Lookup("box")
	quick := a.machines.NewQuick("me@typed.example", false)
	for _, m := range []machines.ID{machines.Local, quick, machines.ID(desk.ID), machines.FarID(machines.ID(box.ID), "k")} {
		if err := a.stillSaved(m); err != nil {
			t.Fatalf("%q is there, and it said %v", m, err)
		}
	}
	for _, h := range []remote.Host{desk, box} {
		if err := book.RemoveID(h.ID); err != nil {
			t.Fatal(err)
		}
	}
	for _, m := range []machines.ID{machines.ID(desk.ID), machines.FarID(machines.ID(box.ID), "k")} {
		if err := a.stillSaved(m); err == nil || !strings.Contains(err.Error(), "removed") {
			t.Fatalf("%q was removed, and it said %v", m, err)
		}
	}
}

func TestRepeatPressedAgainWhileItsMachineOpensDoesNothingMore(t *testing.T) {
	w := gunimtest.New(t, geom.Sz(400, 300), nil)
	a := newApp(w.Client(), screen.NewShells())
	a.ctx = t.Context()
	// A quick connection to a machine that never answers, so the
	// repeat stays on its way.
	quick := a.machines.NewQuick("10.255.255.1:1", false)
	a.running = []*running{{id: "j1", op: jobs.Op{Kind: jobs.Copy, At: "/", Into: "/", Names: []string{"a"}}, from: quick, ended: true}}
	if err := a.repeatJob("j1"); err != nil {
		t.Fatal(err)
	}
	// The same quick connection, dialled at its address.
	if !a.running[0].repeating || a.machines.Get(quick).Dialing == nil || quickCount(a) != 1 {
		t.Fatalf("repeated, the job is %+v and dialing %v", a.running[0], a.machines.Dialing())
	}
	// As if it had come back: its dial given up, and not dialling.
	t.Cleanup(a.machines.Get(quick).Dialing)
	a.machines.At(quick).Dialing = nil
	if err := a.repeatJob("j1"); err != nil || a.machines.Get(quick).Dialing != nil {
		t.Fatalf("pressed again, %v, and it dialled again", err)
	}
}

// Enter on "Replace notes.txt?" leaves the file there alone: it is the
// first choice, which the dialog's Enter gives.
func TestEnterOnTheOverwriteQuestionLeavesTheFile(t *testing.T) {
	w := gunimtest.New(t, geom.Sz(400, 300), nil)
	a := newApp(w.Client(), screen.NewShells())
	a.ctx = t.Context()
	local := vfs.NewLocal()
	for _, tc := range []struct {
		answer string
		want   jobs.What
	}{{"", jobs.Skip}, {"Replace", jobs.Replace}} {
		got := make(chan jobs.Choice, 1)
		go func() {
			c, _ := overwriteAsker{a}.Overwrite(t.Context(), jobs.Conflict{To: local, Path: "/tmp/notes.txt"})
			got <- c
		}()
		waitFor(t, a, "the question", func() bool { return len(a.st.Asks) == 1 })
		q := a.st.Asks[0]
		pick := tc.answer
		if pick == "" {
			// What Enter gives: the first choice.
			pick = q.Choose[0]
		}
		a.handle(AskAnswered{ID: q.ID, Yes: true, Answers: []string{pick, ""}})
		if c := <-got; c.What != tc.want {
			t.Fatalf("answered %q, the job was told %v, want %v", pick, c.What, tc.want)
		}
	}
}

// A copy done lists again every file pane on the filesystems it worked
// on, not only those at its folders as it started: a pane may have
// gone to the folder since.
func TestAFinishedCopyListsEveryPaneOnItsFilesystemAgain(t *testing.T) {
	a, _ := agentApp(t)
	from, into, elsewhere := t.TempDir(), t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(from, "a.txt"), []byte("one"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := a.filesOn("", elsewhere); err != nil {
		t.Fatal(err)
	}
	pane := a.st.Panes[len(a.st.Panes)-1].ID
	waitFor(t, a, "the folder", func() bool { return a.st.Browsers[pane].Seq > 0 })
	seq := a.st.Browsers[pane].Seq
	local := vfs.NewLocal()
	a.follow(jobs.Op{Kind: jobs.Copy, From: local, At: from, Names: []string{"a.txt"}, To: local, Into: into}, "Copying 1 item to x")
	waitFor(t, a, "the pane listed again", func() bool { return a.st.Browsers[pane].Seq > seq })
}

// A copy's saved tick holds whichever way round its names were picked.
func TestACopyIsSavedWhateverTheOrderOfItsNames(t *testing.T) {
	a := newApp(gunimtest.New(t, geom.Sz(400, 300), nil).Client(), screen.NewShells())
	r := &running{op: jobs.Op{Kind: jobs.Copy, At: "/a", Into: "/b", Names: []string{"x", "y"}}}
	saved := a.savedCopyOf(r)
	saved.Names = []string{"y", "x"}
	a.st.SavedCopies = []settings.SavedCopy{saved}
	if !a.isSaved(r) {
		t.Fatal("saved with its names the other way round, the copy reads as not saved")
	}
}

// A copy is filed under the machine it writes to, unless that is this
// one; then under the machine it reads from. A delete is filed where it
// happens.
func TestAJobIsFiledWhereItsFilesAre(t *testing.T) {
	far := machines.FarID("desk", "db")
	for _, c := range []struct {
		kind     jobs.Kind
		from, to machines.ID
		want     machines.ID
	}{
		{jobs.Copy, "srv", machines.Local, "srv"},
		{jobs.Copy, machines.Local, "srv", "srv"},
		{jobs.Copy, far, machines.Local, far},
		{jobs.Delete, "srv", machines.Local, "srv"},
	} {
		r := &running{op: jobs.Op{Kind: c.kind}, from: c.from, to: c.to}
		if got := jobRow(r, jobs.Progress{}).Machine; got != c.want {
			t.Errorf("a %v from %q to %q is filed under %q, want %q", c.kind, c.from, c.to, got, c.want)
		}
	}
}

// Where a saved copy goes names a machine a window reached through
// that window, not by kakel's own key for it.
func TestASavedCopyThroughAWindowIsNamed(t *testing.T) {
	c := settings.SavedCopy{From: "desk" + KeptFarSep + "db", FromID: "d1", At: "/srv", Into: "/tmp"}
	named := func(id machines.ID) string { return map[machines.ID]string{"d1": "desk"}[id] }
	if got := CopiedWhere(c, named); got != "/srv on db through desk → /tmp on this computer" {
		t.Fatalf("the copy reads %q", got)
	}
}

// A listing asked for before another lands is dropped, whichever lands
// last: a copy's listing again never sends a pane back where it was.
func TestAnOlderListingNeverSendsAPaneBack(t *testing.T) {
	a, _ := agentApp(t)
	was, now := t.TempDir(), t.TempDir()
	if err := a.filesOn("", was); err != nil {
		t.Fatal(err)
	}
	pane := a.st.Panes[len(a.st.Panes)-1].ID
	waitFor(t, a, "the folder", func() bool { return a.st.Browsers[pane].Seq > 0 })
	for range 20 {
		a.browse(Browse{Pane: pane, Path: was})
		a.browse(Browse{Pane: pane, Path: now})
		asked := a.listing[pane]
		waitFor(t, a, "the listings", func() bool { return a.st.Browsers[pane].Path == now })
		pumpFor(a, 20*time.Millisecond)
		if got := a.st.Browsers[pane].Path; got != now || a.listing[pane] != asked {
			t.Fatalf("the pane went back to %q", got)
		}
	}
}

// A copy that ends while a pane is on its way to another folder lists
// that folder, not the one the pane is leaving.
func TestACopyEndingListsTheFolderAPaneIsGoingTo(t *testing.T) {
	a, _ := agentApp(t)
	from, was, now := t.TempDir(), t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(from, "a.txt"), []byte("one"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := a.filesOn("", was); err != nil {
		t.Fatal(err)
	}
	pane := a.st.Panes[len(a.st.Panes)-1].ID
	waitFor(t, a, "the folder", func() bool { return a.st.Browsers[pane].Seq > 0 })
	a.browse(Browse{Pane: pane, Path: now})
	a.relistOn(jobs.Op{Kind: jobs.Copy, From: vfs.NewLocal(), At: from, Names: []string{"a.txt"}, To: vfs.NewLocal(), Into: was})
	waitFor(t, a, "the pane there", func() bool { return a.st.Browsers[pane].Path == now })
	pumpFor(a, 50*time.Millisecond)
	if got := a.st.Browsers[pane].Path; got != now {
		t.Fatalf("the pane went back to %q", got)
	}
}
