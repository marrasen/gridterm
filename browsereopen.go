package main

import (
	"fmt"
	"io"
	"io/fs"
	"sync"

	"github.com/marrasen/gridterm/vfs"
)

// reopening is a filesystem that opens itself again when the connection
// under it has gone.
//
// A pane of the file manager used to be closed when its machine
// dropped, because the filesystem was built out of that connection and
// every call on it failed afterwards. This holds the machine instead of
// the session, so the pane stays and the next thing the user does opens
// the machine again.
//
// It knows its connection has gone because it is told, not because it
// reads the error. Telling "the connection went" from "there is no such
// file" means classifying whatever sftp, ssh and the net packages
// happen to say that day, and getting it wrong either reconnects over a
// real answer or leaves a dead session in place.
type reopening struct {
	app  *app
	host string

	// at is the step the machine was reached by when this filesystem
	// was made, for opening it again when the server list has no route
	// to it. A machine connected to from a typed target is on no list
	// and still has to be reachable: this is what dialAgainFor keeps
	// for a terminal, kept here for the same reason.
	at step

	mu sync.Mutex
	// under is what is open on the machine now, and nil when nothing is.
	under vfs.FS
	// gone counts the times the connection under this one has been
	// told to have gone, so an answer to a reconnect that lands after
	// one is not stored.
	//
	// A machine can drop again while it is being opened: the connection
	// is made, the filesystem is opened on it, the machine goes, and
	// what comes back is a session that was dead before anything used
	// it. Counting says that happened; comparing what is open cannot,
	// because nothing is open either way.
	gone int
	// forgotten says the pane this belongs to has been closed, so this
	// opens nothing more.
	//
	// A finished copy holds the filesystems it ran on, and one of those
	// outlives the pane it came from. Without this, a job pane drawn
	// after its machine went reached through that copy and opened a
	// connection nobody had asked for, which put a row on the sidebar
	// out of nothing.
	forgotten bool
	// name, roots and sep are what the last filesystem said about the
	// machine. They are answered from here because the goroutine that
	// draws asks for them, and it must not go to the network.
	name  string
	roots []string
	sep   byte
}

// newReopening wraps the first filesystem opened on a machine.
func newReopening(a *app, host string, at step, first vfs.FS) *reopening {
	r := &reopening{app: a, host: host, at: at, sep: '/'}
	r.took(first)
	return r
}

// took remembers a filesystem and what it says about the machine.
func (r *reopening) took(f vfs.FS) {
	r.under = f
	if f == nil {
		return
	}
	// The name this one already had wins: a machine renamed while its
	// connection was gone is called what the window calls it now, not
	// what the far end says it was.
	if r.name == "" {
		r.name = f.Name()
	}
	if under, is := f.(renamedFS); is && r.name != f.Name() {
		under.Renamed(r.name)
	}
	r.roots, r.sep = f.Roots(), f.Sep()
}

// Lost says the connection under this one has gone, so the next call
// opens the machine again.
//
// The session is not closed: it went with the transport, and asking a
// dead channel to close is what put "close SFTP: EOF" in front of the
// user in the first place.
func (r *reopening) Lost() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.under, r.gone = nil, r.gone+1
}

// on hands the filesystem to do, opening the machine again first when
// there is nothing open.
//
// The lock is not held while the machine is being opened: that waits on
// the goroutine that draws, and that goroutine asks this one for Roots
// and Sep every frame.
func (r *reopening) on(do func(vfs.FS) error) error {
	f, err := r.ready()
	if err != nil {
		return err
	}
	return do(f)
}

