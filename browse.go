package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/jobs"
	"github.com/marrasen/gridterm/meter"
	"github.com/marrasen/gridterm/remote"
	"github.com/marrasen/gridterm/shells"
	"github.com/marrasen/gridterm/ui"
	"github.com/marrasen/gridterm/ui/files"
	"github.com/marrasen/gridterm/vfs"
)

// browser is the window's file manager: any number of panes side by
// side, with a row on the sidebar for each of them.
//
// One manager rather than one per pair of machines. A pane is opened
// from the plus on a machine's row, and the user puts as many in as they
// want: tab along them, and copy from the one with the keys to the next.
type browser struct {
	view *files.Browser

	// rows is the sidebar row for each pane, so a manager touching
	// several machines is shown under each of them.
	rows map[*files.Pane]*conns.Entry

	// far is the machine of a window taken over that a pane reads, for a
	// pane whose bytes go through that window. A pane on this machine or
	// on a window's own disk is not in it.
	far map[*files.Pane]remoteHostKey
}

// openFilesOn puts a pane on a machine, connecting to it first when
// nothing is connected to it yet.
func (a *app) openFilesOn(host string) error { return a.openFilesAt(host, a.oneFolderOn(host)) }

// openFilesAt opens a pane of the file manager on a machine, at a folder
// of its own or at whatever the machine calls home.
func (a *app) openFilesAt(host, at string) error {
	on := a.about(host)
	if on.toTakeOver() {
		return fmt.Errorf("connect to %s first: it is a gridterm window, "+
			"and its files come over that connection", on.name)
	}
	// Nothing to read files over yet, so the connection is made first and
	// the pane opens when it answers, the way "Terminal" does. Under the
	// server list's own spelling, for the reason openTerminalOn gives.
	if on.saved && on.kind != hostHere && on.window == nil && on.machine == nil {
		return a.connectAndBrowse(on.spelling, at)
	}
	// A window's pane goes under the name holding the connection, which
	// is not always the name asked about, so the sidebar keeps one
	// heading for it.
	name := on.name
	if t := on.window; t != nil {
		name = t.name
	}
	return a.browseOn(name, at)
}

// browseOn puts a pane on a machine that can be read now, starting the
// file manager when there is not one yet. at is where the pane opens,
// and empty is home.
func (a *app) browseOn(name, at string) error {
	f, err := a.filesystem(name)
	if err != nil {
		return err
	}
	return a.browseWith(f, name, remoteHostKey{}, at)
}

// openFilesFar puts a pane on a machine a window taken over is connected
// to, read through that window.
// The key comes from a.current, which only ever names a window the panel
// drew a row for, so the window is never nil here.
func (a *app) openFilesFar(key remoteHostKey) error {
	t := key.window
	if !a.windows.holds(t) {
		return fmt.Errorf("this window has let go of %s", t.name)
	}
	f, err := a.windowFilesOn(t, key.host)
	if err != nil {
		return err
	}
	// Under the window's name, the way the window's own file pane is:
	// the connection carrying it is the window's, so the pane goes when
	// the window does.
	return a.browseWith(f, t.name, key, "")
}

// browseWith puts a pane holding a filesystem in the manager, under the
// name given.
//
// far is the machine of a window taken over that the pane reads, and is
// empty for a pane on a machine this window reaches itself.
func (a *app) browseWith(f vfs.FS, name string, far remoteHostKey, at string) error {
	if a.files == nil {
		if err := a.openFileManager(); err != nil {
			return errors.Join(err, f.Close())
		}
	}
	b := a.files
	p := a.newPane(f, b)
	if !b.view.Add(p) {
		// Nothing puts a pane in twice, so this is the window having
		// lost track of what it holds. A row on the sidebar for a pane
		// nothing is showing is worse than no pane at all.
		return errors.Join(errors.New("the file manager refused the pane"), f.Close())
	}
	row := a.browserRow(p, name)
	b.rows[p] = row
	if far.window != nil {
		b.far[p] = far
	}
	a.registry.Add(row)
	a.focus(p)
	a.relayout()

	// Somewhere to start. Asking a machine where home is takes as long
	// as anything else it is asked, so it happens off this goroutine and
	// the pane fills in when the answer arrives.
	a.startAt(p, at)
	return nil
}

// openFileManager puts an empty file manager in the window.
func (a *app) openFileManager() error {
	b := &browser{
		rows: map[*files.Pane]*conns.Entry{},
		far:  map[*files.Pane]remoteHostKey{},
	}
	b.view = files.NewBrowser()
	// The dividers and the bar of keys are drawn in the panes' own
	// colours, so both read as part of the manager.
	b.view.Style = a.paneStyle()
	a.wireBrowser(b)
	if err := a.placePane(b.view); err != nil {
		return err
	}
	a.files = b
	return nil
}

// startAt opens a pane on the user's own directory.
//
// Until it has one a pane is on no directory at all: the keys have
// nothing to act on, and a path built from nowhere is a relative one,
// which would land in whatever directory gridterm itself was started in.
func (a *app) startAt(p *files.Pane, at string) {
	if at != "" {
		// A folder saved on the server, and the reason when it cannot be
		// read. The pane stays where it is rather than being moved
		// somewhere it was not asked for.
		p.OpenThen(at, func(err error) {
			if err != nil {
				a.reportForPane(p, "Could not open "+at+" on "+p.FS().Name(), err)
			}
		})
		return
	}
	f := p.FS()
	go func() {
		home, err := f.Home()
		a.pump.post(func() {
			if err != nil {
				a.reportForPane(p, "Could not open "+f.Name(), err)
				return
			}
			p.Open(home)
		})
	}()
}

// reportForPane says why something a file pane asked for failed, and
// writes it to the log instead when that pane has been closed.
//
// The answer to a request outlives the pane that made it: a listing on
// a machine that has stopped answering fails long after the user shut
// the pane and stopped waiting for it. A dialog then lands over
// whatever they are doing next and takes their next click.
//
// The error is not dropped. It goes to the log, which the user can
// open, because nobody is waiting for this one on screen.
func (a *app) reportForPane(p *files.Pane, title string, err error) {
	if a.holdsFilePane(p) {
		a.reportError(title, err)
		return
	}
	log.Printf("%s: %v (the pane asking had already been closed)", title, err)
}

// holdsFilePane reports whether a file pane is still open in this
// window.
func (a *app) holdsFilePane(p *files.Pane) bool {
	return a.files != nil && a.files.rows[p] != nil
}

