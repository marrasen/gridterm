package jobs

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marrasen/gridterm/vfs"
)

// A copy that fails after the user said to replace something must leave
// the file they had. Writing onto the name empties it first, so a
// failure then -- or a cancel -- would leave them with neither.
func TestReplaceThatFailsLeavesTheFileThatWasThere(t *testing.T) {
	from, to := local(t), local(t)
	write(t, from.real, "one.txt", strings.Repeat("x", 4096))
	write(t, to.real, "one.txt", "the only copy of something")

	held := newSlow(from.fs)
	held.fail = errors.New("the disk went away")

	q := New(1)
	j := q.Start(t.Context(), Op{
		Kind: Copy, From: held, At: from.at, Names: []string{"one.txt"},
		To: to.fs, Into: to.at,
	}, Options{Ask: Always{What: Replace}})

	select {
	case <-held.reading:
	case <-time.After(budget):
		t.Fatal("the job never started reading")
	}
	close(held.held)

	if err := ends(t, j); err == nil {
		t.Fatal("a read that failed was reported as a job that finished")
	}
	if got := read(t, to.real, "one.txt"); got != "the only copy of something" {
		t.Fatalf("the file the user had holds %q", got)
	}
	if left := parts(t, to.real); len(left) != 0 {
		t.Fatalf("%v was left behind", left)
	}
}

// The same for cancelling: a replace that the user gives up on leaves
// what was there.
func TestCancellingAReplaceLeavesTheFileThatWasThere(t *testing.T) {
	from, to := local(t), local(t)
	write(t, from.real, "one.txt", strings.Repeat("x", 4096))
	write(t, to.real, "one.txt", "the only copy of something")
	held := newSlow(from.fs)

	q := New(1)
	j := q.Start(t.Context(), Op{
		Kind: Copy, From: held, At: from.at, Names: []string{"one.txt"},
		To: to.fs, Into: to.at,
	}, Options{Ask: Always{What: Replace}})

	select {
	case <-held.reading:
	case <-time.After(budget):
		t.Fatal("the job never started reading")
	}
	j.Cancel()
	close(held.held)

	if err := ends(t, j); err == nil {
		t.Fatal("cancelling was reported as a job that finished")
	}
	if got := read(t, to.real, "one.txt"); got != "the only copy of something" {
		t.Fatalf("the file the user had holds %q", got)
	}
}

