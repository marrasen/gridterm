package main

import (
	"strings"
	"testing"
)

// A program in a pane may be anything, and a link is a thing the user
// clicks. Only the schemes a browser is the right answer for.
func TestOnlyLinksWorthOpeningAreOpened(t *testing.T) {
	for _, tc := range []struct {
		at   string
		open bool
	}{
		{"https://example.com", true},
		{"http://example.com/a?b=1&c=2", true},
		{"mailto:marcus@example.com", true},
		{"ftp://example.com/f", true},
		{"HTTPS://EXAMPLE.COM", true},
		{"file:///etc/passwd", false},
		{`file:///C:/Windows/System32/calc.exe`, false},
		{"javascript:alert(1)", false},
		{"data:text/html;base64,PHNjcmlwdD4=", false},
		{"vbscript:msgbox", false},
		{"ms-msdt:/id PCWDiagnostic", false},
		{"search-ms:query=x", false},
		{"", false},
		{"   ", false},
	} {
		err := linkIsOpenable(tc.at)
		if tc.open && err != nil {
			t.Errorf("%q was refused: %v", tc.at, err)
		}
		if !tc.open && err == nil {
			t.Errorf("%q would be opened, want it refused", tc.at)
		}
	}
}

// A refusal says which scheme it was, so the user can tell a link
// gridterm will not follow from one it could not reach.
func TestARefusedLinkSaysWhatItWas(t *testing.T) {
	err := linkIsOpenable("file:///etc/passwd")

	if err == nil {
		t.Fatal("a file link was accepted")
	}
	if !strings.Contains(err.Error(), "file") {
		t.Errorf("it said %q, want it to name the scheme", err)
	}
}

// An address with a line break in it is not one. On Windows a command
// line is a string, and a break in it starts another command.
func TestAnAddressWithALineBreakIsRefused(t *testing.T) {
	for _, at := range []string{
		"https://example.com\ncalc.exe",
		"https://example.com\r\ncalc.exe",
		"https://example.com\x00",
	} {
		if err := linkIsOpenable(at); err == nil {
			t.Errorf("%q would be opened, want it refused", at)
		}
	}
}

// The address is handed over as one argument rather than glued into a
// command line, so nothing in it is read as a separate word.
func TestTheAddressGoesAsOneArgument(t *testing.T) {
	at := "https://example.com/a b&c"

	name, args := browserCommand(at)

	if name == "" {
		t.Fatal("no command to open a link with")
	}
	found := false
	for _, a := range args {
		if a == at {
			found = true
		}
	}
	if !found {
		t.Errorf("the arguments are %q, want the address whole among them", args)
	}
}
