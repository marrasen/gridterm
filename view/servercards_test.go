package view

import (
	"testing"

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
