//go:build windows

package clip

import (
	"errors"
	"runtime"
	"strings"
	"testing"
	"time"

	win "golang.org/x/sys/windows"
)

// heldClipboard stands in for the operating system's clipboard,
// recording which thread took it and which gave it back.
//
// Stood in for rather than used: everything else here needs the real
// clipboard, and a test that took it would take it from whoever is using
// the machine.
func heldClipboard(t *testing.T, open func() error) (opened, closed *uint32) {
	t.Helper()
	wasOpen, wasClose := clipOpen, clipClose
	t.Cleanup(func() { clipOpen, clipClose = wasOpen, wasClose })

	opened, closed = new(uint32), new(uint32)
	clipOpen = func() error {
		if err := open(); err != nil {
			return err
		}
		*opened = win.GetCurrentThreadId()
		return nil
	}
	clipClose = func() error {
		*closed = win.GetCurrentThreadId()
		return nil
	}
	return opened, closed
}

// The clipboard is taken and given back on one thread.
//
// Windows gives the clipboard to the thread that opened it rather than
// to the process, and Go moves a goroutine from one thread to another
// whenever it likes. A close that lands on another thread fails, and a
// picture put on the clipboard is then never committed. That is what
// happened to a picture sent from another window: it arrives on a
// goroutine of the server's, where nothing holds the thread still.
func TestTheClipboardIsHeldOnOneThread(t *testing.T) {
	opened, closed := heldClipboard(t, func() error { return nil })

	// On a goroutine of its own, the way a picture from another window
	// arrives, and with every chance in the middle to be moved.
	done := make(chan error, 1)
	go func() {
		done <- withClipboard(func() error {
			for range 4 {
				runtime.Gosched()
				time.Sleep(time.Millisecond)
			}
			return nil
		})
	}()

	if err := <-done; err != nil {
		t.Fatalf("hold the clipboard: %v", err)
	}
	if *opened == 0 || *closed == 0 {
		t.Fatalf("it opened on thread %d and closed on %d, want both recorded", *opened, *closed)
	}
	if *opened != *closed {
		t.Errorf("the clipboard was taken on thread %d and given back on %d", *opened, *closed)
	}
}

// A close that fails is said rather than dropped: whatever was put on
// the clipboard is not there for anyone else.
func TestAClipboardCloseThatFailsIsSaid(t *testing.T) {
	wasOpen, wasClose := clipOpen, clipClose
	t.Cleanup(func() { clipOpen, clipClose = wasOpen, wasClose })
	clipOpen = func() error { return nil }
	clipClose = func() error { return errors.New("it belongs to another thread") }

	err := withClipboard(func() error { return nil })

	if err == nil {
		t.Fatal("a close that failed was dropped")
	}
	if !strings.Contains(err.Error(), "another thread") {
		t.Errorf("it said %q, want the reason the close gave", err)
	}
}

// What went wrong inside is said along with a close that also failed:
// they are two failures, not one.
func TestBothTheWorkAndTheCloseAreSaid(t *testing.T) {
	wasOpen, wasClose := clipOpen, clipClose
	t.Cleanup(func() { clipOpen, clipClose = wasOpen, wasClose })
	clipOpen = func() error { return nil }
	clipClose = func() error { return errors.New("the close went") }

	err := withClipboard(func() error { return errors.New("the picture went") })

	if err == nil {
		t.Fatal("neither failure was said")
	}
	for _, want := range []string{"the picture went", "the close went"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("it said %q, want it to say %q as well", err, want)
		}
	}
}

// The clipboard is one object shared by every program on the machine, so
// another holding it for a moment is waited for rather than taken as a
// failure.
func TestAClipboardHeldByAnotherProgramIsWaitedFor(t *testing.T) {
	tries := 0
	_, _ = heldClipboard(t, func() error {
		tries++
		if tries < 3 {
			return errors.New("another program has it")
		}
		return nil
	})

	if err := withClipboard(func() error { return nil }); err != nil {
		t.Fatalf("it gave up on a clipboard that was busy for a moment: %v", err)
	}
	if tries != 3 {
		t.Errorf("it asked %d times, want it to have waited for the third", tries)
	}
}

// And one held for good is given up on, with the reason, and the work is
// not done without it.
func TestAClipboardHeldForGoodIsGivenUpOn(t *testing.T) {
	_, _ = heldClipboard(t, func() error { return errors.New("another program has it") })
	ran := false

	err := withClipboard(func() error { ran = true; return nil })

	if err == nil {
		t.Fatal("it took a clipboard nothing would give up")
	}
	if ran {
		t.Error("it did the work without the clipboard")
	}
	if !strings.Contains(err.Error(), "another program has it") {
		t.Errorf("it said %q, want the reason the last try gave", err)
	}
}
