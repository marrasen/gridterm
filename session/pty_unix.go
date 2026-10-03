//go:build !windows

package session

import (
	"fmt"
	"os"

	"github.com/aymanbagabas/go-pty"
)

// terminal is the pseudo-terminal a local shell runs on.
type terminal = pty.Pty

// openTerminal opens a pseudo-terminal of cols by rows, or of go-pty's
// size when either is not given.
func openTerminal(cols, rows int) (terminal, error) {
	p, err := pty.New()
	if err != nil {
		return nil, fmt.Errorf("open pty: %w", err)
	}
	if cols > 0 && rows > 0 {
		if err := p.Resize(cols, rows); err != nil {
			_ = p.Close()
			return nil, fmt.Errorf("size pty: %w", err)
		}
	}
	return p, nil
}

// startIn runs the program at path on p, with args its arguments,
// args[0] its name, in dir, with env. It returns the program and what
// waits for it to end.
func startIn(p terminal, path string, args []string, dir string, env []string) (*os.Process, func() error, error) {
	c := p.Command(path, args[1:]...)
	c.Args[0] = args[0]
	c.Dir = dir
	c.Env = env
	if err := c.Start(); err != nil {
		return nil, nil, err
	}
	return c.Process, c.Wait, nil
}
