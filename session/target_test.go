package session

import (
	"strings"
	"testing"
)

func TestParseSSHTarget(t *testing.T) {
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
			cfg, err := ParseSSHTarget(tc.target)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ParseSSHTarget(%q) = %+v, want an error", tc.target, cfg)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseSSHTarget(%q): %v", tc.target, err)
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
func TestParseSSHTargetErrorNamesTheWholeTarget(t *testing.T) {
	_, err := ParseSSHTarget("user@host:bad")
	if err == nil {
		t.Fatal("no error for a bad port")
	}
	if !strings.Contains(err.Error(), "user@host:bad") {
		t.Fatalf("error = %q, want it to quote the whole target", err)
	}
}
