package main

import (
	"sync"
	"testing"
	"time"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/widget"

	"github.com/marrasen/gridterm/logs"
	"github.com/marrasen/gridterm/vt"
)

// lastWindow is the offscreen window windowStage made last, for a test
// that asks what the window's frame was told.
var lastWindow *gunim.Window

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
	}, func(win *window, st State, u *gunim.UI) { win.update(st, u) })
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
