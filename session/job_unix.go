//go:build !windows

package session

// shellJob is nothing on Unix, which has no job objects.
type shellJob struct{}

// holdShell does nothing on Unix.
func holdShell(int) (shellJob, error) { return shellJob{}, nil }

// end reports that no job took the shell down, so Close kills the shell
// itself.
func (*shellJob) end() bool { return false }
