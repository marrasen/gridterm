//go:build windows

package install

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/marrasen/kakel/internal/testhome"
)

// Uninstalling takes away the OpenConsole kakel put in the user's
// cache, every version of it, and leaves the rest of kakel's folder.
func TestUninstallTakesTheConPTYAway(t *testing.T) {
	home := testhome.New(t)
	kakel := filepath.Join(home, "AppData", "Local", "kakel")
	placed := filepath.Join(kakel, "conpty", "1.25.260930003")
	if err := os.MkdirAll(placed, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{filepath.Join(placed, "conpty.dll"), filepath.Join(placed, "OpenConsole.exe"), filepath.Join(kakel, "keep.json")} {
		if err := os.WriteFile(f, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	removeConPTY()
	if _, err := os.Stat(filepath.Join(kakel, "conpty")); !os.IsNotExist(err) {
		t.Fatalf("the conpty folder is still there: %v", err)
	}
	if _, err := os.Stat(filepath.Join(kakel, "keep.json")); err != nil {
		t.Fatalf("the rest of kakel's folder went too: %v", err)
	}
}
