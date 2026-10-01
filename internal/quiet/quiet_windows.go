//go:build windows

package quiet

import (
	"os"
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// Hide has cmd start with no window of its own.
func Hide(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
	cmd.SysProcAttr.CreationFlags |= windows.CREATE_NO_WINDOW
}

// attachParent is kernel32's AttachConsole, and parentProcess its
// ATTACH_PARENT_PROCESS.
var attachParent = windows.NewLazySystemDLL("kernel32.dll").NewProc("AttachConsole")

const parentProcess = ^uintptr(0)

// ToParentConsole sends what the program prints to the console of the
// terminal it was started from, as a program linked without one has
// nowhere to print. It does nothing where the output already goes
// somewhere, as into a pipe, or where there is no such terminal.
func ToParentConsole() {
	if out, err := os.Stdout.Stat(); err == nil && out.Mode()&os.ModeNamedPipe != 0 {
		return
	}
	if r, _, _ := attachParent.Call(parentProcess); r == 0 {
		return
	}
	if f, err := os.OpenFile("CONOUT$", os.O_WRONLY, 0); err == nil {
		os.Stdout, os.Stderr = f, f
	}
}
