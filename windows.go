package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"

	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/meter"
	"github.com/marrasen/gridterm/remote"
	"github.com/marrasen/gridterm/serve"
	"github.com/marrasen/gridterm/ui"
	"github.com/marrasen/gridterm/ui/term"
	"github.com/marrasen/gridterm/vfs"
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
	// name is what the window is called here: the name the server list
	// gives it, or the address when it is not saved. It is the key
	// everything else uses, the way a machine's name is.
	name string

	// addr is where it serves, which is what reaching it again needs.
	addr string

	win *serve.Window

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
	addr := f.AddField("Machine", a.newField(
		fmt.Sprintf("host[:port], port %d unless given", servePort), 0))
	key := f.AddField("Key file", a.newField("optional, or the agent's keys", 0))

	f.AddButton(ui.Button{Title: "Take over", Do: func() error {
		return a.takeOver(strings.TrimSpace(addr.Text()), strings.TrimSpace(key.Text()))
	}})
	f.AddButton(ui.Button{Title: "Cancel"})
	a.showForm(f, nil)
	return nil
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
	// What this window is called here. A saved one goes under the name
	// the user gave it, so the sidebar has one heading for it rather
	// than one for the name and another for the address.
	name := a.windowNamed(addr)
	if a.windows[name] != nil {
		return fmt.Errorf("this window has already taken over %s", name)
	}
	// Already on its way. Asked about rather than refused: waiting for
	// it is usually what the user wants.
	if d := a.opening[name]; d != nil {
		a.askAboutTheOneOnItsWay(d, name, func() { a.workOnWindow(addr, keyFile) })
		return nil
	}

	ctx, cancel := context.WithCancel(a.ctx)

	// A pane rather than a row that only says "opening". It is somewhere
	// to watch from: every step as it is tried, and whatever the far end
	// says, in full and there to copy. The same pane carries the shell
	// when there is one, so the account of how it was reached stays in
	// the scrollback above it.
	//
	// Letting go of the address is done here rather than by the closure
	// that finishes the dial: a dial that has not come back yet still
	// has to stop holding it, or nothing can try again.
	held := &dialling{cancel: cancel, names: []string{name}}
	log := newConnLog(func() { a.pump.post(func() { a.giveUp(held) }) })
	held.say = log.Say
	pane, err := a.openSessionTab(log, name, conns.Terminal, "connecting", nil)
	if err != nil {
		cancel()
		return err
	}
	a.connecting++
	a.holdNames(held)
	log.Say("taking over " + addr)

	ask := &askUser{app: a, log: log, stop: func() { a.giveUp(held) }}
	go func() {
		win, err := reachWindow(ctx, reach{
			addr: addr, keyFile: keyFile, ring: a.keys, ask: ask,
			known: a.knownWindows, agent: remote.AgentKeys,
			patience: a.reachPatience,
			saying:   log.Say,
		})
		a.pump.post(func() {
			a.connecting--
			// Read before the context is let go of on the next line,
			// which would otherwise make every window look like one the
			// user gave up on.
			gaveUp := ctx.Err()
			cancel()
			if err != nil {
				a.settle(held, false)
				if gaveUp != nil {
					a.endedAs(pane, "given up on")
					log.GaveUp()
					return
				}
				a.endedAs(pane, "not taken over")
				log.Failed(err)
				return
			}
			if gaveUp != nil {
				// Given up on while the last of the handshake was
				// finishing. Nothing else knows about it, and a failure
				// to hang up is the user's to see: it is a socket to a
				// machine that thinks somebody is working in it.
				a.settle(held, false)
				a.endedAs(pane, "given up on")
				log.GaveUp()
				if err := win.Close(); err != nil {
					a.reportError("Could not let go of "+addr, err)
				}
				return
			}
			a.holdWindow(name, addr, win)
			a.becomeWindowPane(name, pane, log)
			a.settle(held, true)
		})
	}()
	return nil
}

// workOnWindow is what a request that waited for a window being taken
// over does when it runs.
//
// A terminal on it once it has landed, rather than taking it over a
// second time: that would only report that it has been taken over
// already, which is not what the user waited for.
func (a *app) workOnWindow(addr, keyFile string) {
	name := a.windowNamed(addr)
	if a.windows[name] != nil {
		if err := a.openOnWindow(name, nil); err != nil {
			a.reportError("Could not open a terminal on "+name, err)
		}
		return
	}
	if err := a.takeOver(addr, keyFile); err != nil {
		a.reportError("Could not take over "+addr, err)
	}
}

