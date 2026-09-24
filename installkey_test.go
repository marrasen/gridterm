package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/marrasen/gridterm/vfs"
)

// farDisk is a server's filesystem as SFTP shows it, kept in a directory
// here: every path is a slash path from the server's root, and home is
// wherever the server says it is.
type farDisk struct {
	vfs.FS
	root, home string

	// first runs once, just before the first file is made: another
	// writer getting there between the look and the make.
	first func()
}

func newFarDisk(t *testing.T, home string) *farDisk {
	t.Helper()
	d := &farDisk{FS: vfs.NewLocal(), root: t.TempDir(), home: home}
	if err := os.MkdirAll(d.at(home), 0o755); err != nil {
		t.Fatal(err)
	}
	return d
}

func (d *farDisk) at(p string) string { return filepath.Join(d.root, filepath.FromSlash(p)) }

func (d *farDisk) Home() (string, error)                { return d.home, nil }
func (d *farDisk) Stat(p string) (vfs.Entry, error)     { return d.FS.Stat(d.at(p)) }
func (d *farDisk) Open(p string) (io.ReadCloser, error) { return d.FS.Open(d.at(p)) }
func (d *farDisk) Mkdir(p string, m fs.FileMode) error  { return d.FS.Mkdir(d.at(p), m) }
func (d *farDisk) Remove(p string) error                { return d.FS.Remove(d.at(p)) }
func (d *farDisk) Rename(from, to string) error         { return d.FS.Rename(d.at(from), d.at(to)) }
func (d *farDisk) Chmod(p string, m fs.FileMode) error  { return d.FS.Chmod(d.at(p), m) }
func (d *farDisk) Create(p string, m fs.FileMode) (io.WriteCloser, error) {
	return d.FS.Create(d.at(p), m)
}
func (d *farDisk) Append(p string) (io.WriteCloser, error) {
	return d.FS.(vfs.Appender).Append(d.at(p))
}
func (d *farDisk) CreateNew(p string, m fs.FileMode) (io.WriteCloser, error) {
	if d.first != nil {
		d.first()
		d.first = nil
	}
	return d.FS.(vfs.Appender).CreateNew(d.at(p), m)
}

func (d *farDisk) read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(d.at(p))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// aKeyLine is a new public key as the one line authorized_keys takes.
func aKeyLine(t *testing.T, comment string) string {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	k, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(k))) + " " + comment
}

// A server that has never had a key gets a .ssh and an authorized_keys
// only its owner can read, which is what sshd insists on.
func TestInstallKeyOnAFreshServer(t *testing.T) {
	d := newFarDisk(t, "/home/me")
	line := aKeyLine(t, "me@here")
	got, err := installKey(d, line)
	if err != nil || !got.added || !got.has {
		t.Fatalf("installKey = %+v, %v; want added", got, err)
	}
	if got := d.read(t, "/home/me/.ssh/authorized_keys"); got != line+"\n" {
		t.Errorf("authorized_keys = %q", got)
	}
	for p, want := range map[string]fs.FileMode{"/home/me/.ssh": 0o700, "/home/me/.ssh/authorized_keys": 0o600} {
		info, err := os.Stat(d.at(p))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != want {
			t.Errorf("%s has mode %v, want %v", p, info.Mode().Perm(), want)
		}
	}
}

