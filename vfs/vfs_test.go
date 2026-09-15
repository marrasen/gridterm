package vfs

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
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

	files, err := conn.Files(t.Context())
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

	if _, err := conn.Files(t.Context()); err == nil {
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
	files, err := conn.Files(t.Context())
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

// The path helpers on the shapes a Windows machine has: a drive, a
// share, and a path that is only separators.
//
// A drive letter on its own means the current directory on that drive,
// not the top of it, so going up from C:\Users has to land on C:\ and
// not on C:.
func TestWindowsPaths(t *testing.T) {
	// A filesystem with backslashes whatever this machine uses, so the
	// shapes are tested wherever the tests run.
	f := windows{NewLocal()}

	cases := []struct{ what, got, want string }{
		{`Join a drive and a name`, Join(f, `C:\`, "Users"), `C:\Users`},
		{`Join a bare drive and a name`, Join(f, `C:`, "Users"), `C:\Users`},
		{`Join a drive alone`, Join(f, `C:\`), `C:\`},
		{`Join a share`, Join(f, `\\server\share`, "sub"), `\\server\share\sub`},
		{`Join past an empty part`, Join(f, "", `C:\`, "Users"), `C:\Users`},
		{`Dir under a drive`, Dir(f, `C:\Users`), `C:\`},
		{`Dir of a drive`, Dir(f, `C:\`), `C:\`},
		{`Dir deeper`, Dir(f, `C:\Users\marcus`), `C:\Users`},
		{`Base of a drive`, Base(f, `C:\`), `C:`},
		{`Base deeper`, Base(f, `C:\Users\marcus`), "marcus"},
	}
	for _, tc := range cases {
		if tc.got != tc.want {
			t.Errorf("%s = %q, want %q", tc.what, tc.got, tc.want)
		}
	}
	if !IsTop(f, `C:\`) {
		t.Error("a drive root says it is not the top")
	}
	if IsTop(f, `C:\Users`) {
		t.Error("a directory under a drive says it is the top")
	}
}

// A part that names where it starts decides, even when an empty one came
// before it: an absolute path must not quietly become a relative one.
func TestJoinKeepsWhereThePathStarts(t *testing.T) {
	f := &SFTP{name: "far"}
	if got := Join(f, "", "/tmp", "x"); got != "/tmp/x" {
		t.Errorf("Join past an empty part = %q, want /tmp/x", got)
	}
	if got := Join(f, "/"); got != "/" {
		t.Errorf("Join of the root alone = %q", got)
	}
	if got := Join(f, "/", "tmp"); got != "/tmp" {
		t.Errorf("Join under the root = %q", got)
	}
	if got := Base(f, "/"); got != "/" {
		t.Errorf("Base of the root = %q, want something to show", got)
	}
	if got := Dir(f, "/foo//bar"); got != "/foo" {
		t.Errorf("Dir with a doubled separator = %q", got)
	}
}

// windows is a filesystem that names paths the way Windows does,
// whatever machine the tests are running on.
type windows struct{ *Local }

func (windows) Sep() byte { return '\\' }

// A file that is already there keeps the mode it has. A copy over an
// existing file changes what is in it, not who may read it -- and the
// two filesystems have to agree, or the same copy would do different
// things depending on which end it landed on.
func TestCreateLeavesAnExistingFilesModeAlone(t *testing.T) {
	both(t, func(t *testing.T, tr tree) {
		path := filepath.Join(tr.real, "one.txt")
		write(t, tr.real, "one.txt", "hello")
		before, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat: %v", err)
		}

		// Asked for read-only, over a file that is already there and is
		// not. The mode it has wins, on both filesystems.
		w, err := tr.fs.Create(Join(tr.fs, tr.at, "one.txt"), 0o444)
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		if err := w.Close(); err != nil {
			t.Fatalf("close: %v", err)
		}

		after, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat: %v", err)
		}
		if after.Mode().Perm() != before.Mode().Perm() {
			t.Fatalf("the file is %v, want the %v it already had",
				after.Mode().Perm(), before.Mode().Perm())
		}
		if after.Mode().Perm()&0o200 == 0 {
			t.Fatal("a file that could be written to before cannot be now")
		}
	})
}

// A new directory gets the mode it was asked for, not whatever the
// machine's umask allows. Both filesystems, or a copy would make
// directories the user cannot enter at one end and can at the other.
func TestMkdirUsesTheModeItWasGiven(t *testing.T) {
	if os.PathSeparator != '/' {
		t.Skip("Windows has no permission bits to set on a directory")
	}
	both(t, func(t *testing.T, tr tree) {
		if err := tr.fs.Mkdir(Join(tr.fs, tr.at, "made"), 0o777); err != nil {
			t.Fatalf("Mkdir: %v", err)
		}
		info, err := os.Stat(filepath.Join(tr.real, "made"))
		if err != nil {
			t.Fatalf("stat: %v", err)
		}
		if info.Mode().Perm() != 0o777 {
			t.Fatalf("the directory is %v, want the 0777 it was made with", info.Mode().Perm())
		}
	})
}

// The interface promises a filesystem is safe to use from several
// goroutines, because a copy running in the background reads through the
// same one a pane is listing with.
func TestAFilesystemTakesSeveralGoroutinesAtOnce(t *testing.T) {
	both(t, func(t *testing.T, tr tree) {
		for _, name := range []string{"one.txt", "two.txt", "three.txt"} {
			write(t, tr.real, name, "the body of "+name)
		}

		var wg sync.WaitGroup
		fail := make(chan error, 64)
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := 0; j < 8; j++ {
					if _, err := tr.fs.ReadDir(tr.at); err != nil {
						fail <- err
						return
					}
					f, err := tr.fs.Open(Join(tr.fs, tr.at, "two.txt"))
					if err != nil {
						fail <- err
						return
					}
					body, err := io.ReadAll(f)
					if err != nil {
						fail <- err
						_ = f.Close()
						return
					}
					if err := f.Close(); err != nil {
						fail <- err
						return
					}
					if string(body) != "the body of two.txt" {
						fail <- errors.New("a file read through a busy filesystem came back wrong")
						return
					}
				}
			}()
		}
		wg.Wait()
		close(fail)
		for err := range fail {
			t.Fatalf("reading from several goroutines: %v", err)
		}
	})
}

// Renaming onto a name that is already there replaces it, on both
// filesystems. SFTP's own rename refuses it and renaming on this machine
// does not, so a job that moved a file would do different things
// depending on which end it landed on.
func TestRenameReplacesWhatIsThere(t *testing.T) {
	both(t, func(t *testing.T, tr tree) {
		write(t, tr.real, "one.txt", "the new one")
		write(t, tr.real, "two.txt", "the old one")

		from := Join(tr.fs, tr.at, "one.txt")
		to := Join(tr.fs, tr.at, "two.txt")
		if err := tr.fs.Rename(from, to); err != nil {
			t.Fatalf("Rename: %v", err)
		}
		got, err := os.ReadFile(filepath.Join(tr.real, "two.txt"))
		if err != nil {
			t.Fatalf("read it back: %v", err)
		}
		if string(got) != "the new one" {
			t.Fatalf("the file holds %q, want what was moved onto it", got)
		}
		if _, err := os.Stat(filepath.Join(tr.real, "one.txt")); !errors.Is(err, fs.ErrNotExist) {
			t.Error("the old name is still there")
		}
	})
}

// A name that is already taken says so in a way a caller can act on. A
// browser asking "there is already a folder called that" can only do it
// if both filesystems say the same thing.
func TestMkdirSaysWhenTheNameIsTaken(t *testing.T) {
	both(t, func(t *testing.T, tr tree) {
		at := Join(tr.fs, tr.at, "made")
		if err := tr.fs.Mkdir(at, 0o755); err != nil {
			t.Fatalf("Mkdir: %v", err)
		}
		err := tr.fs.Mkdir(at, 0o755)
		if err == nil {
			t.Fatal("a directory that was already there was made again")
		}
		if !errors.Is(err, fs.ErrExist) {
			t.Fatalf("error = %v, want it to say the name is taken", err)
		}
	})
}

// A directory is not a file. Refused where it is asked for, rather than
// at the first read: a copy that got this far would already have emptied
// the file it was copying to.
func TestOpenRefusesADirectory(t *testing.T) {
	both(t, func(t *testing.T, tr tree) {
		if err := os.Mkdir(filepath.Join(tr.real, "sub"), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		f, err := tr.fs.Open(Join(tr.fs, tr.at, "sub"))
		if err == nil {
			_ = f.Close()
			t.Fatal("a directory opened as a file")
		}
		if !strings.Contains(err.Error(), "directory") {
			t.Fatalf("error = %v, want it to say it is a directory", err)
		}
	})
}

// A link whose target cannot be read fails the listing rather than being
// shown as a link to nowhere. A dangling link reads back perfectly well,
// so a failure here means something else.
func TestADanglingLinkIsStillReadable(t *testing.T) {
	both(t, func(t *testing.T, tr tree) {
		if err := os.Symlink("gone.txt", filepath.Join(tr.real, "dead")); err != nil {
			t.Skipf("no link to test with: %v", err)
		}

		got, err := tr.fs.ReadDir(tr.at)
		if err != nil {
			t.Fatalf("ReadDir: %v", err)
		}
		dead := find(t, got, "dead")
		if !dead.IsLink() {
			t.Fatalf("the link is listed as %v", dead.Mode)
		}
		if dead.Link != "gone.txt" {
			t.Fatalf("the link points at %q, want gone.txt", dead.Link)
		}
	})
}

// A directory that is there but cannot be read is a failure, which is
// the case the whole rule is about: a browser showing nothing where it
// was not allowed to look says the directory is empty.
func TestReadDirSaysWhenItIsNotAllowed(t *testing.T) {
	if os.PathSeparator != '/' {
		t.Skip("a directory on Windows cannot be made unreadable with a mode")
	}
	both(t, func(t *testing.T, tr tree) {
		shut := filepath.Join(tr.real, "shut")
		if err := os.Mkdir(shut, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		write(t, shut, "one.txt", "hello")
		if err := os.Chmod(shut, 0o000); err != nil {
			t.Fatalf("chmod: %v", err)
		}
		t.Cleanup(func() { _ = os.Chmod(shut, 0o755) })

		got, err := tr.fs.ReadDir(Join(tr.fs, tr.at, "shut"))
		if err == nil {
			t.Fatalf("a directory that may not be read listed %v", names(got))
		}
		if got != nil {
			t.Fatalf("a failed listing returned %v as well as the failure", names(got))
		}
		if !errors.Is(err, fs.ErrPermission) {
			t.Fatalf("error = %v, want it to say it is not allowed", err)
		}
	})
}

// A link can be made as well as read, which is what a copy needs to put
// a tree down on the other machine as it found it.
func TestSymlink(t *testing.T) {
	both(t, func(t *testing.T, tr tree) {
		write(t, tr.real, "one.txt", "hello")
		if err := tr.fs.Symlink("one.txt", Join(tr.fs, tr.at, "link")); err != nil {
			t.Skipf("no link can be made here: %v", err)
		}

		got, err := tr.fs.Stat(Join(tr.fs, tr.at, "link"))
		if err != nil {
			t.Fatalf("Stat: %v", err)
		}
		if !got.IsLink() {
			t.Fatalf("what was made is %v, want a link", got.Mode)
		}
		if got.Link != "one.txt" {
			t.Fatalf("it points at %q, want one.txt", got.Link)
		}
		// A link to nothing is still a link: what it points at is not
		// checked, because that is what a link is.
		if err := tr.fs.Symlink("gone.txt", Join(tr.fs, tr.at, "dead")); err != nil {
			t.Fatalf("a link to nothing: %v", err)
		}
	})
}

// Two values standing for the same place are the same filesystem, so a
// move between two directories on one machine is a rename rather than a
// copy and a delete.
//
// Comparing the values themselves answers nothing: a filesystem with no
// fields at all gives the same pointer every time it is made.
func TestSame(t *testing.T) {
	one, two := NewLocal(), NewLocal()
	if !Same(one, two) {
		t.Error("two values for this machine are not the same place")
	}
	if !Same(one, one) {
		t.Error("a filesystem is not itself")
	}

	far := &SFTP{name: "margit"}
	alsoFar := &SFTP{name: "margit"}
	elsewhere := &SFTP{name: "web1"}
	if !Same(far, alsoFar) {
		t.Error("two sessions to one machine are not the same place")
	}
	if Same(far, elsewhere) {
		t.Error("two machines are the same place")
	}
	if Same(one, far) {
		t.Error("this machine and another are the same place")
	}
	if Same(nil, one) || Same(one, nil) || Same(nil, nil) {
		t.Error("nothing is the same place as something")
	}

	// The separator is not what tells them apart: a filesystem that
	// names paths like Windows is still this machine.
	if !Same(windows{NewLocal()}, windows{NewLocal()}) {
		t.Error("two of one kind are not the same place")
	}
	if Same(windows{NewLocal()}, one) {
		t.Error("two kinds are the same place")
	}
}
