package app

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/marrasen/kakel/links"
	"github.com/marrasen/kakel/screen"
	"github.com/marrasen/kakel/session"

	"github.com/marrasen/kakel/machines"

	"github.com/marrasen/kakel/remote"
	"github.com/marrasen/kakel/serve"
)

// Connecting to another window that is served: over SSH, with a key that
// window allows. The window is then a machine in the sidebar, like a
// server: New Terminal opens a shell there, Files its files, and what it
// has open is listed under it, to work in from here.

// RemoteWindow is a window this one is connected to, as the sidebar
// shows it.
type RemoteWindow struct {
	Name machines.ID
	Addr string
	// Open is what it has open, with a screen to work in, less what
	// this window already shows.
	Open []serve.Open
	// Folders are the folders it has saved for the machines it reaches,
	// by its key for each, to offer under their headings.
	Folders map[string][]string
	// Machines are the machines it is connected to, by its key for
	// each, each a heading here with nothing open on it too.
	Machines []string
}

// Intents for other windows.
type (
	// ConnectWindow connects to a window served at Addr, which is
	// host, or host:port, with the key in KeyFile, or the usual keys
	// when it is empty. ID is the saved window's, or the quick
	// connection's it is made again for; with none, it is a quick
	// connection of its own.
	ConnectWindow struct {
		Addr, KeyFile string
		ID            machines.ID
	}
	// DisconnectWindow lets go of a window, by its ID, closing the
	// panes on it.
	DisconnectWindow struct{ ID machines.ID }
	// AttachWindow works in something a window has open, in a pane
	// here.
	AttachWindow struct {
		Window machines.ID
		ID     string
	}
)

// KnownWindowsFile is where the host keys of the windows connected to
// are kept.
const KnownWindowsFile = "known_windows"

// serveAddr is an address with the serving port when it names none.
func serveAddr(addr string) string {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return ""
	}
	if _, _, err := net.SplitHostPort(addr); err != nil {
		return net.JoinHostPort(strings.Trim(addr, "[]"), strconv.Itoa(remote.ServePort))
	}
	return addr
}

// windowAt is the window known at addr, a served window's address with
// its port: one connected to or being connected to, or one saved; ""
// for none.
func (a *app) windowAt(addr string) machines.ID {
	for _, id := range a.machines.IDs(func(m machines.Machine) bool { return m.Window != nil || m.Dialing != nil }) {
		if w := a.machines.Get(id).Window; w != nil && links.SameTarget(serveAddr(w.Addr), addr) {
			return id
		}
		if target, window, ok := a.machines.Quick(id); ok && window && links.SameTarget(serveAddr(target), addr) {
			return id
		}
		if h, ok := a.machines.Saved(id); ok && h.Window && links.SameTarget(h.ServeAddr(), addr) {
			return id
		}
	}
	if a.book != nil {
		for _, h := range a.book.Hosts() {
			if h.Window && links.SameTarget(h.ServeAddr(), addr) {
				return machines.ID(h.ID)
			}
		}
	}
	return ""
}

// connectWindow connects to a served window, and opens a terminal on
// it once it has.
func (a *app) connectWindow(in ConnectWindow) error { return a.reachWindow(in, true) }

