package jobs

import (
	"bytes"
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/marrasen/kakel/vfs"
	"github.com/pkg/sftp"
)

// latent is one end of a connection whose writes arrive delay later,
// each on its own: a far server's round trip, which slows each request
// and not how many can be on their way at once.
type latent struct {
	net.Conn
	delay time.Duration
	queue chan latentWrite
	once  sync.Once
}

type latentWrite struct {
	at   time.Time
	data []byte
}

func newLatent(c net.Conn, delay time.Duration) *latent {
	l := &latent{Conn: c, delay: delay, queue: make(chan latentWrite, 4096)}
	go func() {
		for w := range l.queue {
			time.Sleep(time.Until(w.at))
			if _, err := l.Conn.Write(w.data); err != nil {
				return
			}
		}
	}()
	return l
}

func (l *latent) Write(p []byte) (int, error) {
	l.queue <- latentWrite{at: time.Now().Add(l.delay), data: bytes.Clone(p)}
	return len(p), nil
}

func (l *latent) Close() error {
	l.once.Do(func() { close(l.queue) })
	return l.Conn.Close()
}

// slowFar is this machine's disk reached over SFTP with a round trip of
// twice delay.
func slowFar(t *testing.T, delay time.Duration) side {
	t.Helper()
	here, there := net.Pipe()
	server, err := sftp.NewServer(newLatent(there, delay))
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = server.Serve() }()
	t.Cleanup(func() { _ = server.Close() })
	conn := newLatent(here, delay)
	client, err := sftp.NewClientPipe(conn, conn)
	if err != nil {
		t.Fatal(err)
	}
	f := vfs.NewSFTP("slow", client, client, client.Close)
	t.Cleanup(func() { _ = f.Close() })
	dir := t.TempDir()
	return side{fs: f, at: filepath.ToSlash(dir), real: dir}
}

// A copy to or from a far server keeps many requests on their way, so
// it is not held to one round trip per piece: 8 MB over a 20 ms round
// trip, which one 64 KB piece at a time would take 2.6 s for.
func TestACopyOverALongRoundTripIsNotHeldToIt(t *testing.T) {
	if testing.Short() {
		t.Skip("times a copy")
	}
	body := bytes.Repeat([]byte("0123456789abcdef"), 8<<20/16)
	for _, c := range []struct {
		name     string
		from, to func(*testing.T) side
	}{
		{"from the server", func(t *testing.T) side { return slowFar(t, 10*time.Millisecond) }, local},
		{"to the server", local, func(t *testing.T) side { return slowFar(t, 10*time.Millisecond) }},
		{"between two servers", func(t *testing.T) side { return slowFar(t, 10*time.Millisecond) },
			func(t *testing.T) side { return slowFar(t, 10*time.Millisecond) }},
	} {
		t.Run(c.name, func(t *testing.T) {
			from, to := c.from(t), c.to(t)
			if err := os.WriteFile(filepath.Join(from.real, "big.bin"), body, 0o600); err != nil {
				t.Fatal(err)
			}
			began := time.Now()
			j := New(1).Start(t.Context(), Op{Kind: Copy, From: from.fs, At: from.at, Names: []string{"big.bin"}, To: to.fs, Into: to.at}, Options{})
			if err := ends(t, j); err != nil {
				t.Fatal(err)
			}
			took := time.Since(began)
			got, err := os.ReadFile(filepath.Join(to.real, "big.bin"))
			if err != nil || !bytes.Equal(got, body) {
				t.Fatalf("the copy holds %d bytes, %v", len(got), err)
			}
			if took > 1200*time.Millisecond {
				t.Fatalf("8 MB took %v over a 20 ms round trip", took)
			}
			t.Logf("8 MB in %v", took)
		})
	}
}

// Cancelled while a far server's file is on its way, in either
// direction, the copy stops at once and leaves no part file behind.
func TestACopyOverALongRoundTripStopsWhenCancelled(t *testing.T) {
	body := bytes.Repeat([]byte("0123456789abcdef"), 64<<20/16)
	for _, c := range []struct {
		name     string
		from, to func(*testing.T) side
	}{
		{"from the server", func(t *testing.T) side { return slowFar(t, 10*time.Millisecond) }, local},
		{"to the server", local, func(t *testing.T) side { return slowFar(t, 10*time.Millisecond) }},
	} {
		t.Run(c.name, func(t *testing.T) {
			from, to := c.from(t), c.to(t)
			if err := os.WriteFile(filepath.Join(from.real, "big.bin"), body, 0o600); err != nil {
				t.Fatal(err)
			}
			j := New(1).Start(t.Context(), Op{Kind: Copy, From: from.fs, At: from.at, Names: []string{"big.bin"}, To: to.fs, Into: to.at}, Options{})
			for deadline := time.Now().Add(5 * time.Second); j.Progress().BytesDone == 0; {
				if time.Now().After(deadline) {
					t.Fatal("nothing moved")
				}
				time.Sleep(5 * time.Millisecond)
			}
			cancelled := time.Now()
			j.Cancel()
			if err := ends(t, j); !errors.Is(err, context.Canceled) {
				t.Fatalf("cancelled, the job ended with %v", err)
			}
			if took := time.Since(cancelled); took > time.Second {
				t.Fatalf("it took %v to stop", took)
			}
			if p := j.Progress(); p.BytesDone >= p.Bytes {
				t.Fatalf("it copied all %d bytes though cancelled", p.Bytes)
			}
			parts, _ := filepath.Glob(filepath.Join(to.real, "*"+partSuffix))
			if _, err := os.Stat(filepath.Join(to.real, "big.bin")); err == nil || len(parts) != 0 {
				t.Fatalf("left behind: the file %v, parts %v", err == nil, parts)
			}
		})
	}
}
