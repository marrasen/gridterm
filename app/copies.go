package app

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/marrasen/kakel/machines"
	"github.com/marrasen/kakel/words"

	"github.com/marrasen/kakel/jobs"
	"github.com/marrasen/kakel/settings"
	"github.com/marrasen/kakel/vfs"
)

// Doing a copy again, and keeping copies to do again:
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

// KindCopies is the Saved Copies pane.
const KindCopies = "copies"

// mostSavedCopies is how many copies are kept.
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
		From: a.keptAs(r.from), To: a.keptAs(r.to), FromID: a.serverID(r.from), ToID: a.serverID(r.to),
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
	for _, m := range []machines.ID{r.from, r.to} {
		if err := a.stillSaved(m); err != nil {
			return err
		}
	}
	r.repeating = true
	return a.copyBetween(r.from, r.to, r.op.At, r.op.Into, r.op.Names, func() { r.repeating = false })
}

// copyBetween copies names from a folder on one machine into a folder
// on another, opening the files of both first. over is run once the
// copy has started, or could not.
func (a *app) copyBetween(from, to machines.ID, at, into string, names []string, over func()) error {
	err := a.withFilesOr(from, func(ff vfs.FS) {
		if err := a.withFilesOr(to, func(tf vfs.FS) {
			over()
			op := jobs.Op{Kind: jobs.Copy, From: ff, At: at, Names: names, To: tf, Into: into}
			a.followOn(op, "Copying "+words.Count(len(names), "item")+" to "+vfs.Base(tf, into), from, to)
		}, over); err != nil {
			over()
			a.failed("Couldn't copy", err.Error())
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

// stillSaved refuses a machine whose saved server, or saved window it
// is reached through, was removed from the list since. A listed piece
// of work keeps its quick connections, so they are there still.
func (a *app) stillSaved(m machines.ID) error {
	window, _, _ := m.Far()
	if window == machines.Local || a.machines.IsQuick(window) {
		return nil
	}
	if _, ok := a.machines.Saved(window); !ok {
		return fmt.Errorf("%s was removed from the server list", a.machines.Name(window))
	}
	return nil
}

// machineNow is the machine a piece of work kept from before runs on:
// the saved server with id, whatever it is called now, or, kept with
// none, this computer or a quick connection to name, the address it was
// kept with. A server removed from the list is refused.
func (a *app) machineNow(name, id string) (machines.ID, error) {
	if window, host, far := strings.Cut(name, KeptFarSep); far {
		// Beyond a window, the window found as it was kept.
		w, err := a.machineNow(window, id)
		if err != nil {
			return "", err
		}
		return machines.FarID(w, host), nil
	}
	switch {
	case id == "" && name == "":
		return "", nil
	case id == "":
		// A quick connection, the one there is to that address or one
		// made for it.
		if known, ok := a.machines.Find(name); ok {
			if _, _, far := known.Far(); !far {
				return known, nil
			}
		}
		return a.machines.NewQuick(name, false), nil
	case a.book == nil:
		return "", fmt.Errorf("the server list could not be read, so %s cannot be found", name)
	}
	if _, ok := a.machines.Saved(machines.ID(id)); !ok {
		return "", fmt.Errorf("%s was removed from the server list", name)
	}
	return machines.ID(id), nil
}

// keptAs is what a piece of work kept for next time says it runs on,
// beside the ID of the saved server it runs on: the server's name, for
// the list to show, a quick connection's address, to connect to it again
// by, and "" for this computer. Beyond a window: the window so, and
// its name there, which the window takes back as it does the key and
// which lasts where a quick connection's key does not.
func (a *app) keptAs(machine machines.ID) string {
	if machine == machines.Local {
		return ""
	}
	if window, host, far := machine.Far(); far {
		return a.keptAs(window) + KeptFarSep + a.machines.FarName(window, host)
	}
	return a.machines.Name(machine)
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
		if p.Kind == KindCopies {
			a.bringHere(p.ID)
			return
		}
	}
	a.next++
	a.addPane(Pane{ID: "p" + itoa(a.next), Title: "Saved Copies", Kind: KindCopies}, nil, Placement{})
}

// CopiedWhat names what a saved copy copies.
func CopiedWhat(c settings.SavedCopy) string {
	if len(c.Names) == 1 {
		return c.Names[0]
	}
	return words.Count(len(c.Names), "item") + ": " + strings.Join(c.Names, ", ")
}

// CopiedWhere says where a saved copy goes from and to: a saved server
// by what named calls it now, and anything else as it was kept.
func CopiedWhere(c settings.SavedCopy, named func(machines.ID) string) string {
	end := func(machine, id, at string) string {
		window, host, far := strings.Cut(machine, KeptFarSep)
		switch {
		case far && id != "" && named != nil:
			machine = host + " through " + named(machines.ID(id))
		case far:
			machine = host + " through " + window
		case id != "" && named != nil:
			machine = named(machines.ID(id))
		case machine == "":
			machine = "this computer"
		}
		return at + " on " + machine
	}
	return end(c.From, c.FromID, c.At) + " → " + end(c.To, c.ToID, c.Into)
}