// foldersOn are the folders saved for a machine, and none for one that
// is not in the server list.
func (a *app) foldersOn(host string) []string {
	h, ok := a.book.Lookup(host)
	if !ok {
		return nil
	}
	return h.Folders
}

// browseFoldersOn are the folders the menu and the palette offer for a
// machine: the ones saved for it, and on this machine the WSL
// distributions as well.
//
// A distribution is not saved anywhere. It is installed or it is not,
// and Windows serves its files on a share every program can read, so
// offering one costs nothing but the line.
func (a *app) browseFoldersOn(host string) []string {
	folders := a.foldersOn(host)
	if host != conns.Local {
		return folders
	}
	return append(folders, a.wslFolders()...)
}

// wslFolders are the WSL distributions on this machine, as the paths
// Windows reaches their files by.
//
// From the shells that were found, which are looked for once and kept:
// this is asked on the goroutine that draws, and listing them runs
// wsl.exe.
func (a *app) wslFolders() []string {
	var out []string
	for _, s := range a.shellPick.list() {
		if s.Distro != "" {
			out = append(out, shells.WSLRoot(s.Distro))
		}
	}
	return out
}

// oneFolderOn is where "Files" opens on a machine: the one folder saved
// for it, and home when it has none or several.
func (a *app) oneFolderOn(host string) string {
	if folders := a.foldersOn(host); len(folders) == 1 {
		return folders[0]
	}
	return ""
}

// filesystem opens a filesystem for a machine: this one, or one reached
// over a connection that is already open.
func (a *app) filesystem(host string) (vfs.FS, error) {
	f, err := a.machineFilesWithArchives(host)
	if err != nil {
		return nil, err
	}
	if a.about(host).machine == nil {
		// Local files have no connection to lose, and a window taken
		// over is read over a connection this window did not make.
		return f, nil
	}
	// Outermost, so that what the browser holds and asks about is the
	// wrapper: a pane's filesystem is looked up by identity in one
	// place and type-asserted for a rename in another, and both have to
	// find this rather than the session it happens to hold now.
	r := newReopening(a, host, a.about(host).machine.at, f)
	a.keepReopening(r)
	return r, nil
}

// machineFiles opens a filesystem for a machine, as the machine has
// it.
func (a *app) machineFiles(host string) (vfs.FS, error) {
	on := a.about(host)
	switch {
	case on.kind == hostHere:
		return vfs.NewLocal(), nil
	case on.window != nil:
		return a.windowFiles(on.name)
	case on.machine == nil:
		return nil, fmt.Errorf("nothing is connected to %s", on.name)
	}
	f, err := on.machine.conn.Files(a.ctx)
	if err != nil {
		return nil, err
	}
	// The connection is what says which machine this is: two
	// panes on one machine open two sessions on it.
	return vfs.NewSFTP(on.name, on.machine.conn, f.Client(), f.Close), nil
}

// machineFilesWithArchives is a machine's filesystem with the archives
// on it opened as directories.
//
// One wrapper per filesystem rather than one for the window: it holds
// the archive it is reading, and two panes in two archives would take
// turns throwing each other's out.
func (a *app) machineFilesWithArchives(host string) (vfs.FS, error) {
	f, err := a.machineFiles(host)
	if err != nil {
		return nil, err
	}
	return vfs.WithArchives(f), nil
}

// newPane builds one pane of the file manager.
func (a *app) newPane(f vfs.FS, b *browser) *files.Pane {
	p := files.New(f)
	p.Style = a.paneStyle()
	// Off the drawing goroutine, and back onto it with the answer: a
	// directory on a machine with a long way to go would otherwise stop
	// the window while it was read.
	p.Read = func(f vfs.FS, path string, then func([]vfs.Entry, error)) {
		go func() {
			entries, err := f.ReadDir(path)
			a.pump.post(func() { then(entries, err) })
		}()
	}
	p.OnChange = func() { a.browserMoved(b, p) }
	// Posted rather than run here: opening a pane rearranges the tree,
	// and this is called from inside the key handling of a pane in it.
	p.OnOpen = func(e vfs.Entry) {
		a.pump.post(func() {
			if err := a.readFileFrom(p, e, false); err != nil {
				a.reportError("Could not read "+e.Name, err)
			}
		})
	}
	// Not from the pane's own key handling: a dialog opened from inside
	// one is torn down with whatever the key was delivered through.
	p.OnGoTo = func() {
		a.pump.post(func() {
			if err := a.openGoTo(); err != nil {
				a.reportError("Could not go to a directory", err)
			}
		})
	}
	// Posted for the same reason: the dialog is opened from a click the
	// pane is still handling.
	p.OnError = func(at string, err error) {
		a.pump.post(func() {
			a.reportError("Could not read the directory", fmt.Errorf("%s\n\n%w", at, err))
		})
	}
	return p
}

// paneStyle colours one pane of the file manager.
func (a *app) paneStyle() files.Style {
	return files.Style{
		FG: a.colours.FG,
		BG: a.colours.BG,
		// The bar is marked the way the pane in front is, so the two read
		// as the same thing.
		SelectedFG: a.colours.BG,
		SelectedBG: a.colours.FG,
		// The machine, in the colour its name has on the sidebar, and
		// the directory under it dimmer: which directory this is changes
		// as the user moves, and which machine does not.
		HeaderFG: a.colours.ANSI[6],
		PathFG:   a.colours.FG,
		DirFG:    a.colours.ANSI[4],
		LinkFG:   a.colours.ANSI[6],
		MarkedFG: a.colours.ANSI[3],
		// Waiting to be pasted, which is not the same as picked out: one
		// is what the next key acts on, the other what the last one did.
		ClipFG: a.colours.ANSI[5],
		NoteFG: a.colours.ANSI[8],
		// The bar along the bottom: a key is read on the pane's own
		// ground, so it wants the window's own foreground rather than
		// the dim one a note is written in. A key with nothing behind it
		// sits on a ground between the bar and a working key -- dimmer
		// than one that works, and still lit enough to read.
		KeyFG: a.colours.FG,
		OffBG: grid.Blend(a.colours.BG, a.colours.FG, 1, 5),
		// Red, because a line saying why something failed has to read as
		// a failure before it is read as words.
		ErrorFG: a.colours.ANSI[1],
	}
}

