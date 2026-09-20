package vt

import (
	"bytes"
	"encoding/base64"
)

// longOSC pulls the sequences that carry a picture out of the stream
// before the parser sees them.
//
// The parser keeps a kilobyte of an OSC payload and throws the rest
// away, which is nothing next to a picture. These two are read here
// instead, byte by byte, and everything else reaches the parser as it
// always did.
type longOSC struct {
	// lead is the start of a marker, held back until enough of the
	// next write arrives to say whether it is one.
	lead []byte

	// num is the sequence being read, and nil when none is.
	num []byte

	// body is the payload read so far.
	body []byte

	// over says the payload has outgrown what a picture may be. The
	// rest is read and thrown away, so that it does not print as text.
	over bool

	// esc says an escape turned up inside the payload, which ends it
	// when a backslash follows.
	esc bool
}

// longOSCMarkers are the sequences read here rather than by the
// parser: the one a program sends a picture with, and the one a window
// hands a picture to a window watching it with.
var longOSCMarkers = [][]byte{
	[]byte("\x1b]1337;"),
	[]byte("\x1b]1338;"),
}

// mostLongOSC is the largest payload held, which is the largest
// picture allowed with room for the arguments in front of it.
var mostLongOSC = base64.StdEncoding.EncodedLen(MostImageBytes) + 1024

// how a run of bytes matches the markers.
const (
	markerNo = iota
	markerMaybe
	markerYes
)

// feed puts one write through, handing every byte that is not part of
// a picture sequence to pass and each finished picture sequence to
// take.
func (s *longOSC) feed(p []byte, pass func(byte), take func(num, body []byte)) {
	for _, b := range p {
		switch {
		case s.num == nil:
			s.match(b, pass)
		case s.esc:
			s.esc = false
			if b == '\\' {
				s.finish(take)
			} else {
				// The sequence was abandoned. What was read is not a
				// picture, and the escape starts something else.
				s.drop()
				pass(0x1b)
				s.match(b, pass)
			}
		case b == 0x07:
			s.finish(take)
		case b == 0x1b:
			s.esc = true
		default:
			s.add(b)
		}
	}
}

// match takes one byte while no picture sequence is being read, either
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

// add keeps one byte of the payload, up to the largest picture
// allowed. Past that the bytes are read and dropped, because a payload
// nobody can use still has to be read to find where it ends.
func (s *longOSC) add(b byte) {
	if len(s.body) >= mostLongOSC {
		s.over = true
		return
	}
	s.body = append(s.body, b)
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

// longOSCDone hands a picture sequence to the ordinary OSC handling,
// cut into parameters the way the parser would have cut it.
func (t *Terminal) longOSCDone(num, body []byte) {
	params := make([][]byte, 0, 16)
	params = append(params, num)
	params = append(params, bytes.SplitN(body, []byte(";"), 15)...)
	t.OscDispatch(params, true)
}
