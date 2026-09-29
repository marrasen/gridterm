package app

import (
	"fmt"
	"net"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/marrasen/kakel/links"
	"github.com/marrasen/kakel/screen"

	"github.com/marrasen/kakel/machines"

	"github.com/marrasen/kakel/remote"
	"github.com/marrasen/kakel/vfs"
)

// Links in a terminal: Ctrl and a click on an address opens it in the
// browser, and on a path opens it, a folder in a file pane and a file in
// the reader, at the line it names. An address on a server's own
// loopback, such as a development server's, opens through a tunnel made
// for it.

// withLinks gives pane id's hooks what follows its links, for a pane
// on machine.
func (a *app) withLinks(h screen.Hooks, id string, machine machines.ID) screen.Hooks {
	// Where the pane runs, for FindPath, which the terminal calls on a
	// goroutine of its own: set here, and changed by onWindowsOwn when a
	// pane beyond a window starts again on the window's own machine.
	where := new(atomic.Pointer[machines.ID])
	where.Store(&machine)
	a.linksAt[id] = where
	post := func(f func()) {
		go func() {
			select {
			case a.events <- f:
			case <-a.ctx.Done():
			}
		}()
	}
	h.Link = func(at string) {
		post(func() {
			// Where the pane runs as the link is followed: beyond its
			// window, for one on a machine a window reaches.
			if err := a.openLink(a.filesKey(id), at); err != nil {
				a.failed("Couldn't open "+at, err.Error())
			}
		})
	}
	h.FindPath = func(text, dir string) (string, bool, bool) {
		if on := *where.Load(); on != machines.Local {
			return a.findFar(on, text, dir)
		}
		return links.OnDisk(text, dir)
	}
	h.OpenPath = func(at string, isDir bool, line int) {
		post(func() {
			// Where the pane runs as the path is followed, as for a link.
			if err := a.openPath(a.filesKey(id), at, isDir, line); err != nil {
				a.failed("Couldn't open "+at, err.Error())
			}
		})
	}
	return h
}

// openLink opens an address in the browser: through a tunnel when it
// is on a server's own loopback.
func (a *app) openLink(machine machines.ID, at string) error {
	if err := links.Openable(at); err != nil {
		return err
	}
	target, loopback := links.LocalService(at)
	if !loopback || machine == machines.Local {
		// An address anywhere, or on this machine's own loopback: the
		// browser here reaches it.
		return openInBrowser(at)
	}
	// Through a kakel window, that window carries the tunnel; otherwise
	// the connection does.
	through, _, far := machine.Far()
	if !far {
		through = machine
	}
	if a.machines.Get(through).Window == nil {
		if _, ok, err := a.connOf(machine); err != nil {
			return err
		} else if !ok {
			return fmt.Errorf("this window is not connected to %s any more, so its %s cannot be reached", a.machines.Name(machine), at)
		}
	}
	local, err := a.tunnelTo(machine, target)
	if err != nil {
		return err
	}
	u, err := url.Parse(at)
	if err != nil {
		return err
	}
	_, port, err := net.SplitHostPort(local)
	if err != nil {
		return err
	}
	u.Host = net.JoinHostPort("127.0.0.1", port)
	return openInBrowser(u.String())
}

// tunnelTo is the address here of a tunnel to target on machine: one
// already open, or a new one on a free port.
func (a *app) tunnelTo(machine machines.ID, target string) (string, error) {
	for _, t := range a.st.Tunnels {
		open, ok := a.tunnels[t.ID]
		if !ok || open.Done() || t.Machine != machine {
			continue
		}
		got := open.Forwarder().Tunnel()
		if got.Kind == remote.LocalForward && links.SameTarget(got.Target, target) {
			return open.Forwarder().Addr(), nil
		}
	}
	if err := a.openTunnel(OpenTunnel{Machine: machine, Tunnel: remote.Tunnel{Kind: remote.LocalForward, Listen: "127.0.0.1:0", Target: target}, Sure: true}); err != nil {
		return "", err
	}
	last := a.st.Tunnels[len(a.st.Tunnels)-1]
	return a.tunnels[last.ID].Forwarder().Addr(), nil
}

