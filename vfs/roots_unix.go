//go:build !windows

package vfs

import (
	"os"
	"strconv"
	"syscall"
)

// localRoots is the one place a POSIX path can start. Everything else
// hangs off it, so there is nothing to ask the machine.
func localRoots() []string { return []string{"/"} }

// VolumeOf implements [Volumes]: a folder's volume is the device it is
// on. One that cannot be looked at says none, which is taken for any,
// and a rename that fails then says why.
func (l *Local) VolumeOf(at string) string {
	fi, err := os.Stat(at)
	if err != nil {
		return ""
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return ""
	}
	return strconv.FormatUint(uint64(st.Dev), 10)
}
