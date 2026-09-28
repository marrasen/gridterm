package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marrasen/kakel/conf"
	"github.com/marrasen/kakel/internal/testhome"
	"github.com/marrasen/kakel/remote"
	"github.com/marrasen/kakel/serve"
	"github.com/marrasen/kakel/settings"
)

// The tests run in a home of their own, so a test that writes a
// setting, a key or a skill writes it there.
func TestMain(m *testing.M) { testhome.Main(m) }

// Every place kakel keeps the user's files is in the tests' home. On
// Windows a test once wrote a skill into the real home, because it
// moved HOME and Windows reads USERPROFILE.
func TestTheTestsKeepTheirFilesInAHomeOfTheirOwn(t *testing.T) {
	home := testhome.Made()
	if home == "" {
		t.Fatal("TestMain gave the tests no home")
	}
	places := map[string]func() (string, error){
		"home":            os.UserHomeDir,
		"settings":        conf.Dir,
		"private files":   conf.Private,
		"settings file":   settings.Path,
		"saved servers":   remote.BookPath,
		"host key":        serve.HostKeyPath,
		"Claude's skills": func() (string, error) { return skillPathFor(hostNamed(hostClaudeCode)) },
	}
	for what, where := range places {
		got, err := where()
		if err != nil {
			t.Errorf("%s: %v", what, err)
			continue
		}
		if !inside(got, home) {
			t.Errorf("the %s go in %s, outside the tests' home %s", what, got, home)
		}
	}
}

// inside reports whether path is dir or in it.
func inside(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
