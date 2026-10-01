package view

import (
	"testing"
	"time"

	"github.com/marrasen/kakel/app"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/gunimtest"
	gi "github.com/marrasen/gunim/input"
)

// launcherStage mounts the launcher on a window, with this computer and
// two servers, and returns it with its window.
func launcherStage(t *testing.T) (*Launcher, *gunim.Window) {
	t.Helper()
	w := gunimtest.New(t, LauncherSize, nil)
	var l *Launcher
	gunim.RegisterView(w, "launcher", func(app.LaunchState) *Launcher { l = NewLauncher(); return l },
		func(l *Launcher, st app.LaunchState, u *gunim.UI) { l.Update(st, u) })
	c := w.Client()
	if err := c.Mount(gunim.Root, "launcher", "launcher", app.LaunchState{}, app.LauncherTopic); err != nil {
		t.Fatal(err)
	}
	acts := []app.LaunchAction{{ID: "terminal", Title: "Terminal"}, {ID: "files", Title: "Files"}}
	st := app.LaunchState{Opened: 1, Machines: []app.LaunchMachine{
		{ID: "", Name: "This computer", Actions: acts},
		{ID: "a", Name: "prod-web", Actions: acts, Note: "connected"},
		{ID: "b", Name: "backup", Actions: acts, Default: 1},
	}}
	if err := c.Publish(app.LauncherTopic, st); err != nil {
		t.Fatal(err)
	}
	for range 5 {
		w.Frame(time.Second / 60)
	}
	return l, w
}

// next is the launcher window's next intent.
func next(t *testing.T, w *gunim.Window) gunim.Intent {
	t.Helper()
	select {
	case env := <-w.Client().Intents():
		return env.Intent
	case <-time.After(time.Second):
		t.Fatal("the launcher sent nothing")
		return nil
	}
}

// Typing finds a machine, and Enter opens what was opened there last.
func TestTheLauncherFindsAMachineByItsName(t *testing.T) {
	_, w := launcherStage(t)
	for _, r := range "back" {
		w.Input(gi.TextInput{Text: string(r)})
	}
	w.Input(gi.KeyPress{Key: gi.KeyEnter})
	w.Frame(time.Second / 60)
	if in := next(t, w); in != (app.Launch{Machine: "b", Action: "files"}) {
		t.Fatalf("Enter sent %#v", in)
	}
}

// Tab shows what else can be opened on the machine lit, Down and Enter
// pick one; Escape goes back to the machines, and again closes it.
func TestTheLauncherOffersTheRest(t *testing.T) {
	_, w := launcherStage(t)
	press := func(k gi.Key) {
		w.Input(gi.KeyPress{Key: k})
		w.Frame(time.Second / 60)
	}
	press(gi.KeyDown)
	press(gi.KeyTab)
	press(gi.KeyDown)
	press(gi.KeyEnter)
	if in := next(t, w); in != (app.Launch{Machine: "a", Action: "files"}) {
		t.Fatalf("the second thing on the second machine sent %#v", in)
	}
	// Back to the machines, and out.
	press(gi.KeyEscape)
	press(gi.KeyEscape)
	if in := next(t, w); in != (app.CloseLauncher{}) {
		t.Fatalf("Escape sent %#v", in)
	}
}

// Typing lights the best of what it finds, wherever the light was.
func TestTypingLightsTheBestMatch(t *testing.T) {
	_, w := launcherStage(t)
	w.Input(gi.KeyPress{Key: gi.KeyDown})
	w.Input(gi.KeyPress{Key: gi.KeyDown})
	w.Input(gi.TextInput{Text: "p"})
	w.Input(gi.KeyPress{Key: gi.KeyEnter})
	w.Frame(time.Second / 60)
	if in := next(t, w); in != (app.Launch{Machine: "a", Action: "terminal"}) {
		t.Fatalf("Enter sent %#v", in)
	}
}

// A machine's things stay that machine's as the machines come in
// another order, and a right click picks nothing.
func TestTheLauncherKeepsItsMachine(t *testing.T) {
	l, w := launcherStage(t)
	w.Input(gi.KeyPress{Key: gi.KeyDown})
	w.Input(gi.KeyPress{Key: gi.KeyTab})
	st := l.st
	st.Machines = []app.LaunchMachine{st.Machines[0], st.Machines[2], st.Machines[1]}
	if err := w.Client().Publish(app.LauncherTopic, st); err != nil {
		t.Fatal(err)
	}
	w.Frame(time.Second / 60)
	w.Input(gi.KeyPress{Key: gi.KeyEnter})
	w.Frame(time.Second / 60)
	if in := next(t, w); in != (app.Launch{Machine: "a", Action: "terminal"}) {
		t.Fatalf("Enter sent %#v", in)
	}
	w.Input(gi.PointerDown{Pos: geom.Pt(100, l.listTop+5), Button: gi.ButtonSecondary})
	w.Frame(time.Second / 60)
	select {
	case env := <-w.Client().Intents():
		t.Fatalf("a right click sent %#v", env.Intent)
	case <-time.After(50 * time.Millisecond):
	}
}

// Typing finds more than machines: a shell here by its name or a word
// for it, files on a machine, and Enter opens it.
func TestTheLauncherFindsShellsAndFiles(t *testing.T) {
	l, w := launcherStage(t)
	st := l.st
	st.Things = []app.LaunchThing{
		{Title: "Command Prompt", Note: "This computer", Also: []string{"cmd"}, Kind: "terminal", Action: "shell:cmd"},
		{Title: "Ubuntu (WSL)", Note: "This computer", Also: []string{"wsl:Ubuntu", "wsl"}, Kind: "terminal", Action: "shell:wsl:Ubuntu"},
		{Title: "Files on backup", Kind: "files", Machine: "b", Action: "files"},
	}
	if err := w.Client().Publish(app.LauncherTopic, st); err != nil {
		t.Fatal(err)
	}
	w.Frame(time.Second / 60)
	typeIn := func(s string) {
		for _, r := range s {
			w.Input(gi.TextInput{Text: string(r)})
		}
		w.Input(gi.KeyPress{Key: gi.KeyEnter})
		w.Frame(time.Second / 60)
	}
	typeIn("wsl")
	if in := next(t, w); in != (app.Launch{Action: "shell:wsl:Ubuntu"}) {
		t.Fatalf("wsl and Enter sent %#v", in)
	}
	l.field.SetText("")
	typeIn("files on b")
	if in := next(t, w); in != (app.Launch{Machine: "b", Action: "files"}) {
		t.Fatalf("files on b and Enter sent %#v", in)
	}
}
