package remote

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

// A new key is written as a pair that ssh can read, and the public half
// is the line an authorized_keys file takes.
func TestMakeKeyWritesAPairSshCanRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "id_ed25519")

	got, err := MakeKey(path, "marcus@laptop", "")
	if err != nil {
		t.Fatalf("make a key: %v", err)
	}

	if got.Path != path || got.Pub != path+".pub" {
		t.Errorf("it wrote %q and %q", got.Path, got.Pub)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the key: %v", err)
	}
	signer, err := ssh.ParsePrivateKey(raw)
	if err != nil {
		t.Fatalf("ssh could not read the key: %v", err)
	}
	if signer.PublicKey().Type() != ssh.KeyAlgoED25519 {
		t.Errorf("the key is %s", signer.PublicKey().Type())
	}
	// The public half beside it is the same key, and the line the user
	// pastes.
	line, err := os.ReadFile(got.Pub)
	if err != nil {
		t.Fatalf("read the public half: %v", err)
	}
	want := string(ssh.MarshalAuthorizedKey(signer.PublicKey()))
	if !strings.HasPrefix(string(line), strings.TrimSpace(want)) {
		t.Errorf("the public half is %q, want the key that was written", line)
	}
	if !strings.Contains(string(line), "marcus@laptop") {
		t.Errorf("the public half is %q, want the comment on it", line)
	}
	if got.Line+"\n" != string(line) {
		t.Errorf("it said the line is %q and wrote %q", got.Line, line)
	}
}

// A passphrase locks the key, so it cannot be read without one.
func TestAPassphraseLocksTheKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "id_ed25519")

	if _, err := MakeKey(path, "", "let me in"); err != nil {
		t.Fatalf("make a key: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the key: %v", err)
	}
	if _, err := ssh.ParsePrivateKey(raw); err == nil {
		t.Fatal("the key opened with no passphrase")
	}
	if _, err := ssh.ParsePrivateKeyWithPassphrase(raw, []byte("let me in")); err != nil {
		t.Errorf("the key would not open with its passphrase: %v", err)
	}
}

// A key file that is already there is not written over: the private
// half cannot be got back.
func TestAKeyThatIsThereIsNotWrittenOver(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "id_ed25519")
	if err := os.WriteFile(path, []byte("the key somebody is using"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	_, err := MakeKey(path, "", "")

	if err == nil {
		t.Fatal("it wrote over a key that was there")
	}
	if !strings.Contains(err.Error(), "already there") {
		t.Errorf("it said %q", err)
	}
	raw, _ := os.ReadFile(path)
	if string(raw) != "the key somebody is using" {
		t.Errorf("the key now says %q", raw)
	}
}

// And nor is a public half that is there, with the private half taken
// away again rather than left behind with nothing naming it.
func TestAPublicHalfThatIsThereLeavesNothingBehind(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "id_ed25519")
	if err := os.WriteFile(path+".pub", []byte("somebody else's"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	_, err := MakeKey(path, "", "")

	if err == nil {
		t.Fatal("it wrote over a public half that was there")
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("it left a private key at %s with nothing naming it", path)
	}
}

// The directory is made when it is not there, the way ssh-keygen makes
// ~/.ssh.
func TestMakeKeyMakesTheDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", ".ssh", "id_ed25519")

	if _, err := MakeKey(path, "", ""); err != nil {
		t.Fatalf("make a key: %v", err)
	}

	if _, err := os.Stat(path); err != nil {
		t.Errorf("the key is not there: %v", err)
	}
}

// A key with nowhere to go, or somewhere relative, is refused rather
// than written wherever gridterm happened to be started. On Windows
// where a key sits is the whole of what keeps it private.
func TestAKeyWithoutAFullPathIsRefused(t *testing.T) {
	for _, path := range []string{"", "   ", "id_ed25519", filepath.Join("keys", "id"), "~/.ssh/id"} {
		if _, err := MakeKey(path, "", ""); err == nil {
			t.Errorf("a key at %q was written", path)
		}
	}
}

// The private half is readable by its owner and nobody else where a
// mode says who may read a file.
func TestThePrivateHalfIsReadableByItsOwnerOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		// Windows has no mode to read. What keeps a key private there is
		// the directory it is in, which is why MakeKey insists on a full
		// path -- and TestAKeyWithoutAFullPathIsRefused is what pins
		// that.
		t.Skip("a file mode says nothing on this platform")
	}
	path := filepath.Join(t.TempDir(), "id_ed25519")

	if _, err := MakeKey(path, "", ""); err != nil {
		t.Fatalf("make a key: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if got := info.Mode().Perm(); got != KeyPerm {
		t.Errorf("the key is %o, want %o", got, KeyPerm)
	}
}

// A key is written whole or not at all: nothing is left at the name ssh
// looks for while it is being written.
func TestAKeyLeavesNothingHalfWritten(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "id_ed25519")
	// The public half is in the way, so the private half is written and
	// then taken away again.
	if err := os.WriteFile(path+".pub", []byte("somebody else's"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	if _, err := MakeKey(path, "", ""); err == nil {
		t.Fatal("it wrote over a public half that was there")
	}

	// And nothing of its own is left behind, temporary files included.
	got, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read the directory: %v", err)
	}
	if len(got) != 1 || got[0].Name() != "id_ed25519.pub" {
		var names []string
		for _, e := range got {
			names = append(names, e.Name())
		}
		t.Errorf("the directory holds %v, want only the file that was there", names)
	}
}

// The default is a name of gridterm's own, so the first thing the dialog
// offers is not the one path most likely to be taken already.
func TestTheDefaultPathIsGridtermsOwn(t *testing.T) {
	at, err := DefaultKeyPath()
	if err != nil {
		t.Skipf("no home directory here: %v", err)
	}
	if !filepath.IsAbs(at) {
		t.Errorf("the default is %q, which is not a full path", at)
	}
	if filepath.Base(at) == "id_ed25519" {
		t.Errorf("the default is %q, the name ssh-keygen writes", at)
	}
}