// wireBrowser says what the F keys do.
func (a *app) wireBrowser(b *browser) {
	b.view.OnCopy = func(w files.Work) { a.startJob(jobs.Copy, w) }
	b.view.OnMove = func(w files.Work) { a.startJob(jobs.Move, w) }
	b.view.OnDelete = func(w files.Work) { a.confirmDelete(w) }
	b.view.OnMkdir = func(w files.Work) { a.askForDirectory(w) }
	// Posted for the same reason the open is: putting a pane in the
	// tree from inside the key handling of a pane in it.
	b.view.OnRead = func(p *files.Pane, e vfs.Entry, follow bool) {
		a.pump.post(func() {
			if err := a.readFileFrom(p, e, follow); err != nil {
				a.reportError("Could not read "+e.Name, err)
			}
		})
	}
	b.view.OnRename = func(w files.Work) { a.askToRename(w) }
	// A browser cannot take its own pane out of the tree it sits in, so
	// it says which one and the window does the rest.
	b.view.OnClose = func(p *files.Pane) {
		// Not from here: this runs from a key the browser is handling,
		// and taking the pane out of the tree underneath it would pull
		// the ground from under the rest of that key.
		a.pump.post(func() {
			if err := a.graceLogged(a.closePane(p)); err != nil {
				a.reportError("Could not close the pane", err)
			}
		})
	}
}

// browserRow is one pane of the file manager on the sidebar.
//
// One row per pane rather than one per manager: a manager touching
// several machines is open on all of them, and closing any of those
// machines has to take its pane away.
func (a *app) browserRow(p *files.Pane, host string) *conns.Entry {
	return &conns.Entry{
		Host:   host,
		Kind:   conns.Files,
		Label:  p.At(),
		Reveal: func() { a.focus(p) },
		// The grace is logged rather than shown here too: the cross took
		// the pane away whichever way its session closed.
		Close: func() error { return a.graceLogged(a.closePane(p)) },
	}
}

// browserMoved keeps the sidebar saying where a pane is.
func (a *app) browserMoved(b *browser, p *files.Pane) {
	row := b.rows[p]
	if row == nil {
		return
	}
	row.Label = p.At()
	a.markDirty()
}

// startJob puts file work on the queue and a row on the panel for it.
func (a *app) startJob(kind jobs.Kind, w files.Work) {
	if w.From == nil || (kind != jobs.Delete && w.To == nil) {
		return
	}
	op := jobs.Op{
		Kind: kind, From: w.From.FS(), At: w.At, Names: w.Names,
	}
	to := jobEnd{host: conns.Local}
	if w.To != nil {
		op.To, op.Into = w.To.FS(), w.To.At()
		to = a.endOf(w.To)
	}
	a.runJob(op, a.endOf(w.From), to, nil)
}

// jobEnd is one end of a piece of file work, kept so that end can be
// opened again.
//
// host is the machine the panel files the work under. far is the machine
// of a window taken over that the pane read through that window, and is
// empty for a pane on a machine this window reaches itself: the window's
// name alone would open the window's own disk.
type jobEnd struct {
	host string
	far  remoteHostKey
}

// endOf names the end of a piece of file work one pane stands for.
func (a *app) endOf(p *files.Pane) jobEnd {
	end := jobEnd{host: a.hostOf(p.FS())}
	if b := a.files; b != nil {
		end.far = b.far[p]
	}
	return end
}

// openEnd opens one end of a piece of file work again: a machine this
// window reaches, or a machine read through a window taken over.
func (a *app) openEnd(end jobEnd) (vfs.FS, error) {
	if end.far.window == nil {
		return a.filesystem(end.host)
	}
	t := end.far.window
	if !a.windows.holds(t) {
		return nil, fmt.Errorf("this window has let go of %s", t.name)
	}
	return a.windowFilesOn(t, end.far.host)
}

// jobRowKind is the sidebar kind for a piece of file work, so its row
// carries the picture for what it does.
func jobRowKind(k jobs.Kind) conns.Kind {
	switch k {
	case jobs.Move:
		return conns.Move
	case jobs.Delete:
		return conns.Delete
	}
	return conns.Copy
}

// runJob puts one piece of file work on the queue and a row on the panel
// for it.
//
// from and to are the ends the work is between, kept because they are
// known now: a repeat opens the filesystems again from them, and by then
// there may be nothing to read them off. owned are the filesystems the
// job opened for itself, closed once it has stopped.
func (a *app) runJob(op jobs.Op, from, to jobEnd, owned []vfs.FS) *jobs.Job {
	count := meter.New()
	e := &conns.Entry{
		Host:  from.host,
		Kind:  jobRowKind(op.Kind),
		Meter: count,
	}
	j := a.queue.Start(a.ctx, op, jobs.Options{
		Ask: &askOverwrite{app: a},
		// What moves is counted for the panel. Which way is whichever
		// end is somewhere else: a copy to another machine is bytes
		// leaving, and one from it is bytes arriving.
		Count: count,
		Out:   op.To != nil && to.host != conns.Local,
	})
	e.Label = j.Name()
	e.Reveal = func() { a.showJobPane(j, e, from, to) }
	e.Close = a.dropJobRow(e, j)
	a.jobs[e] = j
	a.registry.Add(e)
	a.letGoWhenDone(j, owned)
	a.markDirty()
	return j
}

// dropJobRow takes a job off the queue and its row off the panel.
//
// Drop on a job that has finished does nothing, so the same function
// ends a copy that is still going and clears the row of one that is not.
func (a *app) dropJobRow(e *conns.Entry, j *jobs.Job) func() error {
	return func() error {
		a.queue.Drop(j)
		delete(a.jobs, e)
		a.registry.Drop(e)
		return nil
	}
}

// letGoWhenDone closes the filesystems a job opened for itself, once it
// has stopped.
//
// On a goroutine of its own, the way releaseFS lets go of a filesystem a
// job is still using: waiting for a job to stop cannot happen on the
// goroutine that draws.
func (a *app) letGoWhenDone(j *jobs.Job, owned []vfs.FS) {
	if len(owned) == 0 {
		return
	}
	a.closes.inBackground(func() error {
		<-j.Done()
		var errs []error
		for _, f := range owned {
			if err := f.Close(); err != nil {
				errs = append(errs, fmt.Errorf("could not close %s: %w", f.Name(), err))
			}
		}
		return errors.Join(errs...)
	})
}

// reloadPanesOn reads again every pane of the file manager that is on
// one of these filesystems.
//
// By vfs.Same rather than by value: two panes on this machine are two
// values standing for one place, and a job that changed something there
// changed it for both of them.
func (a *app) reloadPanesOn(on ...vfs.FS) {
	if a.files == nil {
		return
	}
	for _, p := range a.files.view.Panes() {
		for _, f := range on {
			if vfs.Same(p.FS(), f) {
				p.Reload()
				break
			}
		}
	}
}