// The keys already there stay, a last line with no newline is not run
// into the new one, and the file keeps its own permissions.
func TestInstallKeyKeepsWhatIsThere(t *testing.T) {
	d := newFarDisk(t, "/home/me")
	old := aKeyLine(t, "old")
	if err := os.MkdirAll(d.at("/home/me/.ssh"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(d.at("/home/me/.ssh/authorized_keys"), []byte("# mine\n"+old), 0o640); err != nil {
		t.Fatal(err)
	}
	line := aKeyLine(t, "new")
	if _, err := installKey(d, line); err != nil {
		t.Fatal(err)
	}
	if got, want := d.read(t, "/home/me/.ssh/authorized_keys"), "# mine\n"+old+"\n"+line+"\n"; got != want {
		t.Errorf("authorized_keys = %q, want %q", got, want)
	}
	info, err := os.Stat(d.at("/home/me/.ssh/authorized_keys"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o640 {
		t.Errorf("mode = %v, want the 0640 it had", info.Mode().Perm())
	}
}

// A key the server has already, under another comment and behind
// options, is the same key: the file is left alone.
func TestInstallKeyTwiceAddsItOnce(t *testing.T) {
	d := newFarDisk(t, "/home/me")
	line := aKeyLine(t, "first")
	fields := strings.Fields(line)
	if err := os.MkdirAll(d.at("/home/me/.ssh"), 0o700); err != nil {
		t.Fatal(err)
	}
	was := `no-pty ` + fields[0] + " " + fields[1] + " elsewhere\n"
	if err := os.WriteFile(d.at("/home/me/.ssh/authorized_keys"), []byte(was), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := installKey(d, line)
	if err != nil || got.added || !got.has {
		t.Fatalf("installKey = %+v, %v; want it there and nothing added", got, err)
	}
	if got := d.read(t, "/home/me/.ssh/authorized_keys"); got != was {
		t.Errorf("authorized_keys = %q, want it untouched", got)
	}
}

// An administrator on Windows is let in by the shared file alone, so
// the key goes there as well as into their home.
func TestInstallKeyOnWindowsAsAdministrator(t *testing.T) {
	d := newFarDisk(t, "/C:/Users/me")
	if err := os.MkdirAll(d.at("/C:/ProgramData/ssh"), 0o755); err != nil {
		t.Fatal(err)
	}
	line := aKeyLine(t, "me@here")
	if _, err := installKey(d, line); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{
		"/C:/ProgramData/ssh/administrators_authorized_keys",
		"/C:/Users/me/.ssh/authorized_keys",
	} {
		if got := d.read(t, p); got != line+"\n" {
			t.Errorf("%s = %q", p, got)
		}
	}
}

// Anybody else cannot write the shared file, and is not refused for
// it: their own is the one sshd reads for them.
func TestInstallKeyOnWindowsAsAnybodyElse(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root writes wherever it likes")
	}
	d := newFarDisk(t, "/C:/Users/me")
	shared := d.at("/C:/ProgramData/ssh")
	if err := os.MkdirAll(shared, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(shared, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(shared, 0o755) })
	line := aKeyLine(t, "me@here")
	got, err := installKey(d, line)
	if err != nil || !got.added || !got.has {
		t.Fatalf("installKey = %+v, %v; want added", got, err)
	}
	if got := d.read(t, "/C:/Users/me/.ssh/authorized_keys"); got != line+"\n" {
		t.Errorf("authorized_keys = %q", got)
	}
}

// A .ssh linked from somewhere else is followed, not refused.
func TestInstallKeyFollowsALinkedSSHDirectory(t *testing.T) {
	d := newFarDisk(t, "/home/me")
	if err := os.MkdirAll(d.at("/dotfiles/ssh"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(d.at("/dotfiles/ssh"), d.at("/home/me/.ssh")); err != nil {
		t.Fatal(err)
	}
	line := aKeyLine(t, "me@here")
	if _, err := installKey(d, line); err != nil {
		t.Fatal(err)
	}
	if got := d.read(t, "/dotfiles/ssh/authorized_keys"); got != line+"\n" {
		t.Errorf("authorized_keys = %q", got)
	}
}

// ProgramData is on the system drive, which need not be the drive a
// profile is on.
func TestInstallKeyFindsProgramDataOffTheHomeDrive(t *testing.T) {
	d := newFarDisk(t, "/D:/Users/me")
	if err := os.MkdirAll(d.at("/C:/ProgramData/ssh"), 0o755); err != nil {
		t.Fatal(err)
	}
	line := aKeyLine(t, "me@here")
	if _, err := installKey(d, line); err != nil {
		t.Fatal(err)
	}
	if got := d.read(t, "/C:/ProgramData/ssh/administrators_authorized_keys"); got != line+"\n" {
		t.Errorf("administrators_authorized_keys = %q", got)
	}
}

// A shared file that can be read and not written is reported, and the
// key still goes into the home one: that account may be an
// administrator, or anybody on a server whose shared file everybody can
// read.
func TestInstallKeyOnWindowsSaysTheSharedFileCouldNotBeWritten(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root writes wherever it likes")
	}
	d := newFarDisk(t, "/C:/Users/me")
	shared := d.at("/C:/ProgramData/ssh")
	if err := os.MkdirAll(shared, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(shared, "administrators_authorized_keys"),
		[]byte(aKeyLine(t, "other")+"\n"), 0o400); err != nil {
		t.Fatal(err)
	}
	line := aKeyLine(t, "me@here")
	got, err := installKey(d, line)
	if err == nil {
		t.Error("a shared file that could be read and not written was taken for somebody else's")
	}
	if !got.has {
		t.Error("the key was not reported installed in the home file")
	}
	if got := d.read(t, "/C:/Users/me/.ssh/authorized_keys"); got != line+"\n" {
		t.Errorf("authorized_keys = %q", got)
	}
}

// A key the shared file already has is on the server, whatever became
// of the home one: the failure is reported beside it, not as the key
// missing.
func TestInstallKeyAlreadyInTheSharedFileIsThere(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root writes wherever it likes")
	}
	d := newFarDisk(t, "/C:/Users/me")
	line := aKeyLine(t, "me@here")
	if err := os.MkdirAll(d.at("/C:/ProgramData/ssh"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(d.at("/C:/ProgramData/ssh/administrators_authorized_keys"),
		[]byte(line+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	home := d.at("/C:/Users/me")
	if err := os.Chmod(home, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(home, 0o755) })
	got, err := installKey(d, line)
	if err == nil {
		t.Error("a home that could not be written was not reported")
	}
	if !got.has || got.added {
		t.Errorf("installKey = %+v; want it there and nothing added", got)
	}
}

// Installs on one machine wait for each other, and installs on another
// machine do not wait for them.
func TestInstallsWaitOnlyForTheirOwnMachine(t *testing.T) {
	one, other := new(int), new(int)
	unlock := lockInstalls(one)
	started := make(chan struct{})
	go func() {
		lockInstalls(one)()
		close(started)
	}()
	lockInstalls(other)()
	select {
	case <-started:
		t.Fatal("a second install on one machine did not wait for the first")
	case <-time.After(50 * time.Millisecond):
	}
	unlock()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("the second install never started")
	}
}

// Another install that makes the file first keeps its key: this one adds
// to the file rather than making it over.
func TestInstallKeyAddsToAFileMadeMeanwhile(t *testing.T) {
	d := newFarDisk(t, "/home/me")
	if err := os.MkdirAll(d.at("/home/me/.ssh"), 0o700); err != nil {
		t.Fatal(err)
	}
	theirs := aKeyLine(t, "theirs")
	d.first = func() {
		if err := os.WriteFile(d.at("/home/me/.ssh/authorized_keys"), []byte(theirs+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	line := aKeyLine(t, "mine")
	if _, err := installKey(d, line); err != nil {
		t.Fatal(err)
	}
	if got, want := d.read(t, "/home/me/.ssh/authorized_keys"), theirs+"\n"+line+"\n"; got != want {
		t.Errorf("authorized_keys = %q, want %q", got, want)
	}
}

// A key file named by its private half is read from the .pub beside it.
func TestPublicKeyLineReadsThePubBesideIt(t *testing.T) {
	at := filepath.Join(t.TempDir(), "id_test")
	line := aKeyLine(t, "me@here")
	if err := os.WriteFile(at+".pub", []byte(line+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, named := range []string{at, at + ".pub"} {
		if got, err := publicKeyLine(named); err != nil || got != line {
			t.Errorf("publicKeyLine(%s) = %q, %v", named, got, err)
		}
	}
	if _, err := publicKeyLine(filepath.Join(t.TempDir(), "none")); err != errNoPublicKey {
		t.Errorf("a key with no .pub beside it: %v", err)
	}
}

// A comment line above the key is left behind: the line is the key.
func TestPublicKeyLineSkipsACommentAboveTheKey(t *testing.T) {
	at := filepath.Join(t.TempDir(), "id_test.pub")
	line := aKeyLine(t, "me@here")
	if err := os.WriteFile(at, []byte("# my laptop\n"+line+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := publicKeyLine(at); err != nil || got != line {
		t.Errorf("publicKeyLine = %q, %v; want %q", got, err, line)
	}
}

// Over a real connection: the key lands beside the ones in the home the
// server gives, and asking again leaves it there once.
func TestInstallKeyOverTheConnection(t *testing.T) {
	a, s, host := aConnectedWindow(t, 100, 30)
	home := t.TempDir()
	s.SFTPHome(home)
	old := aKeyLine(t, "old")
	if err := os.Mkdir(filepath.Join(home, ".ssh"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".ssh", "authorized_keys"), []byte(old+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	line := aKeyLine(t, "me@here")

	for _, want := range []string{keyInstalledOn(host), keyAlreadyOn(host)} {
		a.say("")
		a.installKeyOn(host, line)
		waitFor(t, a, "the key to be installed", func() bool { return a.saying() != "" })
		if got := a.saying(); got != want {
			t.Errorf("the status line says %q, want %q", got, want)
		}
	}
	got, err := os.ReadFile(filepath.Join(home, ".ssh", "authorized_keys"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != old+"\n"+line+"\n" {
		t.Errorf("authorized_keys = %q, want the old key and the new one once", got)
	}
}
