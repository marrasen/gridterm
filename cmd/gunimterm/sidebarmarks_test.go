package main

import (
	"testing"
	"time"

	gi "github.com/marrasen/gunim/input"
	"github.com/marrasen/gunim/widget"

	"github.com/marrasen/gridterm/meter"
	"github.com/marrasen/gridterm/vt"
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
		Windows: []RemoteWindow{{Name: "desk", Addr: "desk:2222"}},
		Tunnels: []Tunnel{{ID: "t1", Machine: "srv", Label: ":8080", Live: true, Meter: busy}},
		Jobs: []Job{{ID: "j1", Title: "Copying 2 items", Machine: "srv", Kind: "copy", Share: 0.4},
			{ID: "j2", Title: "Deleting 1 item", Machine: "srv", Kind: "delete", Share: 1, Done: true}},
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
	if r := row("job:j2"); r.closes != (DropJob{ID: "j2"}) || r.marks.filling || r.marks.live(now) != meter.Closed {
		t.Fatalf("finished file work is marked %+v", r.marks)
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

func TestSharingShowsChipsAndOpensThePermissions(t *testing.T) {
	win, sh, publish := windowStage(t)
	quiet := shellHooks{output: func() {}, title: func(string) {}, exit: func() {}, clipboard: func(string) {}}
	sh.set("p1", openShell(&typed{done: make(chan struct{})}, vt.DefaultPalette(), quiet))
	t.Cleanup(func() { _ = sh.get("p1").t.Close() })
	st := State{Panes: []Pane{{ID: "p1", Title: "Terminal 1"}}, Stage: &Box{Pane: "p1"}, Focus: "p1"}
	publish(st)
	for len(lastWindow.Client().Intents()) > 0 {
		<-lastWindow.Client().Intents()
	}
	win.focused = "p1"
	win.run("agent.hand", lastUI)
	for {
		if in, ok := nextIntent(t).(SharePane); ok && in.Pane == "p1" {
			break
		}
	}
	st.Share = Share{Code: "ABC", Panes: []SharedPane{{Pane: "p1"}}}
	st.Serving = Serving{On: true, Clients: []ServedClient{{Name: "laptop", From: "10.0.0.2"}}}
	publish(st)
	if win.dialog == nil {
		t.Fatal("shared, the pane's permissions did not open")
	}
	var said []string
	for _, c := range win.chips.chips {
		said = append(said, c.text)
	}
	for range 3 {
		lastWindow.Frame(time.Second / 60)
	}
	if len(win.chips.boxes) != 2 {
		t.Fatalf("the chips are laid out at %v", win.chips.boxes)
	}
	if len(said) != 2 || said[0] != "Agent Share" || said[1] != "Serving · 1" {
		t.Fatalf("the chips say %v", said)
	}
}
