package vt

import (
	"bytes"
	"encoding/base64"
)

// longOSC pulls the sequences that carry an image or a clipboard's
// worth of text out of the stream before the parser sees them.
//
// The parser keeps a kilobyte of an OSC payload and throws the rest
// away, which is nothing next to an image, and cuts a copy short. These
// are read here instead, byte by byte, and everything else reaches the
// parser as it always did.
type longOSC struct {
	// lead is the start of a marker, held back until enough of the
	// next write arrives to say whether it is one.
	lead []byte

	// num is the sequence being read, and nil when none is.
	num []byte

	// body is the payload read so far.
	body []byte

	// over says the payload has outgrown what an image may be. The
	// rest is read and thrown away, so that it does not print as text.
	over bool

	// esc says an escape turned up inside the payload, which ends it
	// when a backslash follows.
	esc bool
	// one holds a single byte handed on by itself.
	one [1]byte
}

// longOSCMarkers are the sequences read here rather than by the
// parser: the one a program sends an image with, the one a window
// hands an image to a window watching it with, and the one a program
// copies text with, which an editor over SSH sends a whole file in.
var longOSCMarkers = [][]byte{
	[]byte("\x1b]1337;"),
	[]byte("\x1b]1338;"),
	[]byte("\x1b]52;"),
}

// mostLongOSC is the largest payload held, which is the largest
// image allowed with room for the arguments in front of it.
var mostLongOSC = base64.StdEncoding.EncodedLen(MostImageBytes) + 1024

// how a run of bytes matches the markers.
const (
	markerNo = iota
	markerMaybe
	markerYes
)

// feed puts one write through, handing every run of bytes that is not
// part of an image sequence to pass and each finished image sequence to
// take.
func (s *longOSC) feed(p []byte, pass func([]byte), take func(num, body []byte)) {
	one := func(b byte) {
		s.one[0] = b
		pass(s.one[:])
	}
	for i := 0; i < len(p); {
		if s.num == nil && len(s.lead) == 0 {
			// Nearly every byte: nothing held back, and only ESC ] can
			// begin a marker. Everything before one goes through at once.
			j := runBefore(p, i)
			if j > i {
				pass(p[i:j])
			}
			if i = j; i < len(p) {
				s.match(p[i], one)
				i++
			}
			continue
		}
		if s.num != nil && !s.esc {
			// The payload, up to a byte that may end it.
			j := i
			for j < len(p) && p[j] != 0x07 && p[j] != 0x1b && p[j] != 0x18 && p[j] != 0x1a {
				j++
			}
			s.add(p[i:j])
			if i = j; i == len(p) {
				break
			}
		}
		b := p[i]
		i++
		switch {
		case s.num == nil:
			s.match(b, one)
		case s.esc:
			s.esc = false
			if b == '\\' {
				s.finish(take)
			} else {
				// The sequence was abandoned. What was read is not a
				// image, and the escape starts something else.
				s.drop()
				one(0x1b)
				s.match(b, one)
			}
		case b == 0x07:
			s.finish(take)
		case b == 0x18, b == 0x1a:
			// CAN and SUB abandon the sequence, as the parser does, and
			// what follows is output again.
			s.drop()
		case b == 0x1b:
			s.esc = true
		}
	}
}

// runBefore returns where, from i on, the first escape that may begin a
// marker is in p, or len(p) where none does. An escape followed by
// anything but ']' begins none. One at the very end may.
func runBefore(p []byte, i int) int {
	for {
		k := bytes.IndexByte(p[i:], 0x1b)
		if k < 0 {
			return len(p)
		}
		j := i + k
		switch {
		case j+1 == len(p), p[j+1] == ']':
			return j
		case p[j+1] == 0x1b:
			i = j + 1
		default:
			i = j + 2
		}
	}
}

// match takes one byte while no image sequence is being read, either
// growing the start of a marker or letting the byte through.
func (s *longOSC) match(b byte, pass func(byte)) {
	s.lead = append(s.lead, b)
	for {
		switch matchMarker(s.lead) {
		case markerYes:
			s.num = append([]byte(nil), s.lead[2:len(s.lead)-1]...)
			s.lead = s.lead[:0]
			return
		case markerMaybe:
			return
		}
		// Not a marker. Everything held back but the byte that just
		// failed goes to the parser, and that byte may start one of
		// its own: an escape is the only byte a marker begins with, so
		// nothing earlier can.
		last := s.lead[len(s.lead)-1]
		for _, held := range s.lead[:len(s.lead)-1] {
			pass(held)
		}
		s.lead = append(s.lead[:0], last)
		if last != 0x1b {
			pass(last)
			s.lead = s.lead[:0]
			return
		}
	}
}

// matchMarker says whether a run of bytes is a marker, the start of
// one, or neither.
func matchMarker(lead []byte) int {
	out := markerNo
	for _, m := range longOSCMarkers {
		if len(lead) > len(m) || !bytes.Equal(lead, m[:len(lead)]) {
			continue
		}
		if len(lead) == len(m) {
			return markerYes
		}
		out = markerMaybe
	}
	return out
}

// add keeps a run of the payload, up to the largest image
// allowed. Past that the bytes are read and dropped, because a payload
// nobody can use still has to be read to find where it ends.
func (s *longOSC) add(run []byte) {
	if len(run) == 0 {
		return
	}
	if room := mostLongOSC - len(s.body); len(run) > room {
		s.over = true
		run = run[:max(room, 0)]
	}
	s.body = append(s.body, run...)
}

// finish hands over a payload that has reached its end.
func (s *longOSC) finish(take func(num, body []byte)) {
	if !s.over {
		take(s.num, s.body)
	}
	s.drop()
}

// drop forgets the payload being read.
func (s *longOSC) drop() {
	s.num, s.body, s.over, s.esc = nil, nil, false, false
}

// longOSCDone hands an image sequence to the ordinary OSC handling,
// cut into parameters the way the parser would have cut it.
func (t *Terminal) longOSCDone(num, body []byte) {
	params := make([][]byte, 0, 16)
	params = append(params, num)
	params = append(params, bytes.SplitN(body, []byte(";"), 15)...)
	t.OscDispatch(params, true)
}
