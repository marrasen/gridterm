//go:build !windows

package remote

import (
	"errors"
	"io"
	"net"
	"os"
)

// dialAgent opens the running SSH agent, which on Unix is the socket
// SSH_AUTH_SOCK names.
func dialAgent() (io.ReadWriteCloser, error) {
	sock := os.Getenv("SSH_AUTH_SOCK")
	if sock == "" {
		return nil, errors.New("remote: no SSH agent: SSH_AUTH_SOCK is not set")
	}
	return net.Dial("unix", sock)
}
