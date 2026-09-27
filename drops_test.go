package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marrasen/gunim/geom"
	gi "github.com/marrasen/gunim/input"

	"github.com/marrasen/gridterm/vt"
)

func TestFilesDroppedOnAShellThatSaidNoFolderAreTyped(t *testing.T) {
	a, sess := localPane(t, "", "/bin/bash")
	a.handle(DropFiles{Pane: "p1", Paths: []string{"/tmp/a b.txt", "/tmp/c.txt"}})
	waitFor(t, a, "the paths typed", func() bool { return strings.Contains(sess.sent(), `"/tmp/a b.txt" /tmp/c.txt`) })
}

func TestFilesDroppedOnAShellGoIntoItsFolder(t *testing.T) {
	here, from := t.TempDir(), t.TempDir()
	a, sess := localPane(t, "\x1b]7;file://localhost"+here+"\x07", "/bin/bash")
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
	if sess.sent() != "" {
		t.Fatalf("with the file where the shell is, it typed %q", sess.sent())
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
	got, err := os.ReadFile(filepath.Join(os.Getenv("HOME"), "gridterm-pasted", "report.txt"))
	if err != nil || string(got) != "far" {
		t.Fatalf("copied %q, %v", got, err)
	}
}

func TestADropGoesToTheTerminalUnderItOrTheFocusedOne(t *testing.T) {
	win, sh, publish := windowStage(t)
	quiet := shellHooks{output: func() {}, title: func(string) {}, exit: func() {}, clipboard: func(string) {}}
	sh.set("p1", openShell(&typed{done: make(chan struct{})}, vt.DefaultPalette(), quiet))
	t.Cleanup(func() { _ = sh.get("p1").t.Close() })
	publish(State{Panes: []Pane{{ID: "p1", Title: "Terminal 1"}}, Stage: &Box{Pane: "p1"}, Focus: "p1"})
	for len(lastWindow.Client().Intents()) > 0 {
		<-lastWindow.Client().Intents()
	}
	drop := func(at geom.Point) DropFiles {
		t.Helper()
		lastWindow.Input(gi.Drop{Pos: at, Paths: []string{"/tmp/x.png"}})
		lastWindow.Frame(time.Second / 60)
		in, ok := nextIntent(t).(DropFiles)
		if !ok || len(in.Paths) != 1 {
			t.Fatalf("the drop sent %#v", in)
		}
		return in
	}
	box, _ := lastUI.Bounds(win.terms["p1"])
	if in := drop(box.Center()); in.Pane != "p1" {
		t.Fatalf("dropped on the terminal, it went to %q", in.Pane)
	}
	if in := drop(geom.Pt(20, 200)); in.Pane != "" {
		t.Fatalf("dropped on the sidebar, it went to %q, want the focused pane", in.Pane)
	}
}
