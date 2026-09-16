package remote

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/marrasen/gridterm/serve"
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
// else. Matched on serve's own constant, so renaming the channel is a
// change this stops compiling over rather than one it stops noticing.
func isGridterm(err error) bool {
	var refused *ssh.OpenChannelError
	if !errors.As(err, &refused) {
		return false
	}
	return refused.Reason == ssh.UnknownChannelType &&
		strings.Contains(refused.Message, serve.SessionChannel)
}

// notAMachine says the far end is a gridterm window saved as a machine,
// and what to change about it.
//
// Said plainly, because what the user has to change is one field in the
// dialog and the refusal from the other end does not say which. The
// refusal goes in unwrapped, because the sentence around it names the
// machine a second time.
func notAMachine(addr string, refused error) error {
	inner := errors.Unwrap(refused)
	if inner == nil {
		inner = refused
	}
	return fmt.Errorf(
		"remote: %s is a gridterm window, not a machine to log in to."+
			" Set its Kind to %q and take it over instead: %w",
		addr, GridtermWindowKind, inner)
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

	// writeMu guards the fields below, never the write itself.
	writeMu sync.Mutex
	stdin   io.WriteCloser

	// writing counts the writes on the channel right now, and hungUp
	// records that Close has been through. Closing the session stdin
	// writes the channel's sentEOF flag, which Write reads, so the two
	// must not overlap.
	writing int
	hungUp  bool

	// quiet is closed by the write that brings writing to zero once
	// hungUp is set, so Close can tell a keystroke that is about to land
	// from a write wedged on a program that stopped reading.
	quiet chan struct{}

	// wanted is the size Resize was last given and sent is the size last
	// put on the wire, so a drag that outruns the connection collapses
	// to the last size. resizing records that a goroutine is sending
	// them, and it counts in writing while it runs.
	wanted   [2]int
	sent     [2]int
	resizing bool

	// resizeErr keeps a window-change that failed, for the next Resize
	// or Close to return. lateErr takes it instead when it is set.
	resizeErr error
	lateErr   func(error)

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
//
// Opening it is bounded by channelTimeout, all three round trips
// together, because it runs on the goroutine that draws. A caller that
// cancels ctx ends it sooner. Neither touches a shell that was opened:
// the program then runs until it exits or the shell is closed.
func (c *Conn) Shell(ctx context.Context, cfg ShellConfig) (*Shell, error) {
	// Asked before a channel is opened, so a closed connection says so
	// rather than reporting whatever the dead transport failed with.
	if c.isClosing() {
		return nil, fmt.Errorf("remote: %s: %w", c, ErrClosed)
	}
	// One deadline for the whole open. The session, the pty and the
	// program are three round trips, and a deadline each would let a
	// machine that stalls on every one of them hold the window for three
	// times as long.
	ctx, cancel := context.WithTimeout(ctx, channelTimeout)
	defer cancel()

	sess, err := openWithin(ctx, "open a session on "+c.String(), c.client.NewSession)
	if err != nil {
		if isGridterm(err) {
			return nil, notAMachine(c.addr, err)
		}
		return nil, err
	}

	s := &Shell{conn: c, sess: sess, done: make(chan struct{})}
	// Started before it is registered: until the connection knows about
	// it, no other goroutine can reach it, so nothing can read the
	// fields start is still filling in.
	if err := s.start(ctx, cfg); err != nil {
		// A pty or a program that was never answered leaves the session
		// open on the machine, and whatever the abandoned request still
		// writes needs somewhere to go.
		if s.outW != nil {
			_ = s.outW.CloseWithError(io.EOF)
		}
		return nil, errors.Join(err, closeSession(sess))
	}
	if err := c.register(s); err != nil {
		_ = s.closeRider()
		return nil, err
	}
	return s, nil
}

// start requests the pty and runs the program, both within ctx.
func (s *Shell) start(ctx context.Context, cfg ShellConfig) error {
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
	if err := doWithin(ctx, "ask "+s.conn.String()+" for a pty", func() error {
		return s.sess.RequestPty(term, rows, cols, ssh.TerminalModes{
			ssh.ECHO:          1,
			ssh.TTY_OP_ISPEED: 38400,
			ssh.TTY_OP_OSPEED: 38400,
		})
	}); err != nil {
		return err
	}
	// The pty carried the first size, so a Resize to that size has
	// nothing to send.
	s.wanted, s.sent = [2]int{cols, rows}, [2]int{cols, rows}

	run := s.sess.Shell
	if len(cfg.Command) > 0 {
		run = func() error { return s.sess.Start(shellQuote(cfg.Command)) }
	}
	if err := doWithin(ctx, "start the program on "+s.conn.String(), run); err != nil {
		return err
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

// Write sends input to the remote program. One writer at a time; the
// caller serialises.
//
// It blocks until the far end has room, for however long that takes: a
// program that has stopped reading its input is not a failure.
func (s *Shell) Write(b []byte) (int, error) {
	s.writeMu.Lock()
	if s.stdin == nil || s.hungUp {
		s.writeMu.Unlock()
		return 0, io.ErrClosedPipe
	}
	in := s.stdin
	s.writing++
	s.writeMu.Unlock()

	n, err := in.Write(b)

	s.writeMu.Lock()
	s.doneWritingLocked()
	s.writeMu.Unlock()
	return n, err
}

// doneWritingLocked counts one write out and tells Close when the
// channel has gone idle. writeMu is held.
func (s *Shell) doneWritingLocked() {
	s.writing--
	if s.writing == 0 && s.quiet != nil {
		close(s.quiet)
		s.quiet = nil
	}
}

// Resize reports a new window size in character cells. It records the
// size and comes straight back, so a drag never waits on the machine;
// sendSize puts it on the wire.
//
// A window-change that failed is reported one request late, because
// x/crypto hands a write that failed to whoever writes next: the next
// Resize returns it, Close returns what no Resize came back for, and a
// ReportLate hook takes it instead when one is set. A shell that has
// been closed says so, as Write does.
func (s *Shell) Resize(cols, rows int) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	failed := s.resizeErr
	s.resizeErr = nil
	if s.hungUp {
		return errors.Join(failed, io.ErrClosedPipe)
	}
	if cols <= 0 || rows <= 0 {
		return failed
	}
	s.wanted = [2]int{cols, rows}
	if !s.resizing && s.wanted != s.sent {
		s.resizing = true
		// Counted as a write, so Close treats a send parked on a full
		// send buffer the way it treats a parked keystroke.
		s.writing++
		go s.sendSize()
	}
	return failed
}

// ReportLate takes a function for a failure that no caller can be given
// back, which is what a window-change failing amounts to: the send
// outlives the Resize that asked for it and may outlive Close.
//
// It is called from the goroutine that was sending, so an
// implementation that touches a window has to hand the work on. Set it
// before the shell is resized.
func (s *Shell) ReportLate(report func(error)) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	s.lateErr = report
}

// sendSize sends the size last asked for, and again for any size asked
// for while it was sending, until the size stops changing or a send
// fails.
//
// A send that has parked on a full send buffer lets go only when the
// connection is closed: closing the session queues behind the same
// channel lock, so Close counts this as a parked write and leaves it
// behind rather than waiting for it.
func (s *Shell) sendSize() {
	for {
		s.writeMu.Lock()
		want := s.wanted
		if s.hungUp || want == s.sent {
			s.resizing = false
			s.doneWritingLocked()
			s.writeMu.Unlock()
			return
		}
		s.writeMu.Unlock()

		err := s.sess.WindowChange(want[1], want[0])
		if err == nil {
			s.writeMu.Lock()
			s.sent = want
			s.writeMu.Unlock()
			continue
		}

		// A failure ends the send. The size stays unsent so a later drag
		// asks for it again, and a send that kept trying would spin on a
		// channel that has gone.
		if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
			err = nil
		} else {
			err = fmt.Errorf("remote: resize the terminal on %s: %w", s.conn, err)
		}
		s.writeMu.Lock()
		s.resizing = false
		s.doneWritingLocked()
		report := s.lateErr
		if err != nil && report == nil {
			s.resizeErr = errors.Join(s.resizeErr, err)
		}
		s.writeMu.Unlock()
		if err != nil && report != nil {
			report(err)
		}
		return
	}
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

// closeAll hangs the shell up, bounded at every step because it runs on
// the goroutine that draws.
//
// A resize in flight counts as a parked write, so a close during a drag
// can skip the polite end-of-file.
func (s *Shell) closeAll() error {
	// The pointer and the state, not the write, because this runs on the
	// goroutine that draws.
	s.writeMu.Lock()
	in, parked := s.stdin, s.writing > 0
	s.hungUp = true
	if parked {
		s.quiet = make(chan struct{})
	}
	quiet := s.quiet
	s.writeMu.Unlock()

	// A keystroke on the wire at this moment is not a wedged program, so
	// give it up to drainGrace to land rather than deciding from the
	// snapshot above. No Write can start once hungUp is set, so when
	// this comes back the channel is idle and stays idle.
	if parked {
		select {
		case <-quiet:
			parked = false
		case <-time.After(drainGrace):
		}
	}

	// Closing stdin is the polite hangup: a shell reading its input sees
	// end-of-file and exits, running its own exit hooks on the way. It is
	// skipped while a write is still parked, because closing stdin
	// underneath a write is the race writeMu exists to prevent. Closing
	// the session below hangs the program up anyway, without the
	// courtesy.
	//
	// On a goroutine, because the end-of-file is a packet like any other
	// and waits when the send buffer to the machine has filled. An idle
	// channel does not mean a wire that takes bytes: the buffer fills for
	// the whole connection, and a goodbye that has parked there lets go
	// only when the connection is closed.
	var errs []error
	saidGoodbye := false
	if in != nil && !parked {
		hung := make(chan error, 1)
		go func() { hung <- in.Close() }()
		select {
		case err := <-hung:
			if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, net.ErrClosed) {
				errs = append(errs, err)
			}
			saidGoodbye = true
		case <-time.After(drainGrace):
		}
	}

	// Give the remote a moment to finish on its own, so output already
	// sent is not thrown away. Only when the program was started and
	// heard the hangup: nothing else ever closes done, and a program
	// that was never told to finish never will.
	if s.running && saidGoodbye {
		select {
		case <-s.done:
		case <-time.After(drainGrace):
		}
	}
	if s.outW != nil {
		_ = s.outW.CloseWithError(io.EOF)
	}

	// On a goroutine, because closing a session takes the channel's write
	// lock and a write holds that while the transport is busy with a key
	// exchange or a send buffer that has filled. Closing the session does
	// not free a request that has parked there: it queues behind the same
	// lock. So this comes back within the grace and says the machine did
	// not answer, and whatever is parked -- a send, this helper -- goes
	// when the connection does.
	shut := make(chan error, 1)
	go func() { shut <- s.sess.Close() }()
	select {
	case err := <-shut:
		if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, net.ErrClosed) {
			errs = append(errs, err)
		}
	case <-time.After(drainGrace):
		errs = append(errs, fmt.Errorf("remote: close the session on %s: %w within %s",
			s.conn, ErrNoAnswer, drainGrace))
	}

	// A window-change that failed and that no Resize came back for. The
	// send outlived its caller, so this is the last place to report it.
	s.writeMu.Lock()
	failed := s.resizeErr
	s.resizeErr = nil
	s.writeMu.Unlock()
	return errors.Join(append(errs, failed)...)
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
	s, err := conn.Shell(ctx, sh)
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
