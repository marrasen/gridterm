package agent

import (
	"fmt"
	"slices"
	"strings"
)

// plainKeys are the keys a "send" may press by name.
//
// A window turns each into a key press of its own, so what goes to the
// program is what that key means in whatever it is running: Up in a
// shell and Up in vim are encoded differently, and the pane knows which.
var plainKeys = []string{
	"Enter", "Tab", "Escape", "Backspace", "Delete", "Insert", "Space",
	"Up", "Down", "Left", "Right", "Home", "End", "PageUp", "PageDown",
	"F1", "F2", "F3", "F4", "F5", "F6", "F7", "F8", "F9", "F10", "F11", "F12",
}

// chordKeys are the modified keys a "send" may press, beyond the plain
// ones. A letter is any of a to z.
const chordKeys = `Ctrl+<letter>, Alt+<letter> and Shift+Tab.` +
	` Ctrl+[ is Escape, so press Escape; Ctrl+Space is not a name, so send it as the text \u0000`

// MostKeys is how many names one "send" may press.
//
// A key press costs a few bytes and the pane encodes each one, so this
// is far more than anybody types in one call and short of a list that
// takes the window a moment to work through.
const MostKeys = 64

// Keys are the plain key names a "send" may press. The chords are not in
// here, because Ctrl+<letter> and Alt+<letter> stand for any letter.
func Keys() []string { return slices.Clone(plainKeys) }

// KeyNames is every key a "send" presses, for a tool description and for
// telling an agent which name it got wrong.
func KeyNames() string {
	return strings.Join(plainKeys, ", ") + ", " + chordKeys
}

// KnownKey reports whether name is a key this presses. Case does not
// matter: an agent writing "escape" means Escape.
func KnownKey(name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	if mod, rest, ok := strings.Cut(name, "+"); ok {
		switch mod {
		case "ctrl", "alt":
			return len(rest) == 1 && rest[0] >= 'a' && rest[0] <= 'z'
		case "shift":
			return rest == "tab"
		}
		return false
	}
	for _, have := range plainKeys {
		if strings.EqualFold(have, name) {
			return true
		}
	}
	return false
}

// CheckKeys returns an error naming the keys that exist when one of
// these is not a key this presses, or when there are more of them than
// one call presses.
func CheckKeys(keys []string) error {
	if len(keys) > MostKeys {
		return fmt.Errorf("that call presses %d keys, and the most one call presses is %d."+
			" Send them in several calls", len(keys), MostKeys)
	}
	for _, name := range keys {
		if !KnownKey(name) {
			return fmt.Errorf("%q is not a key this presses. The keys are: %s", name, KeyNames())
		}
	}
	return nil
}
