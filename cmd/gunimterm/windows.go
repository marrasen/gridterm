package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/marrasen/gridterm/remote"
	"github.com/marrasen/gridterm/serve"
)

// Connecting to another window, as gridterm connects to one that is
// served: over SSH, with a key that window allows. The window is then a
// machine in the sidebar, like a server: New Terminal opens a shell
// there, Files its files, and what it has open is listed under it, to
// work in from here.

// RemoteWindow is a window this one is connected to, as the sidebar
// shows it.
type RemoteWindow struct {
	Name, Addr string
	// Open is what it has open, with a screen to work in, less what
	// this window already shows.
	Open []serve.Open
}

// Intents for other windows.
type (
	// ConnectWindow connects to a window served at Addr, which is
	// host, or host:port, with the key in KeyFile, or the usual keys
	// when it is empty.
	ConnectWindow struct{ Addr, KeyFile, Name string }
	// DisconnectWindow lets go of a window, closing the panes on it.
	DisconnectWindow struct{ Name string }
	// AttachWindow works in something a window has open, in a pane
	// here.
	AttachWindow struct{ Window, ID string }
)

// knownWindowsFile is where the host keys of the windows connected to
// are kept, as gridterm keeps them.
const knownWindowsFile = "known_windows"

// remoteWin is a window this one holds.
type remoteWin struct {
	win           *serve.Window
	addr, keyFile string
	// bound is the pane here showing each thing it has open, by its id
	// there.
	bound map[string]string
	// seen is its list as last told, to publish only a change.
	seen []serve.Open
}

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

