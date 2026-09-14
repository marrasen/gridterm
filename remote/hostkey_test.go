package remote

import (
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"

	"github.com/marrasen/gridterm/internal/sshtest"
)

// checkedConfig connects with real known_hosts checking rather than the
// pinned key the other tests use. Everything about host key checking
// goes through here.
func checkedConfig(t *testing.T, s *sshtest.Server, knownHosts ...string) Config {
	t.Helper()
	host, port := s.Host()
	return Config{
		Host: host, Port: port, User: "tester",
		KnownHosts: knownHosts,
		Password:   func() (string, error) { return sshtest.Password, nil },
		NoAgent:    true,
		Identities: []string{filepath.Join(t.TempDir(), "none")},
	}
}

// The host key must be checked. A terminal that silently trusts an
// unknown key can be man-in-the-middled without anyone noticing.
func TestDialRejectsTheWrongHostKey(t *testing.T) {
	s := sshtest.New(t)
	other := sshtest.New(t)

	cfg := testConfig(t, s)
	cfg.HostKeyCallback = ssh.FixedHostKey(other.HostKey())
	if _, err := Dial(cfg); err == nil {
		t.Fatal("Dial accepted a connection with the wrong host key")
	}
}

// With no known_hosts file and no override, the connection must fail
// rather than fall back to trusting anything.
func TestDialWithoutKnownHostsRefusesToConnect(t *testing.T) {
	s := sshtest.New(t)

	_, err := Dial(checkedConfig(t, s, "/nonexistent/known_hosts"))
	if err == nil {
		t.Fatal("Dial connected with no known_hosts to check against")
	}
	if !strings.Contains(err.Error(), "known_hosts") {
		t.Fatalf("error = %v, want it to mention known_hosts", err)
	}
}

func TestDialAcceptsAHostInKnownHosts(t *testing.T) {
	s := sshtest.New(t)

	c, err := Dial(checkedConfig(t, s, sshtest.WriteKnownHosts(t, s.KnownHostsLine())))
	if err != nil {
		t.Fatalf("Dial with a matching known_hosts entry: %v", err)
	}
	_ = c.Close()
}

// An unknown host and a changed key are different things and must say
// so. Reporting an unknown host as "key mismatch" trains the user to
// ignore the one message that matters.
func TestDialUnknownHostSaysSo(t *testing.T) {
	s := sshtest.New(t)
	other := sshtest.New(t)

	// A file with an entry for a different host.
	kh := sshtest.WriteKnownHosts(t, other.KnownHostsLine())
	_, err := Dial(checkedConfig(t, s, kh))
	if err == nil {
		t.Fatal("Dial accepted a host that is not in known_hosts")
	}
	if !strings.Contains(err.Error(), "not in known_hosts") {
		t.Fatalf("error = %v, want it to say the host is not in known_hosts", err)
	}
}

func TestDialChangedHostKeySaysSo(t *testing.T) {
	s := sshtest.New(t)
	other := sshtest.New(t)

	// An entry for this address, but holding a different key.
	kh := sshtest.WriteKnownHosts(t, s.LineFor(other.HostKey()))
	_, err := Dial(checkedConfig(t, s, kh))
	if err == nil {
		t.Fatal("Dial accepted a host whose key had changed")
	}
	if !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("error = %v, want it to say the key does not match", err)
	}
	if !strings.Contains(err.Error(), "man-in-the-middle") {
		t.Fatalf("error = %v, want it to name the risk", err)
	}
}

// OpenSSH skips a line it cannot parse. knownhosts.New rejects the whole
// file, so one truncated entry — or one written by a newer OpenSSH with
// a key type this version does not know — would otherwise disable SSH
// for every host.
func TestDialSkipsUnparseableKnownHostsLines(t *testing.T) {
	s := sshtest.New(t)

	kh := sshtest.WriteKnownHosts(t,
		"garbage-line-with-no-key",
		"somehost ssh-ed25519 not-base64!!",
		"somehost ssh-brandnew AAAA",
		"# a comment",
		s.KnownHostsLine(),
	)

	c, err := Dial(checkedConfig(t, s, kh))
	if err != nil {
		t.Fatalf("Dial: %v; one bad line disabled the whole file", err)
	}
	_ = c.Close()
}

func TestDialKnownHostsWithOnlyBadLinesIsAnError(t *testing.T) {
	s := sshtest.New(t)

	kh := sshtest.WriteKnownHosts(t, "garbage", "more garbage")
	_, err := Dial(checkedConfig(t, s, kh))
	if err == nil {
		t.Fatal("Dial connected with no usable known_hosts entries")
	}
	if !strings.Contains(err.Error(), "known_hosts") {
		t.Fatalf("error = %v, want it to mention known_hosts", err)
	}
}
