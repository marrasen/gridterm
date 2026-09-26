package main

import (
	"sync"
	"testing"
	"time"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/geom"
	gi "github.com/marrasen/gunim/input"
	"github.com/marrasen/gunim/widget"

	"github.com/marrasen/gridterm/logs"
	"github.com/marrasen/gridterm/settings"
	"github.com/marrasen/gridterm/vt"
)

// lastWindow is the offscreen window windowStage made last, for a test
// that asks what the window's frame was told.
var lastWindow *gunim.Window

// lastUI is the window's UI as its last update had it, for a test that
// opens a dialog between frames.
var lastUI *gunim.UI

// windowStage mounts the window in an offscreen gunim window, and
// returns it with a way to publish a state and draw a few frames.
func windowStage(t *testing.T) (win *window, sh *shells, publish func(State)) {
	t.Helper()
	w := gunim.NewOffscreen(geom.Sz(900, 600), nil)
	lastWindow = w
	sh = &shells{m: map[string]*shell{}}
	gunim.RegisterView(w, "window", func(State) *window {
		win = newWindow(sh, shortcuts(), nil)
		return win
	}, func(win *window, st State, u *gunim.UI) {
		lastUI = u
		win.update(st, u)
	})
	c := w.Client()
	if err := c.Mount(gunim.Root, "window", "window", State{}, windowTopic); err != nil {
		t.Fatal(err)
	}
	publish = func(st State) {
		t.Helper()
		if err := c.Publish(windowTopic, st); err != nil {
			t.Fatal(err)
		}
		for range 3 {
			w.Frame(time.Second / 60)
		}
	}
	publish(State{})
	return win, sh, publish
}

// Two panes on stages of their own, jobs and a browser, with focus on
// one of them.
func twoPanes(focus string, jobs []Job) State {
	return State{
		Panes:    []Pane{{ID: "p1", Title: "Jobs", Kind: kindJobs}, {ID: "p2", Title: "gthome", Kind: kindFiles}},
		Stage:    &Box{Pane: focus},
		Focus:    focus,
		Sidebar:  true,
		Jobs:     jobs,
		Browsers: map[string]Browser{"p2": {Path: "/"}},
	}
}

func TestAJobArrivingOffStageShowsWhenTheJobsPaneComesBack(t *testing.T) {
	win, _, publish := windowStage(t)
	publish(twoPanes("p1", nil))
	publish(twoPanes("p2", nil))
	job := Job{ID: "j1", Title: "Copying 1 item to x", Detail: "Counting…", Share: -1}
	publish(twoPanes("p2", []Job{job}))
	publish(twoPanes("p1", []Job{job}))
	if _, ok := widget.RowOf[*jobCard](win.jobs.list, "j1"); !ok {
		t.Fatal("back on stage, the jobs pane lacks the job that arrived while it was away")
	}
}

func TestATunnelChangingOffStageShowsWhenItsPaneComesBack(t *testing.T) {
	win, sh, publish := windowStage(t)
	// The tunnel's account, as the program gives the pane.
	account := logs.New(10, nil).Open()
	t.Cleanup(func() { _ = account.Close() })
	quiet := shellHooks{output: func() {}, title: func(string) {}, exit: func() {}, clipboard: func(string) {}}
	sh.set("p1", openShell(account, vt.DefaultPalette(), quiet))
	st := func(focus string, live bool) State {
		return State{
			Panes:    []Pane{{ID: "p1", Title: "Tunnel", Machine: "srv", Kind: kindTunnel, Tunnel: "t1"}, {ID: "p2", Title: "gthome", Kind: kindFiles}},
			Stage:    &Box{Pane: focus},
			Focus:    focus,
			Tunnels:  []Tunnel{{ID: "t1", Machine: "srv", Label: ":80 → x:80", Note: "idle", Live: live, Pane: "p1"}},
			Browsers: map[string]Browser{"p2": {Path: "/"}},
		}
	}
	publish(st("p1", true))
	publish(st("p2", true))
	publish(st("p2", false))
	publish(st("p1", false))
	bar := win.tunnelPanes["p1"].bar
	if len(bar.bar.shown) != 1 || bar.bar.shown[0] != bar.close || bar.close.Label != "Clear" {
		t.Fatalf("back on stage, the stopped tunnel's bar offers %d buttons, the last saying %q", len(bar.bar.shown), bar.close.Label)
	}
}

