package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"maps"
	"net"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pkg/sftp"

	"github.com/marrasen/kakel/machines"
	"github.com/marrasen/kakel/meter"
	"github.com/marrasen/kakel/remote"
	"github.com/marrasen/kakel/screen"
	"github.com/marrasen/kakel/serve"
	"github.com/marrasen/kakel/session"
	"github.com/marrasen/kakel/settings"
	uiterm "github.com/marrasen/kakel/ui/term"
)

// Serving this window: another window, on a machine whose key is in this
// one's authorized keys, connects over SSH and can watch and type in the
// panes here, open shells here, and read and write the files of this
// machine and of the servers this window is connected to.

// Serving is the serving, as the window shows it.
type Serving struct {
	// On says the window is served, at Addr, by the host key with
	// Fingerprint.
	On          bool
	Addr        string
	Fingerprint string
	// Clients are the windows connected, by the name their key has in
	// the authorized keys, and where they came from, and Tunnels the
	// tunnels they hold through this window.
	Clients []ServedClient
	Tunnels []ServedTunnel
	// Port and Anywhere are what serving starts with: the port asked
	// for last, and whether it listened on every network.
	Port     int
	Anywhere bool
	// Allowed are the keys that may connect, by name, from the file at
	// AllowedAt, and Problem what stops that file being read.
	Allowed   []string
	AllowedAt string
	Problem   string
	// Tries counts the times serving was asked to start, which worked
	// or did not, for the window that asked to hear how it went.
	Tries uint64
}

// ServedClient is a window connected to this one.
type ServedClient struct{ Name, From string }

// ServedTunnel is a tunnel a window connected to this one holds through
// it: whose, Client from From, what it forwards, and the machine here
// its streams go out from, by name.
type ServedTunnel struct{ Client, From, Label, On string }

