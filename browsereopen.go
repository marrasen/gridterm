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
	r.under = nil
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
	f, forgotten := r.under, r.forgotten
	r.mu.Unlock()
	if f != nil {
		return f, nil
	}
	if forgotten {
		return nil, fmt.Errorf("nothing is connected to %s", groupName(r.host))
	}

	// On the goroutine that draws, because that is where the machines
	// are. It answers when the connection is made or has failed, which
	// for one already on its way means waiting for that one rather than
	// starting a second.
	type answer struct {
		f   vfs.FS
		err error
	}
	back := make(chan answer, 1)
	r.app.pump.post(func() {
		r.app.filesystemAgain(r.host, r.at, func(f vfs.FS, err error) {
			back <- answer{f, err}
		})
	})
	select {
	case got := <-back:
		if got.err != nil {
			return nil, got.err
		}
		r.mu.Lock()
		defer r.mu.Unlock()
		if r.under != nil {
			// Something else opened one while this was waiting. One is
			// enough, and the one already in use is the one to keep.
			if got.f != nil {
				_ = got.f.Close()
			}
			return r.under, nil
		}
		r.took(got.f)
		return got.f, nil
	case <-r.app.ctx.Done():
		return nil, r.app.ctx.Err()
	}
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
func (r *reopening) Place() any { return placeOn(r.host) }

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
func (r *reopening) Close() error {
	r.mu.Lock()
	f := r.under
	r.under = nil
	r.mu.Unlock()
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
		if r.host == host {
			r.Lost()
		}
	}
}

// keepReopening remembers a filesystem so the machine going can tell
// it, and forgets the ones nothing is using.
func (a *app) keepReopening(r *reopening) {
	a.reopening = append(a.reopening, r)
}

// forgetReopening takes one off the list, for a filesystem that has
// been closed.
func (a *app) forgetReopening(f vfs.FS) {
	r, is := f.(*reopening)
	if !is {
		return
	}
	r.mu.Lock()
	r.forgotten = true
	r.mu.Unlock()
	for i, held := range a.reopening {
		if held == r {
			a.reopening = append(a.reopening[:i], a.reopening[i+1:]...)
			return
		}
	}
}
