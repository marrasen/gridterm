package main

import (
	"github.com/Xpra-org/go-xpra/ui"

	"github.com/marrasen/gridterm/input"
)

// This file is step 6 of REMOTE-APPS.md: turning gridterm's keyboard
// into the one xpra wants.
//
// The load-bearing field is the name. This client uploads no keymap, so
// the server resolves every key through find_matching_keycode, which
// looks the name up in its own keymap -- it has to be a real X11 keysym
// name such as "braceleft", never the character the key produced. The
// value, the text and the keycode are hints it falls back on.
//
// `ui.KeysymName` covers printable ASCII and nothing else, which leaves
// the arrows, the function keys, Home and End and the rest to this
// table. Every name and value in it is checked against the X11 protocol
// headers by keys_test.go, so none of it is from memory.
//
// It takes three things together, and each was tried alone first and
// looked like a broken protocol:
//
//  1. The resolved keysym, not the base key. "A", never "a" with shift
//     held. gridterm reports each keystroke twice -- as a key and as the
//     text it produced -- and only the text knows what the layout made.
//     So a key that types waits for its text, and the table below is
//     only for keys that type nothing.
//  2. Real modifier key presses. gridterm names no modifier key; there
//     is no input.KeyShift, only a bitmask riding along with other
//     events. They have to be made up, and each one describes the state
//     *before* itself, the way X does.
//  3. A keycode, from a keymap this client declares itself. See
//     layout.go. Without one the server resolves the name to some
//     keycode of its own and types it at the wrong level.
//
// With all three, typing
//
//	The QUICK brown Fox; #1 @ 50% (x*y) [a] {b} "c" <d> ~e|f/g?
//
// into mousepad over Xpra 6.5.3 arrives character for character. With
// any one missing it comes out as "hello, world1 42" or worse.

// keysym is one X11 key as xpra names it.
type keysym struct {
	Name  string
	Value int
}

// keysyms is every key gridterm names, in X11's vocabulary.
//
// The letters are filled in below: they are their own names, and their
// values are their code points, so writing them out would be twenty-six
// lines saying the same thing.
var keysyms = map[input.Key]keysym{
	input.KeyEnter:     {"Return", 0xff0d},
	input.KeyTab:       {"Tab", 0xff09},
	input.KeyBackspace: {"BackSpace", 0xff08},
	input.KeyEscape:    {"Escape", 0xff1b},
	input.KeySpace:     {"space", 0x0020},

	input.KeyBracketLeft:  {"bracketleft", 0x005b},
	input.KeyBracketRight: {"bracketright", 0x005d},
	input.KeyBackslash:    {"backslash", 0x005c},

	input.KeyEquals: {"equal", 0x003d},
	input.KeyPlus:   {"plus", 0x002b},
	input.KeyMinus:  {"minus", 0x002d},
	input.Key0:      {"0", 0x0030},

	input.KeyUp:    {"Up", 0xff52},
	input.KeyDown:  {"Down", 0xff54},
	input.KeyRight: {"Right", 0xff53},
	input.KeyLeft:  {"Left", 0xff51},

	// Page Up and Page Down are Prior and Next in X11, which is the
	// kind of thing this table exists to get right.
	input.KeyHome:     {"Home", 0xff50},
	input.KeyEnd:      {"End", 0xff57},
	input.KeyInsert:   {"Insert", 0xff63},
	input.KeyDelete:   {"Delete", 0xffff},
	input.KeyPageUp:   {"Prior", 0xff55},
	input.KeyPageDown: {"Next", 0xff56},

	input.KeyF1:  {"F1", 0xffbe},
	input.KeyF2:  {"F2", 0xffbf},
	input.KeyF3:  {"F3", 0xffc0},
	input.KeyF4:  {"F4", 0xffc1},
	input.KeyF5:  {"F5", 0xffc2},
	input.KeyF6:  {"F6", 0xffc3},
	input.KeyF7:  {"F7", 0xffc4},
	input.KeyF8:  {"F8", 0xffc5},
	input.KeyF9:  {"F9", 0xffc6},
	input.KeyF10: {"F10", 0xffc7},
	input.KeyF11: {"F11", 0xffc8},
	input.KeyF12: {"F12", 0xffc9},
}