// windowNamed is what a window serving at an address is called here.
//
// The name the server list gives it, so everything this window keeps
// about it goes under one name. An address nothing saved is its own
// name.
func (a *app) windowNamed(addr string) string {
	for _, h := range a.book.Hosts() {
		if h.Window && h.ServeAddr() == addr {
			return h.Name
		}
	}
	return addr
}

// savedWindow reports whether a name is a window in the server list.
func (a *app) savedWindow(name string) bool {
	h, ok := a.book.Lookup(name)
	return ok && h.Window
}

// becomeWindowPane hands the pane that was watching a window being taken
// over to a shell on that window.
func (a *app) becomeWindowPane(addr string, pane *term.Terminal, log *connLog) {
	t := a.windows[addr]
	if t == nil {
		a.endedAs(pane, "not taken over")
		log.Failed(fmt.Errorf("this window has not taken over %s", addr))
		return
	}
	size := pane.Size()
	sess, err := t.win.Open(size.Cols, size.Rows)
	if err != nil {
		a.endedAs(pane, "no terminal")
		log.Failed(err)
		return
	}
	log.Say("connected")
	a.paneOnWindow[pane] = t
	log.Became(sess)
}

// reach is everything reaching another window needs.
//
// The two functions are here rather than called for, so a test can hand
// over an agent and a file of its own: the real ones belong to whoever
// is running gridterm.
type reach struct {
	addr, keyFile string
	ring          *remote.Ring
	ask           remote.Ask

	// known says where the windows already reached are recorded, and
	// agent where the keys an SSH agent holds come from.
	known func() (string, error)
	agent agentKeys

	// saying is told what is being done now, for the row to show. It is
	// called from the goroutine doing it, so it hands the work to
	// whatever draws. A nil one is not called.
	saying func(what string)

	// patience is how long the other end has to get through the
	// handshake. Zero asks the serve package for its own.
	patience time.Duration
}

// say tells the row what is being done now.
func (r reach) say(what string) {
	if r.saying != nil {
		r.saying(what)
	}
}

// What a connection says it is doing, in the order it does them.
const (
	stepKeys      = "finding a key to offer"
	stepConnect   = "connecting"
	stepGivingUp  = "giving up"
	stepConnected = "connected"
)

// reachWindow does the part that must not run on the goroutine that
// draws: reading the disk, unlocking a key, and the handshake itself.
func reachWindow(ctx context.Context, r reach) (*serve.Window, error) {
	type answer struct {
		win *serve.Window
		err error
	}
	back := make(chan answer, 1)
	go func() {
		win, err := r.reach(ctx)
		back <- answer{win: win, err: err}
	}()
	select {
	case got := <-back:
		return got.win, got.err
	case <-ctx.Done():
		r.say(stepGivingUp)
		// Given up on. Every step of reaching a window is bounded, so
		// the one still running ends on its own; it is waited for here
		// rather than abandoned, because it may yet come back holding a
		// connection that nothing else would close.
		go func() {
			if got := <-back; got.win != nil {
				_ = got.win.Close()
			}
		}()
		return nil, ctx.Err()
	}
}

// reach makes the connection, step by step. It runs on a goroutine of
// reachWindow's, which is what lets giving up be answered at once
// however far this has got.
func (r reach) reach(ctx context.Context) (*serve.Window, error) {
	known, err := r.known()
	if err != nil {
		return nil, err
	}
	r.say(stepKeys)
	keys, closer, err := keysFor(ctx, r.keyFile, r.ring, r.ask, r.agent, r.saying)
	if err != nil {
		return nil, err
	}
	// Held open until the handshake is done. A signer the agent holds
	// signs over that socket, so closing it first leaves a key that can
	// be offered and cannot be used -- and one such key fails the whole
	// handshake, with the others never tried.
	if closer != nil {
		defer closer.Close()
	}

	check, err := remote.HostKeyCheck(ctx, []string{known}, r.ask)
	if err != nil {
		return nil, err
	}
	r.say(stepConnect)
	win, err := serve.Dial(ctx, serve.DialConfig{
		Addr: r.addr, Keys: keys, HostKey: check, Patience: r.patience,
		Saying: r.saying,
	})
	if err != nil {
		return nil, err
	}
	r.say(stepConnected)
	return win, nil
}

