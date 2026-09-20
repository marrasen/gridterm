package vt

import (
	"strings"
	"testing"
)

// A program asks which terminal this is with XTVERSION, and gets the
// name and the version back as a DCS string. It is the one way of
// asking that survives ssh and tmux, where an environment variable
// does not.
func TestXTVersionNamesTheTerminal(t *testing.T) {
	h := newHarness(t, 20, 5)
	h.term.SetProgram("gridterm 1.2.3")

	h.write("\x1b[>q")

	if len(h.replies) != 1 {
		t.Fatalf("replies = %q, want the one answer", h.replies)
	}
	if want := "\x1bP>|gridterm 1.2.3\x1b\\"; h.replies[0] != want {
		t.Errorf("it answers %q, want %q", h.replies[0], want)
	}
}

// A terminal with no name says nothing, which is what a terminal that
// has never heard of the sequence does. Answering with an empty name
// would have a program believe it had found one.
func TestATerminalWithNoNameAnswersNothing(t *testing.T) {
	h := newHarness(t, 20, 5)

	h.write("\x1b[>q")

	if len(h.replies) != 0 {
		t.Errorf("it answers %q, want nothing", h.replies)
	}
}

// The answer is not printed. A reply that reached the screen would put
// its own name in the middle of whatever the program was drawing.
func TestTheVersionAnswerIsNotPrinted(t *testing.T) {
	h := newHarness(t, 20, 5)
	h.term.SetProgram("gridterm 1.2.3")

	h.write("before\x1b[>qafter")

	if got := strings.Join(h.lines(), ""); got != "beforeafter" {
		t.Errorf("the screen says %q, want the answer kept off it", got)
	}
}

// The greater-than is what makes q XTVERSION. Without it, q is
// DECSCUSR with a space, and nothing at all on its own.
func TestOnlyTheRightQIsTheVersionQuestion(t *testing.T) {
	for what, sent := range map[string]string{
		"a bare q":           "\x1b[q",
		"the cursor style":   "\x1b[ q",
		"a question mark":    "\x1b[?q",
		"a different letter": "\x1b[>c",
	} {
		h := newHarness(t, 20, 5)
		h.term.SetProgram("gridterm 1.2.3")

		h.write(sent)

		for _, said := range h.replies {
			if strings.Contains(said, "gridterm") {
				t.Errorf("%s answered with the version: %q", what, said)
			}
		}
	}
}

// A parameter in front of the q is still XTVERSION. xterm defines only
// 0, and a program that sends it means the same question.
func TestAParameterDoesNotStopTheVersionAnswer(t *testing.T) {
	h := newHarness(t, 20, 5)
	h.term.SetProgram("gridterm 1.2.3")

	h.write("\x1b[>0q")

	if len(h.replies) != 1 || !strings.Contains(h.replies[0], "gridterm 1.2.3") {
		t.Errorf("it answers %q, want the name and version", h.replies)
	}
}

// DA1 is left alone. It says what the terminal can do rather than which
// one it is, and a claim added there is a claim about a capability.
func TestTheVersionAnswerDoesNotChangeDeviceAttributes(t *testing.T) {
	h := newHarness(t, 20, 5)
	h.term.SetProgram("gridterm 1.2.3")

	h.write("\x1b[c")

	if len(h.replies) != 1 || h.replies[0] != "\x1b[?6c" {
		t.Errorf("DA1 answers %q, want it unchanged", h.replies)
	}
}
