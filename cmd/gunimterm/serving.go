package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/pkg/sftp"

	"github.com/marrasen/gridterm/meter"
	"github.com/marrasen/gridterm/remote"
	"github.com/marrasen/gridterm/serve"
	"github.com/marrasen/gridterm/session"
	"github.com/marrasen/gridterm/settings"
	uiterm "github.com/marrasen/gridterm/ui/term"
)

// Serving this window, as gridterm serves one: another window, on a
// machine whose key is in this one's authorized keys, connects over
// SSH and can watch and type in the panes here, open shells here, and
// read and write the files of this machine and of the servers this
// window is connected to.

// Serving is the serving, as the window shows it.
type Serving struct {
	// On says the window is served, at Addr, by the host key with
	// Fingerprint.
	On          bool
	Addr        string
	Fingerprint string
	// Clients are the windows connected, by the name their key has in
	// the authorized keys, and where they came from.
	Clients []ServedClient
	// Port and Anywhere are what serving starts with: the port asked
	// for last, and whether it listened on every network.
	Port     int
	Anywhere bool
	// Allowed are the keys that may connect, by name, from the file at
	// AllowedAt, and Problem what stops that file being read.
	Allowed   []string
	AllowedAt string
	Problem   string
}

// ServedClient is a window connected to this one.
type ServedClient struct{ Name, From string }

// Intents for serving.
type (
	// StartServing serves the window on Port, on this machine only or
	// on every network.
	StartServing struct {
		Port     string
		Anywhere bool
	}
	// StopServing stops, hanging up on every window connected.
	StopServing struct{}
	// DisconnectClients hangs up on the windows connected.
	DisconnectClients struct{}
)

// serving is the program's side.
type serving struct {
	server  *serve.Server
	clients []*serve.Client
	// snap is what this window has open, read by the server's
	// goroutines.
	mu   sync.Mutex
	snap serve.Snapshot
	// paths overrides where the host key and the authorized keys are,
	// for a test.
	hostKey, allowed string
}

// servePaths are the host key's file and the authorized keys'.
func (a *app) servePaths() (hostKey, allowed string, err error) {
	if a.serving.hostKey != "" {
		return a.serving.hostKey, a.serving.allowed, nil
	}
	if hostKey, err = serve.HostKeyPath(); err != nil {
		return "", "", err
	}
	allowed, err = serve.AuthorizedKeysPath()
	return hostKey, allowed, err
}

// servingAllowed is who may connect, for the dialog, and where that is
// written.
func (a *app) servingAllowed() (names []string, at string, err error) {
	_, at, err = a.servePaths()
	if err != nil {
		return nil, "", err
	}
	allowed, err := serve.LoadAllowed(at)
	if err != nil {
		return nil, at, err
	}
	return allowed.Names(), at, nil
}

// startServing serves the window.
func (a *app) startServing(in StartServing) error {
	if a.serving.server != nil {
		return fmt.Errorf("this window is already served on %s", a.serving.server.Addr())
	}
	port, err := strconv.Atoi(strings.TrimSpace(in.Port))
	if err != nil || port < 0 || port > 65535 {
		return fmt.Errorf("%q is not a port number", strings.TrimSpace(in.Port))
	}
	keyAt, allowedAt, err := a.servePaths()
	if err != nil {
		return err
	}
	hostKey, err := serve.HostKey(keyAt)
	if err != nil {
		return err
	}
	allowed, err := serve.LoadAllowed(allowedAt)
	if err != nil {
		return err
	}
	if allowed.Len() == 0 {
		return fmt.Errorf("no keys may connect, so there is nobody to serve. Put the public key of the machine you will connect from in %s", allowedAt)
	}
	host := "127.0.0.1"
	if in.Anywhere {
		host = ""
	}
	post := func(f func()) { a.events <- f }
	srv, err := serve.Listen(serve.Config{
		Addr:       net.JoinHostPort(host, strconv.Itoa(port)),
		HostKey:    hostKey,
		Allowed:    allowed,
		Opens:      a.opens,
		Attach:     a.attachFor,
		Open:       a.openFor,
		StartAgain: a.startAgainFor,
		Files:      a.serveFiles,
		OnJoin:     func(c *serve.Client) { post(func() { a.clientCame(c) }) },
		OnGone: func(c *serve.Client, why error) {
			post(func() { a.clientWent(c, why) })
		},
		OnStopped: func(err error) {
			post(func() {
				a.serving.server, a.serving.clients = nil, nil
				a.notify("This window is no longer served", err.Error(), "")
				a.showServing()
			})
		},
		// Written to the window log, which this reaches from any
		// goroutine.
		OnError: func(err error) { log.Printf("serving: %v", err) },
	})
	if err != nil {
		return err
	}
	a.serving.server = srv
	if a.settings != nil {
		reach := settings.ReachHere
		if in.Anywhere {
			reach = settings.ReachAnywhere
		}
		_ = a.settings.PutServe(port, reach)
		_ = a.settings.PutServeOn(true)
	}
	a.tellServed()
	a.showServing()
	return nil
}

