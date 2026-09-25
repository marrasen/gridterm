package term

import (
	"strings"
	"testing"
	"time"

	"github.com/marrasen/gridterm/ui"
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
