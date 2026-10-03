package term

import (
	"bytes"
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"
)

// newSyncer is a syncer that keeps what it hands the emulator, each
// hand-over apart.
func newSyncer() (*syncer, *[][]byte) {
	var got [][]byte
	return &syncer{write: func(b []byte) { got = append(got, append([]byte(nil), b...)) }}, &got
}

// An update reaches the emulator whole, markers and all, in one
// hand-over, however the reads cut it: here into single bytes.
func TestAnUpdateReachesTheScreenWhole(t *testing.T) {
	s, got := newSyncer()
	stream := []byte("before\x1b[?2026hframe one\x1b[?2026lafter")
	for i := range stream {
		s.feed(stream[i : i+1])
	}
	var frame []byte
	for _, w := range *got {
		if bytes.Contains(w, []byte("frame")) {
			if frame != nil {
				t.Fatalf("the update was handed over in parts: %q", *got)
			}
			frame = w
		}
	}
	if want := "\x1b[?2026hframe one\x1b[?2026l"; string(frame) != want {
		t.Fatalf("the update was handed over as %q, want %q", frame, want)
	}
	if all := bytes.Join(*got, nil); !bytes.Equal(all, stream) {
		t.Fatalf("the emulator was handed %q, want %q", all, stream)
	}
}

// An update's end and the next one's start in the same read: the
// finished one goes over, and the next is held.
func TestTheNextUpdateWaitsInTheSameRead(t *testing.T) {
	s, got := newSyncer()
	s.feed([]byte("\x1b[?2026hone"))
	if len(*got) != 0 {
		t.Fatalf("a part of an update was handed over: %q", *got)
	}
	wrote, began := s.feed([]byte("\x1b[?2026l\x1b[?2026htwo, half"))
	if !wrote || began == 0 {
		t.Fatalf("the read reported wrote %v, a new update %d", wrote, began)
	}
	if len(*got) != 1 || !bytes.Equal((*got)[0], []byte("\x1b[?2026hone\x1b[?2026l")) {
		t.Fatalf("handed over %q, want the first update alone", *got)
	}
}

// An update held too long is handed over as it stands, once, and the
// timer of an update that already ended does nothing.
func TestALongUpdateIsHandedOverOnTime(t *testing.T) {
	s, got := newSyncer()
	_, first := s.feed([]byte("\x1b[?2026hdone\x1b[?2026l"))
	if s.expire(first) {
		t.Fatal("the timer of an update that ended handed something over")
	}
	_, second := s.feed([]byte("\x1b[?2026hstuck"))
	if !s.expire(second) {
		t.Fatal("an update held past its time stayed held")
	}
	if last := (*got)[len(*got)-1]; !bytes.Equal(last, []byte("\x1b[?2026hstuck")) {
		t.Fatalf("handed over %q, want the update as it stood", last)
	}
	s.feed([]byte(" and more"))
	if last := (*got)[len(*got)-1]; !bytes.Equal(last, []byte(" and more")) {
		t.Fatalf("after the time ran out, %q was handed over, want the bytes as they came", last)
	}
}

// A program that ends part way through an update leaves it to be shown.
func TestAnUpdateCutShortByTheEndIsShown(t *testing.T) {
	s, got := newSyncer()
	s.feed([]byte("\x1b[?2026hlast words\x1b[?20"))
	if !s.flush() {
		t.Fatal("the end handed nothing over")
	}
	if all := bytes.Join(*got, nil); !bytes.Equal(all, []byte("\x1b[?2026hlast words\x1b[?20")) {
		t.Fatalf("handed over %q", all)
	}
}

// feedAll feeds stream to s cut into chunks of size bytes, the last
// perhaps shorter.
func feedAll(s *syncer, stream []byte, size int) {
	for len(stream) > 0 {
		n := min(size, len(stream))
		s.feed(stream[:n])
		stream = stream[n:]
	}
}

