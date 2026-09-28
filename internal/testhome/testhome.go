// Package testhome gives tests a home folder of their own.
//
// A test that writes where the user's files go writes into the real
// ones unless the home is moved first. Each system finds that home
// through other variables: HOME on Unix, USERPROFILE on Windows, and
// XDG_CONFIG_HOME, APPDATA and LOCALAPPDATA for settings. Setting HOME
// alone moves nothing on Windows. So every helper here points all of
// them at one temp folder at once.
package testhome

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// Vars are the variables that say where the user's own folders are.
// CLAUDE_CONFIG_DIR is among them: Claude Code reads its skills from
// there, and kakel writes its skill there when it is set.
var Vars = []string{"HOME", "USERPROFILE", "XDG_CONFIG_HOME", "APPDATA", "LOCALAPPDATA", "CLAUDE_CONFIG_DIR"}

// made is the folder Main gave the package's tests.
var made string

// New points every home variable into a new temp folder for the rest
// of the test, and returns the folder.
func New(t testing.TB) string {
	t.Helper()
	home := t.TempDir()
	At(t, home)
	return home
}

// At points every home variable into home for the rest of the test.
func At(t testing.TB, home string) {
	t.Helper()
	for name, dir := range dirs(home) {
		t.Setenv(name, dir)
	}
}

// Main runs a package's tests with every home variable pointing into a
// temp folder, so even a test that forgets New writes into that
// folder. Call it from TestMain.
func Main(m *testing.M) {
	home, err := os.MkdirTemp("", "kakel-home-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "testhome:", err)
		os.Exit(1)
	}
	for name, dir := range dirs(home) {
		if err := os.Setenv(name, dir); err != nil {
			fmt.Fprintln(os.Stderr, "testhome:", err)
			os.Exit(1)
		}
	}
	made = home
	code := m.Run()
	_ = os.RemoveAll(home)
	os.Exit(code)
}

// Made is the folder Main gave the package's tests, and "" outside
// Main.
func Made() string { return made }

// dirs is what each home variable is set to for a home at home. The
// settings folders sit where each system keeps them under a home.
// CLAUDE_CONFIG_DIR is empty, which Claude Code and kakel read as
// ~/.claude.
func dirs(home string) map[string]string {
	return map[string]string{
		"HOME":              home,
		"USERPROFILE":       home,
		"XDG_CONFIG_HOME":   filepath.Join(home, ".config"),
		"APPDATA":           filepath.Join(home, "AppData", "Roaming"),
		"LOCALAPPDATA":      filepath.Join(home, "AppData", "Local"),
		"CLAUDE_CONFIG_DIR": "",
	}
}
