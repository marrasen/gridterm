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

// syncBegin and syncEnd start and end a synchronized update.
var (
	syncBegin = []byte("\x1b[?2026h")
	syncEnd   = []byte("\x1b[?2026l")
)

// syncLimit is the longest an update is held, and syncMost the most
// bytes it is held to. A frame of a full-screen animation at true
// colour is under a megabyte.
const (
	syncLimit = 150 * time.Millisecond
	syncMost  = 8 << 20
)

// syncer holds back the bytes of a synchronized update. write hands
// bytes to the emulator; it is called with mu held, so an update and
// what came before it reach the emulator in order.
type syncer struct {
	mu    sync.Mutex
	write func([]byte)
	// on says an update is being held, and held is what it holds.
	on   bool
	held []byte
	// carry is the end of the last chunk, which may be the start of a
	// marker cut in two by the read.
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
	data := p
	if len(s.carry) > 0 {
		data = append(s.carry, p...)
		s.carry = nil
	}
	for len(data) > 0 {
		if !s.on {
			i := bytes.Index(data, syncBegin)
			if i < 0 {
				keep := markerStart(data, syncBegin)
				if n := len(data) - keep; n > 0 {
					s.write(data[:n])
					wrote = true
				}
				s.carry = append([]byte(nil), data[len(data)-keep:]...)
				return wrote, began
			}
			if i > 0 {
				s.write(data[:i])
				wrote = true
			}
			s.on = true
			s.update++
			began = s.update
			s.held = append(s.held[:0], syncBegin...)
			data = data[i+len(syncBegin):]
			continue
		}
		i := bytes.Index(data, syncEnd)
		if i < 0 {
			keep := markerStart(data, syncEnd)
			s.held = append(s.held, data[:len(data)-keep]...)
			s.carry = append([]byte(nil), data[len(data)-keep:]...)
			if len(s.held) > syncMost {
				s.release()
				wrote = true
			}
			return wrote, began
		}
		s.held = append(s.held, data[:i+len(syncEnd)]...)
		s.release()
		wrote = true
		data = data[i+len(syncEnd):]
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

// markerStart is how many bytes at the end of data could begin marker:
// the longest end of data that is the start of it.
func markerStart(data, marker []byte) int {
	for n := min(len(data), len(marker)-1); n > 0; n-- {
		if bytes.Equal(data[len(data)-n:], marker[:n]) {
			return n
		}
	}
	return 0
}
