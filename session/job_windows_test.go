//go:build windows

package session

import (
	"os/exec"
	"strings"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// stillThere is how long a process has to stay running to count as left
// alone. Nothing waits on a clock, so this is slack.
const stillThere = 500 * time.Millisecond

// Closing a pane leaves work the user detached from it running. A pane is
// a console, and closing a console does not take down what was started
// apart from it.
func TestClosingAPaneLeavesDetachedWorkRunning(t *testing.T) {
	s := shell(t, "cmd.exe")
	l := s.(*local)

	select {
	case <-readerSeeing(t, s, ">"):
	case <-time.After(budget):
		t.Fatal("the shell never showed a prompt")
	}
	was := childrenNamed(t, l.cmd.Process.Pid, ping)
	// start gives ping a console of its own.
	if _, err := s.Write([]byte(startPing + "\r\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	pid := awaitChild(t, l.cmd.Process.Pid, ping, was)
	running := watchExit(t, pid)

	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	awaitStillRunning(t, running, "closing the pane killed work detached from it")
}

// A shell that exits on its own leaves detached work running too. The
// pane stays open on the transcript, and the window closes the session
// from there.
func TestAShellThatExitsLeavesDetachedWorkRunning(t *testing.T) {
	s := shell(t, "cmd.exe", "/c", startPing)
	l := s.(*local)
	go func() {
		b := make([]byte, 4096)
		for {
			if _, err := s.Read(b); err != nil {
				return
			}
		}
	}()

	pid := awaitChild(t, l.cmd.Process.Pid, ping, nil)
	running := watchExit(t, pid)
	if err := s.Wait(); err != nil {
		t.Fatalf("Wait: %v", err)
	}

	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	awaitStillRunning(t, running, "closing a pane whose shell had gone killed work detached from it")
}

// The shell dies with its job. Closing the job handle is what the kernel
// does for gridterm when gridterm ends any other way than through Close.
func TestTheShellDiesWithItsJob(t *testing.T) {
	s := shell(t, "cmd.exe")
	l := s.(*local)

	select {
	case <-readerSeeing(t, s, ">"):
	case <-time.After(budget):
		t.Fatal("the shell never showed a prompt")
	}

	gone := watchExit(t, l.cmd.Process.Pid)
	// Taken out of the session first, so the Close that follows has no
	// handle left to close twice.
	h := windows.Handle(l.job)
	l.job = 0
	if h == 0 {
		t.Fatal("no job held the shell")
	}
	if err := windows.CloseHandle(h); err != nil {
		t.Fatalf("close the job: %v", err)
	}
	awaitExit(t, gone, "the shell outlived the job it was in")
}

// A shell that has already exited is not held, and that is not a failure:
// there is nothing left to leak.
func TestAShellThatHasAlreadyExitedNeedsNoJob(t *testing.T) {
	c := exec.Command("cmd.exe", "/c", "exit 0")
	if err := c.Start(); err != nil {
		t.Fatalf("start a shell: %v", err)
	}
	t.Cleanup(func() { _ = c.Wait() })
	// The handle this opens is what keeps the id from being reused, the
	// way the one go-pty holds does for a real session.
	awaitExit(t, watchExit(t, c.Process.Pid), "the shell never exited")

	job, err := holdShell(c.Process.Pid)
	if err != nil {
		t.Fatalf("holdShell for a shell that had gone: %v", err)
	}
	if job != 0 {
		_ = job.letGo()
		t.Fatal("a job was made for a shell that had gone")
	}
}

// ping is the program the detached-work tests leave running, and
// startPing is the line that starts it with a console of its own. Thirty
// seconds outlives every budget here and is short enough that one left
// behind on a broken build goes by itself.
const (
	ping      = "PING.EXE"
	startPing = "start /min ping -n 30 127.0.0.1"
)

// awaitChild returns the id of a process named name under parent, waiting
// for one to appear that is not in was.
func awaitChild(t *testing.T, parent int, name string, was []int) int {
	t.Helper()
	before := map[int]bool{}
	for _, pid := range was {
		before[pid] = true
	}
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		for _, pid := range childrenNamed(t, parent, name) {
			if before[pid] {
				continue
			}
			t.Cleanup(func() { endProcess(pid) })
			return pid
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("no %s appeared under process %d", name, parent)
	return 0
}

// childrenNamed lists the ids of the processes named name under parent.
func childrenNamed(t *testing.T, parent int, name string) []int {
	t.Helper()
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		t.Fatalf("snapshot the process list: %v", err)
	}
	defer func() { _ = windows.CloseHandle(snap) }()

	var found []int
	var e windows.ProcessEntry32
	e.Size = uint32(unsafe.Sizeof(e))
	for err := windows.Process32First(snap, &e); err == nil; err = windows.Process32Next(snap, &e) {
		if int(e.ParentProcessID) != parent {
			continue
		}
		if strings.EqualFold(windows.UTF16ToString(e.ExeFile[:]), name) {
			found = append(found, int(e.ProcessID))
		}
	}
	return found
}

// watchExit opens a process and returns a handle that is signalled when
// it exits. Holding the handle stops Windows reusing the id, so a wait on
// it cannot be answered by some later process.
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

// awaitStillRunning fails with why if a handle from watchExit is
// signalled, which means the process went.
func awaitStillRunning(t *testing.T, h windows.Handle, why string) {
	t.Helper()
	got, err := windows.WaitForSingleObject(h, uint32(stillThere/time.Millisecond))
	if err != nil {
		t.Fatalf("wait on the process: %v", err)
	}
	if got != uint32(windows.WAIT_TIMEOUT) {
		t.Fatalf("%s: the wait ended with 0x%x inside %v", why, got, stillThere)
	}
}

// endProcess kills a process a test left running.
func endProcess(pid int) {
	h, err := windows.OpenProcess(windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		return
	}
	defer func() { _ = windows.CloseHandle(h) }()
	_ = windows.TerminateProcess(h, 1)
}
