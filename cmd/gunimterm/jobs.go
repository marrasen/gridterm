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
	// DropFileClip empties the file clipboard.
	DropFileClip struct{}
	// ShowJobs opens the jobs pane, or goes to it.
	ShowJobs struct{}
	// CancelJob stops a job part way.
	CancelJob struct{ ID string }
	// ClearJobs takes the finished jobs off the jobs pane.
	ClearJobs struct{}
	// DropJob takes one finished job off the jobs pane.
	DropJob struct{ ID string }
)

// Job is one piece of file work, as the jobs pane shows it.
type Job struct {
	ID    string
	Title string
	// Machine is where it works, as its row in the sidebar is filed,
	// and Kind is copy, move or delete.
	Machine, Kind string
	// Detail says how far it has got, or how it ended.
	Detail string
	// Share is how much is done, from 0 to 1, and below zero while the
	// job is still working out how much there is.
	Share float32
	// Done is set once it has stopped, and Failed when that was for a
	// reason other than finishing or being cancelled.
	Done, Failed bool
	// Names are what it works on, Current the one it is on, and Ticked
	// how many of Names are done, when they are counted one by one.
	Names   []string
	Current string
	Ticked  int
	// Speeds are its speed over the last while, oldest first, for a
	// graph, and Sampled how many samples have been taken in all, so a
	// graph can tell the new ones.
	Speeds  []uint64
	Sampled int
	// Repeatable says a finished copy can be run again, and Saved that
	// it is on the saved list.
	Repeatable, Saved bool
}

// kindJobs is the jobs pane.
const kindJobs = "jobs"

// mostFinishedJobs is how many finished jobs the pane keeps.
const mostFinishedJobs = 20

// fileClip is what the file clipboard holds.
type fileClip struct {
	kind    jobs.Kind
	from    vfs.FS
	machine string
	at      string
	names   []string
}

// FileClip is the file clipboard as the window shows it: the names
// waiting in folder At of the files kept under Key.
type FileClip struct {
	Key, At string
	Names   []string
	Cut     bool
}

// jobKind names a kind of job for its row's icon.
func jobKind(k jobs.Kind) string {
	switch k {
	case jobs.Move:
		return "move"
	case jobs.Delete:
		return "delete"
	}
	return "copy"
}

// running is a job the program follows.
type running struct {
	id    string
	job   *jobs.Job
	title string
	// op is the work, and from and to the machines it is between, to
	// do it again; speeds are its speed, sampled as it is looked at,
	// from lastBytes at lastAt.
	op       jobs.Op
	from, to string
	// fromID and toID are the saved servers at either end, which a
	// repeat finds by id, whatever they are called by then.
	fromID, toID string
	// repeating says a repeat has been asked for and has not started
	// yet: a machine opened again takes as long as a connection does,
	// and a second press in that time would copy the same thing twice.
	repeating bool
	speeds    []uint64
	sampled   int
	lastBytes int64
	lastAt    time.Time
	// ended is set once its end has been reported.
	ended bool
	// panes are the file panes to list again once it is done.
	panes []string
}

// folderOf returns a file pane's filesystem and folder.
func (a *app) folderOf(pane string) (vfs.FS, string, bool) {
	b, ok := a.st.Browsers[pane]
	f := a.filesOf(pane)
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
	a.clip = &fileClip{kind: kind, from: f, machine: a.filesKey(in.Pane), at: at, names: in.Names}
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
	a.followOn(op, fmt.Sprintf("%s %s to %s", verb, count(len(c.names), "item"), vfs.Base(f, into)), c.machine, a.filesKey(in.Pane))
	return nil
}

func (a *app) deleteFiles(in DeleteFiles) {
	f, at, ok := a.folderOf(in.Pane)
	if !ok || len(in.Names) == 0 {
		return
	}
	a.followOn(jobs.Op{Kind: jobs.Delete, From: f, At: at, Names: in.Names}, "Deleting "+count(len(in.Names), "item"), a.filesKey(in.Pane), "")
}

// follow starts a job and follows it, listing again the file panes
// showing either end once it is done.
func (a *app) follow(op jobs.Op, title string) { a.followOn(op, title, "", "") }

// followOn is follow, for a job between the machines from and to, which
// a repeat opens again.
func (a *app) followOn(op jobs.Op, title, from, to string) *jobs.Job {
	if a.jobs == nil {
		a.jobs = jobs.New(2)
	}
	job := a.jobs.Start(a.ctx, op, jobs.Options{Ask: overwriteAsker{a}})
	var panes []string
	for id, b := range a.st.Browsers {
		f := a.filesOf(id)
		if f == nil {
			continue
		}
		if (vfs.Same(f, op.From) && b.Path == op.At) || (op.To != nil && vfs.Same(f, op.To) && b.Path == op.Into) {
			panes = append(panes, id)
		}
	}
	a.clearJobs(false)
	a.jobSeq++
	a.running = append(a.running, &running{id: "j" + itoa(a.jobSeq), job: job, title: title, panes: panes, op: op, from: from, to: to,
		fromID: a.serverID(from), toID: a.serverID(to)})
	if !a.watching {
		a.watching = true
		go a.watchJobs()
	}
	a.showJobs()
	return job
}

