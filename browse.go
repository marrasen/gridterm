package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strings"

	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/jobs"
	"github.com/marrasen/gridterm/meter"
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
}

// openFilesHere puts another pane in the file manager, on the machine
// the user is looking at.
func (a *app) openFilesHere() error { return a.openFilesOn(a.currentHost()) }

// openFilesOn puts a pane on a machine, starting the file manager when
// there is not one yet.
func (a *app) openFilesOn(host string) error {
	f, err := a.filesystem(host)
	if err != nil {
		return err
	}
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
	row := a.browserRow(p, host)
	b.rows[p] = row
	a.registry.Add(row)
	a.focus(p)
	a.relayout()

	// Somewhere to start. Asking a machine where home is takes as long
	// as anything else it is asked, so it happens off this goroutine and
	// the pane fills in when the answer arrives.
	a.startAt(p)
	return nil
}

// openFileManager puts an empty file manager in the window.
func (a *app) openFileManager() error {
	b := &browser{rows: map[*files.Pane]*conns.Entry{}}
	b.view = files.NewBrowser()
	// The dividers and the bar of keys are drawn in the panes' own
	// colours, so both read as part of the manager.
	b.view.Style = a.paneStyle()
	a.wireBrowser(b)
	if err := a.placeTab(b.view); err != nil {
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
func (a *app) startAt(p *files.Pane) {
	f := p.FS()
	go func() {
		home, err := f.Home()
		a.pump.post(func() {
			if err != nil {
				a.reportError("Could not open "+f.Name(), err)
				return
			}
			p.Open(home)
		})
	}()
}

// filesystem opens a filesystem for a machine: this one, or one reached
// over a connection that is already open.
func (a *app) filesystem(host string) (vfs.FS, error) {
	if host == conns.Local {
		return vfs.NewLocal(), nil
	}
	m := a.machines[host]
	if m == nil {
		return nil, fmt.Errorf("nothing is connected to %s", host)
	}
	f, err := m.conn.Files()
	if err != nil {
		return nil, err
	}
	return vfs.NewSFTP(host, f.Client(), f.Close), nil
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
	return p
}

// paneStyle colours one pane of the file manager.
func (a *app) paneStyle() files.Style {
	return files.Style{
		FG: a.colours.FG,
		BG: a.colours.BG,
		// The bar is marked the way a selected tab is, so the two read
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
	b.view.OnRename = func(w files.Work) { a.askToRename(w) }
	// A browser cannot take its own pane out of the tree it sits in, so
	// it says which one and the window does the rest.
	b.view.OnClose = func(p *files.Pane) {
		// Not from here: this runs from a key the browser is handling,
		// and taking the pane out of the tree underneath it would pull
		// the ground from under the rest of that key.
		a.pump.post(func() {
			if err := a.closePane(p); err != nil {
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
		Close:  func() error { return a.closePane(p) },
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
	if w.To != nil {
		op.To, op.Into = w.To.FS(), w.To.At()
	}

	count := meter.New()
	e := &conns.Entry{
		Host:  a.hostOf(w.From.FS()),
		Kind:  conns.Files,
		Meter: count,
	}
	j := a.queue.Start(a.ctx, op, jobs.Options{
		Ask: &askOverwrite{app: a},
		// What moves is counted for the panel. Which way is whichever
		// end is somewhere else: a copy to another machine is bytes
		// leaving, and one from it is bytes arriving.
		Count: count,
		Out:   w.To != nil && a.hostOf(w.To.FS()) != conns.Local,
	})
	e.Label = j.Name()
	e.Close = func() error {
		a.queue.Drop(j)
		delete(a.jobs, e)
		a.registry.Drop(e)
		return nil
	}
	a.jobs[e] = j
	a.registry.Add(e)
	a.markDirty()
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

// hostOf says which machine a filesystem is, for the panel.
func (a *app) hostOf(f vfs.FS) string {
	for name, m := range a.machines {
		if m != nil && f.Name() == name {
			return name
		}
	}
	return conns.Local
}

// refreshJobs keeps the panel saying how the file work is going, and
// reads the panes again when a job has changed something.
//
// Called every frame. A job's progress is a plain value read under a
// lock, so asking costs nothing and nothing is pushed into the tree.
func (a *app) refreshJobs() {
	for e, j := range a.jobs {
		p := j.Progress()
		e.Note = jobNote(p)
		if !p.Done {
			continue
		}
		delete(a.jobs, e)
		// It has finished, so the meter stops and the row goes grey.
		e.Meter.Close()
		// Stopping it is the user's own decision, and cancelling is what
		// the window does when it takes a browser away.
		if p.Err != nil && !errors.Is(p.Err, jobs.ErrStopped) &&
			!errors.Is(p.Err, context.Canceled) {
			a.reportError("Could not finish "+j.Name(), p.Err)
		}
		// What it changed is in front of the user, so it is read again.
		// Only the panes it touched: the window can hold as many panes
		// as the user cares to open, and rereading a directory on a
		// machine the job never went near is a round trip for nothing.
		a.reloadPanesOn(j.Op().From, j.Op().To)
		a.markDirty()
	}
}

// jobNote is what a job's row says at its end.
func jobNote(p jobs.Progress) string {
	switch {
	case p.Done && p.Err != nil:
		return "failed"
	case p.Done:
		return ""
	case p.Files == 0:
		return "looking"
	}
	return fmt.Sprintf("%d of %d", p.FilesDone, p.Files)
}

// confirmDelete asks before taking anything away.
func (a *app) confirmDelete(w files.Work) {
	what := w.Names[0]
	if len(w.Names) > 1 {
		what = fmt.Sprintf("%d things", len(w.Names))
	}
	f := a.newConfirm("Delete "+what+"?", wrapLines(
		"They go from "+w.From.FS().Name()+", at "+w.At+
			". There is nothing that puts them back.", errorLineWidth))
	f.AddButton(ui.Button{Title: "Delete", Do: func() error {
		// Not from here: this dialog closes as soon as this returns, and
		// closing one takes anything stacked on top of it.
		a.pump.post(func() { a.startJob(jobs.Delete, w) })
		return nil
	}})
	f.AddButton(ui.Button{Title: "Keep them"})
	// Opens on the button that changes nothing.
	f.FocusButton(1)
	a.showForm(f, nil)
}

// askForDirectory asks what to call a new directory and makes it.
func (a *app) askForDirectory(w files.Work) {
	f := a.newForm("New directory in " + w.From.FS().Name())
	f.Lines = wrapLines("It is made in "+w.At+".", errorLineWidth)
	name := f.AddField("Name", a.newField("what to call it", 0))
	f.AddButton(ui.Button{Title: "Make it", Do: func() error {
		at := strings.TrimSpace(name.Text())
		if err := plainName(w.From.FS(), at); err != nil {
			// Returned rather than shown here, so the dialog stays open
			// with what was typed still there to correct.
			return err
		}
		a.pump.post(func() { a.makeDirectory(w.From, at) })
		return nil
	}})
	f.AddButton(ui.Button{Title: "Cancel"})
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
				a.reportError("Could not make the directory", err)
				return
			}
			p.Reload()
		})
	}()
}

// askToRename asks what to call something and renames it.
func (a *app) askToRename(w files.Work) {
	was := w.Names[0]
	f := a.newForm("Rename " + was)
	name := f.AddField("Name", a.newField("what to call it", 0))
	name.SetText(was)
	f.AddButton(ui.Button{Title: "Rename", Do: func() error {
		to := strings.TrimSpace(name.Text())
		if err := plainName(w.From.FS(), to); err != nil {
			return err
		}
		if to == was {
			return errors.New("that is what it is called already")
		}
		a.pump.post(func() { a.renameTo(w.From, was, to) })
		return nil
	}})
	f.AddButton(ui.Button{Title: "Cancel"})
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
				a.reportError("Could not rename it",
					fmt.Errorf("%s is already there", to))
			})
			return
		case err != nil && !errors.Is(err, fs.ErrNotExist):
			// Something else went wrong looking. Renaming replaces what
			// is there, so going ahead without knowing is how a file is
			// lost.
			a.pump.post(func() { a.reportError("Could not rename it", err) })
			return
		}
		err = f.Rename(from, at)
		a.pump.post(func() {
			if err != nil {
				a.reportError("Could not rename it", err)
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
	f := a.newConfirm("Replace "+vfs.Base(c.To, c.Path)+"?", wrapLines(
		fmt.Sprintf("On %s there is already a %s of %s, changed %s.",
			c.To.Name(), what(c.Have), size(c.Have.Size), when(c.Have)), errorLineWidth))

	answered := false
	answer := func(choice jobs.Choice) {
		if answered {
			return
		}
		answered = true
		answers <- choice
	}
	f.AddButton(ui.Button{Title: "Replace", Do: func() error {
		answer(jobs.Choice{What: jobs.Replace})
		return nil
	}})
	f.AddButton(ui.Button{Title: "Replace all", Do: func() error {
		answer(jobs.Choice{What: jobs.Replace, All: true})
		return nil
	}})
	f.AddButton(ui.Button{Title: "Skip", Do: func() error {
		answer(jobs.Choice{What: jobs.Skip})
		return nil
	}})
	f.AddButton(ui.Button{Title: "Skip all", Do: func() error {
		answer(jobs.Choice{What: jobs.Skip, All: true})
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

// closeFilesOn takes away every pane of the file manager that is on a
// machine, for a connection that has gone.
func (a *app) closeFilesOn(host string) error {
	b := a.files
	if b == nil {
		return nil
	}
	var errs []error
	for _, p := range b.view.Panes() {
		if p.FS().Name() != host {
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
	err := a.releaseFS(p.FS())
	b := a.files
	if b == nil {
		return errors.Join(err, fmt.Errorf("the pane on %s belonged to no file manager", p.FS().Name()))
	}
	if row := b.rows[p]; row != nil {
		a.registry.Drop(row)
		delete(b.rows, p)
	}
	if len(b.rows) == 0 {
		// The manager went with its last pane, so the window has none.
		a.files = nil
	}
	return err
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
		return f.Close()
	}
	// Counted before the goroutine starts, so a window closing in the
	// same frame still waits for it.
	a.closing.Add(1)
	go func() {
		defer a.closing.Done()
		for _, j := range stopping {
			<-j.Done()
		}
		if err := f.Close(); err != nil {
			a.closeFailed(fmt.Errorf("could not close %s: %w", f.Name(), err))
		}
	}()
	return nil
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
