package jobs

import (
	"context"

	"github.com/marrasen/gridterm/vfs"
)

// What the user chose to do about a name that is already there.
type What uint8

const (
	// Replace writes over what is there.
	Replace What = iota

	// Skip leaves it alone and carries on with the rest.
	Skip

	// Rename puts the new one beside it under another name.
	Rename

	// Stop gives up on the whole job.
	Stop
)

// Conflict is a name the job cannot write without being told what to do.
type Conflict struct {
	// To is the filesystem it would be written on, and Path where.
	To   vfs.FS
	Path string

	// Have is what is there now, and Want what would replace it.
	Have, Want vfs.Entry
}

// Choice is the answer.
type Choice struct {
	// What to do about it.
	What What

	// Name is what to call it instead, for Rename. It is a name in the
	// same directory, not a path.
	Name string

	// All says to do the same with every later conflict in this job,
	// without asking again.
	All bool
}

// Ask reaches the user about a name that is already there.
//
// It is called from the job's own goroutine and blocks it until there is
// an answer. An implementation that draws must hand the question to
// whichever goroutine may draw and wait for the reply.
//
// Returning an error stops the job, which is what a window that is
// closing does: the question can no longer be answered, so there is no
// answer to act on.
type Ask interface {
	Overwrite(ctx context.Context, c Conflict) (Choice, error)
}

// Always answers every question the same way, for a caller that has
// already decided.
type Always Choice

// Overwrite gives the answer it was made with.
func (a Always) Overwrite(context.Context, Conflict) (Choice, error) {
	return Choice(a), nil
}

// decide works out what to do about a conflict: what the user said last
// time if they said it for all of them, and otherwise what they say now.
func (j *Job) decide(ctx context.Context, c Conflict) (Choice, error) {
	j.mu.Lock()
	remembered := j.choice
	j.mu.Unlock()
	if remembered != nil {
		return *remembered, nil
	}
	if j.opts.Ask == nil {
		// Nobody to ask. Deciding alone is the one thing this must not
		// do: the file that is there is the user's.
		return Choice{}, &ConflictError{Conflict: c}
	}

	choice, err := j.opts.Ask.Overwrite(ctx, c)
	if err != nil {
		return Choice{}, err
	}
	if choice.All {
		j.mu.Lock()
		kept := choice
		// A name to rename to means nothing for the next one, which has
		// a different name.
		if kept.What == Rename {
			kept.What = Skip
		}
		j.choice = &kept
		j.mu.Unlock()
	}
	return choice, nil
}

// ConflictError is returned when something is already there and there is
// nobody to ask what to do about it.
type ConflictError struct{ Conflict }

func (e *ConflictError) Error() string {
	return e.To.Name() + ": " + e.Path + " is already there"
}
