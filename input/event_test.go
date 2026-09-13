package input

import "testing"

func enc(e Event) []byte { return Encode(e, nil) }

func TestCtrlLetterBecomesControlCode(t *testing.T) {
	got := enc(Event{Kind: KeyPress, Key: KeyC, Mods: ModCtrl})
	if len(got) != 1 || got[0] != 0x03 {
		t.Fatalf("Ctrl+C = % x, want 03", got)
	}
}

// The whole reason this project needs the forked input pipeline: stock
// ebitengine reports "the user typed c" and nothing more, so Ctrl+C and
// c are indistinguishable.
func TestCtrlCAndPlainCDiffer(t *testing.T) {
	ctrlC := enc(Event{Kind: KeyPress, Key: KeyC, Mods: ModCtrl})
	plainC := enc(Event{Kind: Text, Rune: 'c', NormalText: true})

	if string(ctrlC) == string(plainC) {
		t.Fatalf("Ctrl+C and c both encoded as % x", ctrlC)
	}
	if string(plainC) != "c" {
		t.Fatalf("plain c = %q, want \"c\"", plainC)
	}
}

func TestShortcutTextIsNotCommitted(t *testing.T) {
	// A platform that translates Ctrl+C into a 'c' code point anyway
	// marks it as not-normal text. Committing it would send 03 63.
	if got := enc(Event{Kind: Text, Rune: 'c', Mods: ModCtrl}); got != nil {
		t.Fatalf("non-normal text encoded as % x, want nothing", got)
	}
}

// AltGr layouts report normal text while Ctrl+Alt is held. Deriving
// "is this typing?" from the modifier mask would silently break, for
// example, a Swedish keyboard's AltGr+2 for @.
func TestAltGrTextIsCommittedDespiteModifiers(t *testing.T) {
	got := enc(Event{
		Kind: Text, Rune: '@', NormalText: true, Mods: ModCtrl | ModAlt,
	})
	if string(got) != "@" {
		t.Fatalf("AltGr text = %q, want \"@\"", got)
	}
}

func TestKeyReleaseProducesNothing(t *testing.T) {
	if got := enc(Event{Kind: KeyRelease, Key: KeyA}); got != nil {
		t.Fatalf("release encoded as % x, want nothing", got)
	}
}

func TestRepeatEncodesLikePress(t *testing.T) {
	press := enc(Event{Kind: KeyPress, Key: KeyUp})
	repeat := enc(Event{Kind: KeyRepeat, Key: KeyUp})
	if string(press) != string(repeat) {
		t.Fatalf("repeat = % x, press = % x; holding a key should repeat it",
			repeat, press)
	}
}

func TestNamedKeys(t *testing.T) {
	cases := []struct {
		name string
		key  Key
		want string
	}{
		{"up", KeyUp, "\x1b[A"},
		{"down", KeyDown, "\x1b[B"},
		{"right", KeyRight, "\x1b[C"},
		{"left", KeyLeft, "\x1b[D"},
		{"home", KeyHome, "\x1b[H"},
		{"end", KeyEnd, "\x1b[F"},
		{"insert", KeyInsert, "\x1b[2~"},
		{"delete", KeyDelete, "\x1b[3~"},
		{"pageup", KeyPageUp, "\x1b[5~"},
		{"pagedown", KeyPageDown, "\x1b[6~"},
		{"f1", KeyF1, "\x1bOP"},
		{"f4", KeyF4, "\x1bOS"},
		{"f5", KeyF5, "\x1b[15~"},
		{"f12", KeyF12, "\x1b[24~"},
		{"enter", KeyEnter, "\r"},
		{"tab", KeyTab, "\t"},
		{"backspace", KeyBackspace, "\x7f"},
		{"escape", KeyEscape, "\x1b"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := string(enc(Event{Kind: KeyPress, Key: tc.key})); got != tc.want {
				t.Errorf("= %q, want %q", got, tc.want)
			}
		})
	}
}

