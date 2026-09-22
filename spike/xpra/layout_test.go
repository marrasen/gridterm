package main

import (
	"testing"

	"github.com/Xpra-org/go-xpra/ui"

	"github.com/marrasen/gridterm/input"
)

// TestLayoutCoversEverythingWeCanSend is the one that matters. A keysym
// sent with no keycode is typed at whatever level the server happens to
// be in, which is how "Hello, World!" arrives as "hello, world1".
func TestLayoutCoversEverythingWeCanSend(t *testing.T) {
	for key, sym := range keysyms {
		if keycodeFor(sym.Name) == 0 {
			t.Errorf("%v is named %q and has no keycode", key, sym.Name)
		}
	}
	for r := rune(0x20); r <= 0x7e; r++ {
		name := ui.KeysymName(r)
		if name == "" {
			continue
		}
		if keycodeFor(name) == 0 {
			t.Errorf("typing %q sends %q, which has no keycode", r, name)
		}
	}
	for _, m := range modifierKeys {
		if keycodeFor(m.sym.Name) == 0 {
			t.Errorf("the modifier %q has no keycode", m.sym.Name)
		}
	}
}

// TestKeycodesAreUniqueAndInRange checks the layout is one X11 will
// take. Two keysyms on one keycode would make one of them unreachable.
func TestKeycodesAreUniqueAndInRange(t *testing.T) {
	seen := map[int]string{}
	for _, m := range layout {
		if m.Keycode < firstKeycode || m.Keycode > 255 {
			t.Errorf("%q has keycode %d, outside X11's 8..255", m.Name, m.Keycode)
		}
		if other, dup := seen[m.Keycode]; dup {
			t.Errorf("%q and %q share keycode %d", other, m.Name, m.Keycode)
		}
		seen[m.Keycode] = m.Name
	}
	if len(layout) > 255-firstKeycode+1 {
		t.Errorf("the layout has %d keys, more than X11 has keycodes", len(layout))
	}
}

// TestLayoutIsStable keeps two runs of the same build declaring the same
// thing. The server hashes the keymap to decide whether it changed, so
// an unstable one would have it reload the keyboard on every connect.
func TestLayoutIsStable(t *testing.T) {
	first, _ := buildLayout()
	second, _ := buildLayout()
	if len(first) != len(second) {
		t.Fatalf("two builds gave %d and %d keys", len(first), len(second))
	}
	for i := range first {
		if first[i] != second[i] {
			t.Fatalf("entry %d differs: %+v then %+v", i, first[i], second[i])
		}
	}
}

// TestEverythingIsUnshifted records the choice this layout makes.
//
// A real keyboard puts "a" and "A" on one key and asks the server to
// pick the level. This asks for the finished keysym by name, so the
// modifiers say only what the user was holding -- which is all gridterm
// ever knows.
func TestEverythingIsUnshifted(t *testing.T) {
	for _, m := range layout {
		if m.Level != 0 || m.Group != 0 {
			t.Errorf("%q sits at group %d level %d, want 0 and 0", m.Name, m.Group, m.Level)
		}
	}
	if keycodeFor("a") == keycodeFor("A") {
		t.Error(`"a" and "A" share a keycode; they are separate keys in this layout`)
	}
}

// TestModifierMeaningsNameEveryModifier gives the server the keycode to
// press when it is told to hold one.
func TestModifierMeaningsNameEveryModifier(t *testing.T) {
	meanings := modifierMeanings()
	for _, m := range modifierKeys {
		meaning, ok := meanings[m.sym.Name]
		if !ok {
			t.Errorf("%q is not named as a modifier", m.sym.Name)
			continue
		}
		if want := modifierNames(m.bit)[0]; meaning != want {
			t.Errorf("%q means %q, want %q", m.sym.Name, meaning, want)
		}
	}
	if got := meanings["Shift_L"]; got != "shift" {
		t.Errorf("Shift_L means %q, want shift", got)
	}
}

// TestKeysWeSendCarryTheirKeycode ties the layout to the translator:
// the keycode has to reach the packet, not just exist in a table.
func TestKeysWeSendCarryTheirKeycode(t *testing.T) {
	k := newKeyboard()
	got := k.handle(input.Event{
		Kind: input.Text, Rune: 'A', Mods: input.ModShift, Source: 1, NormalText: true,
	}, 1)
	if len(got) < 1 {
		t.Fatal("typing A sent nothing")
	}
	// The shift press comes first, then the key.
	key := got[len(got)-2]
	if key.Name != "A" {
		t.Fatalf("expected the A press, got %+v", key)
	}
	if key.Keycode != keycodeFor("A") || key.Keycode == 0 {
		t.Errorf("A went out with keycode %d, want %d", key.Keycode, keycodeFor("A"))
	}
	if got[0].Name != "Shift_L" || got[0].Keycode != keycodeFor("Shift_L") {
		t.Errorf("Shift_L went out as %+v, want keycode %d", got[0], keycodeFor("Shift_L"))
	}
}
