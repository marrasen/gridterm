package remote

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marrasen/gridterm/internal/sshtest"
)

// homeAt makes the usual key files look for their keys in a directory
// the test owns.
func homeAt(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	// Windows reads one of these and everything else reads the other.
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	if err := os.MkdirAll(filepath.Join(home, ".ssh"), 0o700); err != nil {
		t.Fatalf("make .ssh: %v", err)
	}
	return home
}

// A usual key file that is there and cannot be read fails the
// connection rather than being skipped.
//
// "Not there" is a reason to try another method. "I could not tell" is
// not: a connection that quietly offers fewer keys ends with the server
// saying no supported methods remain, and nothing anywhere saying why.
func TestAUsualKeyThatCannotBeReadIsReported(t *testing.T) {
	home := homeAt(t)
	// A directory where the key file goes: there, and not readable as a
	// file on any machine this runs on.
	at := filepath.Join(home, ".ssh", "id_ed25519")
	if err := os.Mkdir(at, 0o700); err != nil {
		t.Fatalf("make the key path: %v", err)
	}

	_, _, err := identities(Config{})
	if err == nil {
		t.Fatal("a key file that could not be read was skipped without a word")
	}
	if !strings.Contains(err.Error(), "id_ed25519") {
		t.Errorf("it said %q, want the key file named", err)
	}
}

// A usual key file that is simply not there is skipped: most machines
// have one of the three names and not the others.
func TestAUsualKeyThatIsNotThereIsSkipped(t *testing.T) {
	homeAt(t)

	plain, locked, err := identities(Config{})
	if err != nil {
		t.Fatalf("read the usual keys: %v", err)
	}
	if len(plain) != 0 || len(locked) != 0 {
		t.Fatalf("it found %d keys and %d locked ones in an empty .ssh", len(plain), len(locked))
	}
}

// A usual key file that is there and is not a key is skipped too. A
// stale id_rsa should not stop a connection the agent holds the key for.
func TestAUsualKeyThatIsNotAKeyIsSkipped(t *testing.T) {
	home := homeAt(t)
	at := filepath.Join(home, ".ssh", "id_rsa")
	if err := os.WriteFile(at, []byte("this is not a key\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	if _, _, err := identities(Config{}); err != nil {
		t.Fatalf("read the usual keys: %v", err)
	}
}

// A usual key file that needs a passphrase reaches the passphrase
// prompt rather than being skipped.
//
// This is the case the strictness above must not break: an encrypted
// ~/.ssh/id_ed25519 cannot be parsed without asking, and reading that as
// a file that could not be read would refuse the connection instead of
// asking for the passphrase.
func TestAUsualKeyThatIsLockedIsOfferedForItsPassphrase(t *testing.T) {
	home := homeAt(t)
	locked := sshtest.WriteEncryptedKey(t, "open sesame")
	b, err := os.ReadFile(locked)
	if err != nil {
		t.Fatalf("read the locked key: %v", err)
	}
	at := filepath.Join(home, ".ssh", "id_ed25519")
	if err := os.WriteFile(at, b, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	plain, wants, err := identities(Config{})
	if err != nil {
		t.Fatalf("read the usual keys: %v", err)
	}
	if len(plain) != 0 {
		t.Errorf("%d keys came back unlocked", len(plain))
	}
	if len(wants) != 1 || wants[0] != at {
		t.Fatalf("the keys wanting a passphrase are %v, want %q", wants, at)
	}
}
