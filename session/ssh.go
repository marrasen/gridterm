package session

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/knownhosts"
)

// SSHConfig describes a shell to run on another machine.
type SSHConfig struct {
	// Host is the name or address to connect to. Port defaults to 22.
	Host string
	Port int

	// User defaults to the local username.
	User string

	// Command is what to run. Empty asks for the user's login shell.
	Command []string

	// Cols and Rows are the initial window size.
	Cols, Rows int

	// Term is the TERM value sent to the remote. Defaults to
	// xterm-256color.
	Term string

	// KnownHosts lists the files to verify the host key against. Empty
	// means the usual ~/.ssh/known_hosts.
	KnownHosts []string

	// Identities lists private key files to try. Empty means the usual
	// ~/.ssh/id_ed25519, id_ecdsa and id_rsa.
	Identities []string

	// Passphrase is asked for a private key's passphrase. Nil skips
	// encrypted keys rather than failing.
	Passphrase func(keyfile string) (string, error)

	// Password is asked for the account password, after key
	// authentication has been tried. Nil skips password auth.
	Password func() (string, error)

	// HostKeyCallback overrides known-hosts checking. Intended for
	// tests; leaving it nil is what you want in a real client.
	HostKeyCallback ssh.HostKeyCallback
}

// remote is a shell running on another machine over SSH. No local
// pseudo-terminal is involved: the SSH session channel already carries
// the byte stream, and pty-req and window-change carry the size.
type remote struct {
	client *ssh.Client
	sess   *ssh.Session

	stdin io.WriteCloser
	// out is fed by the session's stdout and stderr. It is unbuffered,
	// which is the flow control: the remote stops sending when the
	// terminal stops reading.
	out   *io.PipeReader
	outW  *io.PipeWriter
	outWg sync.WaitGroup

	closeOnce sync.Once
	closeErr  error
	waitOnce  sync.Once
	waitErr   error
}

// StartSSH opens an SSH connection and starts a shell on it.
func StartSSH(cfg SSHConfig) (Session, error) {
	if cfg.Host == "" {
		return nil, errors.New("ssh: no host given")
	}
	user := cfg.User
	if user == "" {
		u, err := currentUser()
		if err != nil {
			return nil, fmt.Errorf("ssh: no user given and none found: %w", err)
		}
		user = u
	}
	port := cfg.Port
	if port == 0 {
		port = 22
	}

	hostKey := cfg.HostKeyCallback
	if hostKey == nil {
		var err error
		if hostKey, err = knownHostsCallback(cfg.KnownHosts); err != nil {
			return nil, err
		}
	}

	auth, err := authMethods(cfg)
	if err != nil {
		return nil, err
	}
	if len(auth) == 0 {
		return nil, errors.New("ssh: no usable authentication method")
	}

	addr := net.JoinHostPort(cfg.Host, strconv.Itoa(port))
	client, err := ssh.Dial("tcp", addr, &ssh.ClientConfig{
		User:            user,
		Auth:            auth,
		HostKeyCallback: hostKey,
	})
	if err != nil {
		return nil, fmt.Errorf("ssh: connect to %s: %w", addr, err)
	}

	sess, err := client.NewSession()
	if err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("ssh: open session: %w", err)
	}

	r := &remote{client: client, sess: sess}
	r.out, r.outW = io.Pipe()
	// stdout and stderr both land in one stream, as they would on a
	// local pty. With a pty requested the remote merges them itself, but
	// a server that declines the pty would otherwise drop stderr.
	sess.Stdout = r.outW
	sess.Stderr = r.outW

	if r.stdin, err = sess.StdinPipe(); err != nil {
		_ = r.closeAll()
		return nil, fmt.Errorf("ssh: stdin: %w", err)
	}

	term := cfg.Term
	if term == "" {
		term = "xterm-256color"
	}
	cols, rows := max(cfg.Cols, 1), max(cfg.Rows, 1)
	// Ask for the pty before starting anything, so the remote shell sees
	// the right size in its very first prompt.
	if err := sess.RequestPty(term, rows, cols, ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 38400,
		ssh.TTY_OP_OSPEED: 38400,
	}); err != nil {
		_ = r.closeAll()
		return nil, fmt.Errorf("ssh: request pty: %w", err)
	}

	if len(cfg.Command) == 0 {
		err = sess.Shell()
	} else {
		err = sess.Start(shellQuote(cfg.Command))
	}
	if err != nil {
		_ = r.closeAll()
		return nil, fmt.Errorf("ssh: start: %w", err)
	}

	// Closing the pipe writer is what turns the remote's exit into an
	// io.EOF for the reader, so it has to happen exactly when the
	// session ends and not before.
	r.outWg.Add(1)
	go func() {
		defer r.outWg.Done()
		err := r.Wait()
		_ = r.outW.CloseWithError(sessionEnd(err))
	}()

	return r, nil
}

