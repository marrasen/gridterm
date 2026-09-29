package app

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
	a.addPane(Pane{ID: "p1", Title: "one", Kind: KindFiles}, nil, Placement{})
	a.addPane(Pane{ID: "p2", Title: "two", Kind: KindFiles}, nil, Placement{})
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

// A split puts a chooser beside the pane at once. A pane picked in it
// moves into the chooser's place, and the chooser goes.
func TestAPanePickedInASplitsChooserTakesItsPlace(t *testing.T) {
	w := gunimtest.New(t, geom.Sz(400, 300), nil)
	a := newApp(w.Client(), screen.NewShells())
	a.addPane(Pane{ID: "f1", Title: "one", Kind: KindFiles}, nil, Placement{})
	a.addPane(Pane{ID: "f2", Title: "two", Kind: KindFiles}, nil, Placement{})
	a.focus("f1")
	a.handle(ChooseSplit{Vertical: true})
	chooser := a.st.Focus
	g := a.groupOf["f1"]
	if b := a.groups[g]; a.kindOfPane(chooser) != KindChooser || b.B == nil || b.B.Pane != chooser || !b.Vertical {
		t.Fatalf("split, the group is %+v and the focus on %s", b, chooser)
	}
	a.handle(MovePane{Pane: "f2", Instead: chooser})
	if b := a.groups[g]; b.A.Pane != "f1" || b.B.Pane != "f2" || !b.Vertical {
		t.Fatalf("picked, the group is %+v", b)
	}
	if a.has(chooser) || len(a.st.Panes) != 2 || a.st.Focus != "f2" || len(a.groups) != 1 {
		t.Fatalf("picked, the panes are %+v, the focus on %s, %d groups", a.st.Panes, a.st.Focus, len(a.groups))
	}
}

// A new shell picked in a split's chooser opens in its place, and a
// chooser closed gives its half back.
func TestANewShellPickedInASplitsChooserTakesItsPlace(t *testing.T) {
	a, _ := agentApp(t)
	first := a.st.Panes[0].ID
	a.handle(ChooseSplit{})
	chooser := a.st.Focus
	a.handle(SplitPane{Instead: chooser})
	waitFor(t, a, "the new shell", func() bool { return !a.has(chooser) })
	g := a.groupOf[first]
	if b := a.groups[g]; b.A == nil || b.A.Pane != first || b.B == nil || a.kindOfPane(b.B.Pane) != KindTerminal {
		t.Fatalf("picked, the group is %+v", b)
	}
	a.handle(ChooseSplit{})
	chooser = a.st.Focus
	a.remove(chooser)
	if _, kept := a.choosers[chooser]; a.has(chooser) || kept {
		t.Fatalf("closed, the chooser stays: %v", a.choosers)
	}
}

// A second pick in a chooser already gone to the first lands beside the
// pane it was split from, in the same split, not on a stage of its own.
func TestASecondPickInAChooserLandsBesideItsPane(t *testing.T) {
	w := gunimtest.New(t, geom.Sz(400, 300), nil)
	a := newApp(w.Client(), screen.NewShells())
	a.addPane(Pane{ID: "f1", Title: "one", Kind: KindFiles}, nil, Placement{})
	a.addPane(Pane{ID: "f2", Title: "two", Kind: KindFiles}, nil, Placement{})
	a.addPane(Pane{ID: "f3", Title: "three", Kind: KindFiles}, nil, Placement{})
	a.focus("f1")
	a.handle(ChooseSplit{})
	chooser := a.st.Focus
	a.handle(MovePane{Pane: "f2", Instead: chooser})
	a.addPane(Pane{ID: "f4", Title: "four", Kind: KindFiles}, nil, Placement{Instead: chooser})
	if a.groupOf["f4"] != a.groupOf["f1"] {
		t.Fatalf("the second pick went to group %d, the split is %d", a.groupOf["f4"], a.groupOf["f1"])
	}
}
