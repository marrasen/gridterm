//go:build windows

package conpty

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// host is a ConPTY: its three calls, and its name for the log.
type host struct {
	name                  string
	create, resize, close *windows.LazyProc
	// ready makes sure the files of a host kakel carries are where it
	// was loaded from, putting back any that are not, and exe is where
	// its OpenConsole.exe is; nil and empty for Windows' own.
	ready func() error
	exe   string
}

// system is the ConPTY Windows has.
var system = func() *host {
	k := windows.NewLazySystemDLL("kernel32.dll")
	return &host{
		name:   "the ConPTY of Windows",
		create: k.NewProc("CreatePseudoConsole"),
		resize: k.NewProc("ResizePseudoConsole"),
		close:  k.NewProc("ClosePseudoConsole"),
	}
}()

// chosen is the ConPTY consoles are made with: OpenConsole, unless it
// could not be put in place or loaded. It is decided once, and said in
// the log.
var chosen = sync.OnceValue(func() *host {
	h, err := bundled()
	if err != nil {
		log.Printf("Local panes run through the ConPTY of Windows, which can tear a full-screen animation: %v", err)
		return system
	}
	return h
})

// Console is a pseudoconsole and the two pipes to it.
type Console struct {
	host *host
	// in takes the program's input, and out gives its output.
	in, out *os.File

	// mu orders freeing the pseudoconsole against resizing it and
	// starting a program in it, which would use a handle no longer
	// there. Reading and writing stay outside it.
	mu       sync.RWMutex
	hpc      windows.Handle
	released bool
}

// New makes a pseudoconsole of cols by rows cells.
//
// conpty.dll starts OpenConsole.exe from beside itself, and starts
// Windows' own console host, without a word, when it is not there. A
// file there can go or change while kakel runs, as a cleaner of caches
// or another program can make it, so each is compared with what kakel
// carries, and put back, before each console. Windows' own ConPTY is
// used if that, or OpenConsole, fails.
func New(cols, rows int) (*Console, error) { return newFrom(chosen(), cols, rows) }

// newFrom makes a pseudoconsole with h, as New says.
func newFrom(h *host, cols, rows int) (*Console, error) {
	if h.ready == nil {
		return newWith(h, cols, rows)
	}
	err := h.ready()
	if err == nil {
		var c *Console
		if c, err = newWith(h, cols, rows); err == nil {
			return c, nil
		}
	}
	fallback.Do(func() {
		log.Printf("Local panes run through the ConPTY of Windows, which can tear a full-screen animation: %v", err)
	})
	return newWith(system, cols, rows)
}

// fallback says once that OpenConsole failed after it was chosen.
var fallback sync.Once

// newWith makes a pseudoconsole with the ConPTY h.
func newWith(h *host, cols, rows int) (*Console, error) {
	// The console host reads the far end of one pipe and writes the far
	// end of the other. Once it has them, this process lets go of its
	// copies, so the output pipe breaks when the host ends.
	inFar, in, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("pipe for a pseudoconsole: %w", err)
	}
	out, outFar, err := os.Pipe()
	if err != nil {
		_, _ = inFar.Close(), in.Close()
		return nil, fmt.Errorf("pipe for a pseudoconsole: %w", err)
	}
	var hpc windows.Handle
	r, _, _ := h.create.Call(uintptr(coord(cols, rows)), inFar.Fd(), outFar.Fd(), 0, uintptr(unsafe.Pointer(&hpc)))
	_, _ = inFar.Close(), outFar.Close()
	if r != 0 {
		_, _ = in.Close(), out.Close()
		return nil, fmt.Errorf("make a pseudoconsole with %s: %w", h.name, windows.Errno(r&0xffff))
	}
	if h.exe != "" {
		holdHost(h, hpc)
	}
	return &Console{host: h, in: in, out: out, hpc: hpc}, nil
}

// hosts is a job that holds every OpenConsole kakel starts, and kills
// them as kakel ends, however it ends. Windows' own console host ends
// with the process that made it; OpenConsole was seen to stay, with
// nothing in it, after a kakel that ended with a pane open.
var hosts = sync.OnceValues(func() (windows.Handle, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return 0, err
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		_ = windows.CloseHandle(job)
		return 0, err
	}
	return job, nil
})

// holdHost puts the OpenConsole behind hpc, of h, in hosts. conpty.dll's
// HPCON points at the console's signal pipe, a reference to it, and its
// host process, in that order: what ConptyPackPseudoConsole packs. That
// layout is conpty.dll's own, not promised, so the handle is held only
// if it is the OpenConsole.exe kakel placed; a conpty.dll laid out
// otherwise must not have kakel kill some other process as it ends. A
// console it cannot hold still works, and that is said once in the log.
func holdHost(h *host, hpc windows.Handle) {
	job, err := hosts()
	if err == nil {
		handles := (*[3]windows.Handle)(*(*unsafe.Pointer)(unsafe.Pointer(&hpc)))
		err = holdProcess(job, handles[2], h.exe)
	}
	if err != nil {
		unheld.Do(func() { log.Printf("An OpenConsole may outlive kakel: %v", err) })
	}
}

// Dir is the folder kakel puts the OpenConsole it carries in, a folder
// for each version: %LOCALAPPDATA%\kakel\conpty. It is the user's cache,
// not kakel's own folder, which a portable copy keeps beside itself,
// and -uninstall takes it away.
func Dir() (string, error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(cache, "kakel", "conpty"), nil
}

// unheld says once that an OpenConsole could not be held.
var unheld sync.Once