// ready is what is open on the machine, opening it if nothing is.
func (r *reopening) ready() (vfs.FS, error) {
	r.mu.Lock()
	f, forgotten, host, gone := r.under, r.forgotten, r.host, r.gone
	r.mu.Unlock()
	if f != nil {
		return f, nil
	}
	if forgotten {
		return nil, notConnected(host)
	}

	// On the goroutine that draws, because that is where the machines
	// are. It answers when the connection is made or has failed, which
	// for one already on its way means waiting for that one rather than
	// starting a second.
	type answer struct {
		f vfs.FS
		// on is how the machine was reached this time, said by the one
		// that opened it rather than looked up here. The step this
		// kept is the only record of where the machine is when no list
		// has a route to it, and one left as it was when the pane
		// opened is a record of where it used to be.
		on  step
		err error
	}
	back := make(chan answer, 1)
	r.app.pump.post(func() {
		// The name and the step are read here rather than above,
		// because a rename can land between the two and this runs
		// where renames do.
		name := r.calledNow("")
		if name == "" {
			// The pane was closed, or its server has gone from the
			// list. Nothing else is this machine: a server saved since
			// under its name is another one.
			err := notConnected(host)
			if id := r.step().id; id != "" {
				if _, saved := r.app.book.NameOf(id); !saved {
					err = removedServer(host)
				}
			}
			back <- answer{err: err}
			return
		}
		r.app.filesystemAgain(name, r.step(), r.calledNow,
			func(f vfs.FS, on step, err error) {
				back <- answer{f, on, err}
			})
	})
	select {
	case got := <-back:
		if got.err != nil {
			return nil, got.err
		}
		// spare is what came back and is not wanted. It is closed after
		// the lock goes, because closing a session waits for the far
		// end and the goroutine that draws takes this lock every frame
		// to ask what the machine is called.
		var spare vfs.FS
		f, err := func() (vfs.FS, error) {
			r.mu.Lock()
			defer r.mu.Unlock()
			if r.forgotten || r.gone != gone {
				// The pane was closed, or the machine went again,
				// while this was on its way. What came back belongs to
				// nobody, so it is closed rather than stored: a session
				// kept here that nothing will ever call is one the far
				// end holds open for the life of the window.
				spare = got.f
				return nil, notConnected(r.host)
			}
			if r.under != nil {
				// Something else opened one while this was waiting. One
				// is enough, and the one already in use is the one to
				// keep.
				spare = got.f
				return r.under, nil
			}
			if got.on.cfg.Host != "" {
				// The name this one goes by wins, the way it does for
				// what the machine is called: one renamed while its
				// connection was being made is called what the window
				// calls it now, not what it was called when the dial
				// started.
				got.on.name = r.host
				r.at = got.on
			}
			r.took(got.f)
			return got.f, nil
		}()
		if spare != nil {
			_ = spare.Close()
		}
		return f, err
	case <-r.app.ctx.Done():
		return nil, r.app.ctx.Err()
	}
}

// calledNow is what this filesystem's machine goes by now, and empty
// when nothing is using this any more or its server has left the list.
//
// A saved server is asked for by its id, and the pane is filed under
// the name the list gives it now if it is not already. A machine on no
// list goes by the name the window gave it, which a rename cannot touch.
// The name the dial knows is ignored: this one is at least as new.
//
// The empty answer is how a pane closed while its read waited stops
// the machine being opened for it. A connection nobody asked for puts
// a row on the sidebar out of nothing, and the name this would ask
// under is one the window has stopped following renames for.
//
// On the goroutine that draws, which is where renames happen.
func (r *reopening) calledNow(string) string {
	r.mu.Lock()
	forgotten, host, id := r.forgotten, r.host, r.at.id
	r.mu.Unlock()
	if forgotten {
		return ""
	}
	if id == "" {
		return host
	}
	now, saved := r.app.book.NameOf(id)
	if !saved {
		return ""
	}
	if now != host {
		r.app.followSaved(host, now, id)
	}
	return r.Host()
}

// Name is what the panel calls the machine.
func (r *reopening) Name() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.name
}

// Place is the machine, not the connection.
//
// Which is what Place is for -- "a value equal for two filesystems on
// one machine and for nothing else" -- and what it was not: it was the
// connection, so two panes on one machine stopped being the same place
// the moment either of them opened a new one, and a move between them
// quietly became a copy.
func (r *reopening) Place() any {
	r.mu.Lock()
	defer r.mu.Unlock()
	return placeOn(r.host)
}

// step is how the machine was reached, for work that has to open it
// again and has only this to go on.
func (r *reopening) step() step {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.at
}

// Host is the machine this is filed under, which a rename changes.
func (r *reopening) Host() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.host
}

// renamedHost says the machine this is filed under is called something
// else now.
//
// The name is what ties this to a machine: to the walk that tells it
// the connection has gone, and to the route it opens the machine by.
// One left under the old name would never be told, so the pane would
// keep calling a dead session -- which is the dialog this whole file
// exists to take away -- and opening it again would dial under a name
// the window no longer uses, putting a second machine on the sidebar.
func (r *reopening) renamedHost(was, now string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.host != was {
		return
	}
	r.host = now
	if r.at.name == was {
		r.at.name = now
	}
}

// notConnected is what a call on a filesystem whose machine is not
// there is answered with.
func notConnected(host string) error {
	return fmt.Errorf("nothing is connected to %s", groupName(host))
}

// removedServer is what a call on a filesystem or a piece of work is
// answered with when the saved server it is on has been removed.
//
// Not notConnected: a server saved since under the same name may well be
// connected, and this is not on that one.
func removedServer(host string) error {
	return fmt.Errorf("%s was removed from the server list", groupName(host))
}

// placeOn names a machine as a filesystem's place.
type placeOn string

// Roots and Sep are answered from what the machine last said, because
// the goroutine that draws asks for them.
func (r *reopening) Roots() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.roots
}

func (r *reopening) Sep() byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.sep
}

// Home is where a pane starts, which is the first thing asked of a
// machine that has just been opened again.
func (r *reopening) Home() (string, error) {
	var out string
	err := r.on(func(f vfs.FS) error {
		var err error
		out, err = f.Home()
		return err
	})
	return out, err
}

