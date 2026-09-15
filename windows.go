package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/ssh"

	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/meter"
	"github.com/marrasen/gridterm/remote"
	"github.com/marrasen/gridterm/serve"
	"github.com/marrasen/gridterm/ui"
	"github.com/marrasen/gridterm/ui/term"
)

// knownWindowsFile is where the keys of gridterm windows this one has
// reached are recorded.
//
// Its own file rather than the user's ~/.ssh/known_hosts. A gridterm
// serving is not a machine they reach with ssh, and writing it there
// would put a line in a file other tools read and the user maintains
// for something else.
const knownWindowsFile = "known_windows"

// taken is another machine's gridterm, taken over from this one.
//
// It is not a *machine: a machine is an SSH host this window opens
// shells and tunnels and file transfers on, while this is another
// gridterm that opens those things for itself. The two look alike in
// the panel and are nothing alike underneath.
type taken struct {
	name string
	win  *serve.Window

	// entry is the panel row for the window itself, so one with nothing
	// open on it is still visible and still closeable.
	entry *conns.Entry
}

// openTakeOver asks which window to take over.
func (a *app) openTakeOver() error {
	f := a.newForm("Take over a window")
	// Broken into short lines by hand. A dialog is as wide as its
	// longest line and stops there, so a sentence written as one line
	// is a sentence with its end cut off.
	f.Lines = []string{
		"Work in a gridterm running on another machine.",
		"",
		"That window has to be serving, and has to have",
		"this machine's public key in its authorized_keys.",
	}
	addr := a.AddrField(f)
	key := f.AddField("Key file", a.newField("optional, or the agent's keys", 0))

	f.AddButton(ui.Button{Title: "Take over", Do: func() error {
		return a.takeOver(strings.TrimSpace(addr.Text()), strings.TrimSpace(key.Text()))
	}})
	f.AddButton(ui.Button{Title: "Cancel"})
	a.showForm(f, nil)
	return nil
}

// AddrField adds the machine field, which carries the port a serving
// window uses unless one is typed.
func (a *app) AddrField(f *ui.Form) *ui.Field {
	return f.AddField("Machine", a.newField("host[:port], port "+
		fmt.Sprint(servePort)+" unless given", 0))
}

// takeOver reaches another window and puts it on the panel.
//
// The work happens on a goroutine of its own: reaching a machine can
// stop to ask for a passphrase, or to ask whether its key is the one
// expected, and neither can be answered by the goroutine that draws.
func (a *app) takeOver(addr, keyFile string) error {
	if addr == "" {
		return errors.New("no machine to take over")
	}
	if !strings.Contains(addr, ":") {
		addr = fmt.Sprintf("%s:%d", addr, servePort)
	}
	if a.windows[addr] != nil {
		return fmt.Errorf("this window has already taken over %s", addr)
	}
	known, err := knownWindowsPath()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(a.ctx)
	waiting := &conns.Entry{
		Host:   addr,
		Kind:   conns.Terminal,
		Label:  "taking over",
		Note:   "opening",
		Reveal: func() { a.sayWaitingFor(addr) },
		Close:  func() error { cancel(); return nil },
	}
	a.registry.Add(waiting)
	a.connecting++
	a.opening[addr] = cancel

	ask := &askUser{app: a}
	go func() {
		keys, err := a.keysFor(ctx, keyFile, ask)
		var win *serve.Window
		if err == nil {
			var check ssh.HostKeyCallback
			check, err = remote.HostKeyCheck(ctx, []string{known}, ask)
			if err == nil {
				win, err = serve.Dial(ctx, serve.DialConfig{
					Addr: addr, Keys: keys, HostKey: check,
				})
			}
		}
		a.pump.post(func() {
			a.connecting--
			a.registry.Drop(waiting)
			delete(a.opening, addr)
			gaveUp := ctx.Err()
			cancel()
			if err != nil {
				a.reportError("Could not take over "+addr, err)
				return
			}
			if gaveUp != nil {
				// Given up on while the last of the handshake was
				// finishing. Nothing else knows about it.
				if err := win.Close(); err != nil {
					a.logError(err)
				}
				return
			}
			a.holdWindow(addr, win)
			if err := a.openOnWindow(addr, nil); err != nil {
				a.reportError("Could not open a terminal on "+addr, err)
			}
		})
	}()
	return nil
}

