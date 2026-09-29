package session

import (
	"errors"
	"os/exec"
	"syscall"
)

// Status is the exit status a program's ending says, as a shell would
// report it, and whether it says one: from a program on a server or in
// another window, which gives its status, or from one here. One here
// killed by a signal counts as 128 plus the signal, as shells count it;
// where there is no signal to name, it has none. A nil ending is 0.
func Status(ending error) (int, bool) {
	if ending == nil {
		return 0, true
	}
	if far, ok := errors.AsType[interface {
		error
		ExitStatus() int
	}](ending); ok {
		return far.ExitStatus(), true
	}
	if here, ok := errors.AsType[*exec.ExitError](ending); ok {
		if code := here.ExitCode(); code >= 0 {
			return code, true
		}
		if s, ok := here.Sys().(interface {
			Signaled() bool
			Signal() syscall.Signal
		}); ok && s.Signaled() {
			return 128 + int(s.Signal()), true
		}
	}
	return 0, false
}
