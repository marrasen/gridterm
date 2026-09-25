package main

import (
	"os"
	"testing"
	"time"

	"github.com/marrasen/gridterm/internal/sshtest"
)

// TestServeForAManualCheck runs gridterm's test SSH server for ten
// minutes, to connect the window to by hand, and writes its address to
// the file GUNIMTERM_SSHTEST names. Its password is hunter2.
func TestServeForAManualCheck(t *testing.T) {
	path := os.Getenv("GUNIMTERM_SSHTEST")
	if path == "" {
		t.Skip("set GUNIMTERM_SSHTEST to a file for the server's address")
	}
	s := sshtest.New(t)
	if err := os.WriteFile(path, []byte(s.Addr()), 0o600); err != nil {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Minute)
}