// agentKeys is where the keys an SSH agent holds come from. It is a
// parameter so a test can hand over an agent of its own: the real one
// belongs to whoever is running gridterm.
type agentKeys func() ([]ssh.Signer, io.Closer, error)

// keysFor is what to offer the other window: the key file named, if one
// was, and otherwise whatever is already unlocked and whatever the
// agent holds.
//
// The closer is the connection to the agent, which the caller closes
// once it has finished signing with what it was given. It is nil when
// there is nothing to close.
func keysFor(ctx context.Context, keyFile string, ring *remote.Ring,
	ask remote.Ask, fromAgent agentKeys, say func(string)) ([]ssh.Signer, io.Closer, error) {

	if say == nil {
		say = func(string) {}
	}
	if keyFile != "" {
		say("unlocking " + keyFile)
		signer, err := ring.Unlock(ctx, keyFile, ask)
		if err != nil {
			return nil, nil, err
		}
		return []ssh.Signer{signer}, nil, nil
	}
	keys := ring.Signers()
	var closer io.Closer
	agentErr := ring.AgentTrouble()
	if agentErr != nil {
		say("leaving the SSH agent alone: " + agentErr.Error() +
			". Forget unlocked keys to have it asked again")
	} else {
		say("asking the SSH agent what keys it holds")
		var agentSigners []ssh.Signer
		agentSigners, closer, agentErr = fromAgent()
		switch {
		case errors.Is(agentErr, remote.ErrAgentSilent):
			// Remembered, so the next window taken over does not wait
			// for the same answer. One that is not running at all is
			// not remembered: finding that out costs nothing.
			ring.AgentGaveUp(agentErr)
			say("the SSH agent: " + agentErr.Error())
		case agentErr != nil:
			say("the SSH agent: " + agentErr.Error())
		default:
			say(fmt.Sprintf("the agent holds %d keys", len(agentSigners)))
		}
		keys = append(keys, agentSigners...)
	}

	// And the key files in the usual places, which is what connecting to
	// a machine offers. A user with one key in ~/.ssh expects it to be
	// used either way.
	plain, locked, err := remote.UsualKeys()
	if err != nil {
		if closer != nil {
			_ = closer.Close()
		}
		return nil, nil, err
	}
	say(fmt.Sprintf("%d private keys in the usual places need no passphrase, %d do",
		len(plain), len(locked)))
	keys = append(keys, plain...)
	if len(keys) > 0 {
		return keys, closer, nil
	}

	// Nothing that could be read without asking. Only now is a
	// passphrase worth asking for, and only for the first key: a machine
	// with three would otherwise ask three times for a window the first
	// one would have reached.
	if len(locked) > 0 && ask != nil {
		say("unlocking " + locked[0])
		signer, err := ring.Unlock(ctx, locked[0], ask)
		if err != nil {
			if closer != nil {
				_ = closer.Close()
			}
			return nil, nil, err
		}
		return []ssh.Signer{signer}, closer, nil
	}

	if closer != nil {
		_ = closer.Close()
	}
	if agentErr != nil {
		// Why there were none, which is not always "there is no
		// agent": one that answered and then failed is a different
		// thing to go and fix.
		return nil, nil, fmt.Errorf(
			"no keys to offer, and the SSH agent could not be read: %w", agentErr)
	}
	return nil, nil, errors.New(
		"no keys to offer: name a key file, put one in ~/.ssh, or add one to the SSH agent")
}

// holdWindow remembers a window and puts a row on the panel for it.
func (a *app) holdWindow(name, addr string, win *serve.Window) *taken {
	t := &taken{name: name, addr: addr, win: win}
	note := ""
	if name != addr {
		// Which address the name stands for, because the name is the
		// user's and says nothing about where it is.
		note = addr
	}
	t.entry = &conns.Entry{
		Host:   name,
		Kind:   conns.Terminal,
		Label:  "taken over",
		Note:   note,
		Meter:  &meter.Meter{},
		Reveal: func() { a.revealWindow(t) },
		Close:  func() error { return a.dropWindow(name) },
	}
	a.windows[name] = t
	a.registry.Add(t.entry)
	a.refreshServers()

	// A window that quits at the far end is still held here, saying it
	// is taken over, until something notices. Nothing else here would.
	go func() {
		_ = win.Wait()
		a.pump.post(func() { a.windowDied(t) })
	}()
	return t
}

