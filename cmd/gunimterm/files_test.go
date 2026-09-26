package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/geom"
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
