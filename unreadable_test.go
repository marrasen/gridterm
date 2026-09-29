package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/marrasen/kakel/screen"

	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/gunimtest"

	"github.com/marrasen/kakel/internal/testhome"
	"github.com/marrasen/kakel/remote"
	"github.com/marrasen/kakel/settings"
)

// Settings or a server list that cannot be read say so, and where,
// rather than the window quietly forgetting everything they kept.
func TestFilesThatCannotBeReadSaySo(t *testing.T) {
	testhome.New(t)
	for _, tc := range []struct {
		what string
		path func() (string, error)
		load func(*app)
	}{
		{"the settings", settings.Path, (*app).loadSettings},
		{"the server list", remote.BookPath, (*app).loadBook},
	} {
		path, err := tc.path()
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("{ not json"), 0o600); err != nil {
			t.Fatal(err)
		}
		w := gunimtest.New(t, geom.Sz(400, 300), nil)
		a := newApp(w.Client(), screen.NewShells())
		tc.load(a)
		i := slices.IndexFunc(a.st.Notices, func(n Notice) bool { return n.Title == "Couldn't read "+tc.what })
		if i < 0 {
			t.Fatalf("%s could not be read, and it said %+v", tc.what, a.st.Notices)
		}
		if n := a.st.Notices[i]; !strings.Contains(n.Body, path) || n.Kind != NoticeFailed {
			t.Fatalf("it said %+v, want the path and a failure", n)
		}
		if got, _ := os.ReadFile(path); string(got) != "{ not json" {
			t.Fatalf("%s was written over: %q", tc.what, got)
		}
	}
}
