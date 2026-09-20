package shells

import "testing"

// Windows reaches a WSL distribution's files on a share, so a browser
// needs nothing of its own to show one.
func TestWhereWindowsReachesADistribution(t *testing.T) {
	if got, want := WSLRoot("Ubuntu"), `\\wsl.localhost\Ubuntu`; got != want {
		t.Errorf("Ubuntu is at %q, want %q", got, want)
	}
}

// A path inside a distribution translates to the one Windows reaches
// it by, which is what turns a shell saying where it is into somewhere
// to put a dropped file.
func TestAPathInsideADistributionTranslates(t *testing.T) {
	for _, tc := range []struct{ distro, unix, want string }{
		{"Ubuntu", "/home/marcus", `\\wsl.localhost\Ubuntu\home\marcus`},
		{"Debian", "/", `\\wsl.localhost\Debian\`},
		{"Ubuntu", "/tmp/a b", `\\wsl.localhost\Ubuntu\tmp\a b`},
		// Not a distribution, or not a path inside one.
		{"", "/home", ""},
		{"Ubuntu", "relative/path", ""},
		{"Ubuntu", `C:\Users`, ""},
		{"Ubuntu", "", ""},
	} {
		if got := WindowsPath(tc.distro, tc.unix); got != tc.want {
			t.Errorf("%q on %q gave %q, want %q", tc.unix, tc.distro, got, tc.want)
		}
	}
}

// The translation undoes UnixPath, so a directory that went one way
// comes back the same.
func TestTheTranslationGoesBothWays(t *testing.T) {
	// A path under /mnt is this machine's own, reached through the
	// distribution rather than as a drive. It still resolves.
	unix := UnixPath(`C:\Workspace`)
	if unix != "/mnt/c/Workspace" {
		t.Fatalf("UnixPath gave %q", unix)
	}
	if got, want := WindowsPath("Ubuntu", unix), `\\wsl.localhost\Ubuntu\mnt\c\Workspace`; got != want {
		t.Errorf("it came back as %q, want %q", got, want)
	}
}
