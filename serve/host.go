package serve

import (
	"errors"
	"fmt"
	"io"
	"sync"

	"golang.org/x/crypto/ssh"

	"github.com/marrasen/gridterm/session"
)

// The biggest pane a client may ask for. A size is four bytes on the
// wire and arrives from the other end, so it is clamped here rather
// than trusted: the machine that sent it is not the one that has to
// make a terminal that size.
const mostCells = 10000

// serveChannels answers what a client opens on its connection.
//
// Everything but a session is refused by name, so an ordinary SSH
// client that finds this port is told what is wrong rather than handed
// a shell it did not ask for.
//
// gone is closed when the connection has finished. It returns once
// every session it started has been hung up on.
func (s *Server) serveChannels(chans <-chan ssh.NewChannel, gone <-chan struct{}) {
	var running sync.WaitGroup
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
				s.runControl(ch, reqs, gone)
			}()
			continue
		}
		if nch.ChannelType() != chanSession {
			_ = nch.Reject(ssh.UnknownChannelType,
				"this is gridterm, and it serves "+chanSession)
			continue
		}
		var want openSession
		if err := ssh.Unmarshal(nch.ExtraData(), &want); err != nil {
			_ = nch.Reject(ssh.ConnectionFailed, "that is not a session request")
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
			s.runSession(ch, reqs, want, gone)
		}()
	}
	// The connection is finished. Waiting here is what keeps the window
	// from saying the client has gone while a shell it started is still
	// being hung up on.
	running.Wait()
}

// runSession starts something for the client to work in and carries it
// until one end or the other is done.
func (s *Server) runSession(ch ssh.Channel, reqs <-chan *ssh.Request,
	want openSession, gone <-chan struct{}) {

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
		sess, err = s.cfg.Attach(want.Attach, cols, rows)
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
		case <-gone:
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
	// Closed before it is waited for. The copy above ends when the
	// client goes as well as when the program does, and waiting first
	// would wait for a program nothing has hung up on yet.
	closeErr := sess.Close()
	waitErr := sess.Wait()

	s.endSession(ch, waitErr)
	if closeErr != nil {
		s.onError(fmt.Errorf("serve: close a session: %w", closeErr))
	}
	if copyErr != nil && !errors.Is(copyErr, io.EOF) {
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
	_ = ch.CloseWrite()
	_ = ch.Close()
}

// Attacher gives a client what is already running in one of this
// window's panes, named by the ID it was sent down the control channel.
//
// What comes back is a session like any other: reading it gives what
// the pane shows, starting with the screen as it stands, and writing to
// it types into the program. Closing it stops watching; the pane goes
// on running here, which is what lets the screen be right again when
// the client leaves.
//
// It is called from a goroutine of the server's, so an implementation
// that reaches into the window has to hand the work to whatever draws.
type Attacher func(id string, cols, rows int) (session.Session, error)

// Opener starts something for a client to work in.
//
// It is called from a goroutine of the server's, one per session a
// client opens, so an implementation that touches the window has to
// hand the work to whatever draws.
type Opener func(cols, rows int) (session.Session, error)