func TestPaneTitlesComeAndGo(t *testing.T) {
	win, _, publish := windowStage(t)
	st := twoPanes("p2", nil)
	st.PaneTitles = true
	publish(st)
	c, ok := win.stage.shown.(*captioned)
	if !ok {
		t.Fatalf("with titles, the stage shows %T", win.stage.shown)
	}
	if got := c.label.Text; got != "This computer: gthome" {
		t.Fatalf("the title reads %q", got)
	}
	st.PaneTitles = false
	publish(st)
	if _, ok := win.stage.shown.(*browser); !ok {
		t.Fatalf("without titles, the stage shows %T", win.stage.shown)
	}
}

func TestABellAsksForAttention(t *testing.T) {
	_, _, publish := windowStage(t)
	st := twoPanes("p2", nil)
	st.Bells = 1
	st.Panes[0].Rang = true
	publish(st)
	if got := lastWindow.Offscreen().Attention(); got != 1 {
		t.Fatalf("after a bell, attention was asked for %d times", got)
	}
}

// sizes records the sizes a shell is given.
type sizes struct {
	typed
	mu   sync.Mutex
	seen [][2]int
}

func (s *sizes) Resize(cols, rows int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seen = append(s.seen, [2]int{cols, rows})
	return nil
}

func TestAPaneSlidingInKeepsItsShellAUsableSize(t *testing.T) {
	win, sh, publish := windowStage(t)
	_ = win
	quiet := shellHooks{output: func() {}, title: func(string) {}, exit: func() {}, clipboard: func(string) {}}
	var got []*sizes
	for _, id := range []string{"p1", "p2"} {
		s := &sizes{typed: typed{done: make(chan struct{})}}
		got = append(got, s)
		sh.set(id, openShell(s, vt.DefaultPalette(), quiet))
		t.Cleanup(func() { _ = sh.get(id).t.Close() })
	}
	panes := []Pane{{ID: "p1", Title: "Terminal 1"}, {ID: "p2", Title: "Terminal 2"}}
	publish(State{Panes: panes, Stage: &Box{Pane: "p1"}, Focus: "p1"})
	// The second slides in beside the first, frame by frame.
	publish(State{Panes: panes, Focus: "p2", Stage: &Box{ID: "s1", Share: 0.5, Opening: true, A: &Box{Pane: "p1"}, B: &Box{Pane: "p2"}}})
	for range 60 {
		lastWindow.Frame(time.Second / 60)
	}
	for i, s := range got {
		s.mu.Lock()
		for _, size := range s.seen {
			if size[0] < leastCols || size[1] < leastRows {
				t.Errorf("shell %d was given %dx%d on the way", i+1, size[0], size[1])
			}
		}
		s.mu.Unlock()
	}
}

// nextIntent is the next intent the window sends, or fails.
func nextIntent(t *testing.T) gunim.Intent {
	t.Helper()
	select {
	case env := <-lastWindow.Client().Intents():
		return env.Intent
	case <-time.After(time.Second):
		t.Fatal("the window sent nothing")
		return nil
	}
}

func TestTheSidebarWorksFromTheKeyboard(t *testing.T) {
	win, _, publish := windowStage(t)
	publish(twoPanes("p2", nil))
	// Drained: focusing a pane says so.
	for len(lastWindow.Client().Intents()) > 0 {
		<-lastWindow.Client().Intents()
	}
	press := func(k gi.Key) {
		lastWindow.Input(gi.KeyPress{Key: k})
		lastWindow.Frame(time.Second / 60)
	}
	lastWindow.Input(gi.KeyPress{Key: gi.KeyL, Mods: gi.ModControl | gi.ModShift})
	lastWindow.Frame(time.Second / 60)
	focused := func() string {
		for _, k := range win.list.Keys() {
			if row, ok := widget.RowOf[*sideRow](win.list, k); ok && row.ring.Target() == 1 {
				return string(k)
			}
		}
		return ""
	}
	if got := focused(); got != "p2" {
		t.Fatalf("focusing the sidebar lit %q, want the focused pane's row", got)
	}
	press(gi.KeyUp)
	if got := focused(); got != "p1" {
		t.Fatalf("up went to %q", got)
	}
	press(gi.KeyEnter)
	if in, ok := nextIntent(t).(FocusPane); !ok || in.Pane != "p1" {
		t.Fatalf("Enter sent %#v", in)
	}
	press(gi.KeyDelete)
	if in, ok := nextIntent(t).(ClosePane); !ok || in.Pane != "p1" {
		t.Fatalf("Delete sent %#v", in)
	}
}

