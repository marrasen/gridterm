//go:build linux

package install

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Installing puts the program, its desktop file and its icon in the
// user's folders, starts it with the session when asked, and taking it
// away again leaves none of them.
func TestInstallingAndTakingAway(t *testing.T) {
	h := t.TempDir()
	t.Setenv("HOME", h)
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	from := filepath.Join(t.TempDir(), "kakel")
	if err := os.WriteFile(from, []byte("program"), 0o755); err != nil {
		t.Fatal(err)
	}
	exe, err := Install(from, Options{Autostart: true, Version: "v1.2.3"})
	if err != nil {
		t.Fatal(err)
	}
	if exe != filepath.Join(h, ".local", "bin", "kakel") || !Installed(exe) || Installed(from) {
		t.Fatalf("installed at %s", exe)
	}
	if got, _ := os.ReadFile(exe); string(got) != "program" {
		t.Fatalf("the program installed holds %q", got)
	}
	app, err := os.ReadFile(filepath.Join(h, ".local", "share", "applications", "kakel.desktop"))
	if err != nil || !strings.Contains(string(app), "Exec="+exe+"\n") {
		t.Fatalf("the desktop file is %q, %v", app, err)
	}
	if !Autostart() {
		t.Fatal("kakel does not start with the session")
	}
	if a, _ := os.ReadFile(filepath.Join(h, ".config", "autostart", "kakel.desktop")); !strings.Contains(string(a), " -tray") {
		t.Fatalf("the autostart file is %q", a)
	}
	if err := Uninstall(); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{exe, filepath.Join(h, ".local", "share", "applications", "kakel.desktop"), filepath.Join(h, ".config", "autostart", "kakel.desktop")} {
		if _, err := os.Stat(f); !os.IsNotExist(err) {
			t.Fatalf("%s is still there", f)
		}
	}
}

// A replace puts the new program in place of the old whole.
func TestAReplaceIsWhole(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "kakel")
	_ = os.WriteFile(exe, []byte("old"), 0o755)
	_ = os.WriteFile(exe+".new", []byte("new"), 0o755)
	if err := Replace(exe+".new", exe); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(exe); string(got) != "new" {
		t.Fatalf("the program holds %q", got)
	}
}
