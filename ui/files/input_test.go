package files

import (
	"testing"

	"github.com/marrasen/gridterm/input"
)

// keyHandler and mouseHandler are anything a test can press a key on or
// click.
type keyHandler interface {
	HandleKey(input.Event) (bool, error)
}

type mouseHandler interface {
	HandleMouse(input.MouseEvent) (bool, error)
}

// keyTo sends a key and fails the test if the widget reported trouble.
//
// The answer matters: a test that pressed a key, ignored a failure and
// then asserted on what happened next would walk straight past the one
// thing that went wrong.
func keyTo(t *testing.T, w keyHandler, ev input.Event) bool {
	t.Helper()
	took, err := w.HandleKey(ev)
	if err != nil {
		t.Fatalf("%T handling %v: %v", w, ev.Key, err)
	}
	return took
}

// mouseTo sends a mouse event and fails the test if the widget reported
// trouble.
func mouseTo(t *testing.T, w mouseHandler, ev input.MouseEvent) bool {
	t.Helper()
	took, err := w.HandleMouse(ev)
	if err != nil {
		t.Fatalf("%T handling the mouse at %d,%d: %v", w, ev.Col, ev.Row, err)
	}
	return took
}
