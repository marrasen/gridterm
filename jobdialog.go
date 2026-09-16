package main

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/jobs"
	"github.com/marrasen/gridterm/meter"
	"github.com/marrasen/gridterm/ui"
	"github.com/marrasen/gridterm/vfs"
)

// jobDialog is how one piece of file work is going, on screen: what it
// is on now, how far it has got, how fast it is going, and how it ended.
//
// A widget of its own rather than a plain form, so the window can pick
// it out of the modal stack and hand it a fresh reading every frame.
type jobDialog struct {
	*ui.Form

	app   *app
	job   *jobs.Job
	entry *conns.Entry

	// from and to are the machines the work was started between, taken
	// when the job began. A repeat looks them up again by name, because
	// the filesystems the job ran on may be closed by then.
	from, to string

	// said is what the dialog last put under its title, so a frame with
	// nothing new to say lays nothing out again.
	said []string

	// done is whether the buttons on screen are the ones a finished job
	// offers.
	done bool
}

// openJobDialog shows how a job is going. It runs on the drawing
// goroutine, from the job's row on the sidebar.
func (a *app) openJobDialog(j *jobs.Job, e *conns.Entry, from, to string) {
	d := &jobDialog{
		Form:  a.newForm(j.Kind().String() + " " + j.Name()),
		app:   a,
		job:   j,
		entry: e,
		from:  from,
		to:    to,
	}
	// The running pair first, so refresh has something to swap: it only
	// changes the buttons when the job's state does.
	d.setButtons()
	d.refresh(time.Now())
	// Opens on the button that changes nothing, the way the question
	// about deleting does.
	d.FocusButton(1)
	d.SetClose(a.showModal(d, nil))
}

// refresh gives the dialog a new reading, every frame while it is open.
func (d *jobDialog) refresh(now time.Time) {
	p := d.job.Progress()
	// Written only when it has changed. The same text costs nothing --
	// the buffer under the dialog writes each cell the same value and
	// dirties no row -- but laying the fields out again does not.
	if said := d.report(p, now); !slices.Equal(said, d.said) {
		d.said = said
		d.SetLines(said)
	}
	if p.Done != d.done {
		d.done = p.Done
		d.setButtons()
	}
}

// setButtons offers what can be done with the job as it stands: stopping
// one that is running, and doing a finished one again.
func (d *jobDialog) setButtons() {
	if !d.done {
		d.SetButtons([]ui.Button{
			{Title: "Cancel", Do: func() error {
				// Not Queue.Drop: the user asked for the work to stop,
				// not for the row to go, and the row is where the
				// outcome is read.
				d.job.Cancel()
				return nil
			}},
			{Title: "Close"},
		})
		return
	}
	d.SetButtons([]ui.Button{
		{Title: "Repeat", Do: func() error {
			// Posted for the reason AddButton gives: this dialog closes
			// as soon as this returns, and closing one takes anything
			// stacked on top of it -- which is where a failure to start
			// again would be shown.
			op, from, to := d.job.Op(), d.from, d.to
			d.app.pump.post(func() { d.app.repeatJob(op, from, to) })
			return nil
		}},
		{Title: "Close"},
	})
}

// report is what the dialog says about a job at one moment.
func (d *jobDialog) report(p jobs.Progress, now time.Time) []string {
	if !p.Done {
		return []string{
			d.onNow(p),
			amountOf(p, false),
			d.pace(p, now),
		}
	}
	lines := wrapLines(outcomeOf(p), errorLineWidth)
	return append(lines, amountOf(p, true)+" in "+lasted(p.Ended.Sub(p.Started)))
}

// onNow is the name the job is working on, or what it is doing instead.
func (d *jobDialog) onNow(p jobs.Progress) string {
	doing := "Working on"
	switch d.job.Kind() {
	case jobs.Copy:
		doing = "Copying"
	case jobs.Move:
		doing = "Moving"
	case jobs.Delete:
		doing = "Deleting"
	}
	switch {
	case p.Files == 0:
		return "Looking at what there is"
	case p.Current == "":
		return doing + " what was named"
	}
	return doing + " " + p.Current
}

