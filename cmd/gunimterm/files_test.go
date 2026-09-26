package main

import (
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/geom"

	"github.com/marrasen/gridterm/internal/sshtest"
)

func TestAReaderIsToldHowItsSaveWent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	w := gunim.NewOffscreen(geom.Sz(400, 300), nil)
	a := newApp(w.Client(), &shells{m: map[string]*shell{}})
	a.setReader("p1", Reader{Path: "/x/notes.txt", Seq: 1})

	a.handle(SaveLines{Pane: "p1", Path: "~/kept.txt", Lines: []string{"one", "two"}})
	if got, err := os.ReadFile(filepath.Join(home, "kept.txt")); err != nil || string(got) != "one\ntwo\n" {
		t.Fatalf("saved %q, %v", got, err)
	}
	if r := a.st.Readers["p1"]; r.Saves != 1 || r.SaveErr != "" {
		t.Fatalf("saved, the reader reads %+v", r)
	}

	a.handle(SaveLines{Pane: "p1", Path: filepath.Join(home, "missing", "kept.txt"), Lines: []string{"one"}})
	if r := a.st.Readers["p1"]; r.Saves != 2 || r.SaveErr == "" {
		t.Fatalf("saved into a missing folder, the reader reads %+v", r)
	}
	if len(a.st.Notices) != 0 {
		t.Fatalf("the reader says how its save went, and notices say it again: %+v", a.st.Notices)
	}
}

// A file pane whose connection dropped opens its files again on the
// next thing asked of it, connecting again first.
func TestAFilePaneReconnectsOnItsNextAction(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("SSH_AUTH_SOCK", "")
	t.Chdir(t.TempDir())
	s := sshtest.New(t)
	host, port := s.Host()
	w := gunim.NewOffscreen(geom.Sz(400, 300), nil)
	a := newApp(w.Client(), &shells{m: map[string]*shell{}})
	a.ctx = t.Context()
	t.Cleanup(func() {
		for len(a.st.Panes) > 0 {
			a.remove(a.st.Panes[0].ID)
		}
		for _, c := range a.conns {
			_ = c.Close()
		}
	})
	answering := func() {
		for _, q := range a.st.Asks {
			ans := AskAnswered{ID: q.ID, Yes: true}
			if len(q.Prompts) > 0 {
				ans.Answers = []string{sshtest.Password}
			}
			a.handle(ans)
		}
	}
	a.handle(ConnectTo{Target: "tester@" + net.JoinHostPort(host, strconv.Itoa(port))})
	waitFor(t, a, "a shell on the server", func() bool { answering(); return len(a.st.Panes) == 1 })
	machine := a.st.Panes[0].Machine
	a.handle(OpenFiles{})
	waitFor(t, a, "the files", func() bool { return len(a.st.Panes) == 2 && a.st.Browsers[a.st.Panes[1].ID].Seq > 0 })
	files := a.st.Panes[1].ID
	seq := a.st.Browsers[files].Seq

	_ = a.conns[machine].Close()
	waitFor(t, a, "the connection to go", func() bool { return a.conns[machine] == nil && a.remoteFS[machine] == nil })
	a.handle(Browse{Pane: files, Path: a.st.Browsers[files].Path})
	waitFor(t, a, "the folder read again", func() bool { answering(); return a.st.Browsers[files].Seq > seq })
	if a.conns[machine] == nil {
		t.Fatal("the folder was read with no connection")
	}
}
