package shells

import (
	"slices"
	"strings"
	"testing"
)

// A WSL pane is started through wsl.exe, and that is what says a pane
// is one.
func TestWhichCommandsStartWSL(t *testing.T) {
	for what, tc := range map[string]struct {
		argv []string
		want bool
	}{
		"a distribution":       {[]string{`C:\WINDOWS\system32\wsl.exe`, "-d", "Ubuntu"}, true},
		"named in any case":    {[]string{`C:\Windows\System32\WSL.EXE`, "-d", "Ubuntu"}, true},
		"with no path":         {[]string{"wsl.exe"}, true},
		"a shell on this one":  {[]string{`C:\WINDOWS\system32\cmd.exe`}, false},
		"something wsl-ish":    {[]string{`C:\tools\wslconfig.exe`}, false},
		"nothing at all":       {nil, false},
		"a path ending in wsl": {[]string{`C:\tools\wsl`}, false},
	} {
		if got := IsWSL(tc.argv); got != tc.want {
			t.Errorf("%s: IsWSL(%q) = %v, want %v", what, tc.argv, got, tc.want)
		}
	}
}

// A name Windows holds does not reach a WSL shell unless WSLENV lists
// it, so the names are added to whatever is already carried.
func TestCarryingNamesIntoWSL(t *testing.T) {
	got := CarryIntoWSL("", "TERM_PROGRAM", "TERM_PROGRAM_VERSION")

	if want := "TERM_PROGRAM/u:TERM_PROGRAM_VERSION/u"; got != want {
		t.Errorf("with nothing carried it is %q, want %q", got, want)
	}
}

// What the user already carries is kept. Writing over WSLENV would take
// away whatever they set up, which is the sort of thing nobody notices
// until something else stops working.
func TestCarryingKeepsWhatWasAlreadyThere(t *testing.T) {
	got := CarryIntoWSL("MY_TOOL/p:OTHER", "TERM_PROGRAM")

	if !strings.HasPrefix(got, "MY_TOOL/p:OTHER") {
		t.Errorf("it is %q, want it to start with what was already carried", got)
	}
	if !strings.Contains(got, "TERM_PROGRAM/u") {
		t.Errorf("it is %q, want the new name in it", got)
	}
}

// A name already carried is left with the flags the user gave it, and
// is not listed twice. Two entries for one name is not something WSLENV
// promises anything about.
func TestCarryingDoesNotRepeatAName(t *testing.T) {
	got := CarryIntoWSL("TERM_PROGRAM/p", "TERM_PROGRAM", "TERM_PROGRAM_VERSION")

	var named []string
	for _, entry := range strings.Split(got, ":") {
		name, _, _ := strings.Cut(entry, "/")
		named = append(named, name)
	}
	if want := []string{"TERM_PROGRAM", "TERM_PROGRAM_VERSION"}; !slices.Equal(named, want) {
		t.Errorf("it carries %v, want %v", named, want)
	}
	if !strings.Contains(got, "TERM_PROGRAM/p") {
		t.Errorf("it is %q, want the flags the user gave kept", got)
	}
	if strings.Contains(got, "TERM_PROGRAM/u") {
		t.Errorf("it is %q, want the name not carried a second time", got)
	}
}

// Every entry is a name and its flags, so nothing empty gets in.
func TestCarryingLeavesNoEmptyEntry(t *testing.T) {
	got := CarryIntoWSL("::", "TERM_PROGRAM")

	if slices.Contains(strings.Split(got, ":"), "") && !strings.HasPrefix(got, "::") {
		t.Errorf("it is %q, want no empty entry added", got)
	}
	if !strings.Contains(got, "TERM_PROGRAM/u") {
		t.Errorf("it is %q, want the name carried", got)
	}
}
