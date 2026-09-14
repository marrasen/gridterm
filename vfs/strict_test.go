package vfs

import (
	"errors"
	"io"
	"io/fs"
	"net"
	"testing"

	"github.com/pkg/sftp"
)

// strict is a filesystem on a machine that behaves the way OpenSSH's
// sftp-server does rather than the way the Go server in the test harness
// does.
//
// The two differ on exactly the operations where this package has to
// paper over a difference, so a test that only ever met the Go server
// would not see the papering fail. Plain rename refuses a name that is
// already taken here, and only the posix-rename extension replaces one.
func strict(t *testing.T) FS {
	t.Helper()
	here, there := net.Pipe()

	handlers := sftp.InMemHandler()
	handlers.FileCmd = posixRenamer{handlers.FileCmd}
	server := sftp.NewRequestServer(there, handlers)
	go func() { _ = server.Serve() }()
	t.Cleanup(func() { _ = server.Close() })

	client, err := sftp.NewClientPipe(here, here)
	if err != nil {
		t.Fatalf("start the client: %v", err)
	}
	f := NewSFTP("strict", client, client.Close)
	t.Cleanup(func() { _ = f.Close() })
	return f
}

// posixRenamer adds the extension OpenSSH has and the in-memory handler
// does not: a rename that replaces what is there.
type posixRenamer struct{ sftp.FileCmder }

func (p posixRenamer) PosixRename(r *sftp.Request) error {
	// What the extension is for: the name being replaced goes first.
	// A target that is not there is not a failure.
	_ = p.FileCmder.Filecmd(&sftp.Request{Method: "Remove", Filepath: r.Target})
	return p.FileCmder.Filecmd(&sftp.Request{
		Method: "Rename", Filepath: r.Filepath, Target: r.Target,
	})
}

// put writes a file through a filesystem, for a test with no disk of its
// own to check with.
func put(t *testing.T, f FS, path, body string) {
	t.Helper()
	w, err := f.Create(path, 0o644)
	if err != nil {
		t.Fatalf("create %s: %v", path, err)
	}
	if _, err := io.WriteString(w, body); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close %s: %v", path, err)
	}
}

// read reads a file back through a filesystem.
func read(t *testing.T, f FS, path string) string {
	t.Helper()
	r, err := f.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer r.Close()
	body, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(body)
}

// A machine whose plain rename refuses a name that is already taken
// still replaces it, because the extension that does is used where the
// machine has it.
//
// Renaming on this machine replaces what is there, so without this a job
// that moved a file would do one thing at one end and another at the
// other.
func TestRenameReplacesOnAStrictMachine(t *testing.T) {
	f := strict(t)
	put(t, f, "/one.txt", "the new one")
	put(t, f, "/two.txt", "the old one")

	if err := f.Rename("/one.txt", "/two.txt"); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if got := read(t, f, "/two.txt"); got != "the new one" {
		t.Fatalf("the file holds %q, want what was moved onto it", got)
	}
	if _, err := f.Stat("/one.txt"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the old name is still there: %v", err)
	}
}

// The same machine, and the other operations still behave.
func TestAStrictMachineBehavesLikeTheOthers(t *testing.T) {
	f := strict(t)
	if err := f.Mkdir("/made", 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	if err := f.Mkdir("/made", 0o755); !errors.Is(err, fs.ErrExist) {
		t.Fatalf("making it twice = %v, want it to say the name is taken", err)
	}

	put(t, f, "/made/one.txt", "hello")
	got, err := f.ReadDir("/made")
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(got) != 1 || got[0].Name != "one.txt" {
		t.Fatalf("the listing is %v", names(got))
	}
	if _, err := f.ReadDir("/nowhere"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("listing what is not there = %v", err)
	}
	if err := f.Remove("/made/one.txt"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := f.Stat("/made/one.txt"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("it is still there: %v", err)
	}
}
