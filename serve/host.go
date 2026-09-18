package serve

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"

	"golang.org/x/crypto/ssh"

	"github.com/marrasen/gridterm/session"
)

// The biggest pane a client may ask for. A size is four bytes on the
// wire and arrives from the other end, so it is clamped here rather
// than trusted: the machine that sent it is not the one that has to
// make a terminal that size.
const mostCells = 10000

// How many file sessions one connection may have at once.
//
// A file session is not a channel and a buffer: an SFTP server is a
// dozen goroutines and as many open files as it is asked for, none of
// which shows anywhere in the window being served. A browser uses one
// per pane, so this is far more than anybody opens and little enough
// that a client cannot quietly make this machine run out of handles.
//
// It counts the sessions being served right now. A relay left parked on a
// machine that stopped answering has already ended its session and is not
// counted, so this is no bound on those.
const mostFileSessions = 16

// serveChannels answers what a client opens on its connection.
//
// Everything but a session is refused by name, so an ordinary SSH
// client that finds this port is told what is wrong rather than handed
// a shell it did not ask for.
//
// ctx ends when the connection has finished. It returns once every
// session it started has been hung up on.
func (s *Server) serveChannels(ctx context.Context, chans <-chan ssh.NewChannel) {
	var running sync.WaitGroup
	// Counted rather than held in a list: nothing needs to name them,
	// only to know how many there are. Written by the goroutines that
	// finish and read by this one.
	var files atomic.Int32
	for nch := range chans {
		if nch.ChannelType() == chanControl {
			ch, reqs, err := nch.Accept()
			if err != nil {
				s.onError(fmt.Errorf("serve: take a watcher: %w", err))
				continue
			}
			running.Add(1)
			go func() {
				defer running.Done()
				s.runControl(ctx, ch, reqs)
			}()
			continue
		}
		if nch.ChannelType() == chanFiles {
			var want openFiles
			// No payload asks for the machine being served, which is
			// what a client of an older build sends.
			if len(nch.ExtraData()) > 0 {
				if err := ssh.Unmarshal(nch.ExtraData(), &want); err != nil {
					// The same shape trouble a session request has: the
					// encoding is positional, so this is a gridterm of
					// another build.
					_ = nch.Reject(ssh.ConnectionFailed,
						"that is not a file session request this gridterm understands: "+
							"both windows have to be the same build")
					continue
				}
			}
			if files.Load() >= mostFileSessions {
				_ = nch.Reject(ssh.ResourceShortage,
					"this gridterm is already serving as many file sessions"+
						" as it will on one connection")
				continue
			}
			files.Add(1)
			running.Add(1)
			go func() {
				defer running.Done()
				defer files.Add(-1)
				s.runFiles(ctx, nch, want.Host)
			}()
			continue
		}
		if nch.ChannelType() != SessionChannel {
			_ = nch.Reject(ssh.UnknownChannelType,
				"this is gridterm, and it serves "+SessionChannel)
			continue
		}
		var want openSession
		if err := ssh.Unmarshal(nch.ExtraData(), &want); err != nil {
			// The shape is fixed and positional, with no room for a
			// field one end knows and the other does not, so this is
			// what a gridterm of another build looks like from here.
			_ = nch.Reject(ssh.ConnectionFailed,
				"that is not a session request this gridterm understands: "+
					"both windows have to be the same build")
			continue
		}
		ch, reqs, err := nch.Accept()
		if err != nil {
			// The connection has gone. Whatever is left in chans goes
			// with it, so there is nothing to do but say so.
			s.onError(fmt.Errorf("serve: take a session: %w", err))
			continue
		}
		running.Add(1)
		go func() {
			defer running.Done()
			s.runSession(ctx, ch, reqs, want)
		}()
	}
	// The connection is finished. Waiting here is what keeps the window
	// from saying the client has gone while a shell it started is still
	// being hung up on.
	running.Wait()
}

