package remote

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

// TunnelKind is which machine listens.
type TunnelKind uint8

const (
	// LocalForward listens on this machine and connects from the far
	// end. It is what ssh -L does: a port here stands for a service
	// over there.
	LocalForward TunnelKind = iota

	// RemoteForward listens on the far machine and connects from this
	// one. It is what ssh -R does.
	RemoteForward

	// DynamicForward listens on this machine and speaks SOCKS5, so a
	// browser or a command with a proxy setting reaches whatever it asks
	// for as the far machine sees it. It is what ssh -D does.
	DynamicForward
)

// String names a kind the way a dialog does.
func (k TunnelKind) String() string {
	switch k {
	case LocalForward:
		return "local"
	case RemoteForward:
		return "remote"
	case DynamicForward:
		return "socks"
	}
	return "unknown"
}

// Tunnel describes a forwarded port.
type Tunnel struct {
	// Kind is which machine listens.
	Kind TunnelKind

	// Listen is where connections are accepted, as host:port. An empty
	// host means this machine only, which is what ssh does and what
	// keeps a tunnel from being a hole for everything on the network.
	// Port 0 asks for any free port.
	Listen string

	// Target is where they are sent, as host:port, read on whichever
	// machine is doing the connecting. A dynamic tunnel has none: every
	// stream says where it wants to go.
	Target string
}

// loopback is what an unqualified listen address means.
const loopback = "127.0.0.1"

// split breaks an address into a host and a port, filling in loopback
// for a host that was left out.
func split(addr string) (host string, port int, err error) {
	h, p, err := net.SplitHostPort(addr)
	if err != nil {
		return "", 0, fmt.Errorf("%q is not a host and a port: %w", addr, err)
	}
	n, err := strconv.Atoi(strings.TrimSpace(p))
	if err != nil {
		return "", 0, fmt.Errorf("%q is not a port number", p)
	}
	if n < 0 || n > 65535 {
		return "", 0, fmt.Errorf("the port %d is not between 0 and 65535", n)
	}
	h = strings.TrimSpace(h)
	if h == "" {
		h = loopback
	}
	return h, n, nil
}

// listenAddr is where the tunnel accepts connections, with the host
// filled in.
func (t Tunnel) listenAddr() (string, error) {
	host, port, err := split(t.Listen)
	if err != nil {
		return "", fmt.Errorf("the address to listen on: %w", err)
	}
	return net.JoinHostPort(host, strconv.Itoa(port)), nil
}

// Validate reports what is wrong with a tunnel, or nil.
func (t Tunnel) Validate() error {
	if t.Kind != LocalForward && t.Kind != RemoteForward && t.Kind != DynamicForward {
		return fmt.Errorf("a tunnel is local, remote or dynamic")
	}
	if _, err := t.listenAddr(); err != nil {
		return err
	}
	if t.Kind == DynamicForward {
		// Every stream through it says where it is going, so a target
		// here would be a setting that does nothing.
		if strings.TrimSpace(t.Target) != "" {
			return errors.New("a dynamic tunnel reaches wherever it is asked, so it takes no address")
		}
		return nil
	}
	// A target with no host, ":5432", means the loopback of whichever
	// machine does the connecting, which is what ssh means by it too.
	if _, port, err := split(t.Target); err != nil {
		return fmt.Errorf("the address to connect to: %w", err)
	} else if port == 0 {
		return errors.New("the address to connect to needs a port")
	}
	return nil
}

// Exposed reports whether the tunnel accepts connections from other
// machines rather than only from the one it listens on.
//
// It exists so the window can say so before opening one: a forward on
// 0.0.0.0 lets anything that can reach the machine through the tunnel,
// with no password and no log.
//
// An address that does not parse is not exposed because it is not
// opened at all: Validate refuses it first.
func (t Tunnel) Exposed() bool {
	host, _, err := split(t.Listen)
	if err != nil {
		return false
	}
	if ip := net.ParseIP(host); ip != nil {
		return !ip.IsLoopback()
	}
	// A name, which could be anything. Only "localhost" is certainly not
	// reachable from elsewhere.
	return !strings.EqualFold(host, "localhost")
}

// String describes a tunnel the way the panel shows it.
func (t Tunnel) String() string {
	arrow := " → "
	switch t.Kind {
	case RemoteForward:
		return "remote " + t.Listen + arrow + t.Target
	case DynamicForward:
		return "socks5 " + t.Listen
	}
	return t.Listen + arrow + t.Target
}

