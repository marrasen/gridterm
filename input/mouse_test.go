package input

import "testing"

func encMouse(e MouseEvent, m MouseMode) string {
	return string(EncodeMouse(e, m, nil))
}

var sgr = MouseMode{Click: true, SGR: true}
var legacy = MouseMode{Click: true}

func TestMouseNotReportedWhenDisabled(t *testing.T) {
	got := EncodeMouse(
		MouseEvent{Kind: MousePress, Button: MouseLeft}, MouseMode{}, nil)
	if got != nil {
		t.Fatalf("encoded % x with mouse reporting off", got)
	}
}

func TestMouseSGRPressAndRelease(t *testing.T) {
	press := encMouse(MouseEvent{
		Kind: MousePress, Button: MouseLeft, Col: 10, Row: 4}, sgr)
	if press != "\x1b[<0;11;5M" {
		t.Errorf("press = %q, want \"\\x1b[<0;11;5M\"", press)
	}

	release := encMouse(MouseEvent{
		Kind: MouseRelease, Button: MouseLeft, Col: 10, Row: 4}, sgr)
	if release != "\x1b[<0;11;5m" {
		t.Errorf("release = %q, want a lowercase m final byte", release)
	}
}

func TestMouseSGRButtons(t *testing.T) {
	cases := []struct {
		button MouseButton
		want   string
	}{
		{MouseLeft, "\x1b[<0;1;1M"},
		{MouseMiddle, "\x1b[<1;1;1M"},
		{MouseRight, "\x1b[<2;1;1M"},
		{MouseWheelUp, "\x1b[<64;1;1M"},
		{MouseWheelDown, "\x1b[<65;1;1M"},
	}
	for _, tc := range cases {
		got := encMouse(MouseEvent{Kind: MousePress, Button: tc.button}, sgr)
		if got != tc.want {
			t.Errorf("button %d = %q, want %q", tc.button, got, tc.want)
		}
	}
}

func TestMouseModifiers(t *testing.T) {
	cases := []struct {
		mods Mods
		want string
	}{
		{ModShift, "\x1b[<4;1;1M"},
		{ModAlt, "\x1b[<8;1;1M"},
		{ModCtrl, "\x1b[<16;1;1M"},
		{ModCtrl | ModShift, "\x1b[<20;1;1M"},
	}
	for _, tc := range cases {
		got := encMouse(
			MouseEvent{Kind: MousePress, Button: MouseLeft, Mods: tc.mods}, sgr)
		if got != tc.want {
			t.Errorf("mods %v = %q, want %q", tc.mods, got, tc.want)
		}
	}
}

func TestMouseLegacyEncoding(t *testing.T) {
	got := encMouse(MouseEvent{
		Kind: MousePress, Button: MouseLeft, Col: 10, Row: 4}, legacy)
	// Button 0, column and row offset by 33 (32 plus one-based).
	want := string([]byte{0x1b, '[', 'M', 32, 43, 37})
	if got != want {
		t.Fatalf("legacy press = % x, want % x", got, want)
	}
}

// The legacy encoding has no way to say which button was released, so
// every release is reported as button 3.
func TestMouseLegacyReleaseIsAlwaysButtonThree(t *testing.T) {
	for _, b := range []MouseButton{MouseLeft, MouseMiddle, MouseRight} {
		got := EncodeMouse(MouseEvent{Kind: MouseRelease, Button: b}, legacy, nil)
		if len(got) != 6 {
			t.Fatalf("release = % x, want six bytes", got)
		}
		if got[3] != 3+32 {
			t.Errorf("button %d released as %d, want 3", b, got[3]-32)
		}
	}
}

// One byte per coordinate runs out at column 223. Reporting a wrapped
// value would make the program act on the wrong cell, so nothing is sent.
func TestMouseLegacyDropsCoordinatesItCannotCarry(t *testing.T) {
	got := EncodeMouse(
		MouseEvent{Kind: MousePress, Button: MouseLeft, Col: 300}, legacy, nil)
	if got != nil {
		t.Fatalf("encoded % x for column 300, which does not fit", got)
	}
}

// SGR mode has no such limit, which is why every modern program asks
// for it.
func TestMouseSGRCarriesLargeCoordinates(t *testing.T) {
	got := encMouse(
		MouseEvent{Kind: MousePress, Button: MouseLeft, Col: 300, Row: 400}, sgr)
	if got != "\x1b[<0;301;401M" {
		t.Fatalf("= %q, want \"\\x1b[<0;301;401M\"", got)
	}
}

func TestMouseMotionNeedsTheRightMode(t *testing.T) {
	move := MouseEvent{Kind: MouseMove, Col: 1, Row: 1}
	drag := MouseEvent{Kind: MouseMove, Button: MouseLeft, Col: 1, Row: 1}

	if got := EncodeMouse(move, MouseMode{Click: true, SGR: true}, nil); got != nil {
		t.Errorf("plain motion reported in click-only mode: % x", got)
	}
	if got := EncodeMouse(drag, MouseMode{Click: true, SGR: true}, nil); got != nil {
		t.Errorf("drag reported in click-only mode: % x", got)
	}
	if got := EncodeMouse(drag, MouseMode{Drag: true, SGR: true}, nil); got == nil {
		t.Error("drag not reported in button-event mode")
	}
	if got := EncodeMouse(move, MouseMode{Drag: true, SGR: true}, nil); got != nil {
		t.Errorf("buttonless motion reported in button-event mode: % x", got)
	}
	if got := EncodeMouse(move, MouseMode{Motion: true, SGR: true}, nil); got == nil {
		t.Error("buttonless motion not reported in any-event mode")
	}
}

func TestMouseMotionSetsTheMotionBit(t *testing.T) {
	got := encMouse(MouseEvent{
		Kind: MouseMove, Button: MouseLeft, Col: 0, Row: 0},
		MouseMode{Drag: true, SGR: true})
	if got != "\x1b[<32;1;1M" {
		t.Fatalf("= %q, want the motion bit set (32)", got)
	}
}

// A wheel has no release, and reporting one as a button release would
// make a program think a drag ended.
func TestMouseWheelReleaseIsNotReported(t *testing.T) {
	for _, b := range []MouseButton{MouseWheelUp, MouseWheelDown} {
		if got := EncodeMouse(
			MouseEvent{Kind: MouseRelease, Button: b}, sgr, nil); got != nil {
			t.Errorf("wheel release encoded as % x", got)
		}
	}
}

func TestMouseModeEnabled(t *testing.T) {
	cases := []struct {
		mode MouseMode
		want bool
	}{
		{MouseMode{}, false},
		{MouseMode{SGR: true}, false}, // SGR alone is only an encoding
		{MouseMode{Click: true}, true},
		{MouseMode{Drag: true}, true},
		{MouseMode{Motion: true}, true},
	}
	for _, tc := range cases {
		if got := tc.mode.Enabled(); got != tc.want {
			t.Errorf("%+v.Enabled() = %v, want %v", tc.mode, got, tc.want)
		}
	}
}

func TestEncodeMouseAppendsToCallerBuffer(t *testing.T) {
	buf := make([]byte, 0, 32)
	buf = EncodeMouse(MouseEvent{Kind: MousePress, Button: MouseLeft}, sgr, buf)
	buf = EncodeMouse(MouseEvent{Kind: MouseRelease, Button: MouseLeft}, sgr, buf)
	if string(buf) != "\x1b[<0;1;1M\x1b[<0;1;1m" {
		t.Fatalf("= %q, want both reports appended", buf)
	}
}
