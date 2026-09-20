package vt

import (
	"strings"
	"testing"
)

// A program can ask for a message to be shown, and the pane keeps it.
func TestAProgramAsksForAMessage(t *testing.T) {
	term := New(40, 10, DefaultPalette(), 100, Callbacks{})

	term.Write([]byte("\x1b]9;the build finished\x07"))

	text, num := term.Notice()
	if text != "the build finished" {
		t.Errorf("the message came out as %q", text)
	}
	if num != 1 {
		t.Errorf("it is message number %d, want the first", num)
	}
}

// The same words twice are two messages, so the second one is shown
// rather than looking like the first still being up.
func TestTheSameMessageTwiceIsTwoMessages(t *testing.T) {
	term := New(40, 10, DefaultPalette(), 100, Callbacks{})

	term.Write([]byte("\x1b]9;done\x07"))
	term.Write([]byte("\x1b]9;done\x07"))

	if _, num := term.Notice(); num != 2 {
		t.Errorf("two messages came out as number %d", num)
	}
}

// A message holding a semicolon arrives whole: the parser cuts on
// those and the message is what was between them.
func TestAMessageWithASemicolonArrivesWhole(t *testing.T) {
	term := New(40, 10, DefaultPalette(), 100, Callbacks{})

	term.Write([]byte("\x1b]9;built; 3 warnings\x07"))

	if text, _ := term.Notice(); text != "built; 3 warnings" {
		t.Errorf("the message came out as %q", text)
	}
}

// A message cannot draw over the window it is shown in, and cannot be
// longer than a row has for it.
func TestAMessageIsTrimmedToSomethingShowable(t *testing.T) {
	term := New(40, 10, DefaultPalette(), 100, Callbacks{})

	// An escape inside the message ends the sequence, so what follows
	// it never reaches the message at all.
	term.Write([]byte("\x1b]9;clean\x1b[2Jnow\x07"))
	if text, _ := term.Notice(); text != "clean" {
		t.Errorf("the escape survived, as %q", text)
	}

	// A control character that does not end the sequence is dropped.
	term.Write([]byte("\x1b]9;one\x01two\x08\x07"))
	if text, _ := term.Notice(); text != "onetwo" {
		t.Errorf("a control character survived, as %q", text)
	}

	term.Write([]byte("\x1b]9;" + strings.Repeat("x", MostNoticeRunes+50) + "\x07"))
	if text, _ := term.Notice(); len([]rune(text)) != MostNoticeRunes {
		t.Errorf("a long message came out %d runes long", len([]rune(text)))
	}
}

// The next command takes the message off, because the message was
// about the one before it.
func TestTheNextCommandTakesTheMessageOff(t *testing.T) {
	term := New(40, 10, DefaultPalette(), 100, Callbacks{})
	term.Write([]byte("\x1b]9;the build finished\x07"))

	term.Write([]byte("\x1b]133;C\x07"))

	if text, _ := term.Notice(); text != "" {
		t.Errorf("%q is still on the pane while the next command runs", text)
	}
}

// A program can say how far along it is.
func TestAProgramSaysHowFarAlongItIs(t *testing.T) {
	for _, tc := range []struct {
		sent string
		want Progress
	}{
		{"\x1b]9;4;1;42\x07", Progress{State: Working, Percent: 42}},
		{"\x1b]9;4;3\x07", Progress{State: Indeterminate}},
		{"\x1b]9;4;2;70\x07", Progress{State: ProgressFailed, Percent: 70}},
		{"\x1b]9;4;4;10\x07", Progress{State: ProgressWarning, Percent: 10}},
		{"\x1b]9;4;0\x07", Progress{}},
		// Past a hundred is a hundred, and below nothing is nothing.
		{"\x1b]9;4;1;900\x07", Progress{State: Working, Percent: 100}},
		{"\x1b]9;4;1;-5\x07", Progress{State: Working}},
	} {
		term := New(40, 10, DefaultPalette(), 100, Callbacks{})

		term.Write([]byte(tc.sent))

		if got := term.Progress(); got != tc.want {
			t.Errorf("%q gave %+v, want %+v", tc.sent, got, tc.want)
		}
	}
}

// Nonsense instead of a state leaves the last thing the program said.
func TestNonsenseInsteadOfProgressIsRefused(t *testing.T) {
	for _, sent := range []string{
		"\x1b]9;4\x07",
		"\x1b]9;4;9\x07",
		"\x1b]9;4;x;50\x07",
	} {
		term := New(40, 10, DefaultPalette(), 100, Callbacks{})
		term.Write([]byte("\x1b]9;4;1;30\x07"))

		term.Write([]byte(sent))

		if got := term.Progress(); got.Percent != 30 {
			t.Errorf("%q changed it to %+v", sent, got)
		}
	}
}

// Saying it has finished takes the number away with it.
func TestFinishingTakesTheNumberAway(t *testing.T) {
	term := New(40, 10, DefaultPalette(), 100, Callbacks{})
	term.Write([]byte("\x1b]9;4;1;80\x07"))

	term.Write([]byte("\x1b]9;4;0\x07"))

	if got := term.Progress(); got != (Progress{}) {
		t.Errorf("it came out as %+v, want nothing at all", got)
	}
}

// A program may ask what the text and the background are drawn in,
// which is how one works out whether it is on a dark theme.
func TestAProgramAsksWhatColourTheThemeIs(t *testing.T) {
	pal := DefaultPalette()
	for _, tc := range []struct{ sent, want string }{
		{"\x1b]11;?\x07", "\x1b]11;rgb:1414/1717/1c1c\x07"},
		{"\x1b]10;?\x07", "\x1b]10;rgb:c8c8/d0d0/dada\x07"},
		// The answer ends the way the question did.
		{"\x1b]11;?\x1b\\", "\x1b]11;rgb:1414/1717/1c1c\x1b\\"},
	} {
		var said []byte
		term := New(40, 10, pal, 100, Callbacks{
			Reply: func(b []byte) { said = append(said, b...) },
		})

		term.Write([]byte(tc.sent))

		if string(said) != tc.want {
			t.Errorf("%q was answered %q, want %q", tc.sent, said, tc.want)
		}
	}
}

// A program setting the colours is not answered and changes nothing.
// The colours are the window's theme, and a pane left unlike every
// other one would have nothing to put it back.
func TestAProgramCannotSetTheColours(t *testing.T) {
	var said []byte
	term := New(40, 10, DefaultPalette(), 100, Callbacks{
		Reply: func(b []byte) { said = append(said, b...) },
	})

	term.Write([]byte("\x1b]11;rgb:ffff/0000/0000\x07"))

	if len(said) != 0 {
		t.Errorf("it was answered %q", said)
	}
	if got := term.Screen().palette.BG; got != DefaultPalette().BG {
		t.Errorf("the background became %v", got)
	}
}

// A reset forgets what the program said about itself.
func TestAResetForgetsTheMessageAndTheProgress(t *testing.T) {
	term := New(40, 10, DefaultPalette(), 100, Callbacks{})
	term.Write([]byte("\x1b]9;working\x07\x1b]9;4;1;50\x07"))

	term.Write([]byte("\x1bc"))

	if text, _ := term.Notice(); text != "" {
		t.Errorf("%q survived a reset", text)
	}
	if got := term.Progress(); got != (Progress{}) {
		t.Errorf("%+v survived a reset", got)
	}
}
