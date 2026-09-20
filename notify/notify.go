// Package notify shows a short message outside the window, in
// whatever the operating system uses for one.
//
// It is for something a program in a pane asked to have shown. The
// window puts that on the pane's row as well, so a machine with no
// notifications of its own loses nothing but the pop-up.
package notify

// Toaster shows messages outside the window.
//
// Show is called from the goroutine that draws, so it does not wait
// for the message to be seen or dismissed.
type Toaster interface {
	// Show puts one message up. A message that could not be shown is
	// reported rather than retried: the row already says the same
	// thing, so nothing is lost by giving up on the pop-up.
	Show(title, body string) error

	// Close takes away whatever the operating system was holding for
	// this program, such as an icon in the notification area.
	Close() error
}

// nothing is the Toaster for a machine with no notifications this
// knows how to use.
type nothing struct{}

func (nothing) Show(string, string) error { return nil }
func (nothing) Close() error              { return nil }
