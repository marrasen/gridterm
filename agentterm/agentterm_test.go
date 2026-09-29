package agentterm

import (
	"strings"
	"testing"

	"github.com/marrasen/kakel/agent"
	uiterm "github.com/marrasen/kakel/ui/term"
)

// An agent's asking is cut to one plain line.
func TestAnAgentsAskingIsCutToOnePlainLine(t *testing.T) {
	got := SecretLine("the \"db\" pass\u202eword\ue000 -- " + strings.Repeat("x", 200))
	// Only the agent's words, between the window's quotes.
	asked := got[strings.Index(got, `for: "`)+6 : strings.LastIndex(got, `" --`)]
	for _, bad := range []string{"\u202e", "\ue000", "\"", "--"} {
		if strings.Contains(asked, bad) {
			t.Errorf("the line keeps %q: %s", bad, got)
		}
	}
	if strings.Count(got, "x") > agent.MostSecretWords {
		t.Errorf("the line runs to %d characters of asking", strings.Count(got, "x"))
	}
}

func TestTheLastLinesAndTheBlankTail(t *testing.T) {
	for _, c := range []struct {
		text string
		n    int
		want string
	}{
		{"a\nb\nc", 2, "b\nc"},
		{"a\nb\nc", 5, "a\nb\nc"},
		{"a\nb\nc", 0, ""},
	} {
		if got := LastLines(c.text, c.n); got != c.want {
			t.Errorf("the last %d of %q are %q, want %q", c.n, c.text, got, c.want)
		}
	}
	if got := TrimBlankTail("a\nb\n  \n\n"); got != "a\nb" {
		t.Errorf("trimmed, it is %q", got)
	}
	if got := TrimBlankTail(" \n "); got != " \n " {
		t.Errorf("all blank, it is %q, want it as it was", got)
	}
	if n := CountLines("a\nb\n"); n != 3 {
		t.Errorf("it counts %d lines", n)
	}
}

// The prompt the agent typed at is back once it shows on a later line,
// and a command is the agent's once one finished after it typed.
func TestTypingKnowsWhenItsPromptIsBack(t *testing.T) {
	var h Typing
	if h.Watching() || h.Yours(5) {
		t.Fatal("before any typing it watches or claims a command")
	}
	h = Typing{at: "$ ", line: 10, done: 3, sent: true}
	read := func(line uint64, before string) uiterm.Reading {
		return uiterm.Reading{Line: line, Before: before}
	}
	if h.Back(read(10, "$ ")) || !h.Back(read(11, "$ ")) || h.Back(read(11, "> ")) {
		t.Fatal("the prompt is back only on a later line, as it was")
	}
	if !h.Watching() || h.Yours(3) || !h.Yours(4) {
		t.Fatal("a command finished after the typing is the agent's, and only that")
	}
}