func init() {
	for k := input.KeyA; k <= input.KeyZ; k++ {
		letter := rune('a' + k - input.KeyA)
		keysyms[k] = keysym{Name: string(letter), Value: int(letter)}
	}
}

// modifierNames turns gridterm's modifier set into X11's.
//
// Alt and Super are mod1 and mod4 because that is where nearly every
// keymap binds them; the server maps mod1..mod5 onto whatever its own
// keymap has there, so these are a statement about the usual layout
// rather than about the physical keys.
func modifierNames(m input.Mods) []string {
	names := []string{}
	if m&input.ModShift != 0 {
		names = append(names, "shift")
	}
	if m&input.ModCtrl != 0 {
		names = append(names, "control")
	}
	if m&input.ModAlt != 0 {
		names = append(names, "mod1")
	}
	if m&input.ModSuper != 0 {
		names = append(names, "mod4")
	}
	return names
}

// keyboard turns gridterm's input events into xpra key events.
//
// It has state because gridterm reports one keystroke twice: as a key
// transition, and again as the text that keystroke produced, tied
// together by a Source. Sending both would type every letter twice, so
// whichever of the two names the key wins and the other is dropped.
//
// A named key wins when there is one. Everything else waits for its
// text, which is the only thing that knows what an AltGr layout or a
// dead key actually produced.
type keyboard struct {
	// held is the modifier set the server has been told about, as real
	// key presses. gridterm names no modifier key -- there is no
	// input.KeyShift -- so the presses have to be synthesised from the
	// bitmask that rides along with every other event.
	held input.Mods

	// named holds the sources already sent as a named key, so that the
	// text of the same keystroke is dropped and the release is sent.
	// One entry per key held down.
	named map[input.Source]bool
}

func newKeyboard() *keyboard {
	return &keyboard{named: map[input.Source]bool{}}
}

// handle returns the xpra key events one gridterm event produces: none,
// one, or -- for text, which arrives as a single event -- a press and
// the release after it.
func (k *keyboard) handle(e input.Event, window ui.WindowID) []ui.Key {
	out := k.syncModifiers(e.Mods, window)
	return append(out, k.handleKey(e, window)...)
}

// modifierKeys are the keys behind gridterm's modifier bitmask. Left
// hand sides, because a keymap always has those and may not have the
// right ones.
var modifierKeys = []struct {
	bit input.Mods
	sym keysym
}{
	{input.ModShift, keysym{"Shift_L", 0xffe1}},
	{input.ModCtrl, keysym{"Control_L", 0xffe3}},
	{input.ModAlt, keysym{"Alt_L", 0xffe9}},
	{input.ModSuper, keysym{"Super_L", 0xffeb}},
}

// syncModifiers presses and releases modifier keys so that the server
// holds the ones this event says are down.
//
// The modifier list on each event describes the state *before* it, which
// is what X reports and what the server reconciles against: pressing
// Shift is a key event that itself carries no shift.
func (k *keyboard) syncModifiers(want input.Mods, window ui.WindowID) []ui.Key {
	var out []ui.Key
	for _, m := range modifierKeys {
		switch {
		case want&m.bit != 0 && k.held&m.bit == 0:
			out = append(out, k.press(window, m.sym, "", k.held, true))
			k.held |= m.bit
		case want&m.bit == 0 && k.held&m.bit != 0:
			out = append(out, k.press(window, m.sym, "", k.held, false))
			k.held &^= m.bit
		}
	}
	return out
}

