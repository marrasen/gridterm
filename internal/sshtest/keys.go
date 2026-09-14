package sshtest

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/ssh"
)

// WriteKey writes a new private key with no passphrase and returns its
// path.
func WriteKey(t *testing.T) string {
	t.Helper()
	return writeKey(t, "id_test", nil)
}

// WriteEncryptedKey writes a new private key protected by a passphrase
// and returns its path.
func WriteEncryptedKey(t *testing.T, passphrase string) string {
	t.Helper()
	return writeKey(t, "id_locked", []byte(passphrase))
}

// Fingerprint returns the SHA256 fingerprint of a private key file, so a
// test can tell which key a server was offered.
func Fingerprint(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	signer, err := ssh.ParsePrivateKey(b)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return ssh.FingerprintSHA256(signer.PublicKey())
}

func writeKey(t *testing.T, name string, passphrase []byte) string {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	var block *pem.Block
	if len(passphrase) == 0 {
		block, err = ssh.MarshalPrivateKey(priv, "")
	} else {
		block, err = ssh.MarshalPrivateKeyWithPassphrase(priv, "", passphrase)
	}
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}

	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}
