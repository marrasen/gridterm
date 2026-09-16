package jobs

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/marrasen/gridterm/internal/sshtest"
	"github.com/marrasen/gridterm/meter"
	"github.com/marrasen/gridterm/remote"
	"github.com/marrasen/gridterm/vfs"
)

// budget is how long a test waits for a job. Generous on purpose: a
// passing test never waits this long, and a race build on a busy machine
// is slow enough to make a tight budget flaky rather than informative.
const budget = 30 * time.Second

// ends waits for a job to stop and returns why.
func ends(t *testing.T, j *Job) error {
	t.Helper()
	select {
	case <-j.Done():
	case <-time.After(budget):
		t.Fatalf("the job never finished: %+v", j.Progress())
	}
	return j.Progress().Err
}

// side is one end of a job: a filesystem and a directory on it that the
// test owns.
type side struct {
	fs   vfs.FS
	at   string
	real string
}

// local returns a directory on this machine.
func local(t *testing.T) side {
	t.Helper()
	dir := t.TempDir()
	return side{fs: vfs.NewLocal(), at: dir, real: dir}
}

// far returns a directory on the in-process SSH server, reached over
// SFTP: the same disk, seen the way another machine would see it.
func far(t *testing.T) side {
	t.Helper()
	s := sshtest.New(t)
	host, port := s.Host()
	conn, err := remote.Connect(t.Context(), remote.Config{
		Host: host, Port: port, User: "tester",
		HostKeyCallback: ssh.FixedHostKey(s.HostKey()),
		NoAgent:         true,
		Identities:      []string{sshtest.WriteKey(t)},
	})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	files, err := conn.Files(t.Context())
	if err != nil {
		t.Fatalf("start SFTP: %v", err)
	}
	f := vfs.NewSFTP("far", conn, files.Client(), files.Close)
	t.Cleanup(func() { _ = f.Close() })

	dir := t.TempDir()
	return side{fs: f, at: filepath.ToSlash(dir), real: dir}
}

// ends runs a test with the far end on this machine and with it over
// SFTP, so a copy between two machines is tested as well as one on one.
func bothWays(t *testing.T, run func(*testing.T, side, side)) {
	t.Helper()
	t.Run("here to here", func(t *testing.T) { run(t, local(t), local(t)) })
	t.Run("here to there", func(t *testing.T) { run(t, local(t), far(t)) })
	t.Run("there to here", func(t *testing.T) { run(t, far(t), local(t)) })
}

// write puts a file on this machine.
func write(t *testing.T, at, name, body string) {
	t.Helper()
	path := filepath.Join(at, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

// read reads a file on this machine, or fails.
func read(t *testing.T, at, name string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(at, name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(body)
}

// gone asserts that something is not there.
func gone(t *testing.T, at, name string) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(at, name)); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("%s is still there: %v", name, err)
	}
}

// One file, from one filesystem to another.
func TestCopyAFile(t *testing.T) {
	bothWays(t, func(t *testing.T, from, to side) {
		write(t, from.real, "one.txt", "the body")

		q := New(1)
		count := meter.New()
		j := q.Start(t.Context(), Op{
			Kind: Copy, From: from.fs, At: from.at, Names: []string{"one.txt"},
			To: to.fs, Into: to.at,
		}, Options{Count: count})

		if err := ends(t, j); err != nil {
			t.Fatalf("the job: %v", err)
		}
		if got := read(t, to.real, "one.txt"); got != "the body" {
			t.Fatalf("the copy holds %q", got)
		}
		// The original stays where it is.
		if got := read(t, from.real, "one.txt"); got != "the body" {
			t.Fatalf("the original holds %q", got)
		}

		p := j.Progress()
		if p.Files != 1 || p.FilesDone != 1 {
			t.Errorf("%d of %d files done", p.FilesDone, p.Files)
		}
		if p.Bytes != int64(len("the body")) || p.BytesDone != p.Bytes {
			t.Errorf("%d of %d bytes done", p.BytesDone, p.Bytes)
		}
		if in, out := count.Totals(); in+out != uint64(p.Bytes) {
			t.Errorf("the meter counted %d in and %d out, want %d altogether", in, out, p.Bytes)
		}
	})
}