func (k *keyboard) handleKey(e input.Event, window ui.WindowID) []ui.Key {
	switch e.Kind {
	case input.KeyPress, input.KeyRepeat:
		if textual(e.Key) {
			// Wait for the text. It is the resolved symbol -- "A", not
			// "a" with shift held -- which is what the server matches.
			return nil
		}
		sym, ok := keysyms[e.Key]
		if !ok {
			// An ordinary typing key. Its text event knows what it
			// produced and this does not, so say nothing and wait.
			return nil
		}
		if e.Source != 0 {
			k.named[e.Source] = true
		}
		return []ui.Key{k.press(window, sym, "", e.Mods, true)}

	case input.KeyRelease:
		if textual(e.Key) {
			return nil
		}
		sym, ok := keysyms[e.Key]
		if !ok {
			return nil
		}
		if e.Source != 0 {
			delete(k.named, e.Source)
		}
		return []ui.Key{k.press(window, sym, "", e.Mods, false)}

	case input.Text:
		return k.text(e, window)
	}
	return nil
}

// text sends a code point the platform called ordinary typing.
//
// The modifiers are deliberately not passed on. A keysym name reached
// this way is already the finished symbol -- "exclam", not "1" with
// shift held -- so repeating the shift would ask the server to shift
// something that is shifted, and Ctrl or AltGr here belong to the
// layout that produced the character rather than to the character.
func (k *keyboard) text(e input.Event, window ui.WindowID) []ui.Key {
	if k.named[e.Source] {
		// Already sent under its own name.
		return nil
	}
	if !e.NormalText {
		// The side effect of a shortcut, not typing. gridterm's own
		// encoder makes the same test, and for the same reason: without
		// it Ctrl+C would send both the shortcut and the letter.
		return nil
	}
	name := ui.KeysymName(e.Rune)
	if name == "" {
		// Outside printable ASCII. The text is still worth sending, and
		// the server falls back on it when it cannot match a name.
		return k.textFallback(e, window)
	}
	sym := keysym{Name: name, Value: int(e.Rune)}
	down := k.press(window, sym, string(e.Rune), e.Mods, true)
	up := k.press(window, sym, string(e.Rune), e.Mods, false)
	down.Keycode, up.Keycode = keycodeFor(sym.Name), keycodeFor(sym.Name)
	return []ui.Key{down, up}
}

// textFallback sends a character X11 has no ASCII name for.
//
// Unicode keysyms are the code point plus 0x01000000, which is the
// convention every X client uses for anything outside Latin-1.
//
// These do not arrive anywhere today: go-xpra drops any key whose Name
// is empty rather than let the server guess from the keycode
// (client/input.go, handleKey). So every accented and non-Latin
// character is silently lost. Naming them needs the same keymap upload
// the modifiers need -- it is the same gap seen from another side.
func (k *keyboard) textFallback(e input.Event, window ui.WindowID) []ui.Key {
	if e.Rune <= 0 {
		return nil
	}
	sym := keysym{Value: int(e.Rune) | 0x01000000}
	if e.Rune < 0x100 {
		// Latin-1 is its own keysym range, unshifted.
		sym.Value = int(e.Rune)
	}
	return []ui.Key{
		k.press(window, sym, string(e.Rune), 0, true),
		k.press(window, sym, string(e.Rune), 0, false),
	}
}

// press builds one key event.
//
// text is what the keystroke typed, and empty for a key that types
// nothing or whose text has not arrived yet -- a named key is sent the
// moment it goes down, and its text event comes after. The server only
// falls back on it when the name matches nothing, so an empty one costs
// nothing.
func (k *keyboard) press(window ui.WindowID, sym keysym, text string, mods input.Mods, down bool) ui.Key {
	return ui.Key{
		Window:    window,
		Pressed:   down,
		Name:      sym.Name,
		Keysym:    sym.Value,
		Keycode:   keycodeFor(sym.Name),
		Text:      text,
		Modifiers: modifierNames(mods),
	}
}

// textual reports whether a key is one that types something, and so
// should wait for its text rather than be sent under its own name.
func textual(k input.Key) bool {
	switch {
	case k >= input.KeyA && k <= input.KeyZ:
		return true
	case k == input.KeySpace, k == input.Key0,
		k == input.KeyBracketLeft, k == input.KeyBracketRight,
		k == input.KeyBackslash, k == input.KeyEquals,
		k == input.KeyPlus, k == input.KeyMinus:
		return true
	}
	return false
}
