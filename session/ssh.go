package session

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/knownhosts"
)

// dialTimeout bounds the connection attempt. StartSSH runs before the
// window opens, so without it an unresponsive host leaves the user with
// no window, no message and nothing to click.
const dialTimeout = 20 * time.Second

// remoteHangupGrace is how long Close waits for the remote to finish
// after its stdin is closed, so output already on the wire can still be
// read. It matches what the local pty path takes care to preserve.
const remoteHangupGrace = 250 * time.Millisecond

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

	// NoAgent skips the SSH agent even when SSH_AUTH_SOCK is set.
	NoAgent bool

	// Passphrase is asked for a private key's passphrase. Nil skips
	// encrypted keys rather than failing.
	Passphrase func(keyfile string) (string, error)

	// Password is asked for the account password, after key
	// authentication has been tried. Nil skips password auth.
	Password func() (string, error)

	// HostKeyCallback replaces known-hosts checking entirely.
	//
	// This exists so tests can pin a generated host key. Setting it in a
	// real client — to ssh.InsecureIgnoreHostKey, say — removes the only
	// defence against a man-in-the-middle, and nothing else here will
	// notice.
	HostKeyCallback ssh.HostKeyCallback
}

// remote is a shell running on another machine over SSH. No local
// pseudo-terminal is involved: the SSH session channel already carries
// the byte stream, and pty-req and window-change carry the size.
type remote struct {
	client *ssh.Client
	sess   *ssh.Session
	agent  io.Closer // the agent socket, if one was opened

	// writeMu keeps Write and Close off each other. Closing the session
	// stdin writes the channel's sentEOF flag, which Write reads, and
	// the window can close with a keystroke still in flight.
	writeMu sync.Mutex
	stdin   io.WriteCloser

	// out is fed by the session's stdout and stderr. It is unbuffered,
	// which is the flow control: the remote stops sending when the
	// terminal stops reading.
	out  *io.PipeReader
	outW *io.PipeWriter

	// done is closed once the session has ended and the output pipe has
	// been closed behind it.
	done chan struct{}

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
	addr := net.JoinHostPort(cfg.Host, strconv.Itoa(port))

	hostKey := cfg.HostKeyCallback
	if hostKey == nil {
		var err error
		if hostKey, err = knownHostsCallback(cfg.KnownHosts); err != nil {
			return nil, err
		}
	}

	auth, agentConn := authMethods(cfg)
	if len(auth) == 0 {
		if agentConn != nil {
			_ = agentConn.Close()
		}
		return nil, errors.New("ssh: no usable authentication method: " +
			"no agent, no readable private key, and no password source")
	}

	client, err := dial(addr, user, auth, hostKey)
	if err != nil {
		if agentConn != nil {
			_ = agentConn.Close()
		}
		return nil, err
	}

	r := &remote{client: client, agent: agentConn, done: make(chan struct{})}
	if err := r.start(cfg); err != nil {
		_ = r.closeAll()
		return nil, err
	}
	return r, nil
}

// dial connects, retrying once with the host key types that known_hosts
// actually holds for this address.
//
// Without that retry the client and OpenSSH disagree about which host
// key to use: x/crypto prefers rsa-sha2-256 while OpenSSH prefers
// ed25519 and reorders its offer towards what the client already knows.
// A server with both keys then presents the one known_hosts does not
// record, and knownhosts reports "key mismatch" — the man-in-the-middle
// alarm — when nothing at all is wrong.
func dial(addr, user string, auth []ssh.AuthMethod, hostKey ssh.HostKeyCallback) (*ssh.Client, error) {
	base := &ssh.ClientConfig{
		User:            user,
		Auth:            auth,
		HostKeyCallback: hostKey,
		Timeout:         dialTimeout,
	}
	client, err := ssh.Dial("tcp", addr, base)
	if err == nil {
		return client, nil
	}

	var ke *knownhosts.KeyError
	if errors.As(err, &ke) && len(ke.Want) > 0 {
		if algos := wantedKeyTypes(ke.Want); len(algos) > 0 {
			retry := *base
			retry.HostKeyAlgorithms = algos
			if client, err2 := ssh.Dial("tcp", addr, &retry); err2 == nil {
				return client, nil
			} else {
				err = err2
			}
		}
	}
	return nil, describeHostKeyError(err, addr)
}

