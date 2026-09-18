package main

import (
	"fmt"
	"image"
	"image/png"
	"os"
	"strconv"
	"strings"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/marrasen/gridterm/input"
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
// The script is a list of steps separated by spaces:
//
//	wait:30          let thirty frames pass, for a shell to draw its prompt
//	key:ctrl+k       press a chord, spelled the way a keymap spells it
//	type:hello       type text, a character at a time
//	shot:out.png     write the frame to a file
//
// Each step takes a frame, so what a step did has been drawn by the time
// the next one runs. The window closes when the script ends.
type shooter struct {
	steps []shotStep
	at    int

	// wait counts down the frames a wait step asked for.
	wait int

	// pending is the file this frame's Draw should write, set by a shot
	// step and cleared once written.
	pending string

	done bool
}

// shotStep is one instruction of a screenshot script.
type shotStep struct {
	kind  string
	n     int
	chord ui.Chord
	text  string
}

// parseShotScript reads a screenshot script. It returns nil when the
// script is empty, which is what an ordinary run passes.
func parseShotScript(script string) (*shooter, error) {
	var s shooter
	for word := range strings.FieldsSeq(script) {
		kind, arg, ok := strings.Cut(word, ":")
		if !ok {
			return nil, fmt.Errorf("step %q has no colon; want wait:, key:, type: or shot:", word)
		}
		switch kind {
		case "wait":
			n, err := strconv.Atoi(arg)
			if err != nil || n < 0 {
				return nil, fmt.Errorf("wait:%s is not a number of frames", arg)
			}
			s.steps = append(s.steps, shotStep{kind: kind, n: n})
		case "key":
			chord, err := ui.ParseChord(arg)
			if err != nil {
				return nil, err
			}
			s.steps = append(s.steps, shotStep{kind: kind, chord: chord})
		case "type", "shot":
			if arg == "" {
				return nil, fmt.Errorf("%s: needs something after the colon", kind)
			}
			s.steps = append(s.steps, shotStep{kind: kind, text: arg})
		default:
			return nil, fmt.Errorf("unknown step %q; want wait:, key:, type: or shot:", kind)
		}
	}
	if len(s.steps) == 0 {
		return nil, nil
	}
	return &s, nil
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
	case s.at >= len(s.steps):
		s.done = true
		a.quit.Store(true)
		return
	}

	step := s.steps[s.at]
	s.at++
	switch step.kind {
	case "wait":
		s.wait = step.n
	case "key":
		ev := input.Event{Kind: input.KeyPress, Key: step.chord.Key, Mods: step.chord.Mods}
		if _, err := a.root.HandleKey(ev); err != nil {
			a.logError(fmt.Errorf("screenshot key %s: %w", step.chord, err))
		}
	case "type":
		for _, r := range step.text {
			ev := input.Event{Kind: input.Text, Rune: r, NormalText: true}
			if _, err := a.root.HandleKey(ev); err != nil {
				a.logError(fmt.Errorf("screenshot text %q: %w", r, err))
			}
		}
	case "shot":
		s.pending = step.text
	}
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
		f.Close()
		return fmt.Errorf("screenshot %s: %w", path, err)
	}
	return f.Close()
}
