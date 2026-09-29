package view

import (
	"testing"
	"time"
	"unicode"

	"github.com/marrasen/kakel/app"

	"github.com/marrasen/gunim/geom"
	gi "github.com/marrasen/gunim/input"
	"github.com/marrasen/gunim/widget"
)

// accessKeyOf is the letter a menu line is picked by, and whether it
// has one.
func accessKeyOf(s string) (rune, bool) {
	rs := []rune(s)
	for i := 0; i < len(rs)-1; i++ {
		if rs[i] == '&' {
			if rs[i+1] == '&' {
				i++
				continue
			}
			return unicode.ToLower(rs[i+1]), true
		}
	}
	return 0, false
}

// Every menu has a letter of its own on the bar, and every line in a
// menu a letter no other line in it has, so Alt and the letters reach
// each one.
func TestEveryMenuLineHasItsOwnAccessKey(t *testing.T) {
	win, _, publish := windowStage(t)
	publish(app.State{Fonts: []string{"Go Mono (bundled)", "PxPlus IBM VGA8", "DejaVu Sans Mono"}})
	titles := map[rune]string{}
	for _, m := range win.bar.Menus {
		k, ok := accessKeyOf(m.Title)
		if !ok || titles[k] != "" {
			t.Errorf("the menu %q has access key %q, which %q has too", m.Title, k, titles[k])
		}
		titles[k] = m.Title
		lines := map[rune]string{}
		for i, it := range m.Items {
			if isCaption(m, i) {
				continue
			}
			k, ok := accessKeyOf(it)
			if !ok || lines[k] != "" {
				t.Errorf("in %q, %q has access key %q, which %q has too", shownText(m.Title), it, k, lines[k])
			}
			lines[k] = it
		}
	}
}

// A & in a line's own text shows, and marks no key.
func TestAnAmpersandInALineShows(t *testing.T) {
	m := withAccessKeys(widget.BarMenu{Title: "Servers", Items: []string{"R&D box"}})
	if got := shownText(m.Items[0]); got != "R&D box" {
		t.Fatalf("the line shows as %q", got)
	}
	if k, _ := accessKeyOf(m.Items[0]); k != 'r' {
		t.Fatalf("its access key is %q", k)
	}
}

// Alt and a letter in a terminal is the terminal's, as a shell moves by
// words with it; the menus open by it only where it is nobody's.
func TestAltAndALetterInATerminalIsTheTerminals(t *testing.T) {
	win, tm := termStage(t, geom.Sz(900, 600))
	lastUI.Focus(tm)
	lastWindow.Input(gi.KeyPress{Key: gi.KeyF, Mods: gi.ModAlt, Time: time.Now()})
	lastWindow.Frame(time.Second / 60)
	if win.bar.IsOpen() {
		t.Fatal("Alt+F in a terminal opened a menu")
	}
	lastUI.Focus(nil)
	lastWindow.Input(gi.KeyPress{Key: gi.KeyF, Mods: gi.ModAlt, Time: time.Now()})
	lastWindow.Frame(time.Second / 60)
	if !win.bar.IsOpen() {
		t.Fatal("Alt+F with the keyboard nowhere opened no menu")
	}
}

// A menu line that cannot act on the pane in front is greyed: Disconnect
// and Go to Directory on a terminal here, and they come back for a file
// pane on a server.
func TestMenuLinesThatCannotActAreGreyed(t *testing.T) {
	win, _, publish := windowStage(t)
	greyed := func(id string) bool {
		for m := range menus {
			for i, it := range menus[m].items {
				if it.id == id {
					return win.bar.Menus[m].Disabled[i]
				}
			}
		}
		t.Fatalf("no menu line runs %s", id)
		return false
	}
	publish(app.State{Panes: []app.Pane{{ID: "p1", Title: "here", Kind: app.KindFiles}}, Stage: &app.Box{Pane: "p1"}, Focus: "p1",
		Browsers: map[string]app.Browser{"p1": {Path: "/"}}})
	if !greyed("conn.disconnect") || greyed("files.goTo") || !greyed("pane.scrollback") {
		t.Fatalf("on a file pane here: Disconnect greyed %v, Go to Directory %v, Find in Scrollback %v",
			greyed("conn.disconnect"), greyed("files.goTo"), greyed("pane.scrollback"))
	}
	publish(app.State{Panes: []app.Pane{{ID: "p1", Title: "there", Kind: app.KindFiles, Machine: "srv"}}, Stage: &app.Box{Pane: "p1"}, Focus: "p1",
		Browsers: map[string]app.Browser{"p1": {Path: "/"}}})
	if greyed("conn.disconnect") || greyed("conn.log") {
		t.Fatal("on a pane on a server, Disconnect or its log is greyed")
	}
}

// The lines always in the Servers menu keep their letters, whatever the
// saved servers are called.
func TestTheServersMenusOwnLinesKeepTheirLetters(t *testing.T) {
	m := withAccessKeys(widget.BarMenu{Title: "Servers",
		Items: []string{"quark", "alpha", "rho", "Quick Connect…", "Add Server…", "Reload Server List"}}, 3, 4, 5)
	for i, want := range map[int]rune{3: 'q', 4: 'a', 5: 'r'} {
		if k, _ := accessKeyOf(m.Items[i]); k != want {
			t.Errorf("%q has access key %q, want %q", shownText(m.Items[i]), k, want)
		}
	}
}

// A click in a file pane's list makes it the pane in front, as a click
// in a terminal does: the menus act on it.
func TestTheKeyboardComingIntoAFilePaneFrontsIt(t *testing.T) {
	win, _, publish := windowStage(t)
	publish(app.State{Panes: []app.Pane{{ID: "p1", Title: "a", Kind: app.KindFiles}, {ID: "p2", Title: "b", Kind: app.KindFiles}},
		Stage: &app.Box{ID: "s1", A: &app.Box{Pane: "p1"}, B: &app.Box{Pane: "p2"}, Share: 0.5}, Focus: "p1",
		Browsers: map[string]app.Browser{"p1": {Path: "/"}, "p2": {Path: "/"}}})
	for len(lastWindow.Client().Intents()) > 0 {
		<-lastWindow.Client().Intents()
	}
	lastUI.Focus(win.browsers["p2"].table)
	if in, ok := nextIntent(t).(app.FocusPane); !ok || in.Pane != "p2" {
		t.Fatalf("the keyboard coming into p2 sent %#v", in)
	}
}
