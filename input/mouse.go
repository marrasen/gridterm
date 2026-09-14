package input

import "strconv"

// MouseButton identifies which button an event is about.
type MouseButton uint8

const (
	MouseNone MouseButton = iota
	MouseLeft
	MouseMiddle
	MouseRight
	MouseWheelUp
	MouseWheelDown
)

// MouseKind distinguishes the three things a mouse does.
type MouseKind uint8

const (
	MousePress MouseKind = iota
	MouseRelease
	MouseMove
)

// MouseEvent is a mouse action in grid coordinates, zero-based.
type MouseEvent struct {
	Kind   MouseKind
	Button MouseButton
	Col    int
	Row    int
	Mods   Mods
}

// MouseMode is what the program asked for with DECSET 1000/1002/1003
// and 1006.
type MouseMode struct {
	Click  bool // 1000: press and release only
	Drag   bool // 1002: also motion while a button is held
	Motion bool // 1003: also motion with no button held
	SGR    bool // 1006: the extended encoding
}

// Enabled reports whether the program wants mouse reports at all.
func (m MouseMode) Enabled() bool { return m.Click || m.Drag || m.Motion }

// legacyMax is the highest coordinate the original encoding can carry.
// It adds 32 to each value and writes it as one byte, so 223 is the
// last column that fits.
const legacyMax = 223 - 1

// EncodeMouse appends a mouse report and returns the extended buffer, or
// nil when this event should not be reported.
//
// Two encodings are in use. The original one packs the button and the
// coordinates into single bytes offset by 32, which cannot describe a
// terminal wider than 223 columns. SGR mode (1006) writes them as
// decimal numbers and distinguishes press from release, which is why
// every modern program turns it on.
func EncodeMouse(e MouseEvent, m MouseMode, dst []byte) []byte {
	if !m.Enabled() {
		return nil
	}
	switch e.Kind {
	case MouseMove:
		// Motion is only reported when asked for: 1002 while a button is
		// held, 1003 always.
		held := e.Button != MouseNone
		if !(m.Motion || (m.Drag && held)) {
			return nil
		}
	case MouseRelease:
		// The legacy encoding cannot say which button was released, so
		// wheel releases are not a thing and are dropped.
		if isWheel(e.Button) {
			return nil
		}
	}

	cb := buttonCode(e)
	if m.SGR {
		dst = append(dst, 0x1b, '[', '<')
		dst = strconv.AppendInt(dst, int64(cb), 10)
		dst = append(dst, ';')
		dst = strconv.AppendInt(dst, int64(e.Col+1), 10)
		dst = append(dst, ';')
		dst = strconv.AppendInt(dst, int64(e.Row+1), 10)
		final := byte('M')
		if e.Kind == MouseRelease {
			final = 'm'
		}
		return append(dst, final)
	}

	// Legacy encoding: a release is button 3, whichever button it was.
	if e.Kind == MouseRelease {
		cb = 3 | (cb &^ 0x03)
	}
	if e.Col > legacyMax || e.Row > legacyMax {
		// Beyond what one byte can carry. Reporting a wrapped-around
		// coordinate would make the program act on the wrong cell.
		return nil
	}
	return append(dst, 0x1b, '[', 'M',
		byte(cb+32), byte(e.Col+33), byte(e.Row+33))
}

// buttonCode builds the button byte: the low two bits select the button,
// bit 5 marks motion, bit 6 marks a wheel, and bits 2 to 4 carry the
// modifiers.
func buttonCode(e MouseEvent) int {
	var cb int
	switch e.Button {
	case MouseLeft:
		cb = 0
	case MouseMiddle:
		cb = 1
	case MouseRight:
		cb = 2
	case MouseWheelUp:
		cb = 64
	case MouseWheelDown:
		cb = 65
	default:
		cb = 3 // no button
	}
	if e.Kind == MouseMove {
		cb |= 32
	}
	if e.Mods&ModShift != 0 {
		cb |= 4
	}
	if e.Mods&ModAlt != 0 {
		cb |= 8
	}
	if e.Mods&ModCtrl != 0 {
		cb |= 16
	}
	return cb
}

// IsWheel reports whether the button is a wheel notch. A notch arrives
// as a press with no release, so anything tracking a held button has to
// leave it out.
func (b MouseButton) IsWheel() bool {
	return b == MouseWheelUp || b == MouseWheelDown
}

func isWheel(b MouseButton) bool { return b.IsWheel() }
