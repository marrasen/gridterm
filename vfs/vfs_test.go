package vfs

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"

	"github.com/marrasen/gridterm/internal/sshtest"
	"github.com/marrasen/gridterm/remote"
)

// tree is a filesystem to test and a directory on it that the test owns.
type tree struct {
	fs FS

	// at is a directory the test may do what it likes in, named the way
	// this filesystem names things.
	at string

	// real is the same directory as this machine sees it, for a test
	// that wants to check with os what the filesystem did.
	real string
}

// both runs a test against this machine and against a machine reached
// over SSH, so neither implementation can drift from the other.
func both(t *testing.T, run func(*testing.T, tree)) {
	t.Helper()
	t.Run("local", func(t *testing.T) {
		dir := t.TempDir()
		run(t, tree{fs: NewLocal(), at: dir, real: dir})
	})
	t.Run("sftp", func(t *testing.T) {
		dir := t.TempDir()
		// SFTP names everything with slashes, whatever this machine uses.
		run(t, tree{fs: overSSH(t), at: filepath.ToSlash(dir), real: dir})
	})
}

// overSSH returns the filesystem of the in-process SSH server, which is
// this machine seen through SFTP.
func overSSH(t *testing.T) FS {
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

	files, err := conn.Files()
	if err != nil {
		t.Fatalf("start SFTP: %v", err)
	}
	f := NewSFTP("tester@"+host, files.Client(), files.Close)
	t.Cleanup(func() { _ = f.Close() })
	return f
}

// write puts a file on this machine, for a test to read back through the
// filesystem under test.
func write(t *testing.T, at, name, body string) string {
	t.Helper()
	path := filepath.Join(at, name)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

// find returns the entry with a name, or fails.
func find(t *testing.T, entries []Entry, name string) Entry {
	t.Helper()
	for _, e := range entries {
		if e.Name == name {
			return e
		}
	}
	t.Fatalf("there is no %q in %v", name, names(entries))
	return Entry{}
}

// names lists what a listing holds, for a failure message.
func names(entries []Entry) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.Name
	}
	return out
}

// A listing says what is there, what it is, and how big.
func TestReadDir(t *testing.T) {
	both(t, func(t *testing.T, tr tree) {
		write(t, tr.real, "one.txt", "hello")
		if err := os.Mkdir(filepath.Join(tr.real, "sub"), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}

		got, err := tr.fs.ReadDir(tr.at)
		if err != nil {
			t.Fatalf("ReadDir: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("the listing is %v, want two names", names(got))
		}
		file := find(t, got, "one.txt")
		if file.IsDir() {
			t.Error("a file is listed as a directory")
		}
		if file.Size != 5 {
			t.Errorf("one.txt is %d bytes, want 5", file.Size)
		}
		if file.Mod.IsZero() {
			t.Error("one.txt has no time on it")
		}
		if !find(t, got, "sub").IsDir() {
			t.Error("a directory is not listed as one")
		}
	})
}

// A directory that cannot be read is a failure, not an empty listing. A
// browser showing nothing where it could not look is telling the user
// their directory is empty.
func TestReadDirSaysWhenItCannotRead(t *testing.T) {
	both(t, func(t *testing.T, tr tree) {
		got, err := tr.fs.ReadDir(Join(tr.fs, tr.at, "nowhere"))
		if err == nil {
			t.Fatalf("a directory that is not there listed %v", names(got))
		}
		if got != nil {
			t.Fatalf("a failed listing returned %v as well as the failure", names(got))
		}
		if !strings.Contains(err.Error(), tr.fs.Name()) {
			t.Errorf("the failure does not say which machine: %v", err)
		}
		if !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("error = %v, want it to say there is no such directory", err)
		}
	})
}

// Reading a file gives what is in it.
func TestOpen(t *testing.T) {
	both(t, func(t *testing.T, tr tree) {
		write(t, tr.real, "one.txt", "the whole thing")

		f, err := tr.fs.Open(Join(tr.fs, tr.at, "one.txt"))
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		defer f.Close()
		got, err := io.ReadAll(f)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if string(got) != "the whole thing" {
			t.Fatalf("the file holds %q", got)
		}
	})
}

