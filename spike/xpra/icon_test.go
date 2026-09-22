package main

import (
	"strings"
	"testing"
)

// TestPremultipliedSpotsStraightAlpha is the check worth having.
//
// ui.Icon promises alpha-premultiplied BGRA, and go-xpra gets there
// through Go's Color.RGBA, which premultiplies by definition. If that
// ever changed the only sign on screen would be a pale fringe round
// every icon -- easy to miss, and easy to blame on the artwork.
func TestPremultipliedSpotsStraightAlpha(t *testing.T) {
	// One pixel, half transparent, mid grey. Premultiplied: colour has
	// already been halved, so it cannot exceed the alpha.
	good := []byte{0x40, 0x40, 0x40, 0x80}
	if got := premultiplied(good); !strings.Contains(got, "all premultiplied") {
		t.Errorf("premultiplied(%v) = %q", good, got)
	}

	// The same pixel with straight alpha: full-strength colour behind a
	// half-transparent alpha, which premultiplied pixels cannot hold.
	bad := []byte{0xff, 0xff, 0xff, 0x80}
	if got := premultiplied(bad); !strings.Contains(got, "NOT premultiplied") {
		t.Errorf("premultiplied(%v) = %q, want it caught", bad, got)
	}

	// One bad pixel among good ones is still caught, and counted.
	mixed := append(append([]byte{}, good...), append(good, bad...)...)
	got := premultiplied(mixed)
	if !strings.Contains(got, "1 of 3") {
		t.Errorf("premultiplied(mixed) = %q, want 1 of 3 flagged", got)
	}
}

// TestPremultipliedIgnoresOpaquePixels keeps the check quiet for an
// icon with no transparency, where the invariant says nothing.
func TestPremultipliedIgnoresOpaquePixels(t *testing.T) {
	opaque := []byte{0xff, 0xff, 0xff, 0xff, 0x00, 0x80, 0xff, 0xff}
	if got := premultiplied(opaque); !strings.Contains(got, "fully opaque") {
		t.Errorf("premultiplied(opaque) = %q", got)
	}

	// A fully transparent pixel must be all zeroes when premultiplied,
	// so a coloured one with zero alpha is wrong.
	invisible := []byte{0x10, 0x00, 0x00, 0x00}
	if got := premultiplied(invisible); !strings.Contains(got, "NOT premultiplied") {
		t.Errorf("premultiplied(%v) = %q, want it caught", invisible, got)
	}
}
