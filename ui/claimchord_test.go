package ui

import (
	"testing"

	"github.com/marrasen/gridterm/input"
)

// claimer is a fake that takes one chord for itself, the way a reader
// takes shift and a page key.
type claimer struct {
	fake
	chord Chord
}

func (c *claimer) ClaimsChord(ev input.Event, _ string) bool { return ChordOf(ev) == c.chord }

func (c *claimer) HandleKey(ev input.Event) (bool, error) {
	c.seen = append(c.seen, ev.Key)
	return ChordOf(ev) == c.chord, nil
}

// aRootOver returns a root showing one widget, with one command on one
// accelerator, and a count of how often that command ran.
func aRootOver(t *testing.T, w Widget, chord Chord) (*Root, *int) {
	t.Helper()
	ran := 0
	r := &Root{Commands: NewCommands(), Accelerators: NewKeymap(), Keys: NewKeymap()}
	r.Commands.MustRegister(Command{ID: "x", Title: "X", Run: func() error { ran++; return nil }})
	r.Accelerators.MustBind(map[Chord]string{chord: "x"})
	r.Layout(Rect{Cols: 40, Rows: 12})
	r.SetWidget(w)
	return r, &ran
}

var pageDown = Chord{Key: input.KeyPageDown, Mods: input.ModShift}

// A widget that claimed a chord gets the key, and the accelerator on
// the same chord does not run. Without this the widget never sees it.
func TestAClaimedChordReachesTheWidgetRatherThanTheAccelerator(t *testing.T) {
	c := &claimer{chord: pageDown}
	r, ran := aRootOver(t, c, pageDown)

	took, err := r.HandleKey(press(input.KeyPageDown, input.ModShift))

	if err != nil {
		t.Fatalf("the key: %v", err)
	}
	if !took {
		t.Error("nothing took the claimed chord")
	}
	if *ran != 0 {
		t.Errorf("the accelerator ran %d times, want the widget to have had the key", *ran)
	}
	if len(c.seen) != 1 || c.seen[0] != input.KeyPageDown {
		t.Errorf("the widget saw %v, want the one key", c.seen)
	}
}

// A chord the widget did not claim still runs its accelerator, and the
// widget never sees it. That is the rule a claim is the exception to.
func TestAnUnclaimedChordStillRunsItsAccelerator(t *testing.T) {
	c := &claimer{chord: pageDown}
	r, ran := aRootOver(t, c, Chord{Key: input.KeyPageUp, Mods: input.ModShift})

	if _, err := r.HandleKey(press(input.KeyPageUp, input.ModShift)); err != nil {
		t.Fatalf("the key: %v", err)
	}

	if *ran != 1 {
		t.Errorf("the accelerator ran %d times, want 1", *ran)
	}
	if len(c.seen) != 0 {
		t.Errorf("the widget saw %v, want nothing", c.seen)
	}
}

// The claim is asked of the widget the keys reach rather than of the
// top of the tree, so a claimer inside a split is still asked.
func TestTheClaimIsAskedOfTheWidgetTheKeysReach(t *testing.T) {
	c := &claimer{chord: pageDown}
	split := NewSplit(Columns, &fake{name: "other"}, c)
	split.Focus(c)
	r, ran := aRootOver(t, split, pageDown)

	if _, err := r.HandleKey(press(input.KeyPageDown, input.ModShift)); err != nil {
		t.Fatalf("the key: %v", err)
	}

	if *ran != 0 {
		t.Errorf("the accelerator ran %d times, want the claimer to have had the key", *ran)
	}
	if len(c.seen) != 1 {
		t.Errorf("the claimer saw %v, want the one key", c.seen)
	}
}

