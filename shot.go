package main

import (
	"fmt"
	"image"
	"image/png"
	"os"
	"strings"
	"time"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/marrasen/gridterm/agent"
	"github.com/marrasen/gridterm/input"
	"github.com/marrasen/gridterm/steps"
	"github.com/marrasen/gridterm/ui"
)

// shooter drives the window through a short script and writes what is on
// screen to PNG files.
//
// It exists because the thing being built is pixels, and a test can only
// check the numbers that went into them. A blurred panel, a rounded
// corner, a dialog drawn over the wrong thing: none of that is something
// an assertion catches. This renders real frames through the real
// pipeline so they can be looked at.
//
// The window is real and opens for as long as the script runs. Ebiten
// executes queued draw commands only inside its own game loop, so there
// is no way to read a frame back without one.
//
// The script is the steps package's, written on one line:
//
//	wait:250         wait a quarter of a second, for a shell to draw its prompt
//	until:$          wait until that text is on the focused pane
//	key:ctrl+k       press a chord, spelled the way a keymap spells it
//	type:hello       type text, a character at a time
//	shot:out.png     write the frame to a file
//
// The same steps an agent sends to work in a pane over MCP, so a step
// learned in one is a step learned in both. A bare "until" is the one
// an agent has and this does not: waiting for a command to finish takes
// the shell's own marks, which a screenshot script has no way to ask
// about.
//
// Each step takes at least a frame, so what a step did has been drawn by
// the time the next one runs. The window closes when the script ends.
type shooter struct {
	steps []steps.Step
	at    int

	// wait counts down the frames a wait step asked for.
	wait int

	// want is the text an until step is watching for, empty when none
	// is. was is what the pane held when that step began, so the step
	// waits for the text to arrive rather than matching the echo of
	// what the script has just typed. left is how many frames it has
	// before it gives up.
	want string
	was  string
	left int

	// typed says the script has put something into the window, so what
	// an until step is watching for could be an answer to it. Only an
	// until before any of that has nothing to be an answer to.
	typed bool

	// pending is the file this frame's Draw should write, set by a shot
	// step and cleared once written.
	pending string

	// failed is why the script gave up, and nil when it ran the whole
	// way. It is what the process exits with: a script whose wait ran
	// out has not taken the pictures it was asked for, and one that
	// exited 0 leaves whatever is looking at the files reading the ones
	// from last time as if they were new.
	failed error

	done bool
}

// Ticks is the frames a second a script counts its waits in.
//
// Counted in frames rather than against the wall, so a script behaves
// the same on a machine drawing slowly -- a software renderer under
// Xvfb, which is where most of these run -- as on one that is not.
const shotTicks = 60

// longestUntil is how long an until step waits before the script gives
// up, so a script that is watching for something that is never coming
// says so rather than holding the window open for ever.
const longestUntil = 30 * time.Second

// framesFor is how many frames a length of time is, rounded up: a wait
// of one millisecond is a frame, not none.
func framesFor(d time.Duration) int {
	if d <= 0 {
		return 0
	}
	return int((d*shotTicks + time.Second - 1) / time.Second)
}

// parseShotScript reads a screenshot script. It returns nil when the
// script is empty, which is what an ordinary run passes.
func parseShotScript(script string) (*shooter, error) {
	if strings.TrimSpace(script) == "" {
		return nil, nil
	}
	list, err := steps.ParseLine(script)
	if err != nil {
		return nil, err
	}
	// Refused here rather than half way through a script that has
	// already opened a window and taken pictures.
	for i, step := range list {
		switch step.Kind {
		case steps.Key:
			if _, err := ui.ParseChord(step.Chord); err != nil {
				return nil, fmt.Errorf("step %d, %q: %w", i+1, step, err)
			}
		case steps.Until:
			if step.Text == "" {
				return nil, fmt.Errorf("step %d, %q: a bare until waits for the"+
					" shell to say a command has finished, which a screenshot"+
					" script has no way to ask about. Give it the text to wait for",
					i+1, step)
			}
		case steps.Require, steps.Fail:
			return nil, fmt.Errorf("step %d, %q: the guards are for a list of steps"+
				" sent to a pane, where there is somebody to tell that it stopped",
				i+1, step)
		}
	}
	return &shooter{steps: list}, nil
}

