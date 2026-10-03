//go:build windows && amd64

package conpty

import (
	"os"
	"path/filepath"
	"testing"
)

// OpenConsole.exe gone from beside the loaded conpty.dll, as a cleaner
// of caches can make it, is put back for the next console:
// conpty.dll would otherwise run Windows' own console host in its place.
func TestOpenConsoleIsPutBackWhenItWentMissing(t *testing.T) {
	// A folder of its own, as the shared one's OpenConsole.exe may be
	// running. conpty.dll stays behind in it: a loaded DLL cannot be
	// removed.
	dir, err := os.MkdirTemp("", "kakel-conpty-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	h, err := bundledIn(dir)
	if err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(dir, "OpenConsole.exe")
	if err := os.Remove(exe); err != nil {
		t.Fatalf("take OpenConsole away: %v", err)
	}
	c, err := newFrom(h, 80, 24)
	if err != nil {
		t.Fatalf("make a console: %v", err)
	}
	defer c.Close()
	if c.Host() != h.name {
		t.Fatalf("the console was made with %s, want %s", c.Host(), h.name)
	}
	if _, err := os.Stat(exe); err != nil {
		t.Fatalf("OpenConsole was not put back: %v", err)
	}
}