// reachWindow connects to a served window, and opens a terminal on it
// once it has when terminal is set.
func (a *app) reachWindow(in ConnectWindow, terminal bool) error {
	addr := serveAddr(in.Addr)
	if addr == "" {
		return errors.New("type the address of the window to connect to")
	}
	name := in.ID
	if name == "" {
		// Typed: a window known at that address already is that one,
		// rather than a second connection under a second heading.
		name = a.windowAt(addr)
		// A saved one is dialled with its own key when none was typed,
		// as it is from the Servers menu.
		if h, saved := a.machines.Saved(name); saved && strings.TrimSpace(in.KeyFile) == "" {
			in.KeyFile = h.KeyFile()
		}
	}
	if _, saved := a.machines.Saved(name); name == "" || (!saved && !a.machines.IsQuick(name)) {
		// A quick one, or one again that was forgotten meanwhile, as
		// when its row was cleared before the reconnect was answered.
		if name == "" {
			name = a.machines.NewQuick(addr, true)
		} else {
			a.machines.KeepQuick(name, addr, true)
		}
	}
	switch {
	case a.machines.Get(name).Window != nil:
		return fmt.Errorf("this window is already connected to %s", a.machines.Name(name))
	case a.machines.Get(name).Dialing != nil:
		// On its way already: that connection is the one asked for.
		return nil
	}
	// Connected to again some other way: the question is answered.
	a.withdrawLost(name)
	dctx, cancel := context.WithCancel(a.ctx)
	a.machines.At(name).Dialing = cancel
	acct := a.dialLog(name)
	logLine(acct, "", "connecting to the window at "+addr)
	logPane := a.watchDial(name)
	began := time.Now()
	a.showStatus()
	go func() {
		win, err := remote.ReachWindow(dctx, remote.Reach{
			Addr: addr, KeyFile: strings.TrimSpace(in.KeyFile), Ring: a.ring, Ask: newAsker(a, name),
			Known:  knownWindows,
			Saying: func(what string) { logLine(acct, "", what) },
			Wrong:  func(what string) { logLine(acct, badly, what) },
		})
		a.events <- func() {
			a.machines.At(name).Dialing = nil
			cancel()
			a.showStatus()
			if err != nil {
				logLine(acct, badly, "could not connect: "+err.Error())
				if errors.Is(err, context.Canceled) && logPane != "" {
					// Given up on purpose: its log goes with it. A
					// failure leaves the log up, saying why.
					a.closePane(logPane)
				}
				if !errors.Is(err, errDeclined) && !errors.Is(err, context.Canceled) {
					a.failed("Couldn't connect to the window at "+addr, err.Error())
					a.problem()
				}
				return
			}
			logLine(acct, well, "connected in "+time.Since(began).Round(10*time.Millisecond).String())
			a.done()
			a.holdWindow(name, addr, in.KeyFile, win)
			a.dialed(logPane, name, terminal)
		}
	}()
	return nil
}

// knownWindows is the file of windows' host keys.
func knownWindows() (string, error) {
	dir, err := serve.Dir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return filepath.Join(dir, KnownWindowsFile), nil
}

// holdWindow keeps a window connected to, following what it has open
// and noticing when it goes.
func (a *app) holdWindow(name machines.ID, addr, keyFile string, win *serve.Window) {
	w := &machines.Window{Serve: win, Addr: addr, KeyFile: keyFile, Bound: map[string]string{}}
	a.machines.At(name).Window = w
	a.machines.At(name).Dropped = false
	a.showWindows()
	gone := make(chan struct{})
	go func() {
		why := win.Wait()
		close(gone)
		a.events <- func() { a.windowGone(name, w, why) }
	}()
	// What it has open changes as its user works; it is looked at
	// twice a second.
	go func() {
		t := time.NewTicker(500 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-gone:
				return
			case <-a.ctx.Done():
				return
			case <-t.C:
			}
			a.events <- func() {
				if a.machines.Get(name).Window == w && (!slices.Equal(w.Seen, win.Opens()) ||
					!maps.EqualFunc(w.Folders, win.Folders(), slices.Equal) || !slices.Equal(w.Reaches, win.Machines())) {
					a.showWindows()
				} else {
					a.quiet = true
				}
			}
		}
	}()
}

