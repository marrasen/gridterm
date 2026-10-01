package view

import (
	"testing"

	"github.com/marrasen/kakel/app"
	"github.com/marrasen/kakel/remote"
)

// A server saved with a new user, port or key is shown so the next
// time it is edited, not as it was.
func TestAnEditedServerIsEditedAsSaved(t *testing.T) {
	win, _, publish := windowStage(t)
	h := remote.Host{ID: "s1", Name: "web", Address: "web.example", User: "old"}
	publish(app.State{Saved: []remote.Host{h}})
	h.User, h.Port, h.Identities = "new", 2200, []string{"/k"}
	publish(app.State{Saved: []remote.Host{h}})
	if got := win.saved[0]; got.User != "new" || got.Port != 2200 || len(got.Identities) != 1 {
		t.Fatalf("the window keeps %+v", got)
	}
}

// A question offering saved secrets answers with the one picked, last,
// and with -1 when none is.
func TestAQuestionOffersSavedSecrets(t *testing.T) {
	win, _, publish := windowStage(t)
	ask := app.Ask{ID: 7, Title: "Sign in to me@srv", Prompts: []string{"Password"}, Secret: []bool{true}, Yes: "Sign in",
		Also: "Save this password in the secrets", Saved: []string{"one", "two"}}
	publish(app.State{Asks: []app.Ask{ask}})
	frames(20)
	if win.ask == nil {
		t.Fatal("the question did not open")
	}
	in, ok := win.ask.OnAccept().(app.AskAnswered)
	if !ok || len(in.Answers) != 3 || in.Answers[1] != "" || in.Answers[2] != "-1" {
		t.Fatalf("answered %#v, want the password, the box, then -1", in)
	}
}
