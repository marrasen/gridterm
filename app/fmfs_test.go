package app

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/marrasen/kakel/machines"
	"github.com/marrasen/kakel/remote"
	"github.com/marrasen/kakel/vfs"
	"github.com/pkg/sftp"

	"github.com/marrasen/gunim/filemanager"
)

// sftpHere is this machine's files over SFTP, as a server's are read.
func sftpHere(t *testing.T) vfs.FS {
	t.Helper()
	here, there := net.Pipe()
	server, err := sftp.NewServer(there)
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = server.Serve() }()
	t.Cleanup(func() { _ = server.Close() })
	client, err := sftp.NewClientPipe(here, here)
	if err != nil {
		t.Fatal(err)
	}
	f := vfs.NewSFTP("web", nil, client, client.Close)
	t.Cleanup(func() { _ = f.Close() })
	return f
}

// A server's files read in the file manager as its own do: a file new
// where one is refused, a link followed by Stat and not by Lstat, and
// once the connection ends, every call says so.
func TestAServersFilesInTheFileManager(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the SFTP server's paths are this machine's, which on Windows are not slash paths")
	}
	dir := t.TempDir()
	f := sftpHere(t)
	m := newFMFS(serverFS+"s1", f)
	var _ filemanager.Linker = m
	var _ filemanager.Stamper = m
	var _ filemanager.SpaceReporter = m
	var _ filemanager.VolumeNamer = m

	w, err := m.Create(dir + "/a.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(w, "hello"); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(filepath.Join(dir, "a.txt")); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("the new file is %v, %v", info, err)
	}
	if _, err := m.Create(dir + "/a.txt"); !errors.Is(err, fs.ErrExist) {
		t.Fatalf("a second create made %v", err)
	}
	if err := m.Mkdir(dir+"/sub", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := m.Mkdir(dir+"/sub", 0o755); !errors.Is(err, fs.ErrExist) {
		t.Fatalf("a second mkdir made %v", err)
	}
	if err := m.Symlink("a.txt", dir+"/link"); err != nil {
		t.Fatal(err)
	}
	if info, err := m.Stat(dir + "/link"); err != nil || info.Mode().Type() != 0 || info.Size() != 5 {
		t.Fatalf("Stat of the link is %v, %v", info, err)
	}
	if info, err := m.Lstat(dir + "/link"); err != nil || info.Mode().Type() != fs.ModeSymlink {
		t.Fatalf("Lstat of the link is %v, %v", info, err)
	}
	if to, err := m.Readlink(dir + "/link"); err != nil || to != "a.txt" {
		t.Fatalf("the link goes to %q, %v", to, err)
	}
	entries, err := m.ReadDir(context.Background(), dir)
	if err != nil || len(entries) != 3 {
		t.Fatalf("the folder reads %v, %v", entries, err)
	}
	r, err := m.Open(dir + "/a.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Seek(1, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	if b, _ := io.ReadAll(r); string(b) != "ello" {
		t.Fatalf("read %q from the second byte", b)
	}
	_ = r.Close()
	if free, total, err := m.Space(dir); err != nil || total == 0 || free > total {
		t.Fatalf("the space is %d free of %d", free, total)
	}
	if home, err := m.Home(); err != nil || m.homeDir() != home {
		t.Fatalf("home is %q, kept %q, %v", home, m.homeDir(), err)
	}

	if err := m.Rename(dir+"/link", dir+"/a.txt"); err != nil {
		t.Fatal(err)
	}
	if info, err := m.Lstat(dir + "/a.txt"); err != nil || info.Mode().Type() != fs.ModeSymlink {
		t.Fatalf("renamed over, a.txt is %v, %v", info, err)
	}

	m.set(nil)
	if _, err := m.ReadDir(context.Background(), dir); err == nil || !strings.Contains(err.Error(), "connection to web ended") {
		t.Fatalf("with the connection gone, a read made %v", err)
	}
	m.set(f)
	if _, err := m.ReadDir(context.Background(), dir); err != nil {
		t.Fatalf("with the files back, a read made %v", err)
	}
}

