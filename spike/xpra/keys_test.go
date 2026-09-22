package main

import (
	"bufio"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/Xpra-org/go-xpra/ui"

	"github.com/marrasen/gridterm/input"
)

// realKeysyms reads the X11 protocol's own list of keysyms.
//
// testdata/keysyms.txt was extracted from /usr/include/X11/keysymdef.h,
// which the x11proto-dev package installs. Regenerate it with:
//
//	grep -oP '^#define XK_\K(\w+)\s+(0x[0-9a-fA-F]+)' \
//	  /usr/include/X11/keysymdef.h | tr -s ' ' '\t'
//
// The point of checking against it is that a keysym name is not a
// guessable thing. Page Up is "Prior", Escape is "Escape" but Return is
// not "Enter", and a name that does not exist fails silently on the far
// side: the server matches nothing and the key does nothing.
func realKeysyms(t *testing.T) map[string]int {
	t.Helper()
	f, err := os.Open("testdata/keysyms.txt")
	if err != nil {
		t.Fatalf("opening the keysym list: %v", err)
	}
	defer f.Close()

	found := map[string]int{}
	scan := bufio.NewScanner(f)
	for scan.Scan() {
		line := scan.Text()
		if strings.HasPrefix(line, "#") || line == "" {
			continue
		}
		name, value, ok := strings.Cut(line, "\t")
		if !ok {
			t.Fatalf("malformed line %q", line)
		}
		n, err := strconv.ParseUint(strings.TrimPrefix(value, "0x"), 16, 32)
		if err != nil {
			t.Fatalf("malformed value in %q: %v", line, err)
		}
		found[name] = int(n)
	}
	if err := scan.Err(); err != nil {
		t.Fatal(err)
	}
	if len(found) < 2000 {
		t.Fatalf("only %d keysyms in the list; it looks truncated", len(found))
	}
	return found
}

// TestEveryKeysymIsReal checks the whole table against X11's own header.
func TestEveryKeysymIsReal(t *testing.T) {
	real := realKeysyms(t)
	for key, sym := range keysyms {
		value, ok := real[sym.Name]
		if !ok {
			t.Errorf("%v maps to %q, which is not an X11 keysym name", key, sym.Name)
			continue
		}
		if value != sym.Value {
			t.Errorf("%v maps to %q = %#x, but X11 says %#x", key, sym.Name, sym.Value, value)
		}
	}
}

// TestEveryKeyIsMapped checks that no key gridterm names is missing.
//
// Key.String returns "Key?" for a value that is not a key, which is how
// the end of the list is found without gridterm having to export one. A
// key added to the middle of that enum shifts every constant after it,
// and the table above is written against the constants, so it follows
// automatically; one added to the end is what this catches.
func TestEveryKeyIsMapped(t *testing.T) {
	mapped := 0
	for k := input.Key(1); k < input.Key(256); k++ {
		if k.String() == "Key?" {
			continue
		}
		if _, ok := keysyms[k]; !ok {
			t.Errorf("gridterm has a key %v (%d) with no X11 keysym", k, k)
			continue
		}
		mapped++
	}
	// Every constant in the enum but KeyNone.
	if want := int(input.KeyF12); mapped != want {
		t.Errorf("mapped %d keys, want %d", mapped, want)
	}
}

// TestLettersAreTheirOwnNames covers the twenty-six filled in by init.
func TestLettersAreTheirOwnNames(t *testing.T) {
	for k := input.KeyA; k <= input.KeyZ; k++ {
		sym := keysyms[k]
		want := string(rune('a' + k - input.KeyA))
		if sym.Name != want {
			t.Errorf("%v is named %q, want %q", k, sym.Name, want)
		}
		if sym.Value != int(want[0]) {
			t.Errorf("%v has value %#x, want %#x", k, sym.Value, want[0])
		}
	}
	// The name is the unshifted letter even for a capital: X11 names the
	// key, and Shift is a modifier sent beside it.
	if got := keysyms[input.KeyA].Name; got != "a" {
		t.Errorf("KeyA is named %q, want the lower case %q", got, "a")
	}
}

// TestTheEasilyWrongOnes pins the names most likely to be guessed.
func TestTheEasilyWrongOnes(t *testing.T) {
	for _, c := range []struct {
		key  input.Key
		name string
	}{
		{input.KeyPageUp, "Prior"},        // not "PageUp"
		{input.KeyPageDown, "Next"},       // not "PageDown"
		{input.KeyEnter, "Return"},        // not "Enter"
		{input.KeyBackspace, "BackSpace"}, // capital S
		{input.KeyEquals, "equal"},        // not "equals"
		{input.KeySpace, "space"},         // lower case
		{input.KeyEscape, "Escape"},       // upper case
	} {
		if got := keysyms[c.key].Name; got != c.name {
			t.Errorf("%v is named %q, want %q", c.key, got, c.name)
		}
	}
}

