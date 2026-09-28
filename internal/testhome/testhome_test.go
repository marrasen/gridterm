package testhome

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMain(m *testing.M) { Main(m) }

// Every way Go finds the user's folders leads into the test's home.
// On Windows os.UserHomeDir reads USERPROFILE: a helper that moved
// only HOME let a test write into the real home there.
func TestNewMovesEveryHomeFolder(t *testing.T) {
	home := New(t)

	if got, err := os.UserHomeDir(); err != nil || got != home {
		t.Errorf("os.UserHomeDir is %q, %v, want %q", got, err, home)
	}
	if got, err := os.UserConfigDir(); err != nil || !inside(got, home) {
		t.Errorf("os.UserConfigDir is %q, %v, want a folder in %q", got, err, home)
	}
	for _, name := range Vars {
		if got := os.Getenv(name); got != "" && !inside(got, home) {
			t.Errorf("%s is %q, want a folder in %q", name, got, home)
		}
	}
	for _, name := range []string{"HOME", "USERPROFILE", "APPDATA", "LOCALAPPDATA", "XDG_CONFIG_HOME"} {
		if os.Getenv(name) == "" {
			t.Errorf("%s is empty, want a folder in %q", name, home)
		}
	}
}

// Main moves the same folders for the whole package, before any test
// runs.
func TestMainMovesEveryHomeFolder(t *testing.T) {
	home := Made()
	if home == "" {
		t.Fatal("Main gave the tests no home")
	}
	if got, err := os.UserHomeDir(); err != nil || got != home {
		t.Errorf("os.UserHomeDir is %q, %v, want %q", got, err, home)
	}
	if got, err := os.UserConfigDir(); err != nil || !inside(got, home) {
		t.Errorf("os.UserConfigDir is %q, %v, want a folder in %q", got, err, home)
	}
	if got := os.Getenv("LOCALAPPDATA"); !inside(got, home) {
		t.Errorf("LOCALAPPDATA is %q, want a folder in %q", got, home)
	}
}

// inside reports whether path is dir or a folder in it.
func inside(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
