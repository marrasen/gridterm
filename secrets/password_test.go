package secrets

import (
	"strings"
	"testing"
)

// A generated password is as long as it was asked to be, and made of
// nothing the alphabet leaves out.
func TestAGeneratedPasswordIsTheLengthAsked(t *testing.T) {
	for _, n := range []int{1, 8, PasswordLength, PassphraseLength, 300} {
		got, err := NewPassword(n)
		if err != nil {
			t.Fatalf("NewPassword(%d): %v", n, err)
		}
		if len(got) != n {
			t.Errorf("NewPassword(%d) is %d characters long", n, len(got))
		}
		for _, r := range got {
			if !strings.ContainsRune(alphabet, r) {
				t.Errorf("NewPassword(%d) = %q, which is not in the alphabet", n, r)
			}
		}
	}
}

// The alphabet leaves out the characters that are read wrong off a
// screen, because a password is still read aloud now and again.
func TestTheAlphabetLeavesOutTheLookalikes(t *testing.T) {
	for _, r := range "l1IO0" {
		if strings.ContainsRune(alphabet, r) {
			t.Errorf("the alphabet has %q in it", r)
		}
	}
}

// Two passwords are not the same one, which is the whole point.
func TestTwoGeneratedPasswordsDiffer(t *testing.T) {
	seen := map[string]bool{}
	for range 100 {
		got, err := NewPassword(PasswordLength)
		if err != nil {
			t.Fatalf("NewPassword: %v", err)
		}
		if seen[got] {
			t.Fatalf("%q came back twice out of a hundred", got)
		}
		seen[got] = true
	}
}

// Every character is reachable. Rejection sampling that threw away the
// wrong bytes would leave a hole in the alphabet and say nothing.
func TestEveryCharacterOfTheAlphabetTurnsUp(t *testing.T) {
	// Enough draws that a character missing is a bug rather than luck:
	// each of the 57 has about a one in 57 chance a draw, so 20000 draws
	// misses one with a probability under 10^-150.
	got, err := NewPassword(20000)
	if err != nil {
		t.Fatalf("NewPassword: %v", err)
	}
	for _, r := range alphabet {
		if !strings.ContainsRune(got, r) {
			t.Errorf("%q never came out of the generator", r)
		}
	}
}

// A length of nothing is a mistake, not an empty password.
func TestAPasswordOfNoLengthIsRefused(t *testing.T) {
	if _, err := NewPassword(0); err == nil {
		t.Error("NewPassword(0) came back with a password")
	}
}