// A whole tree, with the directories and the links as they were.
func TestCopyATree(t *testing.T) {
	bothWays(t, func(t *testing.T, from, to side) {
		write(t, from.real, "tree/one.txt", "one")
		write(t, from.real, "tree/sub/two.txt", "two")
		write(t, from.real, "tree/sub/deep/three.txt", "three")

		q := New(1)
		j := q.Start(t.Context(), Op{
			Kind: Copy, From: from.fs, At: from.at, Names: []string{"tree"},
			To: to.fs, Into: to.at,
		}, Options{})

		if err := ends(t, j); err != nil {
			t.Fatalf("the job: %v", err)
		}
		if got := read(t, to.real, "tree/one.txt"); got != "one" {
			t.Errorf("tree/one.txt = %q", got)
		}
		if got := read(t, to.real, "tree/sub/two.txt"); got != "two" {
			t.Errorf("tree/sub/two.txt = %q", got)
		}
		if got := read(t, to.real, "tree/sub/deep/three.txt"); got != "three" {
			t.Errorf("tree/sub/deep/three.txt = %q", got)
		}
		if p := j.Progress(); p.Files != 3 || p.FilesDone != 3 {
			t.Errorf("%d of %d files done", p.FilesDone, p.Files)
		}
	})
}

// A link is copied as a link. Following it would copy what it points at,
// which may be the directory being copied.
func TestCopyKeepsALinkALink(t *testing.T) {
	bothWays(t, func(t *testing.T, from, to side) {
		write(t, from.real, "tree/one.txt", "one")
		if err := os.Symlink("one.txt", filepath.Join(from.real, "tree", "link")); err != nil {
			t.Skipf("no link to copy: %v", err)
		}

		q := New(1)
		j := q.Start(t.Context(), Op{
			Kind: Copy, From: from.fs, At: from.at, Names: []string{"tree"},
			To: to.fs, Into: to.at,
		}, Options{})
		if err := ends(t, j); err != nil {
			t.Fatalf("the job: %v", err)
		}

		got, err := os.Lstat(filepath.Join(to.real, "tree", "link"))
		if err != nil {
			t.Fatalf("the link was not copied: %v", err)
		}
		if got.Mode()&fs.ModeSymlink == 0 {
			t.Fatalf("what was copied is %v, want a link", got.Mode())
		}
		at, err := os.Readlink(filepath.Join(to.real, "tree", "link"))
		if err != nil {
			t.Fatalf("readlink: %v", err)
		}
		if at != "one.txt" {
			t.Fatalf("the copied link points at %q", at)
		}
	})
}

// Moving between two filesystems copies and then takes the original
// away.
func TestMoveBetweenFilesystems(t *testing.T) {
	bothWays(t, func(t *testing.T, from, to side) {
		write(t, from.real, "tree/one.txt", "one")

		q := New(1)
		j := q.Start(t.Context(), Op{
			Kind: Move, From: from.fs, At: from.at, Names: []string{"tree"},
			To: to.fs, Into: to.at,
		}, Options{})
		if err := ends(t, j); err != nil {
			t.Fatalf("the job: %v", err)
		}

		if got := read(t, to.real, "tree/one.txt"); got != "one" {
			t.Fatalf("what was moved holds %q", got)
		}
		gone(t, from.real, "tree")
	})
}

