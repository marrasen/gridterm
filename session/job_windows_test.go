//go:build windows

package session

import (
	"strings"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Closing a pane takes down what the shell started, not just the shell.
// A pane left running a build used to leave the build behind.
func TestClosingAPaneKillsWhatTheShellStarted(t *testing.T) {
	s := shell(t, "cmd.exe")
	l := s.(*local)

	select {
	case <-readerSeeing(t, s, ">"):
	case <-time.After(budget):
		t.Fatal("the shell never showed a prompt")
	}
	// start gives ping a console of its own, so closing the pane's
	// pseudoconsole does not take it down -- which is how a long job left
	// running in a pane behaves.
	if _, err := s.Write([]byte("start \"\" /min ping -n 600 127.0.0.1\r\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	pid := awaitChild(t, l.cmd.Process.Pid, "PING.EXE")
	gone := watchExit(t, pid)

	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	awaitExit(t, gone, "ping outlived the pane that started it")
}

// The shell dies with its job. Closing the job handle is what the kernel
// does for gridterm when gridterm ends, whether it exited, panicked or was
// killed, so this is the crash path as the kernel runs it.
func TestTheShellDiesWithItsJob(t *testing.T) {
	s := shell(t, "cmd.exe")
	l := s.(*local)

	select {
	case <-readerSeeing(t, s, ">"):
	case <-time.After(budget):
		t.Fatal("the shell never showed a prompt")
	}

	gone := watchExit(t, l.cmd.Process.Pid)
	if !l.job.end() {
		t.Fatal("no job held the shell")
	}
	awaitExit(t, gone, "the shell outlived the job it was in")
}

// awaitChild returns the id of a process named name under parent, waiting
// for it to appear.
func awaitChild(t *testing.T, parent int, name string) int {
	t.Helper()
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		if pid := findChild(t, parent, name); pid != 0 {
			return pid
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("no %s appeared under process %d", name, parent)
	return 0
}

// findChild returns the id of a process named name under parent, or zero.
func findChild(t *testing.T, parent int, name string) int {
	t.Helper()
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		t.Fatalf("snapshot the process list: %v", err)
	}
	defer func() { _ = windows.CloseHandle(snap) }()

	var e windows.ProcessEntry32
	e.Size = uint32(unsafe.Sizeof(e))
	for err := windows.Process32First(snap, &e); err == nil; err = windows.Process32Next(snap, &e) {
		if int(e.ParentProcessID) != parent {
			continue
		}
		if strings.EqualFold(windows.UTF16ToString(e.ExeFile[:]), name) {
			return int(e.ProcessID)
		}
	}
	return 0
}

// watchExit opens a process and returns a handle that is signalled when it
// exits. Holding the handle stops Windows reusing the id, so the wait
// cannot be answered by some later process.
func watchExit(t *testing.T, pid int) windows.Handle {
	t.Helper()
	h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		t.Fatalf("open process %d: %v", pid, err)
	}
	t.Cleanup(func() { _ = windows.CloseHandle(h) })
	return h
}

// awaitExit waits for a handle from watchExit and fails with why if the
// process is still running when the budget runs out.
func awaitExit(t *testing.T, h windows.Handle, why string) {
	t.Helper()
	got, err := windows.WaitForSingleObject(h, uint32(budget/time.Millisecond))
	if err != nil {
		t.Fatalf("wait for the process to exit: %v", err)
	}
	if got != windows.WAIT_OBJECT_0 {
		t.Fatalf("%s: the wait ended with 0x%x after %v", why, got, budget)
	}
}
