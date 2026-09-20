package main

import (
	"errors"
	"fmt"
	"time"

	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/ui/files"
	"github.com/marrasen/gridterm/vfs"
)

// followEvery is how often a file being followed is asked whether it has
// changed. Often enough to read as live, seldom enough that a file on a
// machine at the far end is not asked about on every frame.
const followEvery = 300 * time.Millisecond

// reader is one file open in the window: the pane, the row it has on the
// sidebar, and what is known about the file it is following.
type reader struct {
	row *conns.Entry

	// on is the filesystem the file is read through, which belongs to
	// the browser pane the reader was opened from.
	on vfs.FS
	at string

	// asked is when the file was last asked whether it had changed, and
	// was is what it said then. checking says a question is out.
	asked    time.Time
	was      vfs.Entry
	knowIt   bool
	checking bool
}

// openReader puts a pane in the window showing one file. expect is how
// big it was listed as, or zero when nobody knows.
//
// The filesystem is the one the browser pane it was opened from is
// reading, and the reader takes a hold on it. A browser pane closed
// while a reader is still on its machine does not take the connection
// with it: the reader is still using it, and rereading down a session
// somebody else closed is how a pane ends up showing a stale file and
// an error nobody can act on.
func (a *app) openReader(f vfs.FS, host, path, name string, follow bool, expect int64) error {
	if f == nil {
		return errors.New("there is no filesystem to read that file through")
	}
	r := files.NewReader(name, path)
	r.Style = a.paneStyle()
	// How big the listing said it is, so the pane says what it is
	// waiting for rather than looking empty while a file on a machine
	// far away comes down the wire.
	r.Expect = expect
	// Off the drawing goroutine, and back onto it with the answer: a
	// file on a machine with a long way to go would otherwise stop the
	// window while it was read.
	// Held for the length of the read as well as for the life of the
	// pane: a reader closed while its read is out would otherwise let go
	// of the last hold and close the session under it.
	r.Read = func(then func([]string, bool, error)) {
		a.holdFS(f)
		go func() {
			lines, cut, err := files.ReadFile(f, path)
			a.pump.post(func() {
				a.doneWithFS(f)
				then(lines, cut, err)
			})
		}()
	}
	if files.IsPicture(name) {
		r.ReadPic = func(then func(files.Pic, error)) {
			a.holdFS(f)
			go func() {
				pic, err := files.ReadPicture(f, path, mostPictureSide)
				a.pump.post(func() {
					a.doneWithFS(f)
					then(pic, err)
				})
			}()
		}
	}
	r.OnCopy = a.clip.set
	r.OnClose = func() {
		a.pump.post(func() {
			if err := a.closePane(r); err != nil {
				a.reportError("Could not close the reader", err)
			}
		})
	}
	if err := a.placePane(r); err != nil {
		return err
	}
	row := &conns.Entry{
		Host:   host,
		Kind:   readerRowKind(follow),
		Label:  name,
		Reveal: func() { a.focus(r) },
		Close:  func() error { return a.closePane(r) },
	}
	if a.readers == nil {
		a.readers = map[*files.Reader]*reader{}
	}
	a.readers[r] = &reader{row: row, on: f, at: path}
	a.holdFS(f)
	a.registry.Add(row)
	// Following before the first read, so a file that is already long
	// opens at its end rather than at its top and then jumping.
	r.Follow(follow)
	r.Open()
	return nil
}

// readerRowKind is the sidebar kind for a reader, Follow while it is
// keeping up with the file.
func readerRowKind(follow bool) conns.Kind {
	if follow {
		return conns.Follow
	}
	return conns.Reader
}

// readFileFrom opens a reader on whatever the browser has picked out.
func (a *app) readFileFrom(p *files.Pane, e vfs.Entry, follow bool) error {
	f := p.FS()
	if f == nil {
		return errors.New("that pane is on no machine")
	}
	if e.IsDir() && !e.IsLink() {
		// Enter goes into a directory rather than reading one, so this
		// is something else asking. A pane that opened and said "is a
		// directory" would be a pane for nothing.
		return fmt.Errorf("%s is a directory", e.Name)
	}
	at := vfs.Join(f, p.At(), e.Name)
	return a.openReader(f, a.hostOfPane(p), at, e.Name, follow, e.Size)
}

