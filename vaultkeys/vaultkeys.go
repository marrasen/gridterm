// Package vaultkeys knows the SSH keys on this machine that open, or
// could open, kakel's secrets: which to unlock, which could keep them,
// what to warn about before trusting one, and what removing one costs.
package vaultkeys

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/marrasen/kakel/remote"
	"github.com/marrasen/kakel/secrets"
	"golang.org/x/crypto/ssh"
)

// ToUnlock is the key file to unlock to open the vault: one of
// its keys on this machine, or else the first it remembers, whose
// error names the file to bring over.
func ToUnlock(v *secrets.Vault) (string, error) {
	first := ""
	for _, s := range v.Keys() {
		if s.KeyFile == "" {
			continue
		}
		if Here(s) {
			return s.KeyFile, nil
		}
		if first == "" {
			first = s.KeyFile
		}
	}
	if first != "" {
		return first, nil
	}
	return "", errors.New("the secrets name no key file that opens them; connect to a server with their key first")
}

// Here reports whether a slot's key file is here.
func Here(s secrets.KeySlot) bool {
	if s.KeyFile == "" {
		return false
	}
	_, err := os.Stat(s.KeyFile)
	return err == nil
}

// Candidates are the key files a vault could be kept on: those kept,
// from kept, which may be nil, first, then the usual one and those in
// ~/.ssh, when their public half is ed25519, the one kind that signs
// the same way every time.
func Candidates(kept func() []string) []string {
	var out []string
	seen := map[string]bool{}
	add := func(path string) {
		if path == "" || seen[path] {
			return
		}
		seen[path] = true
		if _, err := os.Stat(path); err != nil {
			return
		}
		pub, err := os.ReadFile(path + ".pub")
		if err != nil {
			return
		}
		if key, _, _, _, err := ssh.ParseAuthorizedKey(pub); err != nil || key.Type() != ssh.KeyAlgoED25519 {
			return
		}
		out = append(out, path)
	}
	if kept != nil {
		for _, k := range kept() {
			add(k)
		}
	}
	mine, err := remote.DefaultKeyPath()
	if err == nil {
		add(mine)
		pubs, _ := filepath.Glob(filepath.Join(filepath.Dir(mine), "*.pub"))
		for _, p := range pubs {
			add(p[:len(p)-len(".pub")])
		}
	}
	return out
}

// Warnings says what the user should know before trusting a
// key with the secrets, or "".
func Warnings(ring *remote.Ring, v *secrets.Vault, keyFile string) string {
	var out []string
	if agentHolds(ring, keyFile) {
		out = append(out, "A server you forward the agent to can open any copy of the secrets it has.")
	}
	if v != nil {
		if _, err := v.PassphraseFor(keyFile); err == nil {
			out = append(out, "Its passphrase is in the secrets, so another key is still needed to open them.")
		}
	}
	return joinLines(out)
}

// agentHolds reports whether the SSH agent holds a key file's key.
func agentHolds(ring *remote.Ring, keyFile string) bool {
	if ring.AgentTrouble() != nil {
		return false
	}
	pub, err := os.ReadFile(keyFile + ".pub")
	if err != nil {
		return false
	}
	key, _, _, _, err := ssh.ParseAuthorizedKey(pub)
	if err != nil {
		return false
	}
	held, err := remote.AgentHolds(secrets.Fingerprint(key))
	return err == nil && held
}

// joinLines joins paragraphs.
func joinLines(lines []string) string {
	out := ""
	for i, l := range lines {
		if i > 0 {
			out += "\n\n"
		}
		out += l
	}
	return out
}

// RemovingCosts says what is left once a slot goes.
func RemovingCosts(v *secrets.Vault, s secrets.KeySlot) string {
	for _, other := range v.Keys() {
		if other.Fingerprint != s.Fingerprint && Here(other) {
			return "Another key on this machine still opens the secrets."
		}
	}
	if v.TakesAPassphrase() && !s.ByPassphrase() {
		return "The passphrase still opens them here."
	}
	return "Opening them here again needs a key from another machine."
}
