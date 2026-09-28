package main

import (
	"image"
	"reflect"
	"sync"
	"testing"

	"github.com/marrasen/kakel/clip"
)

// testBoard is the clipboard the tests use in place of the system's.
var testBoard struct {
	mu   sync.Mutex
	text string
	png  []byte
}

// onAClipboardOfTheirOwn points the program's side at testBoard.
//
// The window's side copies through its gunim window, and an offscreen
// window has a clipboard of its own. The program's side reaches the
// system's clipboard through clip, in four places: clearing a copied
// secret as the window closes, reading a picture to paste, checking
// for one when a middle click finds no text, and putting a picture
// another window sent. A test that reached one of them with the real
// ones in place would read or replace what the user had copied.
func onAClipboardOfTheirOwn() {
	readClipboard = func() (string, error) {
		testBoard.mu.Lock()
		defer testBoard.mu.Unlock()
		return testBoard.text, nil
	}
	writeClipboard = func(s string) error {
		testBoard.mu.Lock()
		defer testBoard.mu.Unlock()
		testBoard.text = s
		return nil
	}
	readPicture = func() (image.Image, bool, error) { return nil, false, nil }
	takePicture = func(png []byte) error {
		testBoard.mu.Lock()
		defer testBoard.mu.Unlock()
		testBoard.png = append([]byte(nil), png...)
		return nil
	}
}

// Every way the program's side reaches a clipboard leads to the tests'
// own. A test once logged "Prompt copied" on Windows, and a copy that
// reached the system would have replaced what the user had copied.
func TestTheTestsCopyToAClipboardOfTheirOwn(t *testing.T) {
	for what, pair := range map[string][2]any{
		"reading text":      {readClipboard, clip.Text},
		"writing text":      {writeClipboard, clip.SetText},
		"reading a picture": {readPicture, clip.Image},
		"putting a picture": {takePicture, clip.SetPNG},
	} {
		if reflect.ValueOf(pair[0]).Pointer() == reflect.ValueOf(pair[1]).Pointer() {
			t.Errorf("%s goes to the system's clipboard", what)
		}
	}
}
