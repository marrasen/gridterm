//go:build !windows

package vfs

import (
	"os"
	"syscall"
)

// localRoots is the one place a POSIX path can start. Everything else
// hangs off it, so there is nothing to ask the machine.
func localRoots() []string { return []string{"/"} }

// SameVolume implements [Volumes]: two folders are on one volume when
// they are on one device. One that cannot be looked at is taken to be,
// and a rename that fails says why.
func (l *Local) SameVolume(a, b string) bool {
	sa, errA := os.Stat(a)
	sb, errB := os.Stat(b)
	if errA != nil || errB != nil {
		return true
	}
	da, okA := sa.Sys().(*syscall.Stat_t)
	db, okB := sb.Sys().(*syscall.Stat_t)
	return !okA || !okB || da.Dev == db.Dev
}
