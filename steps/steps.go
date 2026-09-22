// Package steps is the one vocabulary for driving a pane: what to type,
// what to press, how long to wait, and what to wait for.
//
// Two things read it. An agent sends a list of steps to the MCP server
// to work in a pane; the -shot flag drives the window through a script
// to take screenshots. They were two grammars for the same idea until
// this, and a step learned in one is now a step learned in both.
//
// A step is a word, a colon, and the rest of the line:
//
//	type:hello       type text, letter for letter
//	key:ctrl+c       press a chord, spelled the way a keymap spells it
//	wait:250         wait that many milliseconds, whatever happens
//	until:$          wait until that text arrives
//	until            wait until whatever is running finishes
//	shot:out.png     write what is on screen to a file
//
// Everything after the first colon is the argument, whole: a colon in
// what is typed or waited for is part of it, which is what lets
// "type:echo a:b" and "until:error: 404" mean what they read as.
package steps

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Kind is what a step does.
type Kind uint8

const (
	// Type puts text into the pane, letter for letter.
	Type Kind = iota + 1

	// Key presses one chord.
	Key

	// Wait is a plain delay: nothing is watched and nothing can end it
	// early.
	Wait

	// Until watches the pane. With text it ends when that text arrives;
	// with none it ends when what is running finishes.
	Until

	// Shot writes what is on screen to a file. Only a screenshot script
	// has anywhere to put one.
	Shot
)

// Step is one thing to do.
type Step struct {
	Kind Kind

	// Text is what Type types, what Until waits for, and where Shot
	// writes. It is empty for a bare Until, which waits on the pane
	// rather than on any particular words.
	Text string

	// Chord is what Key presses, as it was written. It is not checked
	// here: a screenshot script spells chords the way a keymap does and
	// an agent is held to a shorter list, so whoever runs the step says
	// which names it knows.
	Chord string

	// Wait is how long a Wait step waits.
	Wait time.Duration
}

// What a list of steps may not go past.
//
// A cap on each, because they run out differently: a thousand short
// steps is a script nobody wrote by hand, and one step carrying a
// megabyte is a pane being filled rather than typed into.
const (
	// MostSteps is how many steps one list may hold.
	MostSteps = 64

	// MostText is how many bytes all the typing in one list may come to.
	MostText = 8192

	// LongestWait is the longest a wait step may ask for. The same
	// bound a wait for the pane has, so neither way of asking parks a
	// window for an afternoon.
	LongestWait = 5 * time.Minute
)

// Parse reads one step.
func Parse(step string) (Step, error) {
	word, arg, hasArg := strings.Cut(step, ":")
	switch word {
	case "type":
		if arg == "" {
			return Step{}, fmt.Errorf("%q types nothing: put what to type after the colon", step)
		}
		return Step{Kind: Type, Text: arg}, nil
	case "key":
		if arg == "" {
			return Step{}, fmt.Errorf("%q presses nothing: put the key after the colon", step)
		}
		return Step{Kind: Key, Chord: arg}, nil
	case "shot":
		if arg == "" {
			return Step{}, fmt.Errorf("%q writes nowhere: put the file after the colon", step)
		}
		return Step{Kind: Shot, Text: arg}, nil
	case "wait":
		ms, err := strconv.Atoi(arg)
		if err != nil || ms < 0 {
			return Step{}, fmt.Errorf("%q is not a number of milliseconds", step)
		}
		got := time.Duration(ms) * time.Millisecond
		if got > LongestWait {
			return Step{}, fmt.Errorf("%q waits longer than %s, which is the longest a step may wait",
				step, LongestWait)
		}
		return Step{Kind: Wait, Wait: got}, nil
	case "until":
		// A bare "until" waits on the pane rather than on words, so it
		// is the one step that needs no colon. "until:" with nothing
		// after it is the same thing said clumsily, and is taken as it
		// reads.
		_ = hasArg
		return Step{Kind: Until, Text: arg}, nil
	}
	return Step{}, fmt.Errorf("%q is not a step; they are type:, key:, wait:, until and shot:", step)
}

// ParseAll reads a list of steps, one per string, and refuses a list
// that is past what a list may hold.
func ParseAll(list []string) ([]Step, error) {
	if len(list) == 0 {
		return nil, fmt.Errorf("no steps")
	}
	if len(list) > MostSteps {
		return nil, fmt.Errorf("%d steps, and %d is the most a list may hold", len(list), MostSteps)
	}
	out := make([]Step, 0, len(list))
	typed := 0
	for i, one := range list {
		step, err := Parse(one)
		if err != nil {
			return nil, fmt.Errorf("step %d: %w", i+1, err)
		}
		if step.Kind == Type {
			typed += len(step.Text)
		}
		out = append(out, step)
	}
	if typed > MostText {
		return nil, fmt.Errorf("the steps type %d bytes, and %d is the most one list may type",
			typed, MostText)
	}
	return out, nil
}

// ParseLine reads a script written as one line, with the steps
// separated by spaces.
//
// It is how a command line hands in a script, where a list of strings
// has nowhere to live. Nothing typed or waited for can hold a space
// then, which is the price of writing it on one line: ParseAll takes
// the same steps with their spaces intact.
func ParseLine(script string) ([]Step, error) {
	return ParseAll(strings.Fields(script))
}

// String writes a step the way it was read, for an answer that has to
// name the step it stopped at.
func (s Step) String() string {
	switch s.Kind {
	case Type:
		return "type:" + s.Text
	case Key:
		return "key:" + s.Chord
	case Wait:
		return "wait:" + strconv.FormatInt(s.Wait.Milliseconds(), 10)
	case Until:
		if s.Text == "" {
			return "until"
		}
		return "until:" + s.Text
	case Shot:
		return "shot:" + s.Text
	}
	return "an unknown step"
}