// Renaming on one filesystem is where a move within a machine goes, and
// the answers have to mean the same there as anywhere else.
func TestRenamingObeysTheAnswer(t *testing.T) {
	t.Run("put it beside", func(t *testing.T) {
		here := local(t)
		write(t, here.real, "one.txt", "the new one")
		into := filepath.Join(here.real, "into")
		if err := os.Mkdir(into, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		write(t, into, "one.txt", "the old one")

		q := New(1)
		j := q.Start(t.Context(), Op{
			Kind: Move, From: here.fs, At: here.at, Names: []string{"one.txt"},
			To: here.fs, Into: into,
		}, Options{Ask: Always{What: Rename, Name: "one (moved).txt"}})

		if err := ends(t, j); err != nil {
			t.Fatalf("the job: %v", err)
		}
		if got := read(t, into, "one.txt"); got != "the old one" {
			t.Fatalf("what was there holds %q", got)
		}
		if got := read(t, into, "one (moved).txt"); got != "the new one" {
			t.Fatalf("what was moved holds %q", got)
		}
	})

	t.Run("what is there is shown", func(t *testing.T) {
		here := local(t)
		write(t, here.real, "one.txt", "the new one")
		into := filepath.Join(here.real, "into")
		if err := os.Mkdir(into, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		write(t, into, "one.txt", "the old one, which is longer")

		asked := make(chan Conflict, 2)
		q := New(1)
		j := q.Start(t.Context(), Op{
			Kind: Move, From: here.fs, At: here.at, Names: []string{"one.txt"},
			To: here.fs, Into: into,
		}, Options{Ask: askWith(asked, Choice{What: Skip})})
		if err := ends(t, j); err != nil {
			t.Fatalf("the job: %v", err)
		}

		select {
		case c := <-asked:
			if c.Have.Size != int64(len("the old one, which is longer")) {
				t.Fatalf("it said the one there is %d bytes", c.Have.Size)
			}
			if c.Have.Name == "" {
				t.Error("it did not say what is there")
			}
		default:
			t.Fatal("it renamed over a file without asking")
		}
	})
}

// A name to put something beside is a name, not a path. One that walks
// out of the directory would write where the user did not choose.
func TestABesideNameIsANameAndNotAPath(t *testing.T) {
	for _, name := range []string{"", ".", "..", "../out.txt", "in/out.txt", "in\\out.txt"} {
		t.Run(name, func(t *testing.T) {
			from, to := local(t), local(t)
			write(t, from.real, "one.txt", "the new one")
			write(t, to.real, "one.txt", "the old one")

			q := New(1)
			j := q.Start(t.Context(), Op{
				Kind: Copy, From: from.fs, At: from.at, Names: []string{"one.txt"},
				To: to.fs, Into: to.at,
			}, Options{Ask: Always{What: Rename, Name: name}})

			err := ends(t, j)
			if err == nil {
				t.Fatalf("%q was accepted as a name", name)
			}
			// Refused for being the wrong shape, rather than failing
			// somewhere further in with whatever the machine said about
			// the path it ended up building.
			if !strings.Contains(err.Error(), "name") {
				t.Fatalf("%q was refused with %v, want it refused as a name", name, err)
			}
			if got := read(t, to.real, "one.txt"); got != "the old one" {
				t.Fatalf("what was there holds %q", got)
			}
			// And nothing was written outside the directory it was
			// given.
			if _, err := os.Stat(filepath.Join(filepath.Dir(to.real), "out.txt")); err == nil {
				t.Fatal("it wrote outside the directory it was given")
			}
		})
	}
}

// Putting a directory beside one that is there takes what is inside it
// along. The names inside were worked out before anyone knew it would
// end up somewhere else.
func TestRenamingADirectoryTakesWhatIsInsideIt(t *testing.T) {
	from, to := local(t), local(t)
	write(t, from.real, "tree/one.txt", "one")
	write(t, from.real, "tree/sub/two.txt", "two")
	write(t, to.real, "tree", "something else entirely")

	q := New(1)
	j := q.Start(t.Context(), Op{
		Kind: Copy, From: from.fs, At: from.at, Names: []string{"tree"},
		To: to.fs, Into: to.at,
	}, Options{Ask: Always{What: Rename, Name: "tree (copy)"}})

	if err := ends(t, j); err != nil {
		t.Fatalf("the job: %v", err)
	}
	if got := read(t, to.real, "tree"); got != "something else entirely" {
		t.Fatalf("what was there holds %q", got)
	}
	if got := read(t, to.real, "tree (copy)/one.txt"); got != "one" {
		t.Fatalf("tree (copy)/one.txt = %q", got)
	}
	if got := read(t, to.real, "tree (copy)/sub/two.txt"); got != "two" {
		t.Fatalf("tree (copy)/sub/two.txt = %q", got)
	}
}

// Leaving a directory alone leaves what is inside it alone. The user
// said not to touch that directory.
func TestSkippingADirectorySkipsWhatIsInsideIt(t *testing.T) {
	from, to := local(t), local(t)
	write(t, from.real, "tree/one.txt", "the new one")
	write(t, to.real, "tree", "a file with the same name")

	q := New(1)
	j := q.Start(t.Context(), Op{
		Kind: Copy, From: from.fs, At: from.at, Names: []string{"tree"},
		To: to.fs, Into: to.at,
	}, Options{Ask: Always{What: Skip}})

	if err := ends(t, j); err != nil {
		t.Fatalf("the job: %v", err)
	}
	if got := read(t, to.real, "tree"); got != "a file with the same name" {
		t.Fatalf("what was there holds %q", got)
	}
	if p := j.Progress(); p.Skipped == 0 {
		t.Error("nothing was counted as skipped")
	}
}

// Replacing works whatever the two things are, or says plainly why it
// cannot. Falling through to a create on a name that is taken makes the
// job die with the machine's own words.
func TestReplaceAcrossKinds(t *testing.T) {
	t.Run("a file over a link", func(t *testing.T) {
		from, to := local(t), local(t)
		write(t, from.real, "one.txt", "the new one")
		if err := os.Symlink("elsewhere", filepath.Join(to.real, "one.txt")); err != nil {
			t.Skipf("no link to replace: %v", err)
		}

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
	})

	t.Run("a directory over a file", func(t *testing.T) {
		from, to := local(t), local(t)
		write(t, from.real, "thing/one.txt", "one")
		write(t, to.real, "thing", "a file in the way")

		q := New(1)
		j := q.Start(t.Context(), Op{
			Kind: Copy, From: from.fs, At: from.at, Names: []string{"thing"},
			To: to.fs, Into: to.at,
		}, Options{Ask: Always{What: Replace}})
		if err := ends(t, j); err != nil {
			t.Fatalf("the job: %v", err)
		}
		if got := read(t, to.real, "thing/one.txt"); got != "one" {
			t.Fatalf("thing/one.txt = %q", got)
		}
	})

	t.Run("a file over a full directory", func(t *testing.T) {
		from, to := local(t), local(t)
		write(t, from.real, "thing", "the new one")
		write(t, to.real, "thing/kept.txt", "something nobody named")

		q := New(1)
		j := q.Start(t.Context(), Op{
			Kind: Copy, From: from.fs, At: from.at, Names: []string{"thing"},
			To: to.fs, Into: to.at,
		}, Options{Ask: Always{What: Replace}})

		err := ends(t, j)
		if err == nil {
			t.Fatal("a directory with things in it was replaced")
		}
		if !strings.Contains(err.Error(), "has to go first") {
			t.Fatalf("error = %v, want it to say why", err)
		}
		// And what was in it is untouched: emptying it is a job of its
		// own, and nobody named those files.
		if got := read(t, to.real, "thing/kept.txt"); got != "something nobody named" {
			t.Fatalf("what was inside holds %q", got)
		}
	})
}

// Answering "the same for the rest" to putting something beside cannot
// mean anything for the next one, which has a different name. It keeps
// asking rather than quietly turning into something else.
func TestRenameForAllKeepsAsking(t *testing.T) {
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
	}, Options{Ask: askWith(asked, Choice{What: Rename, Name: "kept.txt", All: true})})

	// The second one is asked about too, and the name is already taken
	// by the first, so it stops rather than writing over it.
	err := ends(t, j)
	if err == nil {
		t.Fatal("the same name was used twice")
	}
	if got := len(asked); got < 2 {
		t.Fatalf("it asked %d times, want it to keep asking", got)
	}
	// Nothing was skipped: skipping is not what was answered.
	if p := j.Progress(); p.Skipped != 0 {
		t.Fatalf("%d were skipped, and none was asked to be", p.Skipped)
	}
}