// windowDied is called when the other window has gone by itself.
func (a *app) windowDied(t *taken) {
	if a.windows[t.name] != t {
		// Already let go of from here, and its row with it.
		return
	}
	if err := a.dropWindow(t.name); err != nil {
		a.reportError("Trouble letting go of "+t.name, err)
	}
}

// revealWindow puts one of a window's panes in front of the user.
//
// A window with nothing open on it has nothing to show, so nothing
// happens: there is no window for a connection itself.
func (a *app) revealWindow(t *taken) {
	for pane, on := range a.paneOnWindow {
		if on == t {
			a.focus(pane)
			return
		}
	}
}

// serveFiles gives a client the files of this machine, as SFTP on the
// channel it was handed.
//
// It runs on a goroutine of the server's and touches nothing the window
// holds.
func (a *app) serveFiles(ch io.ReadWriteCloser) error {
	// The channel is not this function's to close: whoever handed it
	// over closes it once, and an SFTP server closes what it was given
	// as it goes. Left alone, the two would close it twice and the
	// second would have to be told not to mind.
	// Where the user lives, so a pane on this machine opens there. A
	// file session starts in the serving process's own directory
	// otherwise, which is wherever the window happened to be launched
	// from.
	options := []sftp.ServerOption{
		// A Windows machine has drives rather than one root. Without
		// this, "/" is whatever drive this process happens to be on and
		// the others cannot be reached by going up.
		sftp.WindowsRootEnumeratesDrives(),
	}
	if home, err := os.UserHomeDir(); err == nil {
		options = append(options, sftp.WithServerWorkingDirectory(home))
	}
	srv, err := sftp.NewServer(keptOpen{ch}, options...)
	if err != nil {
		return fmt.Errorf("could not serve the files of this machine: %w", err)
	}
	served := srv.Serve()
	if errors.Is(served, io.EOF) {
		served = nil
	}
	// Closed after serving, and its failure said: it lets go of every
	// file the session left open.
	if err := errors.Join(served, srv.Close()); err != nil {
		return fmt.Errorf("serving the files of this machine: %w", err)
	}
	return nil
}

// keptOpen is a stream whose close does nothing, for handing to
// something that closes what it is given when the caller needs it after.
type keptOpen struct{ io.ReadWriteCloser }

func (keptOpen) Close() error { return nil }

// windowFiles is the filesystem of the machine a window taken over is
// on, as a browser pane works on it.
func (a *app) windowFiles(addr string) (vfs.FS, error) {
	t := a.windows[addr]
	if t == nil {
		return nil, fmt.Errorf("this window has not taken over %s", addr)
	}
	ch, err := t.win.Files()
	if err != nil {
		return nil, err
	}
	client, err := sftp.NewClientPipe(ch, ch)
	if err != nil {
		// Whatever the far end said about a session it could not start
		// is the only account of it: the failure happened over there.
		// It closes the channel on every failure of its own, so that
		// account has arrived or is about to.
		if why := ch.Said(); why != "" {
			return nil, fmt.Errorf("could not read the files of %s: %s", addr, why)
		}
		return nil, fmt.Errorf("could not read the files of %s: %w", addr, err)
	}
	return vfs.NewSFTP(addr, client, func() error {
		return closeFilesOver(client, ch)
	}), nil
}

// closeFilesOver ends a file session on a window taken over.
//
// Closing the SFTP client sends an end of file and then waits for the
// far end to close the channel. A window that has stopped answering
// never will, and this runs on the goroutine that draws, so the wait is
// bounded: after that the channel is closed from here, which is what
// lets go.
func closeFilesOver(client, ch io.Closer) error {
	done := make(chan error, 1)
	go func() { done <- client.Close() }()

	var errs []error
	select {
	case err := <-done:
		errs = append(errs, err)
	case <-time.After(filesGrace):
		errs = append(errs, ch.Close())
		// Now that the channel has gone, the client's own close can
		// finish. Waited for rather than abandoned: it holds a
		// goroutine until it does.
		errs = append(errs, <-done)
	}
	// The client closes the channel as it goes, so this is the path
	// where it never got that far.
	errs = append(errs, ch.Close())

	for i, err := range errs {
		// A channel already closed says so, which is agreement rather
		// than a failure: this closes it once itself and once through
		// the client.
		if errors.Is(err, io.EOF) {
			errs[i] = nil
		}
	}
	return errors.Join(errs...)
}

