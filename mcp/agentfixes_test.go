package mcp

import (
	"strings"
	"testing"
)

// Text ending in a backslash and an r is refused, and nothing is
// typed. It is what a command line looks like after being escaped
// twice on the way here, and typed into a live shell it runs wrong
// with nothing on the screen to say why.
func TestTextEndingInAnEscapeIsRefused(t *testing.T) {
	for _, text := range []string{
		`go build ./...\r`,
		`go build ./...\n`,
		`go build ./...\r\n`,
	} {
		if why := escapedEnding(text); why == "" {
			t.Errorf("%q was allowed through", text)
			continue
		}
		if why := escapedEnding(text); !strings.Contains(why, `keys ["Enter"]`) {
			t.Errorf("%q was refused with %q, which does not say what to do", text, why)
		}
	}
}

// And text that only happens to hold a backslash is fine: a path on
// Windows is not a mistake.
func TestTextThatHoldsABackslashIsFine(t *testing.T) {
	for _, text := range []string{
		`cd C:\Users\marcus`,
		`printf 'a\rb'`,
		`grep '\n' file`,
		`echo \`,
		"",
		"go build ./...",
	} {
		if why := escapedEnding(text); why != "" {
			t.Errorf("%q was refused: %s", text, why)
		}
	}
}

// The three places that talk about typing agree with each other.
//
// They disagreed once: the server's own instructions said to press
// Enter with keys, and the tool said to put a return in the text. An
// agent filling in arguments reads the tool, and typed a backslash
// into a live shell.
func TestNothingTellsTheCallerToPutAReturnInTheText(t *testing.T) {
	var send tool
	for _, d := range toolList() {
		if d.Name == "send_keys" {
			send = d
		}
	}
	if send.Name == "" {
		t.Fatal("there is no send_keys tool")
	}
	said := []string{send.Description, keysArg, instructions}
	if text, ok := send.InputSchema.Properties["text"]; ok {
		said = append(said, text.Description)
	}
	for _, s := range said {
		if strings.Contains(s, `\r at the end`) || strings.Contains(s, `with "\r" for Enter`) {
			t.Errorf("something still tells the caller to type a return: %q", s)
		}
	}
	// And the tool says what really happens: the text, then the keys.
	if strings.Contains(send.Description, "named keys instead") {
		t.Error("the tool still says the keys go instead of the text")
	}
	if !strings.Contains(keysArg, "once the text has gone in") {
		t.Errorf("the keys argument says %q", keysArg)
	}
}

// A picture on the screen is named in the trailer. The cells under
// one read back as blank, so without this a picture that arrived and
// one that never did look the same.
func TestAPictureIsNamedInTheTrailer(t *testing.T) {
	got := showScreen(Screen{
		Screen:   "some text",
		Pictures: []Picture{{Top: 2, Rows: 13, Cols: 40, Width: 400, Height: 200}},
	}, Ending{}, false)

	if !strings.Contains(got, "Rows 2 to 14 of the screen hold a picture, 400 by 200, sent as OSC 1337") {
		t.Errorf("the trailer says:\n%s", got)
	}
	if !strings.Contains(got, "read as blank") {
		t.Error("it does not say the cells read as blank")
	}
}

// One that came from another window's screen says the sequence it
// really came by.
func TestAPictureFromAnotherWindowSaysSo(t *testing.T) {
	got := showScreen(Screen{
		Pictures: []Picture{{Top: 0, Rows: 4, Width: 10, Height: 10, Wire: true}},
	}, Ending{}, false)

	if !strings.Contains(got, "OSC 1338") {
		t.Errorf("the trailer says:\n%s", got)
	}
}

// A screen with no pictures says nothing about pictures.
func TestAScreenWithNoPicturesSaysNothing(t *testing.T) {
	got := showScreen(Screen{Screen: "some text"}, Ending{}, false)

	if strings.Contains(got, "picture") {
		t.Errorf("the trailer mentions a picture:\n%s", got)
	}
}

// The trailer says when the blank rows were left out, so an answer
// shorter than the lines asked for is not a puzzle.
func TestTheTrailerSaysTheBlankRowsWereLeftOut(t *testing.T) {
	got := showScreen(Screen{Screen: "one\ntwo", Trimmed: true}, Ending{}, false)

	if !strings.Contains(got, "blank rows") {
		t.Errorf("the trailer says:\n%s", got)
	}
}
