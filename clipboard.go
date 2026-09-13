package main

import (
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
	once sync.Once
	ch   chan string
}

func (c *clipboardWriter) set(text string) {
	if text == "" {
		return
	}
	c.once.Do(func() {
		c.ch = make(chan string, 8)
		go func() {
			for s := range c.ch {
				if err := clipboard.WriteAll(s); err != nil {
					log.Printf("clipboard: %v", err)
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

// read returns the clipboard contents, or "" if it cannot be read. It
// blocks, so it is called from the paste path only, where the user is
// already waiting.
func clipboardRead() string {
	s, err := clipboard.ReadAll()
	if err != nil {
		log.Printf("clipboard: %v", err)
		return ""
	}
	return s
}
