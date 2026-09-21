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
	// A path under /mnt is this machine's own drive. It has to come back
	// as that drive: Windows denies access to one reached through the
	// distribution's share, so a file copied there never arrives.
	unix := UnixPath(`C:\Workspace`)
	if unix != "/mnt/c/Workspace" {
		t.Fatalf("UnixPath gave %q", unix)
	}
	if got, want := WindowsPath("Ubuntu", unix), `C:\Workspace`; got != want {
		t.Errorf("it came back as %q, want %q", got, want)
	}
}

// A drive the distribution mounts is reached as that drive rather than
// through the share.
func TestAMountedDriveComesBackAsTheDrive(t *testing.T) {
	for _, tc := range []struct{ unix, want string }{
		{"/mnt/c/Users/marcus", `C:\Users\marcus`},
		{"/mnt/g/Workspace/gridterm", `G:\Workspace\gridterm`},
		{"/mnt/c/a b/c", `C:\a b\c`},
		{"/mnt/c", `C:\`},
		{"/mnt/c/", `C:\`},
		// The distribution's own files, which the share does reach.
		{"/home/marcus", ""},
		{"/mnt", ""},
		{"/mnt/", ""},
		{"/mnt/wsl/instance", ""},
		{"/mnt/cd/rom", ""},
		{"/mnt/1/x", ""},
		{"relative/path", ""},
		{"", ""},
	} {
		if got := DrivePath(tc.unix); got != tc.want {
			t.Errorf("DrivePath(%q) = %q, want %q", tc.unix, got, tc.want)
		}
	}
}

// A distribution's own files still go through the share, which is the
// only way Windows reaches them.
func TestTheDistributionsOwnFilesStillUseTheShare(t *testing.T) {
	if got, want := WindowsPath("Ubuntu", "/home/marcus"),
		`\\wsl.localhost\Ubuntu\home\marcus`; got != want {
		t.Errorf("it came back as %q, want %q", got, want)
	}
}
