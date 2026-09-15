package serve

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/marrasen/gridterm/session"
)

// dialTimeout is how long reaching another window may take.
const dialTimeout = 15 * time.Second

// handshakeTimeout is how long the other end has to get through the
// handshake once it has answered, when the caller asks for no other
// bound.
//
// It is its own deadline because ssh.ClientConfig.Timeout is not one:
// that field bounds the TCP connection that ssh.Dial makes, and this
// makes its own connection so that cancelling can close it. Without
// this, something that accepts and then says nothing -- a firewall that
// accepts, a port forwarded to nothing, another service on the port --
// leaves the window saying "opening" for ever.
const handshakeTimeout = 20 * time.Second

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

	// Patience is how long the other end has to get through the
	// handshake once it has answered. Zero asks for handshakeTimeout.
	Patience time.Duration

	// Saying is told each step as it is tried, for showing somebody what
	// a connection is doing. A nil one is not called.
	//
	// It is called from whichever goroutine is connecting, which is not
	// the one that draws, so an implementation that touches a window has
	// to hand the work to whatever does.
	Saying func(what string)
}

// say tells whoever is watching what is being done now, if anybody is.
func (c DialConfig) say(what string) {
	if c.Saying != nil {
		c.Saying(what)
	}
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

	cfg.say("reaching " + cfg.Addr)
	d := net.Dialer{Timeout: dialTimeout}
	nc, err := d.DialContext(ctx, "tcp", cfg.Addr)
	if err != nil {
		return nil, fmt.Errorf("serve: reach %s: %w", cfg.Addr, err)
	}
	cfg.say("asking " + cfg.Addr + " who it is")
	// Closing the connection is what unblocks the handshake, whichever
	// part of it is waiting.
	stop := context.AfterFunc(ctx, func() { _ = nc.Close() })

	// And a deadline, because the handshake can wait on a machine that
	// answered and then went quiet, and nothing else here bounds that.
	// Cleared once the connection is made: from then on it is a session
	// that may sit idle for as long as the user likes.
	patience := cfg.Patience
	if patience <= 0 {
		patience = handshakeTimeout
	}
	if err := nc.SetDeadline(time.Now().Add(patience)); err != nil {
		stop()
		_ = nc.Close()
		return nil, fmt.Errorf("serve: reach %s: %w", cfg.Addr, err)
	}

	cc, chans, reqs, err := ssh.NewClientConn(nc, cfg.Addr, &ssh.ClientConfig{
		User: "gridterm",
		// A callback rather than the keys themselves, so the account
		// says when signing in starts: everything before it is this end
		// and the network, everything after it is the other window.
		Auth: []ssh.AuthMethod{ssh.PublicKeysCallback(func() ([]ssh.Signer, error) {
			cfg.say(fmt.Sprintf("signing in as gridterm, offering %d keys", len(cfg.Keys)))
			return cfg.Keys, nil
		})},
		HostKeyCallback: func(hostname string, addr net.Addr, key ssh.PublicKey) error {
			cfg.say("it answered, with a " + key.Type() + " host key")
			if err := cfg.HostKey(hostname, addr, key); err != nil {
				return err
			}
			cfg.say("its host key is accepted")
			return nil
		},
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
	if err := nc.SetDeadline(time.Time{}); err != nil {
		_ = cc.Close()
		return nil, fmt.Errorf("serve: reach %s: %w", cfg.Addr, err)
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

// Attach works in something the other window already has open, as it
// was described down the control channel.
//
// The whole description is sent back, not just its ID: an ID is a place
// in a list, and the other window checks that the place still holds
// what this one was told it held.
//
// What comes back reads as that pane's screen, starting with what is
// already on it, and writing to it types there. It keeps running on
// that machine either way: this is a second pair of eyes on it, not a
// hand-over.
func (w *Window) Attach(open Open, cols, rows int) (session.Session, error) {
	return w.session(openSession{
		Cols: uint32(cols), Rows: uint32(rows),
		Attach:     open.ID,
		AttachHost: open.Host, AttachKind: open.Kind,
	})
}

// Files opens a file session on the other machine.
//
// What comes back is a stream, not a filesystem. What runs on it is the
// caller's business, the same way it is the serving window's: this
// package carries the bytes.
func (w *Window) Files() (*FileSession, error) {
	if w.isClosed() {
		return nil, errors.New("serve: that window has been let go of")
	}
	ch, reqs, err := w.client.OpenChannel(chanFiles, nil)
	if err != nil {
		return nil, fmt.Errorf("serve: ask %s for its files: %w", w.addr, err)
	}
	go ssh.DiscardRequests(reqs)

	f := &FileSession{Channel: ch, said: make(chan string, 1)}
	go func() {
		out, _ := io.ReadAll(ch.Stderr())
		f.said <- strings.TrimSpace(string(out))
	}()
	return f, nil
}

// FileSession is a file session on the other machine.
//
// It is a stream: read it, write to it, close it. What runs on it is
// the caller's business.
type FileSession struct {
	ssh.Channel
	said chan string
}

// Said is what the other end said about a session it could not start.
//
// That reason arrives on the channel's error stream, which is the only
// place it exists: it happened over there. It waits for that stream to
// end, so it is for the path where starting the session failed and the
// channel has gone with it. Empty means the other end said nothing.
func (f *FileSession) Said() string {
	why := <-f.said
	// Put back, so asking twice says the same thing rather than the
	// second asking waiting for a stream that has already ended.
	f.said <- why
	return why
}

// Open starts something to work in on the other machine, sized for the
// pane it will be drawn in.
func (w *Window) Open(cols, rows int) (session.Session, error) {
	if w.isClosed() {
		return nil, errors.New("serve: that window has been let go of")
	}
	// Sent as asked. The machine that has to make a terminal this size
	// is the one that clamps it, and a second clamp here would only
	// hide what this window actually asked for.
	return w.session(openSession{Cols: uint32(cols), Rows: uint32(rows)})
}

// open asks the other window for a session and wraps what comes back.
func (w *Window) session(want openSession) (session.Session, error) {
	if w.isClosed() {
		return nil, errors.New("serve: that window has been let go of")
	}
	ch, reqs, err := w.client.OpenChannel(chanSession, ssh.Marshal(want))
	if err != nil {
		return nil, fmt.Errorf("serve: open a session on %s: %w", w.addr, err)
	}
	s := &remoteSession{ch: ch, done: make(chan struct{}), saidDone: make(chan struct{})}
	go s.readRequests(reqs)
	// What the other end says went wrong, into the same stream as the
	// program's own output. It is the only place a pane can show it,
	// and a pane that opened and closed with nothing in it would leave
	// the user with no idea why.
	go func() {
		defer close(s.saidDone)
		// A reason cut in half by a connection that dropped is worse
		// than no reason: the user reads what arrived as the whole of
		// it. So the failure is put on the end of what it cut.
		if _, err := io.Copy(&PlainWriter{To: errWriter{s}}, ch.Stderr()); err != nil {
			_, _ = errWriter{s}.Write([]byte(
				"\r\ngridterm: the rest of that was lost: " + err.Error() + "\r\n"))
		}
	}()
	return s, nil
}

// errWriter puts what the other end said onto a session's own stream,
// so a pane drawing that session shows it.
type errWriter struct{ s *remoteSession }

func (e errWriter) Write(p []byte) (int, error) {
	e.s.mu.Lock()
	defer e.s.mu.Unlock()
	e.s.said = append(e.s.said, p...)
	return len(p), nil
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

	// said is what the other end sent on the channel's error stream,
	// waiting to be read as though the program had written it.
	mu   sync.Mutex
	said []byte

	// saidDone is closed when the error stream has ended, so a read
	// that has run out of program output knows there is no more of it
	// coming.
	saidDone chan struct{}

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

// Read gives what the program said, and what the other end said about
// it.
func (s *remoteSession) Read(p []byte) (int, error) {
	if n := s.takeSaid(p); n > 0 {
		return n, nil
	}
	n, err := s.ch.Read(p)
	if n == 0 && err != nil {
		// The channel has gone. Anything the other end said about why
		// came on the error stream, which a goroutine of its own is
		// copying, so that goroutine is waited for rather than raced
		// with: a refusal that lost the race is a pane that closed
		// with nothing in it and a user with no idea why.
		//
		// It ends when the channel does, so the wait is as long as the
		// read that has already returned.
		<-s.saidDone
		if said := s.takeSaid(p); said > 0 {
			return said, nil
		}
	}
	return n, err
}

// takeSaid takes what the other end said about the session, if any is
// waiting.
func (s *remoteSession) takeSaid(p []byte) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.said) == 0 {
		return 0
	}
	n := copy(p, s.said)
	s.said = s.said[n:]
	return n
}

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
