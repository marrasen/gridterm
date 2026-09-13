package input

import "testing"

func BenchmarkEncodeText(b *testing.B) {
	e := Event{Kind: Text, Rune: 'a', NormalText: true}
	buf := make([]byte, 0, 8)
	b.ReportAllocs()
	for b.Loop() {
		buf = Encode(e, buf[:0])
	}
}

func BenchmarkEncodeCtrlKey(b *testing.B) {
	e := Event{Kind: KeyPress, Key: KeyC, Mods: ModCtrl}
	buf := make([]byte, 0, 8)
	b.ReportAllocs()
	for b.Loop() {
		buf = Encode(e, buf[:0])
	}
}

func BenchmarkEncodeModifiedArrow(b *testing.B) {
	e := Event{Kind: KeyPress, Key: KeyUp, Mods: ModCtrl | ModShift}
	buf := make([]byte, 0, 8)
	b.ReportAllocs()
	for b.Loop() {
		buf = Encode(e, buf[:0])
	}
}