func (r *reopening) ReadDir(path string) ([]vfs.Entry, error) {
	var out []vfs.Entry
	err := r.on(func(f vfs.FS) error {
		var err error
		out, err = f.ReadDir(path)
		return err
	})
	return out, err
}

func (r *reopening) Stat(path string) (vfs.Entry, error) {
	var out vfs.Entry
	err := r.on(func(f vfs.FS) error {
		var err error
		out, err = f.Stat(path)
		return err
	})
	return out, err
}

func (r *reopening) Open(path string) (io.ReadCloser, error) {
	var out io.ReadCloser
	err := r.on(func(f vfs.FS) error {
		var err error
		out, err = f.Open(path)
		return err
	})
	return out, err
}

func (r *reopening) Create(path string, mode fs.FileMode) (io.WriteCloser, error) {
	var out io.WriteCloser
	err := r.on(func(f vfs.FS) error {
		var err error
		out, err = f.Create(path, mode)
		return err
	})
	return out, err
}

func (r *reopening) Mkdir(path string, mode fs.FileMode) error {
	return r.on(func(f vfs.FS) error { return f.Mkdir(path, mode) })
}

func (r *reopening) Symlink(target, path string) error {
	return r.on(func(f vfs.FS) error { return f.Symlink(target, path) })
}

func (r *reopening) Remove(path string) error {
	return r.on(func(f vfs.FS) error { return f.Remove(path) })
}

func (r *reopening) Rename(from, to string) error {
	return r.on(func(f vfs.FS) error { return f.Rename(from, to) })
}

func (r *reopening) Chmod(path string, mode fs.FileMode) error {
	return r.on(func(f vfs.FS) error { return f.Chmod(path, mode) })
}

// Renamed says the machine is called something else now.
//
// The browser asks for this by type assertion, which is the other
// reason this wrapper has to be the outermost one: an assertion
// against the archives wrapper would find nothing, and a machine
// renamed would leave its panes saying the old name for ever.
func (r *reopening) Renamed(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.name = name
	if under, is := r.under.(renamedFS); is {
		under.Renamed(name)
	}
}

// Close lets go of what is open, and opens nothing.
//
// Closing is also how the window stops holding this. A job opens
// filesystems for itself and closes them when it stops, on a goroutine
// of its own and without going through the browser, so leaving the
// forgetting to the browser left those on the window's list for ever --
// growing, and each one still able to open a machine nobody asked for.
func (r *reopening) Close() error {
	r.mu.Lock()
	f := r.under
	r.under, r.forgotten = nil, true
	r.mu.Unlock()
	// Posted, because the list belongs to the goroutine that draws and
	// this is closed on whichever one let go of it.
	r.app.pump.post(func() { r.app.dropReopening(r) })
	if f == nil {
		return nil
	}
	return f.Close()
}

// lostTheMachine tells every filesystem on a machine that its
// connection has gone.
//
// This is the walk that used to close the panes. What it does now is
// say so, which is all the panes need: the next thing the user asks
// for opens the machine again.
func (a *app) lostTheMachine(host string) {
	for _, r := range a.reopening {
		if r.Host() == host {
			r.Lost()
		}
	}
}

// renamedTheMachine tells every filesystem filed under a machine that
// it is called something else now: every one when id is empty, and the
// ones on that saved server when it is not.
//
// This is the whole of a rename for these, and it has to reach the ones
// no pane holds as well: a job opens filesystems for itself, and one of
// those outlives the pane it was opened from.
func (a *app) renamedTheMachine(was, now, id string) {
	for _, r := range a.reopening {
		if id == "" || r.step().id == id {
			r.renamedHost(was, now)
		}
	}
}

// reopeningOn is a filesystem the window holds for a machine, and nil
// when it holds none.
func (a *app) reopeningOn(host string) *reopening {
	for _, r := range a.reopening {
		if r.Host() == host {
			return r
		}
	}
	return nil
}

// keepReopening remembers a filesystem so the machine going can tell
// it, and forgets the ones nothing is using.
func (a *app) keepReopening(r *reopening) {
	a.reopening = append(a.reopening, r)
}

// dropReopeningFS takes a filesystem off the window's list, for one
// being closed on this goroutine.
//
// Closing takes it off the list by itself, but on the next frame,
// because a filesystem is closed on whichever goroutine let go of it.
// One let go of here is off the list before anything else is asked.
func (a *app) dropReopeningFS(f vfs.FS) {
	if r, is := f.(*reopening); is {
		a.dropReopening(r)
	}
}

// dropReopening takes one off the window's list.
func (a *app) dropReopening(r *reopening) {
	for i, held := range a.reopening {
		if held == r {
			a.reopening = append(a.reopening[:i], a.reopening[i+1:]...)
			return
		}
	}
}
