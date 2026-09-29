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
