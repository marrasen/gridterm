package app

import (
	"testing"

	"github.com/marrasen/kakel/screen"

	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/gunimtest"
)

// A folder a pane says it is in starts a new shell there only when it is
// a folder on this computer; one from another system starts it where it
// would have started, rather than stopping it starting.
func TestANewShellStartsOnlyInAFolderThatIsHere(t *testing.T) {
	w := gunimtest.New(t, geom.Sz(400, 300), nil)
	a := newApp(w.Client(), screen.NewShells())
	dir := t.TempDir()
	if got := a.localDir("p1", dir); got != dir {
		t.Fatalf("a folder here gave %q", got)
	}
	if got := a.localDir("p1", "/mnt/d/no/such/folder"); got != "" {
		t.Fatalf("a folder from elsewhere gave %q", got)
	}
}
