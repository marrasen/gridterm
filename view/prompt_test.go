package view

import (
	"slices"
	"testing"
	"time"

	"github.com/marrasen/kakel/app"
	"github.com/marrasen/kakel/look"
	"github.com/marrasen/kakel/themes"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/driver"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/gunimtest"
	gi "github.com/marrasen/gunim/input"
)

// promptStage mounts the window that asks q, in th, at the size it
// opens at, and returns its view, the window and the window's UI.
func promptStage(t *testing.T, q app.Ask, th look.Themed) (*Prompt, *gunim.Window, *gunim.UI) {
	t.Helper()
	w := gunimtest.New(t, PromptSize(q, th.Theme), nil)
	if th.Name != "" {
		look.Register(w, []look.Themed{th})
		if err := w.Client().SetTheme(th.Name); err != nil {
			t.Fatal(err)
		}
	}
	var p *Prompt
	var ui *gunim.UI
	gunim.RegisterView(w, "prompt", func(app.Ask) *Prompt { p = NewPrompt(); return p },
		func(p *Prompt, q app.Ask, u *gunim.UI) { ui = u; p.Update(q, u) })
	if err := w.Client().Mount(gunim.Root, "prompt", "prompt", q); err != nil {
		t.Fatal(err)
	}
	for range 30 {
		w.Frame(time.Second / 60)
	}
	return p, w, ui
}

// signIn is a question to type a password to, as a server asks.
var signIn = app.Ask{ID: 7, Title: "Sign in to me@srv", Icon: "log-in", Prompts: []string{"Password"}, Secret: []bool{true}, Yes: "Sign in"}

// What is typed is the answer Enter gives, from the field that has the
// keyboard as the window opens.
func TestAPromptAnswersWithWhatIsTyped(t *testing.T) {
	_, w, _ := promptStage(t, signIn, look.Themed{})
	w.Input(gi.TextInput{Text: "hunter2"})
	w.Input(gi.KeyPress{Key: gi.KeyEnter})
	w.Frame(time.Second / 60)
	in, ok := next(t, w).(app.AskAnswered)
	if !ok || in.ID != 7 || !in.Yes || !slices.Equal(in.Answers, []string{"hunter2"}) {
		t.Fatalf("Enter answered %#v", in)
	}
}

// Escape answers no.
func TestEscapeCancelsAPrompt(t *testing.T) {
	_, w, _ := promptStage(t, signIn, look.Themed{})
	w.Input(gi.TextInput{Text: "half"})
	w.Input(gi.KeyPress{Key: gi.KeyEscape})
	w.Frame(time.Second / 60)
	if in, ok := next(t, w).(app.AskAnswered); !ok || in.ID != 7 || in.Yes {
		t.Fatalf("Escape answered %#v", in)
	}
}

// The window opens as tall as its question, in every theme: everything
// in it shows, with nothing to scroll, and little room to spare under
// the buttons.
func TestAPromptFitsItsQuestion(t *testing.T) {
	var all []look.Themed
	for _, t := range themes.Built() {
		if th, err := look.Of(t); err == nil {
			all = append(all, th)
		}
	}
	if len(all) == 0 {
		t.Fatal("no theme to draw in")
	}
	for _, q := range []app.Ask{
		signIn,
		{ID: 1, Title: "Sign in to me@srv", Icon: "log-in", Text: "Invalid password.", Prompts: []string{"Password"}, Secret: []bool{true},
			Yes: "Sign in", Also: "Save this password in the secrets", Saved: []string{"one", "two"}},
		{ID: 2, Title: "Unlock your secrets", Icon: "lock", Prompts: []string{"Passphrase"}, Secret: []bool{true}, Yes: "Unlock",
			Facts:   []app.AskFact{{Label: "Saved for", Name: "me@srv", Note: "its password"}, {Label: "Opened by", Name: "id_ed25519", Note: "me@laptop"}},
			Problem: "That passphrase didn't open it. Try again."},
		{ID: 3, Title: "me@a-server-with-a-rather-long-name.example.com asks", Icon: "log-in",
			Text:    "Welcome. This machine is watched, and what you do on it is written down. Answer the two questions to go on.",
			Prompts: []string{"Code:", "PIN:"}, Secret: []bool{false, true}, Yes: "Answer"},
	} {
		for _, th := range all {
			p, _, ui := promptStage(t, q, th)
			size := PromptSize(q, th.Theme)
			buttons := p.dialog.Buttons()
			ok, drawn := ui.Bounds(buttons[len(buttons)-1])
			if !drawn || ok.Max.Y > size.H || size.H-ok.Max.Y > 48 {
				t.Errorf("%s in %s: OK is at %v in a window %v tall", q.Title, th.Name, ok, size.H)
			}
			sf := p.dialog.Body.(*scrollingForm)
			view, _ := ui.Bounds(sf.Scroll)
			form, _ := ui.Bounds(sf.form)
			if form.Size().H > view.Size().H+0.5 {
				t.Errorf("%s in %s: the form is %v tall in %v of room", q.Title, th.Name, form.Size().H, view.Size().H)
			}
		}
	}
}

// The window opens over the one the user works in, or in the middle of
// the main display, and stays on the screen.
func TestAPromptOpensWhereTheUserWorks(t *testing.T) {
	mons := []driver.Monitor{
		{Bounds: geom.Rc(0, 0, 1920, 1080), WorkArea: geom.Rc(0, 0, 1920, 1040), CoordsPerLogical: 1, Primary: true},
		{Bounds: geom.Rc(1920, 0, 2560, 1440), CoordsPerLogical: 1.5},
	}
	size := geom.Sz(420, 200)
	at := func(near *driver.Placement) geom.Rect { return PromptPlace(mons, near, size).Bounds }
	if r := at(nil); r.Center() != geom.Pt(960, 520) {
		t.Errorf("with no window it opens at %v", r)
	}
	if r := at(&driver.Placement{Bounds: geom.Rc(2000, 100, 1000, 800)}); r.Center() != geom.Pt(2500, 500) || r.Size() != geom.Sz(630, 300) {
		t.Errorf("over a window on the second display it opens at %v", r)
	}
	if r := at(&driver.Placement{Bounds: geom.Rc(2000, 100, 1000, 800), Maximized: true}); r.Center() != geom.Pt(3200, 720) {
		t.Errorf("over a maximized window it opens at %v", r)
	}
	if r := at(&driver.Placement{Bounds: geom.Rc(-100, 900, 300, 100)}); r.Min.X < 0 || r.Max.Y > 1040 {
		t.Errorf("over a window half off the screen it opens at %v", r)
	}
}
