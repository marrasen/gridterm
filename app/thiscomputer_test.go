package app

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// A shell started from a folder nobody starts kakel in to work there,
// as a shortcut gives it, starts in the start folder This Computer
// sets, or else at home; any other folder stays.
func TestAShellStartsInTheStartFolder(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the installed folder is the user's own here")
	}
	a := updatesApp(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	bin := filepath.Join(home, ".local", "bin")
	work, elsewhere := t.TempDir(), t.TempDir()
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, from := range []string{"", home, bin} {
		if got := a.startDir(from); got != home {
			t.Fatalf("from %q with no start folder, a shell starts in %s, want home", from, got)
		}
	}
	a.handle(SaveThisComputer{StartFolder: work})
	if a.st.ThisComputer.StartFolder != work {
		t.Fatalf("saved, the state says %+v", a.st.ThisComputer)
	}
	for _, from := range []string{"", home, bin} {
		if got := a.startDir(from); got != work {
			t.Fatalf("from %q, a shell starts in %s, want %s", from, got, work)
		}
	}
	if got := a.startDir(elsewhere); got != elsewhere {
		t.Fatalf("from %s, a shell starts in %s", elsewhere, got)
	}
	// ~ is home, and a folder that isn't there is refused.
	a.handle(SaveThisComputer{StartFolder: "~/.local"})
	if want := filepath.Join(home, ".local"); a.st.ThisComputer.StartFolder != want {
		t.Fatalf("~/.local saved as %q, want %q", a.st.ThisComputer.StartFolder, want)
	}
	a.handle(SaveThisComputer{StartFolder: filepath.Join(home, "nowhere")})
	if want := filepath.Join(home, ".local"); a.st.ThisComputer.StartFolder != want {
		t.Fatalf("a folder that isn't there was saved: %q", a.st.ThisComputer.StartFolder)
	}
}
