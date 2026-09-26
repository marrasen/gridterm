//go:build !linux

package clip

import (
	"fmt"

	"github.com/atotto/clipboard"
)

// Text goes through atotto/clipboard everywhere but Linux: on Windows
// and macOS it calls the system's own clipboard and needs nothing
// installed. Linux is the one that shells out, which is why it has its
// own -- see clipboard_linux.go.

// SetText puts text on the clipboard.
func SetText(s string) error { return clipboard.WriteAll(s) }

// Text returns the text on the clipboard.
//
// It blocks, so it is called from the paste path only, where the user
// is already waiting. A failure is returned rather than pasted as
// nothing: a paste that does nothing looks exactly like an empty
// clipboard, and the user tries again instead of being told why.
func Text() (string, error) {
	s, err := clipboard.ReadAll()
	if err != nil {
		return "", fmt.Errorf("read the clipboard: %w", err)
	}
	return s, nil
}
