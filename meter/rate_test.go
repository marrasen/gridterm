package meter

import (
	"testing"
	"time"
)

func TestRateNeedsTwoSamples(t *testing.T) {
	m := New()
	var r Rate

	m.Moved(1000, 0, at)
	if in, out := r.Sample(m, at); in != 0 || out != 0 {
		t.Fatalf("the first sample reported %d/%d, want nothing to compare against", in, out)
	}
}

func TestRateIsBytesPerSecond(t *testing.T) {
	m := New()
	var r Rate
	r.Sample(m, at)

	m.Moved(4096, 512, at.Add(time.Second))
	in, out := r.Sample(m, at.Add(time.Second))
	if in != 4096 || out != 512 {
		t.Fatalf("rate = %d in, %d out, want 4096 and 512 per second", in, out)
	}

	// Over two seconds, half the speed.
	m.Moved(4096, 0, at.Add(3*time.Second))
	in, _ = r.Sample(m, at.Add(3*time.Second))
	if in != 2048 {
		t.Fatalf("rate = %d, want 2048 per second over two seconds", in)
	}
}

// Sampled every frame, the answer stays put until there is something new
// to say. A number that changes every frame is a row that dirties itself
// every frame.
func TestRateHoldsItsAnswerBetweenWindows(t *testing.T) {
	m := New()
	var r Rate
	r.Sample(m, at)
	m.Moved(1000, 0, at.Add(time.Second))
	want, _ := r.Sample(m, at.Add(time.Second))

	for i := 1; i < 60; i++ {
		when := at.Add(time.Second + time.Duration(i)*16*time.Millisecond)
		m.Moved(10, 0, when)
		if got, _ := r.Sample(m, when); got != want {
			t.Fatalf("the rate moved to %d within a window, want it to stay at %d", got, want)
		}
	}
}

// A connection that stops shows nothing rather than a stale speed.
func TestRateFallsToNothingWhenTheBytesStop(t *testing.T) {
	m := New()
	var r Rate
	r.Sample(m, at)
	m.Moved(1000, 0, at.Add(time.Second))
	if in, _ := r.Sample(m, at.Add(time.Second)); in == 0 {
		t.Fatal("the rate was nothing while bytes were moving")
	}

	if in, out := r.Sample(m, at.Add(5*time.Second)); in != 0 || out != 0 {
		t.Fatalf("rate = %d/%d after the bytes stopped, want nothing", in, out)
	}
}

func TestBytesReadsTheWayAPersonReadsIt(t *testing.T) {
	cases := map[uint64]string{
		0:            "0 B",
		1:            "1 B",
		1023:         "1023 B",
		1024:         "1.0 kB",
		1536:         "1.5 kB",
		10 * 1024:    "10 kB",
		1024 * 1024:  "1.0 MB",
		12 * 1 << 20: "12 MB",
		3 * 1 << 30:  "3.0 GB",
		1 << 40:      "1.0 TB",
		1 << 50:      "1.0 PB",
		1 << 59:      "512 PB",
		// Past the last unit there is. Without the cap this indexes off
		// the end of the table and takes the window with it.
		1 << 60: "1024 PB",
		1 << 63: "8192 PB",
	}
	for n, want := range cases {
		if got := Bytes(n); got != want {
			t.Errorf("Bytes(%d) = %q, want %q", n, got, want)
		}
	}
}

// A row showing nothing happening should show nothing, not "0 B/s".
func TestSpeedSaysNothingWhenThereIsNone(t *testing.T) {
	if got := Speed(0); got != "" {
		t.Fatalf("Speed(0) = %q, want nothing", got)
	}
	if got := Speed(2048); got != "2.0 kB/s" {
		t.Fatalf("Speed(2048) = %q, want 2.0 kB/s", got)
	}
}

// A clock that goes backwards must not produce a speed measured over a
// negative moment. The answer already known is the right one to keep.
func TestRateWithAClockThatWentBackwards(t *testing.T) {
	m := New()
	var r Rate
	r.Sample(m, at)
	m.Moved(2048, 0, at.Add(time.Second))
	want, _ := r.Sample(m, at.Add(time.Second))
	if want == 0 {
		t.Fatal("nothing was measured to begin with")
	}

	m.Moved(1024, 0, at)
	if got, _ := r.Sample(m, at.Add(-time.Hour)); got != want {
		t.Fatalf("rate = %d after the clock went back, want the %d already known", got, want)
	}
}