// update runs one step of the script, at most one per frame, so that
// what a step did is on screen before the next one looks at it.
func (s *shooter) update(a *app) {
	switch {
	case s.done || s.pending != "":
		// Nothing until Draw has taken the picture it was asked for.
		return
	case s.wait > 0:
		s.wait--
		return
	case s.watching(a):
		return
	case s.at >= len(s.steps):
		s.done = true
		a.quit.Store(true)
		return
	}

	step := s.steps[s.at]
	s.at++
	switch step.Kind {
	case steps.Wait:
		s.wait = framesFor(step.Wait)
	case steps.Until:
		// What the pane held as the step began, so the step waits for
		// the text to arrive rather than matching the echo of what the
		// script has just typed.
		//
		// Unless nothing has been typed at all yet: then there is
		// nothing for the text to be an answer to -- "until the prompt
		// is up, then type" -- and the pane is taken as it already is,
		// or the step waits out its whole patience for a prompt that
		// was drawn before the script began.
		//
		// After that every until anchors here, including one following
		// another: "until:Building until:Deployed" asks for Deployed
		// after Building, not for a Deployed left over from last time.
		s.was = ""
		if s.typed {
			s.was = paneNow(a)
		}
		s.want, s.left = step.Text, framesFor(longestUntil)
	case steps.Key:
		chord, err := ui.ParseChord(step.Chord)
		if err != nil {
			a.logError(fmt.Errorf("screenshot key %s: %w", step.Chord, err))
			return
		}
		ev := input.Event{Kind: input.KeyPress, Key: chord.Key, Mods: chord.Mods}
		if _, err := a.root.HandleKey(ev); err != nil {
			a.logError(fmt.Errorf("screenshot key %s: %w", chord, err))
		}
		s.typed = true
	case steps.Type:
		for _, r := range step.Text {
			ev := input.Event{Kind: input.Text, Rune: r, NormalText: true}
			if _, err := a.root.HandleKey(ev); err != nil {
				a.logError(fmt.Errorf("screenshot text %q: %w", r, err))
			}
		}
		s.typed = true
	case steps.Shot:
		s.pending = step.Text
	}
}

// watching works an until step, and reports whether the script is still
// waiting on it.
//
// The text has to arrive: what was on the pane when the step began does
// not count, so "until:done" after typing "echo done" waits for the
// command to say it rather than for the echo of the typing.
func (s *shooter) watching(a *app) bool {
	if s.want == "" {
		return false
	}
	now := paneNow(a)
	if strings.Contains(agent.AddedSince(s.was, now), s.want) {
		s.want = ""
		return false
	}
	if s.left--; s.left <= 0 {
		// Kept rather than logged: the window is going, and shutDown
		// reports this as what the run exited with.
		s.failed = fmt.Errorf("until:%s: nothing said it in %s, and the steps"+
			" after it were not run", s.want, longestUntil)
		s.want = ""
		s.done = true
		a.quit.Store(true)
	}
	return true
}

// paneNow is what the focused pane is showing, for an until step to
// watch. Empty when the focus is not on a terminal.
func paneNow(a *app) string {
	t := a.focusedTerminal()
	if t == nil {
		return ""
	}
	return t.ReadLines(t.Size().Rows).Text
}

// captured writes the frame if one was asked for, and reports whether it
// wrote anything.
//
// ReadPixels gives premultiplied RGBA, which is what image.RGBA holds,
// so the bytes go straight in.
func (s *shooter) captured(screen *ebiten.Image) error {
	if s.pending == "" {
		return nil
	}
	path := s.pending
	s.pending = ""

	b := screen.Bounds()
	if b.Empty() {
		return fmt.Errorf("screenshot %s: the screen has no pixels", path)
	}
	pix := make([]byte, 4*b.Dx()*b.Dy())
	screen.ReadPixels(pix)

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	img := &image.RGBA{Pix: pix, Stride: 4 * b.Dx(), Rect: image.Rect(0, 0, b.Dx(), b.Dy())}
	if err := png.Encode(f, img); err != nil {
		// The encode failure is what went wrong. Closing after it is
		// housekeeping, and its own failure would say less.
		_ = f.Close()
		return fmt.Errorf("screenshot %s: %w", path, err)
	}
	return f.Close()
}
