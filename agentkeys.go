package main

import (
	"fmt"

	"github.com/marrasen/gridterm/agent"
	"github.com/marrasen/gridterm/input"
	"github.com/marrasen/gridterm/ui"
	"github.com/marrasen/gridterm/ui/term"
)

// typeInto types text into a pane and then presses the keys named after
// it.
//
// Everything goes in as one write, because keys split across writes
// arrive apart: over a connection that is a round trip between Escape
// and what follows it, which is how vim's escape timeout is defeated.
//
// Every name is read before anything goes in, so a call naming one key
// this does not have types nothing at all. The pane is asked to encode
// each name and the bytes go down the same quiet path as text, which
// leaves the user's scroll position and their selection alone.
//
// The keys are encoded as the pane stands when the call is made: a
// program the text in the same call starts has not set its modes yet, so
// keys for it belong in a call after it has.
func typeInto(pane *term.Terminal, text string, keys []string) error {
	if err := agent.CheckKeys(keys); err != nil {
		return err
	}
	presses := make([]input.Event, 0, len(keys))
	for _, name := range keys {
		press, err := keyPress(name)
		if err != nil {
			return err
		}
		presses = append(presses, press)
	}
	going := []byte(text)
	for _, press := range presses {
		going = append(going, pane.EncodeKey(press)...)
	}
	pane.Send(going)
	return nil
}

// keyPress is what a key name presses, for the pane's own terminal to
// encode: Up in vim and Up in a shell are different bytes, and the pane
// is what knows which, as it stands at the moment of the call.
//
// Space is the character it types, which is how it reaches a program.
func keyPress(name string) (input.Event, error) {
	if err := agent.CheckKeys([]string{name}); err != nil {
		return input.Event{}, err
	}
	chord, err := ui.ParseChord(name)
	if err != nil {
		// A name agent.CheckKeys allows and this cannot read is these
		// two lists having drifted apart, which is a fault here rather
		// than anything the agent did.
		return input.Event{}, fmt.Errorf("gridterm cannot press %q: %w", name, err)
	}
	if chord.Key == input.KeySpace && chord.Mods == 0 {
		return input.Event{Kind: input.Text, Rune: ' ', NormalText: true}, nil
	}
	return input.Event{Kind: input.KeyPress, Key: chord.Key, Mods: chord.Mods}, nil
}
