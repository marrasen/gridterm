package mcp

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/marrasen/gridterm/agent"
	"github.com/marrasen/gridterm/steps"
)

// longestList is how long a whole list of steps may take.
//
// One wait may be five minutes and a list may hold sixty-four of them,
// which is a window goroutine parked for an afternoon -- the thing the
// wait's own cap exists to prevent. The budget is the list's, spent by
// its waits and its sleeps alike.
const longestList = steps.LongestWait

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
func (s *server) runSteps(pane string, list []steps.Step, lines int, clamped bool,
	timeoutMS int) (result, *rpcError) {

	// Everything a step can be refused for is refused here, before the
	// pane has had a character: a list that stopped half way because
	// its last key was misspelled has already typed the rest.
	if at, why := checkList(list); why != "" {
		return wrong(fmt.Sprintf("step %d, %q: %s. Nothing was typed.",
			at+1, list[at].String(), why))
	}

	// Every wait, not only the last: a list that runs three checks is
	// asking three questions, and an answer carrying one of them sends
	// the next list back to chaining them with semicolons -- where the
	// outputs run together and one exit status covers them all.
	var (
		waits []waited
		last  Screen
		read  bool

		// typed says the list has put something into the pane since
		// the last wait, so what it is waiting for could be an answer
		// to it. A wait before any of that has nothing to be an answer
		// to -- "until the prompt is up, then type" -- and takes the
		// pane as it already is, or it waits out its whole timeout for
		// a prompt that was drawn before the list began.
		typed bool
	)
	ends := time.Now().Add(longestList)
	for i, step := range list {
		if left := time.Until(ends); left <= 0 {
			return stoppedAt(i, step, "the list had run for "+longestList.String()+
				", which is as long as one may take", last, read)
		}
		switch step.Kind {
		case steps.Type:
			if err := s.panes.Send(pane, step.Text, nil); err != nil {
				return stoppedAt(i, step, err.Error(), last, read)
			}
			typed = true
		case steps.Key:
			if err := s.panes.Send(pane, "", []string{step.Chord}); err != nil {
				return stoppedAt(i, step, err.Error(), last, read)
			}
			typed = true
		case steps.Wait:
			if !s.rest(min(step.Wait, time.Until(ends))) {
				return stoppedAt(i, step, "the window is closing", last, read)
			}
		case steps.Until:
			screen, ended, err := s.panes.Wait(pane, lines, Until{
				Contains: step.Text,
				// Only what arrived since this list last typed counts:
				// the text waited for is usually a word it just typed,
				// and a terminal echoes what is typed.
				SinceKeys: step.Text != "" && typed,
				TimeoutMS: waitFor(timeoutMS, time.Until(ends)),
			})
			if err != nil {
				return stoppedAt(i, step, err.Error(), last, read)
			}
			// Taken now rather than at the end of the list: what a
			// command printed is what it printed at the time, and a
			// second wait later in the same list has moved the pane on.
			shown := s.printedOrScreen(pane, screen, ended, lines, false)
			waits = append(waits, waited{at: i, step: step, screen: shown, ended: ended})
			last, read, typed = shown, true, false
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
			if step.Text != "" && !sawIt(step.Text, shown, ended) {
				why := "it ended because " + ended.Because
				if ended.Because == "" {
					why = "it ended"
				}
				return stoppedAt(i, step, why+", and "+
					strconv.Quote(step.Text)+" is not in what it printed", last, read)
			}
		case steps.Require, steps.Fail:
			// What the pane has said since it was last typed at, which
			// is what a guard asks about. Not the whole screen: a
			// "fail:No such file" after a command that worked must not
			// stop the list because the same error from ten minutes ago
			// is still above it.
			screen, whole, err := s.printedNow(pane, lines)
			if err != nil {
				return stoppedAt(i, step, err.Error(), last, read)
			}
			last, read = screen, true
			looked := " in what the pane has said since you typed"
			if whole {
				// Nothing could say where the last command's output
				// began, so the guard is judging the whole screen --
				// where an error from ten minutes ago still counts.
				// Said, because it changes what the answer means.
				looked = " on the screen, which still carries whatever was" +
					" above it: nothing here could say where the last command's" +
					" output began"
			}
			holds := strings.Contains(screen.Screen, step.Text)
			if step.Kind == steps.Require && !holds {
				return stoppedAt(i, step,
					strconv.Quote(step.Text)+" is not"+looked, last, read)
			}
			if step.Kind == steps.Fail && holds {
				return stoppedAt(i, step,
					strconv.Quote(step.Text)+" is"+looked, last, read)
			}
		}
	}
	switch {
	case len(waits) > 0:
		// Whatever each wait saw, so a list that ends in one needs
		// nothing called after it.
		return say(s.sayWaits(pane, list, waits, clamped))
	case read:
		// A list of guards alone: no wait, but the pane was read to
		// answer them, and that reading is the answer.
		return say(allStepsSent(list) + "\n\n" + showScreen(last, Ending{}, clamped))
	}
	return say(allStepsSent(list) + " Nothing was waited for, so the screen" +
		" has not caught up: end a list with until, or call wait_for.")
}

