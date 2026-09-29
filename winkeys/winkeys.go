// Package winkeys turns keys as the window hears them into kakel's
// keys, and back: to encode a key pressed in a terminal, and to press a
// key a script or a shortcut names.
package winkeys

import (
	"cmp"
	"fmt"
	"slices"

	gi "github.com/marrasen/gunim/input"
	"github.com/marrasen/kakel/input"
	"github.com/marrasen/kakel/ui"
)

// Event turns a gunim key press into kakel's, for a key kakel
// encodes.
func Event(e gi.KeyPress) (input.Event, bool) {
	k, ok := keyMap[e.Key]
	// Punctuation is the key it types: a Swedish keyboard puts + where a
	// US one has -, and Ctrl and the key marked plus should make the
	// font bigger.
	if p, typed := punctuation[e.Char]; typed {
		k, ok = p, true
	}
	if !ok {
		return input.Event{}, false
	}
	ev := input.Event{Kind: input.KeyPress, Key: k}
	if e.Repeat {
		ev.Kind = input.KeyRepeat
	}
	for _, m := range [...]struct {
		from gi.Mods
		to   input.Mods
	}{{gi.ModShift, input.ModShift}, {gi.ModControl, input.ModCtrl}, {gi.ModAlt, input.ModAlt}, {gi.ModSuper, input.ModSuper}} {
		if e.Mods.Has(m.from) {
			ev.Mods |= m.to
		}
	}
	return ev, true
}

// punctuation is the punctuation kakel binds, by the character the
// key types rather than where it sits.
var punctuation = map[rune]input.Key{
	'=': input.KeyEquals, '+': input.KeyPlus, '-': input.KeyMinus,
	'[': input.KeyBracketLeft, ']': input.KeyBracketRight, '\\': input.KeyBackslash,
}

// keyMap holds the keys kakel encodes. The keypad's keys, pressed
// without Num Lock, are the keys printed on them.
var keyMap = func() map[gi.Key]input.Key {
	m := map[gi.Key]input.Key{
		gi.KeyEnter: input.KeyEnter, gi.KeyKPEnter: input.KeyEnter,
		gi.KeyTab: input.KeyTab, gi.KeyBackspace: input.KeyBackspace,
		gi.KeyEscape: input.KeyEscape, gi.KeySpace: input.KeySpace,
		gi.KeyLeftBracket: input.KeyBracketLeft, gi.KeyRightBracket: input.KeyBracketRight,
		gi.KeyBackslash: input.KeyBackslash, gi.KeyEqual: input.KeyEquals,
		gi.KeyMinus: input.KeyMinus, gi.Key0: input.Key0,
		gi.KeyUp: input.KeyUp, gi.KeyDown: input.KeyDown,
		gi.KeyLeft: input.KeyLeft, gi.KeyRight: input.KeyRight,
		gi.KeyHome: input.KeyHome, gi.KeyEnd: input.KeyEnd,
		gi.KeyInsert: input.KeyInsert, gi.KeyDelete: input.KeyDelete,
		gi.KeyPageUp: input.KeyPageUp, gi.KeyPageDown: input.KeyPageDown,
		gi.KeyKP8: input.KeyUp, gi.KeyKP2: input.KeyDown,
		gi.KeyKP4: input.KeyLeft, gi.KeyKP6: input.KeyRight,
		gi.KeyKP7: input.KeyHome, gi.KeyKP1: input.KeyEnd,
		gi.KeyKP0: input.KeyInsert, gi.KeyKPDecimal: input.KeyDelete,
		gi.KeyKP9: input.KeyPageUp, gi.KeyKP3: input.KeyPageDown,
	}
	for i := range 26 {
		m[gi.KeyA+gi.Key(i)] = input.KeyA + input.Key(i)
	}
	for i := range 12 {
		m[gi.KeyF1+gi.Key(i)] = input.KeyF1 + input.Key(i)
	}
	return m
}()

// Parse is the key press a chord is, as the window hears it.
func Parse(written string) (gi.KeyPress, error) {
	chord, err := ui.ParseChord(written)
	if err != nil {
		return gi.KeyPress{}, err
	}
	press, ok := Press(chord)
	if !ok {
		return gi.KeyPress{}, fmt.Errorf("%s is no key this window takes", written)
	}
	return press, nil
}

// Press is the key press a chord is, as the window hears it, and
// whether the window has the key at all.
func Press(chord ui.Chord) (gi.KeyPress, bool) {
	// The main keyboard's key before the keypad's that also means it,
	// and the same one each time: keyMap is a map, visited in no fixed
	// order.
	var keys []gi.Key
	for gk, k := range keyMap {
		if k == chord.Key {
			keys = append(keys, gk)
		}
	}
	if len(keys) == 0 {
		return gi.KeyPress{}, false
	}
	slices.SortFunc(keys, func(a, b gi.Key) int { return cmp.Compare(keypad(a), keypad(b)) })
	press := gi.KeyPress{Key: keys[0]}
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

// keypad is 1 for a key on the keypad and 0 for the rest.
func keypad(k gi.Key) int {
	switch k {
	case gi.KeyKPEnter, gi.KeyKP0, gi.KeyKP1, gi.KeyKP2, gi.KeyKP3, gi.KeyKP4,
		gi.KeyKP5, gi.KeyKP6, gi.KeyKP7, gi.KeyKP8, gi.KeyKP9, gi.KeyKPDecimal:
		return 1
	}
	return 0
}
