package view

import (
	"testing"
	"time"

	"github.com/marrasen/kakel/app"
	"github.com/marrasen/kakel/machines"
	"github.com/marrasen/kakel/remote"

	"github.com/marrasen/gunim/geom"
	gi "github.com/marrasen/gunim/input"
)

// cardsStage is a window with the Servers pane in front, two servers
// saved, of size.
func cardsStage(t *testing.T, size geom.Size) *Window {
	t.Helper()
	win, _, publish := windowStageOf(t, size)
	st := withServers(app.State{Focus: "ps"})
	st.Saved = []remote.Host{
		{ID: "s1", Name: "web", Address: "web.example", User: "deploy"},
		{ID: "s2", Name: "db", Address: "10.0.0.12", User: "pg", Port: 2222},
	}
	st.Machines = []machines.Info{{ID: "s1", Name: "web"}, {ID: "s2", Name: "db"}}
	publish(st)
	settle()
	return win
}

// The cards say who and where each server is, and the keyboard goes
// along them by where they stand: Down to the card below, Right to the
// one beside it. Enter opens the menu of what opens there, its first
// line a terminal.
func TestTheServerCardsTakeTheKeyboard(t *testing.T) {
	win := cardsStage(t, geom.Sz(1100, 600))
	db, ok := win.cards.cards["s2"]
	if !ok || db.info.sub != "pg@10.0.0.12:2222" || db.info.state != cardOff || db.info.section != sectionSaved {
		t.Fatalf("db's card is %+v", db)
	}
	here := win.cards.cards[machines.Local]
	lastUI.Focus(here.head)
	frames(2)
	lastWindow.Input(gi.KeyPress{Key: gi.KeyDown})
	frames(1)
	first, second := win.cards.cards[win.cards.order[1]], win.cards.cards[win.cards.order[2]]
	if lastUI.Focused() != first.head {
		t.Fatalf("Down left the keyboard with %T", lastUI.Focused())
	}
	lastWindow.Input(gi.KeyPress{Key: gi.KeyRight})
	frames(1)
	if lastUI.Focused() != second.head {
		t.Fatalf("Right left the keyboard with %T", lastUI.Focused())
	}
	drain()
	lastWindow.Input(gi.KeyPress{Key: gi.KeyEnter})
	frames(2)
	if second.head.menu == nil || second.head.menu.Items[1] != "New Terminal" {
		t.Fatalf("Enter on a card opened %+v", second.head.menu)
	}
	lastWindow.Input(gi.KeyPress{Key: gi.KeyDown})
	lastWindow.Input(gi.KeyPress{Key: gi.KeyEnter})
	frames(1)
	if in, ok := nextIntent(t).(app.OpenOn); !ok || in.Machine != second.id {
		t.Fatalf("the menu's first line sent %#v", in)
	}
	// Ctrl+PageDown is the window's, from a card too.
	lastUI.Focus(second.head)
	frames(1)
	if second.head.cardKey(gi.KeyPress{Key: gi.KeyPageDown, Mods: gi.ModControl}, lastUI) {
		t.Fatal("a card took Ctrl+PageDown")
	}
	// / goes to the search field, and Enter there to the first found.
	lastWindow.Input(gi.TextInput{Text: "/"})
	frames(1)
	if lastUI.Focused() != win.serversView.search || win.serversView.search.Text() != "" {
		t.Fatalf("/ left the keyboard with %T, the field saying %q", lastUI.Focused(), win.serversView.search.Text())
	}
	lastWindow.Input(gi.TextInput{Text: "db"})
	frames(2)
	lastWindow.Input(gi.KeyPress{Key: gi.KeyEnter})
	frames(1)
	if lastUI.Focused() != win.cards.cards["s2"].head {
		t.Fatalf("Enter in the field left the keyboard with %T", lastUI.Focused())
	}
}

// Narrow, each card is a line, its buttons icons at its end.
func TestNarrowCardsAreLines(t *testing.T) {
	win := cardsStage(t, geom.Sz(360, 600))
	if !win.cards.compact {
		t.Fatal("a narrow pane's cards are not compact")
	}
	head := win.cards.cards["s1"].head
	box, _ := lastUI.Bounds(head)
	if box.Size().H != compactHeadHeight {
		t.Fatalf("a compact card's header is %v tall", box.Size().H)
	}
	for i, b := range head.chips {
		r, _ := lastUI.Bounds(b)
		if r.Max.X > box.Max.X || r.Min.X < box.Min.X || i < 2 && (b.Label != "" || b.Tooltip == "") {
			t.Fatalf("button %d is at %v in a card at %v, labelled %q", i, r, box, b.Label)
		}
	}
}

func TestAConnectedPillSaysTheRoundTripAndHowLong(t *testing.T) {
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	c := cardInfo{state: cardConnected, since: now.Add(-(2*time.Hour + 5*time.Minute)), rtt: 23 * time.Millisecond}
	if got, _ := pillFor(c, now, nil); got != "23 ms · 2 h 5 min" {
		t.Fatalf("the pill says %q", got)
	}
	c.rtt = 0
	if got, _ := pillFor(c, now, nil); got != "Connected" {
		t.Fatalf("before a ping the pill says %q", got)
	}
	for d, want := range map[time.Duration]string{30 * time.Second: "just now", 12 * time.Minute: "12 min", 3 * time.Hour: "3 h", 50 * time.Hour: "2 d 2 h"} {
		if got := connectedFor(d); got != want {
			t.Errorf("connectedFor(%v) = %q, want %q", d, got, want)
		}
	}
}

// A machine named twice in the rows, as a tunnel through a window names
// the machine the window reaches, is one card.
func TestAMachineNamedTwiceIsOneCard(t *testing.T) {
	win := cardsStage(t, geom.Sz(1100, 600))
	rows := []sideItem{
		{key: "machine:", text: "This computer", heading: true},
		{key: "machine:w1", text: "far", heading: true},
		{key: "t1", text: "a tunnel"},
		{key: "machine:w1", text: "far", heading: true, depth: 1},
		{key: "p9", text: "a pane"},
	}
	win.cards.sync(rows, lastUI)
	frames(2)
	if n := len(win.cards.order); n != 2 {
		t.Fatalf("%d cards, want 2", n)
	}
	if c := win.cards.cards["w1"]; c == nil || len(c.items) != 2 {
		t.Fatalf("the machine's card holds %+v", c)
	}
}
