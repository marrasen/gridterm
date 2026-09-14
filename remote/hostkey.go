package remote

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io/fs"
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

// knownHostsCallback builds host key verification from known_hosts.
//
// There is deliberately no "accept anything" fallback. A terminal that
// silently trusts an unknown host key is a terminal that can be
// man-in-the-middled, and the failure is quiet.
func knownHostsCallback(paths []string) (ssh.HostKeyCallback, error) {
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
	if len(good) == 0 {
		return nil, fmt.Errorf("remote: no usable known_hosts entries found (looked in %v); "+
			"connect once with ssh to record the host key", paths)
	}

	tmp, err := os.CreateTemp("", "gridterm-known-hosts-*")
	if err != nil {
		return nil, fmt.Errorf("remote: stage known_hosts: %w", err)
	}
	defer os.Remove(tmp.Name())
	for _, l := range good {
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
// three paths and most machines have one. Any other failure to read one
// stops the lot: a truncated list would say a host is unknown, or say
// its key had changed, when the truth is that a file could not be read.
func usableKnownHostLines(paths []string) ([][]byte, error) {
	var out [][]byte
	var found bool
	for _, p := range paths {
		f, err := os.Open(p)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("remote: read known_hosts: %w", err)
		}
		found = true
		lines, err := parseKnownHosts(f)
		_ = f.Close()
		if err != nil {
			return nil, fmt.Errorf("remote: read known_hosts %s: %w", p, err)
		}
		out = append(out, lines...)
	}
	if !found {
		return nil, fmt.Errorf("remote: no known_hosts file found (looked in %v); "+
			"connect once with ssh to record the host key", paths)
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