// A directory the source will not let anything be written into is still
// copied into: the mode it was asked for is set once it is full.
func TestADirectoryThatCannotBeWrittenIntoIsStillFilled(t *testing.T) {
	if os.PathSeparator != '/' {
		t.Skip("Windows has no directory permission bits to lock a copy out with")
	}
	from, to := local(t), local(t)
	write(t, from.real, "tree/one.txt", "one")
	if err := os.Chmod(filepath.Join(from.real, "tree"), 0o555); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(from.real, "tree"), 0o755) })

	q := New(1)
	j := q.Start(t.Context(), Op{
		Kind: Copy, From: from.fs, At: from.at, Names: []string{"tree"},
		To: to.fs, Into: to.at,
	}, Options{})
	if err := ends(t, j); err != nil {
		t.Fatalf("the job: %v", err)
	}

	if got := read(t, to.real, "tree/one.txt"); got != "one" {
		t.Fatalf("tree/one.txt = %q", got)
	}
	info, err := os.Stat(filepath.Join(to.real, "tree"))
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0o555 {
		t.Fatalf("the copied directory is %v, want the 0555 it was copied from", info.Mode().Perm())
	}
	// Put back so the test's own directory can be cleared away.
	if err := os.Chmod(filepath.Join(to.real, "tree"), 0o755); err != nil {
		t.Fatalf("chmod back: %v", err)
	}
}

