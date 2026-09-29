package serve

import (
	"context"
	"errors"
	"fmt"
	"io"

	"golang.org/x/crypto/ssh"
)

// ImagePutter takes an image a client sent and puts it somewhere the
// programs on this machine can paste it, which is the system clipboard.
//
// It is called from the goroutine serving that client, so an
// implementation that touches a window has to hand the work over to
// whatever draws. It is called with the bytes of a PNG.
//
// A nil one means this window does not take images, and a client that
// sends one is told so.
type ImagePutter func(png []byte) error

// runClipboard reads an image from a client and hands it to the window.
//
// The answer goes back down the channel: nothing at all when the image
// landed, and the reason when it did not. A client reads to the end and
// takes what it read as the failure.
func (s *Server) runClipboard(ctx context.Context, nch ssh.NewChannel) {
	if s.cfg.Image == nil {
		_ = nch.Reject(ssh.Prohibited, "this kakel does not take images")
		return
	}
	ch, reqs, err := nch.Accept()
	if err != nil {
		s.onError(fmt.Errorf("serve: take an image: %w", err))
		return
	}
	defer func() { _ = ch.Close() }()
	go ssh.DiscardRequests(reqs)

	if err := s.takeImage(ctx, ch); err != nil {
		s.onError(fmt.Errorf("serve: an image from a client: %w", err))
		// Said down the channel as well, because the client is waiting
		// to hear and has nowhere else to learn it from.
		_, _ = io.WriteString(ch, err.Error())
	}
}

// takeImage reads the bytes and puts them on the clipboard.
func (s *Server) takeImage(ctx context.Context, ch io.Reader) error {
	// One byte past the limit is read on purpose: a read that stopped
	// exactly at it cannot tell an image that fits from one that was
	// cut off.
	raw, err := io.ReadAll(io.LimitReader(ch, mostClipboardBytes+1))
	if err != nil {
		return fmt.Errorf("read it: %w", err)
	}
	if len(raw) == 0 {
		return errors.New("the image was empty")
	}
	if len(raw) > mostClipboardBytes {
		return fmt.Errorf("the image is larger than the %d bytes this window takes", mostClipboardBytes)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.cfg.Image(raw)
}
