package remote

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"

	"github.com/marrasen/gridterm/serve"
)

// ErrCloseAbandoned says an SFTP client's own close never came back
// after its session was closed, so it was left for the connection to
// end.
var ErrCloseAbandoned = errors.New(
	"the file session's close never came back, so it was left to the connection")

// Files is an SFTP session on a connection.
//
// It rides on the connection: closing the connection closes it, and the
// file browser and every transfer going through it stop.
type Files struct {
	conn *Conn

	// sess is the channel the session runs on. It is kept because
	// closing the SFTP client only sends an end of file and then waits
	// for the far end to close the channel; a machine that has stopped
	// answering never does, and this is what ends it.
	sess   *ssh.Session
	client *sftp.Client

	closeOnce sync.Once
	closeErr  error
}

// Files starts an SFTP session on the connection.
//
// One per call rather than one shared: each has its own channel, and two
// browser panes on one machine should not wait on each other's reads.
//
// Starting it is bounded by channelTimeout, all three round trips
// together, because it runs on the goroutine that draws. A caller that
// cancels ctx ends it sooner. Neither touches a session that was
// started.
func (c *Conn) Files(ctx context.Context) (*Files, error) {
	// Asked before a channel is opened, so a closed connection says so
	// rather than reporting whatever the dead transport failed with.
	if c.isClosing() {
		return nil, fmt.Errorf("remote: %s: %w", c, ErrClosed)
	}
	// One deadline for the whole open. The session, the subsystem and the
	// SFTP greeting are three round trips, and a deadline each would let a
	// machine that stalls on every one of them hold the window for three
	// times as long.
	ctx, cancel := context.WithTimeout(ctx, channelTimeout)
	defer cancel()

	sess, err := openWithin(ctx, "open a session on "+c.String(), c.client.NewSession)
	if err != nil {
		return nil, err
	}
	// The session is ours to close from here on, including on every
	// failure below: sftp's own NewClient leaves one behind when the
	// machine turns the subsystem down.
	client, err := startSFTP(ctx, sess, c.String())
	if err != nil {
		return nil, errors.Join(err, closeQuietly(sess))
	}

	f := &Files{conn: c, sess: sess, client: client}
	if err := c.register(f); err != nil {
		return nil, errors.Join(err, f.closeRider())
	}
	return f, nil
}

// startSFTP asks for the subsystem and speaks SFTP over the session,
// both within what is left of the caller's deadline.
func startSFTP(ctx context.Context, sess *ssh.Session, host string) (*sftp.Client, error) {
	out, in, err := sftpPipes(ctx, sess, host)
	if err != nil {
		return nil, err
	}
	return openWithin(ctx, "start SFTP on "+host, func() (*sftp.Client, error) {
		return sftp.NewClientPipe(out, in)
	})
}

// sftpPipes asks the machine for the SFTP subsystem and gives back the
// session's two streams, within what is left of the caller's deadline.
func sftpPipes(ctx context.Context, sess *ssh.Session, host string) (
	io.Reader, io.WriteCloser, error) {

	out, err := sess.StdoutPipe()
	if err != nil {
		return nil, nil, fmt.Errorf("remote: open the read pipe from %s: %w", host, err)
	}
	in, err := sess.StdinPipe()
	if err != nil {
		return nil, nil, fmt.Errorf("remote: open the write pipe to %s: %w", host, err)
	}
	if err := doWithin(ctx, "ask "+host+" for the sftp subsystem", func() error {
		return sess.RequestSubsystem("sftp")
	}); err != nil {
		return nil, nil, err
	}
	return out, in, nil
}

// FileRelay is a raw SFTP subsystem on a connection, for carrying
// somebody else's file session rather than reading files here.
//
// It rides on the connection: closing the connection closes it, so a
// machine that goes ends the relay going through it.
type FileRelay struct {
	conn *Conn
	sess *ssh.Session
	out  io.Reader
	in   io.WriteCloser

	closeOnce sync.Once
	closeErr  error
}

// FileSubsystem starts an SFTP subsystem on the connection and hands
// back the stream it speaks over.
//
// No SFTP client is made: the caller relays the bytes rather than
// reading files itself. Starting it is bounded by channelTimeout, both
// round trips together, the way Conn.Files is. A caller that cancels ctx
// ends it sooner.
func (c *Conn) FileSubsystem(ctx context.Context) (*FileRelay, error) {
	// Asked before a channel is opened, so a closed connection says so
	// rather than reporting whatever the dead transport failed with.
	if c.isClosing() {
		return nil, fmt.Errorf("remote: %s: %w", c, ErrClosed)
	}
	ctx, cancel := context.WithTimeout(ctx, channelTimeout)
	defer cancel()

	sess, err := openWithin(ctx, "open a session on "+c.String(), c.client.NewSession)
	if err != nil {
		return nil, err
	}
	// The session is ours to close from here on, including on every
	// failure below.
	out, in, err := sftpPipes(ctx, sess, c.String())
	if err != nil {
		return nil, errors.Join(err, closeQuietly(sess))
	}

	f := &FileRelay{conn: c, sess: sess, out: out, in: in}
	if err := c.register(f); err != nil {
		return nil, errors.Join(err, f.closeRider())
	}
	return f, nil
}

// Read gives what the machine has said.
func (f *FileRelay) Read(p []byte) (int, error) { return f.out.Read(p) }

// Write sends bytes to the machine.
func (f *FileRelay) Write(p []byte) (int, error) { return f.in.Write(p) }

// On returns the connection it runs over.
func (f *FileRelay) On() *Conn { return f.conn }

