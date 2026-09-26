package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/geom"

	"github.com/marrasen/gridterm/jobs"
	"github.com/marrasen/gridterm/remote"
	"github.com/marrasen/gridterm/settings"
	"github.com/marrasen/gridterm/vfs"
)

func TestACopyShowsOnTheJobsPaneUntilCleared(t *testing.T) {
	from, into := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(from, "a.txt"), make([]byte, 1<<20), 0o600); err != nil {
		t.Fatal(err)
	}
	w := gunim.NewOffscreen(geom.Sz(400, 300), nil)
	a := newApp(w.Client(), &shells{m: map[string]*shell{}})
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
	w := gunim.NewOffscreen(geom.Sz(400, 300), nil)
	a := newApp(w.Client(), &shells{m: map[string]*shell{}})
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
	w := gunim.NewOffscreen(geom.Sz(400, 300), nil)
	a := newApp(w.Client(), &shells{m: map[string]*shell{}})
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
	w := gunim.NewOffscreen(geom.Sz(400, 300), nil)
	a := newApp(w.Client(), &shells{m: map[string]*shell{}})
	a.st.Saved = []remote.Host{{ID: "s1", Name: "desk"}, {ID: "s2", Name: "laptop"}}
	for _, c := range []struct {
		name, id, want, err string
		held                map[string]string
	}{
		{name: "old", id: "s1", want: "desk"},
		{name: "typed", want: "typed"},
		{name: "desk", id: "s9", err: "desk was removed from the server list"},
		{name: "desk", id: "s1", want: "was-desk", held: map[string]string{"was-desk": "s1"}},
		{name: "laptop", id: "s2", err: "laptop is connected to another machine", held: map[string]string{"laptop": "s1"}},
		{name: "laptop", id: "s2", want: "laptop", held: map[string]string{"laptop": ""}},
	} {
		a.connIDs = c.held
		got, err := a.machineNow(c.name, c.id)
		if got != c.want || (err == nil) != (c.err == "") || (err != nil && err.Error() != c.err) {
			t.Errorf("%s (%s) with %v is %q, %v; want %q, %s", c.name, c.id, c.held, got, err, c.want, c.err)
		}
	}
}

func TestRepeatPressedAgainWhileItsMachineOpensDoesNothingMore(t *testing.T) {
	w := gunim.NewOffscreen(geom.Sz(400, 300), nil)
	a := newApp(w.Client(), &shells{m: map[string]*shell{}})
	a.ctx = t.Context()
	// A machine that never answers, so the repeat stays on its way.
	a.running = []*running{{id: "j1", op: jobs.Op{Kind: jobs.Copy, At: "/", Into: "/", Names: []string{"a"}}, from: "10.255.255.1:1", ended: true}}
	if err := a.repeatJob("j1"); err != nil {
		t.Fatal(err)
	}
	if !a.running[0].repeating || !a.dialing["10.255.255.1:1"] {
		t.Fatalf("repeated, the job is %+v and dialing %v", a.running[0], a.dialing)
	}
	a.dialing["10.255.255.1:1"] = false
	if err := a.repeatJob("j1"); err != nil || a.dialing["10.255.255.1:1"] {
		t.Fatalf("pressed again, %v, and it dialled again", err)
	}
}
