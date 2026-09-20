//go:build windows

package notify

import "testing"

// Windows takes an icon in the notification area, which is what a
// message is shown against. No message is shown here: the icon goes
// in and comes out again, which is the part that can be wrong without
// anybody noticing.
func TestWindowsTakesTheIcon(t *testing.T) {
	k := New("gridterm test")

	if _, none := k.(nothing); none {
		t.Fatal("Windows would not take the icon, so no message can be shown")
	}
	if err := k.Close(); err != nil {
		t.Errorf("give the icon back: %v", err)
	}
}

// Closing twice is safe: the window closes once, and a second call
// from a test or a retry must not wait for a goroutine that has gone.
func TestClosingTwiceIsSafe(t *testing.T) {
	k := New("gridterm test")
	if err := k.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	if err := k.Close(); err != nil {
		t.Errorf("closing twice: %v", err)
	}
}

// A message after closing is refused rather than waiting for a
// goroutine that has gone.
func TestAMessageAfterClosingIsRefused(t *testing.T) {
	k := New("gridterm test")
	if err := k.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	if err := k.Show("gridterm", "too late"); err == nil {
		t.Error("it said the message was shown after the icon had gone")
	}
}
