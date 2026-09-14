package remote

import (
	"errors"
	"fmt"
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
//
// An agent that cannot be opened is not an error on its own, because a
// key or a password may still work. The reason is folded into the error
// for the case where nothing else does either.
func authMethods(cfg Config) ([]ssh.AuthMethod, io.Closer, error) {
	var methods []ssh.AuthMethod
	var agentConn io.Closer
	var agentErr error

	if !cfg.NoAgent {
		conn, err := dialAgent()
		switch {
		case err != nil:
			agentErr = err
		default:
			agentConn = conn
			methods = append(methods,
				ssh.PublicKeysCallback(agent.NewClient(conn).Signers))
		}
	}

	signers, err := loadIdentities(cfg)
	if err != nil {
		if agentConn != nil {
			_ = agentConn.Close()
		}
		return nil, nil, err
	}
	if len(signers) > 0 {
		methods = append(methods, ssh.PublicKeys(signers...))
	}

	if cfg.Password != nil {
		methods = append(methods, ssh.PasswordCallback(cfg.Password))
	}
	if len(methods) == 0 && agentErr != nil {
		return nil, nil, fmt.Errorf("remote: nothing to authenticate with: %w", agentErr)
	}
	return methods, agentConn, nil
}

// loadIdentities reads private keys from disk.
//
// A key the caller named is one it asked for, so a failure to read or
// decrypt it fails the connection. One of the three defaults is skipped
// instead: most machines have only one of them, and failing because a
// stale id_rsa has a passphrase would be unhelpful when the agent holds
// the key that works.
func loadIdentities(cfg Config) ([]ssh.Signer, error) {
	if cfg.NoIdentities {
		return nil, nil
	}
	paths, named := cfg.Identities, true
	if len(paths) == 0 {
		named = false
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("remote: no home directory to look for keys in: %w", err)
		}
		for _, name := range []string{"id_ed25519", "id_ecdsa", "id_rsa"} {
			paths = append(paths, filepath.Join(home, ".ssh", name))
		}
	}

	var signers []ssh.Signer
	for _, p := range paths {
		signer, err := loadIdentity(p, cfg.Passphrase)
		if err == nil {
			signers = append(signers, signer)
			continue
		}
		if named {
			return nil, fmt.Errorf("remote: private key %s: %w", p, err)
		}
	}
	return signers, nil
}

// loadIdentity reads one private key, asking for its passphrase when it
// has one.
func loadIdentity(path string, ask func(string) (string, error)) (ssh.Signer, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	signer, err := ssh.ParsePrivateKey(b)
	if err == nil {
		return signer, nil
	}
	var needsPass *ssh.PassphraseMissingError
	if !errors.As(err, &needsPass) {
		return nil, err
	}
	if ask == nil {
		return nil, errors.New("the key has a passphrase and there is nothing to ask for it")
	}
	pass, err := ask(path)
	if err != nil {
		return nil, err
	}
	return ssh.ParsePrivateKeyWithPassphrase(b, []byte(pass))
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
