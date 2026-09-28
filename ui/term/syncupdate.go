package term

import (
	"bytes"
	"sync"
	"time"
)

// A program that draws whole frames, such as an animation player or a
// full-screen editor, can wrap each one in a synchronized update: it
// sends CSI ? 2026 h before the frame and CSI ? 2026 l after it, and
// the terminal shows nothing of the frame until it is whole. Without
// it the screen is drawn part way through a frame, which tears.
//
// The bytes of an update are held back here, before the emulator, and
// handed over whole once the update ends. The emulator then never holds
// half a frame, whenever the screen is drawn, and anyone watching the
// pane from elsewhere is handed the same whole frames. This is what
// Alacritty does. The markers go through with the update: the emulator
// ignores them, and a kakel watching the pane holds the update back
// in its turn.
//
// An update that runs too long, or grows too large, is handed over as
// it stands, so a program that dies part way through one cannot stop
// the screen for good.

// An update begins with a private mode set, CSI ? ... h, and ends with
// a reset, CSI ? ... l, whose parameters include 2026, whatever else
// they set with it. The same bytes inside a DCS, OSC, APC, PM or SOS
// string are part of the string: tmux passes sequences through to the
// outer terminal that way.
const syncMode = 2026

// syncLimit is the longest an update is held, and syncMost the most
// bytes it is held to. A frame of a full-screen animation at true
// colour is under a megabyte.
const (
	syncLimit = 150 * time.Millisecond
	syncMost  = 8 << 20
)

// carryMost is the longest start of a sequence held back from one read
// to the next. A marker is far shorter; a longer sequence is no marker.
const carryMost = 256

// syncer holds back the bytes of a synchronized update. write hands
// bytes to the emulator; it is called with mu held, so an update and
// what came before it reach the emulator in order.
type syncer struct {
	mu    sync.Mutex
	write func([]byte)
	// on says an update is being held, and held is what it holds.
	on   bool
	held []byte
	// scan is where the stream is in its escape sequences.
	scan scanner
	// carry is the start of a sequence cut in two by the read, while no
	// update is held: it may be the start of a marker.
	carry []byte
	// update counts the updates begun, so a timer set for one leaves
	// the next alone.
	update uint64
}

// feed takes a chunk the program wrote. It reports whether anything
// reached the emulator, for the screen to be drawn again, and the
// update a timer is to be set for, zero when no update began.
func (s *syncer) feed(p []byte) (wrote bool, began uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, scanned := p, 0
	// start is where in data the sequence being scanned began.
	start := -1
	if len(s.carry) > 0 {
		data = append(s.carry, p...)
		scanned, start = len(s.carry), 0
		s.carry = nil
	}
	// Everything before from has reached the emulator or is held.
	from := 0
	for i := scanned; i < len(data); i++ {
		// Text holds no marker, so the scan goes from ESC to ESC.
		if s.scan.state == inText {
			j := bytes.IndexByte(data[i:], esc)
			if j < 0 {
				break
			}
			i += j
		}
		m := s.scan.step(data[i])
		if data[i] == esc && s.scan.state == inEscape {
			start = i
		}
		switch {
		case m == markBegin && !s.on:
			// A marker whose start reached the emulator before, as when
			// an update ran out of time part way through it, begins
			// with what is left of it.
			if start < from {
				start = from
			}
			if start > from {
				s.write(data[from:start])
				wrote = true
			}
			s.on = true
			s.update++
			began = s.update
			s.held = append(s.held[:0], data[start:i+1]...)
			from = i + 1
		case m == markEnd && s.on:
			s.held = append(s.held, data[from:i+1]...)
			s.release()
			wrote = true
			from = i + 1
		}
	}
	rest := data[from:]
	if s.on {
		s.held = append(s.held, rest...)
		if len(s.held) > syncMost {
			s.release()
			wrote = true
		}
		return wrote, began
	}
	keep := 0
	if s.scan.mayMark() && start >= from {
		keep = len(data) - start
	}
	if keep > carryMost {
		s.scan.bad = true
		keep = 0
	}
	if n := len(rest) - keep; n > 0 {
		s.write(rest[:n])
		wrote = true
	}
	if keep > 0 {
		s.carry = append([]byte(nil), rest[len(rest)-keep:]...)
	}
	return wrote, began
}

// expire hands over update n as it stands, if it is still being held.
// It reports whether anything reached the emulator.
func (s *syncer) expire(n uint64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.on || s.update != n {
		return false
	}
	s.release()
	return true
}

// flush hands over whatever is held, as the program ends. It reports
// whether anything reached the emulator.
func (s *syncer) flush() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	held := append(s.held, s.carry...)
	s.held, s.carry, s.on = held[:0], nil, false
	if len(held) == 0 {
		return false
	}
	s.write(held)
	return true
}

// release hands the update over and ends it. It runs with mu held.
func (s *syncer) release() {
	if len(s.held) > 0 {
		s.write(s.held)
	}
	s.held, s.on = s.held[:0], false
}