// hostOf says which machine a filesystem is filed under, for the panel.
//
// A filesystem on a machine of a window taken over is named after that
// machine through the window, and is filed under the window: the
// connection carrying it is the window's, so it goes when the window does.
//
// A window taken over is otherwise a machine like any other here: its
// panes go under its name, the commands on its row work on it, and a
// copy to it counts as bytes leaving rather than arriving.
func (a *app) hostOf(f vfs.FS) string {
	if key, over := farFS(f); over {
		return key.window.name
	}
	// A filesystem that holds a machine says which, because the machine
	// may not be connected right now. Going by the name would answer
	// Local for a pane on a machine that dropped, and a copy out of it
	// would be filed under this one.
	if r, is := f.(*reopening); is {
		return r.Host()
	}
	if on := a.about(f.Name()); on.machine != nil || on.window != nil {
		return on.name
	}
	return conns.Local
}

// farFS is the machine of a window taken over that a filesystem reads.
//
// Read off the place the filesystem calls itself, so one a job opened for
// itself answers as a pane's does: nothing but a filesystem reading
// through a window has a place of this kind.
func farFS(f vfs.FS) (remoteHostKey, bool) {
	key, over := f.Place().(remoteHostKey)
	return key, over && key.window != nil
}

// refreshJobs keeps the panel saying how the file work is going, and
// reads the panes again when a job has changed something.
//
// Called every frame. A job's progress is a plain value read under a
// lock, so asking costs nothing and nothing is pushed into the tree.
func (a *app) refreshJobs() { a.refreshJobsAt(time.Now()) }

// refreshJobsAt is refreshJobs at one moment, so everything a frame says
// about a job is worked out from the same clock.
func (a *app) refreshJobsAt(now time.Time) {
	for e, j := range a.jobs {
		p := j.Progress()
		e.Note = jobNote(p)
		if !p.Done {
			continue
		}
		delete(a.jobs, e)
		// It has finished, so the meter stops and the row goes grey.
		e.Meter.Close()
		// And the row is one the cross can take off the panel, now that
		// there is nothing left to stop.
		e.Clear = a.dropJobRow(e, j)
		// Stopping it is the user's own decision, and cancelling is what
		// the window does when it takes a browser away. Anything the
		// failure says beyond that is still shown: a cancel that could
		// not take away what it half wrote is a disk that did not do
		// what it was told.
		if why := trouble(p.Err); why != nil {
			a.reportError("Could not finish "+j.Name(), why)
		}
		// What it changed is in front of the user, so it is read again.
		// Only the panes it touched: the window can hold as many panes
		// as the user cares to open, and rereading a directory on a
		// machine the job never went near is a round trip for nothing.
		a.reloadPanesOn(j.Op().From, j.Op().To)
		a.markDirty()
	}
}

// jobFill is how much of a job's row is filled: the share of the bytes
// that have gone, or of the files while the bytes are not known yet.
//
// A job that has stopped fills nothing. What really keeps a finished row
// empty is panelRow finding no job on the entry, because refreshJobs
// takes it off the list before refreshPanel reads the row; this branch is
// for a caller that asks about a job it is still holding.
func jobFill(p jobs.Progress) float64 {
	switch {
	case p.Done:
		return 0
	case p.Bytes > 0:
		return min(float64(p.BytesDone)/float64(p.Bytes), 1)
	case p.Files > 0:
		return min(float64(p.FilesDone)/float64(p.Files), 1)
	}
	return 0
}

// jobNote is what a job's row says at its end.
func jobNote(p jobs.Progress) string {
	if !p.Done {
		if p.Files == 0 {
			return "looking"
		}
		return fmt.Sprintf("%d of %d", p.FilesDone, p.Files)
	}
	how := ""
	switch {
	case errors.Is(p.Err, context.Canceled):
		how = "cancelled"
	case errors.Is(p.Err, jobs.ErrStopped):
		how = "stopped"
	case p.Err != nil:
		return "failed"
	default:
		return ""
	}
	// Stopping it was the user's own decision; whatever else went wrong
	// was not, and the row is where they would look for it.
	if trouble(p.Err) != nil {
		return how + ", trouble"
	}
	return how
}

// trouble is what a job's failure says beyond the user having stopped
// it, and nil when stopping it is the whole story.
//
// A cancel that could not take away the part it had written reports
// both, and the disk failure is the half nobody asked for.
func trouble(err error) error {
	for err != nil {
		if !errors.Is(err, context.Canceled) && !errors.Is(err, jobs.ErrStopped) {
			return err
		}
		if joined, ok := err.(interface{ Unwrap() []error }); ok {
			var rest []error
			for _, e := range joined.Unwrap() {
				rest = append(rest, trouble(e))
			}
			return errors.Join(rest...)
		}
		next, ok := err.(interface{ Unwrap() error })
		if !ok {
			return nil
		}
		err = next.Unwrap()
	}
	return nil
}

// confirmDelete asks before taking anything away.
func (a *app) confirmDelete(w files.Work) {
	what := w.Names[0]
	if len(w.Names) > 1 {
		what = fmt.Sprintf("%d items", len(w.Names))
	}
	f := a.newConfirm(dlgDelete+what+"?", []string{
		w.From.FS().Name() + ": " + w.At,
		"",
		"This cannot be undone.",
	})
	f.AddButton(ui.Button{Title: btnDelete, Do: func() error {
		// Not from here: this dialog closes as soon as this returns, and
		// closing one takes anything stacked on top of it.
		a.pump.post(func() { a.startJob(jobs.Delete, w) })
		return nil
	}})
	f.AddButton(ui.Button{Title: btnCancel})
	// Opens on the button that changes nothing.
	f.FocusButton(1)
	a.showForm(f, nil)
}

// askForDirectory asks what to call a new directory and makes it.
func (a *app) askForDirectory(w files.Work) {
	f := a.newForm(dlgNewDirectory)
	f.Lines = []string{w.From.FS().Name() + ": " + w.At}
	name := f.AddField(fldName, a.newField("", 0))
	f.AddButton(ui.Button{Title: btnCreate, Do: func() error {
		at := strings.TrimSpace(name.Text())
		if err := plainName(w.From.FS(), at); err != nil {
			// Returned rather than shown here, so the dialog stays open
			// with what was typed still there to correct.
			return err
		}
		a.pump.post(func() { a.makeDirectory(w.From, at) })
		return nil
	}})
	f.AddButton(ui.Button{Title: btnCancel})
	a.showForm(f, nil)
}