// windowGone lets go of a window whose connection has ended. Its panes
// end with it, and say so.
func (a *app) windowGone(name machines.ID, w *machines.Window, why error) {
	if a.machines.Get(name).Window != w {
		return
	}
	a.machines.At(name).Window = nil
	if err := a.tunnelsDiedOn(name, w.Leaving); err != nil {
		a.failed("Trouble closing the tunnels through "+a.machines.Name(name), err.Error())
	}
	a.machines.Each(func(key machines.ID, m *machines.Machine) {
		if m.Files != nil && key.Of(name) {
			_ = m.Files.Close()
			m.Files = nil
			a.forgetFar(key)
		}
	})
	said := "Its panes here have ended."
	switch w.Serve.Going() {
	case serve.GoingStopped:
		said = "It stopped being served. " + said
	case serve.GoingKicked:
		said = "It disconnected this one. " + said
	case "":
		if w.Leaving {
			break
		}
		a.machines.At(name).Dropped = true
		// The connection went, rather than the window saying so on
		// purpose: offered to reach again, the connection alone, with
		// what it has open listed once it answers.
		text := a.machines.Name(name)
		if why != nil && !serve.Ended(why) {
			text += "\n\n" + serve.Plain(why.Error())
		}
		logLine(a.machines.Get(name).Log, "", "connection lost")
		a.problem()
		again := ConnectWindow{Addr: w.Addr, KeyFile: w.KeyFile, ID: name}
		ctx, cancel := context.WithCancel(a.ctx)
		a.withdrawLost(name)
		a.machines.At(name).Lost = cancel
		a.askThen(ctx, Ask{Title: "Connection lost", Icon: "unplug", Text: text, Yes: "Reconnect", No: "Close"}, func(ans AskAnswered) {
			if ctx.Err() != nil {
				// Withdrawn while the answer was on its way.
				return
			}
			a.machines.At(name).Lost = nil
			cancel()
			if !ans.Yes {
				return
			}
			if err := a.reachWindow(again, false); err != nil {
				a.failed("Couldn't reconnect to "+a.machines.Name(name), err.Error())
				a.problem()
			}
		})
		a.showWindows()
		return
	}
	logLine(a.machines.Get(name).Log, "", "disconnected")
	a.notify("Disconnected from the window at "+w.Addr, said, "")
	a.showWindows()
}

// disconnectFar asks window to close its connection to the machine it
// reaches by key. What is open on it, there and here, ends with it.
func (a *app) disconnectFar(window machines.ID, key string) error {
	w := a.machines.Get(window).Window
	if w == nil {
		return fmt.Errorf("this window is not connected to %s any more", a.machines.Name(window))
	}
	far := machines.FarID(window, key)
	called := a.machines.Name(far)
	go func() {
		err := w.Serve.DisconnectOn(key)
		a.events <- func() {
			if err != nil {
				a.failed("Couldn't disconnect "+called, err.Error())
				return
			}
			a.worked("Disconnected "+called, "", "")
		}
	}()
	return nil
}

// withdrawLost takes back the question offering to reconnect to
// machine, if there is one.
func (a *app) withdrawLost(machine machines.ID) {
	if cancel := a.machines.Get(machine).Lost; cancel != nil {
		cancel()
		a.machines.At(machine).Lost = nil
	}
}

// disconnectWindow lets go of a window.
func (a *app) disconnectWindow(name machines.ID) error {
	w := a.machines.Get(name).Window
	ok := w != nil
	if !ok {
		return nil
	}
	// Let go of on purpose: nothing to offer to reconnect.
	w.Leaving = true
	err := w.Serve.Close()
	if serve.Ended(err) {
		err = nil
	}
	return err
}

// showWindows publishes the windows connected to.
func (a *app) showWindows() {
	var out []RemoteWindow
	for _, name := range a.machines.Windows() {
		w := a.machines.Get(name).Window
		w.Seen, w.Folders, w.Reaches = w.Serve.Opens(), w.Serve.Folders(), w.Serve.Machines()
		for _, o := range w.Seen {
			if o.Key() != "" {
				a.machines.NameFar(name, o.Key(), o.Host)
			}
		}
		rw := RemoteWindow{Name: name, Addr: w.Addr, Folders: w.Folders}
		for _, m := range w.Reaches {
			a.machines.NameFar(name, m.Key, m.Name)
			rw.Machines = append(rw.Machines, m.Key)
		}
		for _, o := range w.Seen {
			if !o.HasScreen() {
				continue
			}
			if pane, ok := w.Bound[o.ID]; ok && a.has(pane) {
				continue
			}
			rw.Open = append(rw.Open, o)
		}
		out = append(out, rw)
	}
	a.st.Windows = out
}

