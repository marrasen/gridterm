package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/jobs"
	"github.com/marrasen/gridterm/meter"
	"github.com/marrasen/gridterm/ui"
	"github.com/marrasen/gridterm/ui/files"
	"github.com/marrasen/gridterm/vfs"
)

// browser is a two-pane file browser in the window, with a panel row for
// each side.
type browser struct {
	view *files.Browser

	// left and right are the rows on the panel, one per side, so a
	// browser touching two machines is shown under both of them.
	left, right *conns.Entry
}

// openFilesHere opens a browser on the machine the user is looking at,
// with this machine in the other pane.
func (a *app) openFilesHere() error {
	host := a.currentHost()
	if a.isHere(host) {
		// Both sides on this machine, which is a browser of its own
		// worth: two directories at once.
		return a.openBrowser(conns.Local, conns.Local)
	}
	return a.openBrowser(conns.Local, host)
}

// openBrowser puts a browser in a tab, with a filesystem on each side.
func (a *app) openBrowser(left, right string) error {
	leftFS, err := a.filesystem(left)
	if err != nil {
		return err
	}
	rightFS, err := a.filesystem(right)
	if err != nil {
		_ = leftFS.Close()
		return err
	}

	b := &browser{}
	b.view = files.NewBrowser(a.newPane(leftFS, b, true), a.newPane(rightFS, b, false),
		func(s *ui.Split) {
			s.DividerFG = a.colours.FG
			s.DividerBG = a.colours.BG
		})
	a.wireBrowser(b)

	if err := a.placeTab(b.view); err != nil {
		_ = b.view.Close()
		return err
	}
	a.browsers[b.view] = b
	// The browser is what the tree holds, and it opens on its left side.
	a.focus(b.view)
	leftPane, _ := b.view.Panes()
	b.view.Focus(leftPane)
	a.showBrowser(b, left, right)
	a.relayout()
	return nil
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

// newPane builds one side of a browser.
func (a *app) newPane(f vfs.FS, b *browser, left bool) *files.Pane {
	p := files.New(f)
	p.Style = files.Style{
		FG: a.colours.FG,
		BG: a.colours.BG,
		// The bar is marked the way a selected tab is, so the two read
		// as the same thing.
		SelectedFG: a.colours.BG,
		SelectedBG: a.colours.FG,
		// Where the pane is, in the colour a machine's name has on the
		// panel.
		HeaderFG: a.colours.ANSI[6],
		DirFG:    a.colours.ANSI[4],
		LinkFG:   a.colours.ANSI[6],
		MarkedFG: a.colours.ANSI[3],
		NoteFG:   a.colours.ANSI[8],
		// Red, because a line saying why something failed has to read as
		// a failure before it is read as words.
		ErrorFG: a.colours.ANSI[1],
	}
	// Off the drawing goroutine, and back onto it with the answer: a
	// directory on a machine with a long way to go would otherwise stop
	// the window while it was read.
	p.Read = func(f vfs.FS, path string, then func([]vfs.Entry, error)) {
		go func() {
			entries, err := f.ReadDir(path)
			a.pump.post(func() { then(entries, err) })
		}()
	}
	p.OnChange = func() { a.browserMoved(b, left) }
	return p
}

// wireBrowser says what the F keys do.
func (a *app) wireBrowser(b *browser) {
	b.view.OnCopy = func(w files.Work) { a.startJob(jobs.Copy, w) }
	b.view.OnMove = func(w files.Work) { a.startJob(jobs.Move, w) }
	b.view.OnDelete = func(w files.Work) { a.confirmDelete(w) }
	b.view.OnMkdir = func(w files.Work) { a.askForDirectory(w) }
	b.view.OnRename = func(w files.Work) { a.askToRename(w) }
}

// showBrowser puts a row on the panel for each side.
func (a *app) showBrowser(b *browser, left, right string) {
	leftPane, rightPane := b.view.Panes()
	b.left = a.browserRow(b, leftPane, left)
	b.right = a.browserRow(b, rightPane, right)
	a.registry.Add(b.left)
	a.registry.Add(b.right)
}

// browserRow is one side of a browser on the panel.
//
// One row per side rather than one per browser: a browser touching two
// machines is open on both of them, and closing either machine has to
// take it away.
func (a *app) browserRow(b *browser, p *files.Pane, host string) *conns.Entry {
	return &conns.Entry{
		Host:  host,
		Kind:  conns.Files,
		Label: p.At(),
		// The browser is what the tree holds; which side of it has the
		// keys is the browser's own business.
		Reveal: func() {
			a.focus(b.view)
			b.view.Focus(p)
		},
		Close: func() error { return a.closePane(b.view) },
	}
}

// browserMoved keeps the panel saying where a pane is.
func (a *app) browserMoved(b *browser, left bool) {
	row := b.right
	if left {
		row = b.left
	}
	if row == nil {
		return
	}
	pane, other := b.view.Panes()
	if !left {
		pane = other
	}
	row.Label = pane.At()
	a.markDirty()
}

// startJob puts file work on the queue and a row on the panel for it.
func (a *app) startJob(kind jobs.Kind, w files.Work) {
	if w.From == nil || (kind != jobs.Delete && w.To == nil) {
		return
	}
	op := jobs.Op{
		Kind: kind, From: w.From.FS(), At: w.From.At(), Names: w.Names,
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
		a.registry.Drop(e)
		return nil
	}
	a.jobs[e] = j
	a.registry.Add(e)
	a.markDirty()
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
		if p.Err != nil {
			a.reportError("Could not finish "+j.Name(), p.Err)
		}
		// What it changed is in front of the user, so it is read again.
		for _, b := range a.browsers {
			b.view.Reload()
		}
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
		"They go from "+w.From.FS().Name()+", at "+w.From.At()+
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
	f.Lines = wrapLines("It is made in "+w.From.At()+".", errorLineWidth)
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
		_, err := f.Stat(at)
		if err == nil {
			a.pump.post(func() {
				a.reportError("Could not rename it",
					fmt.Errorf("%s is already there", to))
			})
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
		// The job has been given up on, so there is no answer to act on.
		return jobs.Choice{}, ctx.Err()
	}
}

// showOverwrite puts the question on the screen. It runs on the drawing
// goroutine.
func (a *app) showOverwrite(c jobs.Conflict, answers chan<- jobs.Choice) {
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
	// Whichever way the dialog goes away, the job is told: one that was
	// never answered would hold its goroutine until the window closed.
	a.showForm(f, func() { answer(jobs.Choice{What: jobs.Stop}) })
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

// browsersOn returns the browsers with a side on a machine.
func (a *app) browsersOn(host string) []*browser {
	var found []*browser
	for _, b := range a.browsers {
		left, right := b.view.Panes()
		if left.FS().Name() == host || right.FS().Name() == host {
			found = append(found, b)
		}
	}
	return found
}

// closeBrowser takes a browser off the panel, for one whose pane has
// gone.
func (a *app) closeBrowser(w ui.Widget) error {
	b := a.browsers[w]
	if b == nil {
		return nil
	}
	delete(a.browsers, w)
	a.registry.Drop(b.left)
	a.registry.Drop(b.right)
	return b.view.Close()
}
