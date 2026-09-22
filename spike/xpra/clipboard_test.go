package main

import (
	"testing"

	"github.com/Xpra-org/go-xpra/ui"

	"github.com/marrasen/gridterm/input"
)

// TestDisplayOffersAClipboard checks the wiring. The client only asks
// once, at construction, and a display that does not answer is told to
// the server as having no clipboard at all -- so a nil here is a
// feature quietly missing rather than an error anywhere.
func TestDisplayOffersAClipboard(t *testing.T) {
	d := newDisplay(t.TempDir(), false)
	var provider ui.ClipboardProvider = d
	if provider.Clipboard() == nil {
		t.Fatal("the display offers no clipboard")
	}
}

// TestTheFarSideCopying is the inbound half: the server calls SetText
// when the remote application has copied something.
func TestTheFarSideCopying(t *testing.T) {
	d := newDisplay(t.TempDir(), false)
	board := d.Clipboard()

	if text, taken := d.board.Text(); text != "" || taken != 0 {
		t.Fatalf("a fresh clipboard holds %q after %d copies", text, taken)
	}
	if err := board.SetText("from the far side"); err != nil {
		t.Fatalf("SetText: %v", err)
	}
	text, taken := d.board.Text()
	if text != "from the far side" || taken != 1 {
		t.Errorf("the clipboard holds %q after %d copies", text, taken)
	}

	// The last one wins, the way a clipboard does.
	_ = board.SetText("and then this")
	if text, taken := d.board.Text(); text != "and then this" || taken != 2 {
		t.Errorf("the clipboard holds %q after %d copies", text, taken)
	}
}

// TestThisSideCopying is the outbound half. Nothing is sent for an
// empty clipboard: gridterm clears a selection far more often than it
// makes one, and each clear would otherwise be a packet.
func TestThisSideCopying(t *testing.T) {
	d := newDisplay(t.TempDir(), false)

	d.copyLocally("")
	select {
	case ev := <-d.events:
		t.Fatalf("an empty clipboard sent %+v", ev)
	default:
	}

	d.copyLocally("selected in a pane")
	select {
	case ev := <-d.events:
		change, ok := ev.(ui.ClipboardChange)
		if !ok {
			t.Fatalf("sent %T, want a ClipboardChange", ev)
		}
		if change.Text != "selected in a pane" {
			t.Errorf("announced %q", change.Text)
		}
	default:
		t.Fatal("nothing was announced")
	}
}

// TestParseChords covers the scripted typist's chord spelling, which is
// what makes a clipboard testable at all: the far application only
// copies when something tells it to.
func TestParseChords(t *testing.T) {
	got, err := parseChords("ctrl+a, ctrl+shift+c , alt+F4, super+v")
	if err != nil {
		t.Fatalf("parseChords: %v", err)
	}
	want := []input.Event{
		{Key: input.KeyA, Mods: input.ModCtrl},
		{Key: input.KeyC, Mods: input.ModCtrl | input.ModShift},
		{Key: input.KeyF4, Mods: input.ModAlt},
		{Key: input.KeyV, Mods: input.ModSuper},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d chords, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("chord %d is %+v, want %+v", i, got[i], want[i])
		}
	}

	if none, err := parseChords(""); err != nil || none != nil {
		t.Errorf("an empty spec gave %+v, %v", none, err)
	}
	for _, bad := range []string{"ctrl+", "hyper+a", "ctrl+nosuchkey"} {
		if _, err := parseChords(bad); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
}

// TestChordNamesMatchGridterm keeps the spelling tied to gridterm's own
// names rather than to a second list that can drift.
func TestChordNamesMatchGridterm(t *testing.T) {
	for _, k := range []input.Key{input.KeyF4, input.KeyHome, input.KeyPageUp, input.KeyEscape} {
		got, ok := keyNamed(k.String())
		if !ok || got != k {
			t.Errorf("%q resolved to %v (found=%v), want %v", k.String(), got, ok, k)
		}
	}
	for c := 'a'; c <= 'z'; c++ {
		if got, ok := keyNamed(string(c)); !ok || got != input.KeyA+input.Key(c-'a') {
			t.Errorf("%q resolved to %v", c, got)
		}
	}
}
