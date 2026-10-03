//go:build windows

package session

import (
	"cmp"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/marrasen/kakel/internal/conpty"
)

// terminal is the pseudoconsole a local shell runs in.
type terminal = *conpty.Console

// openTerminal makes a pseudoconsole of cols by rows, or of 80 by 24
// when either is not given.
func openTerminal(cols, rows int) (terminal, error) {
	if cols <= 0 || rows <= 0 {
		cols, rows = 80, 24
	}
	c, err := conpty.New(cols, rows)
	if err != nil {
		return nil, fmt.Errorf("open pty: %w", err)
	}
	return c, nil
}

// startIn runs the program at path in p, with args its command line,
// args[0] its name, in dir, with env. It returns the program and what
// waits for it to end, which reports an exit other than 0 as an
// *exec.ExitError, as os/exec does.
func startIn(p terminal, path string, args []string, dir string, env []string) (*os.Process, func() error, error) {
	// A bare name not found on PATH is looked for in dir, with the
	// extensions PATHEXT gives, as go-pty did.
	if !strings.ContainsAny(path, `/\`) {
		if found, err := exec.LookPath(filepath.Join(cmp.Or(dir, "."), path)); err == nil {
			path = found
		}
	}
	proc, err := p.Start(path, args, dir, env)
	if err != nil {
		return nil, nil, err
	}
	wait := func() error {
		state, err := proc.Wait()
		if err != nil {
			return err
		}
		if !state.Success() {
			return &exec.ExitError{ProcessState: state}
		}
		return nil
	}
	return proc, wait, nil
}
