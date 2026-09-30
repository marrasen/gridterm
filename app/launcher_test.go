package app

import (
	"errors"
	"testing"
	"time"

	"github.com/marrasen/kakel/machines"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/gunimtest"
	gi "github.com/marrasen/gunim/input"
)

// The launcher's key is taken from every program, Shift+Win+K unless
// the settings say another, and a press opens the launcher once.
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
	if took.Key != gi.KeyK || took.Mods != gi.ModShift|gi.ModSuper || press == nil {
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
	if len(a.st.Notices) == 0 || a.st.Notices[len(a.st.Notices)-1].Title != "Couldn't take shift+super+k for the launcher" {
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
