package secrets

import (
	"crypto/rand"
	"errors"
	"fmt"
)

// PasswordLength is how long a password the window offers is, and
// PassphraseLength how long the one it puts on a private key is.
//
// Both are lengths rather than counts of bits, because the number the
// user sees is the number of characters. Twenty from the alphabet below
// is a shade under 117 bits, which is past anything that will be
// guessed; the passphrase is longer because nobody ever types it.
const (
	PasswordLength   = 20
	PassphraseLength = 32
)

// alphabet is what a generated password is drawn from.
//
// Letters and digits only. A symbol buys about as much as one more
// character does, and it is the thing a server's own rules refuse: a
// password that has to be typed into a form that will not take a
// slash is a password the user edits by hand.
//
// The pairs that are read wrong off a screen are left out -- l and I
// and 1, O and 0 -- because a password kept in a vault is still read
// aloud down a telephone now and again.
const alphabet = "abcdefghijkmnopqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789"

// NewPassword returns n characters chosen at random.
func NewPassword(n int) (string, error) {
	if n <= 0 {
		return "", errors.New("secrets: a password needs a length")
	}
	// Bytes at or above this are thrown away rather than folded back
	// round. len(alphabet) does not divide 256, so a remainder would
	// make the first few letters of the alphabet likelier than the last
	// few, and a password is only as good as the one guess nobody can
	// narrow down.
	limit := 256 - 256%len(alphabet)
	out := make([]byte, 0, n)
	buf := make([]byte, n)
	for len(out) < n {
		if _, err := rand.Read(buf); err != nil {
			return "", fmt.Errorf("secrets: create a password: %w", err)
		}
		for _, b := range buf {
			if int(b) >= limit {
				continue
			}
			out = append(out, alphabet[int(b)%len(alphabet)])
			if len(out) == n {
				break
			}
		}
	}
	return string(out), nil
}
