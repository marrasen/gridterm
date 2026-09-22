package remote

import (
	"fmt"

	"golang.org/x/crypto/ssh"
)

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
	defer func() { _ = conn.Close() }()
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
