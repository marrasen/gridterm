package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/marrasen/gridterm/conns"
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

// repeatJob does a finished piece of work again, on filesystems found
// afresh.
//
// Opened from the ends rather than from the filesystems the job held:
// those belong to panes that may have been closed, and a machine that
// dropped and came back is a different connection under the same name.
// A machine that has not come back is connected to here, so the work
// can be done again without the user reconnecting first.
//
// done says the attempt is over, whether it started the work or failed,
// so what asked can let the user ask again. started is handed the new
// work once it is going, so the pane it was asked from can turn onto it.
func (a *app) repeatJob(op jobs.Op, from, to jobEnd, done func(),
	started func(*jobs.Job, *conns.Entry, jobEnd, jobEnd)) {
	title := "Could not " + strings.ToLower(op.Kind.String()) + " it again"
	if done == nil {
		done = func() {}
	}
	run := func(op jobs.Op, from, to jobEnd, owned []vfs.FS) {
		j, e := a.runJobRow(op, from, to, owned)
		if started != nil {
			started(j, e, from, to)
		}
	}
	a.openEndAgain(from, func(source vfs.FS, err error) {
		if err != nil {
			done()
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
			done()
			run(op, from, to, owned)
			return
		}
		a.openEndAgain(to, func(into vfs.FS, err error) {
			done()
			if err != nil {
				// The source is let go of here: nothing else holds it,
				// and a session nobody closes is a session left open on
				// the machine.
				a.reportError(title, errors.Join(err, source.Close()))
				return
			}
			op.To = into
			to.host = a.hostOf(into)
			run(op, from, to, append(owned, into))
		})
	})
}
