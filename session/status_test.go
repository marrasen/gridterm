package session

import (
	"errors"
	"os/exec"
	"runtime"
	"testing"
)

// A program here killed by a signal counts as 128 plus the signal, as a
// shell counts it; one that exits says its code; nil is 0.
func TestAStatusIsCountedAsAShellCountsIt(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no signals to be killed by")
	}
	err := exec.Command("sh", "-c", "kill -9 $$").Run()
	if status, ok := Status(err); !ok || status != 137 {
		t.Fatalf("killed, it reads as %d, %v", status, ok)
	}
	err = exec.Command("sh", "-c", "exit 3").Run()
	if status, ok := Status(err); !ok || status != 3 {
		t.Fatalf("exiting 3, it reads as %d, %v", status, ok)
	}
	if status, ok := Status(nil); !ok || status != 0 {
		t.Fatalf("nil reads as %d, %v", status, ok)
	}
	if _, ok := Status(errors.New("the pipe broke")); ok {
		t.Fatal("an ending with no status reads as one")
	}
}
