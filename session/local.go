package session

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"sync"

	"github.com/aymanbagabas/go-pty"
)

// LocalConfig describes a shell to run on this machine.
type LocalConfig struct {
	// Command is the program and its arguments. Empty means the user's
	// login shell.
	Command []string

	// Dir is the working directory. Empty means the current one.
	Dir string

	// Env is added to the inherited environment. TERM is set for you
	// unless you set it here.
	Env []string

	// Cols and Rows are the initial window size.
	Cols, Rows int
}

// local is a shell attached to a pseudo-terminal. On Unix that is a
// classic PTY; on Windows it is a ConPTY, which go-pty puts behind the
// same interface.
type local struct {
	pty pty.Pty
	cmd *pty.Cmd

	// closeOnce guards Close: closing the pty twice is not safe, and the
	// UI can reach Close by more than one path (window closed, shell
	// exited, error on read).
	closeOnce sync.Once
	closeErr  error

	waitOnce sync.Once
	waitErr  error
}

// StartLocal runs a shell attached to a new pseudo-terminal.
func StartLocal(cfg LocalConfig) (Session, error) {
	argv := cfg.Command
	if len(argv) == 0 {
		sh, err := defaultShell()
		if err != nil {
			return nil, err
		}
		argv = sh
	}

	p, err := pty.New()
	if err != nil {
		return nil, fmt.Errorf("open pty: %w", err)
	}

	// Size the pty before starting the shell. A shell that reads the
	// window size at startup — which is most of them — would otherwise
	// get the default 80x24 and lay out its prompt for the wrong width.
	if cfg.Cols > 0 && cfg.Rows > 0 {
		if err := p.Resize(cfg.Cols, cfg.Rows); err != nil {
			_ = p.Close()
			return nil, fmt.Errorf("size pty: %w", err)
		}
	}

	c := p.Command(argv[0], argv[1:]...)
	c.Dir = cfg.Dir
	c.Env = append(os.Environ(), cfg.Env...)
	if !hasEnv(cfg.Env, "TERM") {
		c.Env = append(c.Env, "TERM=xterm-256color")
	}

	if err := c.Start(); err != nil {
		_ = p.Close()
		return nil, fmt.Errorf("start %s: %w", argv[0], err)
	}

	l := &local{pty: p, cmd: c}
	// A pty master only reports the child's exit once every slave handle
	// is closed, and go-pty keeps one open in this process for the life
	// of the pty. Without this goroutine a Read blocked on the master
	// never returns after the shell exits, and the window would sit
	// there showing a dead prompt. Reaping the child and closing the pty
	// unblocks it.
	go func() {
		_ = l.Wait()
		_ = l.Close()
	}()
	return l, nil
}

func (l *local) Read(b []byte) (int, error) {
	n, err := l.pty.Read(b)
	// The end of a session arrives in a platform-specific way: EIO on
	// Linux once the last slave closes, a broken pipe on Windows, and
	// os.ErrClosed when the reaper above closes the pty out from under a
	// blocked read. Normalising to io.EOF leaves the caller one case.
	if err != nil && n == 0 && isPtyClosed(err) {
		return 0, io.EOF
	}
	return n, err
}

func (l *local) Write(b []byte) (int, error) { return l.pty.Write(b) }

func (l *local) Resize(cols, rows int) error {
	if cols <= 0 || rows <= 0 {
		return nil
	}
	return l.pty.Resize(cols, rows)
}

func (l *local) Wait() error {
	l.waitOnce.Do(func() { l.waitErr = l.cmd.Wait() })
	return l.waitErr
}

func (l *local) Close() error {
	l.closeOnce.Do(func() {
		// Closing the pty sends the child a hangup. Killing it outright
		// first would deny a shell the chance to run its exit hooks.
		l.closeErr = l.pty.Close()
	})
	return l.closeErr
}

// isPtyClosed reports whether err means the far end went away rather
// than something going wrong.
func isPtyClosed(err error) bool {
	return errors.Is(err, io.EOF) ||
		errors.Is(err, os.ErrClosed) ||
		errPtyHangup(err)
}

func hasEnv(env []string, key string) bool {
	prefix := key + "="
	for _, e := range env {
		if len(e) >= len(prefix) && e[:len(prefix)] == prefix {
			return true
		}
	}
	return false
}

// defaultShell returns the user's login shell, falling back to something
// that exists on the platform.
func defaultShell() ([]string, error) {
	if runtime.GOOS == "windows" {
		if c := os.Getenv("COMSPEC"); c != "" {
			return []string{c}, nil
		}
		// PowerShell is the better shell but cmd.exe is the one that is
		// always present.
		return []string{"cmd.exe"}, nil
	}
	if sh := os.Getenv("SHELL"); sh != "" {
		return []string{sh}, nil
	}
	for _, candidate := range []string{"/bin/bash", "/bin/sh"} {
		if _, err := exec.LookPath(candidate); err == nil {
			return []string{candidate}, nil
		}
	}
	return nil, errors.New("no shell found: set $SHELL or pass a command")
}
