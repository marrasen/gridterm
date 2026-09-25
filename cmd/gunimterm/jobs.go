package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strings"
	"time"

	"github.com/marrasen/gridterm/jobs"
	"github.com/marrasen/gridterm/vfs"
)

// Copying, moving, deleting, renaming and making folders, with
// gridterm's jobs package, on the program's side.

// Intents for working on files.
type (
	// ClipFiles puts names from a file pane's folder on the file
	// clipboard, to copy, or to move with Cut.
	ClipFiles struct {
		Pane  string
		Names []string
		Cut   bool
	}
	// PasteFiles copies or moves what the file clipboard holds into a
	// file pane's folder.
	PasteFiles struct{ Pane string }
	// DeleteFiles deletes names from a file pane's folder, which the
	// user has already been asked about.
	DeleteFiles struct {
		Pane  string
		Names []string
	}
	// RenameFile renames one name in a file pane's folder.
	RenameFile struct{ Pane, From, To string }
	// MakeFolder makes a folder in a file pane's folder.
	MakeFolder struct{ Pane, Name string }
	// ShowJobs opens the jobs pane, or goes to it.
	ShowJobs struct{}
	// CancelJob stops a job part way.
	CancelJob struct{ ID string }
	// ClearJobs takes the finished jobs off the jobs pane.
	ClearJobs struct{}
)

// Job is one piece of file work, as the jobs pane shows it.
type Job struct {
	ID    string
	Title string
	// Detail says how far it has got, or how it ended.
	Detail string
	// Share is how much is done, from 0 to 1, and below zero while the
	// job is still working out how much there is.
	Share float32
	// Done is set once it has stopped, and Failed when that was for a
	// reason other than finishing or being cancelled.
	Done, Failed bool
}

// kindJobs is the jobs pane.
const kindJobs = "jobs"

// mostFinishedJobs is how many finished jobs the pane keeps.
const mostFinishedJobs = 20

// fileClip is what the file clipboard holds.
type fileClip struct {
	kind  jobs.Kind
	from  vfs.FS
	at    string
	names []string
}

// running is a job the program follows.
type running struct {
	id    string
	job   *jobs.Job
	title string
	// ended is set once its end has been reported.
	ended bool
	// panes are the file panes to list again once it is done.
	panes []string
}

// folderOf returns a file pane's filesystem and folder.
func (a *app) folderOf(pane string) (vfs.FS, string, bool) {
	b, ok := a.st.Browsers[pane]
	f := a.fsFor(a.machineOf(pane))
	return f, b.Path, ok && f != nil
}

func (a *app) clipFiles(in ClipFiles) {
	f, at, ok := a.folderOf(in.Pane)
	if !ok || len(in.Names) == 0 {
		return
	}
	kind, verb := jobs.Copy, "copy"
	if in.Cut {
		kind, verb = jobs.Move, "move"
	}
	a.clip = &fileClip{kind: kind, from: f, at: at, names: in.Names}
	a.st.Status = fmt.Sprintf("Ready to %s %s; paste in a folder with F7 or Ctrl+V.", verb, count(len(in.Names), "item"))
}

func (a *app) pasteFiles(in PasteFiles) error {
	f, into, ok := a.folderOf(in.Pane)
	if !ok {
		return nil
	}
	c := a.clip
	if c == nil {
		return errors.New("gunimterm: nothing to paste; copy or cut files first, with F5 or F6")
	}
	if c.kind == jobs.Move {
		// A move happens once.
		a.clip = nil
	}
	a.st.Status = ""
	op := jobs.Op{Kind: c.kind, From: c.from, At: c.at, Names: c.names, To: f, Into: into}
	verb := "Copying"
	if c.kind == jobs.Move {
		verb = "Moving"
	}
	a.follow(op, fmt.Sprintf("%s %s to %s", verb, count(len(c.names), "item"), vfs.Base(f, into)))
	return nil
}

func (a *app) deleteFiles(in DeleteFiles) {
	f, at, ok := a.folderOf(in.Pane)
	if !ok || len(in.Names) == 0 {
		return
	}
	a.follow(jobs.Op{Kind: jobs.Delete, From: f, At: at, Names: in.Names}, "Deleting "+count(len(in.Names), "item"))
}

