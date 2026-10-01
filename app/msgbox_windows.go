//go:build windows

package app

import "golang.org/x/sys/windows"

// messageBox shows text in a message box, and reports that it did.
func messageBox(title, text string, problem bool) bool {
	t, _ := windows.UTF16PtrFromString(title)
	m, _ := windows.UTF16PtrFromString(text)
	flags := uint32(windows.MB_OK | windows.MB_ICONINFORMATION)
	if problem {
		flags = windows.MB_OK | windows.MB_ICONERROR
	}
	_, _ = windows.MessageBox(0, m, t, flags)
	return true
}

// confirm asks a yes or no question in a message box.
func confirm(title, text string) bool {
	t, _ := windows.UTF16PtrFromString(title)
	m, _ := windows.UTF16PtrFromString(text)
	ans, err := windows.MessageBox(0, m, t, windows.MB_YESNO|windows.MB_ICONQUESTION)
	// IDYES.
	return err == nil && ans == 6
}
