package app

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/gunimtest"
)

// unlock is a question that wants something typed.
var unlock = Ask{Title: "Unlock your secrets", Icon: "lock", Prompts: []string{"Passphrase"}, Secret: []bool{true}, Yes: "Unlock"}

// promptApp is an app with two windows whose questions to type an answer
// to open in windows of their own, and the questions those are opened
// for.
func promptApp(t *testing.T) (*app, *[]Ask) {
	t.Helper()
	a, _, _ := twoWindowApp(t)
	opened := &[]Ask{}
	a.openPrompt = func(q Ask, _ string, _ *gunim.Window) (gunim.Client, error) {
		*opened = append(*opened, q)
		return gunimtest.New(t, geom.Sz(420, 200), nil).Client(), nil
	}
	return a, opened
}

// asked is the answer to a question asked on a goroutine of its own.
type asked struct {
	ans AskAnswered
	err error
}

// askAway asks q on a goroutine of its own, as a connection does, and
// waits for it to be asked.
func askAway(t *testing.T, a *app, ctx context.Context, q Ask) <-chan asked {
	t.Helper()
	got := make(chan asked, 1)
	was := len(a.st.Asks)
	go func() {
		ans, err := a.ask(ctx, q)
		got <- asked{ans, err}
	}()
	waitFor(t, a, "the question", func() bool { return len(a.st.Asks) > was })
	return got
}

// answerOf is what a question asked with askAway was answered.
func answerOf(t *testing.T, a *app, got <-chan asked) asked {
	t.Helper()
	for {
		select {
		case g := <-got:
			return g
		case f := <-a.events:
			f()
		case <-time.After(5 * time.Second):
			t.Fatal("the question was never answered")
		}
	}
}

// openPromptNow opens the window for the question waiting on one.
func openPromptNow(t *testing.T, a *app) {
	t.Helper()
	a.showPrompt()
	waitFor(t, a, "the window to ask in", func() bool { return a.prompt.c != nil })
}

// A question to type an answer to opens a window of its own, and is no
// dialog in a kakel window; answered there, the answer is what was
// typed, and the window closes.
func TestATypedQuestionOpensInAWindowOfItsOwn(t *testing.T) {
	a, opened := promptApp(t)
	got := askAway(t, a, t.Context(), unlock)
	openPromptNow(t, a)
	if len(*opened) != 1 || (*opened)[0].Title != unlock.Title {
		t.Fatalf("windows opened for %+v", *opened)
	}
	for _, w := range a.wins {
		if asks := a.stateFor(w, a.st).Asks; len(asks) > 0 {
			t.Fatalf("window %d asks %+v too", w.id, asks)
		}
	}
	id := a.prompt.id
	a.fromPrompt(id, AskAnswered{ID: id, Yes: true, Answers: []string{"opensesame"}})
	if g := answerOf(t, a, got); g.err != nil || !slices.Equal(g.ans.Answers, []string{"opensesame"}) {
		t.Fatalf("answered %+v, %v", g.ans, g.err)
	}
	a.showPrompt()
	if a.prompt.c != nil || a.prompt.id != 0 {
		t.Fatalf("the window stays, for question %d", a.prompt.id)
	}
	if len(*opened) != 1 {
		t.Fatalf("%d windows opened for one question", len(*opened))
	}
}

// The window closed before an answer, as by Alt+F4, answers no, and
// the next question waiting opens in a window of its own then.
func TestClosingThePromptCancelsIt(t *testing.T) {
	a, opened := promptApp(t)
	first := askAway(t, a, t.Context(), unlock)
	second := askAway(t, a, t.Context(), Ask{Title: "Sign in to me@srv", Prompts: []string{"Password"}, Secret: []bool{true}, Yes: "Sign in"})
	openPromptNow(t, a)
	a.showPrompt()
	if len(*opened) != 1 {
		t.Fatalf("%d windows open for two questions, want one at a time", len(*opened))
	}
	a.promptClosed(a.prompt.id, *a.prompt.c)
	if g := answerOf(t, a, first); !errors.Is(g.err, errDeclined) {
		t.Fatalf("closing the window answered %+v, %v", g.ans, g.err)
	}
	openPromptNow(t, a)
	if len(*opened) != 2 || (*opened)[1].Title != "Sign in to me@srv" {
		t.Fatalf("windows opened for %+v", *opened)
	}
	a.fromPrompt(a.prompt.id, AskAnswered{ID: a.prompt.id})
	if g := answerOf(t, a, second); !errors.Is(g.err, errDeclined) {
		t.Fatalf("Cancel answered %+v, %v", g.ans, g.err)
	}
}

// A question dropped, as when its connection is cancelled, takes its
// window with it.
func TestADroppedQuestionClosesItsWindow(t *testing.T) {
	a, _ := promptApp(t)
	ctx, cancel := context.WithCancel(t.Context())
	got := askAway(t, a, ctx, unlock)
	openPromptNow(t, a)
	cancel()
	if g := answerOf(t, a, got); !errors.Is(g.err, context.Canceled) {
		t.Fatalf("the dropped question answered %+v, %v", g.ans, g.err)
	}
	waitFor(t, a, "the question to go", func() bool { return len(a.st.Asks) == 0 })
	a.showPrompt()
	if a.prompt.c != nil {
		t.Fatal("the window stays with no question")
	}
}

// A question to choose an answer to stays a dialog in the window in
// front.
func TestAChoiceStaysADialog(t *testing.T) {
	a, opened := promptApp(t)
	got := askAway(t, a, t.Context(), Ask{Title: "Already connecting to srv", Choose: []string{"Wait", "Retry"}, No: "Cancel"})
	a.showPrompt()
	if len(*opened) != 0 || a.prompt.id != 0 {
		t.Fatalf("a window opened for %+v", *opened)
	}
	asks := a.stateFor(a.cur, a.st).Asks
	if len(asks) != 1 {
		t.Fatalf("the window in front asks %+v", asks)
	}
	a.handle(AskAnswered{ID: asks[0].ID, Yes: true, Answers: []string{"Wait"}})
	if g := answerOf(t, a, got); g.err != nil {
		t.Fatal(g.err)
	}
}

// A window that cannot open leaves its question to a dialog.
func TestAPromptThatCannotOpenIsADialog(t *testing.T) {
	a, _ := promptApp(t)
	a.openPrompt = func(Ask, string, *gunim.Window) (gunim.Client, error) {
		return gunim.Client{}, errors.New("no display")
	}
	askAway(t, a, t.Context(), unlock)
	a.showPrompt()
	waitFor(t, a, "the question to fall back", func() bool { return len(a.stateFor(a.cur, a.st).Asks) == 1 })
}

// A question in a window of its own needs no kakel window: with every
// one closed, none opens for it, and kakel stays open for the answer.
func TestAPromptNeedsNoKakelWindow(t *testing.T) {
	a, _ := promptApp(t)
	askAway(t, a, t.Context(), unlock)
	// Every window closed, with its panes.
	a.st.Panes = nil
	for _, w := range a.wins {
		w.gone = true
	}
	was := len(a.st.Notices)
	a.rehome()
	if a.opening != 0 || len(a.st.Notices) != was {
		t.Fatal("a kakel window was opened for the question")
	}
	if !a.prompted() {
		t.Fatal("nothing keeps kakel open for the answer")
	}
}
