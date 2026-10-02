package app

import (
	"runtime"
	"testing"
)

// kakel started with nothing to do goes to the tray, as it does with the
// computer; started for a command, a server, the launcher or pictures,
// it opens what it was asked for.
func TestAStartWithNothingToDoGoesToTheTray(t *testing.T) {
	for _, c := range []struct {
		args []string
		tray bool
	}{
		{nil, true},
		{[]string{"-tray"}, true},
		{[]string{"-font-size", "14"}, true},
		{[]string{"-e", "top"}, false},
		{[]string{"-ssh", "me@there"}, false},
		{[]string{"-launcher"}, false},
		{[]string{"-shot", "wait:10"}, false},
	} {
		o, err := ParseOptions(c.args)
		if err != nil {
			t.Fatal(err)
		}
		if got := o.StartsInTray(); got != c.tray {
			t.Errorf("started with %q, it goes to the tray: %v", c.args, got)
		}
	}
}

func TestAKeyAsAPersonReadsIt(t *testing.T) {
	if got := chordName("ctrl+alt+k"); got != "Ctrl+Alt+K" {
		t.Errorf("ctrl+alt+k reads %q", got)
	}
	want := "Shift+Super+K"
	if runtime.GOOS == "windows" {
		want = "Shift+Win+K"
	}
	if got := chordName("shift+super+k"); got != want {
		t.Errorf("shift+super+k reads %q, want %q", got, want)
	}
}
