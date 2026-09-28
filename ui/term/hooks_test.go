package term

import (
	"strings"
	"testing"
	"time"

	"github.com/marrasen/kakel/input"
	"github.com/marrasen/kakel/ui"
)

func TestOutputIsToldOnceTheScreenHasIt(t *testing.T) {
	sess := newFakeSession()
	told := make(chan string, 4)
	var term *Terminal
	term, err := New(Config{Session: sess, Size: ui.Size{Cols: 20, Rows: 3}, OnOutput: func() {
		// By the time it is told, the screen already shows it.
		told <- term.Text()
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = term.Close() }()
	sess.out <- []byte("hello")
	select {
	case got := <-told:
		if !strings.HasPrefix(got, "hello") {
			t.Fatalf("when told, the screen read %q", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("output was never told")
	}
}

func TestAProgramsClipboardIsHandedOn(t *testing.T) {
	sess := newFakeSession()
	got := make(chan string, 1)
	term, err := New(Config{Session: sess, Size: ui.Size{Cols: 20, Rows: 3}, OnClipboard: func(s string) { got <- s }})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = term.Close() }()
	// OSC 52 with "hi" in base64.
	sess.out <- []byte("\x1b]52;c;aGk=\x07")
	select {
	case s := <-got:
		if s != "hi" {
			t.Fatalf("the clipboard was handed %q", s)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the clipboard was never handed on")
	}
}

// A host that hands a title to a goroutine busy reading the screen is
// told with the screen free. The window's loop does this: a flood of
// titles fills its queue, and it reads a pane before it takes the next.
func TestATitleIsToldWithTheScreenFree(t *testing.T) {
	sess := newFakeSession()
	titles := make(chan string)
	term, err := New(Config{Session: sess, Size: ui.Size{Cols: 20, Rows: 3}, OnTitle: func(s string) { titles <- s }})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = term.Close() }()
	sess.out <- []byte("\x1b]0;one\x07hello")
	read := make(chan string, 1)
	go func() {
		// Held until the screen has something on it, then read while
		// the title waits to be taken.
		for !strings.HasPrefix(term.Text(), "hello") {
			time.Sleep(time.Millisecond)
		}
		read <- term.Text()
	}()
	select {
	case got := <-read:
		if !strings.HasPrefix(got, "hello") {
			t.Fatalf("the screen read %q", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the screen could not be read while the title waited")
	}
	select {
	case got := <-titles:
		if got != "one" {
			t.Fatalf("the title was %q", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the title was never told")
	}
}

func TestTheMouseIsTakenWhileAProgramAsksAndShiftIsUp(t *testing.T) {
	sess := newFakeSession()
	said := make(chan struct{}, 4)
	term, err := New(Config{Session: sess, Size: ui.Size{Cols: 20, Rows: 3}, OnOutput: func() { said <- struct{}{} }})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = term.Close() }()
	if term.MouseTaken(0) {
		t.Fatal("before any program asked, the mouse is taken")
	}
	sess.out <- []byte("\x1b[?1000h")
	<-said
	if !term.MouseTaken(0) {
		t.Fatal("asked for, the mouse is not taken")
	}
	if term.MouseTaken(input.ModShift) {
		t.Fatal("with Shift down, the mouse is still taken")
	}
}