// follow starts a job and follows it, listing again the file panes
// showing either end once it is done.
func (a *app) follow(op jobs.Op, title string) {
	if a.jobs == nil {
		a.jobs = jobs.New(2)
	}
	job := a.jobs.Start(a.ctx, op, jobs.Options{Ask: overwriteAsker{a}})
	var panes []string
	for id, b := range a.st.Browsers {
		f := a.fsFor(a.machineOf(id))
		if f == nil {
			continue
		}
		if (vfs.Same(f, op.From) && b.Path == op.At) || (op.To != nil && vfs.Same(f, op.To) && b.Path == op.Into) {
			panes = append(panes, id)
		}
	}
	a.clearJobs(false)
	a.jobSeq++
	a.running = append(a.running, &running{id: "j" + itoa(a.jobSeq), job: job, title: title, panes: panes})
	if !a.watching {
		a.watching = true
		go a.watchJobs()
	}
	a.showJobs()
}

// watchJobs looks at the jobs four times a second while any runs.
func (a *app) watchJobs() {
	t := time.NewTicker(250 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-a.ctx.Done():
			return
		case <-t.C:
		}
		done := make(chan bool, 1)
		a.events <- func() { done <- a.showJobs() }
		if !<-done {
			return
		}
	}
}

// showJobs brings the jobs pane and the status line up to date, and
// reports the jobs that ended. It reports whether any still runs.
func (a *app) showJobs() bool {
	var lines []string
	var rows []Job
	live := false
	for _, r := range a.running {
		p := r.job.Progress()
		row := jobRow(r, p)
		rows = append(rows, row)
		if !p.Done {
			live = true
			lines = append(lines, r.title+"…")
			if row.Share >= 0 {
				lines[len(lines)-1] = fmt.Sprintf("%s, %d%%", r.title, int(100*row.Share))
			}
			continue
		}
		if r.ended {
			continue
		}
		r.ended = true
		switch {
		case p.Err != nil && !errors.Is(p.Err, context.Canceled):
			a.notify(r.title+" stopped", p.Err.Error(), "")
		case p.Err == nil:
			a.notify(pastTense(r.title), row.Detail, "")
		}
		for _, id := range r.panes {
			if b, ok := a.st.Browsers[id]; ok {
				a.browse(Browse{Pane: id, Path: b.Path})
			}
		}
	}
	a.st.Jobs = rows
	a.st.Status = strings.Join(lines, "  ·  ")
	if !live {
		a.watching = false
	}
	return live
}

// jobRow is a job as the pane shows it.
func jobRow(r *running, p jobs.Progress) Job {
	row := Job{ID: r.id, Title: r.title, Share: -1, Done: p.Done}
	switch {
	case p.Bytes > 0:
		row.Share = float32(p.BytesDone) / float32(p.Bytes)
	case p.Files > 0:
		row.Share = float32(p.FilesDone) / float32(p.Files)
	}
	switch {
	case p.Done && errors.Is(p.Err, context.Canceled):
		row.Detail = "Cancelled after " + count(p.FilesDone, "file")
	case p.Done && p.Err != nil:
		row.Failed, row.Detail = true, p.Err.Error()
	case p.Done:
		row.Share = 1
		row.Detail = count(p.FilesDone, "file") + " done"
		if p.Skipped > 0 {
			row.Detail += fmt.Sprintf(", %d left as they were", p.Skipped)
		}
		if took := p.Ended.Sub(p.Started); took >= time.Second {
			row.Detail += " in " + took.Round(time.Second).String()
		}
	case p.Files == 0:
		row.Detail = "Counting…"
	default:
		row.Detail = fmt.Sprintf("%d of %s", p.FilesDone, count(p.Files, "file"))
		if p.Bytes > 0 {
			row.Detail += fmt.Sprintf(" · %s of %s", humanSize(p.BytesDone), humanSize(p.Bytes))
			if took := time.Since(p.Started).Seconds(); took > 0.5 {
				row.Detail += " · " + humanSize(int64(float64(p.BytesDone)/took)) + "/s"
			}
		}
		if p.Current != "" {
			row.Detail += " · " + p.Current
		}
	}
	return row
}

// pastTense turns a job's title into what it did.
func pastTense(title string) string {
	for _, w := range [][2]string{{"Copying", "Copied"}, {"Moving", "Moved"}, {"Deleting", "Deleted"}} {
		if after, ok := strings.CutPrefix(title, w[0]); ok {
			return w[1] + after
		}
	}
	return title
}