// makeDirectory makes one, off the drawing goroutine because it is on a
// machine that may be a long way away.
func (a *app) makeDirectory(p *files.Pane, name string) {
	f, at := p.FS(), vfs.Join(p.FS(), p.At(), name)
	go func() {
		err := f.Mkdir(at, 0o755)
		a.pump.post(func() {
			if err != nil {
				a.reportError("Could not create the directory", err)
				return
			}
			p.Reload()
		})
	}()
}

// askToRename asks what to call something and renames it.
func (a *app) askToRename(w files.Work) {
	was := w.Names[0]
	f := a.newForm(dlgRename + was)
	name := f.AddField(fldName, a.newField("", 0))
	name.SetText(was)
	f.AddButton(ui.Button{Title: btnRename, Do: func() error {
		to := strings.TrimSpace(name.Text())
		if err := plainName(w.From.FS(), to); err != nil {
			return err
		}
		if to == was {
			// The name it already has. Nothing to do and nothing worth
			// saying: the dialog closes, which is what pressing Rename
			// on an unchanged name means.
			return nil
		}
		a.pump.post(func() { a.renameTo(w.From, was, to) })
		return nil
	}})
	f.AddButton(ui.Button{Title: btnCancel})
	a.showForm(f, nil)
}

// renameTo renames one name, off the drawing goroutine.
func (a *app) renameTo(p *files.Pane, was, to string) {
	f := p.FS()
	from, at := vfs.Join(f, p.At(), was), vfs.Join(f, p.At(), to)
	go func() {
		// Asked about first: rename replaces what is there, and the user
		// typed a name rather than answering a question about one.
		//
		// Changing only the letter case is let through: on a machine
		// that does not tell two such names apart, the name that is
		// already there is the very file being renamed.
		_, err := f.Stat(at)
		switch {
		case err == nil && !strings.EqualFold(was, to):
			a.pump.post(func() {
				a.reportError("Could not rename "+was,
					fmt.Errorf("%s is already there", to))
			})
			return
		case err != nil && !errors.Is(err, fs.ErrNotExist):
			// Something else went wrong looking. Renaming replaces what
			// is there, so going ahead without knowing is how a file is
			// lost.
			a.pump.post(func() { a.reportError("Could not rename "+was, err) })
			return
		}
		err = f.Rename(from, at)
		a.pump.post(func() {
			if err != nil {
				a.reportError("Could not rename "+was, err)
				return
			}
			p.Reload()
		})
	}()
}

// plainName reports whether something is a name in a directory rather
// than a path to somewhere else.
func plainName(f vfs.FS, name string) error {
	switch {
	case name == "":
		return errors.New("it needs a name")
	case name == "." || name == "..":
		return fmt.Errorf("%q is not a name to use", name)
	case strings.ContainsRune(name, rune(f.Sep())), strings.ContainsRune(name, '/'):
		return fmt.Errorf("%q is a path, and a name is wanted", name)
	}
	return nil
}

// askOverwrite asks the user about a name that is already there.
//
// It is called from a job's own goroutine, so the question is handed to
// the one that draws and this waits for the answer.
type askOverwrite struct{ app *app }

// Overwrite shows the question and waits.
func (a *askOverwrite) Overwrite(ctx context.Context, c jobs.Conflict) (jobs.Choice, error) {
	answers := make(chan jobs.Choice, 1)
	a.app.pump.post(func() { a.app.showOverwrite(c, answers) })

	select {
	case choice := <-answers:
		return choice, nil
	case <-ctx.Done():
		// The job has been given up on, so there is no answer to act on
		// and the question goes with it.
		a.app.pump.post(func() { a.app.stopAsking(answers) })
		return jobs.Choice{}, ctx.Err()
	}
}

// showOverwrite puts the question on the screen. It runs on the drawing
// goroutine.
func (a *app) showOverwrite(c jobs.Conflict, answers chan jobs.Choice) {
	f := a.newConfirm(dlgReplaceFile+vfs.Base(c.To, c.Path)+"?", wrapLines(
		fmt.Sprintf("Existing %s on %s: %s, modified %s.",
			what(c.Have), c.To.Name(), size(c.Have.Size), when(c.Have)), errorLineWidth))

	answered := false
	answer := func(choice jobs.Choice) {
		if answered {
			return
		}
		answered = true
		answers <- choice
	}
	f.AddButton(ui.Button{Title: btnReplace, Do: func() error {
		answer(jobs.Choice{What: jobs.Replace})
		return nil
	}})
	f.AddButton(ui.Button{Title: btnReplaceAll, Do: func() error {
		answer(jobs.Choice{What: jobs.Replace, All: true})
		return nil
	}})
	f.AddButton(ui.Button{Title: btnSkip, Do: func() error {
		answer(jobs.Choice{What: jobs.Skip})
		return nil
	}})
	f.AddButton(ui.Button{Title: btnSkipAll, Do: func() error {
		answer(jobs.Choice{What: jobs.Skip, All: true})
		return nil
	}})
	// Stopping the whole job was only ever reachable by dismissing the
	// dialog, which no button said. Now it is one, and Escape is the
	// same answer rather than a different one.
	f.AddButton(ui.Button{Title: btnCancel, Do: func() error {
		answer(jobs.Choice{What: jobs.Stop})
		return nil
	}})
	// Opens on the answer that changes nothing, the way the question
	// about deleting does.
	f.FocusButton(2)
	// Whichever way the dialog goes away, the job is told: one that was
	// never answered would hold its goroutine until the window closed.
	dismiss := a.showForm(f, func() {
		delete(a.asking, answers)
		answer(jobs.Choice{What: jobs.Stop})
	})
	// Kept so a job that is given up on takes its question away with it:
	// a dialog nobody can usefully answer is a dialog in the way.
	a.asking[answers] = dismiss
}

// stopAsking takes away a question whose job has gone. It runs on the
// drawing goroutine.
func (a *app) stopAsking(answers chan jobs.Choice) {
	dismiss := a.asking[answers]
	delete(a.asking, answers)
	if dismiss != nil {
		dismiss()
	}
}

// what names the kind of thing something is.
func what(e vfs.Entry) string {
	switch {
	case e.IsLink():
		return "link"
	case e.IsDir():
		return "directory"
	}
	return "file"
}

// when says when something last changed, or nothing when it is not
// known.
func when(e vfs.Entry) string {
	if e.Mod.IsZero() {
		return "at some point"
	}
	return e.Mod.Format("2006-01-02 15:04")
}

// size writes a byte count the way a person reads one.
func size(n int64) string {
	if n < 0 {
		return "0 B"
	}
	return meter.Bytes(uint64(n))
}

