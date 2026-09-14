package remote

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path/filepath"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// maxKnownHostsLine caps one line of known_hosts. Certificate entries
// can be long, so the cap is generous; a line over it is a read failure
// rather than a line to skip, because carrying on would drop every entry
// after it.
const maxKnownHostsLine = 1024 * 1024

// errUnknownHost is what the checker reports for a host with no entry,
// when there is nobody to ask about it.
var errUnknownHost = errors.New("the host is not in known_hosts")

// hostKeys checks a server's key against known_hosts and can add one the
// user confirms.
type hostKeys struct {
	// check is the known_hosts verification. A host with no entry comes
	// back as a knownhosts.KeyError with nothing wanted.
	check ssh.HostKeyCallback

	// add is the file a newly trusted key is appended to: the first path
	// given, or ~/.ssh/known_hosts.
	add string
}

// callback returns the check to hand to x/crypto.
//
// An unknown host is offered to the user, and only an explicit yes adds
// it. A key that does not match an entry already there is refused
// outright with no way past it: that is what a man-in-the-middle looks
// like, and there is no answer a user could give that would make
// connecting safe.
func (h *hostKeys) callback(ctx context.Context, ask Ask) ssh.HostKeyCallback {
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		err := h.check(hostname, remote, key)
		if err == nil {
			return nil
		}
		var ke *knownhosts.KeyError
		if !errors.As(err, &ke) || len(ke.Want) > 0 {
			return err
		}
		if ask == nil {
			return fmt.Errorf("%w: %w", errUnknownHost, err)
		}
		ok, askErr := ask.TrustHostKey(ctx, HostKey{Addr: hostname, Key: key})
		if askErr != nil {
			return askErr
		}
		if !ok {
			return fmt.Errorf("the host key for %s was not accepted", hostname)
		}
		return h.record(hostname, key)
	}
}

// record appends a newly trusted key to known_hosts.
//
// A failure to write fails the connection. Connecting anyway would mean
// answering the same question on every connection, and a user who is
// asked the same question often enough stops reading it.
func (h *hostKeys) record(hostname string, key ssh.PublicKey) error {
	if h.add == "" {
		return errors.New("remote: nowhere to record the host key")
	}
	if err := os.MkdirAll(filepath.Dir(h.add), 0o700); err != nil {
		return fmt.Errorf("remote: record the host key: %w", err)
	}
	f, err := os.OpenFile(h.add, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("remote: record the host key: %w", err)
	}
	line := knownhosts.Line([]string{hostname}, key) + "\n"
	if _, err := f.WriteString(line); err != nil {
		_ = f.Close()
		return fmt.Errorf("remote: record the host key: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("remote: record the host key: %w", err)
	}
	return nil
}

// loadKnownHosts reads the known_hosts files and returns the check built
// from them.
//
// A file that is not there is not an error: every host is simply unknown
// until one is recorded. A file that exists and cannot be read is an
// error, because a host that is in it would otherwise read as unknown --
// or, worse, as a host whose key had changed.
func loadKnownHosts(paths []string) (*hostKeys, error) {
	if len(paths) == 0 {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("remote: no home directory for known_hosts: %w", err)
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
	check, err := checkerFor(good, paths)
	if err != nil {
		return nil, err
	}
	return &hostKeys{check: check, add: paths[0]}, nil
}

// checkerFor builds the verification from the lines that parsed. With no
// lines at all, every host is unknown.
func checkerFor(lines [][]byte, paths []string) (ssh.HostKeyCallback, error) {
	if len(lines) == 0 {
		return func(hostname string, _ net.Addr, _ ssh.PublicKey) error {
			return &knownhosts.KeyError{}
		}, nil
	}

	tmp, err := os.CreateTemp("", "gridterm-known-hosts-*")
	if err != nil {
		return nil, fmt.Errorf("remote: stage known_hosts: %w", err)
	}
	defer os.Remove(tmp.Name())
	for _, l := range lines {
		if _, err := tmp.Write(append(l, '\n')); err != nil {
			_ = tmp.Close()
			return nil, fmt.Errorf("remote: stage known_hosts: %w", err)
		}
	}
	if err := tmp.Close(); err != nil {
		return nil, fmt.Errorf("remote: stage known_hosts: %w", err)
	}

	cb, err := knownhosts.New(tmp.Name())
	if err != nil {
		// The staged copy is about to be removed, so the path in the
		// wrapped error names a file the user cannot go and look at.
		return nil, fmt.Errorf("remote: read known_hosts %v: %w", paths, err)
	}
	return cb, nil
}

// usableKnownHostLines returns the lines of the given files that parse.
//
// A file that is not there is skipped, because the default list names
// one path and a machine that has never used ssh does not have it. Any
// other failure to read one stops the lot: a truncated list would say a
// host is unknown, or say its key had changed, when the truth is that a
// file could not be read.
func usableKnownHostLines(paths []string) ([][]byte, error) {
	var out [][]byte
	for _, p := range paths {
		f, err := os.Open(p)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("remote: read known_hosts: %w", err)
		}
		lines, err := parseKnownHosts(f)
		_ = f.Close()
		if err != nil {
			return nil, fmt.Errorf("remote: read known_hosts %s: %w", p, err)
		}
		out = append(out, lines...)
	}
	return out, nil
}

// parseKnownHosts reads one known_hosts file, keeping the lines that
// parse and reporting a failure to read it.
func parseKnownHosts(f *os.File) ([][]byte, error) {
	var out [][]byte
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), maxKnownHostsLine)
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
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
