// Package jobs is file work that runs in the background: copying,
// moving and deleting, on one machine or between two.
//
// A job runs on a goroutine of its own and reports what it has done
// through Progress, which the panel reads every frame. Nothing here
// pushes into the widget tree, and nothing here draws.
//
// Every failure stops the job. A copy that could not read one file does
// not carry on with the next: the user asked for a directory to be
// copied, and half a directory that says it succeeded is worse than one
// that stopped and said why. What is left half written is taken away.
package jobs

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/marrasen/gridterm/meter"
	"github.com/marrasen/gridterm/vfs"
)

// Kind is what a job does.
type Kind uint8

const (
	// Copy puts what was named on the other filesystem, leaving the
	// original where it is.
	Copy Kind = iota

	// Move puts it there and takes the original away.
	Move

	// Delete takes what was named away.
	Delete
)

// String names a kind the way the panel shows it.
func (k Kind) String() string {
	switch k {
	case Copy:
		return "Copy"
	case Move:
		return "Move"
	case Delete:
		return "Delete"
	}
	return "Unknown"
}

// Op is what to do.
type Op struct {
	Kind Kind

	// From is the filesystem the names are on, and At the directory they
	// are in.
	From vfs.FS
	At   string

	// Names are what to work on, each a name in At rather than a path.
	Names []string

	// To is the filesystem to put them on and Into the directory there.
	// Both are unused by Delete.
	To   vfs.FS
	Into string
}

// Progress is how far a job has got.
//
// It is a plain value read under a lock, so the panel can ask on every
// frame without waiting for anything.
type Progress struct {
	// Files and Bytes are how much there is altogether, and FilesDone
	// and BytesDone how much of it is finished. They are zero until the
	// job has worked out what it is dealing with.
	Files, FilesDone int
	Bytes, BytesDone int64

	// Current is the name being worked on.
	Current string

	// Skipped counts what the user chose not to overwrite.
	Skipped int

	// Started is when the job began, for a panel that wants to say how
	// long it has been going.
	Started time.Time

	// Err is why it stopped, and Done says it has stopped. A job that
	// finished everything is done with no error.
	Err  error
	Done bool
}

// Options are what a job needs beyond the work itself.
type Options struct {
	// Ask reaches the user about a name that is already there. A nil one
	// stops the job at the first conflict rather than deciding by
	// itself.
	Ask Ask

	// Count records the bytes that move, for the panel. It may be nil.
	Count *meter.Meter

	// Out says the bytes are leaving this machine rather than arriving.
	// Which it is depends on which end of the job is somewhere else,
	// which only the caller knows.
	Out bool
}

// Job is one piece of work, running or finished.
type Job struct {
	op   Op
	opts Options

	// ctx is this job's own, so cancelling one that is running does not
	// touch the one waiting behind it.
	ctx    context.Context
	cancel context.CancelFunc

	// done is closed when the job has stopped, so a caller can wait for
	// it without polling.
	done chan struct{}

	// finished makes sure a job ends once: one dropped while it was
	// waiting is finished by whoever took it off the queue, and one that
	// ran is finished by the goroutine that ran it.
	finished sync.Once

	mu sync.Mutex
	p  Progress

	// parts counts the files written beside their names, so two at once
	// cannot choose the same one.
	parts int

	// made are the directories this job created, to be given the mode
	// they were asked for once everything is inside them.
	made []item

	// choice is what the user said to do about every later conflict,
	// when they said it once for all of them.
	choice *Choice
}

// Kind is what the job does.
func (j *Job) Kind() Kind { return j.op.Kind }

// Op returns what the job was asked to do.
func (j *Job) Op() Op { return j.op }

// Name is what the panel calls the job.
//
// It is read from the drawing goroutine the moment the job is made, so
// it holds for a job that was never going to run: the panel asks before
// anything has had a chance to say why.
func (j *Job) Name() string {
	what := ""
	switch n := len(j.op.Names); {
	case n == 1:
		what = j.op.Names[0]
	default:
		what = fmt.Sprintf("%d items", n)
	}
	if j.op.Kind == Delete || j.op.To == nil {
		return what
	}
	return what + " → " + j.op.To.Name()
}

// Progress returns how far it has got.
func (j *Job) Progress() Progress {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.p
}

// Done reports a channel that is closed when the job has stopped.
func (j *Job) Done() <-chan struct{} { return j.done }

// Cancel gives up on the job. What was half written is taken away.
func (j *Job) Cancel() { j.cancel() }

// update changes the progress under the lock.
func (j *Job) update(fn func(*Progress)) {
	j.mu.Lock()
	defer j.mu.Unlock()
	fn(&j.p)
}

// finish records how the job ended. The first ending is the one that
// counts.
func (j *Job) finish(err error) {
	j.finished.Do(func() {
		j.update(func(p *Progress) {
			p.Err = err
			p.Done = true
			p.Current = ""
		})
		close(j.done)
	})
}

