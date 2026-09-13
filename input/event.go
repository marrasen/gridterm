// Package input turns window-system key and text events into the bytes
// a terminal application expects on its input stream.
//
// This is the layer stock ebitengine cannot provide. Upstream gives you
// polled IsKeyPressed plus AppendInputChars, which cannot tell Ctrl+C
// from the letter c, nor an OS key repeat from a fresh press. The
// unstablebuild fork adds AppendInputEvents, where each observation is
// either a key transition (press/release/repeat with modifiers) or a
// committed code point, and the two are tied together by an InputSource
// id. That is the shape a terminal needs.
//
// This file deliberately does not import ebiten. The encoding rules are
// the fiddly part and they should be testable on any machine, including
// one with no GPU and no X11 headers. The ebiten adapter lives in the
// sub-package input/ebitenin.
package input

import "unicode/utf8"

// Kind distinguishes a key transition from committed text.
type Kind uint8

const (
	KeyPress Kind = iota
	KeyRelease
	KeyRepeat
	Text
)

// Mods is a bitmask of held modifier keys.
type Mods uint8

const (
	ModShift Mods = 1 << iota
	ModAlt
	ModCtrl
	ModSuper
)

// Key identifies a physical key we encode specially. Keys that simply
// produce text are not listed: they arrive as Text events instead.
type Key uint16

const (
	KeyNone Key = iota

	KeyA // letters must stay contiguous, A..Z, for the Ctrl+letter maths
	KeyB
	KeyC
	KeyD
	KeyE
	KeyF
	KeyG
	KeyH
	KeyI
	KeyJ
	KeyK
	KeyL
	KeyM
	KeyN
	KeyO
	KeyP
	KeyQ
	KeyR
	KeyS
	KeyT
	KeyU
	KeyV
	KeyW
	KeyX
	KeyY
	KeyZ

	KeyEnter
	KeyTab
	KeyBackspace
	KeyEscape
	KeySpace
	KeyBracketLeft
	KeyBracketRight
	KeyBackslash

	KeyUp
	KeyDown
	KeyRight
	KeyLeft
	KeyHome
	KeyEnd
	KeyInsert
	KeyDelete
	KeyPageUp
	KeyPageDown

	KeyF1
	KeyF2
	KeyF3
	KeyF4
	KeyF5
	KeyF6
	KeyF7
	KeyF8
	KeyF9
	KeyF10
	KeyF11
	KeyF12
)

// Source identifies the native key action an observation came from. All
// observations of one physical keystroke share a nonzero Source, which
// is what lets a caller decide whether a code point was caused by a key
// it has already handled.
type Source uint64

// Event is one normalised input observation.
type Event struct {
	Kind   Kind
	Key    Key
	Mods   Mods
	Rune   rune
	Source Source

	// NormalText marks a code point the platform considers ordinary
	// typing rather than the side effect of a shortcut. AltGr layouts
	// report normal text while Ctrl+Alt is held, which is why this comes
	// from the platform instead of being derived from Mods.
	NormalText bool
}

// Ctrl reports whether Control was held.
func (e Event) Ctrl() bool { return e.Mods&ModCtrl != 0 }

// Alt reports whether Alt (Option) was held.
func (e Event) Alt() bool { return e.Mods&ModAlt != 0 }

// Shift reports whether Shift was held.
func (e Event) Shift() bool { return e.Mods&ModShift != 0 }

// Mode carries the terminal modes that change how a key is encoded. A
// program turns these on through escape sequences, so the encoder has to
// be told about them or arrow keys stop working inside vim and readline.
type Mode struct {
	// AppCursor is DECCKM. With it set, the cursor keys are sent as SS3
	// (ESC O A) rather than CSI (ESC [ A).
	AppCursor bool
}

// Encode appends the bytes to write to a PTY or an SSH session channel
// for one event, and returns the extended buffer. It returns nil when
// the event produces no input.
//
// Key releases produce nothing, which is correct for a plain VT stream.
// A terminal negotiating the Kitty keyboard protocol would encode them —
// possible here only because the fork reports releases at all.
func Encode(e Event, dst []byte) []byte { return EncodeMode(e, Mode{}, dst) }

// EncodeMode is Encode with the terminal's current modes applied.
func EncodeMode(e Event, m Mode, dst []byte) []byte {
	switch e.Kind {
	case KeyRelease:
		return nil

	case Text:
		// Only commit text the platform classes as ordinary typing.
		// Without this, Ctrl+C would emit both 03 and the letter c on
		// platforms that translate the key anyway.
		if !e.NormalText {
			return nil
		}
		return utf8.AppendRune(dst, e.Rune)

	case KeyPress, KeyRepeat:
		return encodeKey(e, m, dst)
	}
	return nil
}

