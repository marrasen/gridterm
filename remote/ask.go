package remote

import (
	"context"

	"golang.org/x/crypto/ssh"
)

// Ask is how a connection reaches the user for a secret or a decision.
//
// Every method blocks until the user answers or ctx is cancelled. They
// are called from the goroutine that is connecting, never from the one
// drawing, so an implementation has to hand the question to the window
// and wait for the reply.
//
// Returning an error stops the connection, and the error is what the
// caller is told. That is what cancelling a dialog means: the user said
// no, so nothing else is tried and no second question is asked.
type Ask interface {
	// Passphrase unlocks a private key file.
	Passphrase(ctx context.Context, keyfile string) (string, error)

	// Password is the account password, asked only after key
	// authentication has been tried.
	Password(ctx context.Context, user, host string) (string, error)

	// Question is keyboard-interactive authentication: whatever the
	// server decided to ask, which is usually a one-time code.
	Question(ctx context.Context, q Question) ([]string, error)

	// TrustHostKey asks whether to connect to a host that is not in
	// known_hosts. Answering yes records the key.
	TrustHostKey(ctx context.Context, key HostKey) (bool, error)
}

// Question is what a server asked for during keyboard-interactive
// authentication.
//
// User and Host are ours. Everything else is the server's own wording
// and is not to be trusted: a server that chose "Unlock a private key"
// and a plausible key path could otherwise produce a dialog the user
// cannot tell from the local one, and be handed the passphrase to their
// private key. A dialog has to say which machine is asking, in words the
// server cannot write.
type Question struct {
	// User and Host name the connection the question came from.
	User, Host string

	// Name and Instruction are the server's own wording, and may be
	// empty.
	Name, Instruction string

	// Prompts is what to ask, and Echo says which answers may be shown
	// as they are typed. The two are the same length.
	Prompts []string
	Echo    []bool
}

// HostKey is a host key offered by a server that is not in known_hosts.
type HostKey struct {
	// Addr is the host:port being connected to.
	Addr string

	// Key is what the server presented.
	Key ssh.PublicKey
}

// Type returns the key algorithm, such as ssh-ed25519.
func (h HostKey) Type() string {
	if h.Key == nil {
		return ""
	}
	return h.Key.Type()
}

// Fingerprint returns the SHA256 fingerprint, spelled the way ssh and
// ssh-keygen spell it so a user can compare the two.
func (h HostKey) Fingerprint() string {
	if h.Key == nil {
		return ""
	}
	return ssh.FingerprintSHA256(h.Key)
}