// holdProcess puts the process proc in job, if it runs the program at
// exe, and refuses it otherwise.
func holdProcess(job, proc windows.Handle, exe string) error {
	buf := make([]uint16, windows.MAX_LONG_PATH)
	n := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(proc, 0, &buf[0], &n); err != nil {
		return fmt.Errorf("the console's host is not a process kakel can name: %w", err)
	}
	if got := windows.UTF16ToString(buf[:n]); !samePath(got, exe) {
		return fmt.Errorf("the console's host is %s, not %s", got, exe)
	}
	return windows.AssignProcessToJobObject(job, proc)
}

// samePath reports whether a and b name the same file, as Windows
// compares names: without regard to case.
func samePath(a, b string) bool {
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}

// coord packs a size as a COORD, which a ConPTY takes by value.
func coord(cols, rows int) uint32 {
	cols, rows = min(max(cols, 1), 0x7fff), min(max(rows, 1), 0x7fff)
	return uint32(cols) | uint32(rows)<<16
}

// Host names the ConPTY the console runs in.
func (c *Console) Host() string { return c.host.name }

// Read reads the program's output.
func (c *Console) Read(b []byte) (int, error) { return c.out.Read(b) }

// Write writes the program's input.
func (c *Console) Write(b []byte) (int, error) { return c.in.Write(b) }

// Resize gives the console a new size. Once it has been released there
// is nothing to resize.
func (c *Console) Resize(cols, rows int) error {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.released {
		return nil
	}
	if r, _, _ := c.host.resize.Call(uintptr(c.hpc), uintptr(coord(cols, rows))); r != 0 {
		return fmt.Errorf("resize the pseudoconsole: %w", windows.Errno(r&0xffff))
	}
	return nil
}

// Release frees the pseudoconsole and leaves the pipes open, so a read
// drains what the program wrote last. It reports whether there was
// anything to free.
func (c *Console) Release() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.released {
		return false
	}
	c.released = true
	_, _, _ = c.host.close.Call(uintptr(c.hpc))
	return true
}

// Close frees the pseudoconsole, if Release has not, and closes the
// pipes.
func (c *Console) Close() error {
	c.Release()
	return errors.Join(c.in.Close(), c.out.Close())
}

// Start runs the program at path in the console, with args as its
// command line, args[0] its name; in dir, or this process's directory
// when empty; with env as its whole environment.
func (c *Console) Start(path string, args []string, dir string, env []string) (*os.Process, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.released {
		return nil, errors.New("start a program: the pseudoconsole is closed")
	}
	attrs, err := windows.NewProcThreadAttributeList(1)
	if err != nil {
		return nil, fmt.Errorf("start a program: %w", err)
	}
	defer attrs.Delete()
	// The attribute's value is the handle itself, not where it is kept.
	hpc := *(*unsafe.Pointer)(unsafe.Pointer(&c.hpc))
	if err := attrs.Update(windows.PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE, hpc, unsafe.Sizeof(c.hpc)); err != nil {
		return nil, fmt.Errorf("start a program in the pseudoconsole: %w", err)
	}
	si := &windows.StartupInfoEx{ProcThreadAttributeList: attrs.List()}
	si.Cb = uint32(unsafe.Sizeof(*si))
	// No handles of this process's own: a program would otherwise write
	// to wherever kakel's standard output goes, rather than the console.
	si.Flags = windows.STARTF_USESTDHANDLES

	pathp, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	line, err := windows.UTF16PtrFromString(windows.ComposeCommandLine(args))
	if err != nil {
		return nil, err
	}
	var dirp *uint16
	if dir != "" {
		if dirp, err = windows.UTF16PtrFromString(dir); err != nil {
			return nil, err
		}
	}
	block, err := envBlock(env)
	if err != nil {
		return nil, err
	}
	var pi windows.ProcessInformation
	flags := uint32(windows.CREATE_UNICODE_ENVIRONMENT | windows.EXTENDED_STARTUPINFO_PRESENT)
	if err := windows.CreateProcess(pathp, line, nil, nil, false, flags, &block[0], dirp, &si.StartupInfo, &pi); err != nil {
		return nil, fmt.Errorf("start %s: %w", path, err)
	}
	defer func() { _ = windows.CloseHandle(pi.Thread) }()
	defer func() { _ = windows.CloseHandle(pi.Process) }()
	p, err := os.FindProcess(int(pi.ProcessId))
	if err != nil {
		_ = windows.TerminateProcess(pi.Process, 1)
		return nil, fmt.Errorf("start %s: %w", path, err)
	}
	return p, nil
}

// envBlock is env as CreateProcess takes it: each NAME=value ended by a
// NUL, and the block by one more. A name set twice, in any case, keeps
// its last value, as os/exec does. SYSTEMROOT is added when env lacks
// it, which much of Windows will not run without.
func envBlock(env []string) ([]uint16, error) {
	at := map[string]int{}
	var kept []string
	for _, kv := range env {
		// A name may begin with =, as =C: does, which keeps the current
		// directory of drive C.
		name, _, ok := strings.Cut(kv[min(1, len(kv)):], "=")
		if !ok {
			continue
		}
		key := strings.ToUpper(kv[:min(1, len(kv))] + name)
		if i, seen := at[key]; seen {
			kept[i] = kv
			continue
		}
		at[key] = len(kept)
		kept = append(kept, kv)
	}
	if _, ok := at["SYSTEMROOT"]; !ok {
		kept = append(kept, "SYSTEMROOT="+os.Getenv("SYSTEMROOT"))
	}
	var block []uint16
	for _, kv := range kept {
		u, err := windows.UTF16FromString(kv)
		if err != nil {
			return nil, fmt.Errorf("environment %q: %w", kv, err)
		}
		block = append(block, u...)
	}
	return append(block, 0), nil
}
