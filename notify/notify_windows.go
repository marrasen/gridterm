//go:build windows

package notify

import (
	"fmt"
	"os"
	"runtime"
	"unsafe"

	win "golang.org/x/sys/windows"
)

var (
	user32          = win.NewLazySystemDLL("user32.dll")
	createWindowEx  = user32.NewProc("CreateWindowExW")
	destroyWindow   = user32.NewProc("DestroyWindow")
	destroyIcon     = user32.NewProc("DestroyIcon")
	loadIcon        = user32.NewProc("LoadIconW")
	shell32         = win.NewLazySystemDLL("shell32.dll")
	shellNotifyIcon = shell32.NewProc("Shell_NotifyIconW")
	extractIcon     = shell32.NewProc("ExtractIconW")
)

// What Shell_NotifyIcon is being asked to do, and which fields are
// filled in.
const (
	nimAdd    = 0
	nimModify = 1
	nimDelete = 2

	nifIcon = 0x02
	nifTip  = 0x04
	nifInfo = 0x10

	// niifInfo draws the information glyph beside the message.
	niifInfo = 0x01
)

// hwndMessage is the parent that makes a window a message-only one: no
// pixels, nowhere on the screen, and not in the task bar.
const hwndMessage = ^uintptr(2) // (HWND)-3

// idiApplication is the icon Windows falls back to.
const idiApplication = 32512

// notifyIconData is NOTIFYICONDATAW. The field order and the sizes are
// the operating system's, so nothing here may be reordered.
type notifyIconData struct {
	CbSize           uint32
	HWnd             win.HWND
	UID              uint32
	UFlags           uint32
	UCallbackMessage uint32
	HIcon            win.Handle
	SzTip            [128]uint16
	DwState          uint32
	DwStateMask      uint32
	SzInfo           [256]uint16
	UVersion         uint32
	SzInfoTitle      [64]uint16
	DwInfoFlags      uint32
	GuidItem         win.GUID
	HBalloonIcon     win.Handle
}

// toast shows messages through the notification area, which is where
// Windows turns a message from a program into a pop-up and a line in
// the action centre.
//
// Everything happens on one thread of its own. A window belongs to the
// thread that made it, and Show is called from the goroutine that
// draws, which must not be held up by the shell.
type toast struct {
	ask  chan message
	done chan struct{}
}

// message is one thing to show, and somewhere to put what went wrong.
type message struct {
	title, body string
	back        chan error
}

// New returns something that shows messages outside the window. name
// is what the notification area calls this program.
//
// It never fails: a machine that will not show one is a machine where
// the row in the sidebar is the whole of it.
func New(name string) Toaster {
	t := &toast{ask: make(chan message), done: make(chan struct{})}
	ready := make(chan bool)
	go t.serve(name, ready)
	if !<-ready {
		return nothing{}
	}
	return t
}

// serve owns the window and the icon, from the one thread that made
// them until Close.
func (t *toast) serve(name string, ready chan<- bool) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	data, ok := addIcon(name)
	ready <- ok
	if !ok {
		return
	}
	for {
		select {
		case m := <-t.ask:
			m.back <- show(&data, m.title, m.body)
		case <-t.done:
			remove(&data)
			close(t.done)
			return
		}
	}
}

// addIcon puts this program in the notification area, which is what a
// message is shown against.
func addIcon(name string) (notifyIconData, bool) {
	hwnd, _, _ := createWindowEx.Call(0,
		uintptr(unsafe.Pointer(win.StringToUTF16Ptr("STATIC"))),
		uintptr(unsafe.Pointer(win.StringToUTF16Ptr(""))),
		0, 0, 0, 0, 0, hwndMessage, 0, 0, 0)
	if hwnd == 0 {
		return notifyIconData{}, false
	}
	data := notifyIconData{
		HWnd:   win.HWND(hwnd),
		UID:    1,
		UFlags: nifIcon | nifTip,
		HIcon:  ownIcon(),
	}
	data.CbSize = uint32(unsafe.Sizeof(data))
	copy(data.SzTip[:len(data.SzTip)-1], win.StringToUTF16(name))
	if r, _, _ := shellNotifyIcon.Call(nimAdd, uintptr(unsafe.Pointer(&data))); r == 0 {
		destroyWindow.Call(hwnd)
		return notifyIconData{}, false
	}
	return data, true
}

// ownIcon is this program's own icon, taken out of its executable, and
// the one Windows falls back to when that cannot be read.
func ownIcon() win.Handle {
	if exe, err := os.Executable(); err == nil {
		h, _, _ := extractIcon.Call(0,
			uintptr(unsafe.Pointer(win.StringToUTF16Ptr(exe))), 0)
		// One means the file holds no icons, and zero means it could
		// not be read. Neither is a handle.
		if h > 1 {
			return win.Handle(h)
		}
	}
	h, _, _ := loadIcon.Call(0, idiApplication)
	return win.Handle(h)
}

// show puts one message up against the icon.
func show(data *notifyIconData, title, body string) error {
	data.UFlags = nifInfo
	data.DwInfoFlags = niifInfo
	clear(data.SzInfoTitle[:])
	clear(data.SzInfo[:])
	copy(data.SzInfoTitle[:len(data.SzInfoTitle)-1], win.StringToUTF16(title))
	copy(data.SzInfo[:len(data.SzInfo)-1], win.StringToUTF16(body))
	r, _, err := shellNotifyIcon.Call(nimModify, uintptr(unsafe.Pointer(data)))
	if r == 0 {
		return fmt.Errorf("notify: show a message: %w", err)
	}
	return nil
}

// remove takes the icon away again.
func remove(data *notifyIconData) {
	shellNotifyIcon.Call(nimDelete, uintptr(unsafe.Pointer(data)))
	if data.HIcon != 0 {
		destroyIcon.Call(uintptr(data.HIcon))
	}
	destroyWindow.Call(uintptr(data.HWnd))
}

// Show puts one message up and waits only for the shell to take it.
func (t *toast) Show(title, body string) error {
	back := make(chan error, 1)
	select {
	case t.ask <- message{title: title, body: body, back: back}:
		return <-back
	case <-t.done:
		return fmt.Errorf("notify: this window has stopped showing messages")
	}
}

// Close takes the icon out of the notification area.
func (t *toast) Close() error {
	select {
	case <-t.done:
		return nil
	default:
	}
	t.done <- struct{}{}
	// Closed by the goroutine once the icon has gone, so this waits for
	// it: an icon left behind sits there until something hovers over it.
	<-t.done
	return nil
}
