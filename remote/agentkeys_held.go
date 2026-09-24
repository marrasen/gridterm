package remote

import (
	"fmt"
	"time"

	"golang.org/x/crypto/ssh"
)

// agentPatience is how long AgentHolds waits for the agent to say what
// it holds.
//
// Shorter than the listing a connection waits on, because this is asked
// from the goroutine that draws, in the moment between picking a key
// and being asked about it. An agent that will not answer a read from
// its own memory in this long is wedged, and the answer here is only a
// warning: going without it is far better than a window that has
// stopped.
//
// A variable so a test can shorten it. Nothing in the program writes it.
var agentPatience = 2 * time.Second

// AgentHolds reports whether the running SSH agent holds a key with
// this fingerprint, in the form ssh.FingerprintSHA256 gives.
//
// A question worth asking about the key that opens the secrets. A slot
// key is derived from a signature over a challenge kept in the clear in
// the vault file, ed25519 signs the same way every time, and an agent
// signs whatever blob it is handed without looking at it. So one
// forwarded session to a machine that has been taken over is enough to
// hand over the vault, to anybody who also has a copy of the file.
//
// An answer of false covers both "the agent does not have it" and
// "there is no agent to ask", and the error says which. It is a snapshot
// either way: a key can be added to the agent a minute later.
func AgentHolds(fingerprint string) (bool, error) {
	conn, ag, err := localAgent()
	if err != nil {
		return false, err
	}
	// Closing the socket is what unblocks an agent that has taken the
	// connection and then said nothing; nothing else will, which is why
	// the connection path holds its own listing to a clock the same way.
	// Closing twice is safe: the second answers an error nobody reads.
	late := time.AfterFunc(agentPatience, func() { _ = conn.Close() })
	defer func() {
		late.Stop()
		_ = conn.Close()
	}()
	keys, err := ag.List()
	if err != nil {
		return false, fmt.Errorf("ask the SSH agent what it holds: %w", err)
	}
	for _, key := range keys {
		if ssh.FingerprintSHA256(key) == fingerprint {
			return true, nil
		}
	}
	return false, nil
}