// A file that is not there is a failure that says so.
func TestOpenSaysWhenThereIsNothingThere(t *testing.T) {
	both(t, func(t *testing.T, tr tree) {
		f, err := tr.fs.Open(Join(tr.fs, tr.at, "nowhere.txt"))
		if err == nil {
			_ = f.Close()
			t.Fatal("a file that is not there opened")
		}
		if !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("error = %v, want it to say there is no such file", err)
		}
	})
}

// Writing a file puts it there, with the permissions that were asked
// for.
func TestCreate(t *testing.T) {
	both(t, func(t *testing.T, tr tree) {
		w, err := tr.fs.Create(Join(tr.fs, tr.at, "new.txt"), 0o600)
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		if _, err := io.WriteString(w, "written"); err != nil {
			t.Fatalf("write: %v", err)
		}
		if err := w.Close(); err != nil {
			t.Fatalf("close: %v", err)
		}

		got, err := os.ReadFile(filepath.Join(tr.real, "new.txt"))
		if err != nil {
			t.Fatalf("read it back: %v", err)
		}
		if string(got) != "written" {
			t.Fatalf("the file holds %q", got)
		}
		// Windows has no permission bits worth the name, so the whole
		// mode is only asked of the machines that have them.
		if os.PathSeparator == '/' {
			info, err := os.Stat(filepath.Join(tr.real, "new.txt"))
			if err != nil {
				t.Fatalf("stat: %v", err)
			}
			if info.Mode().Perm() != 0o600 {
				t.Errorf("the file is %v, want the mode it was made with", info.Mode().Perm())
			}
		}

		// The one bit every machine has: a file made read-only is
		// read-only. SFTP's own create carries no mode at all, so
		// without setting it afterwards this would be whatever the far
		// end's umask said.
		locked := Join(tr.fs, tr.at, "locked.txt")
		w, err = tr.fs.Create(locked, 0o444)
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		if err := w.Close(); err != nil {
			t.Fatalf("close: %v", err)
		}
		info, err := os.Stat(filepath.Join(tr.real, "locked.txt"))
		if err != nil {
			t.Fatalf("stat: %v", err)
		}
		if info.Mode().Perm()&0o200 != 0 {
			t.Errorf("the file is %v, want one nothing can write to", info.Mode().Perm())
		}
		// Put back, or the directory cannot be cleared away afterwards.
		if err := os.Chmod(filepath.Join(tr.real, "locked.txt"), 0o644); err != nil {
			t.Fatalf("chmod back: %v", err)
		}
	})
}

// Creating over a file replaces it rather than leaving the old ending
// behind: a shorter file written over a longer one must not keep the
// tail of what was there.
func TestCreateReplacesWhatWasThere(t *testing.T) {
	both(t, func(t *testing.T, tr tree) {
		write(t, tr.real, "one.txt", "a long line that was already here")

		w, err := tr.fs.Create(Join(tr.fs, tr.at, "one.txt"), 0o644)
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		if _, err := io.WriteString(w, "short"); err != nil {
			t.Fatalf("write: %v", err)
		}
		if err := w.Close(); err != nil {
			t.Fatalf("close: %v", err)
		}

		got, err := os.ReadFile(filepath.Join(tr.real, "one.txt"))
		if err != nil {
			t.Fatalf("read it back: %v", err)
		}
		if string(got) != "short" {
			t.Fatalf("the file holds %q, want only what was written", got)
		}
	})
}