// A rate remembers the last few seconds, oldest first, with nothing in
// front of the first second it saw.
func TestRateRemembersTheRun(t *testing.T) {
	m := New()
	var r Rate
	at := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	r.Sample(m, at)
	if got := r.Past(); len(got) != 0 {
		t.Fatalf("it remembers %v before anything has gone past", got)
	}

	for i := 1; i <= 3; i++ {
		m.Moved(i*1000, 0, at)
		at = at.Add(RateWindow)
		r.Sample(m, at)
	}
	got := r.Past()
	if len(got) != 3 {
		t.Fatalf("it remembers %v, want three seconds", got)
	}
	if !(got[0] < got[1] && got[1] < got[2]) {
		t.Fatalf("it remembers %v, want them oldest first", got)
	}

	// Past the length it holds, the oldest falls off the front.
	for i := 0; i < Samples*2; i++ {
		at = at.Add(RateWindow)
		r.Sample(m, at)
	}
	if got := len(r.Past()); got != Samples {
		t.Fatalf("it remembers %d seconds, want %d", got, Samples)
	}
	// Nothing has moved for a while, so the run is quiet.
	for i, speed := range r.Past() {
		if speed != 0 {
			t.Fatalf("second %d of a quiet run is %d", i, speed)
		}
	}
}

// Bars scale to the fastest second in the run, so the shape shows
// whatever the speeds happen to be.
func TestBarsScaleToTheRun(t *testing.T) {
	got := Bars([]uint64{0, 50, 100}, 15)
	if len(got) != 3 {
		t.Fatalf("%v", got)
	}
	if got[0] != 0 {
		t.Errorf("a quiet second is %d, want nothing", got[0])
	}
	if got[2] != 15 {
		t.Errorf("the fastest second is %d, want the full height", got[2])
	}
	if got[1] <= got[0] || got[1] >= got[2] {
		t.Errorf("the middle second is %d, want it between them", got[1])
	}

	// The same shape at a different scale gives the same bars.
	slow := Bars([]uint64{0, 50, 100}, 15)
	fast := Bars([]uint64{0, 50_000, 100_000}, 15)
	for i := range slow {
		if slow[i] != fast[i] {
			t.Fatalf("the same shape gives %v and %v", slow, fast)
		}
	}

	// A run with nothing in it is flat rather than full.
	for i, h := range Bars([]uint64{0, 0, 0}, 15) {
		if h != 0 {
			t.Fatalf("bar %d of a quiet run is %d", i, h)
		}
	}
	if got := Bars([]uint64{1}, 0); got != nil {
		t.Errorf("bars with no height = %v", got)
	}
}

// A second that moved anything at all is a bar rather than a gap.
func TestASecondThatMovedAnythingIsABar(t *testing.T) {
	got := Bars([]uint64{1, 1_000_000}, 15)
	if got[0] < 1 {
		t.Fatalf("a second that moved a byte is %d, want at least one", got[0])
	}
}

// A run nobody was measuring is forgotten rather than filled in.
//
// The sidebar can be hidden for minutes. One average over the whole gap
// is not the last second of a run, and the seconds before it were never
// measured at all.
func TestARunNobodyWatchedIsForgotten(t *testing.T) {
	m := New()
	var r Rate
	at := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	r.Sample(m, at)
	for i := 1; i <= 4; i++ {
		m.Moved(i*1000, 0, at)
		at = at.Add(RateWindow)
		r.Sample(m, at)
	}
	if got := len(r.Past()); got != 4 {
		t.Fatalf("it remembers %d seconds before the gap", got)
	}

	// Nobody looks for five minutes, and then a megabyte arrives.
	m.Moved(1<<20, 0, at)
	at = at.Add(5 * time.Minute)
	in, _ := r.Sample(m, at)
	if in == 0 {
		t.Fatal("the speed over the gap is not reported at all")
	}
	if got := r.Past(); len(got) != 0 {
		t.Fatalf("it still remembers %v, want the run forgotten", got)
	}

	// And it starts again from there.
	m.Moved(2048, 0, at)
	at = at.Add(RateWindow)
	r.Sample(m, at)
	if got := len(r.Past()); got != 1 {
		t.Fatalf("it remembers %d seconds after starting again", got)
	}
}

// The run handed out is a copy: the array behind it is written again
// every second.
func TestPastIsACopy(t *testing.T) {
	m := New()
	var r Rate
	at := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	r.Sample(m, at)
	m.Moved(1000, 0, at)
	at = at.Add(RateWindow)
	r.Sample(m, at)

	got := r.Past()
	if len(got) != 1 {
		t.Fatalf("it remembers %v", got)
	}
	was := got[0]
	got[0] = 999999
	if again := r.Past(); again[0] != was {
		t.Fatalf("writing to what Past handed back changed the run to %v", again)
	}
}