func TestClearFinishedClosesEndedPanes(t *testing.T) {
	a, _ := agentApp(t)
	if err := a.open("", placement{}); err != nil {
		t.Fatal(err)
	}
	ended := a.st.Panes[0].ID
	shellEnds(t, a, ended, "0")
	a.handle(ClearFinished{})
	waitFor(t, a, "the ended pane to close", func() bool { return !a.has(ended) })
	if len(a.st.Panes) != 1 {
		t.Fatalf("cleared, the panes are %+v", a.st.Panes)
	}
}

func TestTheTunnelDialogOffersTheTunnelsSavedForTheServer(t *testing.T) {
	win, sh, publish := windowStage(t)
	quiet := shellHooks{output: func() {}, title: func(string) {}, exit: func() {}, clipboard: func(string) {}}
	s := &typed{done: make(chan struct{})}
	sh.set("p1", openShell(s, vt.DefaultPalette(), quiet))
	t.Cleanup(func() { _ = sh.get("p1").t.Close() })
	publish(State{
		Panes: []Pane{{ID: "p1", Title: "Terminal 1", Machine: "srv"}}, Stage: &Box{Pane: "p1"}, Focus: "p1",
		SavedTunnels: []settings.SavedTunnel{
			{Host: "srv", Kind: "remote", Listen: ":8080", Target: "127.0.0.1:80"},
			{Host: "other", Kind: "local", Listen: ":9000", Target: "db:5432"},
		},
	})
	win.tunnelDialog(false, lastUI)
	for range 3 {
		lastWindow.Frame(time.Second / 60)
	}
	fields := win.dialog.Body.(*widget.Form).Focusables()
	if len(fields) != 5 {
		t.Fatalf("the dialog has %d fields, want listen, target, direction, saved and the box", len(fields))
	}
	pick := fields[3].(*widget.Dropdown)
	if len(pick.Items) != 2 || pick.Items[1] != "remote :8080 → 127.0.0.1:80" {
		t.Fatalf("Saved offers %q, want the one tunnel kept for srv", pick.Items)
	}
	lastUI.Focus(pick)
	for _, k := range []gi.Key{gi.KeyDown, gi.KeyDown, gi.KeyEnter} {
		lastWindow.Input(gi.KeyPress{Key: k})
		lastWindow.Frame(time.Second / 60)
	}
	listen, target := fields[0].(*widget.TextField), fields[1].(*widget.TextField)
	if listen.Text() != ":8080" || target.Text() != "127.0.0.1:80" || fields[2].(*widget.Dropdown).Selected != 1 {
		t.Fatalf("picked, the form reads %q, %q, direction %d", listen.Text(), target.Text(), fields[2].(*widget.Dropdown).Selected)
	}
}

func TestAMachinesPlusOpensWhatCanBeOpenedThere(t *testing.T) {
	win, _, publish := windowStage(t)
	publish(twoPanes("p2", nil))
	for len(lastWindow.Client().Intents()) > 0 {
		<-lastWindow.Client().Intents()
	}
	row, ok := widget.RowOf[*sideRow](win.list, "machine:")
	if !ok {
		t.Fatal("this computer has no heading")
	}
	box, _ := lastUI.Bounds(row)
	lastWindow.Input(gi.PointerDown{Pos: geom.Pt(box.Max.X-20, box.Min.Y+box.Size().H/2), Button: gi.ButtonPrimary, Clicks: 1})
	lastWindow.Input(gi.PointerUp{Pos: geom.Pt(box.Max.X-20, box.Min.Y+box.Size().H/2), Button: gi.ButtonPrimary})
	lastWindow.Frame(time.Second / 60)
	for _, k := range []gi.Key{gi.KeyDown, gi.KeyEnter} {
		lastWindow.Input(gi.KeyPress{Key: k})
		lastWindow.Frame(time.Second / 60)
	}
	if in, ok := nextIntent(t).(OpenOn); !ok || in.Machine != "" {
		t.Fatalf("the first line of this computer's menu sent %#v", in)
	}
}
