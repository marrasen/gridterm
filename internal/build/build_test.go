package build

import (
	"runtime/debug"
	"testing"
)

// A version stamped in at link time is what the build calls itself, and
// nothing else is consulted.
func TestAStampedVersionWins(t *testing.T) {
	was := version
	t.Cleanup(func() { version = was })

	version = "v1.2.3"
	if got := Version(); got != "v1.2.3" {
		t.Errorf("Version() = %q, want the version stamped in", got)
	}
	// Whitespace from a shell that expanded an empty variable is not a
	// version: a release build that lost its tag must not call itself
	// the empty string.
	version = "  \n"
	if got := Version(); got == "  \n" {
		t.Error("Version() handed back whitespace as a version")
	}
}

// A module fetched by version calls itself that version. There is no
// checkout to ask, so what the module knows is all there is.
func TestAModuleFetchedByVersionNamesIt(t *testing.T) {
	built := &debug.BuildInfo{}
	built.Main.Version = "v0.4.1"

	if got := versionOf(built); got != "v0.4.1" {
		t.Errorf("versionOf = %q, want the module's own version", got)
	}
}

// A build from a checkout names its commit even though the module
// system has made up a version for it.
//
// That made-up version reads like a release -- v0.0.0 and a timestamp --
// and naming it would tell a reader this build is something it is not.
func TestAPseudoVersionDoesNotPassForARelease(t *testing.T) {
	built := &debug.BuildInfo{Settings: []debug.BuildSetting{
		{Key: "vcs.revision", Value: "3e62f4547e66624c"},
		{Key: "vcs.modified", Value: "true"},
	}}
	built.Main.Version = "v0.0.0-20260921061232-3e62f4547e66+dirty"

	if got := versionOf(built); got != "dev-3e62f4547e66-dirty" {
		t.Errorf("versionOf = %q, want the commit rather than a made-up version", got)
	}
}

// A build from a checkout names the commit it came from, because
// "(devel)" fits every build ever made and says nothing about this one.
func TestABuildFromACheckoutNamesItsCommit(t *testing.T) {
	for what, tc := range map[string]struct {
		settings []debug.BuildSetting
		want     string
	}{
		"a clean tree": {
			[]debug.BuildSetting{{Key: "vcs.revision", Value: "3e62f45abcd1ef00"}},
			"dev-3e62f45abcd1",
		},
		"a tree with changes in no commit": {
			[]debug.BuildSetting{
				{Key: "vcs.revision", Value: "3e62f45abcd1ef00"},
				{Key: "vcs.modified", Value: "true"},
			},
			"dev-3e62f45abcd1-dirty",
		},
		"no version control at all": {nil, "dev"},
	} {
		t.Run(what, func(t *testing.T) {
			built := &debug.BuildInfo{Settings: tc.settings}
			built.Main.Version = "(devel)"

			if got := versionOf(built); got != tc.want {
				t.Errorf("versionOf = %q, want %q", got, tc.want)
			}
		})
	}
}
