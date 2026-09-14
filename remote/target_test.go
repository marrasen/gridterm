package remote

import (
	"strings"
	"testing"
)

func TestParseTarget(t *testing.T) {
	cases := []struct {
		target   string
		wantUser string
		wantHost string
		wantPort int
		wantErr  bool
	}{
		{"host", "", "host", 0, false},
		{"host:2222", "", "host", 2222, false},
		{"user@host", "user", "host", 0, false},
		{"user@host:2222", "user", "host", 2222, false},
		{"192.0.2.1:22", "", "192.0.2.1", 22, false},
		// IPv6 literals. A bare "::1" is an address, not a host and a
		// port, so splitting on the first colon gets it wrong.
		{"[::1]:22", "", "::1", 22, false},
		{"[::1]", "", "::1", 0, false},
		{"::1", "", "::1", 0, false},
		{"fe80::1", "", "fe80::1", 0, false},
		{"user@[::1]:2222", "user", "::1", 2222, false},
		// Ports that are not ports.
		{"host:0", "", "", 0, true},
		{"host:65536", "", "", 0, true},
		{"host:-1", "", "", 0, true},
		{"host:nope", "", "", 0, true},
		{"", "", "", 0, true},
		{"user@", "", "", 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.target, func(t *testing.T) {
			cfg, err := ParseTarget(tc.target)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ParseTarget(%q) = %+v, want an error", tc.target, cfg)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseTarget(%q): %v", tc.target, err)
			}
			if cfg.User != tc.wantUser || cfg.Host != tc.wantHost || cfg.Port != tc.wantPort {
				t.Errorf("= user %q host %q port %d, want user %q host %q port %d",
					cfg.User, cfg.Host, cfg.Port, tc.wantUser, tc.wantHost, tc.wantPort)
			}
		})
	}
}

// The error must name what the user typed, not the half of it left after
// the username was stripped.
func TestParseTargetErrorNamesTheWholeTarget(t *testing.T) {
	_, err := ParseTarget("user@host:bad")
	if err == nil {
		t.Fatal("no error for a bad port")
	}
	if !strings.Contains(err.Error(), "user@host:bad") {
		t.Fatalf("error = %q, want it to quote the whole target", err)
	}
}

// The default port is filled in when the address is built, not when the
// target is parsed, so a config a user typed keeps saying "no port".
func TestConfigAddrFillsInTheDefaultPort(t *testing.T) {
	cases := []struct {
		cfg  Config
		want string
	}{
		{Config{Host: "example.com"}, "example.com:22"},
		{Config{Host: "example.com", Port: 2222}, "example.com:2222"},
		{Config{Host: "::1", Port: 22}, "[::1]:22"},
	}
	for _, tc := range cases {
		if got := tc.cfg.addr(); got != tc.want {
			t.Errorf("Config{%q, %d}.addr() = %q, want %q",
				tc.cfg.Host, tc.cfg.Port, got, tc.want)
		}
	}
}

// A host name is written into known_hosts verbatim once its key is
// trusted, so anything that could put a second line in that file is
// refused before it gets near.
func TestParseTargetRejectsAHostThatIsNotAHost(t *testing.T) {
	for _, target := range []string{
		"host\nevil.example ssh-ed25519 AAAA",
		"host evil.example",
		"host\tname",
		"host#comment",
		"user@ho\x00st",
	} {
		if cfg, err := ParseTarget(target); err == nil {
			t.Errorf("ParseTarget(%q) = %+v, want an error", target, cfg)
		}
	}
}
