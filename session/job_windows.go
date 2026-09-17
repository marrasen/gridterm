//go:build windows

package session

import (
	"fmt"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

// stillActive is the exit code Windows gives for a process that has not
// exited.
const stillActive = 259

// jobLimits are the limits a shell's job carries. Kill-on-close takes the
// shell down with gridterm. Breakaway-ok lets a program that asks to
// leave the job leave it, which installers and launcher stubs do.
const jobLimits = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE |
	windows.JOB_OBJECT_LIMIT_BREAKAWAY_OK

// shellJob is the Windows job object a shell and everything it starts run
// in. Windows kills the job's processes when the last handle to it
// closes, and the kernel closes gridterm's handles however gridterm ends.
type shellJob windows.Handle

// jobFailed is a failure to hold a shell in a job object, worded for the
// user who reads it in a dialog. doing names the step for a reader of
// the log, after the reason rather than in front of it.
func jobFailed(doing string, err error) error {
	return fmt.Errorf(
		"Windows would not put this shell in a job object."+
			" That is what closes a shell, and everything it started, when gridterm"+
			" closes. %w (%s)", err, doing)
}

// holdShell puts a process, and everything it later starts, in a job
// object that kills its members when the job closes. A shell that has
// already exited gives back no job: there is nothing left to hold.
//
// Every failure is worded here, so none of the steps can word one of its
// own.
func holdShell(pid int) (shellJob, error) {
	j, doing, err := hold(pid)
	if err != nil {
		return 0, jobFailed(doing, err)
	}
	return j, nil
}

// hold does the work of holdShell and says which step it was on when it
// failed.
func hold(pid int) (shellJob, string, error) {
	h, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return 0, "making the job object", err
	}
	if err := limitJob(h, jobLimits); err != nil {
		_ = windows.CloseHandle(h)
		return 0, "setting its limits", err
	}

	// go-pty still holds a handle to the process, so the id is still this
	// shell's.
	rights := uint32(windows.PROCESS_SET_QUOTA |
		windows.PROCESS_TERMINATE |
		windows.PROCESS_QUERY_LIMITED_INFORMATION)
	proc, err := windows.OpenProcess(rights, false, uint32(pid))
	if err != nil {
		_ = windows.CloseHandle(h)
		return 0, fmt.Sprintf("opening process %d", pid), err
	}
	defer func() { _ = windows.CloseHandle(proc) }()

	if err := windows.AssignProcessToJobObject(h, proc); err != nil {
		_ = windows.CloseHandle(h)
		if exited(proc) {
			return 0, "", nil
		}
		return 0, fmt.Sprintf("putting process %d in it", pid), err
	}
	return shellJob(h), "", nil
}

// letGo takes kill-on-close off the job and closes it, leaving the shell
// and anything it started running. Close goes this way because the job is
// there for a gridterm that never reaches Close.
//
// The handle stays open when the limits cannot be changed: closing it
// then would kill the tree this is sparing.
func (j *shellJob) letGo() error {
	if *j == 0 {
		return nil
	}
	h := windows.Handle(*j)
	if err := limitJob(h, 0); err != nil {
		return fmt.Errorf("take kill-on-close off the job object: %w", err)
	}
	// Forgotten before the close, so nothing can close it twice. Only
	// Close calls this, and sync.Once is what orders that.
	*j = 0
	return windows.CloseHandle(h)
}

// limitJob puts limit flags on a job object.
func limitJob(h windows.Handle, flags uint32) error {
	limits := newLimits(flags)
	_, err := windows.SetInformationJobObject(h, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(limits)), uint32(unsafe.Sizeof(*limits)))
	runtime.KeepAlive(limits)
	return err
}

// newLimits returns job limits on the heap, where the address stays put:
// SetInformationJobObject takes it as a uintptr, which the collector cannot
// see and a stack that grew under the call would leave behind.
//
//go:noinline
func newLimits(flags uint32) *windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION {
	return &windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
			LimitFlags: flags,
		},
	}
}

// exited reports whether a process has already gone.
func exited(proc windows.Handle) bool {
	var code uint32
	return windows.GetExitCodeProcess(proc, &code) == nil && code != stillActive
}
