package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/marrasen/gridterm/jobs"
	"github.com/marrasen/gridterm/vfs"
)

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
	how := ""
	switch {
	case p.Err == nil:
		return "It finished."
	case errors.Is(p.Err, context.Canceled):
		how = "It was cancelled"
	case errors.Is(p.Err, jobs.ErrStopped):
		how = "It was stopped"
	default:
		return "It failed: " + p.Err.Error()
	}
	// They asked for it to stop. They did not ask for half a file to be
	// left behind, so that is said as well.
	if why := trouble(p.Err); why != nil {
		return how + ", but what was half written could not be taken away: " + why.Error()
	}
	return how + "."
}

// soFar says how long a job has been going, in whole seconds so a dialog
// refreshed every frame says the same thing until there is something new
// to say.
//
// The finished line uses howLong instead. That one counts tenths, which
// on a line redrawn every frame would dirty a row ten times a second.
func soFar(d time.Duration) string {
	d = d.Truncate(time.Second)
	switch {
	case d < time.Second:
		return "going under a second"
	case d < time.Minute:
		return fmt.Sprintf("going %d s", int(d/time.Second))
	case d < time.Hour:
		return fmt.Sprintf("going %d min %d s", int(d/time.Minute), int(d/time.Second)%60)
	}
	return fmt.Sprintf("going %d h %d min", int(d/time.Hour), int(d/time.Minute)%60)
}

// repeatJob does a finished piece of work again, on filesystems found
// afresh.
//
// Opened from the ends rather than from the filesystems the job held:
// those belong to panes that may have been closed, and a machine that
// dropped and came back is a different connection under the same name.
// A machine that has not come back is connected to here, so the work
// can be done again without the user reconnecting first.
func (a *app) repeatJob(op jobs.Op, from, to jobEnd) {
	title := "Could not " + strings.ToLower(op.Kind.String()) + " it again"
	a.openEndAgain(from, func(source vfs.FS, err error) {
		if err != nil {
			a.reportError(title, err)
			return
		}
		owned := []vfs.FS{source}
		op.From = source
		// The name the panel files the row under, worked out from the
		// filesystem that was just opened: a machine or a window renamed
		// since the job ran would leave the row under a name no heading
		// has.
		from.host = a.hostOf(source)
		if op.To == nil {
			a.runJob(op, from, to, owned)
			return
		}
		a.openEndAgain(to, func(into vfs.FS, err error) {
			if err != nil {
				// The source is let go of here: nothing else holds it,
				// and a session nobody closes is a session left open on
				// the machine.
				a.reportError(title, errors.Join(err, source.Close()))
				return
			}
			op.To = into
			to.host = a.hostOf(into)
			a.runJob(op, from, to, append(owned, into))
		})
	})
}
