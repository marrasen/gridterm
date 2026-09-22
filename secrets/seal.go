// Package secrets keeps passwords and notes in a file that only a
// private key opens.
//
// The key is one of the SSH keys the window already unlocks to reach a
// server, so the passphrase asked for at the first connection opens the
// vault too and there is nothing else to remember. Locking the keys
// locks the vault with them.
//
// The file is sealed with a data key of its own, and that data key is
// wrapped once per key that may open it. So a second machine's key is
// added without re-encrypting anything, and a key that is replaced
// takes only its own slot with it.
package secrets

import (
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"

	"golang.org/x/crypto/chacha20poly1305"
	"golang.org/x/crypto/ssh"
)

// signedPrefix goes in front of the challenge a slot asks a key to
// sign, so a signature made for this vault cannot stand in for one made
// anywhere else, and one made anywhere else cannot open a vault.
const signedPrefix = "gridterm-secrets-v1\x00"

// slotInfo and dataInfo separate the two keys derived here.
const (
	slotInfo = "gridterm secrets slot key v1"
	dataInfo = "gridterm secrets data key v1"
)

// keyLen is the length of every key here: the data key and each slot
// key that wraps it.
const keyLen = chacha20poly1305.KeySize

// ErrWrongKey says the key offered does not open the vault.
var ErrWrongKey = errors.New("secrets: that key does not open this vault")

// ErrNotDeterministic says a key cannot hold a vault open.
//
// A slot key is derived from a signature over a fixed challenge, so the
// same key has to make the same signature every time. ed25519 does.
// ECDSA picks a random nonce per signature and would seal a vault that
// never opened again.
var ErrNotDeterministic = errors.New(
	"secrets: only an ed25519 key can open a vault, because only its signature is the same every time")

// usable reports whether a key signs the same way every time, which is
// what a vault needs.
func usable(pub ssh.PublicKey) error {
	if pub.Type() != ssh.KeyAlgoED25519 {
		return fmt.Errorf("%w (this one is %s)", ErrNotDeterministic, pub.Type())
	}
	return nil
}

// Fingerprint names a key in a slot, so the vault can say which key it
// wants without holding the key itself.
func Fingerprint(pub ssh.PublicKey) string { return ssh.FingerprintSHA256(pub) }

// slotKeyFrom derives a slot's key by having the signer sign that
// slot's challenge.
//
// The signature never leaves this function: what is kept is the key
// hashed out of it.
func slotKeyFrom(signer ssh.Signer, challenge, salt []byte) ([]byte, error) {
	if err := usable(signer.PublicKey()); err != nil {
		return nil, err
	}
	msg := append([]byte(signedPrefix), challenge...)
	// rand is unused by an ed25519 signer and is what the interface asks
	// for.
	sig, err := signer.Sign(rand.Reader, msg)
	if err != nil {
		return nil, fmt.Errorf("secrets: sign the vault challenge: %w", err)
	}
	if len(sig.Blob) == 0 {
		return nil, errors.New("secrets: the key signed the vault challenge with nothing")
	}
	// The format goes in as well, so two signature kinds over one
	// challenge cannot land on the same key.
	ikm := append([]byte(sig.Format+"\x00"), sig.Blob...)
	// The signature is the slot key in every sense that matters: the
	// salt and the info beside it are in the file in the clear, so
	// anything holding this signature can derive the key again whenever
	// it likes. Callers wipe the key they are handed, and leaving the
	// thing it was made from lying in memory made that worth nothing.
	defer wipe(ikm)
	defer wipe(sig.Blob)
	key, err := hkdf.Key(sha256.New, ikm, salt, slotInfo, keyLen)
	if err != nil {
		return nil, fmt.Errorf("secrets: derive the slot key: %w", err)
	}
	return key, nil
}

// seal encrypts with a key, returning the nonce and the ciphertext.
func seal(key, plain, extra []byte) (nonce, box []byte, err error) {
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, nil, fmt.Errorf("secrets: set up the cipher: %w", err)
	}
	nonce = make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, nil, fmt.Errorf("secrets: take a nonce: %w", err)
	}
	return nonce, aead.Seal(nil, nonce, plain, extra), nil
}

// unseal decrypts what seal wrote.
func unseal(key, nonce, box, extra []byte) ([]byte, error) {
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, fmt.Errorf("secrets: set up the cipher: %w", err)
	}
	if len(nonce) != aead.NonceSize() {
		return nil, fmt.Errorf("secrets: the nonce is %d bytes and should be %d",
			len(nonce), aead.NonceSize())
	}
	plain, err := aead.Open(nil, nonce, box, extra)
	if err != nil {
		return nil, ErrWrongKey
	}
	return plain, nil
}

// newDataKey returns the key a vault's contents are sealed with.
func newDataKey() ([]byte, error) {
	key := make([]byte, keyLen)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("secrets: take a key: %w", err)
	}
	return key, nil
}

// wipe overwrites a key in place, for one that is being dropped.
//
// Go can move a slice and leave the old bytes behind, so this reduces
// how long a key sits in memory rather than guaranteeing anything.
func wipe(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