func encodeKey(e Event, m Mode, dst []byte) []byte {
	// Keys with a CSI or SS3 form carry their modifiers inside the
	// sequence, so they must be handled before the Alt/ESC-prefix rule
	// below. Getting this order wrong sends ESC ESC [ A for Alt+Up
	// instead of CSI 1;3 A, which vim reads as two separate keys.
	if final, ok := csiFinal[e.Key]; ok {
		// Application cursor mode moves the unmodified cursor keys to
		// SS3. Modified ones stay CSI, as xterm does, because SS3 has
		// nowhere to put a modifier parameter.
		if m.AppCursor && modParam(e.Mods) == 1 && isCursorKey(e.Key) {
			return append(dst, 0x1b, 'O', final)
		}
		return appendCSI(dst, "", final, e.Mods)
	}
	if final, ok := ss3Final[e.Key]; ok {
		if modParam(e.Mods) == 1 {
			return append(dst, 0x1b, 'O', final)
		}
		// Modified F1..F4 switch from SS3 to CSI, as xterm does.
		return appendCSI(dst, "1", final, e.Mods)
	}
	if param, ok := csiTilde[e.Key]; ok {
		return appendCSI(dst, param, '~', e.Mods)
	}

	// Control codes. Ctrl+A..Ctrl+Z map to 0x01..0x1a; the handful of
	// punctuation controls follow the usual xterm mapping.
	if e.Ctrl() && !e.Alt() {
		if e.Key >= KeyA && e.Key <= KeyZ {
			return append(dst, byte(e.Key-KeyA)+1)
		}
		switch e.Key {
		case KeyBracketLeft:
			return append(dst, 0x1b)
		case KeyBackslash:
			return append(dst, 0x1c)
		case KeyBracketRight:
			return append(dst, 0x1d)
		case KeySpace:
			return append(dst, 0x00)
		}
	}

	// Alt prefixes the sequence with ESC, which is how xterm reports
	// Meta by default.
	if e.Alt() {
		stripped := Event{Kind: e.Kind, Key: e.Key, Mods: e.Mods &^ ModAlt}
		if rest := encodeKey(stripped, m, nil); rest != nil {
			return append(append(dst, 0x1b), rest...)
		}
	}

	switch e.Key {
	case KeyEnter:
		return append(dst, '\r')
	case KeyTab:
		if e.Shift() {
			return append(dst, 0x1b, '[', 'Z') // back-tab
		}
		return append(dst, '\t')
	case KeyBackspace:
		return append(dst, 0x7f)
	case KeyEscape:
		return append(dst, 0x1b)
	}
	// Everything else is ordinary typing and arrives as a Text event.
	return nil
}

// csiFinal holds keys encoded as CSI [mods] <final byte>.
var csiFinal = map[Key]byte{
	KeyUp:    'A',
	KeyDown:  'B',
	KeyRight: 'C',
	KeyLeft:  'D',
	KeyEnd:   'F',
	KeyHome:  'H',
}

// isCursorKey reports whether k is one of the four arrows or Home/End,
// the keys DECCKM applies to.
func isCursorKey(k Key) bool {
	switch k {
	case KeyUp, KeyDown, KeyLeft, KeyRight, KeyHome, KeyEnd:
		return true
	}
	return false
}

// ss3Final holds F1..F4, which xterm sends as SS3 (ESC O x) when
// unmodified and as CSI 1;<mods> x when modified.
var ss3Final = map[Key]byte{
	KeyF1: 'P',
	KeyF2: 'Q',
	KeyF3: 'R',
	KeyF4: 'S',
}

// csiTilde holds keys encoded as CSI <param> [;<mods>] ~.
var csiTilde = map[Key]string{
	KeyInsert:   "2",
	KeyDelete:   "3",
	KeyPageUp:   "5",
	KeyPageDown: "6",
	KeyF5:       "15",
	KeyF6:       "17",
	KeyF7:       "18",
	KeyF8:       "19",
	KeyF9:       "20",
	KeyF10:      "21",
	KeyF11:      "23",
	KeyF12:      "24",
}

// appendCSI writes CSI [param] [;mods] final, using xterm's modifier
// encoding: 1 + shift(1) + alt(2) + ctrl(4).
func appendCSI(dst []byte, param string, final byte, mods Mods) []byte {
	m := modParam(mods)
	dst = append(dst, 0x1b, '[')
	switch {
	case m == 1 && param == "":
		// No parameters at all: CSI A.
	case m == 1:
		dst = append(dst, param...)
	default:
		if param == "" {
			param = "1"
		}
		dst = append(dst, param...)
		dst = append(dst, ';')
		dst = append(dst, byte('0'+m))
	}
	return append(dst, final)
}

func modParam(mods Mods) int {
	m := 1
	if mods&ModShift != 0 {
		m += 1
	}
	if mods&ModAlt != 0 {
		m += 2
	}
	if mods&ModCtrl != 0 {
		m += 4
	}
	return m
}

// String renders a key name for diagnostics.
func (k Key) String() string {
	if name, ok := keyNames[k]; ok {
		return name
	}
	if k >= KeyA && k <= KeyZ {
		return string(rune('A' + k - KeyA))
	}
	return "Key?"
}

var keyNames = map[Key]string{
	KeyNone: "-", KeyEnter: "Enter", KeyTab: "Tab", KeyBackspace: "Backspace",
	KeyEscape: "Escape", KeySpace: "Space", KeyBracketLeft: "[",
	KeyBracketRight: "]", KeyBackslash: "\\", KeyUp: "Up", KeyDown: "Down",
	KeyRight: "Right", KeyLeft: "Left", KeyHome: "Home", KeyEnd: "End",
	KeyInsert: "Insert", KeyDelete: "Delete", KeyPageUp: "PageUp",
	KeyPageDown: "PageDown", KeyF1: "F1", KeyF2: "F2", KeyF3: "F3",
	KeyF4: "F4", KeyF5: "F5", KeyF6: "F6", KeyF7: "F7", KeyF8: "F8",
	KeyF9: "F9", KeyF10: "F10", KeyF11: "F11", KeyF12: "F12",
}

// String renders a modifier set for diagnostics.
func (m Mods) String() string {
	if m == 0 {
		return "-"
	}
	var out []byte
	add := func(s string) {
		if len(out) > 0 {
			out = append(out, '+')
		}
		out = append(out, s...)
	}
	if m&ModCtrl != 0 {
		add("ctrl")
	}
	if m&ModAlt != 0 {
		add("alt")
	}
	if m&ModShift != 0 {
		add("shift")
	}
	if m&ModSuper != 0 {
		add("super")
	}
	return string(out)
}
