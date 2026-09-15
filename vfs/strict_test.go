package vfs

import (
	"errors"
	"io"
	"io/fs"
	"net"
	"os"
	"runtime"
	"slices"
	"sync"
	"testing"
	"time"

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
func strict(t *testing.T) FS { return strictBlindTo(t, "") }

// strictBlindTo is the same machine, with one path it will not say
// anything about: asking whether it is there is refused rather than
// answered. blind may be empty, and then every path answers.
func strictBlindTo(t *testing.T, blind string) FS {
	t.Helper()
	here, there := net.Pipe()

	handlers := sftp.InMemHandler()
	handlers.FileCmd = posixRenamer{handlers.FileCmd}
	if blind != "" {
		handlers.FileList = blindTo{handlers.FileList, blind}
	}
	server := sftp.NewRequestServer(there, handlers)
	go func() { _ = server.Serve() }()
	t.Cleanup(func() { _ = server.Close() })

	client, err := sftp.NewClientPipe(here, here)
	if err != nil {
		t.Fatalf("start the client: %v", err)
	}
	f := NewSFTP("strict", client, client, client.Close)
	t.Cleanup(func() { _ = f.Close() })
	return f
}

// blindTo refuses to say whether one path is there, the way a directory
// the user may write in but may not read does.
type blindTo struct {
	sftp.FileLister
	at string
}

func (b blindTo) Filelist(r *sftp.Request) (sftp.ListerAt, error) {
	if r.Filepath == b.at {
		return nil, os.ErrPermission
	}
	return b.FileLister.Filelist(r)
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

// Create stops when the machine will not say whether the file is there.
//
// Whether it is there is what decides the mode: one already there keeps
// its own, and a new one is given the mode that was asked for. A failure
// read as "it is new" overwrites the permissions on the user's file.
func TestCreateStopsWhenItCannotTellWhetherTheFileIsThere(t *testing.T) {
	f := strictBlindTo(t, "/one.txt")

	w, err := f.Create("/one.txt", 0o600)
	if err == nil {
		_ = w.Close()
		t.Fatal("Create carried on without knowing whether the file was there")
	}
	if errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("Create = %v, want the refusal the machine gave", err)
	}
}

// The name is changed on the goroutine that draws while listings and
// jobs read it on their own.
//
// Nothing here can assert a race: the race detector is what sees it, and
// this is the shape that gives it something to see.
func TestRenamingWhileTheFilesystemIsRead(t *testing.T) {
	f := strict(t).(*SFTP)

	var wg sync.WaitGroup
	stop := make(chan struct{})
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				// A failing call, because that is the one that puts the
				// name into what it reports.
				if _, err := f.ReadDir("/nowhere"); err == nil {
					return
				}
			}
		}()
	}

	deadline := time.After(time.Second)
	for i := 0; ; i++ {
		f.Renamed("margit-" + string(rune('a'+i%26)))
		select {
		case <-deadline:
			close(stop)
			wg.Wait()
			return
		default:
		}
		if i > 200 {
			close(stop)
			wg.Wait()
			return
		}
	}
}

// A machine reached over SFTP starts at the root, whatever this machine
// calls its own drives.
func TestTheRootsOfAMachineOverSFTP(t *testing.T) {
	f := strict(t)
	if got := f.Roots(); len(got) != 1 || got[0] != "/" {
		t.Fatalf("Roots = %v, want the POSIX root", got)
	}
}

// This machine's roots are what it says it has.
//
// On Windows that is one per drive, and a drive with nothing in it is
// still a drive: a pane that could not go to an empty card reader could
// not tell the user it was empty. Everywhere else it is the one root
// everything hangs off.
func TestTheRootsOfThisMachine(t *testing.T) {
	got := NewLocal().Roots()
	if len(got) == 0 {
		t.Fatal("this machine offered nowhere to start from")
	}
	for _, at := range got {
		if _, err := NewLocal().ReadDir(at); err != nil {
			// A drive that will not answer is still a drive, so this is
			// not a failure. It is only worth saying.
			t.Logf("%s is offered and would not list: %v", at, err)
		}
	}
	if runtime.GOOS != "windows" {
		if len(got) != 1 || got[0] != "/" {
			t.Fatalf("Roots = %v, want the POSIX root", got)
		}
		return
	}
	// Everywhere a probe would go, and at least there: a drive that is
	// present and will not answer is still a drive, and a pane that was
	// not offered it could not tell the user it was empty.
	for letter := 'A'; letter <= 'Z'; letter++ {
		at := string(letter) + `:\`
		if _, err := os.Stat(at); err != nil {
			continue
		}
		if !slices.Contains(got, at) {
			t.Errorf("%s answers and is not offered", at)
		}
	}
	for _, at := range got {
		if len(at) != 3 || at[1] != ':' || at[2] != '\\' {
			t.Errorf("%q is not a drive", at)
		}
	}
}
