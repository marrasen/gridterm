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
	// Every wait, not only the last: a list that runs three checks is
	// asking three questions, and an answer carrying one of them sends
	// the next list back to chaining them with semicolons -- where the
	// outputs run together and one exit status covers them all.
	var (
		waits   []waited
		last    Screen
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
			last, read = screen, true
			waits = append(waits, waited{at: i, step: step, screen: screen, ended: ended})
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
		case steps.Require, steps.Fail:
			// The screen as it stands, which is what a guard asks
			// about. Not a wait: a guard says what must be true now,
			// and a list that wanted to wait for it has until.
			screen, err := s.panes.Read(pane, lines)
			if err != nil {
				return stoppedAt(i, step, err.Error(), last, read)
			}
			last, read = screen, true
			holds := strings.Contains(screen.Screen, step.Text)
			if step.Kind == steps.Require && !holds {
				return stoppedAt(i, step,
					strconv.Quote(step.Text)+" is not on the screen", last, read)
			}
			if step.Kind == steps.Fail && holds {
				return stoppedAt(i, step,
					strconv.Quote(step.Text)+" is on the screen", last, read)
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
	return say(s.sayWaits(pane, list, waits, lines, clamped))
}

// waited is what one until step saw, kept for the answer.
type waited struct {
	at     int
	step   steps.Step
	screen Screen
	ended  Ending
}

// sayWaits writes a list's answer: what each wait in it saw, in order,
// and the whole of the last one.
//
// Each wait is headed by the step it was and what the shell said that
// command exited with, which is the thing a semicolon-chained command
// line cannot give back: one status for three commands says nothing
// about which of them failed.
func (s *server) sayWaits(pane string, list []steps.Step, waits []waited,
	lines int, clamped bool) string {

	var b strings.Builder
	b.WriteString(allStepsSent(list))
	for i, w := range waits {
		b.WriteString("\n\n" + waitHead(w) + "\n")
		if i == len(waits)-1 {
			// The last one whole, with what gridterm has to say about
			// the pane as it now stands.
			b.WriteString(s.afterWaiting(pane, w.screen, w.ended, lines, clamped, false))
			continue
		}
		b.WriteString(s.printedOrScreen(pane, w.screen, w.ended, lines, false).Screen)
	}
	return b.String()
}

// waitHead names one wait in a list's answer: which step it was, and
// how that command ended.
func waitHead(w waited) string {
	said := "it ended"
	switch {
	case w.ended.GaveUp:
		said = "the time ran out"
	case w.screen.Marks && !w.screen.Running && w.screen.HasStatus:
		said = "exit status " + strconv.Itoa(w.screen.Status)
	case w.ended.Because != "":
		said = w.ended.Because
	}
	return fmt.Sprintf("step %d, %q -- %s:", w.at+1, w.step.String(), said)
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
