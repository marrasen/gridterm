package term

import (
	"bytes"
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
