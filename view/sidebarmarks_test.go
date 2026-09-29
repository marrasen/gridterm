package view

import (
	"strings"
	"testing"
	"time"

	"github.com/marrasen/kakel/app"

	"github.com/marrasen/kakel/internal/sessiontest"
	"github.com/marrasen/kakel/screen"

	"github.com/marrasen/kakel/machines"

	gi "github.com/marrasen/gunim/input"
	"github.com/marrasen/gunim/widget"

	"github.com/marrasen/kakel/meter"
	"github.com/marrasen/kakel/vt"
)

func TestTheSidebarMarksWhatEachRowIs(t *testing.T) {
	win, _, publish := windowStage(t)
	busy := meter.New()
	busy.Moved(10, 0, time.Now())
	st := app.State{Sidebar: true, SidebarWidth: 220, Connected: []machines.ID{"srv"},
		Panes: []app.Pane{
			{ID: "p1", Title: "Terminal 1"},
			{ID: "p2", Title: "docs", Kind: app.KindFiles},
			{ID: "p3", Title: "far shell", Machine: "desk", On: "db"},
		},
		Windows: []app.RemoteWindow{{Name: "desk", Addr: "desk:2222"}},
		Tunnels: []app.Tunnel{{ID: "t1", Machine: "srv", Label: ":8080", Live: true, Meter: busy}},
		Jobs: []app.Job{{ID: "j1", Title: "Copying 2 items", Machine: "srv", Kind: "copy", Share: 0.4},
			{ID: "j2", Title: "Deleting 1 item", Machine: "srv", Kind: "delete", Share: 1, Done: true},
			// On a machine the window reached with nothing open there:
			// under the window.
			{ID: "j3", Title: "Copying 1 item", Machine: machines.FarID("desk", "web"), Kind: "copy", Share: 0.1}},
		Stage:    &app.Box{Pane: "p2"},
		Focus:    "p2",
		Browsers: map[string]app.Browser{"p2": {Path: "/", Seq: 1}},
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
	if r := row("job:j2"); r.closes != (app.DropJob{ID: "j2"}) || r.marks.filling || r.marks.live(now) != meter.Closed {
		t.Fatalf("finished file work is marked %+v", r.marks)
	}
	if r := row("machine:" + string(machines.FarID("desk", "db"))); !r.heading || r.marks.depth != 1 {
		t.Fatal("a machine the window reached has no heading a step in")
	}
	row("p3")
	row("job:j3")

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
	if in, ok := nextIntent(t).(app.ClosePane); !ok || in.Pane != "p2" {
		t.Fatalf("the cross sent %#v", in)
	}
}

func TestSharingShowsChipsAndOpensThePermissions(t *testing.T) {
	win, sh, publish := windowStage(t)
	quiet := screen.Hooks{Output: func() {}, Title: func(string) {}, Exit: func() {}, Clipboard: func(string) {}}
	sh.Set("p1", screen.Open(sessiontest.New(), vt.DefaultPalette(), quiet))
	t.Cleanup(func() { _ = sh.Get("p1").T.Close() })
	st := app.State{Panes: []app.Pane{{ID: "p1", Title: "Terminal 1"}}, Stage: &app.Box{Pane: "p1"}, Focus: "p1"}
	publish(st)
	for len(lastWindow.Client().Intents()) > 0 {
		<-lastWindow.Client().Intents()
	}
	win.focused = "p1"
	win.run("agent.hand", lastUI)
	for {
		if in, ok := nextIntent(t).(app.SharePane); ok && in.Pane == "p1" {
			break
		}
	}
	st.Share = app.Share{Code: "ABC", Panes: []app.SharedPane{{Pane: "p1"}}}
	st.Serving = app.Serving{On: true, Clients: []app.ServedClient{{Name: "laptop", From: "10.0.0.2"}}}
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

// A pane's note is what its row says.
func TestARowSaysItsPanesNote(t *testing.T) {
	rows := sidebarRows([]app.Pane{{ID: "p1", Title: "Terminal 1", Note: "42%, build done"}}, nil, app.Share{}, nil, nil, nil)
	if rows[1].note != "42%, build done" {
		t.Fatalf("the sidebar row says %q", rows[1].note)
	}
}

func TestATunnelsPaneIsLitOnTheTunnelsRow(t *testing.T) {
	panes := []app.Pane{{ID: "p1", Title: "Terminal 1", Machine: "srv"}, {ID: "p2", Title: "Tunnel :80 → x:80", Machine: "srv", Kind: app.KindTunnel, Tunnel: "t1"}}
	tunnels := []app.Tunnel{{ID: "t1", Machine: "srv", Label: ":80 → x:80", Note: "idle", Live: true, Pane: "p2"}}
	var keys []string
	for _, r := range sidebarRows(panes, tunnels, app.Share{}, nil, nil, nil) {
		keys = append(keys, r.key+"="+r.pane)
	}
	want := "machine:=,machine:srv=,p1=p1,tunnel:t1=p2"
	if got := strings.Join(keys, ","); got != want {
		t.Fatalf("the rows are %s, want %s", got, want)
	}
}

// The secrets say which terminal waits for one.
func TestTheSecretsHeadingNamesTheTerminalWaiting(t *testing.T) {
	if got := secretsHeading(app.Secrets{Waiting: "Terminal 1"}); got != "Secrets — Terminal 1 is waiting for one" {
		t.Fatalf("the heading reads %q", got)
	}
}

// A window connected to this one shows each tunnel it holds through it,
// under its own row, and in who is connected.
func TestATunnelThroughThisWindowHasARow(t *testing.T) {
	win, _, publish := windowStage(t)
	st := app.State{Sidebar: true, SidebarWidth: 220, Serving: app.Serving{On: true,
		Clients: []app.ServedClient{{Name: "laptop", From: "10.0.0.2"}},
		Tunnels: []app.ServedTunnel{{Client: "laptop", From: "10.0.0.2", Label: ":8080 → db:5432", On: "db"}}}}
	publish(st)
	row, ok := widget.RowOf[*sideRow](win.list, widget.Key("client:laptop:0:tunnel:0"))
	if !ok || row.title.Text != ":8080 → db:5432" || row.note.Text != "for laptop, on db" {
		t.Fatalf("the tunnel's row is %v", ok)
	}
	if said := connectedSays(st.Serving); !strings.Contains(said, "a tunnel, :8080 → db:5432, on db") {
		t.Fatalf("who is connected reads %q", said)
	}
}