// filesGrace is how long letting go of a window waits for its file
// session to say goodbye before closing the channel from here.
const filesGrace = 250 * time.Millisecond

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

// watchingPane is the pane already watching something on a window taken
// over, or nil when nothing is.
func (a *app) watchingPane(what remoteKey) *term.Terminal {
	for pane, have := range a.watching {
		if have == what {
			return pane
		}
	}
	return nil
}

// farSize is how big the screen a pane is watching is now, or zero when
// the window over there no longer says it has it open.
//
// Asked for afresh rather than remembered: the far end is redrawn at
// its own size whenever that changes, and a size kept from the moment
// the pane opened would go on being shown long after it stopped being
// true.
func (a *app) farSize(what remoteKey) (cols, rows int) {
	open, ok := a.openOver(what)
	if !ok {
		return 0, 0
	}
	return open.Cols, open.Rows
}

// dropWindow lets go of another window, taking the panes drawn from it.
func (a *app) dropWindow(addr string) error {
	t := a.windows[addr]
	if t == nil {
		if d := a.opening[addr]; d != nil {
			// Still on its way. Cancelling closes the connection under
			// the handshake, and the goroutine takes the row away. The
			// address is let go of here rather than there, so another
			// attempt can have it at once.
			a.giveUp(d)
			return nil
		}
		return nil
	}
	delete(a.windows, addr)
	a.registry.Drop(t.entry)

	// A file pane reading through this window is reading through a
	// connection that is about to go. It is taken away here, because
	// nothing else would: a pane does not end by itself the way a shell
	// does.
	errs := []error{a.closeFilesOn(addr)}

	// Then the terminal panes, each closed while the connection
	// carrying it is still there to hang up politely.
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

// closeWindows hangs up on every window this one took over, for a
// window that is shutting down.
//
// The connections and nothing else. The panes have gone by then, and a
// window on its way out has no tree left to take one out of and nobody
// to show a rebuilt menu to.
func (a *app) closeWindows() error {
	var errs []error
	for addr, t := range a.windows {
		delete(a.windows, addr)
		errs = append(errs, t.win.Close())
	}
	return errors.Join(errs...)
}

// isWindow reports whether a name is a window this one has taken over.
func (a *app) isWindow(name string) bool { return a.windows[name] != nil }

// knownWindows is where this window records the keys of the windows it
// has reached. A test points it somewhere of its own.
func (a *app) knownWindows() (string, error) {
	if a.knownWindowsAt != "" {
		return a.knownWindowsAt, nil
	}
	return knownWindowsPath()
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

// attachHere opens a pane on what the window taken over already has
// running, rather than starting something new there.
func (a *app) attachHere(what remoteKey, at *spot) error {
	t := a.windows[what.window]
	if t == nil {
		return fmt.Errorf("this window has not taken over %s", what.window)
	}
	// Already watching it: the pane comes forward rather than a second
	// one opening on the same program, which would be two panes typing
	// into one shell.
	if pane := a.watchingPane(what); pane != nil {
		a.focus(pane)
		return nil
	}
	open, ok := a.openOver(what)
	if !ok {
		return fmt.Errorf("%s no longer has that open", what.window)
	}
	if !open.HasScreen() {
		// Asking anyway opened a pane that showed the refusal, and the
		// pane was a row of its own: every attempt left another one
		// behind.
		return fmt.Errorf("%q on %s has no screen to watch: it is %s, not a terminal",
			open.Label, what.window, strings.ToLower(open.Kind))
	}
	sess, err := t.win.Attach(open, a.lastSize[0], a.lastSize[1])
	if err != nil {
		return err
	}
	pane, err := a.openSessionTab(sess, what.window, conns.Terminal, open.Label, at)
	if err != nil {
		// The session is ours and nothing else knows about it. Failing
		// to let go of it leaves the pane over there being watched by
		// nobody, which is worth saying along with why this failed.
		return errors.Join(err, sess.Close())
	}
	a.paneOnWindow[pane] = t
	a.watching[pane] = what
	return nil
}

// openOver is what a window taken over says about one of the things it
// has open, and whether it still has it.
func (a *app) openOver(what remoteKey) (serve.Open, bool) {
	t := a.windows[what.window]
	if t == nil || what.id == "" {
		return serve.Open{}, false
	}
	for _, open := range t.win.Opens() {
		if open.ID == what.id {
			return open, true
		}
	}
	return serve.Open{}, false
}