func TestModifierNames(t *testing.T) {
	for _, c := range []struct {
		mods input.Mods
		want []string
	}{
		{0, nil},
		{input.ModShift, []string{"shift"}},
		{input.ModCtrl, []string{"control"}},
		{input.ModAlt, []string{"mod1"}},
		{input.ModSuper, []string{"mod4"}},
		{input.ModCtrl | input.ModShift, []string{"shift", "control"}},
		{input.ModShift | input.ModAlt | input.ModCtrl | input.ModSuper,
			[]string{"shift", "control", "mod1", "mod4"}},
	} {
		got := modifierNames(c.mods)
		if len(got) != len(c.want) {
			t.Errorf("%v gave %v, want %v", c.mods, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%v gave %v, want %v", c.mods, got, c.want)
				break
			}
		}
	}
}

// TestATypingKeyWaitsForItsText is the rule the translator exists for.
//
// gridterm reports a keystroke twice, as a key and as the text it
// produced. The text wins, because it is the resolved symbol: "A", not
// "a" with shift held. That is what go-xpra's own X11 backend sends --
// see effectiveKeysym in that repository -- and it is what a real server
// types correctly.
func TestATypingKeyWaitsForItsText(t *testing.T) {
	k := newKeyboard()
	const src input.Source = 42

	// The press carries the modifier, so Shift goes down first. The key
	// itself says nothing yet: only its text knows what it produced.
	got := k.handle(input.Event{
		Kind: input.KeyPress, Key: input.KeyA, Mods: input.ModShift, Source: src,
	}, 1)
	if len(got) != 1 || got[0].Name != "Shift_L" || !got[0].Pressed {
		t.Fatalf("the press gave %+v, want Shift_L going down and nothing else", got)
	}
	if len(got[0].Modifiers) != 0 {
		t.Errorf("Shift_L went down carrying %v; it describes the state before itself",
			got[0].Modifiers)
	}

	got = k.handle(input.Event{
		Kind: input.Text, Rune: 'A', Mods: input.ModShift, Source: src, NormalText: true,
	}, 1)
	if len(got) != 2 {
		t.Fatalf("the text gave %d events, want a press and a release", len(got))
	}
	if got[0].Name != "A" {
		t.Errorf("name is %q, want the resolved %q", got[0].Name, "A")
	}
	if len(got[0].Modifiers) != 1 || got[0].Modifiers[0] != "shift" {
		t.Errorf("modifiers are %v, want [shift]", got[0].Modifiers)
	}

	// Letting go of shift releases the key that was pressed for it.
	got = k.handle(input.Event{Kind: input.KeyRelease, Key: input.KeyA, Source: src}, 1)
	if len(got) != 1 || got[0].Name != "Shift_L" || got[0].Pressed {
		t.Fatalf("the release gave %+v, want Shift_L going up", got)
	}
}

// TestModifiersAreSynthesised is the part gridterm forces on us.
//
// There is no input.KeyShift: gridterm reports modifiers as a bitmask
// riding along with other events and never as keys of their own. The
// server needs real presses, so they are made up from the bitmask.
// Without them a real server types every shifted key at the wrong level.
func TestModifiersAreSynthesised(t *testing.T) {
	k := newKeyboard()

	got := k.handle(input.Event{
		Kind: input.KeyPress, Key: input.KeyLeft,
		Mods: input.ModCtrl | input.ModShift, Source: 1,
	}, 1)
	if len(got) != 3 {
		t.Fatalf("got %d events, want Shift_L, Control_L and the key: %+v", len(got), got)
	}
	if got[0].Name != "Shift_L" || got[1].Name != "Control_L" || got[2].Name != "Left" {
		t.Fatalf("got %q, %q, %q", got[0].Name, got[1].Name, got[2].Name)
	}
	// Each modifier describes the state before itself, the way X does.
	if len(got[0].Modifiers) != 0 {
		t.Errorf("Shift_L carried %v, want none", got[0].Modifiers)
	}
	if len(got[1].Modifiers) != 1 || got[1].Modifiers[0] != "shift" {
		t.Errorf("Control_L carried %v, want [shift]", got[1].Modifiers)
	}

	// Nothing is pressed twice.
	got = k.handle(input.Event{
		Kind: input.KeyRelease, Key: input.KeyLeft,
		Mods: input.ModCtrl | input.ModShift, Source: 1,
	}, 1)
	if len(got) != 1 || got[0].Name != "Left" || got[0].Pressed {
		t.Fatalf("the release gave %+v, want just the key going up", got)
	}

	// And letting go of both releases both.
	got = k.handle(input.Event{Kind: input.KeyPress, Key: input.KeyHome, Source: 2}, 1)
	if len(got) != 3 || got[0].Name != "Shift_L" || got[1].Name != "Control_L" {
		t.Fatalf("got %+v, want both modifiers released before the key", got)
	}
	if got[0].Pressed || got[1].Pressed {
		t.Error("the modifiers were pressed again rather than released")
	}
}

