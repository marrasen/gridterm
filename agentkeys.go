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
// Every name is read before anything goes in, so a call naming one key
// this does not have types nothing at all.
func typeInto(pane *term.Terminal, text string, keys []string) error {
	presses := make([][]input.Event, 0, len(keys))
	for _, name := range keys {
		press, err := keyPress(name)
		if err != nil {
			return err
		}
		presses = append(presses, press)
	}
	if text != "" {
		pane.Send([]byte(text))
	}
	for _, press := range presses {
		for _, ev := range press {
			if _, err := pane.HandleKey(ev); err != nil {
				return err
			}
		}
	}
	return nil
}

// keyPress is what a key name presses, for the pane's own terminal to
// encode: Up in vim and Up in a shell are different bytes, and the pane
// is what knows which.
//
// Most names are one press. Space is the character it types, which is
// how it reaches a program, and Alt+<letter> is Escape and then the
// letter, which is what a terminal sends for it.
func keyPress(name string) ([]input.Event, error) {
	if err := agent.CheckKeys([]string{name}); err != nil {
		return nil, err
	}
	chord, err := ui.ParseChord(name)
	if err != nil {
		// A name agent.CheckKeys allows and this cannot read is these
		// two lists having drifted apart, which is a fault here rather
		// than anything the agent did.
		return nil, fmt.Errorf("gridterm cannot press %q: %w", name, err)
	}
	switch {
	case chord.Key == input.KeySpace && chord.Mods == 0:
		return []input.Event{{Kind: input.Text, Rune: ' ', NormalText: true}}, nil
	case chord.Mods == input.ModAlt:
		return []input.Event{
			{Kind: input.KeyPress, Key: input.KeyEscape},
			{Kind: input.Text, Rune: letterOf(chord.Key), NormalText: true},
		}, nil
	}
	return []input.Event{{Kind: input.KeyPress, Key: chord.Key, Mods: chord.Mods}}, nil
}

// letterOf is the character a letter key types.
func letterOf(k input.Key) rune { return rune('a' + k - input.KeyA) }
