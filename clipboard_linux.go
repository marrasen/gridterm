//go:build linux

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
	"slices"
	"sync"
	"time"

	"golang.design/x/clipboard"
)

// The clipboard on Linux goes through golang.design/x/clipboard, which
// talks to X11 itself rather than shelling out.
//
// Shelling out is what atotto/clipboard does, and it leaves the
// clipboard dead on any machine where neither xclip nor xsel is
// installed. Neither is part of a desktop, so that is an ordinary
// machine rather than a broken one. It also cannot carry a picture.
//
// One library for both is not only tidier. X11 hands the clipboard to a
// single owning process, so text written by a helper process and
// pictures written by this one would take the clipboard from each other
// on every copy.

// clipboardWait bounds a read.
//
// Reading asks whichever program owns the selection to hand the bytes
// over, and these are called from the paste path, on the goroutine that
// draws. A program that never answers would otherwise stop the window.
const clipboardWait = 2 * time.Second

// clipReady reports whether the clipboard can be reached, worked out
// once and remembered.
//
// It is not done at startup. A gridterm with no display still runs --
// one started over SSH to serve panes -- and there the clipboard is
// simply not among the things it can do. The library also warns that a
// Read or a Write after a failed Init may panic outright, so every call
// below asks this first.
var clipReady = sync.OnceValue(func() error {
	if err := clipboard.Init(); err != nil {
		return fmt.Errorf("reach the clipboard: %w", err)
	}
	return nil
})

// clipboardHas reports whether the clipboard holds a given format.
func clipboardHas(ctx context.Context, want clipboard.Format) (bool, error) {
	formats, err := clipboard.Formats(ctx)
	if err != nil {
		return false, fmt.Errorf("ask what is on the clipboard: %w", err)
	}
	return slices.Contains(formats, want), nil
}

// clipboardHasText reports whether there is any text on the clipboard.
func clipboardHasText() bool {
	if clipReady() != nil {
		// The clipboard cannot be reached at all. Saying yes sends the
		// paste down the text path, where the read reports why in words
		// the user sees. Saying no would paste nothing and explain
		// nothing, which reads like an empty clipboard.
		return true
	}
	ctx, cancel := context.WithTimeout(context.Background(), clipboardWait)
	defer cancel()

	has, err := clipboardHas(ctx, clipboard.FmtText)
	if err != nil {
		// Same reasoning: unknown is answered as yes, so the failure is
		// met on the path that can say it.
		return true
	}
	return has
}

// clipboardImage returns the picture on the clipboard.
//
// Whether there is one is reported separately from whether reading it
// failed, because the paste command says different things about them:
// an empty clipboard is an ordinary thing to meet and a clipboard that
// would not be read is not.
func clipboardImage() (image.Image, bool, error) {
	if err := clipReady(); err != nil {
		return nil, false, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), clipboardWait)
	defer cancel()

	switch has, err := clipboardHas(ctx, clipboard.FmtImage); {
	case err != nil:
		return nil, false, err
	case !has:
		return nil, false, nil
	}

	raw, err := clipboard.Read(ctx, clipboard.FmtImage)
	if err != nil {
		// It was there a moment ago. Either it has since been taken
		// away, or the program holding it would not hand it over: both
		// are failures rather than an empty clipboard, and pasting
		// nothing here would say neither.
		return nil, true, fmt.Errorf("read the picture on the clipboard: %w", err)
	}
	if len(raw) == 0 {
		return nil, true, errors.New("the picture on the clipboard is empty")
	}
	// Pictures are handed over PNG-encoded whatever the program that
	// copied one used.
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, true, fmt.Errorf("the picture on the clipboard will not open: %w", err)
	}
	return img, true, nil
}

// setClipboardImage puts a picture on the clipboard, PNG-encoded, which
// is how the library exchanges pictures and what carries the alpha.
//
// X11 gives the clipboard to the process that wrote it, which then
// serves the bytes to whoever pastes. So the picture is there for as
// long as this window is open, and a clipboard manager is what carries
// it past that.
func setClipboardImage(img image.Image) error {
	if err := clipReady(); err != nil {
		return err
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return fmt.Errorf("encode the picture: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), clipboardWait)
	defer cancel()

	if _, err := clipboard.Write(ctx, clipboard.FmtImage, buf.Bytes()); err != nil {
		return fmt.Errorf("put the picture on the clipboard: %w", err)
	}
	return nil
}

// writeClipboardText puts text on the clipboard.
func writeClipboardText(s string) error {
	if err := clipReady(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), clipboardWait)
	defer cancel()

	if _, err := clipboard.Write(ctx, clipboard.FmtText, []byte(s)); err != nil {
		return fmt.Errorf("put the text on the clipboard: %w", err)
	}
	return nil
}

// readClipboardText returns the text on the clipboard.
//
// It blocks, so it is called from the paste path only, where the user
// is already waiting. A failure is returned rather than pasted as
// nothing: a paste that does nothing looks exactly like an empty
// clipboard, and the user tries again instead of being told why.
func readClipboardText() (string, error) {
	if err := clipReady(); err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), clipboardWait)
	defer cancel()

	raw, err := clipboard.Read(ctx, clipboard.FmtText)
	if err != nil {
		return "", fmt.Errorf("read the clipboard: %w", err)
	}
	return string(raw), nil
}
