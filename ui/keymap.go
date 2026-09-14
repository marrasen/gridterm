package ui

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/marrasen/gridterm/input"
)

// Chord is a key with the modifiers held down with it.
type Chord struct {
	Key  input.Key
	Mods input.Mods
}

// ChordOf returns the chord a key event presses. Text events have no
// chord, so they report the zero Chord, which nothing can be bound to.
func ChordOf(ev input.Event) Chord {
	if ev.Kind != input.KeyPress && ev.Kind != input.KeyRepeat {
		return Chord{}
	}
	return Chord{Key: ev.Key, Mods: ev.Mods}
}

// String renders a chord the way a menu shows it, such as "ctrl+shift+K".
func (c Chord) String() string {
	if c.Key == input.KeyNone {
		return "-"
	}
	if c.Mods == 0 {
		return c.Key.String()
	}
	return c.Mods.String() + "+" + c.Key.String()
}

// Keymap binds chords to command ids.
//
// It holds ids rather than functions, so a binding can be read from a
// settings file and can name a command registered later.
type Keymap struct {
	bound map[Chord]string
}

// NewKeymap returns an empty keymap.
func NewKeymap() *Keymap {
	return &Keymap{bound: make(map[Chord]string)}
}

// Bind points a chord at a command id, replacing whatever it pointed at.
// Rebinding is ordinary, so unlike registering a command it is not an
// error.
func (k *Keymap) Bind(c Chord, id string) error {
	if c.Key == input.KeyNone {
		return fmt.Errorf("cannot bind the empty chord")
	}
	if id == "" {
		return fmt.Errorf("chord %s bound to no command", c)
	}
	k.bound[c] = id
	return nil
}

// MustBind binds chords and panics if any is rejected. It is for the
// defaults a program sets at startup.
func (k *Keymap) MustBind(pairs map[Chord]string) {
	// Sorted, so a bad entry reports the same chord every run.
	for _, c := range sortedChords(pairs) {
		if err := k.Bind(c, pairs[c]); err != nil {
			panic(err)
		}
	}
}

// Unbind removes a binding.
func (k *Keymap) Unbind(c Chord) { delete(k.bound, c) }

// Lookup returns the command id a chord runs.
func (k *Keymap) Lookup(c Chord) (string, bool) {
	if c.Key == input.KeyNone {
		return "", false
	}
	id, ok := k.bound[c]
	return id, ok
}

// ChordFor returns a chord that runs the given command, for showing
// beside it in a menu. When several are bound it returns the same one
// every time rather than whichever the map happened to yield.
func (k *Keymap) ChordFor(id string) (Chord, bool) {
	var best Chord
	found := false
	for c, have := range k.bound {
		if have != id {
			continue
		}
		if !found || compareChords(c, best) < 0 {
			best, found = c, true
		}
	}
	return best, found
}

// Binding is one chord and the command it runs.
type Binding struct {
	Chord Chord
	ID    string
}

// Bindings returns every binding, in a stable order, so writing them to
// a settings file gives the same lines every run.
func (k *Keymap) Bindings() []Binding {
	out := make([]Binding, 0, len(k.bound))
	for c, id := range k.bound {
		out = append(out, Binding{Chord: c, ID: id})
	}
	slices.SortFunc(out, func(a, b Binding) int {
		return compareChords(a.Chord, b.Chord)
	})
	return out
}

// Len returns how many chords are bound.
func (k *Keymap) Len() int { return len(k.bound) }

// ParseChord reads a chord written as "ctrl+shift+K", the spelling
// String produces and a settings file would hold.
func ParseChord(s string) (Chord, error) {
	parts := strings.Split(s, "+")
	name := strings.TrimSpace(parts[len(parts)-1])
	var c Chord
	for _, mod := range parts[:len(parts)-1] {
		switch strings.ToLower(strings.TrimSpace(mod)) {
		case "ctrl", "control":
			c.Mods |= input.ModCtrl
		case "alt", "option":
			c.Mods |= input.ModAlt
		case "shift":
			c.Mods |= input.ModShift
		case "super", "cmd", "win":
			c.Mods |= input.ModSuper
		default:
			return Chord{}, fmt.Errorf("unknown modifier %q in %q", mod, s)
		}
	}
	key, ok := keyByName(name)
	if !ok {
		return Chord{}, fmt.Errorf("unknown key %q in %q", name, s)
	}
	c.Key = key
	return c, nil
}

// keysByName matches key names the way Key.String writes them. The walk
// stops at the first key with no name, so a key with no entry in the
// input package's name table hides every key after it.
// TestEveryKeyHasAName there is the tripwire for that, and a key added
// to input has to be named for this to find it.
var (
	keysByName     map[string]input.Key
	keysByNameOnce sync.Once
)

// keyByName looks a key up by name, ignoring case so a settings file can
// say "enter".
func keyByName(name string) (input.Key, bool) {
	if name == "" {
		return input.KeyNone, false
	}
	// ParseChord reads settings files, which a program may well do off
	// the drawing goroutine, so building this must not race.
	keysByNameOnce.Do(func() {
		keysByName = make(map[string]input.Key)
		for k := input.Key(1); k.String() != "Key?"; k++ {
			keysByName[strings.ToLower(k.String())] = k
		}
	})
	k, ok := keysByName[strings.ToLower(name)]
	return k, ok
}

// compareChords orders chords so a reverse lookup is repeatable.
func compareChords(a, b Chord) int {
	if n := cmp.Compare(a.Key, b.Key); n != 0 {
		return n
	}
	return cmp.Compare(a.Mods, b.Mods)
}

func sortedChords(m map[Chord]string) []Chord {
	out := make([]Chord, 0, len(m))
	for c := range m {
		out = append(out, c)
	}
	slices.SortFunc(out, compareChords)
	return out
}
