package app

import (
	"errors"
	"runtime"
	"testing"
	"time"

	"github.com/marrasen/kakel/machines"
	"github.com/marrasen/kakel/single"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/gunimtest"
	gi "github.com/marrasen/gunim/input"
)

// The launcher's key is taken from every program, Shift+Win+K on
// Windows and Ctrl+Alt+K elsewhere unless the settings say another, and
// a press opens the launcher once.
func TestTheLauncherKeyOpensTheLauncher(t *testing.T) {
	a, _, _ := twoWindowApp(t)
	var took gunim.HotKey
	var press func()
	a.hotKeys = func(k gunim.HotKey, fn func()) (func(), error) {
		took, press = k, fn
		return func() {}, nil
	}
	opened := 0
	a.openLaunch = func() (gunim.Client, error) {
		opened++
		return gunimtest.New(t, geom.Sz(560, 380), nil).Client(), nil
	}
	a.takeLauncherKey()
	want := gi.ModControl | gi.ModAlt
	if runtime.GOOS == "windows" {
		want = gi.ModShift | gi.ModSuper
	}
	if took.Key != gi.KeyK || took.Mods != want || press == nil {
		t.Fatalf("the key taken is %+v", took)
	}
	press()
	press()
	for range 2 {
		select {
		case f := <-a.events:
			f()
		case <-time.After(time.Second):
			t.Fatal("the key did nothing")
		}
	}
	waitFor(t, a, "the launcher", func() bool { return a.launch.c != nil })
	if opened != 1 {
		t.Fatalf("the launcher opened %d times, want once", opened)
	}
}

// A key another program has is said, with where to choose another.
func TestATakenLauncherKeyIsSaid(t *testing.T) {
	a, _, _ := twoWindowApp(t)
	a.hotKeys = func(gunim.HotKey, func()) (func(), error) { return nil, gunim.ErrHotKeyTaken }
	a.takeLauncherKey()
	if len(a.st.Notices) == 0 || a.st.Notices[len(a.st.Notices)-1].Title != "Couldn't take "+DefaultLauncherKey+" for the launcher" {
		t.Fatalf("the notices are %+v", a.st.Notices)
	}
	a.hotKeys = func(gunim.HotKey, func()) (func(), error) { return nil, gunim.ErrNoHotKeys }
	was := len(a.st.Notices)
	a.takeLauncherKey()
	if len(a.st.Notices) != was {
		t.Fatal("a platform with no such keys was said to have failed")
	}
	a.hotKeys = func(gunim.HotKey, func()) (func(), error) { return nil, errors.New("odd") }
	if err := a.setLauncherKey("ctrl+nokey"); err == nil {
		t.Fatal("a key that is none was kept")
	}
}

// A pick in the launcher closes it, opens what was picked in the window
// last worked in, and is what Enter opens there next time.
func TestALaunchOpensWhereTheUserWorks(t *testing.T) {
	a, _, two := twoWindowApp(t)
	a.next = 100
	a.noteWork()
	a.handleLaunch(Launch{Machine: machines.Local, Action: "files"})
	if a.cur != two || a.kindOfPane(a.st.Focus) != KindFiles {
		t.Fatalf("window %d in front, the focus on a %q pane", a.cur.id, a.kindOfPane(a.st.Focus))
	}
	ms := a.launchMachines()
	if ms[0].ID != machines.Local || ms[0].Actions[ms[0].Default].ID != "files" {
		t.Fatalf("this computer offers %+v, Enter opening %d", ms[0].Actions, ms[0].Default)
	}
}

// kakel -launcher, handed over, opens the launcher and no window.
func TestALauncherHandoverOpensTheLauncher(t *testing.T) {
	a, _, _ := twoWindowApp(t)
	opened := 0
	a.openLaunch = func() (gunim.Client, error) {
		opened++
		return gunimtest.New(t, geom.Sz(560, 380), nil).Client(), nil
	}
	wins := len(a.wins)
	a.handover(single.Handover{Args: []string{"-launcher"}})
	waitFor(t, a, "the launcher", func() bool { return a.launch.c != nil })
	if opened != 1 || len(a.wins) != wins {
		t.Fatalf("the launcher opened %d times, and %d windows are open, were %d", opened, len(a.wins), wins)
	}
}

// A key with no Ctrl, Alt or Win, or one no platform lends, is refused
// before the key held goes; the key held is kept, and none takes none.
func TestTheLauncherKeyMustBeOneToTake(t *testing.T) {
	for _, k := range []string{"k", "shift+k", "ctrl+alt+left"} {
		if _, err := LauncherHotKey(k); err == nil {
			t.Errorf("%q was taken as a launcher's key", k)
		}
	}
	if _, err := LauncherHotKey(" Ctrl+Alt+K "); err != nil {
		t.Errorf("ctrl+alt+k was refused: %v", err)
	}
	a, _, _ := twoWindowApp(t)
	took := 0
	a.hotKeys = func(gunim.HotKey, func()) (func(), error) { took++; return func() {}, nil }
	a.takeLauncherKey()
	if err := a.setLauncherKey(DefaultLauncherKey); err != nil || took != 1 {
		t.Fatalf("the key held, set again, said %v and was taken %d times", err, took)
	}
	if err := a.setLauncherKey("k"); err == nil || a.launch.release == nil {
		t.Fatal("a bare letter was kept, or the key held went")
	}
	if err := a.setLauncherKey("None"); err != nil || a.launch.release != nil {
		t.Fatalf("none said %v, and the key is still held %v", err, a.launch.release != nil)
	}
}

// Once kakel is leaving, the launcher opens nothing.
func TestALaunchWhileLeavingOpensNothing(t *testing.T) {
	a, _, _ := twoWindowApp(t)
	a.leave()
	panes := len(a.st.Panes)
	a.handleLaunch(Launch{Machine: machines.Local, Action: "files"})
	if len(a.st.Panes) != panes {
		t.Fatal("a launch while leaving opened a pane")
	}
}