// stopServing stops serving, and says not to offer it again next time.
func (a *app) stopServing() error {
	srv := a.serving.server
	if srv == nil {
		return nil
	}
	a.serving.server, a.serving.clients = nil, nil
	if a.settings != nil {
		_ = a.settings.PutServeOn(false)
	}
	a.showServing()
	return srv.Close()
}

// disconnectClients hangs up on every window connected.
func (a *app) disconnectClients() error {
	var errs []error
	for _, c := range a.serving.clients {
		if a.serving.server != nil {
			a.serving.server.GoingTo(c, serve.GoingKicked)
		}
		if err := c.Close(); err != nil && !serve.Ended(err) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (a *app) clientCame(c *serve.Client) {
	a.serving.clients = append(a.serving.clients, c)
	a.notify(c.Name+" connected", "From "+c.Addr+". It can open shells, use the panes here, and read and write files as you.", "")
	a.showServing()
}

func (a *app) clientWent(c *serve.Client, why error) {
	a.serving.clients = slices.DeleteFunc(a.serving.clients, func(have *serve.Client) bool { return have == c })
	if why != nil && !serve.Ended(why) {
		a.notify("Connection to "+c.Name+" lost", why.Error(), "")
	}
	a.showServing()
}

// showServing publishes the serving.
func (a *app) showServing() {
	s := Serving{Port: remote.ServePort}
	if a.settings != nil {
		if port, ok := a.settings.ServePort(); ok {
			s.Port = port
		}
		if reach, ok := a.settings.ServeReach(); ok {
			s.Anywhere = reach == settings.ReachAnywhere
		}
	}
	names, at, err := a.servingAllowed()
	s.Allowed, s.AllowedAt = names, at
	if err != nil {
		s.Problem = err.Error()
	}
	if srv := a.serving.server; srv != nil {
		s.On, s.Addr, s.Fingerprint = true, srv.Addr(), serve.Fingerprint(srv.HostKey())
		for _, c := range a.serving.clients {
			s.Clients = append(s.Clients, ServedClient{Name: c.Name, From: c.Addr})
		}
	}
	a.st.Serving = s
}

// tellServed tells the windows connected what this one has open, when
// that has changed. It runs after every change the program publishes.
func (a *app) tellServed() {
	srv := a.serving.server
	if srv == nil {
		return
	}
	snap := serve.Snapshot{}
	for _, p := range a.st.Panes {
		kind := servedKind(p.Kind)
		if kind == "" {
			continue
		}
		o := serve.Open{ID: p.ID, Host: p.Machine, Kind: kind, Label: p.Title, State: meter.Opened.String()}
		if t := a.terminal(p.ID); t != nil {
			size := t.Size()
			o.Cols, o.Rows = size.Cols, size.Rows
		}
		snap.Open = append(snap.Open, o)
	}
	a.serving.mu.Lock()
	same := slices.Equal(a.serving.snap.Open, snap.Open)
	a.serving.snap = snap
	a.serving.mu.Unlock()
	if !same {
		srv.Publish(snap)
	}
}

// servedKind is what a pane is called to another window, in gridterm's
// words, and "" for one that is not offered: the jobs and the secrets
// belong to this window.
func servedKind(kind string) string {
	switch kind {
	case kindTerminal:
		return "Terminal"
	case kindFiles:
		return "Files"
	case kindReader:
		return "Reader"
	case kindTunnel, kindLog:
		return "Log"
	}
	return ""
}

// opens is what this window has open, for the server's goroutines.
func (a *app) opens() serve.Snapshot {
	a.serving.mu.Lock()
	defer a.serving.mu.Unlock()
	return a.serving.snap
}

// attachFor gives a connected window a pane that is open here, both
// windows drawing it at the size the watcher asks for.
func (a *app) attachFor(want serve.Attached, cols, rows int) (session.Session, error) {
	return onApp(a, func() (session.Session, error) {
		t := a.terminal(want.ID)
		if t == nil {
			return nil, fmt.Errorf("there is no terminal called %q open here any more", want.ID)
		}
		return a.watchPane(t, cols, rows)
	})
}

// openFor opens a shell here for a connected window, in a pane of its
// own that this window shows too.
func (a *app) openFor(cols, rows int) (session.Session, serve.Attached, error) {
	type opened struct {
		sess session.Session
		id   string
	}
	got, err := onApp(a, func() (opened, error) {
		var id string
		if err := a.openThen("", placement{}, func(pane string, _ error) { id = pane }); err != nil {
			return opened{}, err
		}
		a.setPane(id, func(p *Pane) { p.Title += " (opened from another window)" })
		sess, err := a.watchPane(a.terminal(id), cols, rows)
		return opened{sess, id}, err
	})
	if err != nil {
		return nil, serve.Attached{}, err
	}
	return got.sess, serve.Attached{ID: got.id, Kind: "Terminal"}, nil
}

// startAgainFor starts again a pane's program, for a connected window
// working in it.
func (a *app) startAgainFor(want serve.Attached) error {
	_, err := onApp(a, func() (struct{}, error) { return struct{}{}, a.startAgain(want.ID) })
	return err
}

// serveFiles gives a connected window the files of this machine, or
// of a server this window is connected to, carried over its
// connection.
func (a *app) serveFiles(_ context.Context, host string, ch io.ReadWriteCloser) error {
	if host == "" {
		opts := []sftp.ServerOption{sftp.WindowsRootEnumeratesDrives()}
		if home, err := os.UserHomeDir(); err == nil {
			opts = append(opts, sftp.WithServerWorkingDirectory(home))
		}
		srv, err := sftp.NewServer(keptOpen{ch}, opts...)
		if err != nil {
			return err
		}
		served := srv.Serve()
		if errors.Is(served, io.EOF) {
			served = nil
		}
		return errors.Join(served, srv.Close())
	}
	conn, err := onApp(a, func() (*remote.Conn, error) {
		if c, ok := a.conns[host]; ok {
			return c, nil
		}
		return nil, fmt.Errorf("this window is not connected to %s", host)
	})
	if err != nil {
		return err
	}
	relay, err := conn.FileSubsystem(a.ctx)
	if err != nil {
		return fmt.Errorf("could not open a file session on %s: %w", host, err)
	}
	done := make(chan error, 2)
	go func() { _, err := io.Copy(relay, ch); done <- err }()
	go func() { _, err := io.Copy(ch, relay); done <- err }()
	first := <-done
	if errors.Is(first, io.EOF) {
		first = nil
	}
	return errors.Join(first, relay.Close())
}

// keptOpen keeps the SFTP server from closing the channel, which the
// server that gave it closes.
type keptOpen struct{ io.ReadWriteCloser }

func (keptOpen) Close() error { return nil }

// watchPane is a session on a pane that is open here: the screen as it
// stands and then everything the program writes, and what is typed
// going to the program. The pane keeps the watcher's size while it is
// watched.
func (a *app) watchPane(t *uiterm.Terminal, cols, rows int) (session.Session, error) {
	w := &watched{pane: t, out: make(chan []byte, 256), done: make(chan struct{}), over: make(chan struct{})}
	stop, err := t.Watch(feed{w})
	if err != nil {
		return nil, err
	}
	w.stop = stop
	post := func(f func()) {
		select {
		case a.events <- f:
		case <-a.ctx.Done():
		}
	}
	t.Hold(cols, rows)
	w.resize = func(cols, rows int) error {
		go post(func() { t.Hold(cols, rows) })
		return nil
	}
	w.gone = func() {
		go post(func() {
			if t.Watched() == 0 {
				t.Release()
			}
		})
	}
	return w, nil
}

// watched is the session a watcher reads.
type watched struct {
	pane      *uiterm.Terminal
	stop      func()
	resize    func(cols, rows int) error
	gone      func()
	out       chan []byte
	behind    atomic.Bool
	ended     atomic.Bool
	closeOnce sync.Once
	done      chan struct{}
	overOnce  sync.Once
	over      chan struct{}
	left      []byte
}

// feed is how the pane hands a watcher what it writes.
type feed struct{ w *watched }

func (f feed) Screen(p []byte) error {
	select {
	case <-f.w.done:
		return io.EOF
	default:
	}
	f.w.drain()
	f.w.out <- append([]byte(nil), p...)
	return nil
}

func (f feed) Write(p []byte) (int, error) {
	select {
	case <-f.w.done:
		return 0, io.EOF
	default:
	}
	select {
	case f.w.out <- append([]byte(nil), p...):
	default:
		// Too far behind to catch up byte by byte: the screen as it
		// stands is sent again instead.
		f.w.behind.Store(true)
		f.w.drain()
		select {
		case f.w.out <- nil:
		default:
		}
	}
	return len(p), nil
}

func (f feed) Ended() {
	f.w.ended.Store(true)
	f.w.overOnce.Do(func() { close(f.w.over) })
}

func (w *watched) drain() {
	for {
		select {
		case <-w.out:
		default:
			return
		}
	}
}

func (w *watched) Read(p []byte) (int, error) {
	for len(w.left) == 0 {
		if w.behind.Swap(false) {
			_ = w.pane.Resync(feed{w})
		}
		select {
		case b := <-w.out:
			w.left = b
		case <-w.done:
			return 0, io.EOF
		case <-w.over:
			select {
			case b := <-w.out:
				w.left = b
			default:
				return 0, io.EOF
			}
		}
	}
	n := copy(p, w.left)
	w.left = w.left[n:]
	return n, nil
}

func (w *watched) Write(p []byte) (int, error) {
	select {
	case <-w.done:
		return 0, uiterm.ErrEnded
	default:
	}
	if w.ended.Load() {
		return 0, uiterm.ErrEnded
	}
	w.pane.Send(p)
	return len(p), nil
}

func (w *watched) Resize(cols, rows int) error { return w.resize(cols, rows) }

func (w *watched) Wait() error {
	select {
	case <-w.done:
	case <-w.over:
	}
	return nil
}

func (w *watched) Close() error {
	w.closeOnce.Do(func() {
		close(w.done)
		w.stop()
		w.gone()
	})
	return nil
}

// offerToServeAgain asks, as the window opens, whether to serve it
// again, when it was served as the last one closed. It runs on a
// goroutine of its own, as asking waits.
func (a *app) offerToServeAgain() {
	s := a.st.Serving
	where := "this machine only"
	if s.Anywhere {
		where = "every network"
	}
	ans, err := a.ask(a.ctx, Ask{Title: "Serve this window again?", Text: fmt.Sprintf("It was served when it last closed: on port %d, listening on %s.", s.Port, where), Yes: "Serve", No: "Not Now"})
	if err != nil || !ans.Yes {
		return
	}
	in := StartServing{Port: strconv.Itoa(s.Port), Anywhere: s.Anywhere}
	a.events <- func() {
		if err := a.startServing(in); err != nil {
			a.notify("Couldn't serve the window", err.Error(), "")
		}
	}
}
