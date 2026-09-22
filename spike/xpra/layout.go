package main

import (
	"sort"

	"github.com/Xpra-org/go-xpra/ui"
)

// This file is the keyboard layout the spike declares to the server.
//
// gridterm has no X keyboard underneath it, so it has no keycodes to
// borrow: it is handed finished characters and a modifier bitmask, and
// nothing else. Xpra's answer to that is for the client to define its
// own layout and send it, which is what ui.KeymapProvider is for.
//
// The layout here is deliberately unlike a real keyboard. Every keysym
// gets a keycode of its own, at level 0, and there are no shifted
// levels at all. A real keyboard puts "a" and "A" on one key and asks
// the server to pick the level; this asks for the finished keysym by
// name and lets the modifiers say only what the user was holding.
//
// That is simpler and it is also more honest about what gridterm knows.
// It never learns which physical key produced a character, so it cannot
// say that "A" is the shifted form of anything -- only that the user
// typed "A" with shift down. Both facts go out, and the far application
// sees exactly that.

// firstKeycode is where the layout starts. X11 keycodes run from 8 to
// 255 and 8 is conventionally left alone.
const firstKeycode = 9

// layout is every keysym this client can send, each on its own keycode,
// and keycodes is the same thing indexed for the lookup every key event
// does.
var layout, keycodes = buildLayout()

// keycodeFor is the keycode this client sends for a keysym name, and 0
// for one that is not in the layout.
func keycodeFor(name string) int { return keycodes[name] }

// modifierMeanings names the keys that act as modifiers. The server
// needs this to know which keycode to press when it is told to hold
// shift.
func modifierMeanings() map[string]string {
	meanings := map[string]string{}
	for _, m := range modifierKeys {
		meanings[m.sym.Name] = modifierNames(m.bit)[0]
	}
	return meanings
}

// buildLayout collects every keysym the translator can produce and
// hands each one a keycode.
//
// Sorted by name before the keycodes are handed out, so that two runs
// of the same build declare the same layout: the server hashes it to
// decide whether it has changed.
func buildLayout() ([]ui.KeyMapping, map[string]int) {
	syms := map[string]int{}

	// The named keys: arrows, function keys, Escape and the rest.
	for _, sym := range keysyms {
		syms[sym.Name] = sym.Value
	}
	// Everything printable, which is what arrives as text. Latin-1 is
	// included because X11 names all of it and because most of Europe
	// needs it: a Swedish keyboard is unusable without å, ä and ö.
	//
	// It stops there. Past Latin-1 the keysyms are X11's Unicode range,
	// which is far larger than the 247 keycodes a keyboard has, so a
	// client that wanted them would have to hand out keycodes as
	// characters turned up and tell the server the layout had changed.
	// That is not written; text.go drops what it cannot place.
	for r := rune(0x20); r <= 0xff; r++ {
		if name := ui.KeysymName(r); name != "" {
			syms[name] = ui.KeysymValue(r)
		}
	}
	// And the modifiers, which the server presses on our behalf.
	for _, m := range modifierKeys {
		syms[m.sym.Name] = m.sym.Value
	}

	names := make([]string, 0, len(syms))
	for name := range syms {
		names = append(names, name)
	}
	sort.Strings(names)

	mappings := make([]ui.KeyMapping, 0, len(names))
	index := make(map[string]int, len(names))
	for i, name := range names {
		code := firstKeycode + i
		mappings = append(mappings, ui.KeyMapping{
			Name:    name,
			Keyval:  syms[name],
			Keycode: code,
			Group:   0,
			Level:   0,
		})
		index[name] = code
	}
	return mappings, index
}

// Keymap, ModifierMeanings and Layout make the display a
// ui.KeymapProvider.
func (d *display) Keymap() []ui.KeyMapping { return layout }

func (d *display) ModifierMeanings() map[string]string { return modifierMeanings() }

// Layout names the XKB layout the server should load.
//
// This is not cosmetic. A client without a full native keymap has its
// keys translated onto the server's own, so a keysym that layout does
// not contain cannot be typed: ask for "us" and every accented
// character is silently lost, however carefully it was named.
//
// gridterm would take this from the platform. The spike takes it from a
// flag, defaulting to the layout the machine it was written on uses.
func (d *display) Layout() (string, string, string) { return d.layoutName, "", "" }
