package main

import (
	"errors"
	"slices"
	"strings"

	"github.com/marrasen/gridterm/jobs"
	"github.com/marrasen/gridterm/settings"
	"github.com/marrasen/gridterm/vfs"
)

// Doing a copy again, and keeping copies to do again, as gridterm does:
// a finished copy can be repeated, and saved, and the saved ones run
// from the Saved Copies pane or the palette. The machines at either
// end are opened again first, as their files may have been closed.

// Intents for repeating and saving copies.
type (
	// RepeatJob does a finished copy again.
	RepeatJob struct{ ID string }
	// SaveCopy puts a finished copy on the saved list, or with On
	// false, takes it off.
	SaveCopy struct {
		ID string
		On bool
	}
	// RunSavedCopy does a saved copy.
	RunSavedCopy struct{ Saved settings.SavedCopy }
	// ForgetCopy takes a copy off the saved list.
	ForgetCopy struct{ Saved settings.SavedCopy }
	// ShowCopies opens the Saved Copies pane, or goes to it.
	ShowCopies struct{}
)

// kindCopies is the Saved Copies pane.
const kindCopies = "copies"

// mostSavedCopies is how many copies are kept, as gridterm keeps them.
const mostSavedCopies = 50

// runningNamed is the job with id, or nil.
func (a *app) runningNamed(id string) *running {
	for _, r := range a.running {
		if r.id == id {
			return r
		}
	}
	return nil
}

// savedCopyOf is a copy written down to keep.
func (a *app) savedCopyOf(r *running) settings.SavedCopy {
	return settings.SavedCopy{
		From: r.from, To: r.to, FromID: a.serverID(r.from), ToID: a.serverID(r.to),
		At: r.op.At, Into: r.op.Into, Names: slices.Clone(r.op.Names),
	}
}

// repeatJob does a finished copy again, between the same folders.
func (a *app) repeatJob(id string) error {
	r := a.runningNamed(id)
	if r == nil || r.op.Kind != jobs.Copy {
		return errors.New("there is no finished copy to do again")
	}
	return a.copyBetween(r.from, r.to, r.op.At, r.op.Into, r.op.Names)
}

// copyBetween copies names from a folder on one machine into a folder
// on another, opening the files of both first.
func (a *app) copyBetween(from, to, at, into string, names []string) error {
	return a.withFiles(from, func(ff vfs.FS) {
		if err := a.withFiles(to, func(tf vfs.FS) {
			op := jobs.Op{Kind: jobs.Copy, From: ff, At: at, Names: names, To: tf, Into: into}
			a.followOn(op, "Copying "+count(len(names), "item")+" to "+vfs.Base(tf, into), from, to)
		}); err != nil {
			a.notify("Couldn't copy", err.Error(), "")
		}
	})
}

// saveCopy keeps a finished copy, or forgets it.
func (a *app) saveCopy(in SaveCopy) error {
	r := a.runningNamed(in.ID)
	if r == nil || a.settings == nil {
		return nil
	}
	saved := a.savedCopyOf(r)
	var err error
	if in.On {
		err = a.settings.KeepCopy(saved, mostSavedCopies)
	} else {
		err = a.settings.DropCopy(saved)
	}
	a.st.SavedCopies = a.settings.Copies()
	return err
}

// isSaved reports whether a job's copy is on the saved list.
func (a *app) isSaved(r *running) bool {
	if r.op.Kind != jobs.Copy {
		return false
	}
	saved := a.savedCopyOf(r)
	return slices.ContainsFunc(a.st.SavedCopies, func(c settings.SavedCopy) bool { return sameCopy(c, saved) })
}

// sameCopy reports whether two saved copies are the same work.
func sameCopy(x, y settings.SavedCopy) bool {
	return x.From == y.From && x.To == y.To && x.At == y.At && x.Into == y.Into && slices.Equal(x.Names, y.Names)
}

// runSavedCopy does a saved copy, between the machines it was saved
// for, by the names they have now.
func (a *app) runSavedCopy(c settings.SavedCopy) error {
	return a.copyBetween(a.nameNow(c.From, c.FromID), a.nameNow(c.To, c.ToID), c.At, c.Into, c.Names)
}

// nameNow is a saved server's name now, by its id, or name as it was.
func (a *app) nameNow(name, id string) string {
	for _, h := range a.st.Saved {
		if id != "" && h.ID == id {
			return h.Name
		}
	}
	return name
}

// forgetCopy takes a copy off the saved list.
func (a *app) forgetCopy(c settings.SavedCopy) error {
	if a.settings == nil {
		return nil
	}
	err := a.settings.DropCopy(c)
	a.st.SavedCopies = a.settings.Copies()
	return err
}

// showCopies opens the Saved Copies pane, or goes to it.
func (a *app) showCopies() {
	for _, p := range a.st.Panes {
		if p.Kind == kindCopies {
			a.st.Focus = p.ID
			return
		}
	}
	a.next++
	a.addPane(Pane{ID: "p" + itoa(a.next), Title: "Saved Copies", Kind: kindCopies}, nil, placement{})
}

// copiedWhat names what a saved copy copies.
func copiedWhat(c settings.SavedCopy) string {
	if len(c.Names) == 1 {
		return c.Names[0]
	}
	return count(len(c.Names), "item") + ": " + strings.Join(c.Names, ", ")
}

// copiedWhere says where a saved copy goes from and to.
func copiedWhere(c settings.SavedCopy) string {
	end := func(machine, at string) string {
		if machine == "" {
			machine = "this computer"
		}
		return at + " on " + machine
	}
	return end(c.From, c.At) + " → " + end(c.To, c.Into)
}
