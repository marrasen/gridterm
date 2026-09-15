package remote

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

// drainGrace is how long Close waits for the remote to finish after its
// stdin is closed, so output already on the wire can still be read.
const drainGrace = 250 * time.Millisecond

// GridtermWindowKind is what the server list calls an entry that is
// another gridterm serving, for the message that tells a user their
// machine is really one of those.
//
// A constant here rather than in the window, so the message and the
// dialog cannot drift apart.
const GridtermWindowKind = "gridterm window"

// isGridterm reports whether a refusal came from a gridterm serving.
//
// It says so in the refusal itself, which is the only thing that
// crosses: an SSH client is told a channel type is unknown and nothing
// else. Matched on the channel type it names rather than on the
// sentence around it.
func isGridterm(err error) bool {
	return err != nil && strings.Contains(err.Error(), "session@gridterm")
}

// ShellConfig describes one shell or command to run on a connection.
type ShellConfig struct {
	// Command is what to run. Empty asks for the user's login shell.
	Command []string

	// Cols and Rows are the initial window size.
	Cols, Rows int

	// Term is the TERM value sent to the remote. Defaults to
	// xterm-256color.
	Term string
}

// Shell is a program running on a connection, as a byte stream and a
// size. It satisfies session.Session, so nothing above it can tell a
// remote shell from a local one.
//
// No local pseudo-terminal is involved: the SSH session channel already
// carries the byte stream, and pty-req and window-change carry the size.
type Shell struct {
	conn *Conn
	sess *ssh.Session

	// writeMu keeps Write and Close off each other. Closing the session
	// stdin writes the channel's sentEOF flag, which Write reads, and
	// the window can close with a keystroke still in flight.
	writeMu sync.Mutex
	stdin   io.WriteCloser

	// out is fed by the session's stdout and stderr. It is unbuffered,
	// which is the flow control: the remote stops sending when the
	// terminal stops reading.
	out  *io.PipeReader
	outW *io.PipeWriter

	// running records that the goroutine which closes done was started.
	// A shell that failed before that has nothing to wait for.
	running bool
	done    chan struct{}

	closeOnce sync.Once
	closeErr  error
	waitOnce  sync.Once
	waitErr   error
}

// Shell starts a program on the connection and returns it as a byte
// stream. Several may run on one connection at a time.
func (c *Conn) Shell(cfg ShellConfig) (*Shell, error) {
	// Asked before a channel is opened, so a closed connection says so
	// rather than reporting whatever the dead transport failed with.
	if c.isClosing() {
		return nil, fmt.Errorf("remote: %s: %w", c, ErrClosed)
	}
	sess, err := c.client.NewSession()
	if err != nil {
		if isGridterm(err) {
			// The far end is gridterm serving, not a machine with a
			// shell. Said plainly, because what the user has to change
			// is one field in the dialog and the refusal from the other
			// end does not say which.
			return nil, fmt.Errorf(
				"remote: %s is a gridterm window, not a machine to log in to."+
					" Set its Kind to %q and take it over instead: %w",
				c.addr, GridtermWindowKind, err)
		}
		return nil, fmt.Errorf("remote: open a session on %s: %w", c, err)
	}

	s := &Shell{conn: c, sess: sess, done: make(chan struct{})}
	// Started before it is registered: until the connection knows about
	// it, no other goroutine can reach it, so nothing can read the
	// fields start is still filling in.
	if err := s.start(cfg); err != nil {
		_ = s.closeRider()
		return nil, err
	}
	if err := c.register(s); err != nil {
		_ = s.closeRider()
		return nil, err
	}
	return s, nil
}

// start requests the pty and runs the program.
func (s *Shell) start(cfg ShellConfig) error {
	s.out, s.outW = io.Pipe()
	// stdout and stderr both land in one stream, as they would on a
	// local pty. With a pty the remote merges them itself; wiring both
	// costs nothing and covers a server that behaves otherwise.
	s.sess.Stdout = s.outW
	s.sess.Stderr = s.outW

	var err error
	if s.stdin, err = s.sess.StdinPipe(); err != nil {
		return fmt.Errorf("remote: open the stdin pipe: %w", err)
	}

	term := cfg.Term
	if term == "" {
		term = "xterm-256color"
	}
	cols, rows := max(cfg.Cols, 1), max(cfg.Rows, 1)
	// Ask for the pty before starting anything, so the remote shell sees
	// the right size in its very first prompt.
	if err := s.sess.RequestPty(term, rows, cols, ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 38400,
		ssh.TTY_OP_OSPEED: 38400,
	}); err != nil {
		return fmt.Errorf("remote: request a pty: %w", err)
	}

	if len(cfg.Command) == 0 {
		err = s.sess.Shell()
	} else {
		err = s.sess.Start(shellQuote(cfg.Command))
	}
	if err != nil {
		return fmt.Errorf("remote: start the remote program: %w", err)
	}

	s.running = true
	go s.reap()
	return nil
}

