package main

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/marrasen/gridterm/input"
)

// A chord is a key pressed with modifiers held, which is how a user
// copies, pastes and selects. The scripted typist needs them to test the
// clipboard at all: the far application only copies when something tells
// it to.

// parseChords reads a comma-separated list like "ctrl+a,ctrl+c".
func parseChords(spec string) ([]input.Event, error) {
	if spec == "" {
		return nil, nil
	}
	var out []input.Event
	for _, one := range strings.Split(spec, ",") {
		event, err := parseChord(strings.TrimSpace(one))
		if err != nil {
			return nil, err
		}
		out = append(out, event)
	}
	return out, nil
}

// parseChord reads one chord, as in "ctrl+shift+v".
func parseChord(spec string) (input.Event, error) {
	parts := strings.Split(strings.ToLower(spec), "+")
	if len(parts) == 0 {
		return input.Event{}, fmt.Errorf("%q names no key", spec)
	}

	var mods input.Mods
	for _, part := range parts[:len(parts)-1] {
		switch part {
		case "ctrl", "control":
			mods |= input.ModCtrl
		case "shift":
			mods |= input.ModShift
		case "alt":
			mods |= input.ModAlt
		case "super", "win", "cmd":
			mods |= input.ModSuper
		default:
			return input.Event{}, fmt.Errorf("%q: no modifier called %q", spec, part)
		}
	}

	name := parts[len(parts)-1]
	key, ok := keyNamed(name)
	if !ok {
		return input.Event{}, fmt.Errorf("%q: no key called %q", spec, name)
	}
	return input.Event{Key: key, Mods: mods}, nil
}

// keyNamed finds a gridterm key by the name it prints itself as, which
// is what Key.String gives.
func keyNamed(name string) (input.Key, bool) {
	if len(name) == 1 && name[0] >= 'a' && name[0] <= 'z' {
		return input.KeyA + input.Key(name[0]-'a'), true
	}
	for k := input.Key(1); k <= input.KeyF12; k++ {
		if strings.EqualFold(k.String(), name) {
			return k, true
		}
	}
	return 0, false
}

// sendChords presses and releases each chord in turn.
func (w *window) sendChords(chords []input.Event) {
	board := newKeyboard()
	for i, chord := range chords {
		source := input.Source(1000 + i)
		log.Printf("chord: %v+%v", chord.Mods, chord.Key)
		for _, kind := range []input.Kind{input.KeyPress, input.KeyRelease} {
			event := chord
			event.Kind = kind
			event.Source = source
			for _, key := range board.handle(event, w.id) {
				w.display.send(key)
			}
		}
		// Long enough for the application to act on it before the next.
		time.Sleep(250 * time.Millisecond)
	}
	// Let go of anything the last chord left held.
	for _, key := range board.syncModifiers(0, w.id) {
		w.display.send(key)
	}
}
