package app

import (
	"errors"
	"runtime"
	"slices"
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

// The launcher offers each shell here, WSL's files, files and a
// terminal on each machine, the favourites, and kakel's windows; a pick of one opens it
// and is not what Enter opens on the machine next time.
func TestTheLauncherOffersShellsFilesAndWindows(t *testing.T) {
	a, _, two := twoWindowApp(t)
	a.next = 100
	a.noteWork()
	a.st.Shells = []ShellChoice{{ID: "cmd", Title: "Command Prompt"}, {ID: "wsl:Ubuntu", Title: "Ubuntu (WSL)", Folder: `\\wsl$\Ubuntu`}}
	a.st.Favourites = []Favourite{{Path: "/home/me/src", Name: "Code"}}
	things := a.launchThings(a.launchMachines())
	titles := map[string]LaunchThing{}
	for _, th := range things {
		titles[th.Title] = th
	}
	for _, want := range []string{"Command Prompt", "Ubuntu (WSL)", "Files in Ubuntu (WSL)", "Terminal on This computer", "Files on This computer", "Servers", "Secrets", "New Window"} {
		if _, ok := titles[want]; !ok {
			t.Fatalf("the launcher offers no %q among %v", want, things)
		}
	}
	if th := titles["Code"]; th.Action != "files:/home/me/src" || th.Note != "This computer" || !slices.Contains(th.Also, "/home/me/src") {
		t.Fatalf("a favourite is offered as %+v", th)
	}
	if th := titles["Ubuntu (WSL)"]; !slices.Contains(th.Also, "wsl") {
		t.Fatalf("WSL is not found by wsl: %+v", th)
	}
	a.handleLaunch(Launch{Action: "app:servers"})
	if a.serversPane() == "" || a.cur != two {
		t.Fatalf("Servers did not open in the window worked in: %q in window %d", a.serversPane(), a.cur.id)
	}
	if ms := a.launchMachines(); ms[0].Default != 0 {
		t.Fatal("opening Servers became what Enter opens on this computer")
	}
}

// New Window from the launcher opens one window, also with kakel in the
// tray and none open.
func TestNewWindowFromTheLauncherOpensOne(t *testing.T) {
	a, one, two := twoWindowApp(t)
	a.traySet = (&fakeTray{}).tray()
	a.publish()
	opened := 0
	a.openWindow = func(_ *gunim.Window, _ geom.Point, s geom.Size) (gunim.Client, *gunim.Window, error) {
		opened++
		return gunimtest.New(t, s, nil).Client(), nil, nil
	}
	for _, w := range []*ownWin{one, two} {
		for _, p := range a.panesIn(w) {
			a.remove(p.ID)
		}
		a.letWindowGo(w)
	}
	a.handleLaunch(Launch{Action: "app:window"})
	waitFor(t, a, "the window", func() bool { return len(a.liveWins()) > 0 })
	time.Sleep(50 * time.Millisecond)
	if opened != 1 || a.opening != 0 {
		t.Fatalf("New Window opened %d windows, %d on their way", opened, a.opening)
	}
}

// Files asked for from the tray or the launcher, where files open in a
// window, open there alone: no kakel window opens for them.
func TestFilesFromTheTrayOpenNoKakelWindow(t *testing.T) {
	a, one, two := twoWindowApp(t)
	a.traySet = (&fakeTray{}).tray()
	a.settings = mustSettings(t)
	if err := a.settings.PutFilesInWindow(true); err != nil {
		t.Fatal(err)
	}
	files := &fakeFiles{}
	a.files = files
	a.publish()
	opened := 0
	a.openWindow = func(_ *gunim.Window, _ geom.Point, s geom.Size) (gunim.Client, *gunim.Window, error) {
		opened++
		return gunimtest.New(t, s, nil).Client(), nil, nil
	}
	for _, w := range []*ownWin{one, two} {
		for _, p := range a.panesIn(w) {
			a.remove(p.ID)
		}
		a.letWindowGo(w)
	}
	a.handleLaunch(Launch{Action: "files"})
	a.filesFromOutside("", "/tmp")
	time.Sleep(50 * time.Millisecond)
	if len(files.opened) != 2 || opened != 0 || a.opening != 0 {
		t.Fatalf("files opened %d windows of their own, and %d kakel windows opened, %d on their way", len(files.opened), opened, a.opening)
	}
}