// reap waits for the remote program and tidies up after it.
//
// Closing the pipe writer is what turns the remote's exit into an io.EOF
// for the reader, so it happens exactly when the session ends and not
// before. The shell closes itself afterwards, or a connection whose
// programs all exited would keep a record of every one of them.
func (s *Shell) reap() {
	end := sessionEnd(s.Wait())
	close(s.done)
	_ = s.outW.CloseWithError(end)
	_ = s.Close()
}

func (s *Shell) Read(b []byte) (int, error) { return s.out.Read(b) }

// Write sends input to the remote program.
func (s *Shell) Write(b []byte) (int, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if s.stdin == nil {
		return 0, io.ErrClosedPipe
	}
	return s.stdin.Write(b)
}

// Resize reports a new window size in character cells.
func (s *Shell) Resize(cols, rows int) error {
	if cols <= 0 || rows <= 0 {
		return nil
	}
	return s.sess.WindowChange(rows, cols)
}

// Wait blocks until the program exits and returns its error, if any. It
// is idempotent: every call returns the same result.
func (s *Shell) Wait() error {
	s.waitOnce.Do(func() { s.waitErr = s.sess.Wait() })
	return s.waitErr
}

// Close hangs the program up and lets go of the connection's record of
// it. The connection itself stays open: other shells and tunnels may
// still be riding on it.
func (s *Shell) Close() error {
	err := s.closeRider()
	s.conn.drop(s)
	return err
}

// closeRider is Close without the deregistering, for a connection that
// is closing its riders and will throw the whole record away anyway.
func (s *Shell) closeRider() error {
	s.closeOnce.Do(func() { s.closeErr = s.closeAll() })
	return s.closeErr
}

func (s *Shell) closeAll() error {
	// Closing stdin is the polite hangup: a shell reading its input sees
	// end-of-file and exits, running its own exit hooks on the way.
	s.writeMu.Lock()
	if s.stdin != nil {
		_ = s.stdin.Close()
	}
	s.writeMu.Unlock()

	// Give the remote a moment to finish on its own, so output already
	// sent is not thrown away. Only when the program was started:
	// nothing else ever closes done.
	if s.running {
		select {
		case <-s.done:
		case <-time.After(drainGrace):
		}
	}
	if s.outW != nil {
		_ = s.outW.CloseWithError(io.EOF)
	}

	if err := s.sess.Close(); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}

// StartShell opens a connection and runs one shell on it.
//
// It is the one-shot form, for a caller that wants a single remote shell
// and nothing else. A caller that wants more than one thing on a machine
// should Connect and keep the Conn.
func StartShell(ctx context.Context, cfg Config, sh ShellConfig) (*OwnedShell, error) {
	conn, err := Connect(ctx, cfg)
	if err != nil {
		return nil, err
	}
	s, err := conn.Shell(sh)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	return &OwnedShell{Shell: s, conn: conn}, nil
}

// OwnedShell is a shell that carries its own connection and closes it
// after the shell, so Conn.Close closing that shell as a rider does not
// recurse.
type OwnedShell struct {
	*Shell
	conn *Conn
}

// Conn returns the connection the shell is running on.
func (o *OwnedShell) Conn() *Conn { return o.conn }

// Close ends the shell and then the connection carrying it.
func (o *OwnedShell) Close() error {
	return errors.Join(o.Shell.Close(), o.conn.Close())
}

// sessionEnd turns the reason a session ended into what a reader should
// see.
//
// A remote shell exiting, with any status or on a signal, is an ordinary
// end of session and becomes io.EOF. A session that ends with no exit
// status at all is not: that is what a dropped connection looks like,
// and reporting it as a clean logout hides the difference between
// closing a window and losing the network.
func sessionEnd(err error) error {
	if err == nil {
		return io.EOF
	}
	var exit *ssh.ExitError
	if errors.As(err, &exit) {
		return io.EOF
	}
	var missing *ssh.ExitMissingError
	if errors.As(err, &missing) {
		return errors.New("remote: the host closed the connection")
	}
	return err
}

// shellQuote joins argv into something the remote login shell will split
// back into the same words. SSH has no argv on the wire: the server
// hands the command string to the user's shell, so this assumes that
// shell is POSIX — cmd.exe ignores single quotes entirely.
func shellQuote(argv []string) string {
	var sb strings.Builder
	for i, a := range argv {
		if i > 0 {
			sb.WriteByte(' ')
		}
		sb.WriteByte('\'')
		for _, c := range []byte(a) {
			if c == '\'' {
				// End the quote, emit an escaped quote, reopen.
				sb.WriteString(`'\''`)
				continue
			}
			sb.WriteByte(c)
		}
		sb.WriteByte('\'')
	}
	return sb.String()
}
