package main

import (
	"strings"
	"testing"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/geom"
)

func TestEachLogHasOnePane(t *testing.T) {
	w := gunim.NewOffscreen(geom.Sz(400, 300), nil)
	sh := &shells{m: map[string]*shell{}}
	a := newApp(w.Client(), sh)
	a.ctx = t.Context()
	t.Cleanup(func() {
		for _, p := range a.st.Panes {
			a.remove(p.ID)
		}
	})
	a.handle(ShowLog{})
	a.handle(ShowLog{})
	if len(a.st.Panes) != 1 || a.st.Panes[0].Kind != kindLog || a.st.Panes[0].Title != "Window Log" {
		t.Fatalf("asked twice for the window log, the panes are %+v", a.st.Panes)
	}
	a.handle(ShowLog{Machine: "srv"})
	if len(a.st.Panes) != 1 || len(a.st.Notices) != 1 || !strings.Contains(a.st.Notices[0].Body, "srv") {
		t.Fatalf("with no connection log for srv, the panes are %+v and the notices %+v", a.st.Panes, a.st.Notices)
	}
	logLine(a.account("srv"), badly, "no route")
	a.handle(ShowLog{Machine: "srv"})
	if len(a.st.Panes) != 2 || a.st.Panes[1].Machine != "srv" || a.st.Focus != a.st.Panes[1].ID {
		t.Fatalf("srv's log opened as %+v, focus on %q", a.st.Panes, a.st.Focus)
	}
	a.handle(FocusPane{Pane: a.st.Panes[0].ID})
	a.handle(ShowLog{Machine: "srv"})
	if len(a.st.Panes) != 2 || a.st.Focus != a.st.Panes[1].ID {
		t.Fatalf("asked again, srv's log is %+v, focus on %q", a.st.Panes, a.st.Focus)
	}
}