// renamedFS is a filesystem that can be told its machine is called
// something else now.
type renamedFS interface{ Renamed(string) }

// renamedPane is one file pane's filesystem and what it is called once
// the machine it is filed under has another name.
type renamedPane struct {
	fs  renamedFS
	far remoteHostKey
}

// named is what the filesystem calls itself once the machine it is filed
// under is called now.
func (r renamedPane) named(now string) string {
	if r.far.window == nil {
		return now
	}
	return farName(r.far.host, now)
}

// filesystemsOn are the file panes' filesystems filed under a machine
// that can be told it is called something else.
func (a *app) filesystemsOn(host string) []renamedPane {
	b := a.files
	if b == nil {
		return nil
	}
	var out []renamedPane
	for _, p := range b.view.Panes() {
		f, ok := p.FS().(renamedFS)
		if !ok || !a.filedUnder(p, host) {
			continue
		}
		out = append(out, renamedPane{fs: f, far: b.far[p]})
	}
	return out
}

// filedUnder reports whether a file pane belongs to a machine: one on
// that machine, and one on a machine reached through a window of that
// name.
//
// A pane through a window is matched by its sidebar row, which is what
// filing it under a name means. The window itself has been given its new
// name by the time a rename asks, and the row has not.
func (a *app) filedUnder(p *files.Pane, host string) bool {
	if b := a.files; b != nil {
		if _, over := b.far[p]; over {
			row := b.rows[p]
			return row != nil && row.Host == host
		}
	}
	return p.FS().Name() == host
}

// openGoTo asks a file pane for somewhere to go.
//
// The only way to reach another drive: going up from one leads nowhere,
// because there is nothing above it. It is also the only way to reach a
// directory whose name nobody wants to walk to.
func (a *app) openGoTo() error {
	p, ok := ui.FocusedLeaf(a.root.Widget()).(*files.Pane)
	if !ok {
		return errors.New("the keys are not on a file pane")
	}
	f := a.newForm(dlgGoTo)
	where := f.AddField(fldPath, a.newField("", 0))
	// The places this filesystem starts from, so a drive is one key
	// away rather than something to remember the letter of.
	where.Options = append([]string{p.At()}, p.FS().Roots()...)
	where.SetText(p.At())
	a.completePath(where, p.FS())
	// Both set once the dialog is up, which is before any button can be
	// pressed.
	var (
		dismiss func()
		gone    bool
	)
	// Kept open until the read answers.
	f.AddButton(ui.Button{Title: btnGo, Keep: true, Do: func() error {
		path := strings.TrimSpace(where.Text())
		if path == "" {
			// Returned rather than shown here, so the dialog stays open
			// with what was typed still there to correct.
			return errors.New("Enter a path")
		}
		p.OpenThen(path, func(err error) {
			if gone {
				// Cancelled while the read was out, so the reason goes
				// where every other one goes.
				if err != nil {
					a.reportError("Could not read the directory",
						fmt.Errorf("%s\n\n%w", path, err))
				}
				return
			}
			if err != nil {
				f.SetError(err)
				return
			}
			dismiss()
		})
		return nil
	}})
	f.AddButton(ui.Button{Title: btnCancel})
	dismiss = a.showForm(f, func() { gone = true })
	return nil
}

// closeFilesOn takes away every pane of the file manager filed under a
// machine, for a connection that has gone.
//
// A pane on a machine reached through a window of that name goes too:
// the connection carrying it is the window's.
//
// One at a time, so each pane's filesGrace is waited out after the last:
// closing a pane cuts the widget tree, drops a sidebar row and edits what
// the window holds, none of which may happen anywhere but here.
func (a *app) closeFilesOn(host string) error {
	b := a.files
	if b == nil {
		return nil
	}
	var errs []error
	for _, p := range b.view.Panes() {
		if !a.filedUnder(p, host) {
			continue
		}
		errs = append(errs, a.closePane(p))
	}
	return errors.Join(errs...)
}

// filesPaneGone takes a pane of the file manager off the sidebar, for
// one the tree has already let go of.
//
// The filesystem goes last, and only once nothing is still using it: a
// job reading through a session closed underneath it fails part way and
// cannot even take away what it half wrote.
func (a *app) filesPaneGone(p *files.Pane) error {
	// The filesystem first, and whatever the sidebar knows about the
	// pane after: a pane with no manager to belong to still holds a
	// session that has to be let go of.
	err := a.browserLetGoFS(p.FS())
	b := a.files
	if b == nil {
		return errors.Join(err, fmt.Errorf("the pane on %s belonged to no file manager", p.FS().Name()))
	}
	if row := b.rows[p]; row != nil {
		a.registry.Drop(row)
		delete(b.rows, p)
	}
	delete(b.far, p)
	if len(b.rows) == 0 {
		// The manager went with its last pane, so the window has none.
		a.files = nil
	}
	return err
}

// browserLetGoFS closes a filesystem the browser has finished with,
// unless a reader is still using it.
//
// A reader opened from a browser pane reads through that pane's
// filesystem. Closing it under one leaves the reader with a stale file
// and a reread that fails down a session somebody else closed, so the
// close waits for the last reader to go and for the last read it has out
// to come back.
func (a *app) browserLetGoFS(f vfs.FS) error {
	if f == nil {
		return nil
	}
	if a.fsHeld[f] > 0 {
		// A reader is still on it, so it is not let go of and it is not
		// forgotten either: the reader reads again when the user asks,
		// and that read opens the machine the same as the browser's
		// would have.
		if a.fsGone == nil {
			a.fsGone = map[vfs.FS]bool{}
		}
		a.fsGone[f] = true
		return nil
	}
	return a.releaseFS(f)
}

// releaseFS lets go of a filesystem, once no job is still using it.
//
// A filesystem with no job on it is closed here and the failure handed
// straight back. One that has to wait is closed on another goroutine,
// because a job stops when whatever it is waiting on gives up and the
// window may not wait with it. The window counts those and waits for
// them on the way out, and whatever they report is shown on the next
// frame or printed on the way out.
func (a *app) releaseFS(f vfs.FS) error {
	stopping := a.stopJobsOn(f)
	if len(stopping) == 0 {
		a.dropReopeningFS(f)
		return f.Close()
	}
	// The ones with a job still on them stay on the list until that job
	// has stopped: the job is still using this, and a machine that goes
	// while it runs has to reach it.
	a.closes.inBackground(func() error {
		for _, j := range stopping {
			<-j.Done()
		}
		if err := f.Close(); err != nil {
			return fmt.Errorf("could not close %s: %w", f.Name(), err)
		}
		return nil
	})
	return nil
}