// runFiles gives a client the files of a machine and carries the
// session until one end or the other is done.
//
// host is the machine the client asked for, empty for the machine being
// served. ctx ends when the client's connection has finished, and is
// handed to the Filer so it can stop waiting on anything the client was
// the only reader of.
func (s *Server) runFiles(ctx context.Context, nch ssh.NewChannel, host string) {
	if s.cfg.Files == nil {
		_ = nch.Reject(ssh.Prohibited, "this gridterm does not serve its files")
		return
	}
	ch, reqs, err := nch.Accept()
	if err != nil {
		// The connection has gone, so there is nothing to do but say
		// so.
		s.onError(fmt.Errorf("serve: take a file session: %w", err))
		return
	}
	// Drained rather than answered: nothing is asked down this channel,
	// and sixteen unread requests stop the connection's read loop.
	go ssh.DiscardRequests(reqs)

	if err := s.cfg.Files(ctx, host, ch); err != nil {
		// Said over there as well as here: the client is left holding
		// a stream that stopped, and this is the only account of why.
		if _, werr := io.WriteString(ch.Stderr(), "gridterm: "+err.Error()+"\r\n"); werr != nil {
			s.onError(fmt.Errorf("serve: say why a file session stopped: %w", werr))
		}
		s.onError(fmt.Errorf("serve: carry a file session: %w", err))
	}
	if err := ch.Close(); err != nil && !errors.Is(err, io.EOF) {
		s.onError(fmt.Errorf("serve: close a file session: %w", err))
	}
}

// Filer gives a client the files of a machine the window can reach.
//
// host is the machine the client asked for, as the window names it on
// its own panel. Empty is the machine the window is on, which is what a
// client that asked for nothing in particular means.
//
// It is handed a channel and returns when it is finished with it,
// whether because the client went or because it failed. What runs on
// the channel is the window's business.
//
// The channel is not the Filer's to close: it is closed once the Filer
// returns, and whatever the Filer returns is sent to the client on the
// channel's error stream first. A Filer that closes it as well would
// have its own close reported as a failure.
//
// It must return when the client goes, which a channel that has gone
// shows as an end of file on every read. One that waits on anything
// else holds the whole connection open: this window does not say a
// client has left until every session it started has finished.
//
// ctx ends when the client's connection has finished. An end of file on
// the channel does not say which of the two happened, so a Filer that
// would wait for something on the client's behalf asks this instead.
//
// It is called from a goroutine of the server's, one per session a
// client opens.
type Filer func(ctx context.Context, host string, ch io.ReadWriteCloser) error

// runSession starts something for the client to work in and carries it
// until one end or the other is done.
func (s *Server) runSession(ctx context.Context, ch ssh.Channel,
	reqs <-chan *ssh.Request, want openSession) {

	// Clamped here rather than where it was sent from. Nothing stops a
	// client asking for a pane of four billion cells, and this is the
	// end that has to make a terminal that size.
	cols := min(max(int(want.Cols), 1), mostCells)
	rows := min(max(int(want.Rows), 1), mostCells)

	var (
		sess session.Session
		err  error
	)
	switch {
	case want.Attach != "":
		if s.cfg.Attach == nil {
			s.refuseSession(ch, reqs,
				errors.New("this gridterm cannot be worked in from elsewhere"))
			return
		}
		sess, err = s.cfg.Attach(Attached{
			ID:   want.Attach,
			Host: want.AttachHost,
			Kind: want.AttachKind,
		}, cols, rows)
	case s.cfg.Open == nil:
		s.refuseSession(ch, reqs, errors.New("this gridterm has nothing to open"))
		return
	default:
		sess, err = s.cfg.Open(cols, rows)
	}
	if err != nil {
		s.refuseSession(ch, reqs, err)
		return
	}

	// The session goes when the connection does, whether or not either
	// copy below notices. A program that has stopped reading its input
	// leaves the copy into it parked in a write, and the copy out of it
	// parked in a read: without this, a client that vanished would
	// leave both, the program and its pseudo-terminal behind for good.
	closed := make(chan struct{})
	defer close(closed)
	go func() {
		select {
		case <-ctx.Done():
			if err := sess.Close(); err != nil {
				s.onError(fmt.Errorf("serve: close a session: %w", err))
			}
		case <-closed:
		}
	}()

	// What the client says about the session while it runs, which is
	// that its pane changed size.
	go func() {
		for req := range reqs {
			ok := false
			if req.Type == reqWindowChange {
				var size windowChange
				if err := ssh.Unmarshal(req.Payload, &size); err == nil {
					ok = sess.Resize(
						min(max(int(size.Cols), 1), mostCells),
						min(max(int(size.Rows), 1), mostCells)) == nil
				}
			}
			if req.WantReply {
				_ = req.Reply(ok, nil)
			}
		}
	}()

	// What the client types, into the program.
	go func() {
		_, err := io.Copy(sess, ch)
		if err != nil && !errors.Is(err, io.EOF) {
			// Keystrokes that went nowhere. Nothing above can tell, so
			// it is said here or not at all.
			s.onError(fmt.Errorf("serve: carry what the client typed: %w", err))
		}
		// The client is finished typing. The program is told the same
		// way a local one is told when its terminal closes.
		if err := sess.Close(); err != nil {
			s.onError(fmt.Errorf("serve: close a session: %w", err))
		}
	}()

	// What the program writes, out to the client.
	_, copyErr := io.Copy(ch, sess)
	if errors.Is(copyErr, io.EOF) {
		copyErr = nil
	}
	// Closed before it is waited for. The copy above ends when the
	// client goes as well as when the program does, and waiting first
	// would wait for a program nothing has hung up on yet.
	closeErr := sess.Close()
	waitErr := sess.Wait()

	// A session whose output stopped reaching the client is not a
	// program that ran and finished, whatever the program thinks: the
	// client is holding half a screen and the only end that knows is
	// this one.
	s.endSession(ch, errors.Join(waitErr, copyErr))
	if closeErr != nil {
		s.onError(fmt.Errorf("serve: close a session: %w", closeErr))
	}
	if copyErr != nil {
		s.onError(fmt.Errorf("serve: carry a session: %w", copyErr))
	}
}

