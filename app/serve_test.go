package app

import (
	"os"
	"testing"
	"time"

	"github.com/marrasen/kakel/internal/sshtest"
)

// TestServeForAManualCheck runs kakel's test SSH server for ten
// minutes, to connect the window to by hand, and writes its address to
// the file KAKEL_SSHTEST names. Its password is hunter2.
func TestServeForAManualCheck(t *testing.T) {
	path := os.Getenv("KAKEL_SSHTEST")
	if path == "" {
		t.Skip("set KAKEL_SSHTEST to a file for the server's address")
	}
	s := sshtest.New(t)
	if err := os.WriteFile(path, []byte(s.Addr()), 0o600); err != nil {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Minute)
}
