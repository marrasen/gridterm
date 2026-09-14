package remote

import (
	"fmt"
	"io"
	"os"
)

// openSSHAgentPipe is where Windows OpenSSH listens. Unlike Unix it
// publishes no environment variable, so the path is the only way to find
// it.
const openSSHAgentPipe = `\.\pipe\openssh-ssh-agent`

// dialAgent opens the running SSH agent.
//
// SSH_AUTH_SOCK is honoured first, because a user running a Unix-style
// agent under MSYS or Cygwin sets it, but it names a pipe here rather
// than a socket. Dialling it as a unix socket — which is what this used
// to do — fails on every Windows machine, so the agent was never
// reachable at all.
func dialAgent() (io.ReadWriteCloser, error) {
	path := os.Getenv("SSH_AUTH_SOCK")
	if path == "" {
		path = openSSHAgentPipe
	}
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("remote: open SSH agent %s: %w", path, err)
	}
	return f, nil
}
