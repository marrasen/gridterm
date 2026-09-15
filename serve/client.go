package serve

import (
	"bufio"
	"context"
	"encoding/json"
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

	// open is the last the other window said it had open. Written by
	// the goroutine reading the control channel and read by whoever is
	// drawing, so the lock is what keeps a half-written list off the
	// screen.
	open []Open
}

// DialConfig is what reaching another window takes.
type DialConfig struct {
	// Addr is the machine and port it is serving on.
	Addr string

	// Keys are the keys to offer. The other window has to have one of
	// them listed: there is no other way in, and nothing else is tried.
	Keys []ssh.Signer

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
	case len(cfg.Keys) == 0:
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
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(cfg.Keys...)},
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
	w := &Window{client: ssh.NewClient(cc, chans, reqs), addr: cfg.Addr}
	w.watch()
	return w, nil
}

// Opens is what the other window last said it had open.
//
// The list it sent, not one worked out here: what a window has open is
// that window's business, and a client that guessed would be a client
// that disagreed with the machine it is looking at.
func (w *Window) Opens() []Open {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]Open(nil), w.open...)
}

// watch listens for what the other window has open.
//
// A window that refuses the channel is one of an older build, or one
// that says nothing about itself. Either way there is nothing to be
// done about it and nothing to tell the user: what it serves still
// works, and the list stays empty.
func (w *Window) watch() {
	ch, reqs, err := w.client.OpenChannel(chanControl, nil)
	if err != nil {
		return
	}
	go ssh.DiscardRequests(reqs)
	go func() {
		defer ch.Close()
		// A snapshot a line at a time. The buffer is generous because a
		// window with a great many things open sends a long line, and a
		// line cut in half is a snapshot thrown away.
		lines := bufio.NewScanner(ch)
		lines.Buffer(make([]byte, 0, 64<<10), 4<<20)
		for lines.Scan() {
			var snap Snapshot
			if err := json.Unmarshal(lines.Bytes(), &snap); err != nil {
				// One snapshot that cannot be read changes nothing: the
				// next one replaces it whole.
				continue
			}
			w.mu.Lock()
			w.open = snap.Open
			w.mu.Unlock()
		}
	}()
}

// Addr is where this window was reached.
func (w *Window) Addr() string { return w.addr }

// Open starts something to work in on the other machine, sized for the
// pane it will be drawn in.
func (w *Window) Open(cols, rows int) (session.Session, error) {
	if w.isClosed() {
		return nil, errors.New("serve: that window has been let go of")
	}
	// Sent as asked. The machine that has to make a terminal this size
	// is the one that clamps it, and a second clamp here would only
	// hide what this window actually asked for.
	ch, reqs, err := w.client.OpenChannel(chanSession, ssh.Marshal(openSession{
		Cols: uint32(cols), Rows: uint32(rows),
	}))
	if err != nil {
		return nil, fmt.Errorf("serve: open a session on %s: %w", w.addr, err)
	}
	s := &remoteSession{ch: ch, done: make(chan struct{})}
	go s.readRequests(reqs)
	return s, nil
}

// Wait blocks until the connection to the other window ends.
//
// A window that quit at the far end is still held here, saying it is
// taken over, until something notices. Nothing else would.
func (w *Window) Wait() error { return w.client.Wait() }

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

	// done is closed when the other end has said how the program ended,
	// or the channel has gone without it saying. status and gotOne are
	// written before that and read after it, so the close is what keeps
	// them straight.
	done   chan struct{}
	status uint32
	gotOne bool

	closeOnce sync.Once
	closeErr  error
}

func (s *remoteSession) Read(p []byte) (int, error) { return s.ch.Read(p) }

// Write sends input. No lock of our own: a session is never written to
// from two places at once, and a lock shared with Close is how a pane
// stops being closeable. Write blocks until the far end has room, and
// Close is the only thing that can unblock it.
func (s *remoteSession) Write(p []byte) (int, error) { return s.ch.Write(p) }

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
//
// A session whose channel closed without the other end saying how it
// ended is a connection that dropped, not a program that finished. The
// two are different things to show somebody, and returning nothing for
// both would make this whole exchange pointless.
func (s *remoteSession) Wait() error {
	<-s.done
	switch {
	case !s.gotOne:
		return errors.New("serve: the connection went before it said how that ended")
	case s.status != 0:
		return fmt.Errorf("serve: it ended with status %d", s.status)
	}
	return nil
}

// Close hangs the program up.
func (s *remoteSession) Close() error {
	s.closeOnce.Do(func() {
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
			// A status that cannot be read is no status at all, which
			// Wait reports as a connection that went. Taking it as a
			// clean finish would be the one answer that is certainly
			// wrong.
			if err := ssh.Unmarshal(req.Payload, &got); err == nil {
				s.status, s.gotOne = got.Status, true
			}
		}
		if req.WantReply {
			_ = req.Reply(false, nil)
		}
	}
}
