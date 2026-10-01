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
// along them: Right to the next card, Enter does its first button.
func TestTheServerCardsTakeTheKeyboard(t *testing.T) {
	win := cardsStage(t, geom.Sz(1100, 600))
	db, ok := win.cards.cards["s2"]
	if !ok || db.info.sub != "pg@10.0.0.12:2222" || db.info.state != cardOff || db.info.section != sectionSaved {
		t.Fatalf("db's card is %+v", db)
	}
	here := win.cards.cards[machines.Local]
	lastUI.Focus(here.head)
	frames(2)
	lastWindow.Input(gi.KeyPress{Key: gi.KeyRight})
	frames(1)
	next := win.cards.cards[win.cards.order[1]]
	if lastUI.Focused() != next.head {
		t.Fatalf("Right left the keyboard with %T", lastUI.Focused())
	}
	drain()
	lastWindow.Input(gi.KeyPress{Key: gi.KeyEnter})
	frames(1)
	if in, ok := nextIntent(t).(app.ConnectTo); !ok || in.Server != next.id {
		t.Fatalf("Enter on a saved server sent %#v", in)
	}
	// / goes to the search field, and Down from it to the first card.
	lastWindow.Input(gi.TextInput{Text: "/"})
	frames(1)
	if lastUI.Focused() != win.serversView.search || win.serversView.search.Text() != "" {
		t.Fatalf("/ left the keyboard with %T, the field saying %q", lastUI.Focused(), win.serversView.search.Text())
	}
	lastWindow.Input(gi.KeyPress{Key: gi.KeyDown})
	frames(1)
	if lastUI.Focused() != win.cards.first() {
		t.Fatalf("Down from the field left the keyboard with %T", lastUI.Focused())
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
	chips := head.cardChips(box.Size(), lastUI.Theme())
	if last := chips[len(chips)-1]; last.Max.X > box.Size().W {
		t.Fatalf("its ⋯ is at %v, past its edge", last)
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
