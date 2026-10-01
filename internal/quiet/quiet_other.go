//go:build !windows

package quiet

import "os/exec"

// Hide does nothing here: no program is given a window it did not ask
// for.
func Hide(*exec.Cmd) {}

// ToParentConsole does nothing here: the output already goes to the
// terminal.
func ToParentConsole() {}