// Making, renaming and removing all do what they say, and say so when
// they cannot.
func TestMkdirRenameRemove(t *testing.T) {
	both(t, func(t *testing.T, tr tree) {
		dir := Join(tr.fs, tr.at, "made")
		if err := tr.fs.Mkdir(dir, 0o755); err != nil {
			t.Fatalf("Mkdir: %v", err)
		}
		if info, err := os.Stat(filepath.Join(tr.real, "made")); err != nil || !info.IsDir() {
			t.Fatalf("the directory was not made: %v", err)
		}
		// Twice is a failure, not a quiet success.
		if err := tr.fs.Mkdir(dir, 0o755); err == nil {
			t.Fatal("a directory that was already there was made again")
		}

		write(t, tr.real, "one.txt", "hello")
		from, to := Join(tr.fs, tr.at, "one.txt"), Join(tr.fs, tr.at, "two.txt")
		if err := tr.fs.Rename(from, to); err != nil {
			t.Fatalf("Rename: %v", err)
		}
		if _, err := os.Stat(filepath.Join(tr.real, "one.txt")); !errors.Is(err, fs.ErrNotExist) {
			t.Error("the old name is still there")
		}
		if _, err := os.Stat(filepath.Join(tr.real, "two.txt")); err != nil {
			t.Errorf("the new name is not there: %v", err)
		}

		if err := tr.fs.Remove(to); err != nil {
			t.Fatalf("Remove: %v", err)
		}
		if _, err := os.Stat(filepath.Join(tr.real, "two.txt")); !errors.Is(err, fs.ErrNotExist) {
			t.Error("the file is still there after being removed")
		}
		// And removing what is not there says so.
		if err := tr.fs.Remove(to); err == nil {
			t.Fatal("removing something that is not there was a success")
		}
	})
}

