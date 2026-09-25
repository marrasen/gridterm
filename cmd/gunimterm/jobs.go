package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
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
)

// fileClip is what the file clipboard holds.
type fileClip struct {
	kind  jobs.Kind
	from  vfs.FS
	at    string
	names []string
}

// running is a job the program follows.
type running struct {
	job   *jobs.Job
	title string
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
	a.running = append(a.running, &running{job: job, title: title, panes: panes})
	if len(a.running) == 1 {
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

// showJobs puts the running jobs in the status line, and reports the
// ones that ended. It reports whether any still runs.
func (a *app) showJobs() bool {
	var still []*running
	var lines []string
	for _, r := range a.running {
		p := r.job.Progress()
		if !p.Done {
			still = append(still, r)
			line := r.title + "…"
			if p.Bytes > 0 {
				line = fmt.Sprintf("%s, %d%%", r.title, int(100*p.BytesDone/p.Bytes))
			}
			lines = append(lines, line)
			continue
		}
		switch {
		case p.Err != nil && !errors.Is(p.Err, context.Canceled):
			a.notify(r.title+" stopped", p.Err.Error(), "")
		default:
			body := fmt.Sprintf("%s done", count(p.FilesDone, "file"))
			if p.Skipped > 0 {
				body += fmt.Sprintf(", %d left as they were", p.Skipped)
			}
			a.notify(strings.Replace(strings.Replace(strings.Replace(r.title, "Copying", "Copied", 1), "Moving", "Moved", 1), "Deleting", "Deleted", 1), body, "")
		}
		for _, id := range r.panes {
			if b, ok := a.st.Browsers[id]; ok {
				a.browse(Browse{Pane: id, Path: b.Path})
			}
		}
	}
	a.running = still
	a.st.Status = strings.Join(lines, "  ·  ")
	return len(still) > 0
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