func (r *remote) Read(b []byte) (int, error) { return r.out.Read(b) }

// Write sends input to the remote shell, looping on short writes.
func (r *remote) Write(b []byte) (int, error) {
	total := 0
	for total < len(b) {
		n, err := r.stdin.Write(b[total:])
		total += n
		if err != nil {
			return total, err
		}
		if n == 0 {
			return total, io.ErrShortWrite
		}
	}
	return total, nil
}

func (r *remote) Resize(cols, rows int) error {
	if cols <= 0 || rows <= 0 {
		return nil
	}
	return r.sess.WindowChange(rows, cols)
}

func (r *remote) Wait() error {
	r.waitOnce.Do(func() { r.waitErr = r.sess.Wait() })
	return r.waitErr
}

func (r *remote) Close() error {
	r.closeOnce.Do(func() { r.closeErr = r.closeAll() })
	return r.closeErr
}

func (r *remote) closeAll() error {
	// Closing stdin is the polite hangup: a shell reading its input sees
	// end-of-file and exits, running its own exit hooks on the way.
	if r.stdin != nil {
		_ = r.stdin.Close()
	}
	if r.outW != nil {
		_ = r.outW.CloseWithError(io.EOF)
	}
	var errs []error
	if r.sess != nil {
		if err := r.sess.Close(); err != nil && !errors.Is(err, io.EOF) {
			errs = append(errs, err)
		}
	}
	if r.client != nil {
		if err := r.client.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// sessionEnd turns the reason a session ended into what a reader should
// see. A remote shell exiting non-zero is an ordinary end of session,
// not a read error.
func sessionEnd(err error) error {
	var exit *ssh.ExitError
	if err == nil || errors.As(err, &exit) {
		return io.EOF
	}
	var missing *ssh.ExitMissingError
	if errors.As(err, &missing) {
		return io.EOF
	}
	return err
}

// knownHostsCallback builds host key verification from known_hosts.
//
// There is deliberately no "accept anything" fallback. A terminal that
// silently trusts an unknown host key is a terminal that can be
// man-in-the-middled, and the failure is quiet.
func knownHostsCallback(paths []string) (ssh.HostKeyCallback, error) {
	if len(paths) == 0 {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("ssh: no home directory for known_hosts: %w", err)
		}
		paths = []string{filepath.Join(home, ".ssh", "known_hosts")}
	}
	var usable []string
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			usable = append(usable, p)
		}
	}
	if len(usable) == 0 {
		return nil, fmt.Errorf("ssh: no known_hosts file found (looked in %v); "+
			"connect once with ssh to record the host key", paths)
	}
	cb, err := knownhosts.New(usable...)
	if err != nil {
		return nil, fmt.Errorf("ssh: read known_hosts: %w", err)
	}
	return cb, nil
}

// authMethods assembles the authentication methods to try, in the order
// a user expects: the agent first, then on-disk keys, then a password.
func authMethods(cfg SSHConfig) ([]ssh.AuthMethod, error) {
	var methods []ssh.AuthMethod

	if sock := os.Getenv("SSH_AUTH_SOCK"); sock != "" {
		if conn, err := net.Dial("unix", sock); err == nil {
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
	return methods, nil
}

// loadIdentities reads private keys from disk. A key that cannot be read
// or decrypted is skipped rather than fatal: the agent or another key
// may still work, and failing the whole connection because one stale key
// file has a passphrase would be unhelpful.
func loadIdentities(cfg SSHConfig) []ssh.Signer {
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

// shellQuote joins argv into something the remote login shell will split
// back into the same words. SSH has no argv on the wire: the server
// hands the command string to the user's shell.
func shellQuote(argv []string) string {
	var sb []byte
	for i, a := range argv {
		if i > 0 {
			sb = append(sb, ' ')
		}
		sb = append(sb, '\'')
		for _, c := range []byte(a) {
			if c == '\'' {
				// End the quote, emit an escaped quote, reopen.
				sb = append(sb, '\'', '\\', '\'', '\'')
				continue
			}
			sb = append(sb, c)
		}
		sb = append(sb, '\'')
	}
	return string(sb)
}