// A claimer that does not have the keys is not asked, so its claim
// cannot take the shortcut away from the pane that does have them.
func TestAClaimerWithoutTheKeysIsNotAsked(t *testing.T) {
	c := &claimer{chord: pageDown}
	other := &fake{name: "other"}
	split := NewSplit(Columns, other, c)
	split.Focus(other)
	r, ran := aRootOver(t, split, pageDown)

	if _, err := r.HandleKey(press(input.KeyPageDown, input.ModShift)); err != nil {
		t.Fatalf("the key: %v", err)
	}

	if *ran != 1 {
		t.Errorf("the accelerator ran %d times, want 1", *ran)
	}
	if len(c.seen) != 0 {
		t.Errorf("a claimer without the keys saw %v, want nothing", c.seen)
	}
}

// A dialog is over the tree, so it has the keys and the claim under it
// is not asked. Without this a reader behind a form would eat a chord
// the user pressed at the form.
func TestAClaimUnderADialogIsNotAsked(t *testing.T) {
	c := &claimer{chord: pageDown}
	r, ran := aRootOver(t, c, pageDown)
	r.PushModal(&fake{name: "dialog"})

	if _, err := r.HandleKey(press(input.KeyPageDown, input.ModShift)); err != nil {
		t.Fatalf("the key: %v", err)
	}

	if *ran != 1 {
		t.Errorf("the accelerator ran %d times, want 1", *ran)
	}
	if len(c.seen) != 0 {
		t.Errorf("the claimer behind the dialog saw %v, want nothing", c.seen)
	}
}

// A press of a button that is already down proves its release was lost:
// no mouse sends two presses without a release between them. The window
// lets go and works out afresh what is under the pointer.
//
// Without this a lost release -- what a window losing focus mid-drag
// leaves behind -- sends every later click to the widget that took the
// first one, wherever the pointer has since gone.
func TestAPressOfAButtonAlreadyDownStartsAfresh(t *testing.T) {
	left, right := &fake{name: "left", takes: input.KeyA}, &fake{name: "right", takes: input.KeyA}
	split := NewSplit(Columns, left, right)
	r := &Root{}
	r.Layout(Rect{Cols: 40, Rows: 10})
	r.SetWidget(split)

	// A press in the left pane, and no release: the window never heard
	// one, so it still thinks the button is down.
	press := input.MouseEvent{Kind: input.MousePress, Button: input.MouseLeft, Col: 2, Row: 2}
	if _, err := r.HandleMouse(press); err != nil {
		t.Fatalf("the first press: %v", err)
	}
	if !r.Held() {
		t.Fatal("the first press did not take the pointer, so this proves nothing")
	}
	before := len(right.clicks)

	// The same button again, in the other pane.
	again := input.MouseEvent{Kind: input.MousePress, Button: input.MouseLeft, Col: 30, Row: 2}
	if _, err := r.HandleMouse(again); err != nil {
		t.Fatalf("the second press: %v", err)
	}

	if len(right.clicks) == before {
		t.Error("the second press never reached the pane it landed in")
	}
	if len(left.clicks) != 1 {
		t.Errorf("the left pane saw %d presses, want only the first", len(left.clicks))
	}
}

// A different button during a drag proves nothing: the first one may
// well still be down, so the drag keeps the pointer.
func TestAnotherButtonMidDragDoesNotEndTheDrag(t *testing.T) {
	left, right := &fake{name: "left"}, &fake{name: "right"}
	split := NewSplit(Columns, left, right)
	r := &Root{}
	r.Layout(Rect{Cols: 40, Rows: 10})
	r.SetWidget(split)
	if _, err := r.HandleMouse(input.MouseEvent{
		Kind: input.MousePress, Button: input.MouseLeft, Col: 2, Row: 2,
	}); err != nil {
		t.Fatalf("the first press: %v", err)
	}
	before := len(right.clicks)

	if _, err := r.HandleMouse(input.MouseEvent{
		Kind: input.MousePress, Button: input.MouseRight, Col: 30, Row: 2,
	}); err != nil {
		t.Fatalf("the right press: %v", err)
	}

	if len(right.clicks) != before {
		t.Error("a second button mid-drag reached the other pane, want the drag to keep the pointer")
	}
	if len(left.clicks) != 2 {
		t.Errorf("the pane being dragged in saw %d events, want both", len(left.clicks))
	}
}