// DisconnectClient hangs up on one window this one is served to, by
// its name and where it came from.
type DisconnectClient struct{ Name, From string }

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
	// carried are the tunnels each client holds through this window, as
	// it last said.
	carried map[*serve.Client][]serve.TunnelNote
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
		OpenOn:     a.openOnFor,
		Dial:       a.dialFor,
		Log:        a.logFor,
		Disconnect: a.disconnectFor,
		StartAgain: a.startAgainFor,
		Files:      a.serveFiles,
		Picture:    func(png []byte) error { return takePicture(png) },
		OnJoin:     func(c *serve.Client) { post(func() { a.clientCame(c) }) },
		Tunnels: func(c *serve.Client, notes []serve.TunnelNote) {
			post(func() { a.clientsTunnels(c, notes) })
		},
		OnGone: func(c *serve.Client, why error) {
			post(func() { a.clientWent(c, why) })
		},
		OnStopped: func(err error) {
			post(func() {
				a.serving.server, a.serving.clients, a.serving.carried = nil, nil, nil
				a.failed("This window is no longer served", err.Error())
				a.problem()
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
		a.keep("what is served", a.settings.PutServe(port, reach))
		a.keep("that this window is served", a.settings.PutServeOn(true))
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
	a.serving.server, a.serving.clients, a.serving.carried = nil, nil, nil
	if a.settings != nil {
		a.keep("that this window is not served", a.settings.PutServeOn(false))
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

// disconnectClient hangs up on one window this one is served to.
func (a *app) disconnectClient(in DisconnectClient) error {
	found := false
	for _, c := range a.serving.clients {
		if c.Name != in.Name || c.Addr != in.From {
			continue
		}
		found = true
		if a.serving.server != nil {
			a.serving.server.GoingTo(c, serve.GoingKicked)
		}
		if err := c.Close(); err != nil && !serve.Ended(err) {
			return err
		}
	}
	if found {
		return nil
	}
	// Gone before the button was pressed: said, rather than a button
	// that did nothing. One that came back is named, as its row reads
	// the same.
	for _, c := range a.serving.clients {
		if c.Name == in.Name {
			return fmt.Errorf("%s had already gone and has connected again since, from %s, so nothing was disconnected. Disconnect it again to hang up on the one connected now", in.Name, c.Addr)
		}
	}
	return fmt.Errorf("%s had already gone, so there was nothing to disconnect", in.Name)
}

func (a *app) clientCame(c *serve.Client) {
	a.serving.clients = append(a.serving.clients, c)
	a.notify(c.Name+" connected", "From "+c.Addr+". It can open shells, use the panes here, and read and write files as you.", "")
	a.showServing()
}

// clientsTunnels notes the tunnels client c holds through this window,
// to show whose streams it carries.
func (a *app) clientsTunnels(c *serve.Client, notes []serve.TunnelNote) {
	if !slices.Contains(a.serving.clients, c) {
		return
	}
	if a.serving.carried == nil {
		a.serving.carried = map[*serve.Client][]serve.TunnelNote{}
	}
	// What a client says is shown here: kept to so many rows, each one
	// line of plain words.
	if len(notes) > mostCarried {
		notes = notes[:mostCarried]
	}
	clean := make([]serve.TunnelNote, 0, len(notes))
	for _, n := range notes {
		clean = append(clean, serve.TunnelNote{Host: oneLine(n.Host), Label: oneLine(n.Label)})
	}
	a.serving.carried[c] = clean
	a.showServing()
}

// mostCarried is how many tunnels of one client are shown.
const mostCarried = 50

// oneLine is what a client said, as one line of plain words, cut short.
func oneLine(s string) string {
	s = strings.Join(strings.Fields(serve.Plain(s)), " ")
	if r := []rune(s); len(r) > 120 {
		s = string(r[:120]) + "…"
	}
	return s
}

func (a *app) clientWent(c *serve.Client, why error) {
	a.serving.clients = slices.DeleteFunc(a.serving.clients, func(have *serve.Client) bool { return have == c })
	delete(a.serving.carried, c)
	if why != nil && !serve.Ended(why) {
		a.failed("Connection to "+c.Name+" lost", why.Error())
		a.problem()
	}
	a.showServing()
}

// showServing publishes the serving.
func (a *app) showServing() {
	// The count of tries goes on, for the windows waiting on theirs.
	s := Serving{Port: remote.ServePort, Tries: a.st.Serving.Tries}
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
			for _, n := range a.serving.carried[c] {
				on := "this machine"
				if n.Host != "" {
					on = n.Host
					if id, ok := a.machines.Find(n.Host); ok {
						on = a.machines.Name(id)
					}
				}
				s.Tunnels = append(s.Tunnels, ServedTunnel{Client: c.Name, From: c.Addr, Label: n.Label, On: on})
			}
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
		// By its name, which the other window shows, and asks for back.
		host := ""
		if p.Machine != "" {
			host = a.machines.Name(p.Machine)
		}
		// Its ID for a saved machine; a quick one goes by its address,
		// which lasts where its ID does not.
		hostID := ""
		if _, saved := a.machines.Saved(p.Machine); saved {
			hostID = string(p.Machine)
		}
		o := serve.Open{ID: p.ID, Host: host, HostID: hostID, Kind: kind, Label: p.Title, State: meter.Opened.String()}
		if t := a.terminal(p.ID); t != nil {
			size := t.Size()
			o.Cols, o.Rows = size.Cols, size.Rows
		}
		snap.Open = append(snap.Open, o)
	}
	// The folders saved for each server connected to, which a client
	// offers under its heading, by the ID the Opens name it by.
	for _, id := range a.machines.Connected() {
		// By the key the Opens name it by: its ID when saved, its
		// name when not.
		key := a.machines.Name(id)
		if _, saved := a.machines.Saved(id); saved {
			key = string(id)
		}
		snap.Machines = append(snap.Machines, serve.Machine{Key: key, Name: a.machines.Name(id)})
		if h, ok := a.machines.Saved(id); ok && len(h.Folders) > 0 {
			if snap.Folders == nil {
				snap.Folders = map[string][]string{}
			}
			snap.Folders[h.ID] = h.Folders
		}
	}
	a.serving.mu.Lock()
	same := slices.Equal(a.serving.snap.Open, snap.Open) && slices.Equal(a.serving.snap.Machines, snap.Machines) &&
		maps.EqualFunc(a.serving.snap.Folders, snap.Folders, slices.Equal)
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
	case KindTerminal:
		return "Terminal"
	case KindFiles:
		return "Files"
	case KindReader:
		return "Reader"
	case KindTunnel, KindLog:
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
		if err := a.openThen("", Placement{}, func(pane string, _ error) { id = pane }); err != nil {
			return opened{}, err
		}
		a.openedFor[id] = true
		sess, err := a.watchPane(a.terminal(id), cols, rows)
		return opened{sess, id}, err
	})
	if err != nil {
		return nil, serve.Attached{}, err
	}
	return got.sess, serve.Attached{ID: got.id, Kind: "Terminal"}, nil
}

// openOnFor opens a terminal, or command in dir when there is one, on
// a machine this window reaches, for a connected window, in a pane of
// its own here that the other window watches. host is the machine as
// this window's list names it, "" for this machine.
func (a *app) openOnFor(host, command, dir string, cols, rows int) (session.Session, serve.Attached, error) {
	type result struct {
		id  string
		err error
	}
	done := make(chan result, 1)
	then := func(id string, err error) { done <- result{id, err} }
	_, err := onApp(a, func() (struct{}, error) {
		machine, err := a.servedMachine(host)
		if err != nil {
			return struct{}{}, err
		}
		if command == "" {
			return struct{}{}, a.openThen(machine, Placement{}, then)
		}
		return struct{}{}, a.runCommandThen(RunCommand{Machine: machine, Line: command, Dir: dir}, then)
	})
	if err != nil {
		return nil, serve.Attached{}, err
	}
	var got result
	select {
	case got = <-done:
	case <-a.ctx.Done():
		return nil, serve.Attached{}, a.ctx.Err()
	}
	if got.err != nil {
		return nil, serve.Attached{}, got.err
	}
	sess, err := onApp(a, func() (session.Session, error) {
		t := a.terminal(got.id)
		if t == nil {
			return nil, errors.New("it closed here before it could be shown")
		}
		a.openedFor[got.id] = true
		return a.watchPane(t, cols, rows)
	})
	if err != nil {
		return nil, serve.Attached{}, err
	}
	return sess, serve.Attached{ID: got.id, Host: host, Kind: "Terminal"}, nil
}

// dialFor opens a stream to addr from a machine this window reaches,
// for a tunnel a connected window holds: from this machine for host "",
// or over the connection to a server connected here.
func (a *app) dialFor(ctx context.Context, host, addr string) (net.Conn, error) {
	conn, err := onApp(a, func() (*remote.Conn, error) {
		machine, err := a.servedMachine(host)
		if err != nil || machine == machines.Local {
			return nil, err
		}
		return a.machines.Get(machine).Conn, nil
	})
	switch {
	case err != nil:
		return nil, err
	case conn == nil:
		var d net.Dialer
		return d.DialContext(ctx, "tcp", addr)
	}
	return conn.Dial(ctx, addr)
}

// logFor gives a connected window the connection log this window keeps
// for host, a machine it has connected to, to read. Its own window log
// is its own, and a kakel window beyond this one keeps its own logs.
func (a *app) logFor(host string) (session.Session, error) {
	return onApp(a, func() (session.Session, error) {
		id, ok := a.machines.Find(host)
		switch {
		case host == "" || !ok || id == machines.Local:
			return nil, fmt.Errorf("that window keeps no connection log for %q", host)
		case a.machines.IsWindow(id):
			return nil, fmt.Errorf("%s is another kakel window, which keeps its own logs", a.machines.Name(id))
		}
		l := a.machines.Get(id).Log
		if l == nil {
			return nil, fmt.Errorf("that window has not connected to %s, so it has no log of it", a.machines.Name(id))
		}
		return l.Open(), nil
	})
}

// disconnectFor closes this window's connection to host, as the
// connected window c asked, and says who asked. This machine is not
// one to disconnect.
func (a *app) disconnectFor(c *serve.Client, host string) error {
	_, err := onApp(a, func() (struct{}, error) {
		if host == "" {
			return struct{}{}, errors.New("a window cannot disconnect from its own machine")
		}
		machine, err := a.servedMachine(host)
		if err != nil {
			return struct{}{}, err
		}
		if err := a.disconnect(machine); err != nil {
			return struct{}{}, err
		}
		a.notify(c.Name+" disconnected "+a.machines.Name(machine), "Asked from the window connected from "+c.Addr+".", "")
		return struct{}{}, nil
	})
	return err
}

// servedMachine is the machine a connected window asks for something
// on, as this window's list names it: this machine for "", or a server
// connected here. A kakel window beyond this one is refused: what it
// has is its own to open. What it says is read in the window that
// asked, so it calls this one "that window".
func (a *app) servedMachine(host string) (machines.ID, error) {
	if host == "" {
		return machines.Local, nil
	}
	id, ok := a.machines.Find(host)
	switch {
	case !ok:
		return "", fmt.Errorf("that window knows no machine called %s", host)
	case a.machines.IsWindow(id):
		return "", fmt.Errorf("%s is another kakel window, which opens what it has itself", a.machines.Name(id))
	case a.machines.Get(id).Conn == nil:
		return "", fmt.Errorf("that window is not connected to %s", a.machines.Name(id))
	}
	return id, nil
}

// startAgainFor starts again a pane's program, for a connected window
// working in it, over a connection this window holds. Whether the
// window asked to have it dialled makes no difference: it never is.
func (a *app) startAgainFor(want serve.Attached, _ bool) error {
	type count struct{ restarts, endings int }
	was, err := onApp(a, func() (count, error) {
		if a.terminal(want.ID) == nil {
			// Closed here: the other window opens one of its own.
			return count{}, serve.ErrNotOpen
		}
		c := count{a.restarts[want.ID], a.endings[want.ID]}
		// Never dialled for another window, whichever it asked for: a
		// connection this window makes is for someone at it to make,
		// where its questions are asked. A window of an older build asks
		// with dial set, and is refused as well.
		if machine := a.machineOf(want.ID); machine != "" && a.machines.Get(machine).Conn == nil && a.machines.Get(machine).Window == nil {
			// Said so that it reads right in the window that asked.
			return c, fmt.Errorf("the window it runs in is no longer connected to %s. Reconnect to %s from that window first", a.machines.Name(machine), a.machines.Name(machine))
		}
		return c, a.startAgainOr(want.ID, false)
	})
	if err != nil {
		return err
	}
	// Answered once it has started again, so what attaches to it next
	// finds it: a shell on a server beyond this window comes back a
	// moment later, after a password asked here, maybe. One that could
	// not start ends again, asking again, and says so.
	deadline := time.Now().Add(startAgainWait)
	for {
		now, err := onApp(a, func() (count, error) {
			if a.terminal(want.ID) == nil {
				return count{}, serve.ErrNotOpen
			}
			return count{a.restarts[want.ID], a.endings[want.ID]}, nil
		})
		switch {
		case err != nil:
			return err
		case now.restarts > was.restarts:
			return nil
		case now.endings > was.endings:
			return errors.New("it could not start again")
		case time.Now().After(deadline):
			return errors.New("it did not start again in time")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// startAgainWait is the longest a window asking for a pane to start
// again waits for it: long enough for a password to be typed.
const startAgainWait = 2 * time.Minute

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
		// Asked for by the ID or the name this window gave it.
		id, known := a.machines.Find(host)
		if known {
			if c, ok, err := a.connOf(id); ok {
				if err == nil && a.parked[c] >= mostParked {
					// A machine that has stopped answering is given no
					// more to leave waiting.
					return nil, fmt.Errorf("%d file sessions to %s are still waiting to end, as it has stopped answering", a.parked[c], a.machines.Name(id))
				}
				return c, err
			}
			host = a.machines.Name(id)
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
	closed := relay.Close()
	// The other copy ends once the close is answered. A machine that
	// has stopped answering never answers it: the copy is left waiting,
	// counted against the connection until it ends, which it does when
	// the connection goes.
	select {
	case <-done:
	case <-time.After(relayGrace):
		go func() {
			post(a, func() { a.parked[conn]++ })
			<-done
			post(a, func() {
				if a.parked[conn]--; a.parked[conn] <= 0 {
					delete(a.parked, conn)
				}
			})
		}()
	}
	return errors.Join(first, closed)
}

// mostParked is how many file sessions may be left waiting to end on a
// connection before another is refused, and relayGrace how long one is
// given to end once closed.
const (
	mostParked = 4
	relayGrace = 5 * time.Second
)

// post runs f on the program's goroutine, unless the program is closing.
func post(a *app, f func()) {
	select {
	case a.events <- f:
	case <-a.ctx.Done():
	}
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
	post := func(f func()) {
		select {
		case a.events <- f:
		case <-a.ctx.Done():
		}
	}
	w, err := screen.Watch(t, func(cols, rows int) {
		go post(func() { t.Hold(cols, rows) })
	}, func() {
		go post(func() {
			if t.Watched() == 0 {
				t.Release()
			}
		})
	})
	if err != nil {
		return nil, err
	}
	t.Hold(cols, rows)
	return w, nil
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
	port := "on port " + strconv.Itoa(s.Port)
	if s.Port == 0 {
		port = "on whichever port was free"
	}
	ans, err := a.ask(a.ctx, Ask{Title: "Serve this window again?", Text: "It was served when it last closed: " + port + ", listening on " + where + ".",
		Choose: []string{"Serve", "Don't Ask Again"}, No: "Not Now"})
	if err != nil || !ans.Yes {
		return
	}
	if len(ans.Answers) > 0 && ans.Answers[len(ans.Answers)-1] == "Don't Ask Again" {
		// The window stops being served as it opens, until the user
		// serves it again.
		a.events <- func() {
			if a.settings != nil {
				if err := a.settings.PutServeOn(false); err != nil {
					a.failed("Couldn't keep that for next time", err.Error())
				}
			}
		}
		return
	}
	in := StartServing{Port: strconv.Itoa(s.Port), Anywhere: s.Anywhere}
	a.events <- func() {
		if err := a.startServing(in); err != nil {
			a.failed("Couldn't serve the window", err.Error())
		}
	}
}
