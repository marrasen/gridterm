package serve

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"golang.org/x/crypto/ssh"
)

// servingPictures is a window that takes pictures, and what it took.
func servingPictures(t *testing.T, put PicturePutter) (*Server, *Window) {
	t.Helper()
	mine, line := aKey(t, "marcus@laptop")
	host, err := HostKey(t.TempDir() + "/host_key")
	if err != nil {
		t.Fatalf("host key: %v", err)
	}
	keys, err := ParseAllowed([]byte(line), "the test")
	if err != nil {
		t.Fatalf("allowed: %v", err)
	}
	s, err := Listen(Config{
		Addr: "127.0.0.1:0", HostKey: host, Allowed: keys,
		Picture: put,
		OnError: func(error) {},
	})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	w, err := Dial(context.Background(), DialConfig{
		Addr: s.Addr(), Keys: []ssh.Signer{mine},
		HostKey: ssh.FixedHostKey(host.PublicKey()),
	})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })
	return s, w
}

// A picture sent from a client reaches the window being served, whole
// and in one piece.
func TestAPictureSentReachesTheWindowBeingServed(t *testing.T) {
	var mu sync.Mutex
	var got []byte
	_, w := servingPictures(t, func(png []byte) error {
		mu.Lock()
		defer mu.Unlock()
		got = append([]byte(nil), png...)
		return nil
	})
	// Longer than one packet, so a picture that arrived in pieces and
	// was put together wrongly shows up.
	want := make([]byte, 300<<10)
	for i := range want {
		want[i] = byte(i)
	}

	if err := w.SendPicture(want); err != nil {
		t.Fatalf("send it: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(got) != len(want) {
		t.Fatalf("it arrived as %d bytes, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("byte %d is %#x, want %#x", i, got[i], want[i])
		}
	}
}

// What the window could not do with a picture reaches the client, so the
// user is told rather than left thinking it landed.
func TestAPictureTheWindowWouldNotTakeIsSaidBack(t *testing.T) {
	_, w := servingPictures(t, func([]byte) error {
		return errors.New("the clipboard is held by something else")
	})

	err := w.SendPicture([]byte("a picture"))

	if err == nil {
		t.Fatal("a picture the window refused came back as sent")
	}
	if !strings.Contains(err.Error(), "held by something else") {
		t.Errorf("it says %q, want what the window said", err)
	}
}

// A window that takes no pictures turns one away by name.
func TestAWindowThatTakesNoPicturesSaysSo(t *testing.T) {
	_, w := servingPictures(t, nil)

	err := w.SendPicture([]byte("a picture"))

	if err == nil {
		t.Fatal("a window taking no pictures took one")
	}
	if !strings.Contains(err.Error(), "does not take pictures") {
		t.Errorf("it says %q", err)
	}
}

// A picture larger than a window takes is refused before it is sent, so
// a client cannot fill the other end's memory by trying.
func TestAPictureTooLargeIsRefusedBeforeItIsSent(t *testing.T) {
	var sent bool
	_, w := servingPictures(t, func([]byte) error {
		sent = true
		return nil
	})

	err := w.SendPicture(make([]byte, mostClipboardBytes+1))

	if err == nil {
		t.Fatal("a picture past the limit was sent")
	}
	if sent {
		t.Error("the window was given a picture past the limit it takes")
	}
}

// An empty picture is refused rather than emptying the other end's
// clipboard for nothing.
func TestAnEmptyPictureIsRefused(t *testing.T) {
	var given bool
	_, w := servingPictures(t, func([]byte) error {
		given = true
		return nil
	})

	err := w.SendPicture(nil)

	if err == nil {
		t.Fatal("an empty picture was sent")
	}
	if given {
		t.Error("the window was given an empty picture")
	}
}
