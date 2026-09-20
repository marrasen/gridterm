package main

import (
	"fmt"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
)

// followLink opens a hyperlink a program put under its text.
//
// The address comes from whatever is running in the pane, which may be
// anything at all, so it is checked here as well as where it was read:
// only the schemes a browser is the right answer for, and never
// something that could hand a local program a command line.
func (a *app) followLink(at string) {
	if err := openInBrowser(at); err != nil {
		a.reportError("Could not open the link", err)
	}
}

// openInBrowser hands an address to whatever the machine opens links
// with.
func openInBrowser(at string) error {
	if err := linkIsOpenable(at); err != nil {
		return err
	}
	name, args := browserCommand(at)
	cmd := exec.Command(name, args...)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("could not open %s: %w", at, err)
	}
	// Not waited for: a browser outlives the window, and a window that
	// waited would hold a process that never ends.
	go func() { _ = cmd.Wait() }()
	return nil
}

// browserCommand is how this machine opens an address.
func browserCommand(at string) (string, []string) {
	switch runtime.GOOS {
	case "windows":
		// Through cmd's own start, because rundll32 mangles an address
		// holding an ampersand. The empty string is start's title
		// argument, which it needs before a quoted address.
		return "cmd", []string{"/c", "start", "", at}
	case "darwin":
		return "open", []string{at}
	}
	return "xdg-open", []string{at}
}

// linkIsOpenable says whether an address is one worth handing to a
// browser, and why not when it is not.
//
// The second check, after the emulator's. A link reaches here from a
// program in a pane, and the whole point of the feature is that the
// user clicks it: file:// would open whatever is on this disk, and a
// scheme a local handler is registered for would run that handler.
func linkIsOpenable(at string) error {
	at = strings.TrimSpace(at)
	if at == "" {
		return fmt.Errorf("there is no address to open")
	}
	u, err := url.Parse(at)
	if err != nil {
		return fmt.Errorf("%s is not an address: %w", at, err)
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https", "mailto", "ftp", "ftps":
	default:
		return fmt.Errorf(
			"gridterm opens web and mail links, and %q is a %q link",
			at, u.Scheme)
	}
	// A newline would end the command line and start another, and
	// nothing legitimate carries one.
	if strings.ContainsAny(at, "\r\n\x00") {
		return fmt.Errorf("that address has a line break in it, so it is not one")
	}
	return nil
}
