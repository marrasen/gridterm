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
