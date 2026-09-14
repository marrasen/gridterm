package remote

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// openSSHAgentPipe is where Windows OpenSSH listens. Unlike Unix it
// publishes no environment variable, so the path is the only way to find
// it.
const openSSHAgentPipe = `\\.\pipe\openssh-ssh-agent`

// dialAgent opens the running SSH agent.
//
// SSH_AUTH_SOCK is honoured only when it names a pipe, because an MSYS
// or Cygwin agent sets it to a small regular file that opens happily and
// then speaks no agent protocol at all.
func dialAgent() (io.ReadWriteCloser, error) {
	path := os.Getenv("SSH_AUTH_SOCK")
	if !strings.HasPrefix(path, `\\`) {
		path = openSSHAgentPipe
	}
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("open the SSH agent at %s: %w", path, err)
	}
	return f, nil
}
