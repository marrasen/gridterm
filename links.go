package main

import (
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/marrasen/kakel/remote"
	"github.com/marrasen/kakel/vfs"
)

// Links in a terminal: Ctrl and a click on an address opens it in the
// browser, and on a path opens it, a folder in a file pane and a file in
// the reader, at the line it names. An address on a server's own
// loopback, such as a development server's, opens through a tunnel made
// for it.

// withLinks gives a pane's hooks what follows its links, for a pane on
// machine.
func (a *app) withLinks(h shellHooks, machine string) shellHooks {
	post := func(f func()) {
		go func() {
			select {
			case a.events <- f:
			case <-a.ctx.Done():
			}
		}()
	}
	h.link = func(at string) {
		post(func() {
			if err := a.openLink(machine, at); err != nil {
				a.failed("Couldn't open "+at, err.Error())
			}
		})
	}
	h.findPath = func(text, dir string) (string, bool, bool) {
		if machine == "" {
			return findOnDisk(text, dir)
		}
		return a.findFar(machine, text, dir)
	}
	h.openPath = func(at string, isDir bool, line int) {
		post(func() {
			if err := a.openPath(machine, at, isDir, line); err != nil {
				a.failed("Couldn't open "+at, err.Error())
			}
		})
	}
	return h
}

// openLink opens an address in the browser: through a tunnel when it
// is on a server's own loopback.
func (a *app) openLink(machine, at string) error {
	if err := linkIsOpenable(at); err != nil {
		return err
	}
	target, far := serviceOnTheFarEnd(machine, at)
	if !far {
		return openInBrowser(at)
	}
	if _, ok := a.conns[machine]; !ok {
		return fmt.Errorf("this window is not connected to %s any more, so its %s cannot be reached", machine, at)
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
func (a *app) tunnelTo(machine, target string) (string, error) {
	for _, t := range a.st.Tunnels {
		open, ok := a.tunnels[t.ID]
		if !ok || open.done || t.Machine != machine {
			continue
		}
		got := open.f.Tunnel()
		if got.Kind == remote.LocalForward && sameTarget(got.Target, target) {
			return open.f.Addr(), nil
		}
	}
	if err := a.openTunnel(OpenTunnel{Machine: machine, Tunnel: remote.Tunnel{Kind: remote.LocalForward, Listen: "127.0.0.1:0", Target: target}, Sure: true}); err != nil {
		return "", err
	}
	last := a.st.Tunnels[len(a.st.Tunnels)-1]
	return a.tunnels[last.ID].f.Addr(), nil
}

// serviceOnTheFarEnd is where an address on a server's own loopback
// points, as a target for a tunnel, and whether it is one.
func serviceOnTheFarEnd(machine, at string) (string, bool) {
	if machine == "" {
		return "", false
	}
	u, err := url.Parse(at)
	if err != nil || u.Port() == "" || !isLoopbackName(u.Hostname()) {
		return "", false
	}
	return net.JoinHostPort("127.0.0.1", u.Port()), true
}

func isLoopbackName(name string) bool {
	if strings.EqualFold(name, "localhost") {
		return true
	}
	ip := net.ParseIP(name)
	return ip != nil && (ip.IsLoopback() || ip.IsUnspecified())
}

func sameTarget(a, b string) bool {
	ah, ap, err := net.SplitHostPort(a)
	if err != nil {
		return a == b
	}
	bh, bp, err := net.SplitHostPort(b)
	if err != nil {
		return a == b
	}
	return ap == bp && (strings.EqualFold(ah, bh) || isLoopbackName(ah) && isLoopbackName(bh))
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

// linkIsOpenable refuses what is no web or mail address.
func linkIsOpenable(at string) error {
	at = strings.TrimSpace(at)
	if at == "" || strings.ContainsAny(at, "\r\n\x00") {
		return errors.New("that is no address")
	}
	u, err := url.Parse(at)
	if err != nil {
		return fmt.Errorf("%s is not an address: %w", at, err)
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https", "mailto", "ftp", "ftps":
		return nil
	}
	return fmt.Errorf("kakel opens web and mail links, and %s is a %q link", at, u.Scheme)
}

// findOnDisk is the file or folder a path in a pane names on this
// machine, read against the pane's folder when it is relative.
func findOnDisk(text, dir string) (at string, isDir, ok bool) {
	text = strings.TrimSpace(text)
	if text == "" || strings.ContainsAny(text, "\r\n\x00") {
		return "", false, false
	}
	try := text
	if !filepath.IsAbs(text) && !strings.HasPrefix(text, "/") && !strings.HasPrefix(text, `\\`) {
		if dir == "" {
			return "", false, false
		}
		try = filepath.Join(dir, text)
	}
	info, err := os.Lstat(try)
	if err != nil {
		return "", false, false
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		if target, err := os.Stat(try); err == nil {
			return try, target.IsDir(), true
		}
	}
	return try, info.IsDir(), true
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
func (a *app) findFar(machine, text, dir string) (string, bool, bool) {
	text = strings.TrimSpace(text)
	if text == "" || strings.ContainsAny(text, "\r\n\x00") {
		return "", false, false
	}
	at := text
	if !strings.HasPrefix(text, "/") && !windowsAbs(text) {
		if dir == "" {
			return "", false, false
		}
		at = strings.TrimRight(dir, `/\`) + "/" + text
	}
	key := machine + "\x00" + at
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
func (a *app) forgetFar(machine string) {
	a.far.mu.Lock()
	defer a.far.mu.Unlock()
	for key := range a.far.known {
		if strings.HasPrefix(key, machine+"\x00") {
			delete(a.far.known, key)
		}
	}
}

// windowsAbs reports whether a path starts at the top of a Windows
// drive, "C:\dir" or "C:/dir", the way a shell on a Windows machine
// prints one.
func windowsAbs(p string) bool {
	if len(p) < 3 || p[1] != ':' || p[2] != '\\' && p[2] != '/' {
		return false
	}
	c := p[0]
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

// openPath opens a path a link named: a folder in a file pane, a file
// in the reader at line.
func (a *app) openPath(machine, at string, isDir bool, line int) error {
	// A server's files are opened first when no file pane has yet.
	return a.withFiles(machine, func(f vfs.FS) {
		if !isDir {
			a.readOn(machine, f, at, false, line, placement{})
			return
		}
		if err := a.openFilesOn(machine, f, at); err != nil {
			a.failed("Couldn't open "+at, err.Error())
		}
	})
}
