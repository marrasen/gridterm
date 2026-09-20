// Package build says what this program calls itself.
package build

import "runtime/debug"

// Name is what gridterm calls itself to the programs it runs and to
// anything that asks what terminal this is.
const Name = "gridterm"

// Version is the version of the module this was built from, or "dev"
// for a build that has none.
func Version() string {
	if built, ok := debug.ReadBuildInfo(); ok && built.Main.Version != "" {
		return built.Main.Version
	}
	return "dev"
}