// keysFor is what to offer the other window: the key file named, if one
// was, and otherwise whatever is already unlocked and whatever the
// agent holds.
func (a *app) keysFor(ctx context.Context, keyFile string, ask remote.Ask) ([]ssh.Signer, error) {
	if keyFile != "" {
		signer, err := a.keys.Unlock(ctx, keyFile, ask)
		if err != nil {
			return nil, err
		}
		return []ssh.Signer{signer}, nil
	}
	keys := a.keys.Signers()
	// The agent's as well, which is where most people keep the key they
	// would reach any other machine with. No agent is not a failure:
	// the ring may hold one already, and the error says so if neither
	// does.
	if fromAgent, closer, err := remote.AgentKeys(); err == nil {
		keys = append(keys, fromAgent...)
		if closer != nil {
			defer closer.Close()
		}
	}
	if len(keys) == 0 {
		return nil, errors.New(
			"no keys to offer: name a key file, or add one to the SSH agent")
	}
	return keys, nil
}

// holdWindow remembers a window and puts a row on the panel for it.
func (a *app) holdWindow(addr string, win *serve.Window) *taken {
	t := &taken{name: addr, win: win}
	t.entry = &conns.Entry{
		Host:  addr,
		Kind:  conns.Terminal,
		Label: "taken over",
		Meter: &meter.Meter{},
		Close: func() error { return a.dropWindow(addr) },
	}
	a.windows[addr] = t
	a.registry.Add(t.entry)
	a.refreshServers()
	return t
}

// openOnWindow opens a terminal in the other window, drawn in a pane
// here.
func (a *app) openOnWindow(addr string, at *spot) error {
	t := a.windows[addr]
	if t == nil {
		return fmt.Errorf("this window has not taken over %s", addr)
	}
	sess, err := t.win.Open(a.lastSize[0], a.lastSize[1])
	if err != nil {
		return err
	}
	pane, err := a.openSessionTab(sess, addr, conns.Terminal, "terminal", at)
	if err != nil {
		// The session is ours and nothing else knows about it.
		_ = sess.Close()
		return err
	}
	a.paneOnWindow[pane] = t
	return nil
}

// dropWindow lets go of another window, taking the panes drawn from it.
func (a *app) dropWindow(addr string) error {
	t := a.windows[addr]
	if t == nil {
		if cancel := a.opening[addr]; cancel != nil {
			// Still on its way. Cancelling closes the connection under
			// the handshake, and the goroutine takes the row away.
			cancel()
			return nil
		}
		return nil
	}
	delete(a.windows, addr)
	a.registry.Drop(t.entry)

	// The panes first, so each is closed while the connection carrying
	// it is still there to hang up politely.
	var errs []error
	for pane, on := range a.paneOnWindow {
		if on != t {
			continue
		}
		delete(a.paneOnWindow, pane)
		errs = append(errs, a.closePane(pane))
	}
	errs = append(errs, t.win.Close())
	a.refreshServers()
	return errors.Join(errs...)
}

// closeWindows lets go of every window this one has taken over.
func (a *app) closeWindows() error {
	var errs []error
	for addr := range a.windows {
		errs = append(errs, a.dropWindow(addr))
	}
	return errors.Join(errs...)
}

// windowNames is every window this one has taken over.
func (a *app) windowNames() []string {
	out := make([]string, 0, len(a.windows))
	for addr := range a.windows {
		out = append(out, addr)
	}
	return out
}

// knownWindowsPath is where the keys of windows this one has reached
// are recorded.
func knownWindowsPath() (string, error) {
	at, err := serve.Dir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(at, 0o700); err != nil {
		return "", fmt.Errorf("could not make %s: %w", at, err)
	}
	return filepath.Join(at, knownWindowsFile), nil
}

// takenPane reports whether a pane is drawn from another window.
func (a *app) takenPane(t *term.Terminal) bool { return a.paneOnWindow[t] != nil }