// scanState is where a scanner is in the escape sequences of the
// stream.
type scanState uint8

const (
	inText scanState = iota
	// inEscape follows an ESC, and inEscapeMiddle an ESC and a byte
	// between it and its last, as in ESC ( B.
	inEscape
	inEscapeMiddle
	inCSI
	// inString is inside a DCS, OSC, APC, PM or SOS string, and
	// inStringEscape follows an ESC inside one.
	inString
	inStringEscape
)

// mark is what a byte ends: nothing, or a marker.
type mark uint8

const (
	markNone mark = iota
	markBegin
	markEnd
)

// Control bytes the scanner acts on.
const (
	bel = 0x07
	can = 0x18
	sub = 0x1a
	esc = 0x1b
)

// scanner follows the escape sequences in a stream a byte at a time,
// far enough to find the markers of an update and the strings they
// could hide in. It keeps nothing of a sequence but what it needs.
type scanner struct {
	state scanState
	// osc says the string is an OSC, which BEL ends as well as ST.
	osc bool

	// Of the CSI being scanned: prefix is its private marker, such as
	// '?', and middle its last intermediate byte, zero for none. params
	// counts its parameter bytes. param is the parameter being read, and
	// digits says it is all digits so far. has says a parameter so far
	// was 2026. bad says it is no marker whatever its last byte.
	prefix, middle byte
	params         int
	param          int
	digits, has    bool
	bad            bool
}

// step takes the next byte of the stream, and reports the marker it
// ends, if any.
func (sc *scanner) step(c byte) mark {
	switch sc.state {
	case inText:
		if c == esc {
			sc.state = inEscape
		}
	case inEscape:
		switch {
		case c == '[':
			sc.state = inCSI
			sc.prefix, sc.middle, sc.params, sc.param = 0, 0, 0, 0
			sc.digits, sc.has, sc.bad = true, false, false
		case c == ']':
			sc.state, sc.osc = inString, true
		case c == 'P' || c == '_' || c == '^' || c == 'X':
			sc.state, sc.osc = inString, false
		case c >= 0x20 && c <= 0x2f:
			sc.state = inEscapeMiddle
		default:
			sc.control(c)
		}
	case inEscapeMiddle:
		if c < 0x20 || c > 0x2f {
			sc.control(c)
		}
	case inCSI:
		return sc.csi(c)
	case inString:
		switch {
		case c == esc:
			sc.state = inStringEscape
		case c == bel && sc.osc, c == can, c == sub:
			sc.state = inText
		}
	case inStringEscape:
		// ST ends the string. Any other byte after an ESC is part of it:
		// tmux doubles each ESC it passes through.
		if c == '\\' || c == can || c == sub {
			sc.state = inText
		} else {
			sc.state = inString
		}
	}
	return markNone
}

// control takes a byte that ends an escape sequence that is no CSI, or
// that interrupts one. ESC starts another; CAN and SUB cancel it; any
// other control byte leaves the sequence where it was.
func (sc *scanner) control(c byte) {
	switch {
	case c == esc:
		sc.state = inEscape
	case c < 0x20 && c != can && c != sub:
	default:
		sc.state = inText
	}
}

// csi takes a byte of a CSI.
func (sc *scanner) csi(c byte) mark {
	switch {
	case c >= '0' && c <= '9' && sc.middle == 0:
		sc.params++
		if sc.param < 1e6 {
			sc.param = sc.param*10 + int(c-'0')
		}
	case c == ';' && sc.middle == 0:
		sc.params++
		sc.endParam()
	case c >= 0x3a && c <= 0x3f && sc.middle == 0:
		// A private marker counts only as the first byte. A colon splits
		// a parameter into parts, which no mode has.
		switch {
		case c == ':':
			sc.digits = false
		case sc.params == 0:
			sc.prefix = c
		default:
			sc.bad = true
		}
		sc.params++
	case c >= 0x20 && c <= 0x2f:
		sc.middle = c
	case c >= 0x40 && c <= 0x7e:
		sc.state = inText
		sc.endParam()
		if sc.bad || sc.prefix != '?' || sc.middle != 0 || !sc.has {
			return markNone
		}
		switch c {
		case 'h':
			return markBegin
		case 'l':
			return markEnd
		}
	case c >= 0x30 && c <= 0x3f:
		// A parameter byte after an intermediate.
		sc.bad = true
	default:
		sc.control(c)
	}
	return markNone
}

// endParam finishes the parameter being read.
func (sc *scanner) endParam() {
	if sc.digits && sc.param == syncMode {
		sc.has = true
	}
	sc.param, sc.digits = 0, true
}

// mayMark reports whether the sequence being scanned could still turn
// out to be a marker.
func (sc *scanner) mayMark() bool {
	switch sc.state {
	case inEscape:
		return true
	case inCSI:
		return !sc.bad && sc.middle == 0 && (sc.prefix == '?' || sc.params == 0)
	}
	return false
}