func TestCtrlPunctuationControls(t *testing.T) {
	cases := []struct {
		key  Key
		want byte
	}{
		{KeyBracketLeft, 0x1b},
		{KeyBackslash, 0x1c},
		{KeyBracketRight, 0x1d},
		{KeySpace, 0x00},
	}
	for _, tc := range cases {
		got := enc(Event{Kind: KeyPress, Key: tc.key, Mods: ModCtrl})
		if len(got) != 1 || got[0] != tc.want {
			t.Errorf("Ctrl+%s = % x, want %02x", tc.key, got, tc.want)
		}
	}
}

func TestShiftTabIsBackTab(t *testing.T) {
	got := enc(Event{Kind: KeyPress, Key: KeyTab, Mods: ModShift})
	if string(got) != "\x1b[Z" {
		t.Fatalf("Shift+Tab = %q, want \"\\x1b[Z\"", got)
	}
}

func TestModifiedArrowsCarryTheXtermModifierParameter(t *testing.T) {
	cases := []struct {
		name string
		mods Mods
		want string
	}{
		{"shift", ModShift, "\x1b[1;2A"},
		{"alt", ModAlt, "\x1b[1;3A"},
		{"ctrl", ModCtrl, "\x1b[1;5A"},
		{"ctrl+shift", ModCtrl | ModShift, "\x1b[1;6A"},
		{"ctrl+alt", ModCtrl | ModAlt, "\x1b[1;7A"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := enc(Event{Kind: KeyPress, Key: KeyUp, Mods: tc.mods})
			if string(got) != tc.want {
				t.Errorf("= %q, want %q", got, tc.want)
			}
		})
	}
}

func TestModifiedF1SwitchesFromSS3ToCSI(t *testing.T) {
	got := enc(Event{Kind: KeyPress, Key: KeyF1, Mods: ModShift})
	if string(got) != "\x1b[1;2P" {
		t.Fatalf("Shift+F1 = %q, want \"\\x1b[1;2P\"", got)
	}
}

func TestModifiedTildeKeysCarryTheModifierParameter(t *testing.T) {
	got := enc(Event{Kind: KeyPress, Key: KeyDelete, Mods: ModCtrl})
	if string(got) != "\x1b[3;5~" {
		t.Fatalf("Ctrl+Delete = %q, want \"\\x1b[3;5~\"", got)
	}
}

func TestAltPrefixesWithEsc(t *testing.T) {
	got := enc(Event{Kind: KeyPress, Key: KeyEnter, Mods: ModAlt})
	if string(got) != "\x1b\r" {
		t.Fatalf("Alt+Enter = %q, want \"\\x1b\\r\"", got)
	}
}

func TestUnmodifiedLetterKeyPressProducesNothing(t *testing.T) {
	// The letter arrives as a Text event instead; encoding the key press
	// too would double every character.
	if got := enc(Event{Kind: KeyPress, Key: KeyA}); got != nil {
		t.Fatalf("plain 'a' key press encoded as % x, want nothing", got)
	}
}

func TestTextEncodesAsUTF8(t *testing.T) {
	got := enc(Event{Kind: Text, Rune: 'ö', NormalText: true})
	if string(got) != "ö" {
		t.Fatalf("= % x, want the UTF-8 bytes of ö", got)
	}
}

func TestEncodeAppendsToCallerBuffer(t *testing.T) {
	buf := make([]byte, 0, 16)
	buf = Encode(Event{Kind: Text, Rune: 'a', NormalText: true}, buf)
	buf = Encode(Event{Kind: KeyPress, Key: KeyEnter}, buf)
	if string(buf) != "a\r" {
		t.Fatalf("= %q, want \"a\\r\"", buf)
	}
}

func TestModifierHelpers(t *testing.T) {
	e := Event{Mods: ModCtrl | ModShift}
	if !e.Ctrl() || !e.Shift() || e.Alt() {
		t.Fatalf("Ctrl=%v Shift=%v Alt=%v, want true true false",
			e.Ctrl(), e.Shift(), e.Alt())
	}
	if got := e.Mods.String(); got != "ctrl+shift" {
		t.Errorf("Mods.String() = %q, want \"ctrl+shift\"", got)
	}
}
