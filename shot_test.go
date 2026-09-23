package main

import (
	"strconv"
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

// An until step waits for the text to arrive, so the prompt that was
// already on the pane does not end it.
//
// Read against a real pane rather than against hand-written strings. A
// reading is always the whole screen, blank rows and all, so one line of
// output leaves two readings no longer lining up end to end: judged as
// raw text the whole screen counts as new, and the prompt above ends the
// wait before the command has finished. The rows that did carry over are
// what says which of them is new.
func TestWhatAnUntilStepCountsAsArriving(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	pane := onlyPaneOn(t, a)
	says := func(what, look string) {
		t.Helper()
		a.shells[0].out <- []byte(what)
		waitFor(t, a, "the pane to show "+strconv.Quote(look), func() bool {
			return strings.Contains(paneText(pane), look)
		})
	}
	says("$ ", "$")

	s, err := parseShotScript("type:build key:Enter until:$")
	if err != nil {
		t.Fatalf("read it: %v", err)
	}
	for range 3 {
		s.update(a.app)
	}
	if s.want != "$" {
		t.Fatalf("the script is waiting for %q", s.want)
	}

	// What the command prints, on the lines under the prompt. The prompt
	// itself is still where it was, and is not an answer to anything the
	// script has typed since.
	says("\r\nbuilding\r\n", "building")
	if !s.watching(a.app) {
		t.Error("the prompt that was already there ended the wait")
	}

	// The prompt the shell draws when the command is done does end it.
	a.shells[0].out <- []byte("$ ")
	waitFor(t, a, "the pane to show the prompt again", func() bool {
		return strings.Count(paneText(pane), "$") == 2
	})
	if s.watching(a.app) {
		t.Error("the new prompt did not end the wait")
	}
}

// A script whose wait runs out is a failed run.
//
// The pictures after that wait were never taken, so a run that exited
// saying nothing would leave whatever is looking at the files reading
// the ones from last time as if they were this run's.
func TestAScriptThatWaitsForNothingFails(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	s, err := parseShotScript("type:build until:nevermore shot:/tmp/never.png")
	if err != nil {
		t.Fatalf("read it: %v", err)
	}
	s.update(a.app)
	s.update(a.app)
	if s.want != "nevermore" {
		t.Fatalf("the script is waiting for %q", s.want)
	}

	// The last frame of its patience.
	s.left = 1
	s.update(a.app)

	if s.failed == nil {
		t.Fatal("it gave up without saying so, and the run exits 0")
	}
	if !strings.Contains(s.failed.Error(), "nevermore") {
		t.Errorf("it failed with %q, which does not say what it was waiting for", s.failed)
	}
	if !s.done || s.pending != "" {
		t.Error("it went on to the steps after the wait")
	}
	if !a.quit.Load() {
		t.Error("the window was left open")
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
