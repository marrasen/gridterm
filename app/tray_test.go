package app

import (
	"errors"
	"testing"
	"time"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/gunimtest"
	"github.com/marrasen/kakel/single"
)

// fakeTray records what the program shows in the tray.
type fakeTray struct {
	shown []gunim.Tray
	stay  []bool
	err   error
}

func (f *fakeTray) tray() Tray {
	return Tray{
		Set: func(t gunim.Tray) error {
			if f.err != nil {
				return f.err
			}
			f.shown = append(f.shown, t)
			return nil
		},
		StayOpen: func(on bool) { f.stay = append(f.stay, on) },
	}
}

// titles are the titles of a tray menu's lines.
func titles(items []gunim.TrayItem) []string {
	var out []string
	for _, it := range items {
		out = append(out, it.Title)
	}
	return out
}

// kakel shows its icon in the tray, with a menu of the machines, and
// keeps running there; the icon is made again only when the menu would
// change.
func TestKakelShowsItselfInTheTray(t *testing.T) {
	a, _, _ := twoWindowApp(t)
	f := &fakeTray{}
	a.traySet = f.tray()
	a.publish()
	a.publish()
	if len(f.shown) != 1 || !a.inTray() || len(f.stay) == 0 || !f.stay[len(f.stay)-1] {
		t.Fatalf("shown %d times, in the tray %v, staying %v", len(f.shown), a.inTray(), f.stay)
	}
	got := titles(f.shown[0].Items)
	if len(got) < 5 || got[0] != "Servers" || got[2] != "This computer" || got[len(got)-1] != "Quit kakel" {
		t.Fatalf("the menu is %v", got)
	}
	a.leaveTray()
	if a.inTray() || len(f.shown[len(f.shown)-1].Icon) != 0 {
		t.Fatal("the icon stayed as kakel left")
	}
}

// Where there is no tray, kakel is as it was: the last window ends it.
func TestWithoutATrayTheLastWindowEndsKakel(t *testing.T) {
	a, one, two := twoWindowApp(t)
	f := &fakeTray{err: gunim.ErrNoTray}
	a.traySet = f.tray()
	a.publish()
	if a.inTray() {
		t.Fatal("kakel is in a tray there is none of")
	}
	for _, p := range a.panesIn(one) {
		a.remove(p.ID)
	}
	a.letWindowGo(one)
	a.front(two)
	a.closeWindow(two)
	waitFor(t, a, "the question", func() bool { return len(a.st.Asks) > 0 })
	if a.st.Asks[len(a.st.Asks)-1].Title != "Exit kakel?" {
		t.Fatalf("closing the last window asked %+v", a.st.Asks)
	}
}

// In the tray, the last window closes as any other does, and kakel
// stays once its panes have gone.
func TestInTheTrayTheLastWindowOnlyCloses(t *testing.T) {
	a, one, two := twoWindowApp(t)
	f := &fakeTray{}
	a.traySet = f.tray()
	a.publish()
	for _, p := range a.panesIn(two) {
		a.remove(p.ID)
	}
	a.letWindowGo(two)
	a.front(one)
	a.closeWindow(one)
	waitFor(t, a, "the question", func() bool { return len(a.st.Asks) > 0 })
	if q := a.st.Asks[len(a.st.Asks)-1]; q.Title != "Close this window?" {
		t.Fatalf("closing the last window asked %q", q.Title)
	}
	a.st.Asks = nil
	for _, p := range a.panesIn(one) {
		a.remove(p.ID)
	}
	a.leaveIfEmpty()
	if a.gone || !one.gone {
		t.Fatalf("empty in the tray, kakel left %v, its window stayed %v", a.gone, !one.gone)
	}
}

// A pane that arrives after the last window closed in the tray, as a
// shell once its server answers, gets a window of its own, as does a
// question.
func TestAPaneWithNoWindowGetsOne(t *testing.T) {
	a, one, two := twoWindowApp(t)
	a.traySet = (&fakeTray{}).tray()
	a.publish()
	a.openWindow = func(_ *gunim.Window, _ geom.Point, s geom.Size) (gunim.Client, *gunim.Window, error) {
		return gunimtest.New(t, s, nil).Client(), nil, nil
	}
	for _, w := range []*ownWin{one, two} {
		for _, p := range a.panesIn(w) {
			a.remove(p.ID)
		}
		a.letWindowGo(w)
	}
	a.addPane(Pane{ID: "late", Kind: KindFiles}, nil, Placement{})
	a.rehome()
	waitFor(t, a, "a window for it", func() bool { a.rehome(); return a.ownerOf("late") != nil && !a.ownerOf("late").gone })
	if w := a.ownerOf("late"); a.focusIn(w) != "late" {
		t.Fatalf("the new window has %q in front", a.focusIn(w))
	}
}

// A tray that refuses is not asked again and again, and says so once.
func TestATrayThatRefusesIsAskedOnce(t *testing.T) {
	a, _, _ := twoWindowApp(t)
	f := &fakeTray{err: errors.New("the taskbar is not up")}
	a.traySet = f.tray()
	calls := 0
	a.traySet.Set = func(gunim.Tray) error { calls++; return f.err }
	for range 5 {
		a.publish()
	}
	failed := 0
	for _, n := range a.st.Notices {
		if n.Title == "Couldn't show kakel in the tray" {
			failed++
		}
	}
	if calls != 1 || failed != 1 {
		t.Fatalf("the tray was asked %d times and the failure said %d times", calls, failed)
	}
}

// A command line handed over opens a window of its own, running what
// it asks for.
func TestAHandoverOpensAWindow(t *testing.T) {
	a, _, _ := twoWindowApp(t)
	a.next = 100
	a.openWindow = func(_ *gunim.Window, _ geom.Point, s geom.Size) (gunim.Client, *gunim.Window, error) {
		return gunimtest.New(t, s, nil).Client(), nil, nil
	}
	dir := t.TempDir()
	a.handover(single.Handover{Args: []string{"-e", "true"}, Dir: dir})
	select {
	case f := <-a.events:
		f()
	case <-time.After(time.Second):
		t.Fatal("no window opened")
	}
	if len(a.wins) != 3 || a.cur != a.wins[2] || len(a.panesIn(a.cur)) != 1 || a.nextDir != "" {
		t.Fatalf("%d windows, the new one holding %d panes", len(a.wins), len(a.panesIn(a.cur)))
	}
	if a.opts.command != "" {
		t.Fatal("the command handed over stayed in kakel's own options")
	}
	if c := a.commands[a.panesIn(a.cur)[0].ID]; c.dir != dir {
		t.Fatalf("the command runs in %q, want %q, where it was started", c.dir, dir)
	}
}
