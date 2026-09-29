package agenthost

import (
	"path/filepath"
	"testing"

	"github.com/marrasen/kakel/internal/testhome"
)

// A relative directory in an agent program's setting is read from home.
func TestASkillDirectoryIsReadFromHome(t *testing.T) {
	home := testhome.New(t)
	got, err := fromHome("conf/claude")
	if err != nil || got != filepath.Join(home, "conf", "claude") {
		t.Fatalf("read %q, %v", got, err)
	}
}

// A directory rooted with a slash is left as it is: on Windows it is
// the top of the current drive, not a folder under home.
func TestARootedClaudeConfigDirIsLeftAlone(t *testing.T) {
	testhome.New(t)
	for _, dir := range []string{"/etc/claude", `\claude`} {
		if got, err := fromHome(dir); err != nil || got != dir {
			t.Errorf("%q read as %q, %v", dir, got, err)
		}
	}
}
