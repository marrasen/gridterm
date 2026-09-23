package main

import (
	"strings"
	"testing"
	"time"
)

// A screenshot script is read in the steps every pane takes, so a step
// learned in one place is a step learned in both.
func TestAScreenshotScriptIsReadInSteps(t *testing.T) {
	got, err := parseShotScript("wait:250 until:$ key:ctrl+shift+k type:about shot:/tmp/a.png")
	if err != nil {
		t.Fatalf("read it: %v", err)
	}
	if len(got.steps) != 5 {
		t.Fatalf("it read %d steps, want 5", len(got.steps))
	}
	// Milliseconds, counted in frames so a slow machine draws the same
	// script the same way.
	if want := framesFor(250 * time.Millisecond); framesFor(got.steps[0].Wait) != want {
		t.Errorf("the wait is %d frames, want %d", framesFor(got.steps[0].Wait), want)
	}
}

// A wait is counted in frames, rounded up, so the shortest wait is a
// frame rather than none.
func TestAWaitIsCountedInFrames(t *testing.T) {
	for _, one := range []struct {
		d    time.Duration
		want int
	}{
		{d: 0, want: 0},
		{d: time.Millisecond, want: 1},
		{d: time.Second, want: shotTicks},
		{d: 250 * time.Millisecond, want: shotTicks / 4},
	} {
		if got := framesFor(one.d); got != one.want {
			t.Errorf("framesFor(%s) = %d, want %d", one.d, got, one.want)
		}
	}
}

// The steps a screenshot script cannot do are refused by name, before a
// window has been opened and pictures taken.
func TestAScreenshotScriptRefusesWhatItCannotDo(t *testing.T) {
	for _, one := range []struct {
		script string
		say    string
	}{
		{script: "until", say: "no way to ask"},
		{script: "require:$", say: "list of steps"},
		{script: "fail:oops", say: "list of steps"},
		{script: "key:Nonesuch", say: "Nonesuch"},
		{script: "press:Enter", say: "not a step"},
	} {
		got, err := parseShotScript(one.script)
		if err == nil {
			t.Errorf("%q was taken: %+v", one.script, got)
			continue
		}
		if !strings.Contains(err.Error(), one.say) {
			t.Errorf("%q failed with %q, which does not say %q", one.script, err, one.say)
		}
	}
	// And an empty script is an ordinary run, not a failure.
	if got, err := parseShotScript("   "); err != nil || got != nil {
		t.Errorf("an empty script gave %+v, %v", got, err)
	}
}

// An until step waits for the text to arrive, so it is not ended by the
// echo of what the script has just typed.
func TestWhatAnUntilStepCountsAsArriving(t *testing.T) {
	for _, one := range []struct {
		name     string
		was, now string
		want     string
	}{
		{name: "the echo of what was typed",
			was: "$ echo done\n", now: "$ echo done\n", want: ""},
		{name: "and the answer after it",
			was: "$ echo done\n", now: "$ echo done\ndone\n$ ", want: "done\n$ "},
		{name: "a pane that had nothing",
			was: "", now: "$ ", want: "$ "},
		{name: "a pane that scrolled a screenful past",
			was: "old", now: "quite new", want: "quite new"},
	} {
		if got := addedTo(one.was, one.now); got != one.want {
			t.Errorf("%s: addedTo(%q, %q) = %q, want %q",
				one.name, one.was, one.now, got, one.want)
		}
	}
}

// A screenshot script may open by waiting for the prompt.
//
// Nothing has been typed, so there is nothing for the text to be an
// answer to and the pane is taken as it is. Anchored, that step would
// wait out its whole patience for a prompt drawn before the script
// began.
func TestAScriptMayOpenByWaitingForWhatIsAlreadyThere(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	s, err := parseShotScript("until:$ type:ls")
	if err != nil {
		t.Fatalf("read it: %v", err)
	}

	// The first step runs and takes the pane as it stands.
	s.update(a.app)
	if s.was != "" {
		t.Errorf("it measured against %q, want the pane as it is", s.was)
	}
	// And once something has been typed, a wait is measured from there.
	s.want = ""
	s.update(a.app)
	if !s.typed {
		t.Fatal("typing was not noticed")
	}
}
