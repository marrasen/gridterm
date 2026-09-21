package remote

import (
	"context"

	"golang.org/x/crypto/ssh"
)

// Ask is how a connection reaches the user for a secret or a decision.
//
// Every method but Notice blocks until the user answers or ctx is
// cancelled. They are called from the goroutine that is connecting,
// never from the one drawing, so an implementation has to hand the
// question to the window and wait for the reply.
//
// Returning an error stops the connection, and the error is what the
// caller is told. That is what cancelling a dialog means: the user said
// no, so nothing else is tried and no second question is asked.
type Ask interface {
	// Passphrase unlocks a private key file. A passphrase that does not
	// open the key is asked for again, so a typo is a second try rather
	// than a connection that quietly signs in some other way -- or does
	// not sign in at all. Cancelling is what stops the asking.
	Passphrase(ctx context.Context, key LockedKey) (string, error)

	// Password is the account password, asked only after key
	// authentication has been tried.
	Password(ctx context.Context, user, host string) (string, error)

	// Question is keyboard-interactive authentication: whatever the
	// server decided to ask, which is usually a one-time code.
	Question(ctx context.Context, q Question) ([]string, error)

	// TrustHostKey asks whether to connect to a host that is not in
	// known_hosts. Answering yes records the key.
	TrustHostKey(ctx context.Context, key HostKey) (bool, error)

	// Notice is something a server said rather than asked: a link to
	// open, most often, which is how a server that signs people in
	// through a browser tells them where to go. The handshake goes on
	// waiting while they do it, and finishes on its own once they have.
	//
	// It must return as soon as the message is on its way to the user.
	// Nothing is waiting for an answer, and a client that sat on this
	// would hold up the very handshake the user is being asked to
	// unblock.
	//
	// The message stops being worth showing when ctx is done, which is
	// when the connection has been made, has failed, or was given up
	// on.
	Notice(ctx context.Context, n Notice)
}

// LockedKey is a private key file that will not open without a
// passphrase.
type LockedKey struct {
	// Path is the key file being unlocked.
	Path string

	// Wrong counts the passphrases already refused for this file, and is
	// zero the first time it is asked. Anything above zero means the
	// last answer did not open the key, which the dialog has to say: a
	// question asked twice with no word of why reads as a question that
	// was not heard.
	Wrong int
}

// Notice is something a server told the user during authentication.
//
// User and Host are ours. Everything else is the server's own wording
// and is not to be trusted, for the same reason a Question's is not: a
// message the user cannot tell from the window's own could send them
// somewhere of the server's choosing.
type Notice struct {
	// User and Host name the connection the message came from.
	User, Host string

	// Name and Instruction are the server's own wording, and may be
	// empty. Text is the message of a banner, which has neither.
	Name, Instruction, Text string
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
