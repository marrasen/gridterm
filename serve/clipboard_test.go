package serve

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"golang.org/x/crypto/ssh"
)

// servingImages is a window that takes images, and what it took.
func servingImages(t *testing.T, put ImagePutter) (*Server, *Window) {
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
		Image:   put,
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

// An image sent from a client reaches the window being served, whole
// and in one piece.
func TestAnImageSentReachesTheWindowBeingServed(t *testing.T) {
	var mu sync.Mutex
	var got []byte
	_, w := servingImages(t, func(png []byte) error {
		mu.Lock()
		defer mu.Unlock()
		got = append([]byte(nil), png...)
		return nil
	})
	// Longer than one packet, so an image that arrived in pieces and
	// was put together wrongly shows up.
	want := make([]byte, 300<<10)
	for i := range want {
		want[i] = byte(i)
	}

	if err := w.SendImage(want); err != nil {
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

// What the window could not do with an image reaches the client, so the
// user is told rather than left thinking it landed.
func TestAnImageTheWindowWouldNotTakeIsSaidBack(t *testing.T) {
	_, w := servingImages(t, func([]byte) error {
		return errors.New("the clipboard is held by something else")
	})

	err := w.SendImage([]byte("an image"))

	if err == nil {
		t.Fatal("an image the window refused came back as sent")
	}
	if !strings.Contains(err.Error(), "held by something else") {
		t.Errorf("it says %q, want what the window said", err)
	}
}

// A window that takes no images turns one away by name.
func TestAWindowThatTakesNoImagesSaysSo(t *testing.T) {
	_, w := servingImages(t, nil)

	err := w.SendImage([]byte("an image"))

	if err == nil {
		t.Fatal("a window taking no images took one")
	}
	if !strings.Contains(err.Error(), "does not take images") {
		t.Errorf("it says %q", err)
	}
}

// An image larger than a window takes is refused before it is sent, so
// a client cannot fill the other end's memory by trying.
func TestAnImageTooLargeIsRefusedBeforeItIsSent(t *testing.T) {
	var sent bool
	_, w := servingImages(t, func([]byte) error {
		sent = true
		return nil
	})

	err := w.SendImage(make([]byte, mostClipboardBytes+1))

	if err == nil {
		t.Fatal("an image past the limit was sent")
	}
	if sent {
		t.Error("the window was given an image past the limit it takes")
	}
}

// An empty image is refused rather than emptying the other end's
// clipboard for nothing.
func TestAnEmptyImageIsRefused(t *testing.T) {
	var given bool
	_, w := servingImages(t, func([]byte) error {
		given = true
		return nil
	})

	err := w.SendImage(nil)

	if err == nil {
		t.Fatal("an empty image was sent")
	}
	if given {
		t.Error("the window was given an empty image")
	}
}
