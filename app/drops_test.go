package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFilesDroppedOnAShellThatSaidNoFolderAreTyped(t *testing.T) {
	a, sess := localPane(t, "", "/bin/bash")
	a.handle(DropFiles{Pane: "p1", Paths: []string{"/tmp/a b.txt", "/tmp/c.txt"}})
	waitFor(t, a, "the paths typed", func() bool { return strings.Contains(sess.Sent(), `"/tmp/a b.txt" /tmp/c.txt`) })
}

func TestFilesDroppedOnAShellGoIntoItsFolder(t *testing.T) {
	here, from := t.TempDir(), t.TempDir()
	a, sess := localPane(t, saysFolder(here), "/bin/bash")
	waitFor(t, a, "the shell to say where it is", func() bool {
		dir, _ := a.terminal("p1").Dir()
		return dir == here
	})
	src := filepath.Join(from, "notes.txt")
	if err := os.WriteFile(src, []byte("dropped"), 0o600); err != nil {
		t.Fatal(err)
	}
	a.handle(DropFiles{Paths: []string{src}})
	waitFor(t, a, "the notice", func() bool { return len(a.st.Notices) > 0 })
	if got, err := os.ReadFile(filepath.Join(here, "notes.txt")); err != nil || string(got) != "dropped" {
		t.Fatalf("copied %q, %v", got, err)
	}
	if n := a.st.Notices[0]; n.Title != "Copied notes.txt to "+here+" on this computer" {
		t.Fatalf("said %+v", n)
	}
	if sess.Sent() != "" {
		t.Fatalf("with the file where the shell is, it typed %q", sess.Sent())
	}
}

func TestFilesDroppedOnAPaneOnAWindowAreCopiedThereAndTyped(t *testing.T) {
	a, b := connectedWindows(t)
	src := filepath.Join(t.TempDir(), "report.txt")
	if err := os.WriteFile(src, []byte("far"), 0o600); err != nil {
		t.Fatal(err)
	}
	b.handle(DropFiles{Pane: b.st.Panes[0].ID, Paths: []string{src}})
	there := a.st.Panes[1].ID
	pumpBoth(t, a, b, "the path typed there", func() bool {
		if len(b.st.Notices) > 0 {
			t.Fatalf("dropping said %+v", b.st.Notices)
		}
		return strings.Contains(a.terminal(there).Text(), "report.txt")
	})
	got, err := os.ReadFile(filepath.Join(os.Getenv("HOME"), "kakel-pasted", "report.txt"))
	if err != nil || string(got) != "far" {
		t.Fatalf("copied %q, %v", got, err)
	}
}
