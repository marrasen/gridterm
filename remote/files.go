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
		return nil, errors.Join(err, closeSession(sess))
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
	out, err := sess.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("remote: open the read pipe from %s: %w", host, err)
	}
	in, err := sess.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("remote: open the write pipe to %s: %w", host, err)
	}
	if err := doWithin(ctx, "ask "+host+" for the sftp subsystem", func() error {
		return sess.RequestSubsystem("sftp")
	}); err != nil {
		return nil, err
	}
	return openWithin(ctx, "start SFTP on "+host, func() (*sftp.Client, error) {
		return sftp.NewClientPipe(out, in)
	})
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
func (f *Files) closeAll() error {
	done := make(chan error, 1)
	go func() { done <- f.client.Close() }()

	var errs []error
	select {
	case err := <-done:
		errs = append(errs, err)
	case <-time.After(drainGrace):
		errs = append(errs, closeSession(f.sess))
		// Now that the channel has gone, the client's own close can
		// finish. Waited for rather than abandoned: it holds a goroutine
		// until it does.
		errs = append(errs, <-done)
	}
	errs = append(errs, closeSession(f.sess))

	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("remote: close SFTP on %s: %w", f.conn, err)
	}
	return nil
}

// closeSession shuts a session's channel, ignoring the ways it says it
// was already shut.
func closeSession(sess *ssh.Session) error {
	err := sess.Close()
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
// What comes back is the stream and the SFTP client speaking over it.
// Both are the caller's to close.
func WindowFiles(ctx context.Context, win *serve.Window) (*serve.FileSession, *sftp.Client, error) {
	addr := win.Addr()
	// One deadline for the whole open, so a window that stalls on the
	// channel and again on the greeting cannot hold this one for twice
	// as long.
	ctx, cancel := context.WithTimeout(ctx, channelTimeout)
	defer cancel()

	ch, err := openWithin(ctx, "ask "+addr+" for its files", win.Files)
	if err != nil {
		return nil, nil, err
	}
	client, err := openWithin(ctx, "start SFTP on "+addr, func() (*sftp.Client, error) {
		return sftp.NewClientPipe(ch, ch)
	})
	if err != nil {
		// The channel is closed first, because closing it is what ends
		// the stream the far window's own account arrives on.
		closeErr := closeFileSession(ch)
		return nil, nil, errors.Join(withWhatItSaid(ctx, ch, err), closeErr)
	}
	return ch, client, nil
}

// withWhatItSaid puts the far window's account of a file session it
// could not start onto the failure, when it gave one.
//
// That account is the only one, because the failure happened over there.
// Waiting for it is bounded like every other wait on this goroutine, and
// a window given up on for saying nothing is not asked at all.
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
	}
	return err
}

// closeFileSession shuts a file session's channel, ignoring the ways it
// says it was already shut.
func closeFileSession(ch *serve.FileSession) error {
	err := ch.Close()
	if err == nil || errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
		return nil
	}
	return err
}
