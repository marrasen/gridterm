package remote

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/marrasen/gridterm/session"
)

// hangupGrace is how long Close waits for the remote to finish after its
// stdin is closed, so output already on the wire can still be read. It
// matches what the local pty path takes care to preserve.
const hangupGrace = 250 * time.Millisecond

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

	// done is closed once the session has ended and the output pipe has
	// been closed behind it.
	done chan struct{}

	closeOnce sync.Once
	closeErr  error
	waitOnce  sync.Once
	waitErr   error
}

// Shell starts a program on the connection and returns it as a byte
// stream. Several may run on one connection at a time.
func (c *Conn) Shell(cfg ShellConfig) (*Shell, error) {
	sess, err := c.client.NewSession()
	if err != nil {
		return nil, fmt.Errorf("remote: open session: %w", err)
	}

	s := &Shell{conn: c, sess: sess, done: make(chan struct{})}
	// Registered before anything is started, so a connection closing at
	// this moment tears the half-built shell down rather than leaving it
	// running with nothing holding it.
	if err := c.add(s); err != nil {
		_ = sess.Close()
		return nil, err
	}
	if err := s.start(cfg); err != nil {
		_ = s.Close()
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
		return fmt.Errorf("remote: stdin: %w", err)
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
		return fmt.Errorf("remote: request pty: %w", err)
	}

	if len(cfg.Command) == 0 {
		err = s.sess.Shell()
	} else {
		err = s.sess.Start(shellQuote(cfg.Command))
	}
	if err != nil {
		return fmt.Errorf("remote: start: %w", err)
	}

	// Closing the pipe writer is what turns the remote's exit into an
	// io.EOF for the reader, so it happens exactly when the session ends
	// and not before.
	go func() {
		defer close(s.done)
		_ = s.outW.CloseWithError(sessionEnd(s.Wait()))
	}()
	return nil
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
	s.closeOnce.Do(func() {
		s.closeErr = s.closeAll()
		s.conn.drop(s)
	})
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

	// Give the session a moment to finish on its own. Tearing it down
	// immediately would discard whatever the remote had already sent,
	// which is the mistake the local pty path takes care to avoid.
	//
	// Only when something was started: a shell that failed in start has
	// nothing running, and nothing will ever close done.
	if s.outW != nil {
		select {
		case <-s.done:
		case <-time.After(hangupGrace):
		}
		_ = s.outW.CloseWithError(io.EOF)
	}

	if err := s.sess.Close(); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}

// StartShell opens a connection and runs one shell on it, and hands back
// a shell that closes the connection with it.
//
// It is the one-shot form, for a caller that wants a single remote shell
// and nothing else. A caller that wants more than one thing on a machine
// should Dial and keep the Conn.
func StartShell(cfg Config, sh ShellConfig) (session.Session, error) {
	conn, err := Dial(cfg)
	if err != nil {
		return nil, err
	}
	s, err := conn.Shell(sh)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	return &ownedShell{Shell: s, conn: conn}, nil
}

// ownedShell is a shell that carries its connection.
//
// The connection closes after the shell rather than the shell closing
// through the connection, so Conn.Close closing this shell as one of its
// riders does not come back round into Conn.Close again.
type ownedShell struct {
	*Shell
	conn *Conn
}

func (o *ownedShell) Close() error {
	err := o.Shell.Close()
	if cerr := o.conn.Close(); cerr != nil && err == nil {
		err = cerr
	}
	return err
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
		return errors.New("remote: connection closed by the remote host")
	}
	return err
}

// shellQuote joins argv into something the remote login shell will split
// back into the same words. SSH has no argv on the wire: the server
// hands the command string to the user's shell, so this assumes that
// shell is POSIX — cmd.exe ignores single quotes entirely.
func shellQuote(argv []string) string {
	const quote = '\''
	var sb strings.Builder
	for i, a := range argv {
		if i > 0 {
			sb.WriteByte(' ')
		}
		sb.WriteByte(quote)
		for _, c := range []byte(a) {
			if c == quote {
				// End the quote, emit an escaped quote, reopen.
				sb.WriteString("'\\''")
				continue
			}
			sb.WriteByte(c)
		}
		sb.WriteByte(quote)
	}
	return sb.String()
}
