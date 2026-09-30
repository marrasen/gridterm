package view

import (
	"io/fs"
	"testing"

	"github.com/marrasen/kakel/app"
	"github.com/marrasen/kakel/vfs"

	"github.com/marrasen/gunim/geom"
	gi "github.com/marrasen/gunim/input"
	"github.com/marrasen/gunim/widget"
)

// filePaneStage is a window showing file pane p1 on /srv, holding a
// folder, docs, and a file, a.txt.
func filePaneStage(t *testing.T, st app.Browser) *browser {
	t.Helper()
	win, _, publish := windowStage(t)
	if st.Path == "" {
		st.Path = "/srv"
	}
	st.Seq, st.Sep = 1, "/"
	st.Entries = []vfs.Entry{{Name: "docs", Mode: fs.ModeDir}, {Name: "a.txt", Size: 3}}
	publish(app.State{Panes: []app.Pane{{ID: "p1", Title: "srv", Kind: app.KindFiles}}, Stage: &app.Box{Pane: "p1"}, Focus: "p1",
		Browsers: map[string]app.Browser{"p1": st}})
	settle()
	return win.browsers["p1"]
}

// rowPoint is the middle of row k, in the table's space.
func rowPoint(t *testing.T, b *browser, k widget.Key) geom.Point {
	t.Helper()
	r, ok := b.table.RowRect(k)
	if !ok {
		t.Fatalf("no row %q", k)
	}
	return r.Center()
}

// Rows drag as the pane's files, and leave kakel only from this
// computer.
func TestFileRowsDragAsFiles(t *testing.T) {
	b := filePaneStage(t, app.Browser{})
	data, ghost, _ := b.dragRows([]widget.Key{"a.txt", up}, geom.Point{})
	d, ok := data.(app.FileDrag)
	if !ok || ghost == nil || len(d.Names) != 1 || d.Names[0] != "a.txt" || d.At != "/srv" || !d.Local {
		t.Fatalf("the drag carries %+v", data)
	}
	if data, _, _ := b.dragRows([]widget.Key{up}, geom.Point{}); data != nil {
		t.Fatal("the folder above was dragged")
	}
}

// A drag over a folder's row goes into it, and springs it open; over
// the rest, into the folder shown. Between folders of one machine it
// moves, from another it copies, and Ctrl copies.
func TestAFilePaneSaysWhatADropWouldDo(t *testing.T) {
	b := filePaneStage(t, app.Browser{})
	drag := app.FileDrag{Pane: "p9", At: "/home", Names: []string{"x"}}
	spot, ok := b.dropSpot(gi.Drop{Pos: rowPoint(t, b, "docs"), Data: drag}, lastUI)
	if !ok || !spot.Opens || spot.Refused || b.plan.Into != "/srv/docs" || b.plan.Copy {
		t.Fatalf("over docs, the spot is %+v and the plan %+v", spot, b.plan)
	}
	if h, _ := spot.Hint.(widget.DropHint); h.Effect != widget.DropMove || h.Text != "Move to docs" {
		t.Fatalf("the hint says %+v", h)
	}
	spot, _ = b.dropSpot(gi.Drop{Pos: rowPoint(t, b, "a.txt"), Data: drag, Mods: gi.ModControl}, lastUI)
	if spot.Opens || b.plan.Into != "/srv" || !b.plan.Copy {
		t.Fatalf("over a file with Ctrl, the spot is %+v and the plan %+v", spot, b.plan)
	}
	drag.Machine = "srv"
	b.dropSpot(gi.Drop{Pos: rowPoint(t, b, "a.txt"), Data: drag}, lastUI)
	if !b.plan.Copy {
		t.Fatal("from another machine, the drop moves")
	}
	b.dropSpot(gi.Drop{Pos: rowPoint(t, b, "a.txt"), Paths: []string{"/tmp/y"}}, lastUI)
	if !b.plan.Copy || len(b.plan.Paths) != 1 {
		t.Fatalf("files from another program plan %+v", b.plan)
	}
}

// A drop that would do nothing is refused and says why: where they are
// already, into a folder dragged, or inside an archive.
func TestAFilePaneRefusesADropThatDoesNothing(t *testing.T) {
	b := filePaneStage(t, app.Browser{})
	spot, _ := b.dropSpot(gi.Drop{Pos: rowPoint(t, b, "a.txt"), Data: app.FileDrag{Pane: "p1", At: "/srv", Names: []string{"a.txt"}}}, lastUI)
	if h, _ := spot.Hint.(widget.DropHint); !spot.Refused || h.Text != "Already here" {
		t.Fatalf("dropped where it is, the spot is %+v", spot)
	}
	spot, _ = b.dropSpot(gi.Drop{Pos: rowPoint(t, b, "docs"), Data: app.FileDrag{Pane: "p1", At: "/srv", Names: []string{"docs"}}}, lastUI)
	if h, _ := spot.Hint.(widget.DropHint); !spot.Refused || h.Text != "Cannot go inside itself" {
		t.Fatalf("a folder on itself, the spot is %+v", spot)
	}
	b = filePaneStage(t, app.Browser{Path: "/srv/x.zip", Archive: true})
	spot, _ = b.dropSpot(gi.Drop{Pos: rowPoint(t, b, "a.txt"), Paths: []string{"/tmp/y"}}, lastUI)
	if !spot.Refused {
		t.Fatal("a drop into an archive was taken")
	}
	if data, _, _ := b.dragRows([]widget.Key{"a.txt"}, geom.Point{}); data.(app.FileDrag).Local {
		t.Fatal("a file in an archive may leave kakel")
	}
}
