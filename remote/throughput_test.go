package remote

import (
	"context"
	"io"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/marrasen/gridterm/internal/sshtest"
)

// How fast a shell's output can be read over one connection.
//
// This is the other half of what somebody waits out after stopping a
// remote find(1): the pane can swallow fifteen megabytes a second, so
// if stopping one takes seconds the time is being spent getting the
// bytes here rather than drawing them.
func BenchmarkReadAShellFlood(b *testing.B) {
	s := sshtest.New(b)
	conn, err := Connect(context.Background(), benchConfig(b, s))
	if err != nil {
		b.Fatalf("connect: %v", err)
	}
	defer func() { _ = conn.Close() }()

	b.SetBytes(int64(sshtest.FloodSize))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		sh, err := conn.Shell(ShellConfig{Cols: 80, Rows: 24})
		if err != nil {
			b.Fatalf("shell: %v", err)
		}
		b.StartTimer()

		if _, err := sh.Write([]byte("flood\n")); err != nil {
			b.Fatalf("write: %v", err)
		}
		buf := make([]byte, 64*1024)
		read, started := 0, time.Now()
		for read < sshtest.FloodSize {
			n, err := sh.Read(buf)
			read += n
			if err != nil {
				if err == io.EOF {
					break
				}
				b.Fatalf("read: %v", err)
			}
			if time.Since(started) > 60*time.Second {
				b.Fatalf("read %d of %d bytes and gave up", read, sshtest.FloodSize)
			}
		}
		b.StopTimer()
		_ = sh.Close()
		b.StartTimer()
	}
}

// benchConfig is testConfig for a benchmark, which cannot use the
// helpers that take a *testing.T.
func benchConfig(b *testing.B, s *sshtest.Server) Config {
	b.Helper()
	host, port := s.Host()
	return Config{
		Host: host, Port: port, User: "tester",
		Ask:             newTestAsk(),
		HostKeyCallback: ssh.FixedHostKey(s.HostKey()),
		NoAgent:         true,
		NoIdentities:    true,
	}
}