// checkList is why a list cannot be run at all, and which step it is.
//
// Everything that can be known without touching the pane: a key nobody
// has a name for, text that arrived escaped twice, and a screenshot,
// which is a step only the window's own screenshot script can take.
func checkList(list []steps.Step) (at int, why string) {
	for i, step := range list {
		switch step.Kind {
		case steps.Key:
			if err := agent.CheckKeys([]string{step.Chord}); err != nil {
				return i, err.Error()
			}
		case steps.Type:
			if said := escapedEnding(step.Text); said != "" {
				return i, said
			}
		case steps.Shot:
			return i, "a screenshot is not something this can take"
		}
	}
	return 0, ""
}

// rest sleeps, and reports whether it slept the whole way rather than
// the window going while it did.
func (s *server) rest(d time.Duration) bool {
	if d <= 0 {
		return true
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-s.stop:
		return false
	case <-s.broke:
		return false
	}
}

// waitFor is how long one wait in a list may take: what was asked for,
// held to what the list has left.
func waitFor(askedMS int, left time.Duration) int {
	most := int(left.Milliseconds())
	if most < 1 {
		most = 1
	}
	if askedMS <= 0 || askedMS > most {
		return most
	}
	return askedMS
}

// sawIt reports whether a wait for text saw that text.
//
// The wait itself is the answer where it ended on the text: it watches
// for the text arriving, which is what the step asked for. A shell that
// marks its commands ends a wait the moment the command finishes,
// before that check is reached, so there the question is whether the
// text is in what the command printed -- not whether it is somewhere on
// a screen that may still be carrying it from an earlier run.
func sawIt(want string, shown Screen, ended Ending) bool {
	switch ended.Because {
	case agent.EndedOnText:
		return true
	case agent.EndedOnMarks:
		return strings.Contains(shown.Screen, want)
	}
	return false
}

// printedNow is what the pane has said since it was last typed at, for
// a guard, and the whole screen when that cannot be picked out.
//
// whole says which it is. A guard judging the whole screen can be
// stopped by an error from an hour ago, so the answer has to say that
// is what happened rather than letting it read as this command's.
func (s *server) printedNow(pane string, lines int) (screen Screen, whole bool, err error) {
	most := lines
	if most == 0 {
		most = mostLines
	}
	if out, err := s.panes.Output(pane, most); err == nil {
		return out, false, nil
	}
	screen, err = s.panes.Read(pane, lines)
	return screen, true, err
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
func (s *server) sayWaits(pane string, list []steps.Step, waits []waited, clamped bool) string {
	var b strings.Builder
	b.WriteString(allStepsSent(list))
	for i, w := range waits {
		b.WriteString("\n\n" + waitHead(w) + "\n")
		if i == len(waits)-1 {
			// The last one whole, with what gridterm has to say about
			// the pane as it now stands.
			b.WriteString(showScreen(w.screen, w.ended, clamped))
			continue
		}
		b.WriteString(w.screen.Screen)
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
