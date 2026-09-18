package term

import (
	"image/color"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/ui"
)

// flood is a session that hands over one lot of output as fast as it is
// read, and then ends.
type flood struct {
	mu   sync.Mutex
	left []byte
	done chan struct{}
	once sync.Once
}

func newFlood(out []byte) *flood {
	return &flood{left: out, done: make(chan struct{})}
}

func (f *flood) Read(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.left) == 0 {
		f.once.Do(func() { close(f.done) })
		return 0, io.EOF
	}
	n := copy(p, f.left)
	f.left = f.left[n:]
	return n, nil
}

func (f *flood) Write(p []byte) (int, error) { return len(p), nil }
func (f *flood) Resize(int, int) error       { return nil }
func (f *flood) Wait() error                 { <-f.done; return nil }
func (f *flood) Close() error                { f.once.Do(func() { close(f.done) }); return nil }

// findOutput is what a command that floods a terminal looks like.
func findOutput(bytes int) []byte {
	const line = "/usr/lib/x86_64-linux-gnu/perl-base/unicore/lib/Gc/Cntrl.pl\r\n"
	var b strings.Builder
	for b.Len() < bytes {
		b.WriteString(line)
	}
	return []byte(b.String())
}

// How long a pane takes to swallow what is already in flight.
//
// This is what the user waits out after stopping a remote find(1): the
// far end has stopped, and the pane is still working through what was
// sent before it did. It is the whole path -- the read, the emulator,
// the lock the drawing shares -- rather than the emulator alone.
func BenchmarkPaneSwallowsAFlood(b *testing.B) {
	out := findOutput(4 << 20)
	b.SetBytes(int64(len(out)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		f := newFlood(out)
		t, err := New(Config{Session: f, Size: ui.Size{Cols: 80, Rows: 24}})
		if err != nil {
			b.Fatal(err)
		}
		t.Layout(ui.Size{Cols: 80, Rows: 24})
		b.StartTimer()

		// Read to the end, which is when everything has been parsed.
		waited := time.Now()
		for !t.Exited() {
			if time.Since(waited) > 60*time.Second {
				b.Fatal("the pane never caught up")
			}
			time.Sleep(time.Millisecond)
		}
		b.StopTimer()
		_ = t.Close()
		b.StartTimer()
	}
}

// And the same with the window drawing it every frame, which is what
// really happens: the drawing and the reading share a lock.
func BenchmarkPaneSwallowsAFloodWhileDrawn(b *testing.B) {
	out := findOutput(4 << 20)
	b.SetBytes(int64(len(out)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		f := newFlood(out)
		t, err := New(Config{Session: f, Size: ui.Size{Cols: 80, Rows: 24}})
		if err != nil {
			b.Fatal(err)
		}
		t.Layout(ui.Size{Cols: 80, Rows: 24})
		g := grid.New(80, 24, color.RGBA{}, color.RGBA{})
		stop := make(chan struct{})
		var drawing sync.WaitGroup
		drawing.Go(func() {
			tick := time.NewTicker(16 * time.Millisecond)
			defer tick.Stop()
			for {
				select {
				case <-stop:
					return
				case <-tick.C:
					t.Draw(g.View())
				}
			}
		})
		b.StartTimer()

		waited := time.Now()
		for !t.Exited() {
			if time.Since(waited) > 60*time.Second {
				b.Fatal("the pane never caught up")
			}
			time.Sleep(time.Millisecond)
		}
		b.StopTimer()
		close(stop)
		drawing.Wait()
		_ = t.Close()
		b.StartTimer()
	}
}