// Forwarder is a tunnel that is open.
//
// It rides on the connection: closing the connection closes the tunnel
// and every stream going through it.
type Forwarder struct {
	conn *Conn
	t    Tunnel

	// ln accepts connections. For a local tunnel it listens on this
	// machine; for a remote one it is the far machine's listener,
	// reached over the connection.
	ln net.Listener

	// count is what the panel reads. A nil one counts nothing.
	count Counter

	// onError reports a failure with nowhere to be returned to: an
	// accept that failed, or a stream that could not be connected.
	onError func(error)

	// streams is how many are open right now, and served is how many
	// there have been.
	streams atomic.Int64
	served  atomic.Int64

	// mu guards the live streams and the closing flag.
	mu      sync.Mutex
	live    map[io.Closer]struct{}
	closing bool

	closeOnce sync.Once
	closeErr  error
}

// Counter is told what moves through a tunnel.
//
// It is an interface so that remote does not have to know what the panel
// counts with. An implementation must be safe to use from several
// goroutines: every stream writes through it at once.
type Counter interface {
	// Wrap returns a writer that counts what is written through it. out
	// says the bytes are leaving this machine.
	Wrap(w io.Writer, out bool) io.Writer
}

// TunnelConfig is what to open a tunnel with.
type TunnelConfig struct {
	// Tunnel is the forward itself.
	Tunnel Tunnel

	// Count records what moves, for the panel. It may be nil.
	Count Counter

	// OnError is told about a failure that has nowhere to be returned
	// to. It is called from the goroutines carrying the streams, so it
	// must be safe to call from any of them.
	OnError func(error)
}

// OpenTunnel starts forwarding a port.
//
// The listener is opened before this returns, so a port already in use
// is reported here rather than from a goroutine nobody is watching.
func (c *Conn) OpenTunnel(cfg TunnelConfig) (*Forwarder, error) {
	if err := cfg.Tunnel.Validate(); err != nil {
		return nil, fmt.Errorf("remote: %w", err)
	}
	if c.isClosing() {
		return nil, fmt.Errorf("remote: %s: %w", c, ErrClosed)
	}
	addr, err := cfg.Tunnel.listenAddr()
	if err != nil {
		return nil, fmt.Errorf("remote: %w", err)
	}

	var ln net.Listener
	switch cfg.Tunnel.Kind {
	case LocalForward, DynamicForward:
		ln, err = net.Listen("tcp", addr)
	case RemoteForward:
		// Asked of the far machine, which answers with the port it
		// bound. A machine that refuses forwarding says so here.
		ln, err = c.client.Listen("tcp", addr)
	}
	if err != nil {
		return nil, fmt.Errorf("remote: listen on %s for %s: %w", addr, cfg.Tunnel, err)
	}

	f := &Forwarder{
		conn:    c,
		t:       cfg.Tunnel,
		ln:      ln,
		onError: cfg.OnError,
		live:    make(map[io.Closer]struct{}),
	}
	f.count = cfg.Count
	// Registered before the loop starts, so a connection closing at this
	// moment closes the listener rather than leaving it accepting into a
	// connection that has gone.
	if err := c.register(f); err != nil {
		_ = ln.Close()
		return nil, err
	}
	go f.serve()
	return f, nil
}

// Addr returns the address the tunnel is listening on, which is where
// the port ended up when the tunnel asked for any free one.
func (f *Forwarder) Addr() string { return f.ln.Addr().String() }

// Tunnel returns what the forwarder was opened with.
func (f *Forwarder) Tunnel() Tunnel { return f.t }

// Streams returns how many connections are going through the tunnel
// right now.
func (f *Forwarder) Streams() int { return int(f.streams.Load()) }

// Served returns how many connections there have been altogether.
func (f *Forwarder) Served() int { return int(f.served.Load()) }

// String describes the tunnel and the machine it runs over.
func (f *Forwarder) String() string { return f.t.String() + " on " + f.conn.String() }

// serve accepts connections until the tunnel closes.
func (f *Forwarder) serve() {
	for {
		nc, err := f.ln.Accept()
		if err != nil {
			if !f.isClosing() {
				// The listener failed on its own, which ends the tunnel:
				// nothing else will ever accept on it.
				f.report(fmt.Errorf("remote: %s: stopped accepting: %w", f, err))
				_ = f.Close()
			}
			return
		}
		go f.carry(nc)
	}
}