// Close ends the subsystem and lets go of the connection's record of it.
//
// It sends the machine a channel close and nothing more. A read already
// waiting on the channel ends when that machine answers the close, or
// when the connection to it goes; a machine that has stopped answering
// leaves that read where it is, so a caller has to be able to walk away
// from it.
func (f *FileRelay) Close() error {
	err := f.closeRider()
	f.conn.drop(f)
	return err
}

// closeRider is Close without the deregistering, for a connection that
// is closing its riders and will throw the whole record away anyway.
func (f *FileRelay) closeRider() error {
	f.closeOnce.Do(func() {
		if err := closeQuietly(f.sess); err != nil {
			f.closeErr = fmt.Errorf("remote: close a file relay on %s: %w", f.conn, err)
		}
	})
	return f.closeErr
}

// Client is the SFTP client itself, for whatever works on files.
func (f *Files) Client() *sftp.Client { return f.client }

// On returns the connection it runs over.
func (f *Files) On() *Conn { return f.conn }

// Close ends the session and lets go of the connection's record of it.
func (f *Files) Close() error {
	err := f.closeRider()
	f.conn.drop(f)
	return err
}

// closeRider is Close without the deregistering, for a connection that
// is closing its riders and will throw the whole record away anyway.
func (f *Files) closeRider() error {
	f.closeOnce.Do(func() { f.closeErr = f.closeAll() })
	return f.closeErr
}

// closeAll ends the session, giving the far end a moment to finish
// first.
//
// Closing the SFTP client sends an end of file and then waits for the
// far end to close the channel. A machine that has stopped answering
// never will, and this runs on the goroutine that draws, so the wait is
// bounded the same way a shell's is: after that the channel is closed
// from here, which is what lets go.
//
// Closing the channel only sends a message, so a transport that is dead
// both ways leaves the client's close waiting for an answer that cannot
// arrive. It gets one more grace and is then abandoned, which says so
// with ErrCloseAbandoned: that goroutine ends when the connection is
// closed, which Conn.Close does as soon as its riders are done.
func (f *Files) closeAll() error {
	done := make(chan error, 1)
	go func() { done <- f.client.Close() }()

	var errs []error
	select {
	case err := <-done:
		errs = append(errs, err)
	case <-time.After(drainGrace):
		errs = append(errs, closeQuietly(f.sess))
		// Now that the channel has gone, the client's own close can
		// finish, unless the transport is dead and nothing wakes the
		// read it is parked on.
		select {
		case err := <-done:
			errs = append(errs, err)
		case <-time.After(drainGrace):
			errs = append(errs, ErrCloseAbandoned)
		}
	}
	errs = append(errs, closeQuietly(f.sess))

	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("remote: close SFTP on %s: %w", f.conn, err)
	}
	return nil
}

// closeQuietly shuts a session or a channel, ignoring the ways it says
// it was already shut.
func closeQuietly[T io.Closer](c T) error {
	err := c.Close()
	if err == nil || errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
		return nil
	}
	return err
}

// WindowFiles starts SFTP on a window taken over, over a channel of its
// own.
//
// Bounded by channelTimeout, the channel and the SFTP greeting together,
// for the reason Conn.Files is bounded: it runs on the goroutine that
// draws, and a window that is still on the network and no longer
// answering would otherwise stop this one. A caller that cancels ctx
// gives up sooner. A channel that arrives after that is closed rather
// than left open on the far window.
//
// name is the window as the caller knows it, which is what the error has
// to say. What comes back is the stream and the SFTP client speaking
// over it, both the caller's to close.
func WindowFiles(ctx context.Context, name string, win *serve.Window) (
	*serve.FileSession, *sftp.Client, error) {

	return WindowFilesOn(ctx, name, "", win)
}

// WindowFilesOn starts SFTP on a machine a window taken over is
// connected to, over a channel of its own.
//
// host is that machine as the window taken over names it, and the bytes
// go through that window: this one has no connection of its own to it.
// An empty host asks for the machine the window itself is on, which is
// what WindowFiles does. Bounded the way WindowFiles is.
func WindowFilesOn(ctx context.Context, name, host string, win *serve.Window) (
	*serve.FileSession, *sftp.Client, error) {

	ctx, cancel := context.WithTimeout(ctx, channelTimeout)
	defer cancel()

	where := name
	if host != "" {
		where = host + " through " + name
	}
	ch, err := openWithin(ctx, "open a file session on "+where,
		func() (*serve.FileSession, error) { return win.FilesOn(host) })
	if err != nil {
		return nil, nil, err
	}
	client, err := openWithin(ctx, "start SFTP on "+where, func() (*sftp.Client, error) {
		return sftp.NewClientPipe(ch, ch)
	})
	if err != nil {
		// The channel is closed first, because closing it is what ends
		// the stream the far window's own account arrives on.
		closeErr := closeQuietly(ch)
		return nil, nil, errors.Join(withWhatItSaid(ctx, ch, err), closeErr)
	}
	return ch, client, nil
}

// withWhatItSaid puts the far window's account of a file session it
// could not start onto the failure, when it gave one.
//
// That account is the only one, because the failure happened over there.
// It is waited for briefly, and no longer than the open's own deadline:
// once the context has ended it is not asked.
func withWhatItSaid(ctx context.Context, ch *serve.FileSession, err error) error {
	if ctx.Err() != nil {
		return err
	}
	said := make(chan string, 1)
	go func() { said <- ch.Said() }()
	select {
	case why := <-said:
		if why != "" {
			return fmt.Errorf("%w: it said: %s", err, why)
		}
	case <-time.After(drainGrace):
	case <-ctx.Done():
	}
	return err
}