// openInBrowser opens an address in the user's browser. A variable,
// so a test opens nothing.
var openInBrowser = func(at string) error {
	name, args := "xdg-open", []string{at}
	switch runtime.GOOS {
	case "windows":
		name, args = "cmd", []string{"/c", "start", "", at}
	case "darwin":
		name = "open"
	}
	cmd := exec.Command(name, args...)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("could not open %s: %w", at, err)
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

// mostFarPaths is how many paths on servers are remembered.
const mostFarPaths = 2048

// pathsFar remembers which paths on servers are there. A pane asks as
// the pointer moves, on the window's goroutine, and a server takes a
// round trip to answer, so the first ask sends for it and the answer
// is there by the next.
type pathsFar struct {
	mu     sync.Mutex
	known  map[string]farPath
	asking map[string]bool
}

type farPath struct {
	at           string
	isDir, found bool
}

// findFar is the file or folder a path in a pane on machine names, as
// far as is known yet.
func (a *app) findFar(machine machines.ID, text, dir string) (string, bool, bool) {
	text = strings.TrimSpace(text)
	if text == "" || strings.ContainsAny(text, "\r\n\x00") {
		return "", false, false
	}
	at := text
	if !strings.HasPrefix(text, "/") && !links.WindowsAbs(text) {
		if dir == "" {
			return "", false, false
		}
		at = strings.TrimRight(dir, `/\`) + "/" + text
	}
	key := string(machine) + "\x00" + at
	a.far.mu.Lock()
	defer a.far.mu.Unlock()
	if known, ok := a.far.known[key]; ok {
		return known.at, known.isDir, known.found
	}
	if a.far.asking[key] || len(a.far.known) >= mostFarPaths {
		return "", false, false
	}
	a.far.asking[key] = true
	go func() {
		a.events <- func() {
			answer := func(p farPath) {
				a.far.mu.Lock()
				defer a.far.mu.Unlock()
				delete(a.far.asking, key)
				a.far.known[key] = p
			}
			// Not knowing is not remembered: it is asked again.
			giveUp := func() {
				a.far.mu.Lock()
				defer a.far.mu.Unlock()
				delete(a.far.asking, key)
			}
			look := func(f vfs.FS) {
				go func() {
					e, err := f.Stat(vfs.Spelled(f, at))
					if err != nil {
						answer(farPath{})
						return
					}
					answer(farPath{at: at, isDir: e.IsDir(), found: true})
				}()
			}
			if f := a.fsFor(machine); f != nil {
				look(f)
				return
			}
			// The machine's files are opened for it, quietly, over the
			// connection there is: a path under the pointer is no reason
			// to connect, or to say anything when the files will not
			// open.
			open := a.filesOpener(machine)
			if open == nil {
				giveUp()
				return
			}
			go func() {
				f, err := open()
				a.events <- func() {
					if err != nil {
						giveUp()
						return
					}
					look(a.keepFiles(machine, f))
				}
			}()
		}
	}()
	return "", false, false
}

// forgetFar forgets what a machine said about its paths, once the files
// it was said through have gone: a machine reached again under the same
// name may be another, and its files may have changed meanwhile.
func (a *app) forgetFar(machine machines.ID) {
	a.far.mu.Lock()
	defer a.far.mu.Unlock()
	for key := range a.far.known {
		if strings.HasPrefix(key, string(machine)+"\x00") {
			delete(a.far.known, key)
		}
	}
}

// openPath opens a path a link named: a folder in a file pane, a file
// in the reader at line.
func (a *app) openPath(machine machines.ID, at string, isDir bool, line int) error {
	// A server's files are opened first when no file pane has yet.
	return a.withFiles(machine, func(f vfs.FS) {
		if !isDir {
			a.readOn(machine, f, at, false, line, Placement{})
			return
		}
		if err := a.openFilesOn(machine, f, at); err != nil {
			a.failed("Couldn't open "+at, err.Error())
		}
	})
}