// openThrough opens something new on window, a kakel window connected
// to, in a pane here: a terminal on the machine it reaches by key, ""
// for its own, or cmd there when cmd has a command. The window opens it
// in a pane of its own, which this one watches. then hears the pane,
// or why there is none.
func (a *app) openThrough(window machines.ID, key string, cmd command, id, title string, at Placement, then func(string, error)) error {
	w := a.machines.Get(window).Window
	if w == nil {
		return fmt.Errorf("this window is not connected to %s any more", a.machines.Name(window))
	}
	on := window
	if key != "" {
		on = machines.FarID(window, key)
	}
	a.starting++
	go func() {
		// What the window calls it arrives on a goroutine of the
		// connection's, and is written down on the program's.
		named := func(n serve.Attached) {
			go func() { a.events <- func() { a.bindFar(w, n.ID, id) } }()
		}
		var sess session.Session
		var err error
		if key == "" && len(cmd.argv) == 0 {
			// A terminal on its own machine, as every build can open.
			sess, err = w.Serve.Open(screen.Cols, screen.Rows, named)
		} else {
			sess, err = w.Serve.OpenOn(key, strings.Join(cmd.argv, " "), cmd.dir, screen.Cols, screen.Rows, named)
		}
		a.events <- func() {
			a.starting--
			if err != nil {
				what := "Couldn't open a shell on "
				if len(cmd.argv) > 0 {
					what = "Couldn't run " + strings.Join(cmd.argv, " ") + " on "
				}
				a.failed(what+a.machines.Name(on), err.Error())
				a.stayIfEmpty()
				then("", err)
				return
			}
			a.addPane(a.paneOn(on, Pane{ID: id, Title: title, Command: len(cmd.argv) > 0}), screen.Open(sess, a.palette, a.withLinks(a.hooks(id), id, on)), at)
			a.showWindows()
			then(id, nil)
		}
	}()
	return nil
}

// attachWindow works in something a window has open, in a pane here,
// or goes to the pane already showing it.
func (a *app) attachWindow(in AttachWindow) error {
	w := a.machines.Get(in.Window).Window
	ok := w != nil
	if !ok {
		return fmt.Errorf("this window is not connected to %s any more", a.machines.Name(in.Window))
	}
	if pane, ok := w.Bound[in.ID]; ok && a.has(pane) {
		a.bringHere(pane)
		return nil
	}
	open, ok := w.Serve.OpenNamed(in.ID)
	if !ok {
		return fmt.Errorf("%s no longer has that open", a.machines.Name(in.Window))
	}
	if !open.HasScreen() {
		return fmt.Errorf("%q on %s has no screen to work in", open.Label, a.machines.Name(in.Window))
	}
	a.next++
	id := "p" + strconv.Itoa(a.next)
	go func() {
		sess, err := w.Serve.Attach(open, screen.Cols, screen.Rows)
		a.events <- func() {
			if err != nil {
				a.failed("Couldn't work in "+open.Label, err.Error())
				return
			}
			// Its links and paths are followed where it runs: beyond the
			// window, for one on a machine the window reaches.
			on := in.Window
			if open.Key() != "" {
				on = machines.FarID(in.Window, open.Key())
			}
			a.addPane(Pane{ID: id, Title: open.Label, Machine: in.Window, On: open.Key()}, screen.Open(sess, a.palette, a.withLinks(a.hooks(id), id, on)), Placement{})
			if open.Key() != "" {
				a.farHost[id] = open.Key()
				a.machines.NameFar(in.Window, open.Key(), open.Host)
			}
			w.Bound[in.ID] = id
			a.showWindows()
		}
	}()
	return nil
}

// giveUp stops a connection being made to machine, and reports
// whether there was one.
func (a *app) giveUp(machine machines.ID) bool {
	cancel := a.machines.Get(machine).Dialing
	ok := cancel != nil
	if ok {
		cancel()
		logLine(a.account(machine), "", "given up")
	}
	return ok
}

// Disconnect closes the connection to a server or a window. Its panes
// end, and say so, and can be started again once it is connected
// again.
type Disconnect struct{ Machine machines.ID }

// disconnect closes the connection to machine.
func (a *app) disconnect(machine machines.ID) error {
	if window, key, far := machine.Far(); far {
		return a.disconnectFar(window, key)
	}
	if a.giveUp(machine) {
		return nil
	}
	if a.machines.Get(machine).Window != nil {
		return a.disconnectWindow(machine)
	}
	conn := a.machines.Get(machine).Conn
	ok := conn != nil
	if !ok {
		return fmt.Errorf("this window is not connected to %s", a.machines.Name(machine))
	}
	// Let go of on purpose: its row goes with it, and those of the
	// servers reached through it.
	a.machines.At(machine).LetGo = true
	a.machines.LetGoOfRiders(conn)
	return conn.Close()
}
