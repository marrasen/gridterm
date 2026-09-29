package main

import (
	"testing"

	"github.com/marrasen/kakel/screen"

	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/gunimtest"

	"github.com/marrasen/kakel/vfs"
)

// A pane open on a stage of its own moves into a split beside another.
func TestAPaneMovesIntoASplit(t *testing.T) {
	w := gunimtest.New(t, geom.Sz(400, 300), nil)
	a := newApp(w.Client(), screen.NewShells())
	a.addPane(Pane{ID: "p1", Title: "one", Kind: kindFiles}, nil, placement{})
	a.addPane(Pane{ID: "p2", Title: "two", Kind: kindFiles}, nil, placement{})
	if a.groupOf["p1"] == a.groupOf["p2"] {
		t.Fatal("the panes began together")
	}
	a.handle(MovePane{Pane: "p2", Beside: "p1"})
	g := a.groupOf["p1"]
	if a.groupOf["p2"] != g {
		t.Fatalf("moved, the panes are in groups %d and %d", g, a.groupOf["p2"])
	}
	if b := a.groups[g]; b.A == nil || b.A.Pane != "p1" || b.B == nil || b.B.Pane != "p2" || b.Vertical {
		t.Fatalf("moved, the group is %+v", b)
	}
	if len(a.groups) != 1 || a.st.Focus != "p2" {
		t.Fatalf("moved, there are %d groups and the focus is on %s", len(a.groups), a.st.Focus)
	}
}

// Files opened from a file pane opens beside it, for the two side by
// side a copy goes between.
func TestFilesFromAFilePaneOpenBesideIt(t *testing.T) {
	w := gunimtest.New(t, geom.Sz(400, 300), nil)
	a := newApp(w.Client(), screen.NewShells())
	a.ctx = t.Context()
	local := vfs.NewLocal()
	dir := t.TempDir()
	if err := a.openFilesOn("", local, dir); err != nil {
		t.Fatal(err)
	}
	first := a.st.Focus
	if err := a.openFilesOn("", local, dir); err != nil {
		t.Fatal(err)
	}
	if second := a.st.Focus; second == first || a.groupOf[second] != a.groupOf[first] {
		t.Fatalf("the second file pane, %s, is in group %d, and the first, %s, in %d", second, a.groupOf[second], first, a.groupOf[first])
	}
}
