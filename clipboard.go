package main

import (
	"fmt"
	"log"
	"sync"

	"github.com/atotto/clipboard"
)

// Clipboard access goes through a goroutine.
//
// On Linux the clipboard is owned by a running process, so the library
// shells out to xclip or xsel — which can take tens of milliseconds, or
// block outright if neither is installed. Doing that on the UI thread
// would stutter the window on every copy.
type clipboardWriter struct {
	// write puts text on the clipboard. Empty means the system's own,
	// and a test sets its own: a test run must not reach into the
	// clipboard of whoever is running it.
	write func(string) error

	// failed is told when a copy could not be made. Something the user
	// was told is on the clipboard and is not is worth saying: they
	// will go looking for it.
	failed func(error)

	once sync.Once
	ch   chan string
}

func (c *clipboardWriter) set(text string) {
	if text == "" {
		return
	}
	c.once.Do(func() {
		put := c.write
		if put == nil {
			put = clipboard.WriteAll
		}
		c.ch = make(chan string, 8)
		go func() {
			for s := range c.ch {
				if err := put(s); err != nil {
					if c.failed != nil {
						c.failed(err)
					} else {
						log.Printf("clipboard: %v", err)
					}
				}
			}
		}()
	})
	select {
	case c.ch <- text:
	default:
		// The clipboard helper is wedged. Dropping a copy is better than
		// blocking the window on it.
	}
}

// pasteText is what a widget pastes: the clipboard, with a failure shown
// rather than pasted as nothing.
//
// The toolkit's paste hooks hand back text and no error, so a widget
// that could not read the clipboard has nowhere to report it. This is
// where it is reported, on the goroutine that draws, which is the only
// one that calls it.
func (a *app) pasteText() string {
	read := a.readClip
	if read == nil {
		read = clipboardRead
	}
	s, err := read()
	if err != nil {
		a.reportError("Could not paste", err)
		return ""
	}
	return s
}

// clipboardRead returns what is on the clipboard.
//
// It blocks, so it is called from the paste path only, where the user is
// already waiting. A failure is returned rather than pasted as nothing:
// a paste that does nothing looks exactly like an empty clipboard, and
// the user tries again instead of being told why.
func clipboardRead() (string, error) {
	s, err := clipboard.ReadAll()
	if err != nil {
		return "", fmt.Errorf("read the clipboard: %w", err)
	}
	return s, nil
}
