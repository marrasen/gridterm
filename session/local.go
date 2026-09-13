package session

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/aymanbagabas/go-pty"
)

// hangupGrace is how long Close waits for a child to act on the hangup
// before killing it. A shell exits in microseconds; a child that ignores
// SIGHUP would otherwise outlive the window that started it.
const hangupGrace = 250 * time.Millisecond

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

	// done is closed once the child has been reaped, so Close can tell
	// whether the hangup worked without polling.
	done chan struct{}

	// detached records that this process no longer holds the pty slave,
	// which makes go-pty's own Close report a harmless double close.
	detached bool
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

	l := &local{pty: p, cmd: c, done: make(chan struct{})}

	// Hand the slave back to the child alone. With this process no
	// longer holding it, the master drains and then reports the child's
	// exit by itself, so nothing has to close the pty out from under a
	// pending read — which would throw away whatever output was still
	// buffered.
	l.detached = detachSlave(p)

	go func() {
		defer close(l.done)
		_ = l.Wait()
		if !l.detached {
			// No slave to release, so the only way to unblock a pending
			// read is to close the pty. On Windows that is also what
			// flushes the pseudoconsole, so no output is lost.
			_ = l.Close()
		}
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

// Write sends input to the child. It loops until everything is written,
// because a short write with no error is legal for an io.Writer and
// would otherwise drop the tail of a paste silently.
func (l *local) Write(b []byte) (int, error) {
	total := 0
	for total < len(b) {
		n, err := l.pty.Write(b[total:])
		total += n
		if err != nil {
			return total, err
		}
		if n == 0 {
			return total, io.ErrShortWrite
		}
	}
	return total, nil
}

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

// Close hangs the child up and then makes sure it is gone.
func (l *local) Close() error {
	l.closeOnce.Do(func() {
		// Closing the pty sends the child a hangup. Killing it outright
		// first would deny a shell the chance to run its exit hooks.
		err := l.pty.Close()
		if l.detached && closeErrIsBenign(err) {
			// go-pty closes the slave this process already released.
			err = nil
		}
		l.closeErr = err

		// A child that ignores SIGHUP would outlive the window, holding
		// the terminal's file descriptors and, with tabs, leaking one
		// process per closed tab.
		select {
		case <-l.done:
		case <-time.After(hangupGrace):
			if p := l.cmd.Process; p != nil {
				_ = p.Kill()
			}
		}
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

// hasEnv reports whether env sets key. The comparison ignores case
// because Windows environment variables are case-insensitive, and
// os/exec keeps the last of a duplicated name there — so a caller's
// "Term=dumb" would otherwise be silently overridden by an appended
// "TERM=xterm-256color".
func hasEnv(env []string, key string) bool {
	for _, e := range env {
		name, _, ok := strings.Cut(e, "=")
		if ok && strings.EqualFold(name, key) {
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
