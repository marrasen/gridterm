package vaultkeys

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"golang.org/x/crypto/ssh"

	"github.com/marrasen/kakel/internal/testhome"
)

// writeKey writes a key file and its public half, of the kind pub is.
func writeKey(t *testing.T, path string, pub any) {
	t.Helper()
	key, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("private"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+".pub", ssh.MarshalAuthorizedKey(key), 0o600); err != nil {
		t.Fatal(err)
	}
}

// The keys that could keep the secrets are ed25519 ones only, the ones
// kept first, each once.
func TestTheCandidatesAreEd25519KeysKeptOnesFirst(t *testing.T) {
	home := testhome.New(t)
	edPub, _, _ := ed25519.GenerateKey(rand.Reader)
	ec, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	dotSSH := filepath.Join(home, ".ssh")
	writeKey(t, filepath.Join(dotSSH, "id_ed25519"), edPub)
	writeKey(t, filepath.Join(dotSSH, "work_ed25519"), edPub)
	writeKey(t, filepath.Join(dotSSH, "id_ecdsa"), &ec.PublicKey)
	kept := filepath.Join(home, "elsewhere", "vault_ed25519")
	writeKey(t, kept, edPub)
	got := Candidates(func() []string { return []string{kept, filepath.Join(dotSSH, "work_ed25519"), "/no/such/key"} })
	want := []string{kept, filepath.Join(dotSSH, "work_ed25519"), filepath.Join(dotSSH, "id_ed25519")}
	if !slices.Equal(got, want) {
		t.Fatalf("the candidates are %q, want %q", got, want)
	}
}
