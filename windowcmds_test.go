package main

import (
	"strings"
	"testing"

	"github.com/marrasen/gridterm/ui"
)

// Closing the window asks first, and says what would go with it.
func TestClosingTheWindowAsksFirst(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	withDialogs(t, a)
	a.commands()

	a.askToQuit()

	f, ok := a.root.Modal().(*ui.Form)
	if !ok {
		t.Fatalf("nothing asked: %T", a.root.Modal())
	}
	if !strings.Contains(f.Title, "Close") {
		t.Errorf("the question is %q", f.Title)
	}
	if got := strings.Join(f.Lines, " "); !strings.Contains(got, "pane") {
		t.Errorf("it does not say what is open: %q", got)
	}
	if a.quit.Load() {
		t.Error("the window went before the question was answered")
	}
}

// Saying no leaves the window alone.
func TestSayingNoLeavesTheWindow(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	withDialogs(t, a)
	a.commands()
	a.askToQuit()
	f := a.root.Modal().(*ui.Form)

	pressButton(t, a, f, "Cancel")
	a.pump.run()

	if a.quit.Load() {
		t.Error("it went anyway")
	}
	// And asking again works: the first question is not still up.
	a.askToQuit()
	if _, ok := a.root.Modal().(*ui.Form); !ok {
		t.Error("asking a second time put nothing up")
	}
}

// Saying yes lets it go.
func TestSayingYesClosesTheWindow(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	withDialogs(t, a)
	a.commands()
	a.askToQuit()
	f := a.root.Modal().(*ui.Form)

	pressButton(t, a, f, "Close it")
	a.pump.run()

	if !a.quit.Load() {
		t.Error("it stayed")
	}
}

// The question is asked once however long the close button is held.
func TestTheQuestionIsAskedOnce(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	withDialogs(t, a)
	a.commands()

	for range 5 {
		a.askToQuit()
	}

	if _, ok := a.root.Modal().(*ui.Form); !ok {
		t.Fatal("nothing asked")
	}
	// One dialog, not five stacked.
	pressButton(t, a, a.root.Modal().(*ui.Form), "Cancel")
	a.pump.run()
	if a.root.Modal() != nil {
		t.Errorf("a second question is behind the first: %T", a.root.Modal())
	}
}

// A window holding nothing goes without a word: there is nothing to
// warn about.
func TestAWindowHoldingNothingJustGoes(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	withDialogs(t, a)
	a.commands()
	for pane := range a.panes {
		if err := a.closePane(pane); err != nil {
			t.Fatalf("close it: %v", err)
		}
	}
	a.quit.Store(false)

	a.askToQuit()

	if a.root.Modal() != nil {
		t.Errorf("it asked about a window holding nothing: %T", a.root.Modal())
	}
	if !a.quit.Load() {
		t.Error("it did not go")
	}
}

// What is open is said the way a person says it.
func TestWhatIsOpenReadsAsASentence(t *testing.T) {
	for _, tc := range []struct {
		what []string
		want string
	}{
		{nil, "nothing"},
		{[]string{"2 panes"}, "2 panes"},
		{[]string{"2 panes", "1 tunnel"}, "2 panes and 1 tunnel"},
		{[]string{"a", "b", "c"}, "a, b and c"},
	} {
		if got := listOf(tc.what); got != tc.want {
			t.Errorf("%v came out as %q, want %q", tc.what, got, tc.want)
		}
	}
	if got := count(1, "pane", "panes"); got != "1 pane" {
		t.Errorf("one came out as %q", got)
	}
	if got := count(3, "pane", "panes"); got != "3 panes" {
		t.Errorf("three came out as %q", got)
	}
}

// Full screen takes the sidebar and the row of menu titles with it,
// and puts back what was there on the way out.
func TestFullScreenTakesTheFrameAway(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	withDialogs(t, a)
	a.commands()
	withMenubar(t, a)
	if err := a.showPanel(true); err != nil {
		t.Fatalf("open the sidebar: %v", err)
	}

	if err := a.toggleFullScreen(); err != nil {
		t.Fatalf("go full screen: %v", err)
	}

	if !a.FullScreen() {
		t.Error("it is not full screen")
	}
	if !a.bar.Hidden {
		t.Error("the row of menu titles is still drawn")
	}
	if a.panelShowing() {
		t.Error("the sidebar is still open")
	}

	if err := a.toggleFullScreen(); err != nil {
		t.Fatalf("come back out: %v", err)
	}

	if a.FullScreen() || a.bar.Hidden {
		t.Error("coming out left it full screen")
	}
	if !a.panelShowing() {
		t.Error("the sidebar did not come back")
	}
}

// A sidebar somebody had closed themselves stays closed on the way
// out of full screen.
func TestFullScreenPutsBackWhatWasThere(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	withDialogs(t, a)
	a.commands()
	withMenubar(t, a)
	if err := a.showPanel(false); err != nil {
		t.Fatalf("close the sidebar: %v", err)
	}

	if err := a.toggleFullScreen(); err != nil {
		t.Fatalf("go full screen: %v", err)
	}
	if err := a.toggleFullScreen(); err != nil {
		t.Fatalf("come back out: %v", err)
	}

	if a.panelShowing() {
		t.Error("the sidebar was opened by coming out of full screen")
	}
}

// About says what this is.
func TestAboutSaysWhatThisIs(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	withDialogs(t, a)
	a.commands()

	if err := a.showAbout(); err != nil {
		t.Fatalf("show it: %v", err)
	}

	n, ok := a.root.Modal().(*ui.Notice)
	if !ok {
		t.Fatalf("nothing came up: %T", a.root.Modal())
	}
	if n.Title != "gridterm" {
		t.Errorf("it is titled %q", n.Title)
	}
	if !strings.Contains(n.Message(), "terminal") {
		t.Errorf("it says %q", n.Message())
	}
}