// pace is how fast the job is going and how long it has been going for.
func (d *jobDialog) pace(p jobs.Progress, now time.Time) string {
	going := lasted(now.Sub(p.Started)) + " so far"
	if speed := meter.Speed(d.speed(now)); speed != "" {
		return speed + ", " + going
	}
	return going
}

// speed is the faster of the two directions on the job's row, in bytes
// per second.
//
// From the row's own rate rather than a new one: a speed is a difference
// between two moments, and the panel is already taking those.
func (d *jobDialog) speed(now time.Time) uint64 {
	if d.entry == nil || d.entry.Meter == nil {
		return 0
	}
	rate := d.app.rates[d.entry]
	if rate == nil {
		rate = &meter.Rate{}
		d.app.rates[d.entry] = rate
	}
	in, out := rate.Sample(d.entry.Meter, now)
	return max(in, out)
}

// amountOf is how much the job has done: files, and bytes where there
// are any. done asks for the totals rather than how far through it is.
func amountOf(p jobs.Progress, done bool) string {
	if done {
		line := fileWord(p.FilesDone)
		if p.BytesDone > 0 {
			line += ", " + size(p.BytesDone)
		}
		if p.Skipped > 0 {
			line += fmt.Sprintf(", %d skipped", p.Skipped)
		}
		return line
	}
	if p.Files == 0 {
		return ""
	}
	line := fmt.Sprintf("%d of %s", p.FilesDone, fileWord(p.Files))
	if p.Bytes > 0 {
		line += ", " + size(p.BytesDone) + " of " + size(p.Bytes)
	}
	return line
}

// fileWord writes a count of files with the right word after it.
func fileWord(n int) string {
	if n == 1 {
		return "1 file"
	}
	return fmt.Sprintf("%d files", n)
}

// outcomeOf says how a job ended.
func outcomeOf(p jobs.Progress) string {
	switch {
	case p.Err == nil:
		return "It finished."
	case errors.Is(p.Err, context.Canceled):
		return "It was cancelled."
	case errors.Is(p.Err, jobs.ErrStopped):
		return "It was stopped."
	}
	return "It failed: " + p.Err.Error()
}

// lasted says how long something took, in whole seconds so a dialog
// refreshed every frame says the same thing until there is something new
// to say.
func lasted(d time.Duration) string {
	d = d.Round(time.Second)
	switch {
	case d < 0:
		return "0 s"
	case d < time.Minute:
		return fmt.Sprintf("%d s", int(d/time.Second))
	case d < time.Hour:
		return fmt.Sprintf("%d min %d s", int(d/time.Minute), int(d/time.Second)%60)
	}
	return fmt.Sprintf("%d h %d min", int(d/time.Hour), int(d/time.Minute)%60)
}

// repeatJob does a finished piece of work again, on filesystems found
// afresh.
//
// By machine name rather than the filesystems the job held: those belong
// to panes that may have been closed, and a machine that dropped and
// came back is a different connection under the same name.
func (a *app) repeatJob(op jobs.Op, from, to string) {
	title := "Could not " + strings.ToLower(op.Kind.String()) + " it again"
	source, err := a.filesystem(from)
	if err != nil {
		a.reportError(title, err)
		return
	}
	owned := []vfs.FS{source}
	op.From = source
	if op.To != nil {
		into, err := a.filesystem(to)
		if err != nil {
			// The source is let go of here: nothing else holds it, and
			// a session nobody closes is a session left open on the
			// machine.
			a.reportError(title, errors.Join(err, source.Close()))
			return
		}
		owned = append(owned, into)
		op.To = into
	}
	a.runJob(op, from, to, owned)
}