// cancelJob stops a job part way.
func (a *app) cancelJob(id string) {
	for _, r := range a.running {
		if r.id == id {
			r.job.Cancel()
		}
	}
}

// clearJobs takes the finished jobs away, and keeps the newest few
// finished when there are too many to show.
func (a *app) clearJobs(all bool) {
	var keep []*running
	finished := 0
	for i := len(a.running) - 1; i >= 0; i-- {
		r := a.running[i]
		if r.ended {
			finished++
			if all || finished > mostFinishedJobs {
				continue
			}
		}
		keep = append(keep, r)
	}
	slices.Reverse(keep)
	a.running = keep
}

// showJobsPane opens the jobs pane, or goes to it.
func (a *app) showJobsPane() {
	for _, p := range a.st.Panes {
		if p.Kind == kindJobs {
			a.st.Focus = p.ID
			return
		}
	}
	a.next++
	a.addPane(Pane{ID: "p" + itoa(a.next), Title: "Jobs", Kind: kindJobs}, nil, placement{})
}

// renameFile renames one name, refusing to write over another, as
// gridterm does, where a change of letter case alone goes through.
func (a *app) renameFile(in RenameFile) {
	f, at, ok := a.folderOf(in.Pane)
	if !ok || in.To == in.From {
		return
	}
	if err := plainName(f, in.To); err != nil {
		a.notify("Couldn't rename "+in.From, upperFirst(err.Error())+".", "")
		return
	}
	from, to := vfs.Join(f, at, in.From), vfs.Join(f, at, in.To)
	go func() {
		_, err := f.Stat(to)
		switch {
		case err == nil && !strings.EqualFold(in.From, in.To):
			err = fmt.Errorf("%s is already there", in.To)
		case err != nil && !errors.Is(err, fs.ErrNotExist):
		default:
			err = f.Rename(from, to)
		}
		a.events <- func() {
			if err != nil {
				a.notify("Couldn't rename "+in.From, err.Error(), "")
				return
			}
			a.browse(Browse{Pane: in.Pane, Path: at, Land: in.To})
		}
	}()
}

// makeFolder makes a folder, and puts the cursor on it.
func (a *app) makeFolder(in MakeFolder) {
	f, at, ok := a.folderOf(in.Pane)
	if !ok {
		return
	}
	if err := plainName(f, in.Name); err != nil {
		a.notify("Couldn't make the folder", upperFirst(err.Error())+".", "")
		return
	}
	go func() {
		err := f.Mkdir(vfs.Join(f, at, in.Name), 0o755)
		a.events <- func() {
			if err != nil {
				a.notify("Couldn't make "+in.Name, err.Error(), "")
				return
			}
			a.browse(Browse{Pane: in.Pane, Path: at, Land: in.Name})
		}
	}()
}

// plainName reports whether name is a name in a folder rather than a
// path, as gridterm checks it.
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

// count writes n things, with the plural where it takes one.
func count(n int, thing string) string {
	if n == 1 {
		return "1 " + thing
	}
	return fmt.Sprintf("%d %ss", n, thing)
}

// overwriteAsker asks the user about a name that is already there.
type overwriteAsker struct{ a *app }

// Overwrite implements [jobs.Ask].
func (q overwriteAsker) Overwrite(ctx context.Context, c jobs.Conflict) (jobs.Choice, error) {
	name := vfs.Base(c.To, c.Path)
	text := fmt.Sprintf("%s is already in %s: %s, from %s. The one arriving is %s, from %s.",
		name, vfs.Dir(c.To, c.Path), describe(c.Have), c.Have.Mod.Format("2006-01-02 15:04"),
		describe(c.Want), c.Want.Mod.Format("2006-01-02 15:04"))
	ans, err := q.a.ask(ctx, Ask{Title: "Replace " + name + "?", Text: text,
		Choose: []string{"Replace", "Leave It"}, Also: "Do the same for the rest", No: "Stop"})
	if err != nil {
		return jobs.Choice{What: jobs.Stop}, err
	}
	what := jobs.Replace
	if len(ans.Answers) > 0 && ans.Answers[0] == "Leave It" {
		what = jobs.Skip
	}
	return jobs.Choice{What: what, All: len(ans.Answers) > 1 && ans.Answers[1] == "yes"}, nil
}

func describe(e vfs.Entry) string {
	if e.IsDir() {
		return "a folder"
	}
	return humanSize(e.Size)
}