// A server without posix-rename still has a file replaced by a rename,
// and loses nothing when the rename can't be done; a new file is refused
// where one is.
func TestRenameReplacesWithoutPosixRename(t *testing.T) {
	here, there := net.Pipe()
	handlers := sftp.InMemHandler()
	handlers.FileCmd = noReplace{handlers.FileCmd, handlers.FileList, "/locked"}
	server := sftp.NewRequestServer(there, handlers)
	go func() { _ = server.Serve() }()
	t.Cleanup(func() { _ = server.Close() })
	client, err := sftp.NewClientPipe(here, here)
	if err != nil {
		t.Fatal(err)
	}
	m := newFMFS(serverFS+"s1", vfs.NewSFTP("mem", nil, client, client.Close))
	put := func(path, body string) {
		t.Helper()
		w, err := m.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.WriteString(w, body)
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
	}
	put("/a", "new")
	put("/b", "old")
	if _, err := m.Create("/b"); !errors.Is(err, fs.ErrExist) {
		t.Fatalf("a second create made %v", err)
	}
	if err := m.Rename("/a", "/b"); err != nil {
		t.Fatal(err)
	}
	r, err := m.Open("/b")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(r)
	_ = r.Close()
	entries, _ := m.ReadDir(context.Background(), "/")
	if string(b) != "new" || len(entries) != 1 {
		t.Fatalf("/b reads %q, and / holds %d", b, len(entries))
	}
	if err := m.Rename("/missing", "/b"); err == nil {
		t.Fatal("renaming nothing worked")
	}
	put("/locked", "locked")
	if err := m.Rename("/locked", "/b"); err == nil {
		t.Fatal("renaming a file the server won't move worked")
	}
	r, err = m.Open("/b")
	if err != nil {
		t.Fatal(err)
	}
	b, _ = io.ReadAll(r)
	_ = r.Close()
	entries, _ = m.ReadDir(context.Background(), "/")
	if string(b) != "new" || len(entries) != 2 {
		t.Fatalf("after a failed rename, /b reads %q, and / holds %d", b, len(entries))
	}
}

// noReplace is a server's renames as one without posix-rename does
// them: a name that is taken is refused. The file at locked won't move.
type noReplace struct {
	sftp.FileCmder
	list   sftp.FileLister
	locked string
}

func (n noReplace) Filecmd(r *sftp.Request) error {
	if r.Method == "Rename" || r.Method == "PosixRename" {
		if r.Filepath == n.locked {
			return errors.New("locked")
		}
		l, err := n.list.Filelist(&sftp.Request{Method: "Stat", Filepath: r.Target})
		if err == nil {
			if got, _ := l.ListAt(make([]os.FileInfo, 1), 0); got > 0 {
				return errors.New("taken")
			}
		}
	}
	return n.FileCmder.Filecmd(r)
}

// A Windows machine's drives are volumes of their own, so a drag between
// them copies.
func TestAWindowsServersDrivesAreVolumes(t *testing.T) {
	m := newFMFS(serverFS+"s1", nil)
	for path, want := range map[string]string{"/C:/Users": "/C:", "/c:": "/C:", "/d:/x": "/D:", "/home/me": "/", "/C:x": "/"} {
		if got, _ := m.Volume(path); got != want {
			t.Errorf("%s is on %q, not %q", path, got, want)
		}
	}
}

// A server's files open in a file manager window, and the server's place
// shows where its home is. Once the connection ends, the place is
// elsewhere to the window, so a click on it connects again.
func TestAServersFilesOpenInAWindow(t *testing.T) {
	a, _ := agentApp(t)
	a.settings = mustSettings(t)
	files := &fakeFiles{}
	a.files = files
	a.st.Saved = []remote.Host{{ID: "s1", Name: "web", Address: "web.example"}}
	a.st.Connected = []machines.ID{"s1"}
	a.machines.At("s1").Files = sftpHere(t)

	a.handle(OpenFileManager{Machine: "s1"})
	if len(files.opened) != 1 {
		t.Fatalf("opened %d windows", len(files.opened))
	}
	fm, ok := files.opened[0].FS.(*fmFS)
	if !ok || fm.ID() != serverFS+"s1" {
		t.Fatalf("the window opened on %#v", files.opened[0].FS)
	}
	home, err := a.fsFor("s1").Home()
	if err != nil {
		t.Fatal(err)
	}
	// The home is read by itself, and the places told.
	select {
	case f := <-a.events:
		f()
	case <-time.After(5 * time.Second):
		t.Fatal("the server's home was never read")
	}
	place := func() filemanager.Place {
		places, _ := files.opened[0].Places()
		for _, p := range places {
			if p.Group == "Servers" {
				return p
			}
		}
		t.Fatal("no server among the places")
		return filemanager.Place{}
	}
	if p := place(); p.FS != serverFS+"s1" || p.Path != home {
		t.Fatalf("the server's place is %+v", p)
	}
	a.fmGone("s1")
	a.notePlaces()
	if p := place(); p.FS != serverFS+"s1"+gone {
		t.Fatalf("with its files gone, the server's place is %+v", p)
	}
}