// A link counts as one of the things there are to do, or a tree with
// links in it never reaches the end of the bar.
func TestALinkCountsAsOneOfThem(t *testing.T) {
	from, to := local(t), local(t)
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
	p := j.Progress()
	if p.Files != 2 || p.FilesDone != 2 {
		t.Fatalf("%d of %d done, want both the file and the link", p.FilesDone, p.Files)
	}
}

// A move within one filesystem says how far it got, which is all the way
// however large the directory was.
func TestARenameMoveSaysItIsDone(t *testing.T) {
	here := local(t)
	write(t, here.real, "tree/one.txt", "one")
	write(t, here.real, "tree/sub/two.txt", "two")
	into := filepath.Join(here.real, "into")
	if err := os.Mkdir(into, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	q := New(1)
	j := q.Start(t.Context(), Op{
		Kind: Move, From: here.fs, At: here.at, Names: []string{"tree"},
		To: here.fs, Into: into,
	}, Options{})
	if err := ends(t, j); err != nil {
		t.Fatalf("the job: %v", err)
	}
	p := j.Progress()
	if p.Files == 0 {
		t.Fatal("it says there was nothing to do")
	}
	if p.Files != p.FilesDone || p.Bytes != p.BytesDone {
		t.Fatalf("%d of %d files and %d of %d bytes done",
			p.FilesDone, p.Files, p.BytesDone, p.Bytes)
	}
}

// A directory named with a separator on the end is the same directory.
// Comparing what the user typed against what a path helper built is a
// comparison of spellings, not of places.
func TestAMoveIsNotFooledByATrailingSeparator(t *testing.T) {
	here := local(t)
	write(t, here.real, "one.txt", "the body")
	into := filepath.Join(here.real, "into")
	if err := os.Mkdir(into, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	q := New(1)
	j := q.Start(t.Context(), Op{
		Kind: Move, From: here.fs, At: here.at + string(os.PathSeparator),
		Names: []string{"one.txt"},
		To:    here.fs, Into: into,
	}, Options{})
	if err := ends(t, j); err != nil {
		t.Fatalf("the job: %v", err)
	}
	if got := read(t, into, "one.txt"); got != "the body" {
		t.Fatalf("what was moved holds %q", got)
	}
	gone(t, here.real, "one.txt")
}

// Deleting something that has already gone is the point of deleting, not
// a failure.
func TestDeletingWhatHasAlreadyGone(t *testing.T) {
	here := local(t)
	write(t, here.real, "tree/one.txt", "one")
	write(t, here.real, "tree/two.txt", "two")

	// Something else takes one of them away while the job is planning.
	q := New(1)
	j := q.Start(t.Context(), Op{
		Kind: Delete, From: vanishing{FS: here.fs, at: filepath.Join(here.real, "tree", "two.txt")},
		At: here.at, Names: []string{"tree"},
	}, Options{})

	if err := ends(t, j); err != nil {
		t.Fatalf("the job: %v", err)
	}
	gone(t, here.real, "tree")
}

// vanishing takes a name away the moment the job asks to remove
// something, which is what anything else on the machine may do.
type vanishing struct {
	vfs.FS
	at string
}

func (v vanishing) Remove(path string) error {
	if v.at != "" {
		_ = os.Remove(v.at)
		v.at = ""
	}
	return v.FS.Remove(path)
}

// The panel reads how far a job has got while the job is running, which
// is the one thing this package exists to allow.
func TestProgressCanBeReadWhileTheJobRuns(t *testing.T) {
	from, to := local(t), local(t)
	for i := range 20 {
		write(t, from.real, "tree/"+string(rune('a'+i))+".txt", strings.Repeat("x", 512))
	}

	q := New(1)
	j := q.Start(t.Context(), Op{
		Kind: Copy, From: from.fs, At: from.at, Names: []string{"tree"},
		To: to.fs, Into: to.at,
	}, Options{})

	// The way the panel does it: over and over, from another goroutine,
	// while the work is going on.
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			default:
			}
			p := j.Progress()
			if p.FilesDone > p.Files && p.Files > 0 {
				panic("more files done than there are")
			}
			_ = p.Current
		}
	}()

	err := ends(t, j)
	close(stop)
	<-done
	if err != nil {
		t.Fatalf("the job: %v", err)
	}
}