// The bytes of a marker inside a string are part of the string. tmux
// passes a sequence through to the outer terminal inside a DCS, with
// each ESC doubled, and the terminal holds nothing for it.
func TestAMarkerInsideAStringHoldsNothing(t *testing.T) {
	for _, stream := range []string{
		"\x1bPtmux;\x1b\x1b[?2026h\x1b\\after",
		"\x1b]0;\x1b[?2026h title\x07after",
		"\x1b]0;\x1b[?2026h title\x1b\\after",
		"\x1b_\x1b[?2026h\x1b\\after",
		"\x1b^\x1b[?2026h\x1b\\after",
		"\x1bX\x1b[?2026h\x1b\\after",
		"\x1bPtmux;\x1b\x1b]8;;x\x1b\x1b\\\x1b[?2026h\x1b\\after",
	} {
		for _, size := range []int{1, 3, len(stream)} {
			s, got := newSyncer()
			feedAll(s, []byte(stream), size)
			if s.on {
				t.Errorf("%q in chunks of %d: an update is held", stream, size)
			}
			if all := bytes.Join(*got, nil); string(all) != stream {
				t.Errorf("%q in chunks of %d: handed over %q", stream, size, all)
			}
		}
	}
}

// A string ended, the stream is scanned for markers again.
func TestAMarkerAfterAStringIsSeen(t *testing.T) {
	stream := "\x1bPtmux;\x1b\x1b[?2026h\x1b\\\x1b[?2026hframe"
	for _, size := range []int{1, 2, len(stream)} {
		s, _ := newSyncer()
		feedAll(s, []byte(stream), size)
		if !s.on {
			t.Errorf("in chunks of %d: the update after the string is not held", size)
		}
	}
}

// A marker that sets or resets other modes along with 2026 starts or
// ends an update all the same.
func TestAMarkerAmongOtherModesIsSeen(t *testing.T) {
	for _, c := range []struct{ begin, end string }{
		{"\x1b[?2026;25h", "\x1b[?25;2026l"},
		{"\x1b[?25;2026h", "\x1b[?2026;25l"},
		{"\x1b[?1;2026;7h", "\x1b[?02026l"},
	} {
		stream := "a" + c.begin + "frame" + c.end + "b"
		for _, size := range []int{1, 4, len(stream)} {
			s, got := newSyncer()
			feedAll(s, []byte(stream), size)
			want := c.begin + "frame" + c.end
			found := false
			for _, w := range *got {
				if string(w) == want {
					found = true
				}
			}
			if !found || s.on {
				t.Errorf("%q in chunks of %d: handed over %q, want %q whole", stream, size, *got, want)
			}
		}
	}
}

// Modes near 2026, other private markers and other finals are no
// marker.
func TestLookalikesAreNoMarker(t *testing.T) {
	for _, stream := range []string{
		"\x1b[?20261h", "\x1b[?202h", "\x1b[2026h", "\x1b[>2026h",
		"\x1b[?2026$p", "\x1b[?2026;1$h", "\x1b[?2026:1h", "\x1b[?2026m",
	} {
		s, got := newSyncer()
		feedAll(s, []byte(stream+"x"), 1)
		if s.on {
			t.Errorf("%q started an update", stream)
		}
		if all := bytes.Join(*got, nil); string(all) != stream+"x" {
			t.Errorf("%q: handed over %q", stream, all)
		}
	}
}

