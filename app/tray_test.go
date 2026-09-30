package app

import (
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
	a.closeWindow(two)
	if len(a.st.Asks) == 0 || a.st.Asks[0].Title != "Close this window?" {
		t.Fatalf("closing the last window but one asked %+v", a.st.Asks)
	}
	a.st.Asks = nil
	for _, p := range a.panesIn(two) {
		a.remove(p.ID)
	}
	a.letWindowGo(two)
	a.front(one)
	for _, p := range a.panesIn(one) {
		a.remove(p.ID)
	}
	a.leaveIfEmpty()
	if a.gone || !one.gone {
		t.Fatalf("empty in the tray, kakel left %v, its window stayed %v", a.gone, !one.gone)
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
	a.handover(single.Handover{Args: []string{"-e", "true"}, Dir: t.TempDir()})
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
}
