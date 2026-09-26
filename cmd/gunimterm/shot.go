package main

import (
	"context"
	"errors"
	"fmt"
	"image/png"
	"os"
	"strings"
	"time"

	gi "github.com/marrasen/gunim/input"

	"github.com/marrasen/gridterm/agent"
	"github.com/marrasen/gridterm/input"
	"github.com/marrasen/gridterm/steps"
	"github.com/marrasen/gridterm/ui"
)

// -shot drives the window through a short script and writes what is on
// screen to PNG files, as gridterm's does: for looking at the pixels,
// which a test cannot check. The script is the steps package's, on one
// line:
//
//	wait:250         wait a quarter of a second
//	until:$          wait until that text arrives on the focused pane
//	key:ctrl+k       press a chord, spelled the way a keymap spells it
//	type:hello       type text, a character at a time
//	shot:out.png     write the window to a file
//
// The window closes when the script ends. A script whose until ran out
// fails the run, so nothing reads last time's pictures as new ones.

// longestUntil is how long an until step waits before the script gives
// up.
const longestUntil = 30 * time.Second

// stepGap is the time left after each step, so what it did is drawn
// before the next one looks.
const stepGap = 50 * time.Millisecond

// parseShot reads a screenshot script, refusing what it cannot do
// before a window opens.
func parseShot(script string) ([]steps.Step, error) {
	list, err := steps.ParseLine(script)
	if err != nil {
		return nil, err
	}
	for i, step := range list {
		switch step.Kind {
		case steps.Key:
			if _, err := chordPress(step.Chord); err != nil {
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
	return list, nil
}

// chordPress is the key press a chord is, as the window hears it.
func chordPress(written string) (gi.KeyPress, error) {
	chord, err := ui.ParseChord(written)
	if err != nil {
		return gi.KeyPress{}, err
	}
	press, ok := pressOf(chord)
	if !ok {
		return gi.KeyPress{}, fmt.Errorf("%s is no key this window takes", written)
	}
	return press, nil
}

// pressOf is the key press a chord is, as the window hears it, and
// whether the window has the key at all.
func pressOf(chord ui.Chord) (gi.KeyPress, bool) {
	for gk, k := range keyMap {
		if k != chord.Key {
			continue
		}
		press := gi.KeyPress{Key: gk}
		for _, m := range [...]struct {
			from input.Mods
			to   gi.Mods
		}{{input.ModShift, gi.ModShift}, {input.ModCtrl, gi.ModControl}, {input.ModAlt, gi.ModAlt}, {input.ModSuper, gi.ModSuper}} {
			if chord.Mods.Has(m.from) {
				press.Mods |= m.to
			}
		}
		return press, true
	}
	return gi.KeyPress{}, false
}

// runShot drives the window through the script, on a goroutine of its
// own, and closes the window at the end. Why it gave up, if it did, is
// kept in shotErr for the run to end with.
func (a *app) runShot(list []steps.Step) {
	err := a.shoot(a.ctx, list)
	a.events <- func() {
		a.shotErr = err
		a.exitNow()
	}
}

// shoot runs the steps.
func (a *app) shoot(ctx context.Context, list []steps.Step) error {
	typed := false
	// before is the pane as it was just before the last key or text
	// went in, and fresh says the step before this one put it there:
	// an answer can arrive before the next step looks, and what was
	// typed before that key is on the pane already.
	before, fresh := "", false
	now := func() string {
		s, _ := onApp(a, func() (string, error) { return a.paneNow(), nil })
		return s
	}
	pause := func(d time.Duration) error {
		select {
		case <-time.After(d):
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	for _, step := range list {
		switch step.Kind {
		case steps.Wait:
			if err := pause(step.Wait); err != nil {
				return err
			}
		case steps.Until:
			// What the pane held as the step began does not count, so
			// the step waits for the text to arrive. Before anything is
			// typed there is nothing it could be an answer to, and what
			// is there already counts.
			was := ""
			switch {
			case fresh:
				was = before
			case typed:
				was = now()
			}
			for deadline := time.Now().Add(longestUntil); ; {
				if strings.Contains(agent.AddedSince(was, now()), step.Text) {
					break
				}
				if time.Now().After(deadline) {
					return fmt.Errorf("until:%s: nothing said it in %s, and the steps after it were not run", step.Text, longestUntil)
				}
				if err := pause(stepGap); err != nil {
					return err
				}
			}
		case steps.Key:
			press, err := chordPress(step.Chord)
			if err != nil {
				return err
			}
			before = now()
			if press.Key == gi.KeySpace && press.Mods&^gi.ModShift == 0 {
				// A space comes from the keyboard as text, as the one
				// thing type: cannot hold.
				if err := a.c.Input(ctx, gi.TextInput{Text: " "}); err != nil {
					return err
				}
				typed = true
				break
			}
			if err := a.c.Input(ctx, press); err != nil {
				return err
			}
			if err := a.c.Input(ctx, gi.KeyRelease{Key: press.Key, Mods: press.Mods}); err != nil {
				return err
			}
			typed = true
		case steps.Type:
			before = now()
			for _, r := range step.Text {
				if err := a.c.Input(ctx, gi.TextInput{Text: string(r)}); err != nil {
					return err
				}
			}
			typed = true
		case steps.Shot:
			img, err := a.c.Shot(ctx)
			if err != nil {
				return fmt.Errorf("shot:%s: %w", step.Text, err)
			}
			f, err := os.Create(step.Text)
			if err != nil {
				return err
			}
			if err := errors.Join(png.Encode(f, img), f.Close()); err != nil {
				return fmt.Errorf("shot:%s: %w", step.Text, err)
			}
		}
		fresh = step.Kind == steps.Key || step.Kind == steps.Type
		if err := pause(stepGap); err != nil {
			return err
		}
	}
	return nil
}

// paneNow is what the focused pane shows, for an until step to watch:
// empty when the focus is on no terminal.
func (a *app) paneNow() string {
	t := a.terminal(a.st.Focus)
	if t == nil {
		return ""
	}
	return t.ReadLines(t.Size().Rows).Text
}
