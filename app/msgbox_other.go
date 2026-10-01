//go:build !windows

package app

// messageBox shows nothing: kakel has a console here.
func messageBox(string, string, bool) bool { return false }

// confirm says yes: taking kakel away is asked for from a shell, where
// the command is the asking.
func confirm(string, string) bool { return true }
