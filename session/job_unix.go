//go:build !windows

package session

// shellJob is nothing on Unix, which has no job objects.
type shellJob struct{}

// holdShell does nothing on Unix.
func holdShell(int) (shellJob, error) { return shellJob{}, nil }

// letGo does nothing on Unix.
func (*shellJob) letGo() error { return nil }
