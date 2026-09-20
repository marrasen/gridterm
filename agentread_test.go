package main

import (
	"strings"
	"testing"

	"github.com/marrasen/gridterm/agent"
)

// A read of a few lines from a tall pane with its content near the
// top gives the content, not the blank rows off the bottom.
//
// It was three wasted calls in a real session: a 47-row pane with the
// cursor at row 15, and reads of 15, 20 and 30 lines all came back
// blank because the content was above the window every time.
func TestAShortReadOfATallPaneFindsTheContent(t *testing.T) {
	a := newTestApp(t, 100, 47)
	withPanel(t, a)
	withDialogs(t, a)
	a.commands()
	pane, code, c := handedOver(t, a)
	id := onlyPaneID(t, a, c, code)

	a.shells[0].out <- []byte("the line I am looking for\r\n$ ")
	waitFor(t, a, "the pane to say it", func() bool {
		return strings.Contains(paneText(pane), "the line I am looking for")
	})

	// One line is the prompt, which is the last line with anything on
	// it. What matters is that it is not a blank row off the bottom.
	if got := readAs(t, a, c, id, 1).Screen; strings.TrimSpace(got) == "" {
		t.Errorf("a read of one line gave %q", got)
	}
	for _, lines := range []int{2, 5, 15, 30} {
		look := readAs(t, a, c, id, lines)
		if !strings.Contains(look.Screen, "the line I am looking for") {
			t.Errorf("a read of %d lines gave %q", lines, look.Screen)
		}
	}
}

// And it says the blank rows were left out, so an answer shorter than
// the lines asked for is not a puzzle.
func TestAShortReadSaysTheBlanksWereLeftOut(t *testing.T) {
	a := newTestApp(t, 100, 47)
	withPanel(t, a)
	withDialogs(t, a)
	a.commands()
	pane, code, c := handedOver(t, a)
	id := onlyPaneID(t, a, c, code)
	a.shells[0].out <- []byte("one line\r\n")
	waitFor(t, a, "the pane to say it", func() bool {
		return strings.Contains(paneText(pane), "one line")
	})

	look := readAs(t, a, c, id, 30)

	if !look.Trimmed {
		t.Error("it did not say the blank rows were left out")
	}
}

// A pane with nothing on it at all still answers with a screen rather
// than with nothing, which would read as a failure.
func TestAnEmptyPaneStillAnswers(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	withDialogs(t, a)
	a.commands()
	_, code, c := handedOver(t, a)
	id := onlyPaneID(t, a, c, code)

	look := readAs(t, a, c, id, 10)

	if look.Screen == "" && !look.Trimmed {
		t.Error("an empty pane answered with nothing and did not say why")
	}
}

// A picture a program put in the pane is named in the reading. The
// cells under it read as blank, so an agent could otherwise not tell
// a picture that arrived from one that never did.
func TestAPictureIsInTheReading(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	withDialogs(t, a)
	a.commands()
	pane, code, c := handedOver(t, a)
	id := onlyPaneID(t, a, c, code)

	a.shells[0].out <- []byte(anInlinePNG(t, 10, 4))
	waitFor(t, a, "the pane to take the picture", func() bool {
		return len(pane.Pictures()) == 1
	})

	look := readAs(t, a, c, id, 24)

	if len(look.Pictures) != 1 {
		t.Fatalf("the reading names %d pictures", len(look.Pictures))
	}
	got := look.Pictures[0]
	if got.Rows != 4 || got.Cols != 10 {
		t.Errorf("it covers %dx%d cells, want 10x4", got.Cols, got.Rows)
	}
	if got.Width != 64 || got.Height != 64 {
		t.Errorf("it is %dx%d pixels", got.Width, got.Height)
	}
	if got.Wire {
		t.Error("a picture a program sent was put down as one from another window")
	}
}

// A pane with no pictures says nothing about pictures.
func TestAPaneWithNoPicturesNamesNone(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	withDialogs(t, a)
	a.commands()
	pane, code, c := handedOver(t, a)
	id := onlyPaneID(t, a, c, code)
	a.shells[0].out <- []byte("just text\r\n")
	waitFor(t, a, "the pane to say it", func() bool {
		return strings.Contains(paneText(pane), "just text")
	})

	look := readAs(t, a, c, id, 24)

	if len(look.Pictures) != 0 {
		t.Errorf("it names %d pictures", len(look.Pictures))
	}
}

// readAs is what an agent sees, asked the way an agent asks it: over
// the wire, off the drawing goroutine, with the window running.
func readAs(t *testing.T, a *testApp, c *agent.Client, id string, lines int) agent.Look {
	t.Helper()
	var look agent.Look
	offWindow(t, a, "the window to answer the agent", func() error {
		var err error
		look, err = c.Read(id, lines)
		return err
	})
	return look
}

// onlyPaneID is what the wire calls the one pane in a share.
func onlyPaneID(t *testing.T, a *testApp, c *agent.Client, code string) string {
	t.Helper()
	var got agent.Pane
	offWindow(t, a, "the window to list the share", func() error {
		var err error
		got, err = firstOf(c.Use(code))
		return err
	})
	return got.ID
}