// reportClosed shows what the filesystems let go of on goroutines of
// their own reported, once each.
//
// A close that only ran a bound of its own out is logged instead, the way
// one on the goroutine that draws is: there is nothing on a notice about
// it for the user to do.
func (a *app) reportClosed() {
	for _, err := range a.closes.reported() {
		if err := a.graceLogged(err); err != nil {
			a.reportError("Could not disconnect the filesystem", err)
		}
	}
}

// stopJobsOn gives up on the jobs using any of these filesystems, and
// hands them back so a caller can wait for them to stop.
func (a *app) stopJobsOn(on ...vfs.FS) []*jobs.Job {
	var stopping []*jobs.Job
	for e, j := range a.jobs {
		op := j.Op()
		for _, f := range on {
			if op.From != f && op.To != f {
				continue
			}
			j.Cancel()
			stopping = append(stopping, j)
			// The row goes with the browser it was started from.
			delete(a.jobs, e)
			a.registry.Drop(e)
			break
		}
	}
	return stopping
}

// windowFiles is the filesystem of the machine a window taken over is
// on, as a browser pane works on it.
//
// Opening it is bounded by remote.WindowFiles, the way the same pane on
// a machine is bounded by Conn.Files: this runs on the goroutine that
// draws.
func (a *app) windowFiles(addr string) (vfs.FS, error) {
	t := a.about(addr).window
	if t == nil {
		return nil, fmt.Errorf("this window is not connected to %s", addr)
	}
	if err := a.roomForFilesOver(t); err != nil {
		return nil, err
	}
	ch, client, err := remote.WindowFiles(a.ctx, addr, t.win)
	if err != nil {
		return nil, err
	}
	// The window taken over is what says which machine this is.
	return vfs.NewSFTP(addr, t.win, client, func() error {
		return a.letGoOfFilesOver(t, client, ch)
	}), nil
}

// windowFilesOn is the filesystem of a machine a window taken over is
// connected to, as a browser pane works on it.
//
// The bytes go through that window: this one has no connection to the
// machine, and the window it took over has. Bounded by
// remote.WindowFilesOn, the way windowFiles is.
func (a *app) windowFilesOn(t *taken, host string) (vfs.FS, error) {
	if err := a.roomForFilesOver(t); err != nil {
		return nil, err
	}
	ch, client, err := remote.WindowFilesOn(a.ctx, t.name, host, t.win)
	if err != nil {
		return nil, err
	}
	// The machine it reads, through the window carrying the bytes: every
	// question the pane asks is about that machine, and a pane calling
	// itself by the window's name answered all of them wrongly. Which
	// machine over there it reads is the place, so two panes on one
	// machine over there are one place and neither is the window's own
	// disk.
	return vfs.NewSFTP(farName(host, t.name), remoteHostKey{window: t, host: host}, client, func() error {
		return a.letGoOfFilesOver(t, client, ch)
	}), nil
}

// roomForFilesOver refuses a file pane on a window already holding as
// many abandoned closes as this one will.
//
// Each one waits on a window that has stopped answering and ends only
// when this window hangs up on it, so the count is what says a window is
// no longer one to ask.
func (a *app) roomForFilesOver(t *taken) error {
	if n := a.windows.abandonedCloses(t.win); n >= mostAbandonedCloses {
		return fmt.Errorf(
			"%s has stopped answering; %s file sessions to it are still waiting to end",
			t.name, inWords(n))
	}
	return nil
}

// letGoOfFilesOver ends a file session on a window taken over and counts
// one left behind against that window until it ends.
//
// A close that was abandoned holds a goroutine and the file session's
// error stream. Both end when this window hangs up on that one, which
// letGoOfWindow does and closing a pane does not, so the count is what
// keeps them bounded in between.
func (a *app) letGoOfFilesOver(t *taken, client, ch io.Closer) error {
	left, err := closeFilesOver(client, ch)
	if left == nil {
		return err
	}
	// The window as it is called now, which is what the line saying the
	// close ended has to say.
	win, name := t.win, t.name
	a.pump.post(func() { a.windows.closeAbandoned(win) })
	go func() {
		fromWindow := <-left
		a.pump.post(func() {
			a.windows.closeStopped(win)
			a.logError(errors.Join(fmt.Errorf(
				"the file session left on %s has ended", name), fromWindow))
		})
	}()
	return err
}

// farName is what a pane on a machine of a window taken over calls the
// place it reads.
func farName(host, window string) string { return host + " through " + window }

// errFilesGraceExpired says the far end never answered a file session's
// goodbye, so the channel was closed from here.
//
// Named rather than anonymous: it is the only difference between the two
// ways closeFilesOver can end, and it is logged rather than shown, so a
// test has to be able to tell the two apart.
var errFilesGraceExpired = errors.New(
	"the file session's goodbye went unanswered, so it was closed from here")

// errFilesCloseAbandoned says the client's own close never came back
// after the channel was closed, so it was left behind.
//
// Named for the reason errFilesGraceExpired is: it is logged rather than
// shown, and a test has to be able to tell this path from the others.
var errFilesCloseAbandoned = errors.New(
	"the file session's close never came back, so it was left to the connection")