// Items a file manager window sends between machines go as one of
// kakel's copy jobs: copied, or moved, into the folder asked for.
func TestATransferBetweenMachinesRunsAsAJob(t *testing.T) {
	a, _ := agentApp(t)
	a.st.Saved = []remote.Host{{ID: "s1", Name: "web", Address: "web.example"}}
	a.st.Connected = []machines.ID{"s1"}
	a.machines.At("s1").Files = sftpHere(t)
	here, there := t.TempDir(), t.TempDir()
	for _, name := range []string{"a.txt", "b.txt"} {
		if err := os.WriteFile(filepath.Join(here, name), []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	into := onServer(there)
	// Through the door a window uses: on a goroutine of its own.
	go a.transferFiles(nil, filemanager.Transfer{FromFS: "", Paths: []string{filepath.Join(here, "a.txt")}, ToFS: serverFS + "s1", Into: into})
	(<-a.events)()
	a.transfer(nil, filemanager.Transfer{FromFS: "", Paths: []string{filepath.Join(here, "b.txt")}, ToFS: serverFS + "s1", Into: into, Move: true})
	if len(a.running) != 2 || a.running[0].from != machines.Local || a.running[0].to != "s1" {
		t.Fatalf("the jobs are %+v", a.running)
	}
	waitFor(t, a, "both copied", func() bool {
		_, errA := os.Stat(filepath.Join(there, "a.txt"))
		_, errB := os.Stat(filepath.Join(there, "b.txt"))
		_, gone := os.Stat(filepath.Join(here, "b.txt"))
		return errA == nil && errB == nil && os.IsNotExist(gone)
	})
	if _, err := os.Stat(filepath.Join(here, "a.txt")); err != nil {
		t.Fatalf("the copy took the original away: %v", err)
	}

	// Back from the server, from two folders at once: a job for each.
	sub := filepath.Join(there, "sub")
	if err := os.Mkdir(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "c.txt"), []byte("c"), 0o600); err != nil {
		t.Fatal(err)
	}
	back := t.TempDir()
	a.transfer(nil, filemanager.Transfer{FromFS: serverFS + "s1", Paths: []string{into + "/a.txt", onServer(sub) + "/c.txt"}, ToFS: "", Into: back})
	if len(a.running) != 4 || a.running[2].from != "s1" || a.running[3].to != machines.Local {
		t.Fatalf("the jobs back are %+v", a.running[2:])
	}
	waitFor(t, a, "both copied back", func() bool {
		_, errA := os.Stat(filepath.Join(back, "a.txt"))
		_, errC := os.Stat(filepath.Join(back, "c.txt"))
		return errA == nil && errC == nil
	})
}

// A transfer to a machine kakel can't reach starts nothing, and says so.
func TestATransferToNowhereSaysSo(t *testing.T) {
	a, _ := agentApp(t)
	here := t.TempDir()
	a.transfer(nil, filemanager.Transfer{FromFS: "", Paths: []string{filepath.Join(here, "a.txt")}, ToFS: serverFS + "nowhere", Into: "/tmp"})
	if len(a.running) != 0 {
		t.Fatalf("the jobs are %+v", a.running)
	}
	if !slices.ContainsFunc(a.st.Notices, func(n Notice) bool { return n.Title == "Couldn't copy the files" }) {
		t.Fatalf("it said %+v", a.st.Notices)
	}
}
