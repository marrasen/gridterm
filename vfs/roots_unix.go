//go:build !windows

package vfs

// localRoots is the one place a POSIX path can start. Everything else
// hangs off it, so there is nothing to ask the machine.
func localRoots() []string { return []string{"/"} }
