package steps

import (
	"strings"
	"testing"
	"time"
)

// Every kind of step, read the way it is written.
func TestEachStepIsRead(t *testing.T) {
	for _, one := range []struct {
		step string
		want Step
	}{
		{step: "type:hello", want: Step{Kind: Type, Text: "hello"}},
		{step: "key:ctrl+c", want: Step{Kind: Key, Chord: "ctrl+c"}},
		{step: "wait:250", want: Step{Kind: Wait, Wait: 250 * time.Millisecond}},
		{step: "wait:0", want: Step{Kind: Wait}},
		{step: "until:$", want: Step{Kind: Until, Text: "$"}},
		{step: "until", want: Step{Kind: Until}},
		{step: "shot:/tmp/a.png", want: Step{Kind: Shot, Text: "/tmp/a.png"}},
		// Everything after the first colon is the argument, whole.
		{step: "type:echo a:b", want: Step{Kind: Type, Text: "echo a:b"}},
		{step: "until:error: 404", want: Step{Kind: Until, Text: "error: 404"}},
		// Spaces are part of what is typed, which is why a list of
		// steps is a list rather than a line.
		{step: "type:vim notes.md", want: Step{Kind: Type, Text: "vim notes.md"}},
	} {
		got, err := Parse(one.step)
		if err != nil {
			t.Errorf("Parse(%q): %v", one.step, err)
			continue
		}
		if got != one.want {
			t.Errorf("Parse(%q) = %+v, want %+v", one.step, got, one.want)
		}
		// And it says itself back the way it was read.
		if said := got.String(); said != one.step {
			t.Errorf("Parse(%q).String() = %q", one.step, said)
		}
	}
}

// A step that says nothing this understands is refused, with the step
// in the words of the refusal.
func TestAStepThatIsNotOneIsRefused(t *testing.T) {
	for _, one := range []string{
		"", "hello", "type", "type:", "key:", "shot:",
		"wait:soon", "wait:-1", "press:Enter", ":Enter",
	} {
		if got, err := Parse(one); err == nil {
			t.Errorf("Parse(%q) = %+v, want a refusal", one, got)
		} else if one != "" && !strings.Contains(err.Error(), one) {
			t.Errorf("Parse(%q) failed with %q, which does not say which step", one, err)
		}
	}
}

// A wait longer than a window will park for is refused where it is
// written, rather than being quietly cut down to size.
func TestAWaitPastTheLongestIsRefused(t *testing.T) {
	past := int(LongestWait/time.Millisecond) + 1
	if _, err := Parse("wait:" + itoa(past)); err == nil {
		t.Error("a wait past the longest was taken")
	}
	if _, err := Parse("wait:" + itoa(int(LongestWait/time.Millisecond))); err != nil {
		t.Errorf("the longest wait itself was refused: %v", err)
	}
}

// A list says which step it could not read, counted the way a person
// counts them.
func TestAListNamesTheStepItStoppedAt(t *testing.T) {
	_, err := ParseAll([]string{"type:ls", "key:Enter", "press:Enter"})
	if err == nil {
		t.Fatal("the list was taken")
	}
	if !strings.Contains(err.Error(), "step 3") {
		t.Errorf("it says %q, which does not say which step", err)
	}
}

// What a list may not go past.
func TestAListHasBounds(t *testing.T) {
	if _, err := ParseAll(nil); err == nil {
		t.Error("a list of nothing was taken")
	}
	many := make([]string, MostSteps+1)
	for i := range many {
		many[i] = "key:Enter"
	}
	if _, err := ParseAll(many); err == nil {
		t.Errorf("%d steps were taken, and %d is the most", len(many), MostSteps)
	}
	if _, err := ParseAll(many[:MostSteps]); err != nil {
		t.Errorf("the most steps a list may hold were refused: %v", err)
	}
	// Typing is bounded across the whole list, not step by step: a pane
	// filled a line at a time is still a pane being filled.
	long := []string{"type:" + strings.Repeat("x", MostText/2+1),
		"type:" + strings.Repeat("y", MostText/2+1)}
	if _, err := ParseAll(long); err == nil {
		t.Error("a list typing more than the most was taken")
	}
}

// itoa keeps the test from importing strconv for one line.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