// watchJobs looks at the jobs four times a second while any runs.
func (a *app) watchJobs() {
	t := time.NewTicker(sampleEvery)
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
		r.sample(p)
		row := jobRow(r, p)
		row.Saved = a.isSaved(r)
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
		case jobs.Trouble(p.Err) != nil:
			a.notify(r.title+" stopped", jobs.Outcome(p), "")
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
	row := Job{ID: r.id, Title: r.title, Share: -1, Done: p.Done, Names: r.op.Names, Current: p.Current,
		Speeds: slices.Clone(r.speeds), Sampled: r.sampled, Repeatable: p.Done && r.op.Kind == jobs.Copy, Machine: r.to, Kind: jobKind(r.op.Kind)}
	if r.op.Kind == jobs.Delete {
		row.Machine = r.from
	}
	if p.Files == len(r.op.Names) {
		row.Ticked = p.FilesDone
	}
	switch {
	case p.Bytes > 0:
		row.Share = float32(p.BytesDone) / float32(p.Bytes)
	case p.Files > 0:
		row.Share = float32(p.FilesDone) / float32(p.Files)
	}
	var said []string
	switch {
	case p.Done && p.Err != nil:
		// A job the user stopped says so, and how far it got; one that
		// failed, or left half a file behind, says why.
		row.Failed = jobs.Trouble(p.Err) != nil
		said = append(said, jobs.Outcome(p))
		if p.FilesDone > 0 {
			said = append(said, count(p.FilesDone, "file")+" done")
		}
	case p.Done:
		row.Share = 1
		done := count(p.FilesDone, "file") + " done"
		if p.Skipped > 0 {
			done += fmt.Sprintf(", %d left as they were", p.Skipped)
		}
		said = append(said, done)
		took := p.Ended.Sub(p.Started)
		if took >= time.Second {
			said = append(said, "in "+took.Round(time.Second).String())
		}
		if took > 0 && p.BytesDone > 0 {
			said = append(said, humanSize(int64(float64(p.BytesDone)/took.Seconds()))+"/s on average")
		}
	case p.Files == 0:
		said = append(said, "Counting…")
	default:
		said = append(said, fmt.Sprintf("%d of %s", p.FilesDone, count(p.Files, "file")))
		if !p.Started.IsZero() {
			said = append(said, jobs.Going(time.Since(p.Started)))
		}
		if p.Bytes > 0 {
			said = append(said, humanSize(p.BytesDone)+" of "+humanSize(p.Bytes))
		}
		if speed := r.speedNow(); speed > 0 {
			said = append(said, humanSize(int64(speed))+"/s")
			if p.Bytes > p.BytesDone {
				left := time.Duration(float64(p.Bytes-p.BytesDone) / float64(speed) * float64(time.Second))
				if left < time.Second {
					said = append(said, "about a second left")
				} else {
					said = append(said, "about "+left.Round(time.Second).String()+" left")
				}
			}
		}
	}
	row.Detail = strings.Join(said, " · ")
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
	switch {
	case errors.Is(err, errDeclined):
		// Stop is an answer, and the job ends saying it was stopped.
		return jobs.Choice{What: jobs.Stop}, nil
	case err != nil:
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

// mostSpeeds is how many speed samples a job keeps for its graph, and
// sampleEvery how often one is taken: twelve seconds, ten a second.
const (
	mostSpeeds  = 120
	sampleEvery = 100 * time.Millisecond
)

// sample writes down how fast the job went since it was last looked at.
func (r *running) sample(p jobs.Progress) {
	now := time.Now()
	if !r.lastAt.IsZero() && !p.Done {
		if dt := now.Sub(r.lastAt).Seconds(); dt > 0 {
			r.speeds = append(r.speeds, uint64(max(0, float64(p.BytesDone-r.lastBytes)/dt)))
			r.sampled++
			if len(r.speeds) > mostSpeeds {
				r.speeds = r.speeds[len(r.speeds)-mostSpeeds:]
			}
		}
	}
	r.lastBytes, r.lastAt = p.BytesDone, now
}

// speedNow is the job's speed over its last second.
func (r *running) speedNow() uint64 {
	n := min(int(time.Second/sampleEvery), len(r.speeds))
	if n == 0 {
		return 0
	}
	var sum uint64
	for _, s := range r.speeds[len(r.speeds)-n:] {
		sum += s
	}
	return sum / uint64(n)
}
