//go:build windows && amd64

package conpty

import (
	"io"
	"os"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// openConsoles counts the OpenConsole processes running from where
// kakel puts it.
func openConsoles(t *testing.T) int {
	t.Helper()
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = windows.CloseHandle(snap) }()
	var e windows.ProcessEntry32
	e.Size = uint32(unsafe.Sizeof(e))
	n := 0
	for err = windows.Process32First(snap, &e); err == nil; err = windows.Process32Next(snap, &e) {
		if windows.UTF16ToString(e.ExeFile[:]) != "OpenConsole.exe" {
			continue
		}
		h, oerr := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, e.ProcessID)
		if oerr != nil {
			continue
		}
		buf := make([]uint16, windows.MAX_PATH)
		size := uint32(len(buf))
		if windows.QueryFullProcessImageName(h, 0, &buf[0], &size) == nil && windows.UTF16ToString(buf[:size]) == placeDir()+`\OpenConsole.exe` {
			n++
		}
		_ = windows.CloseHandle(h)
	}
	return n
}

// A console closed while its program runs, and the program then
// killed, as a pane closes, leaves no OpenConsole running.
func TestAConsoleClosedUnderItsProgramEndsItsHost(t *testing.T) {
	if chosen() == system {
		t.Skip("OpenConsole is not in use")
	}
	before := openConsoles(t)
	c, err := New(80, 24)
	if err != nil {
		t.Fatal(err)
	}
	cmd := `C:\Windows\System32\ping.exe`
	p, err := c.Start(cmd, []string{cmd, "-n", "30", "127.0.0.1"}, "", os.Environ())
	if err != nil {
		t.Fatal(err)
	}
	go func() { _, _ = io.Copy(io.Discard, c) }()
	c.Release()
	_ = p.Kill()
	_, _ = p.Wait()
	_ = c.Close()
	deadline := time.Now().Add(5 * time.Second)
	for openConsoles(t) > before {
		if time.Now().After(deadline) {
			t.Fatalf("%d OpenConsole running after the console was closed, %d before", openConsoles(t), before)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// A console's OpenConsole is held in the job that kills it as kakel
// ends. Windows' own console host ends with the process that made it;
// an OpenConsole was seen to stay, with nothing in it, after a test
// binary ended with a pane open.
func TestAConsoleEndsWithKakel(t *testing.T) {
	if chosen() == system {
		t.Skip("OpenConsole is not in use")
	}
	c, err := New(80, 24)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	job, err := hosts()
	if err != nil {
		t.Fatal(err)
	}
	handles := (*[3]windows.Handle)(*(*unsafe.Pointer)(unsafe.Pointer(&c.hpc)))
	var in int32
	isProcessInJob := windows.NewLazySystemDLL("kernel32.dll").NewProc("IsProcessInJob")
	if r, _, err := isProcessInJob.Call(uintptr(handles[2]), uintptr(job), uintptr(unsafe.Pointer(&in))); r == 0 {
		t.Fatal(err)
	}
	if in == 0 {
		t.Fatal("the console's OpenConsole is not held to end with kakel")
	}
}

// A console closed with nothing ever started in it, as when the program
// cannot be found, leaves no OpenConsole running.
func TestAConsoleNeverUsedEndsItsHost(t *testing.T) {
	if chosen() == system {
		t.Skip("OpenConsole is not in use")
	}
	before := openConsoles(t)
	c, err := New(80, 24)
	if err != nil {
		t.Fatal(err)
	}
	_ = c.Close()
	deadline := time.Now().Add(5 * time.Second)
	for openConsoles(t) > before {
		if time.Now().After(deadline) {
			t.Fatalf("%d OpenConsole running after the console was closed, %d before", openConsoles(t), before)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