// closeFilesOver ends a file session on a window taken over.
//
// Closing the SFTP client sends an end of file and then waits for the
// far end to close the channel. This runs on the goroutine that draws, so
// that wait is bounded by filesGrace: one round trip to the window taken
// over, which is what has to fit inside a frame. After it the channel is
// closed from here, which is what lets go, and that path says so with
// errFilesGraceExpired.
//
// Closing the channel only sends a message, so a link that is dead both
// ways leaves the client's close waiting for an answer that cannot
// arrive. It gets one more grace and is then abandoned, which says so
// with errFilesCloseAbandoned. That goroutine ends when the connection
// to the window is closed, which only letting go of the window does:
// closing a pane leaves it where it is, so left comes back carrying it
// and is nil on every other path.
func closeFilesOver(client, ch io.Closer) (left <-chan error, err error) {
	done := make(chan error, 1)
	go func() { done <- client.Close() }()

	var errs []error
	select {
	case err := <-done:
		errs = append(errs, err)
	case <-time.After(filesGrace):
		errs = append(errs, errFilesGraceExpired, ch.Close())
		// Now that the channel has gone, the client's own close can
		// finish, unless the link is dead and nothing wakes the read it
		// is parked on.
		select {
		case err := <-done:
			errs = append(errs, err)
		case <-time.After(filesGrace):
			errs = append(errs, errFilesCloseAbandoned)
			left = done
		}
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
	return left, errors.Join(errs...)
}

// filesGrace is how long letting go of a window waits for its file
// session to say goodbye before closing the channel from here.
//
// One round trip to that window, spent on the goroutine that draws. A
// slow link or a pause in the garbage collector can run it out with
// nothing wrong, so running out is logged rather than shown.
const filesGrace = 250 * time.Millisecond

// graceLogged takes the parts of a failure that are only a file session
// closing on a bound of its own, logs them, and hands back the rest.
//
// The bound they come from is one round trip on the goroutine that draws,
// which a slow link runs out on its own. There is nothing on a notice
// about it for the user to do, and the pane has gone either way.
func (a *app) graceLogged(err error) error {
	expired, rest := splitGraceExpired(err)
	if expired != nil {
		a.logError(expired)
	}
	return rest
}

// splitGraceExpired splits a failure into the parts about a file session
// that ran out its bound and everything else, looking inside a joined
// error and inside a wrapping one.
//
// A wrap carrying a grace and real trouble together is left whole and
// shown: the words around it say what was being closed, and a failure
// the user can act on is in there.
func splitGraceExpired(err error) (expired, rest error) {
	if err == nil {
		return nil, nil
	}
	switch under := err.(type) {
	case interface{ Unwrap() []error }:
		var expireds, rests []error
		for _, one := range under.Unwrap() {
			was, other := splitGraceExpired(one)
			expireds = append(expireds, was)
			rests = append(rests, other)
		}
		return errors.Join(expireds...), errors.Join(rests...)
	case interface{ Unwrap() error }:
		was, other := splitGraceExpired(under.Unwrap())
		if was != nil && other == nil {
			return err, nil
		}
		return nil, err
	}
	if errors.Is(err, errFilesGraceExpired) || errors.Is(err, errFilesCloseAbandoned) ||
		errors.Is(err, remote.ErrCloseAbandoned) {
		return err, nil
	}
	return nil, err
}

// relayGrace is how long a file session relayed to a machine waits for
// that machine to answer the close, once the client has gone.
//
// Far longer than filesGrace, because it is spent somewhere else:
// filesGrace holds up the goroutine that draws and has to fit inside a
// frame, while this runs on a goroutine serving one client and is waiting
// for a single round trip over the network.
const relayGrace = 2 * time.Second

// relayTogether is how long a relayed file session whose machine has
// finished waits to see the client finish too.
//
// Long enough to tell two ends that stopped at the same moment from a
// machine that ended under a client still working, and short enough that
// the client is told either way while it is still waiting.
const relayTogether = 20 * time.Millisecond

// completePath fills in the rest of a directory name as one is typed,
// from the filesystem the pane is on.
func (a *app) completePath(where *ui.Field, f vfs.FS) {
	// The listing of the directory last read, so typing on in one
	// directory reads it once rather than once a keystroke, and reading
	// is the directory a read is out for.
	var (
		lastDir string
		have    []vfs.Entry
		reading string
		show    func(string)
	)
	show = func(text string) {
		where.Ghost = ""
		dir, leaf, ok := splitLeaf(f, text)
		if !ok {
			return
		}
		if dir == lastDir && have != nil {
			where.Ghost = restOf(f, have, leaf)
			return
		}
		if dir == reading {
			return
		}
		reading = dir
		go func() {
			entries, err := f.ReadDir(dir)
			a.pump.post(func() {
				if reading == dir {
					reading = ""
				}
				if err != nil {
					// A directory that is not there yet is what half a
					// typed path looks like. Anything else is written
					// down, and is a question at the top of the to-do
					// list.
					if !errors.Is(err, fs.ErrNotExist) {
						a.logError(fmt.Errorf("completing a path on %s: %w", f.Name(), err))
					}
					return
				}
				lastDir, have = dir, entries
				// Against what the field says now rather than what asked
				// for the listing: the user has gone on typing, and this
				// is the answer for where they are.
				show(where.Text())
			})
		}()
	}
	where.OnChange = show
}

// splitLeaf divides a typed path into the directory to list and the part
// of a name that has been typed. It says no for a path with no separator
// in it, which names no directory to read.
func splitLeaf(f vfs.FS, text string) (dir, leaf string, ok bool) {
	sep := string(f.Sep())
	if sep == `\` {
		// Windows takes either separator, so a path typed with the
		// other one still completes.
		text = strings.ReplaceAll(text, "/", sep)
	}
	if !strings.Contains(text, sep) {
		return "", "", false
	}
	if strings.HasSuffix(text, sep) {
		return text, "", true
	}
	return vfs.Dir(f, text), vfs.Base(f, text), true
}

// restOf is what every directory whose name begins with leaf agrees on
// after it, and empty when none does, when they disagree at once, or
// when leaf already names a directory of its own.
func restOf(f vfs.FS, entries []vfs.Entry, leaf string) string {
	match := matcher(f)
	rest := ""
	found := false
	for _, e := range entries {
		// A link is offered too. Most of /bin, /lib and /sbin are links
		// on a modern machine, and Go to is for reaching a directory
		// however it is spelt.
		if !e.IsDir() && !e.IsLink() {
			continue
		}
		if match(e.Name, leaf) < 0 {
			continue
		}
		// A name that is leaf itself adds nothing, which is what leaves
		// a path that already works alone.
		after := e.Name[len(leaf):]
		if !found {
			rest, found = after, true
			continue
		}
		rest = sharedStart(rest, after)
		if rest == "" {
			return ""
		}
	}
	return rest
}

// matcher compares a name against what has been typed: 0 when they are
// the same name, 1 when the name begins with it, and -1 otherwise.
//
// Case counts on a machine whose filesystem counts it. On Windows it
// does not, and a user who types in lower case means the directory
// whatever its own spelling is.
func matcher(f vfs.FS) func(name, leaf string) int {
	same := func(a, b string) bool { return a == b }
	if f.Sep() == '\\' {
		same = strings.EqualFold
	}
	return func(name, leaf string) int {
		switch {
		case same(name, leaf):
			return 0
		case len(name) > len(leaf) && same(name[:len(leaf)], leaf):
			return 1
		}
		return -1
	}
}

// sharedStart is the longest run two strings begin with, cut at a
// character boundary so half of one is never offered.
func sharedStart(a, b string) string {
	at := 0
	for at < len(a) && at < len(b) && a[at] == b[at] {
		at++
	}
	// The end of a is a boundary in its own right; only a cut inside it
	// has to step back.
	for at > 0 && at < len(a) && !utf8.RuneStart(a[at]) {
		at--
	}
	return a[:at]
}
