package main

import (
	"errors"
	"fmt"
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
	if r.repeating {
		return nil
	}
	from, err := a.machineNow(r.from, r.fromID)
	if err != nil {
		return err
	}
	to, err := a.machineNow(r.to, r.toID)
	if err != nil {
		return err
	}
	r.repeating = true
	return a.copyBetween(from, to, r.op.At, r.op.Into, r.op.Names, func() { r.repeating = false })
}

// copyBetween copies names from a folder on one machine into a folder
// on another, opening the files of both first. over is run once the
// copy has started, or could not.
func (a *app) copyBetween(from, to, at, into string, names []string, over func()) error {
	err := a.withFilesOr(from, func(ff vfs.FS) {
		if err := a.withFilesOr(to, func(tf vfs.FS) {
			over()
			op := jobs.Op{Kind: jobs.Copy, From: ff, At: at, Names: names, To: tf, Into: into}
			a.followOn(op, "Copying "+count(len(names), "item")+" to "+vfs.Base(tf, into), from, to)
		}, over); err != nil {
			over()
			a.notify("Couldn't copy", err.Error(), "")
		}
	}, over)
	if err != nil {
		over()
	}
	return err
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
	from, err := a.machineNow(c.From, c.FromID)
	if err != nil {
		return err
	}
	to, err := a.machineNow(c.To, c.ToID)
	if err != nil {
		return err
	}
	return a.copyBetween(from, to, c.At, c.Into, c.Names, func() {})
}

// machineNow is the machine a piece of work kept from before runs on:
// the saved server with id, as the connection to it is called, or by
// its name on the list now. A server removed from the list is refused,
// and so is one whose name a connection to another machine holds. A
// machine kept without an id is name, as it was.
func (a *app) machineNow(name, id string) (string, error) {
	if id == "" {
		return name, nil
	}
	for held, heldID := range a.connIDs {
		if heldID == id {
			return held, nil
		}
	}
	now := ""
	for _, h := range a.st.Saved {
		if h.ID == id {
			now = h.Name
		}
	}
	if now == "" {
		return "", fmt.Errorf("%s was removed from the server list", name)
	}
	if heldID := a.connIDs[now]; heldID != "" {
		return "", fmt.Errorf("%s is connected to another machine", now)
	}
	return now, nil
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
