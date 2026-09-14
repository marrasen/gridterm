package serve

import (
	"errors"
	"fmt"
	"io"
	"sync"

	"golang.org/x/crypto/ssh"

	"github.com/marrasen/gridterm/session"
)

// serveChannels answers what a client opens on its connection.
//
// Everything but a session is refused by name, so an ordinary SSH
// client that finds this port is told what is wrong rather than handed
// a shell it did not ask for.
func (s *Server) serveChannels(chans <-chan ssh.NewChannel) {
	var running sync.WaitGroup
	for nch := range chans {
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
			// with it, so there is nothing to report and nothing to do.
			s.onError(fmt.Errorf("serve: take a session: %w", err))
			continue
		}
		running.Add(1)
		go func() {
			defer running.Done()
			s.runSession(ch, reqs, want)
		}()
	}
	// The connection is finished. Its sessions are closing with it, and
	// waiting for them is what keeps a window from ending while a shell
	// it started is still being hung up on.
	running.Wait()
}

// runSession starts something for the client to work in and carries it
// until one end or the other is done.
func (s *Server) runSession(ch ssh.Channel, reqs <-chan *ssh.Request, want openSession) {
	if s.cfg.Open == nil {
		_ = ch.CloseWrite()
		_ = ch.Close()
		s.onError(errors.New("serve: this window has nothing to open"))
		return
	}
	cols, rows := int(want.Cols), int(want.Rows)
	sess, err := s.cfg.Open(cols, rows)
	if err != nil {
		// Said down the channel as well as reported here. The client is
		// drawing a pane for this and has nothing else to put in it.
		_, _ = io.WriteString(ch, "gridterm could not start that: "+err.Error()+"\r\n")
		_ = ch.CloseWrite()
		_ = ch.Close()
		s.onError(fmt.Errorf("serve: open a session: %w", err))
		return
	}

	// The requests that arrive while it runs, which is how the client
	// says its pane changed size.
	go func() {
		for req := range reqs {
			ok := false
			if req.Type == reqWindowChange {
				var size windowChange
				if err := ssh.Unmarshal(req.Payload, &size); err == nil {
					ok = sess.Resize(int(size.Cols), int(size.Rows)) == nil
				}
			}
			if req.WantReply {
				_ = req.Reply(ok, nil)
			}
		}
	}()

	// What the client types, into the program.
	go func() {
		_, _ = io.Copy(sess, ch)
		// The client is finished typing. The program is told the same
		// way a local one is told when its terminal closes.
		_ = sess.Close()
	}()

	// What the program writes, out to the client.
	_, copyErr := io.Copy(ch, sess)
	waitErr := sess.Wait()
	_ = sess.Close()

	// How it ended, before the channel goes: a client that only saw the
	// channel close could not tell a program that finished from a
	// connection that dropped.
	status := uint32(0)
	if waitErr != nil {
		status = 1
	}
	_, _ = ch.SendRequest(reqExitStatus, false, ssh.Marshal(exitStatus{Status: status}))
	_ = ch.CloseWrite()
	_ = ch.Close()

	if copyErr != nil && !errors.Is(copyErr, io.EOF) {
		s.onError(fmt.Errorf("serve: carry a session: %w", copyErr))
	}
}

// Opener starts something for a client to work in.
//
// It is called from a goroutine of the server's, one per session a
// client opens, so an implementation that touches the window has to
// hand the work to whatever draws.
type Opener func(cols, rows int) (session.Session, error)
