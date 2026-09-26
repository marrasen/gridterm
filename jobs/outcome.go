package jobs

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Outcome says how a job ended.
func Outcome(p Progress) string {
	how := ""
	switch {
	case p.Err == nil:
		return "It finished."
	case errors.Is(p.Err, context.Canceled):
		how = "It was cancelled"
	case errors.Is(p.Err, ErrStopped):
		how = "It was stopped"
	default:
		return "It failed: " + p.Err.Error()
	}
	// They asked for it to stop. They did not ask for half a file to be
	// left behind, so that is said as well.
	if why := Trouble(p.Err); why != nil {
		return how + ", but what was half written could not be taken away: " + why.Error()
	}
	return how + "."
}

// Trouble is what a job's failure says beyond the user having stopped
// it, and nil when stopping it is the whole story.
//
// A cancel that could not take away the part it had written reports
// both, and the disk failure is the half nobody asked for.
func Trouble(err error) error {
	for err != nil {
		if !errors.Is(err, context.Canceled) && !errors.Is(err, ErrStopped) {
			return err
		}
		if joined, ok := err.(interface{ Unwrap() []error }); ok {
			var rest []error
			for _, e := range joined.Unwrap() {
				rest = append(rest, Trouble(e))
			}
			return errors.Join(rest...)
		}
		next, ok := err.(interface{ Unwrap() error })
		if !ok {
			return nil
		}
		err = next.Unwrap()
	}
	return nil
}

// Going says how long a job has been going, in whole seconds so a line
// refreshed every frame says the same thing until there is something new
// to say.
func Going(d time.Duration) string {
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
