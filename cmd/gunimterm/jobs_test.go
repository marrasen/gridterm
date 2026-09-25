package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/geom"

	"github.com/marrasen/gridterm/jobs"
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
	if j.Failed || j.Share != 1 || j.Detail != "1 file done" {
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
		{jobs.Progress{Files: 4, FilesDone: 1, Current: "b.txt"}, "1 of 4 files · b.txt", false, 0.25},
		{jobs.Progress{Done: true, FilesDone: 2, Err: context.Canceled}, "Cancelled after 2 files", false, -1},
		{jobs.Progress{Done: true, Err: errors.New("disk full")}, "disk full", true, -1},
		{jobs.Progress{Done: true, FilesDone: 3, Skipped: 1, Started: start, Ended: start.Add(2 * time.Second)}, "3 files done, 1 left as they were in 2s", false, 1},
	} {
		got := jobRow(&running{id: "j1", title: "t"}, c.p)
		if got.Detail != c.detail || got.Failed != c.failed || got.Share != c.share {
			t.Errorf("%+v reads %q, failed %v, share %v; want %q, %v, %v", c.p, got.Detail, got.Failed, got.Share, c.detail, c.failed, c.share)
		}
	}
}