// TestANonTypingKeyIsSentUnderItsName covers the other half: a key that
// produces no text has only the table to name it.
func TestANonTypingKeyIsSentUnderItsName(t *testing.T) {
	k := newKeyboard()
	got := k.handle(input.Event{Kind: input.KeyPress, Key: input.KeyLeft, Source: 3}, 1)
	if len(got) != 1 {
		t.Fatalf("got %d events, want one press: %+v", len(got), got)
	}
	if got[0].Name != "Left" || !got[0].Pressed {
		t.Errorf("got %+v, want a press named Left", got[0])
	}

	release := k.handle(input.Event{Kind: input.KeyRelease, Key: input.KeyLeft, Source: 3}, 1)
	if len(release) != 1 || release[0].Pressed {
		t.Errorf("the release gave %+v, want one release", release)
	}
}

// TestTextualKeysAreTheOnesThatType pins which side of that split each
// key falls on.
func TestTextualKeysAreTheOnesThatType(t *testing.T) {
	for _, k := range []input.Key{
		input.KeyA, input.KeyZ, input.KeySpace, input.Key0,
		input.KeyBracketLeft, input.KeyMinus, input.KeyEquals,
	} {
		if !textual(k) {
			t.Errorf("%v types something and should wait for its text", k)
		}
	}
	for _, k := range []input.Key{
		input.KeyLeft, input.KeyF1, input.KeyHome, input.KeyDelete,
		input.KeyEscape, input.KeyEnter, input.KeyTab, input.KeyBackspace,
	} {
		if textual(k) {
			t.Errorf("%v types nothing and has only its name", k)
		}
	}
}

// TestUnnamedKeysComeFromTheirText covers everything gridterm does not
// name -- the digits 1 to 9, the punctuation, anything from an AltGr
// layout. Those arrive only as text.
func TestUnnamedKeysComeFromTheirText(t *testing.T) {
	k := newKeyboard()
	const src input.Source = 7

	// The key transition says nothing but the shift it carries, because
	// it does not know what the layout produced.
	got := k.handle(input.Event{
		Kind: input.KeyPress, Key: input.KeyNone, Mods: input.ModShift, Source: src,
	}, 1)
	if len(got) != 1 || got[0].Name != "Shift_L" {
		t.Errorf("an unnamed key press gave %+v, want only Shift_L", got)
	}

	got = k.handle(input.Event{
		Kind: input.Text, Rune: '!', Mods: input.ModShift, Source: src, NormalText: true,
	}, 1)
	if len(got) != 2 {
		t.Fatalf("typing ! gave %d events, want a press and a release", len(got))
	}
	if got[0].Name != "exclam" || !got[0].Pressed {
		t.Errorf("the press is %+v, want a press named \"exclam\"", got[0])
	}
	if got[1].Name != "exclam" || got[1].Pressed {
		t.Errorf("the release is %+v, want a release named \"exclam\"", got[1])
	}
	if got[0].Text != "!" {
		t.Errorf("the text hint is %q, want %q", got[0].Text, "!")
	}
	// The modifiers gridterm reported go with it. The name says which
	// keysym and the modifiers say which level to type it at, and the
	// server wants both -- though see the note at the top of keys.go:
	// it cannot act on them without a keymap it was never sent.
	if len(got[0].Modifiers) != 1 || got[0].Modifiers[0] != "shift" {
		t.Errorf("typing shift+! carried %v, want [shift]", got[0].Modifiers)
	}
}

// TestShortcutTextIsDropped is the other half of the same rule: a
// platform that translates Ctrl+C into both a key and a code point must
// not have the letter typed into the far application.
func TestShortcutTextIsDropped(t *testing.T) {
	k := newKeyboard()
	got := k.handle(input.Event{
		Kind: input.Text, Rune: 'c', Source: 9, NormalText: false,
	}, 1)
	if len(got) != 0 {
		t.Errorf("a shortcut's text gave %+v, want nothing", got)
	}
}

