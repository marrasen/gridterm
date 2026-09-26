package main

import (
	"testing"
	"time"

	gi "github.com/marrasen/gunim/input"
	"github.com/marrasen/gunim/widget"

	"github.com/marrasen/gridterm/meter"
)

func TestTheSidebarMarksWhatEachRowIs(t *testing.T) {
	win, _, publish := windowStage(t)
	busy := meter.New()
	busy.Moved(10, 0, time.Now())
	st := State{Sidebar: true, SidebarWidth: 220, Connected: []string{"srv"},
		Panes: []Pane{
			{ID: "p1", Title: "Terminal 1"},
			{ID: "p2", Title: "docs", Kind: kindFiles},
			{ID: "p3", Title: "far shell", Machine: "desk", On: "db"},
		},
		Windows:  []RemoteWindow{{Name: "desk", Addr: "desk:2222"}},
		Tunnels:  []Tunnel{{ID: "t1", Machine: "srv", Label: ":8080", Live: true, Meter: busy}},
		Jobs:     []Job{{ID: "j1", Title: "Copying 2 items", Machine: "srv", Kind: "copy", Share: 0.4}},
		Stage:    &Box{Pane: "p2"},
		Focus:    "p2",
		Browsers: map[string]Browser{"p2": {Path: "/", Seq: 1}},
	}
	publish(st)
	row := func(key string) *sideRow {
		t.Helper()
		r, ok := widget.RowOf[*sideRow](win.list, widget.Key(key))
		if !ok {
			t.Fatalf("no row %s in %v", key, win.list.Keys())
		}
		return r
	}
	now := time.Now()
	if m := row("p2").marks; m.kind != "files" || m.live(now) != meter.Settled {
		t.Fatalf("a file pane is marked %q, %v", m.kind, m.live(now))
	}
	if m := row("tunnel:t1").marks; m.traffic != busy || m.live(now) != meter.Active {
		t.Fatalf("a busy tunnel is marked %+v", m)
	}
	if m := row("job:j1").marks; !m.filling || m.fill != 0.4 || m.kind != "copy" {
		t.Fatalf("file work is marked %+v", m)
	}
	if r := row("machine:desk" + farSep + "db"); !r.heading || r.marks.depth != 1 {
		t.Fatal("a machine the window reached has no heading a step in")
	}
	row("p3")

	// The cross on a row closes it.
	for len(lastWindow.Client().Intents()) > 0 {
		<-lastWindow.Client().Intents()
	}
	for range 30 {
		lastWindow.Frame(time.Second / 60)
	}
	r := row("p2")
	at, _ := lastUI.Bounds(r)
	p := at.Min.Add(r.marks.cross.Center())
	lastWindow.Input(gi.PointerMove{Pos: p})
	lastWindow.Input(gi.PointerDown{Pos: p, Button: gi.ButtonPrimary, Clicks: 1})
	lastWindow.Input(gi.PointerUp{Pos: p, Button: gi.ButtonPrimary})
	lastWindow.Frame(time.Second / 60)
	if in, ok := nextIntent(t).(ClosePane); !ok || in.Pane != "p2" {
		t.Fatalf("the cross sent %#v", in)
	}
}
