package serve

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/marrasen/gridterm/session"
)

// dialTimeout is how long reaching another window may take.
const dialTimeout = 15 * time.Second

// Window is another machine's gridterm, taken over from this one.
//
// Sessions opened on it are session.Session like any other, so a pane
// drawing one cannot tell it from a shell on this machine. That is the
// whole shape of it: what crosses the wire is the bytes of a program
// and the size of the pane it is drawn in, and this window draws
// everything else itself.
type Window struct {
	client *ssh.Client

	// addr is where it was reached, for saying which window this is.
	addr string

	mu     sync.Mutex
	closed bool
}

// DialConfig is what reaching another window takes.
type DialConfig struct {
	// Addr is the machine and port it is serving on.
	Addr string

	// Key is this window's own key, which the other one must have
	// listed. There is no other way in.
	Key ssh.Signer

	// HostKey checks the machine answering is the one meant. It is
	// never nil: a window that took whatever answered would be one that
	// hands a shell to whoever got there first.
	HostKey ssh.HostKeyCallback
}

// Dial reaches another window that is serving.
//
// Cancelling ctx gives up, wherever the handshake has got to: closing
// the connection under it is the only way to stop x/crypto part way
// through one.
func Dial(ctx context.Context, cfg DialConfig) (*Window, error) {
	switch {
	case cfg.Addr == "":
		return nil, errors.New("serve: no window to reach")
	case cfg.Key == nil:
		return nil, errors.New("serve: no key to reach it with")
	case cfg.HostKey == nil:
		return nil, errors.New("serve: nothing to check the machine by")
	}

	d := net.Dialer{Timeout: dialTimeout}
	nc, err := d.DialContext(ctx, "tcp", cfg.Addr)
	if err != nil {
		return nil, fmt.Errorf("serve: reach %s: %w", cfg.Addr, err)
	}
	// Closing the connection is what unblocks the handshake, whichever
	// part of it is waiting.
	stop := context.AfterFunc(ctx, func() { _ = nc.Close() })

	cc, chans, reqs, err := ssh.NewClientConn(nc, cfg.Addr, &ssh.ClientConfig{
		User:            "gridterm",
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(cfg.Key)},
		HostKeyCallback: cfg.HostKey,
		Timeout:         dialTimeout,
	})
	if err != nil {
		if !stop() {
			_ = nc.Close()
			return nil, errors.Join(ctx.Err(), err)
		}
		_ = nc.Close()
		return nil, fmt.Errorf("serve: reach %s: %w", cfg.Addr, err)
	}
	if !stop() {
		// Given up on between the handshake finishing and us noticing.
		_ = cc.Close()
		return nil, ctx.Err()
	}
	return &Window{client: ssh.NewClient(cc, chans, reqs), addr: cfg.Addr}, nil
}

// Addr is where this window was reached.
func (w *Window) Addr() string { return w.addr }

// Open starts something to work in on the other machine, sized for the
// pane it will be drawn in.
func (w *Window) Open(cols, rows int) (session.Session, error) {
	if w.isClosed() {
		return nil, errors.New("serve: that window has been let go of")
	}
	ch, reqs, err := w.client.OpenChannel(chanSession, ssh.Marshal(openSession{
		Cols: uint32(max(cols, 1)), Rows: uint32(max(rows, 1)),
	}))
	if err != nil {
		return nil, fmt.Errorf("serve: open a session on %s: %w", w.addr, err)
	}
	s := &remoteSession{ch: ch, done: make(chan struct{})}
	go s.readRequests(reqs)
	return s, nil
}

// Close lets go of the other window. Everything opened on it goes too.
func (w *Window) Close() error {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return nil
	}
	w.closed = true
	w.mu.Unlock()
	return w.client.Close()
}

func (w *Window) isClosed() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.closed
}

// remoteSession is a program running in another window.
//
// It is a session.Session, so the pane drawing it cannot tell it from a
// shell on this machine. No local pseudo-terminal is involved: the
// channel carries the bytes and window-change carries the size, which
// is what the other end turns back into a real terminal.
type remoteSession struct {
	ch ssh.Channel

	// writeMu keeps Write and Close off each other: closing writes the
	// channel's own flags, and the window can close with a keystroke
	// still in flight.
	writeMu sync.Mutex

	// done is closed when the other end has said how the program ended,
	// or the channel has gone. status is only read after that.
	done   chan struct{}
	status uint32
	gotOne bool

	closeOnce sync.Once
	closeErr  error
}

func (s *remoteSession) Read(p []byte) (int, error) { return s.ch.Read(p) }

func (s *remoteSession) Write(p []byte) (int, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return s.ch.Write(p)
}

// Resize tells the other end the pane changed size.
func (s *remoteSession) Resize(cols, rows int) error {
	if cols <= 0 || rows <= 0 {
		return nil
	}
	_, err := s.ch.SendRequest(reqWindowChange, false, ssh.Marshal(windowChange{
		Cols: uint32(cols), Rows: uint32(rows),
	}))
	if err != nil {
		return fmt.Errorf("serve: resize a session: %w", err)
	}
	return nil
}

// Wait blocks until the program ends and says how it went.
func (s *remoteSession) Wait() error {
	<-s.done
	if s.gotOne && s.status != 0 {
		return fmt.Errorf("serve: it ended with status %d", s.status)
	}
	return nil
}

// Close hangs the program up.
func (s *remoteSession) Close() error {
	s.closeOnce.Do(func() {
		s.writeMu.Lock()
		defer s.writeMu.Unlock()
		if err := s.ch.Close(); err != nil && !errors.Is(err, io.EOF) {
			s.closeErr = fmt.Errorf("serve: close a session: %w", err)
		}
	})
	return s.closeErr
}

// readRequests takes what the other end says about the program while it
// runs, which is how it ended.
func (s *remoteSession) readRequests(reqs <-chan *ssh.Request) {
	defer close(s.done)
	for req := range reqs {
		if req.Type == reqExitStatus {
			var got exitStatus
			if err := ssh.Unmarshal(req.Payload, &got); err == nil {
				s.status, s.gotOne = got.Status, true
			}
		}
		if req.WantReply {
			_ = req.Reply(false, nil)
		}
	}
}
