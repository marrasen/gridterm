package main

import (
	"log"
	"sync"

	"github.com/Xpra-org/go-xpra/ui"
)

// clipboard is the text clipboard the far application shares with this
// one.
//
// It is two one-way paths rather than a shared thing. The server calls
// SetText when the remote application has copied something, and a
// ui.ClipboardChange event goes the other way when the local one has.
// Neither side polls, and neither side can read the other's on demand:
// whoever copied last announced it.
type clipboard struct {
	mu sync.Mutex

	// text is the last thing the far side copied, and taken is how many
	// times it has done so. Both are for the report and the tests; a
	// real backend would put it on the platform clipboard here.
	text  string
	taken int
}

// SetText takes what the remote application copied.
func (c *clipboard) SetText(text string) error {
	c.mu.Lock()
	c.text = text
	c.taken++
	c.mu.Unlock()
	log.Printf("clipboard: the far side copied %q", text)
	return nil
}

// Text is the last thing the far side copied.
func (c *clipboard) Text() (string, int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.text, c.taken
}

// Clipboard makes the display a ui.ClipboardProvider.
//
// Returning nil would be the honest answer for a backend whose desktop
// has no clipboard service, and the client copes: it tells the server it
// cannot do clipboards at all rather than failing later.
func (d *display) Clipboard() ui.Clipboard { return d.board }

// copyLocally announces that this end has copied something, which is
// what gridterm would do when a pane's selection changes.
func (d *display) copyLocally(text string) {
	if text == "" {
		return
	}
	log.Printf("clipboard: this side copied %q", text)
	d.send(ui.ClipboardChange{Text: text})
}