// Whatever the stream, however the reads cut it and whenever an update
// runs out of time, the emulator is handed every byte, once, in order.
func TestEveryByteReachesTheEmulatorInOrder(t *testing.T) {
	pieces := []string{
		"text", "\x1b[?2026h", "\x1b[?2026l", "\x1b[?25;2026h", "\x1b]0;t\x07",
		"\x1bPtmux;\x1b\x1b[?2026h\x1b\\", "\x1b[31m", "\x1b", "\x1b[", "\x1b[?",
		"\x18", "\x1b(B", "\x1b[6n", "\x1b[?2026$p", "é", "\r\n",
	}
	r := rand.New(rand.NewPCG(1, 2))
	for range 2000 {
		var stream []byte
		for range r.IntN(12) {
			stream = append(stream, pieces[r.IntN(len(pieces))]...)
		}
		s, got := newSyncer()
		// Cut into reads, with an update's time running out now and
		// then between them.
		size := 1 + r.IntN(8)
		for rest := stream; len(rest) > 0; {
			n := min(size, len(rest))
			s.feed(rest[:n])
			rest = rest[n:]
			if r.IntN(4) == 0 {
				s.expire(s.update)
			}
		}
		s.flush()
		if all := bytes.Join(*got, nil); !bytes.Equal(all, stream) {
			t.Fatalf("fed %q, handed over %q", stream, all)
		}
	}
}

// A question the emulator answers, asked inside an update, reaches the
// emulator at once, with the update so far: a program waiting for the
// answer would otherwise wait out the update's time on every frame.
// The rest of that update goes over as it comes.
func TestAQuestionInsideAnUpdateIsAnsweredAtOnce(t *testing.T) {
	for _, ask := range []string{
		"\x1b[6n", "\x1b[?6n", "\x1b[5n", "\x1b[c", "\x1b[0c", "\x1b[>c",
		"\x1b[?2026$p", "\x1b[4$p", "\x1b[>q", "\x1b[>0q",
		"\x1b]11;?\x07", "\x1b]10;?\x1b\\", "\x1b]4;1;?\x07",
	} {
		for _, size := range []int{1, 3, 64} {
			s, got := newSyncer()
			feedAll(s, []byte("\x1b[?2026hhalf"+ask), size)
			if s.on {
				t.Errorf("%q in chunks of %d: the update is still held", ask, size)
				continue
			}
			if all := bytes.Join(*got, nil); string(all) != "\x1b[?2026hhalf"+ask {
				t.Errorf("%q in chunks of %d: handed over %q", ask, size, all)
			}
			*got = nil
			feedAll(s, []byte("rest\x1b[?2026l"), size)
			if all := bytes.Join(*got, nil); string(all) != "rest\x1b[?2026l" {
				t.Errorf("%q in chunks of %d: the rest came over as %q", ask, size, all)
			}
		}
	}
}

// Anything else inside an update stays held, questions of a kind the
// emulator leaves unanswered and colours being set among them.
func TestTheRestOfAnUpdateStaysHeld(t *testing.T) {
	for _, say := range []string{
		"\x1b[31m", "\x1b[2J", "\x1b[?25l", "\x1b[1;1H", "\x1b[6 q",
		"\x1b]11;rgb:00/00/00\x07", "\x1b]0;what?\x07", "\x1bP$qm\x1b\\",
		"\x1b]8;;http://x/?q=1\x1b\\",
	} {
		s, got := newSyncer()
		feedAll(s, []byte("\x1b[?2026h"+say), 1)
		if !s.on || len(*got) != 0 {
			t.Errorf("%q: the update went over as %q", say, *got)
		}
	}
}

// An update is held to a megabyte, a frame's size, and the memory an
// unusually large one took is given back once it is handed over.
func TestALargeUpdateIsHandedOverAndForgotten(t *testing.T) {
	s, got := newSyncer()
	s.feed([]byte("\x1b[?2026h"))
	line := bytes.Repeat([]byte("x"), 64<<10)
	for range 24 {
		s.feed(line)
	}
	if s.on || len(*got) == 0 {
		t.Fatalf("an update of 1.5 MiB is still held")
	}
	if c := cap(s.held); c > 1<<20 {
		t.Fatalf("the syncer keeps %d bytes after the update went over", c)
	}
}