// TestLatin1TextIsSent covers the characters most of Europe types.
//
// They used to be dropped: ui.KeysymName named printable ASCII and
// nothing else, and go-xpra refuses to send a key it cannot name. The
// fork names Latin-1 in full, and the layout gives each one a key.
func TestLatin1TextIsSent(t *testing.T) {
	for _, c := range []struct {
		r    rune
		name string
	}{
		{'ä', "adiaeresis"}, {'ö', "odiaeresis"}, {'å', "aring"},
		{'é', "eacute"}, {'ß', "ssharp"}, {'£', "sterling"},
	} {
		k := newKeyboard()
		got := k.handle(input.Event{
			Kind: input.Text, Rune: c.r, Source: 1, NormalText: true,
		}, 1)
		if len(got) != 2 {
			t.Errorf("typing %q gave %d events, want a press and a release", c.r, len(got))
			continue
		}
		if got[0].Name != c.name {
			t.Errorf("%q is named %q, want %q", c.r, got[0].Name, c.name)
		}
		// In this range the keysym is the code point.
		if got[0].Keysym != int(c.r) {
			t.Errorf("%q has keysym %#x, want %#x", c.r, got[0].Keysym, c.r)
		}
		if got[0].Keycode == 0 {
			t.Errorf("%q went out with no keycode, so the server has no key to press", c.r)
		}
	}
}

// TestBeyondLatin1IsDropped records the limit, and that it is a quiet
// one rather than a wrong character.
//
// X11's Unicode keysyms are far more numerous than the 247 keycodes a
// keyboard has, so a layout declared once at connection time cannot hold
// them. Doing better means handing out keycodes as characters turn up
// and telling the server the layout changed, which is not written.
func TestBeyondLatin1IsDropped(t *testing.T) {
	for _, r := range []rune{'€', '日', 'Ω', '😀'} {
		k := newKeyboard()
		if got := k.handle(input.Event{
			Kind: input.Text, Rune: r, Source: 1, NormalText: true,
		}, 1); len(got) != 0 {
			t.Errorf("typing %q gave %+v, want nothing: it is not in the layout", r, got)
		}
		// It is nameable, though -- only unplaceable.
		if ui.KeysymName(r) == "" {
			t.Errorf("%q has no keysym name at all", r)
		}
	}
}

// TestChordsAreSentUnderTheirOwnName is the case a typing key must not
// wait for text, because none is coming.
//
// gridterm marks the code point a shortcut produces as not-normal text
// and this drops it, so a key that waited would send its modifier and
// then nothing. Ctrl+C is the obvious victim; so is every paste.
func TestChordsAreSentUnderTheirOwnName(t *testing.T) {
	for _, mods := range []input.Mods{input.ModCtrl, input.ModAlt, input.ModSuper,
		input.ModCtrl | input.ModShift} {
		k := newKeyboard()
		got := k.handle(input.Event{
			Kind: input.KeyPress, Key: input.KeyV, Mods: mods, Source: 1,
		}, 1)

		var pressed *ui.Key
		for i := range got {
			if got[i].Name == "v" {
				pressed = &got[i]
			}
		}
		if pressed == nil {
			t.Errorf("%v+V sent %d events and none of them was the V: %+v", mods, len(got), got)
			continue
		}
		if pressed.Keycode != keycodeFor("v") {
			t.Errorf("%v+V went out with keycode %d, want %d", mods, pressed.Keycode, keycodeFor("v"))
		}

		release := k.handle(input.Event{
			Kind: input.KeyRelease, Key: input.KeyV, Mods: mods, Source: 1,
		}, 1)
		var up bool
		for _, r := range release {
			if r.Name == "v" && !r.Pressed {
				up = true
			}
		}
		if !up {
			t.Errorf("%v+V was never released: %+v", mods, release)
		}
	}
}

// TestPlainTypingStillWaits keeps the other half: with no Ctrl, Alt or
// Super the text is coming and is the better name.
func TestPlainTypingStillWaits(t *testing.T) {
	for _, mods := range []input.Mods{0, input.ModShift} {
		k := newKeyboard()
		got := k.handle(input.Event{
			Kind: input.KeyPress, Key: input.KeyV, Mods: mods, Source: 1,
		}, 1)
		for _, e := range got {
			if e.Name == "v" || e.Name == "V" {
				t.Errorf("%v+V was sent before its text arrived: %+v", mods, e)
			}
		}
	}
}
