package mcp

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/marrasen/gridterm/agent"
	"github.com/marrasen/gridterm/steps"
)

// runSteps works a list of steps in a pane, in order, and says how far
// it got.
//
// The whole point of a list is what happens between the steps: an agent
// that types a command, waits for the program it started, and types
// into that program does it in one go, rather than in three calls with
// a gap in each where the pane can be something else by the time the
// next one lands.
//
// It stops at the first step that does not do what it says. Carrying on
// would type the rest into whatever is there instead, which is the one
// thing this is for avoiding.
func (s *server) runSteps(pane string, list []steps.Step, lines, timeoutMS int) (result, *rpcError) {
	var (
		last    Screen
		ending  Ending
		read    bool
		clamped bool
	)
	for i, step := range list {
		switch step.Kind {
		case steps.Type:
			if why := escapedEnding(step.Text); why != "" {
				return stoppedAt(i, step, why, last, read)
			}
			if err := s.panes.Send(pane, step.Text, nil); err != nil {
				return stoppedAt(i, step, err.Error(), last, read)
			}
		case steps.Key:
			// Refused before anything is typed, so an agent that spelled
			// a key wrong is told which names there are and the pane has
			// had nothing.
			if err := agent.CheckKeys([]string{step.Chord}); err != nil {
				return stoppedAt(i, step, err.Error(), last, read)
			}
			if err := s.panes.Send(pane, "", []string{step.Chord}); err != nil {
				return stoppedAt(i, step, err.Error(), last, read)
			}
		case steps.Wait:
			time.Sleep(step.Wait)
		case steps.Until:
			screen, ended, err := s.panes.Wait(pane, lines, Until{
				Contains: step.Text,
				// Only what arrived while this list was running counts.
				// The text waited for is usually a word the list just
				// typed, and a terminal echoes what is typed.
				SinceKeys: step.Text != "",
				TimeoutMS: timeoutMS,
			})
			if err != nil {
				return stoppedAt(i, step, err.Error(), last, read)
			}
			last, ending, read = screen, ended, true
			if ended.GaveUp {
				return stoppedAt(i, step, "the time ran out", last, read)
			}
			// A wait for text has to have seen that text. It can end
			// for another reason -- the command finished, the pane went
			// quiet -- and a step that says "until the editor is up"
			// has not done what it says because something else stopped
			// the waiting. Going on would type the rest into whatever
			// ended it: "cd somewhere && vim notes.md" with the cd
			// wrong is a shell prompt, and the lines meant for the
			// editor would be run as commands.
			if step.Text != "" && !strings.Contains(screen.Screen, step.Text) {
				why := "it ended because " + ended.Because
				if ended.Because == "" {
					why = "it ended"
				}
				return stoppedAt(i, step, why+", and "+
					strconv.Quote(step.Text)+" is not on the screen", last, read)
			}
		case steps.Shot:
			// A screenshot has nowhere to go from here: the file would
			// be written on the user's machine and the agent could not
			// read it.
			return stoppedAt(i, step, "a screenshot is not something this can take", last, read)
		}
	}
	// Whatever the last wait saw, so a list that ends in one needs
	// nothing called after it. A list that ends in a keystroke has
	// nothing to report but that it was sent.
	if !read {
		return say(allStepsSent(list) + " Nothing was waited for, so the screen" +
			" has not caught up: end a list with until, or call wait_for.")
	}
	return say(allStepsSent(list) + "\n\n" + showScreen(last, ending, clamped))
}

// allStepsSent is the line that says a whole list went in.
func allStepsSent(list []steps.Step) string {
	return "All " + strconv.Itoa(len(list)) + " steps ran."
}

// stoppedAt is the answer from a list that could not go on, saying
// which step stopped it and what the pane looked like there.
//
// The step is named as it was written and counted the way a person
// counts them, so an agent reading this can point at the element of the
// list it sent.
func stoppedAt(at int, step steps.Step, why string, last Screen, read bool) (result, *rpcError) {
	said := fmt.Sprintf("Stopped at step %d, %q: %s."+
		" The steps after it were not run, because they would have gone to"+
		" whatever is in the pane now rather than to what you were waiting for.",
		at+1, step.String(), strings.TrimRight(why, "."))
	if !read {
		return wrong(said)
	}
	return wrong(said + "\n\n" + showScreen(last, Ending{}, false))
}