// Moving on one filesystem renames, which costs nothing however large
// the directory is.
func TestMoveOnOneFilesystemRenames(t *testing.T) {
	from := local(t)
	write(t, from.real, "tree/one.txt", "one")
	into := filepath.Join(from.real, "elsewhere")
	if err := os.Mkdir(into, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	q := New(1)
	j := q.Start(t.Context(), Op{
		Kind: Move, From: from.fs, At: from.at, Names: []string{"tree"},
		To: from.fs, Into: into,
	}, Options{})
	if err := ends(t, j); err != nil {
		t.Fatalf("the job: %v", err)
	}

	if got := read(t, into, "tree/one.txt"); got != "one" {
		t.Fatalf("what was moved holds %q", got)
	}
	gone(t, from.real, "tree")
}

// Deleting takes a whole tree away, deepest first.
func TestDeleteATree(t *testing.T) {
	bothWays(t, func(t *testing.T, from, _ side) {
		write(t, from.real, "tree/one.txt", "one")
		write(t, from.real, "tree/sub/two.txt", "two")

		q := New(1)
		j := q.Start(t.Context(), Op{
			Kind: Delete, From: from.fs, At: from.at, Names: []string{"tree"},
		}, Options{})
		if err := ends(t, j); err != nil {
			t.Fatalf("the job: %v", err)
		}
		gone(t, from.real, "tree")
	})
}

// A name that is already there is asked about, and the answer is obeyed.
func TestOverwriteAsksFirst(t *testing.T) {
	bothWays(t, func(t *testing.T, from, to side) {
		write(t, from.real, "one.txt", "the new one")
		write(t, to.real, "one.txt", "the old one")

		asked := make(chan Conflict, 4)
		q := New(1)
		j := q.Start(t.Context(), Op{
			Kind: Copy, From: from.fs, At: from.at, Names: []string{"one.txt"},
			To: to.fs, Into: to.at,
		}, Options{Ask: askWith(asked, Choice{What: Skip})})

		if err := ends(t, j); err != nil {
			t.Fatalf("the job: %v", err)
		}
		select {
		case c := <-asked:
			if !strings.HasSuffix(c.Path, "one.txt") {
				t.Errorf("it asked about %q", c.Path)
			}
			if c.Have.Size != int64(len("the old one")) {
				t.Errorf("it said the one there is %d bytes", c.Have.Size)
			}
		default:
			t.Fatal("it wrote over a file without asking")
		}
		if got := read(t, to.real, "one.txt"); got != "the old one" {
			t.Fatalf("skipping wrote %q anyway", got)
		}
		if p := j.Progress(); p.Skipped != 1 {
			t.Errorf("%d were skipped, want 1", p.Skipped)
		}
	})
}

// Answering "replace" replaces it.
func TestOverwriteReplaces(t *testing.T) {
	from, to := local(t), local(t)
	write(t, from.real, "one.txt", "the new one")
	write(t, to.real, "one.txt", "the old one")

	q := New(1)
	j := q.Start(t.Context(), Op{
		Kind: Copy, From: from.fs, At: from.at, Names: []string{"one.txt"},
		To: to.fs, Into: to.at,
	}, Options{Ask: Always{What: Replace}})

	if err := ends(t, j); err != nil {
		t.Fatalf("the job: %v", err)
	}
	if got := read(t, to.real, "one.txt"); got != "the new one" {
		t.Fatalf("the file holds %q", got)
	}
}

// Answering "rename" puts it beside what was there.
func TestOverwriteRenames(t *testing.T) {
	from, to := local(t), local(t)
	write(t, from.real, "one.txt", "the new one")
	write(t, to.real, "one.txt", "the old one")

	q := New(1)
	j := q.Start(t.Context(), Op{
		Kind: Copy, From: from.fs, At: from.at, Names: []string{"one.txt"},
		To: to.fs, Into: to.at,
	}, Options{Ask: Always{What: Rename, Name: "one (copy).txt"}})

	if err := ends(t, j); err != nil {
		t.Fatalf("the job: %v", err)
	}
	if got := read(t, to.real, "one.txt"); got != "the old one" {
		t.Errorf("what was there holds %q", got)
	}
	if got := read(t, to.real, "one (copy).txt"); got != "the new one" {
		t.Errorf("the copy holds %q", got)
	}
}

// Answering once for all of them asks once and no more.
func TestOverwriteAllAsksOnce(t *testing.T) {
	from, to := local(t), local(t)
	for _, name := range []string{"one.txt", "two.txt", "three.txt"} {
		write(t, from.real, name, "new")
		write(t, to.real, name, "old")
	}

	asked := make(chan Conflict, 8)
	q := New(1)
	j := q.Start(t.Context(), Op{
		Kind: Copy, From: from.fs, At: from.at,
		Names: []string{"one.txt", "two.txt", "three.txt"},
		To:    to.fs, Into: to.at,
	}, Options{Ask: askWith(asked, Choice{What: Replace, All: true})})

	if err := ends(t, j); err != nil {
		t.Fatalf("the job: %v", err)
	}
	if got := len(asked); got != 1 {
		t.Fatalf("it asked %d times, want once for all of them", got)
	}
	for _, name := range []string{"one.txt", "two.txt", "three.txt"} {
		if got := read(t, to.real, name); got != "new" {
			t.Errorf("%s holds %q", name, got)
		}
	}
}

// Answering "stop" stops the job, and what was already copied stays.
func TestOverwriteStop(t *testing.T) {
	from, to := local(t), local(t)
	write(t, from.real, "one.txt", "new")
	write(t, to.real, "one.txt", "old")

	q := New(1)
	j := q.Start(t.Context(), Op{
		Kind: Copy, From: from.fs, At: from.at, Names: []string{"one.txt"},
		To: to.fs, Into: to.at,
	}, Options{Ask: Always{What: Stop}})

	if err := ends(t, j); err == nil {
		t.Fatal("stopping was reported as a job that finished")
	}
	if got := read(t, to.real, "one.txt"); got != "old" {
		t.Fatalf("it wrote %q on the way out", got)
	}
}

// With nobody to ask, a name that is already there stops the job rather
// than being decided alone. The file that is there is the user's.
func TestNobodyToAskStopsTheJob(t *testing.T) {
	from, to := local(t), local(t)
	write(t, from.real, "one.txt", "new")
	write(t, to.real, "one.txt", "old")

	q := New(1)
	j := q.Start(t.Context(), Op{
		Kind: Copy, From: from.fs, At: from.at, Names: []string{"one.txt"},
		To: to.fs, Into: to.at,
	}, Options{})

	err := ends(t, j)
	var conflict *ConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("error = %v, want it to say what was in the way", err)
	}
	if got := read(t, to.real, "one.txt"); got != "old" {
		t.Fatalf("it wrote %q anyway", got)
	}
}