// Queue is the jobs the window has, running and finished.
//
// A Queue is safe to use from several goroutines. The jobs run on their
// own; this is what holds them and what limits how many run at once.
type Queue struct {
	// limit is how many run at once. More than a few at a time on one
	// connection makes every one of them slower rather than the set of
	// them faster.
	limit int

	mu      sync.Mutex
	jobs    []*Job
	running int
	waiting []*Job
}

// DefaultLimit is how many jobs run at once when nothing says otherwise.
const DefaultLimit = 2

// New returns an empty queue. A limit of zero means DefaultLimit.
func New(limit int) *Queue {
	if limit <= 0 {
		limit = DefaultLimit
	}
	return &Queue{limit: limit}
}

// Start puts a job on the queue and runs it when there is room.
//
// It returns the job straight away, before any work has been done, so
// the caller has something to show and something to cancel.
func (q *Queue) Start(ctx context.Context, op Op, opts Options) *Job {
	ctx, cancel := context.WithCancel(ctx)
	j := &Job{
		op:     op,
		opts:   opts,
		ctx:    ctx,
		cancel: cancel,
		done:   make(chan struct{}),
		p:      Progress{Started: time.Now()},
	}

	// Checked here rather than on the job's own goroutine, so a job that
	// was never going to run says so before the panel has drawn it once.
	if err := op.check(); err != nil {
		q.mu.Lock()
		q.jobs = append(q.jobs, j)
		q.mu.Unlock()
		j.finish(err)
		return j
	}

	q.mu.Lock()
	q.jobs = append(q.jobs, j)
	start := q.running < q.limit
	if start {
		q.running++
	} else {
		q.waiting = append(q.waiting, j)
	}
	q.mu.Unlock()

	if start {
		go q.run(j)
	}
	return j
}

// run does one job and then starts whatever was waiting for room.
func (q *Queue) run(j *Job) {
	for {
		// Each job runs under its own context, so cancelling one that is
		// running does not take the one behind it with it.
		j.finish(j.do(j.ctx))

		q.mu.Lock()
		if len(q.waiting) == 0 {
			q.running--
			q.mu.Unlock()
			return
		}
		j = q.waiting[0]
		q.waiting = q.waiting[1:]
		q.mu.Unlock()
	}
}

// Jobs returns what the queue holds, oldest first.
func (q *Queue) Jobs() []*Job {
	q.mu.Lock()
	defer q.mu.Unlock()
	return append([]*Job(nil), q.jobs...)
}

// Drop takes a job off the list, for one the user has dismissed. A job
// that is still running is cancelled first: dropping it would leave
// nothing able to stop it.
func (q *Queue) Drop(j *Job) {
	j.Cancel()

	q.mu.Lock()
	for i, have := range q.jobs {
		if have == j {
			q.jobs = append(q.jobs[:i], q.jobs[i+1:]...)
			break
		}
	}
	waiting := false
	for i, have := range q.waiting {
		if have == j {
			q.waiting = append(q.waiting[:i], q.waiting[i+1:]...)
			waiting = true
			break
		}
	}
	q.mu.Unlock()

	if waiting {
		// It never started and nothing will start it now, so nothing
		// else would ever end it: whoever is waiting on it would wait
		// for ever.
		j.finish(context.Canceled)
	}
}

// DropFinished takes every job that has ended off the list.
func (q *Queue) DropFinished() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	kept := q.jobs[:0]
	dropped := 0
	for _, j := range q.jobs {
		if j.Progress().Done {
			dropped++
			continue
		}
		kept = append(kept, j)
	}
	for i := len(kept); i < len(q.jobs); i++ {
		q.jobs[i] = nil
	}
	q.jobs = kept
	return dropped
}

// CancelAll gives up on every job, for a window that is closing.
func (q *Queue) CancelAll() {
	for _, j := range q.Jobs() {
		j.Cancel()
	}
}

// Wait blocks until every job has stopped. It is for a window closing
// and for tests; nothing on the drawing goroutine may call it.
func (q *Queue) Wait() {
	for _, j := range q.Jobs() {
		<-j.Done()
	}
}

// WaitFor is Wait with a limit on how long it will wait, and reports
// whether everything stopped.
//
// Cancelling a job cannot interrupt a read or a write that is already
// under way: a filesystem method takes no context, so a job on a machine
// that has stopped answering is stuck until whatever it is waiting on
// gives up. A window that is closing lets those go rather than waiting
// with them -- the connections are closed on the way out, which is what
// ends them.
func (q *Queue) WaitFor(d time.Duration) bool {
	deadline := time.After(d)
	for _, j := range q.Jobs() {
		select {
		case <-j.Done():
		case <-deadline:
			return false
		}
	}
	return true
}

// errStopped is what a job returns when the user answered a question
// with "stop".
var errStopped = errors.New("jobs: stopped")