// wantedKeyTypes lists the key algorithms known_hosts holds for a host,
// most specific first and without duplicates.
func wantedKeyTypes(want []knownhosts.KnownKey) []string {
	var out []string
	seen := map[string]bool{}
	for _, k := range want {
		if k.Key == nil {
			continue
		}
		t := k.Key.Type()
		if seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
		// An RSA entry authorises the modern SHA-2 signature algorithms
		// for the same key; offering only "ssh-rsa" would be refused by
		// a server that has disabled SHA-1.
		if t == ssh.KeyAlgoRSA {
			out = append(out, ssh.KeyAlgoRSASHA256, ssh.KeyAlgoRSASHA512)
		}
	}
	return out
}

// describeHostKeyError separates "I have never seen this host" from "the
// key changed", which knownhosts reports as the same type and which mean
// very different things to a user.
func describeHostKeyError(err error, addr string) error {
	var ke *knownhosts.KeyError
	if !errors.As(err, &ke) {
		return fmt.Errorf("ssh: connect to %s: %w", addr, err)
	}
	if len(ke.Want) == 0 {
		return fmt.Errorf("ssh: %s is not in known_hosts; "+
			"connect once with ssh to record its host key", addr)
	}
	return fmt.Errorf("ssh: host key for %s does not match known_hosts. "+
		"The host may have been rebuilt, or this may be a "+
		"man-in-the-middle: %w", addr, ke)
}

// start opens the session channel, requests a pty and runs the shell.
func (r *remote) start(cfg SSHConfig) error {
	sess, err := r.client.NewSession()
	if err != nil {
		return fmt.Errorf("ssh: open session: %w", err)
	}
	r.sess = sess

	r.out, r.outW = io.Pipe()
	// stdout and stderr both land in one stream, as they would on a
	// local pty. With a pty the remote merges them itself; wiring both
	// costs nothing and covers a server that behaves otherwise.
	sess.Stdout = r.outW
	sess.Stderr = r.outW

	if r.stdin, err = sess.StdinPipe(); err != nil {
		return fmt.Errorf("ssh: stdin: %w", err)
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
		return fmt.Errorf("ssh: request pty: %w", err)
	}

	if len(cfg.Command) == 0 {
		err = sess.Shell()
	} else {
		err = sess.Start(shellQuote(cfg.Command))
	}
	if err != nil {
		return fmt.Errorf("ssh: start: %w", err)
	}

	// Closing the pipe writer is what turns the remote's exit into an
	// io.EOF for the reader, so it happens exactly when the session
	// ends and not before.
	go func() {
		defer close(r.done)
		_ = r.outW.CloseWithError(sessionEnd(r.Wait()))
	}()
	return nil
}

func (r *remote) Read(b []byte) (int, error) { return r.out.Read(b) }