// A directory with something in it is not removed. Remove takes one
// name; emptying a tree is a job, and one of those can be cancelled.
func TestRemoveWillNotEmptyADirectory(t *testing.T) {
	both(t, func(t *testing.T, tr tree) {
		if err := os.Mkdir(filepath.Join(tr.real, "full"), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		write(t, filepath.Join(tr.real, "full"), "one.txt", "hello")

		if err := tr.fs.Remove(Join(tr.fs, tr.at, "full")); err == nil {
			t.Fatal("a directory with something in it was removed")
		}
		if _, err := os.Stat(filepath.Join(tr.real, "full", "one.txt")); err != nil {
			t.Errorf("what was in it has gone: %v", err)
		}
	})
}

// Stat reads one name.
func TestStat(t *testing.T) {
	both(t, func(t *testing.T, tr tree) {
		write(t, tr.real, "one.txt", "hello")

		got, err := tr.fs.Stat(Join(tr.fs, tr.at, "one.txt"))
		if err != nil {
			t.Fatalf("Stat: %v", err)
		}
		if got.Name != "one.txt" {
			t.Errorf("the name is %q", got.Name)
		}
		if got.Size != 5 {
			t.Errorf("the size is %d, want 5", got.Size)
		}
		if got.IsDir() {
			t.Error("a file says it is a directory")
		}
		if _, err := tr.fs.Stat(Join(tr.fs, tr.at, "nowhere")); err == nil {
			t.Fatal("something that is not there was read")
		}
	})
}

// Setting the permissions sets them.
func TestChmod(t *testing.T) {
	if os.PathSeparator != '/' {
		t.Skip("Windows has no permission bits to set")
	}
	both(t, func(t *testing.T, tr tree) {
		write(t, tr.real, "one.txt", "hello")
		if err := tr.fs.Chmod(Join(tr.fs, tr.at, "one.txt"), 0o600); err != nil {
			t.Fatalf("Chmod: %v", err)
		}
		info, err := os.Stat(filepath.Join(tr.real, "one.txt"))
		if err != nil {
			t.Fatalf("stat: %v", err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("the file is %v, want 0600", info.Mode().Perm())
		}
	})
}

// A symbolic link is shown as a link, with what it points at, rather
// than being followed: the user decides what to do about it.
func TestLinksAreShownRatherThanFollowed(t *testing.T) {
	both(t, func(t *testing.T, tr tree) {
		write(t, tr.real, "one.txt", "hello")
		if err := os.Symlink("one.txt", filepath.Join(tr.real, "link")); err != nil {
			// Making one on Windows needs a privilege the tests may not
			// have. Tried rather than skipped by which machine this is,
			// so it runs wherever it can.
			t.Skipf("no link to test with: %v", err)
		}

		got, err := tr.fs.ReadDir(tr.at)
		if err != nil {
			t.Fatalf("ReadDir: %v", err)
		}
		link := find(t, got, "link")
		if !link.IsLink() {
			t.Fatalf("the link is listed as %v", link.Mode)
		}
		if link.Link != "one.txt" {
			t.Errorf("the link points at %q, want one.txt", link.Link)
		}

		one, err := tr.fs.Stat(Join(tr.fs, tr.at, "link"))
		if err != nil {
			t.Fatalf("Stat: %v", err)
		}
		if !one.IsLink() {
			t.Fatalf("Stat followed the link: %v", one.Mode)
		}
	})
}

// Home is somewhere that can be listed, which is what a pane opens on.
func TestHome(t *testing.T) {
	both(t, func(t *testing.T, tr tree) {
		home, err := tr.fs.Home()
		if err != nil {
			t.Fatalf("Home: %v", err)
		}
		if home == "" {
			t.Fatal("home is nowhere")
		}
		if _, err := tr.fs.ReadDir(home); err != nil {
			t.Fatalf("home cannot be listed: %v", err)
		}
	})
}

// The two filesystems put paths together their own way, because a
// Windows pane and a POSIX one sit side by side.
func TestPaths(t *testing.T) {
	posix := &SFTP{name: "far"}
	if got := Join(posix, "/home/marcus", "work"); got != "/home/marcus/work" {
		t.Errorf("Join = %q", got)
	}
	if got := Join(posix, "/home/marcus/", "/work/"); got != "/home/marcus/work" {
		t.Errorf("Join with spare separators = %q", got)
	}
	if got := Join(posix, "home", "work"); got != "home/work" {
		t.Errorf("Join of a relative path = %q", got)
	}
	if got := Dir(posix, "/home/marcus/work"); got != "/home/marcus" {
		t.Errorf("Dir = %q", got)
	}
	if got := Dir(posix, "/home"); got != "/" {
		t.Errorf("Dir under the root = %q", got)
	}
	if got := Dir(posix, "/"); got != "/" {
		t.Errorf("Dir of the root = %q", got)
	}
	if !IsTop(posix, "/") {
		t.Error("the root is not the top")
	}
	if IsTop(posix, "/home") {
		t.Error("a directory under the root says it is the top")
	}
	if got := Base(posix, "/home/marcus/work"); got != "work" {
		t.Errorf("Base = %q", got)
	}
	if got := Base(posix, "/home/marcus/work/"); got != "work" {
		t.Errorf("Base with a trailing separator = %q", got)
	}

	local := NewLocal()
	if got := Base(local, Join(local, "one", "two")); got != "two" {
		t.Errorf("Base on this machine = %q", got)
	}
	if got := Dir(local, Join(local, "one", "two")); got != "one" {
		t.Errorf("Dir on this machine = %q", got)
	}
}

// A machine that will not do SFTP says so where it was asked, rather
// than opening a pane that cannot list anything.
func TestSFTPSaysWhenTheMachineWillNotDoIt(t *testing.T) {
	s := sshtest.New(t)
	s.RefuseSFTP()
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

	if _, err := conn.Files(); err == nil {
		t.Fatal("SFTP started on a machine that refuses it")
	}
}

// Closing the connection closes the file session riding on it.
func TestClosingTheConnectionClosesTheFiles(t *testing.T) {
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
	files, err := conn.Files()
	if err != nil {
		t.Fatalf("start SFTP: %v", err)
	}
	f := NewSFTP("far", files.Client(), files.Close)

	if err := conn.Close(); err != nil {
		t.Fatalf("close the connection: %v", err)
	}
	if _, err := f.ReadDir("/"); err == nil {
		t.Fatal("the filesystem still listed a directory after its connection closed")
	}
}
