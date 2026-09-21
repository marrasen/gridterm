// Package build says what this program calls itself, and which build it
// is.
package build

import (
	"runtime/debug"
	"strings"
)

// Name is what gridterm calls itself to the programs it runs and to
// anything that asks what terminal this is.
const Name = "gridterm"

// devVersion is what a build that was never tagged calls itself.
const devVersion = "dev"

// version is stamped in at link time by a release build:
//
//	go build -ldflags "-X github.com/marrasen/gridterm/internal/build.version=v0.1.0"
//
// It is empty everywhere else, and the answer is then worked out from
// what the toolchain recorded. See the release target in the Makefile.
var version string

// Version is what this build calls itself.
//
// It goes to a program in a pane as TERM_PROGRAM_VERSION and to an agent
// over MCP, and it is the first thing worth knowing about a bug report.
// So a build that was never tagged still says something useful about
// which build it is, rather than a word that fits every build ever made.
func Version() string {
	if v := strings.TrimSpace(version); v != "" {
		return v
	}
	built, ok := debug.ReadBuildInfo()
	if !ok {
		return devVersion
	}
	return versionOf(built)
}

// versionOf reads a version out of what the toolchain recorded, for a
// build with nothing stamped in.
//
// The commit comes first, and the module's own version only when there
// is no commit. Those two cases do not overlap: a build from a checkout
// has version control to ask and a module fetched by version does not.
// Asking in this order keeps the answer to the question a reader is
// really asking -- which build is this -- rather than handing back the
// pseudo-version the module system makes up for an untagged commit,
// which looks like a release and is not one.
//
// Taken apart from Version so a test can describe a build rather than
// depend on the one it is running as.
func versionOf(built *debug.BuildInfo) string {
	var revision string
	var modified bool
	for _, set := range built.Settings {
		switch set.Key {
		case "vcs.revision":
			revision = set.Value
		case "vcs.modified":
			modified = set.Value == "true"
		}
	}

	if revision == "" {
		// Fetched by version rather than built from a checkout, so the
		// module knows what it is: this is what
		// `go install github.com/marrasen/gridterm@v0.1.0` gives.
		// "(devel)" is the toolchain saying it has no idea.
		if v := built.Main.Version; v != "" && v != "(devel)" {
			return v
		}
		return devVersion
	}

	// Twelve characters, which is what a git log is read with and is
	// long enough to find a commit by.
	if len(revision) > 12 {
		revision = revision[:12]
	}
	out := devVersion + "-" + revision
	if modified {
		// Said, because a build from a tree with uncommitted changes
		// cannot be got back to from the commit it names.
		out += "-dirty"
	}
	return out
}