// hostOfPane is the machine a browser pane is reading, as the sidebar
// calls it, so a reader opened from it sits under the same heading.
func (a *app) hostOfPane(p *files.Pane) string {
	if a.files != nil {
		if row := a.files.rows[p]; row != nil {
			return row.Host
		}
	}
	return conns.Local
}

// dropReader takes a reader's row off the sidebar, for a pane that has
// been closed, and lets go of the filesystem it was reading through.
func (a *app) dropReader(r *files.Reader) error {
	held := a.readers[r]
	if held == nil {
		return nil
	}
	a.registry.Drop(held.row)
	delete(a.readers, r)
	return a.letGoFS(held.on)
}

// doneWithFS lets go of a hold taken for one read, and says so when the
// close fails: it is a session on a connection, and one that will not
// close is worth hearing about.
func (a *app) doneWithFS(f vfs.FS) {
	if err := a.letGoFS(f); err != nil {
		a.reportError("Could not close the connection the file was read through", err)
	}
}

// holdFS says a reader is using a filesystem, so the browser letting go
// of it does not close it.
func (a *app) holdFS(f vfs.FS) {
	if f == nil {
		return
	}
	if a.fsHeld == nil {
		a.fsHeld = map[vfs.FS]int{}
	}
	a.fsHeld[f]++
}

// letGoFS says a reader has finished with a filesystem, and closes it
// when it was the last one and the browser has already let go.
func (a *app) letGoFS(f vfs.FS) error {
	if f == nil || a.fsHeld[f] == 0 {
		return nil
	}
	a.fsHeld[f]--
	if a.fsHeld[f] > 0 {
		return nil
	}
	delete(a.fsHeld, f)
	if !a.fsGone[f] {
		// The browser still has it. Closing it now would take the
		// machine out from under a pane that is still reading it.
		return nil
	}
	delete(a.fsGone, f)
	return a.releaseFS(f)
}

// followReaders asks each file being followed whether it has changed,
// and rereads the ones that have.
//
// Asked rather than watched: a file on a machine at the far end has
// nothing to watch it with, and a question every three hundred
// milliseconds is cheap on either. The asking happens off the goroutine
// that draws, the same as the reading.
func (a *app) followReaders(now time.Time) {
	for r, held := range a.readers {
		// Following goes on and off from the pane, so keep the row in step.
		held.row.Kind = readerRowKind(r.Following())
		if !r.Following() || held.checking || r.Busy() {
			continue
		}
		if now.Sub(held.asked) < followEvery {
			continue
		}
		held.asked = now
		held.checking = true
		on, at, pane := held.on, held.at, r
		a.holdFS(on)
		go func() {
			e, err := on.Stat(at)
			a.pump.post(func() {
				a.doneWithFS(on)
				a.fileChanged(pane, e, err)
			})
		}()
	}
}

// fileChanged takes the answer to that question.
//
// A file that cannot be asked about is one the reader can no longer
// trust, so the reason goes on the pane. Nothing else would ever say it:
// a reader that keeps failing this question never issues another read,
// and would go on showing a file that has been taken away.
func (a *app) fileChanged(r *files.Reader, e vfs.Entry, err error) {
	held := a.readers[r]
	if held == nil {
		// Closed while the question was out.
		return
	}
	held.checking = false
	if err != nil {
		r.Failed(err)
		return
	}
	// A file that has not changed is read again all the same when the
	// last read failed, or one blip would leave the error on the pane
	// until somebody wrote to the file.
	if held.knowIt && e.Size == held.was.Size && e.Mod.Equal(held.was.Mod) && r.Err() == nil {
		return
	}
	// Written down only once a read has gone out. A read dropped
	// because another was still running would otherwise leave the window
	// thinking it had already read this version.
	if !r.Open() {
		return
	}
	held.was, held.knowIt = e, true
}

// readerNote is what a reader's row says beside its name: where in the
// file it is, or why the file would not read.
func (a *app) readerNote(r *files.Reader) string {
	if err := r.Err(); err != nil {
		return err.Error()
	}
	if r.Busy() {
		return "reading"
	}
	if r.ShowsAPicture() {
		// A picture has no lines to count, so the row says how big it is.
		return r.Where()
	}
	if n := r.Lines(); n > 0 {
		of := fmt.Sprintf("%d lines", n)
		if r.Cut() {
			of += "+"
		}
		return of
	}
	return ""
}
