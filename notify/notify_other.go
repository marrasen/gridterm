//go:build !windows

package notify

// New returns something that shows messages outside the window, and on
// a machine with none this knows how to use it shows nothing.
//
// The message is on the pane's row either way, so what is lost is the
// pop-up rather than the message.
func New(string) Toaster { return nothing{} }
