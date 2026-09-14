package remote

import (
	"errors"
	"io"
	"os"
	"os/user"
	"path/filepath"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

// authMethods assembles the authentication methods to try, in the order
// a user expects: the agent first, then on-disk keys, then a password.
// The returned closer owns the agent socket, if one was opened.
func authMethods(cfg Config) ([]ssh.AuthMethod, io.Closer) {
	var methods []ssh.AuthMethod
	var agentConn io.Closer

	if !cfg.NoAgent {
		if conn, err := dialAgent(); err == nil {
			agentConn = conn
			methods = append(methods,
				ssh.PublicKeysCallback(agent.NewClient(conn).Signers))
		}
	}

	if signers := loadIdentities(cfg); len(signers) > 0 {
		methods = append(methods, ssh.PublicKeys(signers...))
	}

	if cfg.Password != nil {
		methods = append(methods, ssh.PasswordCallback(cfg.Password))
	}
	return methods, agentConn
}

// loadIdentities reads private keys from disk. A key that cannot be read
// or decrypted is skipped rather than fatal: the agent or another key
// may still work, and failing the whole connection because one stale key
// file has a passphrase would be unhelpful.
func loadIdentities(cfg Config) []ssh.Signer {
	paths := cfg.Identities
	if len(paths) == 0 {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil
		}
		for _, name := range []string{"id_ed25519", "id_ecdsa", "id_rsa"} {
			paths = append(paths, filepath.Join(home, ".ssh", name))
		}
	}

	var signers []ssh.Signer
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		signer, err := ssh.ParsePrivateKey(b)
		if err == nil {
			signers = append(signers, signer)
			continue
		}
		var needsPass *ssh.PassphraseMissingError
		if !errors.As(err, &needsPass) || cfg.Passphrase == nil {
			continue
		}
		pass, err := cfg.Passphrase(p)
		if err != nil {
			continue
		}
		if signer, err := ssh.ParsePrivateKeyWithPassphrase(b, []byte(pass)); err == nil {
			signers = append(signers, signer)
		}
	}
	return signers
}

// currentUser returns the local account name, used as the SSH user when
// none was given.
func currentUser() (string, error) {
	if u, err := user.Current(); err == nil && u.Username != "" {
		return u.Username, nil
	}
	// os/user needs cgo or a readable passwd database; the environment
	// is a reasonable second source.
	for _, k := range []string{"USER", "USERNAME", "LOGNAME"} {
		if v := os.Getenv(k); v != "" {
			return v, nil
		}
	}
	return "", errors.New("no current user")
}