// connectWindow connects to a served window, and opens a terminal on
// it once it has.
func (a *app) connectWindow(in ConnectWindow) error {
	addr := serveAddr(in.Addr)
	if addr == "" {
		return errors.New("type the address of the window to connect to")
	}
	name := addr
	if in.Name != "" {
		name = in.Name
	}
	if _, ok := a.windows[name]; ok || a.dialing[name] {
		return fmt.Errorf("this window is already connected to %s", name)
	}
	a.dialing[name] = true
	dctx, cancel := context.WithCancel(a.ctx)
	a.dialCancel[name] = cancel
	acct := a.account(name)
	logLine(acct, "", "connecting to the window at "+addr)
	began := time.Now()
	a.st.Status = "Connecting to the window at " + addr + "…"
	go func() {
		win, err := remote.ReachWindow(dctx, remote.Reach{
			Addr: addr, KeyFile: strings.TrimSpace(in.KeyFile), Ring: a.ring, Ask: newAsker(a),
			Known:  knownWindows,
			Saying: func(what string) { logLine(acct, "", what) },
			Wrong:  func(what string) { logLine(acct, badly, what) },
		})
		a.events <- func() {
			delete(a.dialing, name)
			delete(a.dialCancel, name)
			cancel()
			a.st.Status = ""
			if err != nil {
				logLine(acct, badly, "could not connect: "+err.Error())
				if !errors.Is(err, errDeclined) && !errors.Is(err, context.Canceled) {
					a.notify("Couldn't connect to the window at "+addr, err.Error(), "")
				}
				return
			}
			logLine(acct, well, "connected in "+time.Since(began).Round(10*time.Millisecond).String())
			a.holdWindow(name, addr, in.KeyFile, win)
			if err := a.open(name, placement{}); err != nil {
				a.notify("Couldn't open a terminal on "+name, err.Error(), "")
			}
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
	return filepath.Join(dir, knownWindowsFile), nil
}

// holdWindow keeps a window connected to, following what it has open
// and noticing when it goes.
func (a *app) holdWindow(name, addr, keyFile string, win *serve.Window) {
	w := &remoteWin{win: win, addr: addr, keyFile: keyFile, bound: map[string]string{}}
	a.windows[name] = w
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
				if a.windows[name] == w && !slices.Equal(w.seen, win.Opens()) {
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
func (a *app) windowGone(name string, w *remoteWin, why error) {
	if a.windows[name] != w {
		return
	}
	delete(a.windows, name)
	for key, f := range a.remoteFS {
		if key == name || strings.HasPrefix(key, name+farSep) {
			_ = f.Close()
			delete(a.remoteFS, key)
		}
	}
	said := "Its panes here have ended."
	switch w.win.Going() {
	case serve.GoingStopped:
		said = "It stopped being served. " + said
	case serve.GoingKicked:
		said = "It disconnected this one. " + said
	default:
		if why != nil && !serve.Ended(why) {
			said = serve.Plain(why.Error()) + ". " + said
		}
	}
	logLine(a.accounts[name], "", "disconnected")
	a.notify("Disconnected from the window at "+w.addr, said, "")
	a.showWindows()
}

// disconnectWindow lets go of a window.
func (a *app) disconnectWindow(name string) error {
	w, ok := a.windows[name]
	if !ok {
		return nil
	}
	err := w.win.Close()
	if serve.Ended(err) {
		err = nil
	}
	return err
}

// showWindows publishes the windows connected to.
func (a *app) showWindows() {
	var out []RemoteWindow
	for name, w := range a.windows {
		w.seen = w.win.Opens()
		rw := RemoteWindow{Name: name, Addr: w.addr}
		for _, o := range w.seen {
			if !o.HasScreen() {
				continue
			}
			if pane, ok := w.bound[o.ID]; ok && a.has(pane) {
				continue
			}
			rw.Open = append(rw.Open, o)
		}
		out = append(out, rw)
	}
	slices.SortFunc(out, func(x, y RemoteWindow) int { return strings.Compare(x.Name, y.Name) })
	a.st.Windows = out
}

// openOnWindow opens a shell on a window, in a pane here.
func (a *app) openOnWindow(name, id, title string, at placement, then func(string, error)) error {
	w := a.windows[name]
	go func() {
		// What the window calls the shell arrives on a goroutine of the
		// connection's, and is written down on the program's.
		sess, err := w.win.Open(shellCols, shellRows, func(n serve.Attached) {
			go func() {
				a.events <- func() {
					if n.ID != "" && a.has(id) {
						w.bound[n.ID] = id
						a.showWindows()
					}
				}
			}()
		})
		a.events <- func() {
			if err != nil {
				a.notify("Couldn't open a shell on "+name, err.Error(), "")
				then("", err)
				return
			}
			a.addPane(Pane{ID: id, Title: title, Machine: name}, openShell(sess, a.palette, a.withLinks(a.hooks(id), name)), at)
			a.showWindows()
			then(id, nil)
		}
	}()
	return nil
}

// attachWindow works in something a window has open, in a pane here,
// or goes to the pane already showing it.
func (a *app) attachWindow(in AttachWindow) error {
	w, ok := a.windows[in.Window]
	if !ok {
		return fmt.Errorf("this window is not connected to %s any more", in.Window)
	}
	if pane, ok := w.bound[in.ID]; ok && a.has(pane) {
		a.st.Focus = pane
		return nil
	}
	open, ok := w.win.OpenNamed(in.ID)
	if !ok {
		return fmt.Errorf("%s no longer has that open", in.Window)
	}
	if !open.HasScreen() {
		return fmt.Errorf("%q on %s has no screen to work in", open.Label, in.Window)
	}
	a.next++
	id := "p" + strconv.Itoa(a.next)
	go func() {
		sess, err := w.win.Attach(open, shellCols, shellRows)
		a.events <- func() {
			if err != nil {
				a.notify("Couldn't work in "+open.Label, err.Error(), "")
				return
			}
			a.addPane(Pane{ID: id, Title: open.Label, Machine: in.Window, On: open.Host}, openShell(sess, a.palette, a.withLinks(a.hooks(id), in.Window)), placement{})
			if open.Host != "" {
				a.farHost[id] = open.Host
			}
			w.bound[in.ID] = id
			a.showWindows()
		}
	}()
	return nil
}

// giveUp stops a connection being made to machine, and reports
// whether there was one.
func (a *app) giveUp(machine string) bool {
	cancel, ok := a.dialCancel[machine]
	if ok {
		cancel()
		logLine(a.account(machine), "", "given up")
	}
	return ok
}

// Disconnect closes the connection to a server or a window. Its panes
// end, and say so, and can be started again once it is connected
// again.
type Disconnect struct{ Machine string }

// disconnect closes the connection to machine.
func (a *app) disconnect(machine string) error {
	if a.giveUp(machine) {
		return nil
	}
	if _, ok := a.windows[machine]; ok {
		return a.disconnectWindow(machine)
	}
	conn, ok := a.conns[machine]
	if !ok {
		return fmt.Errorf("this window is not connected to %s", machine)
	}
	return conn.Close()
}
