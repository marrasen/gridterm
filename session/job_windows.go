//go:build windows

package session

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

// shellJob is the Windows job object a shell and everything it starts run
// in. Windows kills a job's processes when the last handle to it closes,
// and the kernel closes gridterm's handles however gridterm ends, so a
// crash or a kill no longer leaves a shell behind.
type shellJob windows.Handle

// holdShell puts a process, and everything it later starts, into a job
// object that kills its members when the job closes.
func holdShell(pid int) (shellJob, error) {
	h, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return 0, fmt.Errorf("create job object: %w", err)
	}
	limits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
			LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
		},
	}
	_, err = windows.SetInformationJobObject(h, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits)))
	if err != nil {
		_ = windows.CloseHandle(h)
		return 0, fmt.Errorf("set job object to kill on close: %w", err)
	}

	// The caller still holds the handle os.FindProcess opened, so the id
	// cannot have been reused by something else in the meantime.
	proc, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		_ = windows.CloseHandle(h)
		return 0, fmt.Errorf("open process %d: %w", pid, err)
	}
	defer func() { _ = windows.CloseHandle(proc) }()

	if err := windows.AssignProcessToJobObject(h, proc); err != nil {
		_ = windows.CloseHandle(h)
		return 0, fmt.Errorf("put process %d in a job object: %w", pid, err)
	}
	return shellJob(h), nil
}

// end closes the job, which terminates every process still in it, and
// reports whether it could. Ending a job twice is not safe, so the handle
// is forgotten on the way out.
func (j *shellJob) end() bool {
	if *j == 0 {
		return false
	}
	h := windows.Handle(*j)
	*j = 0
	return windows.CloseHandle(h) == nil
}