// How fast the scan for updates goes through a frame of a true colour
// animation, held whole as one update.
func BenchmarkSyncerHalfBlockFrame(b *testing.B) {
	var f strings.Builder
	f.WriteString("\x1b[?2026h\x1b[H")
	for y := range 75 {
		for x := range 250 {
			fmt.Fprintf(&f, "\x1b[38;2;%d;90;160m\x1b[48;2;40;%d;160m▀", (x+y)%256, (x*y)%256)
		}
		f.WriteString("\r\n")
	}
	f.WriteString("\x1b[?2026l")
	out := []byte(f.String())
	s := &syncer{write: func([]byte) {}}
	b.SetBytes(int64(len(out)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for at := 0; at < len(out); at += readChunk {
			s.feed(out[at:min(at+readChunk, len(out))])
		}
	}
}

// However a stream is cut into reads, the scan finds the same updates
// and questions, and ends in the same place, as a byte at a time.
func TestTheScanIsTheSameHoweverTheReadsAreCut(t *testing.T) {
	parts := []string{"\x1b[?2026h", "\x1b[?2026l", "\x1b[?25;2026h", "\x1b[2026h", "\x1b[?12;2026l",
		"\x1b[38;2;1;2;3m", "\x1b[6n", "\x1b[>c", "\x1b[?2026$p", "\x1b[1;2 q", "\x1b]11;?\x07", "x", "▀", "\x1b", "[", "?", "20", "26", "h"}
	run := func(in []byte, cuts []int) (scanner, bool, uint64, string) {
		var wrote strings.Builder
		s := &syncer{write: func(b []byte) { wrote.Write(b) }}
		from := 0
		for _, to := range append(cuts, len(in)) {
			s.feed(in[from:to])
			from = to
		}
		return s.scan, s.on, s.update, wrote.String() + "|" + string(s.held) + "|" + string(s.carry)
	}
	r := rand.New(rand.NewPCG(3, 4))
	for range 3000 {
		var in []byte
		for range r.IntN(12) {
			in = append(in, parts[r.IntN(len(parts))]...)
		}
		var cuts, each []int
		for k := 1; k < len(in); k++ {
			each = append(each, k)
			if r.IntN(3) == 0 {
				cuts = append(cuts, k)
			}
		}
		ws, won, wu, wout := run(in, each)
		gs, gon, gu, gout := run(in, cuts)
		if gs != ws || gon != won || gu != wu || gout != wout {
			t.Fatalf("%q cut at %v: scan %+v on %v update %d %q;\nbyte by byte %+v on %v update %d %q", in, cuts, gs, gon, gu, gout, ws, won, wu, wout)
		}
	}
}

// Taking a CSI's parameters in a run leaves the scanner where taking
// them a byte at a time does, and finds the same markers.
func TestAParamRunIsTheSameAsSteps(t *testing.T) {
	r := rand.New(rand.NewPCG(5, 6))
	alphabet := []byte("\x1b[?>$;0123456789 :hlnpcqm")
	for range 5000 {
		in := []byte("\x1b[")
		for range r.IntN(16) {
			in = append(in, alphabet[r.IntN(len(alphabet))])
		}
		var slow, fast scanner
		var slowMarks, fastMarks []mark
		for _, c := range in {
			slowMarks = append(slowMarks, slow.step(c))
		}
		for i := 0; i < len(in); i++ {
			if fast.state == inCSI {
				i += fast.paramRun(in[i:])
				if i == len(in) {
					break
				}
			}
			fastMarks = append(fastMarks, fast.step(in[i]))
		}
		slowMarks = slices.DeleteFunc(slowMarks, func(m mark) bool { return m == markNone })
		fastMarks = slices.DeleteFunc(fastMarks, func(m mark) bool { return m == markNone })
		if fast != slow || !slices.Equal(fastMarks, slowMarks) {
			t.Fatalf("%q: in a run %+v %v; a byte at a time %+v %v", in, fast, fastMarks, slow, slowMarks)
		}
	}
}