// Write sends input to the remote shell.
func (r *remote) Write(b []byte) (int, error) {
	r.writeMu.Lock()
	defer r.writeMu.Unlock()
	if r.stdin == nil {
		return 0, io.ErrClosedPipe
	}
	return r.stdin.Write(b)
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
	r.writeMu.Lock()
	if r.stdin != nil {
		_ = r.stdin.Close()
	}
	r.writeMu.Unlock()

	// Give the session a moment to finish on its own. Tearing the
	// connection down immediately would discard whatever the remote had
	// already sent, which is the same mistake the local pty path takes
	// care to avoid.
	if r.done != nil {
		select {
		case <-r.done:
		case <-time.After(remoteHangupGrace):
		}
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
	if r.agent != nil {
		_ = r.agent.Close()
	}
	return errors.Join(errs...)
}

// sessionEnd turns the reason a session ended into what a reader should
// see.
//
// A remote shell exiting, with any status or on a signal, is an ordinary
// end of session and becomes io.EOF. A session that ends with no exit
// status at all is not: that is what a dropped connection looks like,
// and reporting it as a clean logout hides the difference between
// closing a window and losing the network.
func sessionEnd(err error) error {
	if err == nil {
		return io.EOF
	}
	var exit *ssh.ExitError
	if errors.As(err, &exit) {
		return io.EOF
	}
	var missing *ssh.ExitMissingError
	if errors.As(err, &missing) {
		return errors.New("ssh: connection closed by the remote host")
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

	// Keep only lines this library can parse. OpenSSH skips a bad line;
	// knownhosts.New rejects the whole file, so one truncated entry — or
	// one written by a newer OpenSSH with a key type this version does
	// not know — would otherwise disable SSH for every host.
	good, err := usableKnownHostLines(paths)
	if err != nil {
		return nil, err
	}
	if len(good) == 0 {
		return nil, fmt.Errorf("ssh: no usable known_hosts entries found (looked in %v); "+
			"connect once with ssh to record the host key", paths)
	}

	tmp, err := os.CreateTemp("", "gridterm-known-hosts-*")
	if err != nil {
		return nil, fmt.Errorf("ssh: stage known_hosts: %w", err)
	}
	defer os.Remove(tmp.Name())
	for _, l := range good {
		if _, err := tmp.Write(append(l, '\n')); err != nil {
			tmp.Close()
			return nil, fmt.Errorf("ssh: stage known_hosts: %w", err)
		}
	}
	if err := tmp.Close(); err != nil {
		return nil, fmt.Errorf("ssh: stage known_hosts: %w", err)
	}

	cb, err := knownhosts.New(tmp.Name())
	if err != nil {
		return nil, fmt.Errorf("ssh: read known_hosts: %w", err)
	}
	return cb, nil
}

// usableKnownHostLines returns the lines of the given files that parse.
func usableKnownHostLines(paths []string) ([][]byte, error) {
	var out [][]byte
	var found bool
	for _, p := range paths {
		f, err := os.Open(p)
		if err != nil {
			continue
		}
		found = true
		sc := bufio.NewScanner(f)
		// Certificate entries can be long; the default 64 KiB token
		// limit is generous but a single huge line should not abort the
		// whole file.
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
			line := bytes.TrimSpace(sc.Bytes())
			if len(line) == 0 || line[0] == '#' {
				continue
			}
			if _, _, _, _, _, err := ssh.ParseKnownHosts(line); err != nil {
				continue
			}
			out = append(out, append([]byte(nil), line...))
		}
		f.Close()
	}
	if !found {
		return nil, fmt.Errorf("ssh: no known_hosts file found (looked in %v); "+
			"connect once with ssh to record the host key", paths)
	}
	return out, nil
}

// authMethods assembles the authentication methods to try, in the order
// a user expects: the agent first, then on-disk keys, then a password.
// The returned closer owns the agent socket, if one was opened.
func authMethods(cfg SSHConfig) ([]ssh.AuthMethod, io.Closer) {
	var methods []ssh.AuthMethod
	var agentConn io.Closer

	if sock := os.Getenv("SSH_AUTH_SOCK"); sock != "" && !cfg.NoAgent {
		if conn, err := net.Dial("unix", sock); err == nil {
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
// hands the command string to the user's shell, so this assumes that
// shell is POSIX — cmd.exe ignores single quotes entirely.
func shellQuote(argv []string) string {
	var sb strings.Builder
	for i, a := range argv {
		if i > 0 {
			sb.WriteByte(' ')
		}
		sb.WriteByte('\'')
		for _, c := range []byte(a) {
			if c == '\'' {
				// End the quote, emit an escaped quote, reopen.
				sb.WriteString(`'\''`)
				continue
			}
			sb.WriteByte(c)
		}
		sb.WriteByte('\'')
	}
	return sb.String()
}