// askWith answers every question the same way and records what it was
// asked.
func askWith(into chan Conflict, choice Choice) Ask {
	return &recorder{into: into, choice: choice}
}

type recorder struct {
	into   chan Conflict
	choice Choice

	mu sync.Mutex
	n  int
}

func (r *recorder) Overwrite(_ context.Context, c Conflict) (Choice, error) {
	r.mu.Lock()
	r.n++
	r.mu.Unlock()
	select {
	case r.into <- c:
	default:
	}
	return r.choice, nil
}

// Which way the bytes are counted is the caller's to say: a copy to
// another machine is bytes leaving, and one from it is bytes arriving.
// Nothing here can tell which end is somewhere else.
func TestTheCallerSaysWhichWayTheBytesGo(t *testing.T) {
	for _, tc := range []struct {
		name string
		out  bool
	}{
		{"leaving", true},
		{"arriving", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			from, to := local(t), local(t)
			write(t, from.real, "one.txt", "the body")

			count := meter.New()
			q := New(1)
			j := q.Start(t.Context(), Op{
				Kind: Copy, From: from.fs, At: from.at, Names: []string{"one.txt"},
				To: to.fs, Into: to.at,
			}, Options{Count: count, Out: tc.out})
			if err := ends(t, j); err != nil {
				t.Fatalf("the job: %v", err)
			}

			in, out := count.Totals()
			want := uint64(len("the body"))
			if tc.out && (out != want || in != 0) {
				t.Fatalf("counted %d in and %d out, want %d leaving", in, out, want)
			}
			if !tc.out && (in != want || out != 0) {
				t.Fatalf("counted %d in and %d out, want %d arriving", in, out, want)
			}
		})
	}
}

// A move that skipped something must not delete the original: it is
// still only in one place.
func TestMoveKeepsWhatItSkipped(t *testing.T) {
	// Two machines, so the move is a copy and then a delete. On one
	// machine it is a rename, which leaves both alone by itself.
	from, to := local(t), far(t)
	write(t, from.real, "one.txt", "the new one")
	write(t, to.real, "one.txt", "the old one")

	q := New(1)
	j := q.Start(t.Context(), Op{
		Kind: Move, From: from.fs, At: from.at, Names: []string{"one.txt"},
		To: to.fs, Into: to.at,
	}, Options{Ask: Always{What: Skip}})

	if err := ends(t, j); err == nil {
		t.Fatal("a move that copied nothing was reported as finished")
	}
	if got := read(t, from.real, "one.txt"); got != "the new one" {
		t.Fatalf("the original holds %q", got)
	}
	if got := read(t, to.real, "one.txt"); got != "the old one" {
		t.Fatalf("what was there holds %q", got)
	}
}

// A job says when it stopped as well as when it began, so whatever shows
// it can say how long it took however long ago it ended.
func TestAJobSaysWhenItEnded(t *testing.T) {
	from := local(t)
	write(t, from.real, "one.txt", "the body")
	to := local(t)

	q := New(1)
	j := q.Start(t.Context(), Op{
		Kind: Copy, From: from.fs, At: from.at, Names: []string{"one.txt"},
		To: to.fs, Into: to.at,
	}, Options{})
	if err := ends(t, j); err != nil {
		t.Fatalf("the job: %v", err)
	}

	p := j.Progress()
	if p.Ended.IsZero() {
		t.Fatal("the job never said when it ended")
	}
	if p.Ended.Before(p.Started) {
		t.Fatalf("it ended at %v, before it began at %v", p.Ended, p.Started)
	}
	// It stops moving once the job has: how long it took is not how long
	// ago it was.
	time.Sleep(time.Millisecond)
	if again := j.Progress().Ended; !again.Equal(p.Ended) {
		t.Fatalf("it says it ended at %v and then at %v", p.Ended, again)
	}
}