// refuseSession tells the client this window could not start anything
// and takes the channel down.
//
// The reason goes on the channel's error stream rather than into the
// bytes of the program, because there is no program: sent the other
// way, a client could not tell this window's words from the output of
// whatever it thought it had started.
func (s *Server) refuseSession(ch ssh.Channel, reqs <-chan *ssh.Request, why error) {
	// Drained even though nothing is answered. The requests a client
	// has already sent arrive down the connection's one read loop, and
	// sixteen unread ones stop it: every other channel on that
	// connection would wait for ever.
	go ssh.DiscardRequests(reqs)

	if _, err := io.WriteString(ch.Stderr(), "gridterm: "+why.Error()+"\r\n"); err != nil {
		s.onError(fmt.Errorf("serve: say why a session could not start: %w", err))
	}
	// A failure, not a program that ended cleanly.
	s.endSession(ch, why)
	s.onError(fmt.Errorf("serve: open a session: %w", why))
}

// endSession says how a session ended and closes the channel.
//
// The status is sent before the channel goes. A client that only saw
// the channel close could not tell a program that finished from a
// connection that dropped, and those are different things to show
// somebody. Whether it ended well is all that crosses: why it did not
// stays on the machine it happened on.
func (s *Server) endSession(ch ssh.Channel, why error) {
	status := uint32(0)
	if why != nil {
		status = 1
	}
	_, err := ch.SendRequest(reqExitStatus, false, ssh.Marshal(exitStatus{Status: status}))
	if err != nil && !errors.Is(err, io.EOF) {
		// The client will read this as a connection that dropped, which
		// is nearly true and is the safer of the two readings.
		s.onError(fmt.Errorf("serve: say how a session ended: %w", err))
	}
	// The close is the last thing the client hears, so one that failed
	// is the only account of a channel that did not end cleanly. A
	// connection that has already gone is not one of those, and the
	// second close reports nothing when the first said so: they are one
	// failure, not two.
	if err := ch.CloseWrite(); err != nil && !Ended(err) {
		s.onError(fmt.Errorf("serve: end a session: %w", err))
		_ = ch.Close()
		return
	}
	if err := ch.Close(); err != nil && !Ended(err) {
		s.onError(fmt.Errorf("serve: close a session: %w", err))
	}
}

// Attached names something a client asked to work in, as the client was
// told about it.
type Attached struct {
	// ID is the ID of the Open that named it.
	ID string

	// Host and Kind are the machine and the sort of thing that Open said
	// it was. Not the label, which a shell changes every time it sets
	// its title.
	Host, Kind string
}

// Attacher gives a client what is already running in one of this
// window's panes, named by the ID it was sent down the control channel
// and by the machine and kind that ID was said to be.
//
// All three, because an ID is a place in a list that is built afresh: an
// implementation checks what it finds against what the client was told
// it was asking for, and refuses rather than hand over something else.
// That is integrity, not authorisation -- the key the client signed with
// is what says it may be here at all.
//
// What comes back is a session like any other: reading it gives what
// the pane shows, starting with the screen as it stands, and writing to
// it types into the program. Closing it stops watching; the pane goes
// on running here, which is what lets the screen be right again when
// the client leaves.
//
// It is called from a goroutine of the server's, so an implementation
// that reaches into the window has to hand the work to whatever draws.
// cols and rows are how big the watcher's pane is. The window may
// give the pane that size, or may keep its own and let the watcher see
// a screen of another size; either way the watcher has said.
type Attacher func(want Attached, cols, rows int) (session.Session, error)

// Opener starts something for a client to work in.
//
// It is called from a goroutine of the server's, one per session a
// client opens, so an implementation that touches the window has to
// hand the work to whatever draws.
type Opener func(cols, rows int) (session.Session, error)