// carry connects the other end and copies until one side stops.
func (f *Forwarder) carry(near net.Conn) {
	target := f.t.Target
	if f.t.Kind == DynamicForward {
		// Where this stream is going is the first thing it says.
		asked, err := socksAsk(near)
		if err != nil {
			f.report(fmt.Errorf("remote: %s: %w", f, err))
			_ = near.Close()
			return
		}
		target = asked
	}

	far, err := f.dial(target)
	if f.t.Kind == DynamicForward {
		// The client is waiting to be told, whichever way it went.
		if answer := socksAnswer(near, socksCode(err)); answer != nil && err == nil {
			f.report(fmt.Errorf("remote: %s: %w", f, answer))
			_ = near.Close()
			_ = far.Close()
			return
		}
	}
	if err != nil {
		f.report(fmt.Errorf("remote: %s: %w", f, err))
		_ = near.Close()
		return
	}
	// A stream opened as the tunnel closes would be left copying with
	// nothing to close it.
	if !f.hold(near, far) {
		_ = near.Close()
		_ = far.Close()
		return
	}
	defer f.release(near, far)

	f.streams.Add(1)
	f.served.Add(1)
	defer f.streams.Add(-1)

	// Which of the two is on this machine decides which way the bytes
	// are counted: what is written to the far machine is going out.
	here, there := near, far
	if f.t.Kind == RemoteForward {
		// The listener is over there, so the accepted connection is the
		// far side and what it was connected to is here.
		here, there = far, near
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, _ = io.Copy(f.wrap(there, true), here)
		// Half the stream is done. Closing both is what stops the other
		// half, which is otherwise blocked on a read that will not
		// return until the peer goes away.
		_ = there.Close()
		_ = here.Close()
	}()
	go func() {
		defer wg.Done()
		_, _ = io.Copy(f.wrap(here, false), there)
		_ = here.Close()
		_ = there.Close()
	}()
	wg.Wait()
}

// wrap counts what is written to one side of a stream.
func (f *Forwarder) wrap(w io.Writer, out bool) io.Writer {
	if f.count == nil {
		return w
	}
	return f.count.Wrap(w, out)
}

// dial opens the other end of a stream, from whichever machine is doing
// the connecting.
func (f *Forwarder) dial(target string) (net.Conn, error) {
	ctx, cancel := context.WithTimeout(context.Background(), dialTimeout)
	defer cancel()

	switch f.t.Kind {
	case RemoteForward:
		var d net.Dialer
		c, err := d.DialContext(ctx, "tcp", target)
		if err != nil {
			return nil, fmt.Errorf("reach %s from here: %w", target, err)
		}
		return c, nil
	default:
		c, err := f.conn.client.DialContext(ctx, "tcp", target)
		if err != nil {
			return nil, fmt.Errorf("reach %s from %s: %w", target, f.conn, err)
		}
		return c, nil
	}
}

// hold records a live stream so closing the tunnel closes it. It
// reports false once the tunnel is closing, when nothing would.
func (f *Forwarder) hold(cs ...io.Closer) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closing {
		return false
	}
	for _, c := range cs {
		f.live[c] = struct{}{}
	}
	return true
}

// release forgets a stream that has finished.
func (f *Forwarder) release(cs ...io.Closer) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range cs {
		delete(f.live, c)
	}
}

// isClosing reports whether Close has started.
func (f *Forwarder) isClosing() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.closing
}

// report hands on a failure that has nowhere to be returned to. One with
// nobody to tell is dropped rather than logged: the window is where a
// failure belongs, and a console nobody is reading is not.
func (f *Forwarder) report(err error) {
	if err == nil || f.onError == nil {
		return
	}
	f.onError(err)
}

// Close stops the tunnel and cuts every stream going through it, and
// lets go of the connection's record of it.
func (f *Forwarder) Close() error {
	err := f.closeRider()
	f.conn.drop(f)
	return err
}

// closeRider is Close without the deregistering, for a connection that
// is closing its riders and will throw the whole record away anyway.
func (f *Forwarder) closeRider() error {
	f.closeOnce.Do(func() { f.closeErr = f.closeAll() })
	return f.closeErr
}

// closeAll stops accepting and ends what is still going through.
func (f *Forwarder) closeAll() error {
	f.mu.Lock()
	f.closing = true
	live := make([]io.Closer, 0, len(f.live))
	for c := range f.live {
		live = append(live, c)
	}
	f.live = nil
	f.mu.Unlock()

	var errs []error
	if err := f.ln.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
		errs = append(errs, fmt.Errorf("remote: stop listening for %s: %w", f.t, err))
	}
	// The streams after the listener, so nothing new arrives while these
	// are being cut. One whose peer has already gone says so with EOF or
	// with net.ErrClosed, and a stream that has ended is what closing it
	// was for.
	for _, c := range live {
		if err := c.Close(); err != nil && !errors.Is(err, net.ErrClosed) && !errors.Is(err, io.EOF) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
