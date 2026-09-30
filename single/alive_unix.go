//go:build !windows

package single

import "syscall"

// alive reports whether a process numbered pid is running.
func alive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}
